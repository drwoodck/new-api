package model

import (
	"errors"
	"fmt"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupDeviceTestDB 用每个测试独立的命名 DB，不用 "file::memory:?cache=shared" ——
// 那是进程级共享，行会在测试之间互相泄漏(见摸底笔记 relay-requires-user-agent 系列坑)。
func setupDeviceTestDB(t *testing.T) {
	t.Helper()
	dsn := fmt.Sprintf("file:device_binding_%s?mode=memory&cache=private", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&DeviceBinding{}, &Token{}))
	DB = db
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
	})
}

// 同一 install_id 重复绑定不占新槽位 —— 画布每次登录都会调,
// 否则用户登录三次就把自己锁死了
func TestBindDeviceIsIdempotentPerInstall(t *testing.T) {
	setupDeviceTestDB(t)
	for i := 0; i < 5; i++ {
		_, err := BindDevice(1, "install-a", "0.1.22", 100)
		if err != nil {
			t.Fatalf("第 %d 次绑定失败: %v", i+1, err)
		}
	}
	devices, err := ListUserDevices(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 {
		t.Errorf("同一 install_id 绑 5 次应只有 1 行,得到 %d", len(devices))
	}
}

// 上限 3 台 —— 第 4 个**不同** install_id 必须被拒
func TestBindDeviceEnforcesLimit(t *testing.T) {
	setupDeviceTestDB(t)
	for _, id := range []string{"i1", "i2", "i3"} {
		if _, err := BindDevice(1, id, "0.1.22", 100); err != nil {
			t.Fatalf("%s 应能绑定: %v", id, err)
		}
	}
	_, err := BindDevice(1, "i4", "0.1.22", 100)
	if !errors.Is(err, ErrDeviceLimitReached) {
		t.Errorf("第 4 台应返回 ErrDeviceLimitReached,得到 %v", err)
	}
}

// 上限是**每用户**的,不是全局
func TestDeviceLimitIsPerUser(t *testing.T) {
	setupDeviceTestDB(t)
	for _, id := range []string{"i1", "i2", "i3"} {
		if _, err := BindDevice(1, id, "0.1.22", 100); err != nil {
			t.Fatalf("%s 应能绑定: %v", id, err)
		}
	}
	// 另一个用户仍应能绑
	if _, err := BindDevice(2, "i1", "0.1.22", 200); err != nil {
		t.Errorf("用户 2 不该受用户 1 的上限影响: %v", err)
	}
}

// 解绑腾出槽位 —— 这是用户从「已达上限」自救的唯一路径
func TestUnbindFreesASlot(t *testing.T) {
	setupDeviceTestDB(t)
	for _, id := range []string{"i1", "i2", "i3"} {
		if _, err := BindDevice(1, id, "0.1.22", 100); err != nil {
			t.Fatalf("%s 应能绑定: %v", id, err)
		}
	}
	if err := UnbindDevice(1, "i2"); err != nil {
		t.Fatal(err)
	}
	if _, err := BindDevice(1, "i4", "0.1.22", 100); err != nil {
		t.Errorf("解绑后应能绑新设备: %v", err)
	}
}

// 不能解绑别人的设备
func TestUnbindRejectsForeignInstall(t *testing.T) {
	setupDeviceTestDB(t)
	if _, err := BindDevice(1, "i1", "0.1.22", 100); err != nil {
		t.Fatal(err)
	}
	if err := UnbindDevice(2, "i1"); err == nil {
		t.Error("用户 2 不该能解绑用户 1 的设备")
	}
	devices, _ := ListUserDevices(1)
	if len(devices) != 1 {
		t.Error("用户 1 的设备不该被别人删掉")
	}
}

// 解绑要连带删除对应 token —— 那才是真正的吊销
func TestUnbindDeletesAssociatedToken(t *testing.T) {
	setupDeviceTestDB(t)
	token := &Token{UserId: 1, Key: "test-device-token-key"}
	require.NoError(t, DB.Create(token).Error)

	_, err := BindDevice(1, "i1", "0.1.22", token.Id)
	require.NoError(t, err)

	require.NoError(t, UnbindDevice(1, "i1"))

	var count int64
	require.NoError(t, DB.Model(&Token{}).Where("id = ?", token.Id).Count(&count).Error)
	if count != 0 {
		t.Errorf("解绑设备应连带删除关联的 token,但 token 仍存在")
	}
}
