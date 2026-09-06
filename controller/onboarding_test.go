package controller

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupOnboardingTestDB(t *testing.T) *gin.Engine {
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
	require.NoError(t, db.AutoMigrate(
		&model.Model{},
		&model.CanvasCatalogModel{},
		&model.ModelGroupPrice{},
		&model.Ability{},
		&model.Option{},
	))

	// 保存并恢复进程级全局状态:五张价格表 + 自用模式开关 + 忽略名单 option,
	// 防止被同包其它测试污染、也防止本测试污染其它测试。
	prevPrice := ratio_setting.ModelPrice2JSONString()
	prevRatio := ratio_setting.ModelRatio2JSONString()
	prevVideoSecond := ratio_setting.VideoSecondPrice2JSONString()
	prevVideoTiers := ratio_setting.VideoPriceTiers2JSONString()
	prevInputMaterial := ratio_setting.InputMaterialPrices2JSONString()
	prevSelfUse := operation_setting.SelfUseModeEnabled
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(prevPrice))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(prevRatio))
		require.NoError(t, ratio_setting.UpdateVideoSecondPriceByJSONString(prevVideoSecond))
		require.NoError(t, ratio_setting.UpdateVideoPriceTiersByJSONString(prevVideoTiers))
		require.NoError(t, ratio_setting.UpdateInputMaterialPricesByJSONString(prevInputMaterial))
		operation_setting.SelfUseModeEnabled = prevSelfUse
		common.OptionMapRWMutex.Lock()
		delete(common.OptionMap, "OnboardingIgnoredModels")
		common.OptionMapRWMutex.Unlock()
	})

	// 清空全局价格并关闭自用模式,保证"未定价"判定从干净状态出发;忽略名单清零。
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString("{}"))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString("{}"))
	require.NoError(t, ratio_setting.UpdateVideoSecondPriceByJSONString("{}"))
	require.NoError(t, ratio_setting.UpdateVideoPriceTiersByJSONString("{}"))
	require.NoError(t, ratio_setting.UpdateInputMaterialPricesByJSONString("{}"))
	operation_setting.SelfUseModeEnabled = false
	// 生产环境由 model.InitOptionMap 初始化 OptionMap,单测没跑那条路径,先保证非 nil,
	// 否则 model.UpdateOption 写 option 时会 panic。
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	common.OptionMapRWMutex.Unlock()
	model.DB.Exec("DELETE FROM options")

	router := gin.New()
	router.GET("/api/onboarding/overview", GetOnboardingOverview)
	router.POST("/api/onboarding/launch", LaunchOnboardingModels)
	router.POST("/api/onboarding/ignore", IgnoreOnboardingModels)
	return router
}

func seedOnboardingAbility(t *testing.T, modelName string, channelID int) {
	t.Helper()
	ability := model.Ability{Group: "default", Model: modelName, ChannelId: channelID, Enabled: true}
	require.NoError(t, model.DB.Create(&ability).Error)
}

func seedOnboardingModelMeta(t *testing.T, modelName string, status int) {
	t.Helper()
	meta := &model.Model{ModelName: modelName, Status: status, SyncOfficial: 1}
	require.NoError(t, meta.Insert())
}

func seedOnboardingGroupPrice(t *testing.T, modelName string) {
	t.Helper()
	price := 1.0
	row := model.ModelGroupPrice{ModelName: modelName, GroupName: "default", ModelPrice: &price}
	require.NoError(t, model.DB.Create(&row).Error)
}

func seedOnboardingDraftCatalog(t *testing.T, remoteID string) {
	t.Helper()
	entry := model.CanvasCatalogModel{
		RemoteID: remoteID, DisplayName: remoteID, Capabilities: "video_gen",
		Contract: "relay_video_async_v1", Enabled: boolPtr(false),
	}
	require.NoError(t, entry.Insert())
}

type launchResult struct {
	Launched []string `json:"launched"`
	Unpriced []string `json:"unpriced"`
}

