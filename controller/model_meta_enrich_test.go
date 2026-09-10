package controller

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupEnrichTestDB 给 enrichModels 一个独立内存库。enrichModels 的每个字段
// 都来自不同的缓存(models 行 / abilities 缓存 / 分组成员关系),夹具必须把这
// 三层都建出来,只建 models 行测出来的"空值"是没有意义的。
func setupEnrichTestDB(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)

	original := model.DB
	t.Cleanup(func() {
		model.DB = original
		model.InvalidatePricingCache()
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	model.DB = db
	require.NoError(t, db.AutoMigrate(
		&model.Ability{}, &model.Channel{}, &model.Model{}, &model.ModelGroupPrice{},
	))
	model.InvalidatePricingCache()
}

// enrichOne 走真实链路:插库 -> SearchModels 取行 -> enrichModels 填充。
func enrichOne(t *testing.T, modelName string) *model.Model {
	t.Helper()
	rows, _, err := model.SearchModels(modelName, "", "", "", 0, 100)
	require.NoError(t, err)
	require.Len(t, rows, 1, "夹具里应当只有这一条模型行")
	enrichModels(rows)
	return rows[0]
}

func seedChannel(t *testing.T, id int, status int) {
	t.Helper()
	require.NoError(t, model.DB.Create(&model.Channel{
		Id: id, Key: fmt.Sprintf("key-%d", id), Status: status, Name: fmt.Sprintf("channel-%d", id),
	}).Error)
}

// seedModelRow 落一条元信息行。status 必须用显式 Update 写:
// Model.Status 带 gorm:"default:1",结构体 Create 的零值会被列默认值覆盖,
// 直接 Create{Status:0} 存进去的是 1。
func seedModelRow(t *testing.T, modelName string, status int, nameRule int) {
	t.Helper()
	require.NoError(t, model.DB.Create(&model.Model{
		ModelName: modelName, Status: 1, NameRule: nameRule,
	}).Error)
	if status != 1 {
		require.NoError(t, model.DB.Model(&model.Model{}).
			Where("model_name = ?", modelName).
			Update("status", status).Error)
	}
}

// TestEnrichEnableGroupsScenarioMatrix 是「元信息页启用分组为什么是空的」的
// 场景矩阵:逐个组合 models 行 / abilities / 渠道状态,记录哪种组合会让
// Status=1 的模型拿到空 EnableGroups。矩阵本身即为回归契约。
func TestEnrichEnableGroupsScenarioMatrix(t *testing.T) {
	cases := []struct {
		name string
		// 元信息行的匹配规则与状态
		nameRule int
		// metaOff 表示元信息行 status=0
		metaOff bool
		// abilities 与渠道
		abilityModel string
		abilityGroup string
		abilityOff   bool
		channelID    int
		channelOff   bool
		// channelDeleted 表示渠道行根本不存在(没建过),区别于 channelOff(建了但停用)。
		channelDeleted bool
		secondGroup    string
		// 期望
		wantGroups []string
	}{
		{
			name:         "精确模型: 启用 ability + 启用渠道",
			abilityModel: "m-exact-live", abilityGroup: "default", channelID: 1,
			wantGroups: []string{"default"},
		},
		{
			name:         "精确模型: 启用 ability 但渠道已停用",
			abilityModel: "m-exact-live", abilityGroup: "default", channelID: 1, channelOff: true,
			wantGroups: []string{"default"},
		},
		{
			name:         "精确模型: ability 已停用",
			abilityModel: "m-exact-live", abilityGroup: "default", abilityOff: true, channelID: 1,
			wantGroups: nil,
		},
		{
			name:       "精确模型: 没有任何 ability",
			wantGroups: nil,
		},
		{
			name:         "精确模型: 元信息行停用 + 启用 ability",
			metaOff:      true,
			abilityModel: "m-exact-live", abilityGroup: "default", channelID: 1,
			wantGroups: nil,
		},
		{
			name:         "精确模型: 多分组 ability",
			abilityModel: "m-exact-live", abilityGroup: "default", channelID: 1, secondGroup: "vip",
			wantGroups: []string{"default", "vip"},
		},
		{
			name:         "前缀规则: 有命中的启用 ability",
			nameRule:     model.NameRulePrefix,
			abilityModel: "m-prefix-live", abilityGroup: "default", channelID: 1,
			wantGroups: []string{"default"},
		},
		{
			name:         "前缀规则: 启用 ability 但渠道已停用",
			nameRule:     model.NameRulePrefix,
			abilityModel: "m-prefix-live", abilityGroup: "default", channelID: 1, channelOff: true,
			wantGroups: []string{"default"},
		},
		{
			name:       "前缀规则: 没有任何命中的 ability",
			nameRule:   model.NameRulePrefix,
			wantGroups: nil,
		},
		{
			// 元信息行的名字与 ability 上的名字大小写/书写不一致时,查表必然落空。
			// 这是「模型明明在跑,元信息页启用分组却是 -」最隐蔽的一种成因。
			name:         "精确模型: 元信息名与 ability 名大小写不一致",
			abilityModel: "m-Exact-Live", abilityGroup: "default", channelID: 1,
			wantGroups: nil,
		},
		{
			// 渠道行被删掉后 ability 仍在:abilities.enabled 仍是 true,分组照出。
			// 它说明这一列的口径是「abilities 是否启用」,不是「渠道能不能跑」。
			name:         "精确模型: 启用 ability 但渠道行已删除",
			abilityModel: "m-exact-live", abilityGroup: "default", channelID: 1, channelDeleted: true,
			wantGroups: []string{"default"},
		},
	}

	for idx, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupEnrichTestDB(t)

			status := 1
			if tc.metaOff {
				status = 0
			}
			modelName := "m-exact-live"
			if tc.nameRule != model.NameRuleExact {
				modelName = "m-prefix-"
			}
			seedModelRow(t, modelName, status, tc.nameRule)

			if tc.channelID > 0 && !tc.channelDeleted {
				channelStatus := 1
				if tc.channelOff {
					channelStatus = 0
				}
				seedChannel(t, tc.channelID, channelStatus)
			}
			if tc.abilityModel != "" {
				require.NoError(t, model.DB.Create(&model.Ability{
					Group: tc.abilityGroup, Model: tc.abilityModel,
					ChannelId: tc.channelID, Enabled: !tc.abilityOff,
				}).Error)
			}
			if tc.secondGroup != "" {
				require.NoError(t, model.DB.Create(&model.Ability{
					Group: tc.secondGroup, Model: tc.abilityModel,
					ChannelId: tc.channelID, Enabled: true,
				}).Error)
			}

			model.InvalidatePricingCache()
			row := enrichOne(t, modelName)

			got := append([]string(nil), row.EnableGroups...)
			sort.Strings(got)
			want := append([]string(nil), tc.wantGroups...)
			sort.Strings(want)
			assert.Equal(t, want, got, "场景 #%d %s", idx, tc.name)
		})
	}
}
