package controller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupDeviceControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	gin.SetMode(gin.TestMode)
	common.RedisEnabled = false

	// model.DB 是包级共享变量,model 包的 TestMain 把它建成一个迁移了
	// User/UserSession/Task 等表的全局库,供整个 model 测试二进制复用。
	// 把它换成一个只有 DeviceBinding/Token 的私有库却不换回来,会让
	// 本文件之后按顺序跑的其它包测试在缺表的库上失败——这里显式保存/还原。
	originalDB := model.DB

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db

	require.NoError(t, db.AutoMigrate(&model.DeviceBinding{}, &model.Token{}))

	t.Cleanup(func() {
		model.DB = originalDB
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

type deviceListResponse struct {
	Success bool         `json:"success"`
	Data    []DeviceInfo `json:"data"`
}

type deviceLimitResponse struct {
	Success bool   `json:"success"`
	Code    string `json:"code"`
	Data    struct {
		Limit   int          `json:"limit"`
		Devices []DeviceInfo `json:"devices"`
	} `json:"data"`
}

func TestGetUserDevicesReturnsOwnDevicesOnly(t *testing.T) {
	setupDeviceControllerTestDB(t)

	_, err := model.BindDevice(1, "install-a", "0.1.22", 100)
	require.NoError(t, err)
	_, err = model.BindDevice(2, "install-b", "0.1.22", 200)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/user/devices", nil)
	ctx.Set("id", 1)

	GetUserDevices(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var resp deviceListResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.True(t, resp.Success)
	require.Len(t, resp.Data, 1)
	require.Equal(t, "install-a", resp.Data[0].InstallID)
}

func TestDeleteUserDeviceRejectsForeignInstall(t *testing.T) {
	setupDeviceControllerTestDB(t)

	_, err := model.BindDevice(1, "install-a", "0.1.22", 100)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/api/user/devices/install-a", nil)
	ctx.Params = gin.Params{{Key: "install_id", Value: "install-a"}}
	ctx.Set("id", 2)

	DeleteUserDevice(ctx)

	devices, listErr := model.ListUserDevices(1)
	require.NoError(t, listErr)
	require.Len(t, devices, 1, "用户 2 不该能删掉用户 1 的设备")
}

func TestDeleteUserDeviceUnbindsOwnDevice(t *testing.T) {
	setupDeviceControllerTestDB(t)

	_, err := model.BindDevice(1, "install-a", "0.1.22", 0)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/api/user/devices/install-a", nil)
	ctx.Params = gin.Params{{Key: "install_id", Value: "install-a"}}
	ctx.Set("id", 1)

	DeleteUserDevice(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	devices, listErr := model.ListUserDevices(1)
	require.NoError(t, listErr)
	require.Empty(t, devices)
}

func TestBindCanvasDeviceAtLimitReturnsActionableDeviceList(t *testing.T) {
	setupDeviceControllerTestDB(t)

	for _, id := range []string{"i1", "i2", "i3"} {
		_, err := model.BindDevice(1, id, "0.1.22", 0)
		require.NoError(t, err)
	}

	body, _ := json.Marshal(deviceBindRequest{InstallID: "i4", ClientVersion: "0.1.22"})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/canvas/device-bind", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("id", 1)
	ctx.Set("token_id", 999)

	BindCanvasDevice(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var resp deviceLimitResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.False(t, resp.Success)
	require.Equal(t, "DEVICE_LIMIT_REACHED", resp.Code)
	require.Equal(t, model.MaxDevicesPerUser, resp.Data.Limit)
	require.Len(t, resp.Data.Devices, 3, "达到上限的响应必须带上已绑设备列表，否则用户不知道该解绑哪一个")
}

func TestBindCanvasDeviceIsIdempotent(t *testing.T) {
	setupDeviceControllerTestDB(t)

	body, _ := json.Marshal(deviceBindRequest{InstallID: "i1", ClientVersion: "0.1.22"})

	for i := 0; i < 3; i++ {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/api/canvas/device-bind", bytes.NewReader(body))
		ctx.Request.Header.Set("Content-Type", "application/json")
		ctx.Set("id", 1)
		ctx.Set("token_id", 100)

		BindCanvasDevice(ctx)
		require.Equal(t, http.StatusOK, recorder.Code)
	}

	devices, err := model.ListUserDevices(1)
	require.NoError(t, err)
	require.Len(t, devices, 1)
}
