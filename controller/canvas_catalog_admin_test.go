package controller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type adminApiResponse struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func setupCatalogAdminTestDB(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	model.DB = db
	require.NoError(t, db.AutoMigrate(&model.CanvasCatalogModel{}))

	router := gin.New()
	router.GET("/api/canvas/admin/models", GetAllCanvasCatalogModelsAdmin)
	router.GET("/api/canvas/admin/models/:id", GetCanvasCatalogModelAdmin)
	router.POST("/api/canvas/admin/models", CreateCanvasCatalogModelAdmin)
	router.PUT("/api/canvas/admin/models", UpdateCanvasCatalogModelAdmin)
	router.PUT("/api/canvas/admin/models/batch-enabled", BatchUpdateCanvasCatalogEnabledAdmin)
	router.DELETE("/api/canvas/admin/models/:id", DeleteCanvasCatalogModelAdmin)
	return router
}

func doJSON(t *testing.T, router *gin.Engine, method, path string, body any) (*httptest.ResponseRecorder, adminApiResponse) {
	t.Helper()
	var reqBody *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		reqBody = bytes.NewReader(b)
	} else {
		reqBody = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var resp adminApiResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return w, resp
}

func TestCreateCanvasCatalogModelAdmin(t *testing.T) {
	router := setupCatalogAdminTestDB(t)

	w, resp := doJSON(t, router, "POST", "/api/canvas/admin/models", model.CanvasCatalogModel{
		RemoteID:     "kungai-seedance-2",
		DisplayName:  "seedance-2.0",
		Capabilities: "video_gen",
		Contract:     "relay_video_async_v1",
		Enabled:      boolPtr(true),
	})
	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, resp.Success)

	var created model.CanvasCatalogModel
	require.NoError(t, json.Unmarshal(resp.Data, &created))
	assert.NotZero(t, created.Id)
	assert.Equal(t, "kungai-seedance-2", created.RemoteID)
}

func TestCreateCanvasCatalogModelAdminRejectsMissingFields(t *testing.T) {
	router := setupCatalogAdminTestDB(t)

	_, resp := doJSON(t, router, "POST", "/api/canvas/admin/models", model.CanvasCatalogModel{
		DisplayName: "no remote id",
		Contract:    "relay_video_async_v1",
	})
	assert.False(t, resp.Success)
}

func TestCreateCanvasCatalogModelAdminRejectsDuplicateRemoteID(t *testing.T) {
	router := setupCatalogAdminTestDB(t)

	first := model.CanvasCatalogModel{RemoteID: "dup-id", DisplayName: "First", Contract: "relay_video_async_v1"}
	require.NoError(t, first.Insert())

	_, resp := doJSON(t, router, "POST", "/api/canvas/admin/models", model.CanvasCatalogModel{
		RemoteID:    "dup-id",
		DisplayName: "Second",
		Contract:    "relay_video_async_v1",
	})
	assert.False(t, resp.Success)
}

func TestUpdateCanvasCatalogModelAdmin(t *testing.T) {
	router := setupCatalogAdminTestDB(t)

	existing := model.CanvasCatalogModel{RemoteID: "m1", DisplayName: "Old Name", Contract: "relay_video_async_v1"}
	require.NoError(t, existing.Insert())

	w, resp := doJSON(t, router, "PUT", "/api/canvas/admin/models", model.CanvasCatalogModel{
		Id:          existing.Id,
		RemoteID:    "m1",
		DisplayName: "New Name",
		Contract:    "relay_video_async_v1",
		Enabled:     boolPtr(true),
	})
	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, resp.Success)

	updated, err := model.GetCanvasCatalogModelByID(existing.Id)
	require.NoError(t, err)
	assert.Equal(t, "New Name", updated.DisplayName)
}

func TestUpdateCanvasCatalogModelAdminRejectsMissingID(t *testing.T) {
	router := setupCatalogAdminTestDB(t)

	_, resp := doJSON(t, router, "PUT", "/api/canvas/admin/models", model.CanvasCatalogModel{
		RemoteID:    "m1",
		DisplayName: "No ID",
		Contract:    "relay_video_async_v1",
	})
	assert.False(t, resp.Success)
}

func TestDeleteCanvasCatalogModelAdmin(t *testing.T) {
	router := setupCatalogAdminTestDB(t)

	existing := model.CanvasCatalogModel{RemoteID: "to-delete", DisplayName: "Delete Me", Contract: "relay_video_async_v1"}
	require.NoError(t, existing.Insert())

	w, resp := doJSON(t, router, "DELETE", "/api/canvas/admin/models/"+strconv.Itoa(existing.Id), nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, resp.Success)

	_, err := model.GetCanvasCatalogModelByID(existing.Id)
	assert.Error(t, err)
}

