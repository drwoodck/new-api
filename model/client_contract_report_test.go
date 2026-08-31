package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupClientContractReportTestDB 复用 model/canvas_catalog_test.go 里
// setupCanvasCatalogTestDB 的建库方式(sqlite 内存库),额外迁移本文件的表。
// 不新增测试基建,只是同一模式换一张表。
func setupClientContractReportTestDB(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	require.NoError(t, db.AutoMigrate(&ClientContractReport{}))
}

// 同一 install_id 重复上报只留一行 —— 画布每次登录/同步都会报,
// 追加会无界增长
func TestUpsertReplacesSameInstall(t *testing.T) {
	setupClientContractReportTestDB(t)

	r1 := &ClientContractReport{InstallID: "inst-1", ClientVersion: "0.1.20"}
	require.NoError(t, r1.SetContracts([]string{"video.v1"}))
	require.NoError(t, r1.Upsert())

	r2 := &ClientContractReport{InstallID: "inst-1", ClientVersion: "0.1.22"}
	require.NoError(t, r2.SetContracts([]string{"video.v1", "image.v1"}))
	require.NoError(t, r2.Upsert())

	var count int64
	require.NoError(t, DB.Model(&ClientContractReport{}).Where("install_id = ?", "inst-1").Count(&count).Error)
	assert.Equal(t, int64(1), count, "same install_id must replace, not append")

	var reloaded ClientContractReport
	require.NoError(t, DB.Where("install_id = ?", "inst-1").First(&reloaded).Error)
	assert.Equal(t, "0.1.22", reloaded.ClientVersion)
	assert.Equal(t, []string{"video.v1", "image.v1"}, reloaded.GetContracts())
}

// 支持率统计:契约在多少个在线装机里被支持
func TestContractSupportStatsCountsDistinctInstalls(t *testing.T) {
	setupClientContractReportTestDB(t)

	seed := []struct {
		installID string
		contracts []string
	}{
		{"inst-1", []string{"video.v1"}},
		{"inst-2", []string{"video.v1", "image.v1"}},
		{"inst-3", []string{"image.v1"}},
	}
	for _, s := range seed {
		r := &ClientContractReport{InstallID: s.installID, ClientVersion: "0.1.22"}
		require.NoError(t, r.SetContracts(s.contracts))
		require.NoError(t, r.Upsert())
	}

	stats, err := GetContractSupportStats(defaultReportWindowDays)
	require.NoError(t, err)

	require.Contains(t, stats, "video.v1")
	assert.Equal(t, 2, stats["video.v1"].Supported)
	assert.Equal(t, 3, stats["video.v1"].Total)

	require.Contains(t, stats, "image.v1")
	assert.Equal(t, 2, stats["image.v1"].Supported)
	assert.Equal(t, 3, stats["image.v1"].Total)

	online, err := CountOnlineInstalls(defaultReportWindowDays)
	require.NoError(t, err)
	assert.Equal(t, int64(3), online)
}

// 纯函数部分可以无 DB 测试:窗口截断的时间戳计算
func TestWindowCutoffRejectsNonPositive(t *testing.T) {
	// 窗口为 0 或负数时应回退到默认值,而不是把截断点算到未来
	// (那会让统计分母变成 0,支持率显示成 0/0)
	for _, days := range []int{0, -1, -100} {
		got := windowCutoff(days)
		want := windowCutoff(defaultReportWindowDays)
		// 允许 1 秒误差(两次调用之间的时间流逝)
		if got < want-1 || got > want+1 {
			t.Errorf("windowDays=%d 应回退到默认 %d 天,得到截断点 %d(期望约 %d)",
				days, defaultReportWindowDays, got, want)
		}
	}
}

// 契约名去重 —— 客户端可能重复上报同一契约
func TestNormalizeContractsDedupes(t *testing.T) {
	got := normalizeContracts([]string{"video.v1", "video.v1", "image.v1", "", "  "})
	if len(got) != 2 {
		t.Fatalf("期望 2 个去重后的契约,得到 %d: %v", len(got), got)
	}
	seen := map[string]bool{}
	for _, c := range got {
		if seen[c] {
			t.Errorf("重复项: %q", c)
		}
		seen[c] = true
	}
	if seen[""] {
		t.Error("空字符串不该被保留")
	}
}

// 上报载荷的必填校验
func TestValidateReportRejectsEmptyInstallID(t *testing.T) {
	r := &ClientContractReport{ClientVersion: "0.1.22"}
	if err := r.Validate(); err == nil {
		t.Error("install_id 为空应报错")
	}
	r.InstallID = "abc"
	r.ClientVersion = ""
	if err := r.Validate(); err == nil {
		t.Error("client_version 为空应报错")
	}
}