// TestOnboardingOverviewAggregates 钉住四列聚合:缺元数据 / 未定价 / 起草目录 /
// 忽略名单;且 ignored 从三列剔除。
func TestOnboardingOverviewAggregates(t *testing.T) {
	router := setupOnboardingTestDB(t)

	// 1. missing_meta:abilities 有、models 无。
	seedOnboardingAbility(t, "missing-only", 1)
	// 2. unpriced:abilities 有 + models 行,无任何计费配置。
	seedOnboardingAbility(t, "unpriced-model", 2)
	seedOnboardingModelMeta(t, "unpriced-model", 1)
	// 3. priced:abilities 有 + models 行 + 分组价行 → 不进任何待办列。
	seedOnboardingAbility(t, "priced-model", 3)
	seedOnboardingModelMeta(t, "priced-model", 1)
	seedOnboardingGroupPrice(t, "priced-model")
	// 4. ignored:abilities 有、models 无,但在忽略名单 → 从 missing_meta/unpriced 剔除。
	seedOnboardingAbility(t, "ignored-model", 4)
	// 5. draft_catalog:起草态(enabled=false)目录条目。
	seedOnboardingDraftCatalog(t, "draft-model")

	require.NoError(t, model.UpdateOption("OnboardingIgnoredModels", `["ignored-model"]`))

	w, resp := doJSON(t, router, "GET", "/api/onboarding/overview", nil)
	assert.Equal(t, 200, w.Code)
	require.True(t, resp.Success, resp.Message)

	var overview struct {
		MissingMeta  []string `json:"missing_meta"`
		Unpriced     []string `json:"unpriced"`
		DraftCatalog []struct {
			CatalogID    int    `json:"catalog_id"`
			RemoteID     string `json:"remote_id"`
			DisplayName  string `json:"display_name"`
			Capabilities string `json:"capabilities"`
			Contract     string `json:"contract"`
			Enabled      bool   `json:"enabled"`
		} `json:"draft_catalog"`
		Ignored []string `json:"ignored"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &overview))

	assert.ElementsMatch(t, []string{"missing-only"}, overview.MissingMeta,
		"ignored-model 应被忽略名单剔除")
	// missing-only 既缺元数据也无计费配置 → 同时出现在 unpriced 列(三列是
	// 独立清单,允许重叠)。
	assert.ElementsMatch(t, []string{"missing-only", "unpriced-model"}, overview.Unpriced)
	require.Len(t, overview.DraftCatalog, 1)
	assert.Equal(t, "draft-model", overview.DraftCatalog[0].RemoteID)
	assert.Equal(t, "video_gen", overview.DraftCatalog[0].Capabilities)
	assert.Equal(t, "relay_video_async_v1", overview.DraftCatalog[0].Contract)
	assert.False(t, overview.DraftCatalog[0].Enabled)
	assert.NotZero(t, overview.DraftCatalog[0].CatalogID)
	assert.ElementsMatch(t, []string{"ignored-model"}, overview.Ignored)
}

// TestOnboardingLaunchRejectsUnpricedWithoutForce 钉住未定价拦截:无 force 时
// 收进 unpriced、不启动、models 行保持起草态。
func TestOnboardingLaunchRejectsUnpricedWithoutForce(t *testing.T) {
	router := setupOnboardingTestDB(t)

	seedOnboardingAbility(t, "reject-model", 1)
	seedOnboardingModelMeta(t, "reject-model", 0)

	w, resp := doJSON(t, router, "POST", "/api/onboarding/launch", gin.H{
		"model_names": []string{"reject-model"},
	})
	assert.Equal(t, 200, w.Code)
	require.True(t, resp.Success, resp.Message)

	var result launchResult
	require.NoError(t, json.Unmarshal(resp.Data, &result))
	assert.Empty(t, result.Launched)
	assert.Equal(t, []string{"reject-model"}, result.Unpriced)

	var m model.Model
	require.NoError(t, model.DB.Where("model_name = ?", "reject-model").First(&m).Error)
	assert.Equal(t, 0, m.Status, "未定价且无 force 不得改动 models 行状态")
}

// TestOnboardingLaunchForcesUnpriced 钉住 force=true:未定价模型照常启动,
// models.status 置 1。
func TestOnboardingLaunchForcesUnpriced(t *testing.T) {
	router := setupOnboardingTestDB(t)

	seedOnboardingAbility(t, "force-model", 1)
	seedOnboardingModelMeta(t, "force-model", 0)

	w, resp := doJSON(t, router, "POST", "/api/onboarding/launch", gin.H{
		"model_names": []string{"force-model"},
		"force":       true,
	})
	assert.Equal(t, 200, w.Code)
	require.True(t, resp.Success, resp.Message)

	var result launchResult
	require.NoError(t, json.Unmarshal(resp.Data, &result))
	assert.Equal(t, []string{"force-model"}, result.Launched)
	assert.Empty(t, result.Unpriced)

	var m model.Model
	require.NoError(t, model.DB.Where("model_name = ?", "force-model").First(&m).Error)
	assert.Equal(t, 1, m.Status)
}

// TestOnboardingLaunchCreatesMetaAndEnablesCatalog 钉住完整开闸:已定价模型无
// models 行时新建 status=1,起草态目录条目 enabled 置 true。
func TestOnboardingLaunchCreatesMetaAndEnablesCatalog(t *testing.T) {
	router := setupOnboardingTestDB(t)

	seedOnboardingAbility(t, "new-model", 1)
	seedOnboardingGroupPrice(t, "new-model")
	seedOnboardingDraftCatalog(t, "new-model")

	w, resp := doJSON(t, router, "POST", "/api/onboarding/launch", gin.H{
		"model_names": []string{"new-model"},
	})
	assert.Equal(t, 200, w.Code)
	require.True(t, resp.Success, resp.Message)

	var result launchResult
	require.NoError(t, json.Unmarshal(resp.Data, &result))
	assert.Equal(t, []string{"new-model"}, result.Launched)
	assert.Empty(t, result.Unpriced)

	var m model.Model
	require.NoError(t, model.DB.Where("model_name = ?", "new-model").First(&m).Error)
	assert.Equal(t, 1, m.Status, "无 models 行时应新建 status=1 的 meta 行")
	assert.True(t, m.GroupPricingEnabled,
		"已有分组定价行的模型,新建 meta 行必须继承 group_pricing_enabled=true,否则分组行失效")

	entry, err := model.GetCanvasCatalogModelByRemoteID("new-model")
	require.NoError(t, err)
	require.NotNil(t, entry)
	assert.True(t, entry.IsEnabled(), "起草态目录条目应被置为 enabled=true")
}

// TestOnboardingIgnoreWritesOption 钉住忽略名单:合并去重、保序、写 option 持久化。
func TestOnboardingIgnoreWritesOption(t *testing.T) {
	router := setupOnboardingTestDB(t)

	w, resp := doJSON(t, router, "POST", "/api/onboarding/ignore", gin.H{
		"model_names": []string{"m1", "m2", "m1"},
	})
	assert.Equal(t, 200, w.Code)
	require.True(t, resp.Success, resp.Message)

	var result struct {
		Ignored []string `json:"ignored"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &result))
	assert.Equal(t, []string{"m1", "m2"}, result.Ignored)

	// 再次忽略:m2 已存在、m3 新增,保序合并。
	_, resp = doJSON(t, router, "POST", "/api/onboarding/ignore", gin.H{
		"model_names": []string{"m2", "m3"},
	})
	require.True(t, resp.Success, resp.Message)
	require.NoError(t, json.Unmarshal(resp.Data, &result))
	assert.Equal(t, []string{"m1", "m2", "m3"}, result.Ignored)

	// option 已持久化。
	var stored []string
	require.NoError(t, json.Unmarshal([]byte(model.GetOption("OnboardingIgnoredModels")), &stored))
	assert.Equal(t, []string{"m1", "m2", "m3"}, stored)
}