func TestGetAllCanvasCatalogModelsAdminIncludesDisabled(t *testing.T) {
	router := setupCatalogAdminTestDB(t)

	enabled := model.CanvasCatalogModel{RemoteID: "enabled-1", DisplayName: "Enabled", Contract: "c", Enabled: boolPtr(true)}
	disabled := model.CanvasCatalogModel{RemoteID: "disabled-1", DisplayName: "Disabled", Contract: "c", Enabled: boolPtr(false)}
	require.NoError(t, enabled.Insert())
	require.NoError(t, disabled.Insert())

	w, resp := doJSON(t, router, "GET", "/api/canvas/admin/models", nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, resp.Success)

	var all []model.CanvasCatalogModel
	require.NoError(t, json.Unmarshal(resp.Data, &all))
	assert.Len(t, all, 2)
}

// TestCreateCanvasCatalogModelAdminDerivesContractFromCapability 锁住 Task 3 的
// 契约推导:管理员填了 capability 但没填 contract 时,服务端要按
// constant.ContractForCapability 自动填上,而不是要求「contract 不能为空」。
func TestCreateCanvasCatalogModelAdminDerivesContractFromCapability(t *testing.T) {
	router := setupCatalogAdminTestDB(t)

	w, resp := doJSON(t, router, "POST", "/api/canvas/admin/models", model.CanvasCatalogModel{
		RemoteID:     "derive-video",
		DisplayName:  "Derive Video",
		Capabilities: "video_gen",
		// Contract 有意留空
	})
	assert.Equal(t, http.StatusOK, w.Code)
	require.True(t, resp.Success, resp.Message)

	var created model.CanvasCatalogModel
	require.NoError(t, json.Unmarshal(resp.Data, &created))
	assert.Equal(t, "relay_video_async_v1", created.Contract)
}

// TestCreateCanvasCatalogModelAdminManualContractWinsOverDerivation 锁住逃生舱:
// 管理员手填的 contract 永远优先,即便它和 capability 推导出的值不一致 ——
// 新契约/新 capability 上线前,人工兜底不能被自动推导覆盖。
func TestCreateCanvasCatalogModelAdminManualContractWinsOverDerivation(t *testing.T) {
	router := setupCatalogAdminTestDB(t)

	w, resp := doJSON(t, router, "POST", "/api/canvas/admin/models", model.CanvasCatalogModel{
		RemoteID:     "manual-override",
		DisplayName:  "Manual Override",
		Capabilities: "video_gen",
		Contract:     "relay_image_async_v1", // 故意填一个与推导结果不同的值
	})
	assert.Equal(t, http.StatusOK, w.Code)
	require.True(t, resp.Success, resp.Message)

	var created model.CanvasCatalogModel
	require.NoError(t, json.Unmarshal(resp.Data, &created))
	assert.Equal(t, "relay_image_async_v1", created.Contract, "手填必须优先于推导")
}

// TestCreateCanvasCatalogModelAdminUnknownCapabilityStillRequiresContract 锁住
// 「不猜」:capability 推导不出已知契约时,仍要求管理员手填,而不是留空硬塞进库。
func TestCreateCanvasCatalogModelAdminUnknownCapabilityStillRequiresContract(t *testing.T) {
	router := setupCatalogAdminTestDB(t)

	_, resp := doJSON(t, router, "POST", "/api/canvas/admin/models", model.CanvasCatalogModel{
		RemoteID:     "unknown-cap",
		DisplayName:  "Unknown Capability",
		Capabilities: "some_future_capability",
	})
	assert.False(t, resp.Success)
}

// TestUpdateCanvasCatalogModelAdminDerivesContractFromCapability 更新路径同 Create
// 一样支持推导 —— 管理员编辑既有条目、清空 contract 只填 capability 时也该生效。
func TestUpdateCanvasCatalogModelAdminDerivesContractFromCapability(t *testing.T) {
	router := setupCatalogAdminTestDB(t)

	existing := model.CanvasCatalogModel{
		RemoteID: "update-derive", DisplayName: "Update Derive",
		Capabilities: "image_gen", Contract: "relay_image_async_v1",
	}
	require.NoError(t, existing.Insert())

	existing.Contract = "" // 模拟管理员清空 contract,只保留 capability
	w, resp := doJSON(t, router, "PUT", "/api/canvas/admin/models", existing)
	assert.Equal(t, http.StatusOK, w.Code)
	require.True(t, resp.Success, resp.Message)

	var updated model.CanvasCatalogModel
	require.NoError(t, json.Unmarshal(resp.Data, &updated))
	assert.Equal(t, "relay_image_async_v1", updated.Contract)
}

// TestUpdateCanvasCatalogModelAdminDoesNotOverwriteDescription 锁住说明冻结:
// 说明真源已迁到 models 表,目录更新不得再用表单空值/旧值覆盖存量 description
// (2026-09-04 spec 3.7)。存量文字只读,供 wire 在无 models 行时回退。
func TestUpdateCanvasCatalogModelAdminDoesNotOverwriteDescription(t *testing.T) {
	router := setupCatalogAdminTestDB(t)

	existing := model.CanvasCatalogModel{
		RemoteID: "desc-frozen", DisplayName: "Old", Contract: "relay_video_async_v1",
		Description: "legacy text",
	}
	require.NoError(t, existing.Insert())

	w, resp := doJSON(t, router, "PUT", "/api/canvas/admin/models", model.CanvasCatalogModel{
		Id: existing.Id, RemoteID: "desc-frozen", DisplayName: "New",
		Contract: "relay_video_async_v1", Description: "hacked",
	})
	assert.Equal(t, http.StatusOK, w.Code)
	require.True(t, resp.Success, resp.Message)

	updated, err := model.GetCanvasCatalogModelByID(existing.Id)
	require.NoError(t, err)
	assert.Equal(t, "legacy text", updated.Description)
}

// TestCreateCanvasCatalogModelAdminIgnoresDescription 新建同样不接受 description
// 入参 —— 客户端说明统一从 models 表带出,目录侧不再产生新说明。
func TestCreateCanvasCatalogModelAdminIgnoresDescription(t *testing.T) {
	router := setupCatalogAdminTestDB(t)

	w, resp := doJSON(t, router, "POST", "/api/canvas/admin/models", model.CanvasCatalogModel{
		RemoteID: "desc-ignored", DisplayName: "Ignored", Contract: "relay_video_async_v1",
		Description: "should be dropped",
	})
	assert.Equal(t, http.StatusOK, w.Code)
	require.True(t, resp.Success, resp.Message)

	var created model.CanvasCatalogModel
	require.NoError(t, json.Unmarshal(resp.Data, &created))
	assert.Empty(t, created.Description)
}

// TestBatchUpdateCanvasCatalogEnabledAdmin 锁定批量上架/下架的契约:只更新
// 清单内的条目、不存在的 id 静默跳过(以 updated 回显实际行数)、空清单与
// 缺 enabled 取值必须被拒绝 —— enabled 是指针,false 与「未提供」必须分得开。
func TestBatchUpdateCanvasCatalogEnabledAdmin(t *testing.T) {
	router := setupCatalogAdminTestDB(t)
	ids := make([]int, 0, 3)
	for _, rid := range []string{"batch-a", "batch-b", "batch-c"} {
		w, resp := doJSON(t, router, "POST", "/api/canvas/admin/models", model.CanvasCatalogModel{
			RemoteID:     rid,
			DisplayName:  rid,
			Capabilities: "video_gen",
			Contract:     "relay_video_async_v1",
			Enabled:      boolPtr(true),
		})
		require.Equal(t, http.StatusOK, w.Code)
		require.True(t, resp.Success)
		var created model.CanvasCatalogModel
		require.NoError(t, json.Unmarshal(resp.Data, &created))
		ids = append(ids, created.Id)
	}

	// 批量下架前两个;第三个不在清单内,必须原样保留
	w, resp := doJSON(t, router, "PUT", "/api/canvas/admin/models/batch-enabled", map[string]any{
		"ids": []int{ids[0], ids[1], 99999}, "enabled": false,
	})
	require.Equal(t, http.StatusOK, w.Code)
	require.True(t, resp.Success)
	var data struct {
		Updated int64 `json:"updated"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &data))
	assert.Equal(t, int64(2), data.Updated, "不存在的 id 不计入 updated")

	rows, err := model.GetAllCanvasCatalogModelsAdmin()
	require.NoError(t, err)
	byID := make(map[int]model.CanvasCatalogModel, len(rows))
	for _, r := range rows {
		byID[r.Id] = r
	}
	first, second, third := byID[ids[0]], byID[ids[1]], byID[ids[2]]
	assert.False(t, first.IsEnabled(), "清单内的条目应被下架")
	assert.False(t, second.IsEnabled(), "清单内的条目应被下架")
	assert.True(t, third.IsEnabled(), "清单外的条目不得被牵连")

	// 批量重新上架
	w, resp = doJSON(t, router, "PUT", "/api/canvas/admin/models/batch-enabled", map[string]any{
		"ids": ids, "enabled": true,
	})
	require.Equal(t, http.StatusOK, w.Code)
	require.True(t, resp.Success)
	rows, err = model.GetAllCanvasCatalogModelsAdmin()
	require.NoError(t, err)
	for _, r := range rows {
		row := r
		assert.True(t, row.IsEnabled(), "批量上架后全部条目应为启用")
	}

	// 空清单拒绝
	w, resp = doJSON(t, router, "PUT", "/api/canvas/admin/models/batch-enabled", map[string]any{
		"ids": []int{}, "enabled": true,
	})
	assert.Equal(t, http.StatusOK, w.Code)
	assert.False(t, resp.Success, "空清单必须报错")

	// 缺 enabled 取值拒绝(false 与「未提供」必须区分)
	w, resp = doJSON(t, router, "PUT", "/api/canvas/admin/models/batch-enabled", map[string]any{
		"ids": ids,
	})
	assert.Equal(t, http.StatusOK, w.Code)
	assert.False(t, resp.Success, "缺 enabled 必须报错")
}
