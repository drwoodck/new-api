package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupChannelMetaEnrichTest 在 onboarding 夹具之上补 abilities/vendors 两张表。
// 前者的 AutoMigrate 只建了 channels/models/options/model_group_price,而级联的
// 覆盖判据要 INNER JOIN abilities。
func setupChannelMetaEnrichTest(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupOnboardingPrefetchTest(t)
	require.NoError(t, db.AutoMigrate(&model.Ability{}, &model.Vendor{}))
	return db
}

// channelMetaEnrichFixture 是富化用的 type2 响应。enrich-long-model 的
// display_name 特意给 300 个 rune(超过 varchar(256)),用来钉长度钳制。
func channelMetaEnrichFixture(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf(`{
	  "success": true,
	  "data": [
	    {
	      "model_name": "enrich-basic-model",
	      "quota_type": 0,
	      "model_ratio": 1.0,
	      "completion_ratio": 1.0,
	      "model_price": 0,
	      "display_name": "Seedance 2.0 全量版",
	      "description": "上游给的说明文案",
	      "icon": "Sparkles",
	      "tags": "video,text-to-video",
	      "vendor_name": "Acme Labs"
	    },
	    {
	      "model_name": "enrich-long-model",
	      "quota_type": 0,
	      "model_ratio": 1.0,
	      "completion_ratio": 1.0,
	      "model_price": 0,
	      "display_name": %q
	    },
	    {
	      "model_name": "enrich-manual-model",
	      "quota_type": 0,
	      "model_ratio": 1.0,
	      "completion_ratio": 1.0,
	      "model_price": 0,
	      "display_name": "上游想写进来的名字",
	      "description": "上游想写进来的说明"
	    },
	    {
	      "model_name": "enrich-sentinel-model",
	      "quota_type": 0,
	      "model_ratio": 37.5,
	      "completion_ratio": 1,
	      "model_price": 0,
	      "display_name": "占位显示名"
	    }
	  ]
	}`, strings.Repeat("字", 300))
}

// newEnrichServer 起一个假上游,返回请求计数。handler 在 server goroutine 里跑,
// 计数必须原子 —— 用计数器而不是掐时间:计时断言会随机器负载飘。
func newEnrichServer(t *testing.T, body func() string) (*httptest.Server, *int32) {
	t.Helper()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body()))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// seedEnrichChannel 建一个显式配了 base_url、Models 列已填的启用渠道。
// seedOnboardingChannel 不设 Models,而「空 EnrichOptions = 该渠道全部模型」这条
// 语义要靠它才覆盖得到。
func seedEnrichChannel(t *testing.T, db *gorm.DB, id int, baseURL string, models ...string) {
	t.Helper()
	baseURL = strings.TrimRight(baseURL, "/")
	require.NoError(t, db.Create(&model.Channel{
		Id:      id,
		Type:    1,
		Name:    fmt.Sprintf("enrich-ch-%d", id),
		BaseURL: &baseURL,
		Status:  1,
		Models:  strings.Join(models, ","),
	}).Error)
}

// seedCascadeChannel 建一个只关心启用状态的最小渠道(级联不看 base_url)。
func seedCascadeChannel(t *testing.T, db *gorm.DB, id int, status int) {
	t.Helper()
	require.NoError(t, db.Create(&model.Channel{
		Id: id, Type: 1, Name: fmt.Sprintf("cascade-ch-%d", id), Status: status,
	}).Error)
}

// seedCascadeAbility 造一条「渠道 id 提供模型 name」的启用能力行。
func seedCascadeAbility(t *testing.T, db *gorm.DB, channelID int, modelName string) {
	t.Helper()
	require.NoError(t, db.Create(&model.Ability{
		Group: "default", Model: modelName, ChannelId: channelID, Enabled: true,
	}).Error)
}

func readModelMetaRow(t *testing.T, db *gorm.DB, modelName string) model.Model {
	t.Helper()
	var row model.Model
	require.NoError(t, db.Where("model_name = ?", modelName).First(&row).Error)
	return row
}

// —— 富化 ——

// TestEnrichFillsEmptyDisplayNameAndDescription 钉住核心诉求:空 EnrichOptions
// 表示「该渠道当前全部模型」,元信息列被上游值填上。
func TestEnrichFillsEmptyDisplayNameAndDescription(t *testing.T) {
	db := setupChannelMetaEnrichTest(t)
	srv, _ := newEnrichServer(t, func() string { return channelMetaEnrichFixture(t) })
	seedEnrichChannel(t, db, 8201, srv.URL, "enrich-basic-model")
	require.NoError(t, db.Create(&model.Model{ModelName: "enrich-basic-model"}).Error)
	require.NoError(t, db.Create(&model.Vendor{Name: "Acme Labs"}).Error)
	var vendor model.Vendor
	require.NoError(t, db.Where("name = ?", "Acme Labs").First(&vendor).Error)

	res, err := EnrichChannelModelMeta(context.Background(), 8201, EnrichOptions{})
	require.NoError(t, err)

	row := readModelMetaRow(t, db, "enrich-basic-model")
	assert.Equal(t, "Seedance 2.0 全量版", row.DisplayName)
	assert.Equal(t, "上游给的说明文案", row.Description)
	assert.Equal(t, "Sparkles", row.Icon)
	assert.Equal(t, "video,text-to-video", row.Tags)
	assert.Equal(t, vendor.Id, row.VendorID)
	assert.ElementsMatch(t,
		[]string{"display_name", "description", "icon", "tags", "vendor_id"},
		res.Filled["enrich-basic-model"])
}

// TestEnrichDoesNotOverwriteManualEdits 钉住「只填空,绝不覆盖」。人工改过的值与
// 上游值必须共存 —— 这是元信息页可手工维护与自动获取能同时成立的前提。
func TestEnrichDoesNotOverwriteManualEdits(t *testing.T) {
	db := setupChannelMetaEnrichTest(t)
	srv, _ := newEnrichServer(t, func() string { return channelMetaEnrichFixture(t) })
	seedEnrichChannel(t, db, 8202, srv.URL, "enrich-manual-model")
	require.NoError(t, db.Create(&model.Model{
		ModelName:   "enrich-manual-model",
		DisplayName: "人工定稿的显示名",
		Description: "人工定稿的说明",
	}).Error)

	res, err := EnrichChannelModelMeta(context.Background(), 8202, EnrichOptions{})
	require.NoError(t, err)

	row := readModelMetaRow(t, db, "enrich-manual-model")
	assert.Equal(t, "人工定稿的显示名", row.DisplayName, "人工填过的显示名不得被上游覆盖")
	assert.Equal(t, "人工定稿的说明", row.Description, "人工填过的说明不得被上游覆盖")
	assert.NotContains(t, res.Filled, "enrich-manual-model", "无空列可填时不应产生写入")
}

// TestEnrichSkipsFrozenRow 钉住 No Sync 闸门:sync_official=0 的行整行跳过,
// 且跳过原因要留在 Skipped 里 —— 静默跳过在用户看来与「功能坏了」无法区分。
func TestEnrichSkipsFrozenRow(t *testing.T) {
	db := setupChannelMetaEnrichTest(t)
	srv, _ := newEnrichServer(t, func() string { return channelMetaEnrichFixture(t) })
	seedEnrichChannel(t, db, 8203, srv.URL, "enrich-basic-model")
	require.NoError(t, (&model.Model{ModelName: "enrich-basic-model", Status: 1, SyncOfficial: 0}).Insert())
	// 夹具哨兵:确认 0 真的落库了。带 default:1 标签的列在写零值时会被 GORM 从
	// INSERT 里省略、由数据库默认值接管,不校验的话后面全是假绿。
	require.Equal(t, 0, readModelMetaRow(t, db, "enrich-basic-model").SyncOfficial, "夹具失败:No Sync 行没造出来")

	res, err := EnrichChannelModelMeta(context.Background(), 8203, EnrichOptions{})
	require.NoError(t, err)

	row := readModelMetaRow(t, db, "enrich-basic-model")
	assert.Empty(t, row.DisplayName, "No Sync 行不得被富化")
	assert.Contains(t, res.Skipped["enrich-basic-model"], "No Sync")
}

// TestEnrichNeverTouchesStatus 钉住职责边界:富化函数只写元信息列,启停是
// SyncModelStatusWithChannels 的事。两者结构体与 SET 子句互不出现对方的列。
func TestEnrichNeverTouchesStatus(t *testing.T) {
	db := setupChannelMetaEnrichTest(t)
	srv, _ := newEnrichServer(t, func() string { return channelMetaEnrichFixture(t) })
	seedEnrichChannel(t, db, 8204, srv.URL, "enrich-basic-model")
	require.NoError(t, (&model.Model{ModelName: "enrich-basic-model", Status: 0, SyncOfficial: 1}).Insert())
	require.Equal(t, 0, readModelMetaRow(t, db, "enrich-basic-model").Status, "夹具失败:禁用行没造出来")

	_, err := EnrichChannelModelMeta(context.Background(), 8204, EnrichOptions{})
	require.NoError(t, err)

	assert.Equal(t, 0, readModelMetaRow(t, db, "enrich-basic-model").Status,
		"富化不得改写 status —— 那是级联函数的职责")
}

// TestEnrichMissingUpstreamEntry 钉住「上游没有这个模型」:不报错,但必须显式记进
// Skipped。这正是用户报的 sora 情形 —— 渠道里写着上游根本没有的模型名。
func TestEnrichMissingUpstreamEntry(t *testing.T) {
	db := setupChannelMetaEnrichTest(t)
	srv, _ := newEnrichServer(t, func() string { return channelMetaEnrichFixture(t) })
	seedEnrichChannel(t, db, 8205, srv.URL, "enrich-absent-model")
	require.NoError(t, db.Create(&model.Model{ModelName: "enrich-absent-model"}).Error)

	res, err := EnrichChannelModelMeta(context.Background(), 8205, EnrichOptions{})
	require.NoError(t, err, "上游缺该模型不是错误,整批不该失败")

	assert.Contains(t, res.Skipped["enrich-absent-model"], "上游定价页没有这个模型")
	assert.Empty(t, readModelMetaRow(t, db, "enrich-absent-model").DisplayName)
}

// TestEnrichSendsSingleRequestPerUpstream 钉住一次富化只外呼一次:渠道里有 N 个
// 模型不等于打 N 次上游。
func TestEnrichSendsSingleRequestPerUpstream(t *testing.T) {
	db := setupChannelMetaEnrichTest(t)
	srv, hits := newEnrichServer(t, func() string { return channelMetaEnrichFixture(t) })
	seedEnrichChannel(t, db, 8206, srv.URL, "enrich-basic-model", "enrich-long-model", "enrich-manual-model")
	for _, name := range []string{"enrich-basic-model", "enrich-long-model", "enrich-manual-model"} {
		require.NoError(t, db.Create(&model.Model{ModelName: name}).Error)
	}

	_, err := EnrichChannelModelMeta(context.Background(), 8206, EnrichOptions{})
	require.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(hits), "3 个模型仍应只打 1 次上游")
}

// TestEnrichClampsOverlongDisplayName 钉住按 rune 的长度钳制。SQLite 不校验列长
// (照收),MySQL 非宽松模式报 1406、PostgreSQL 报 22001 —— 不钳制就是「只在
// SQLite 通过」的假绿。
func TestEnrichClampsOverlongDisplayName(t *testing.T) {
	db := setupChannelMetaEnrichTest(t)
	srv, _ := newEnrichServer(t, func() string { return channelMetaEnrichFixture(t) })
	seedEnrichChannel(t, db, 8207, srv.URL, "enrich-long-model")
	require.NoError(t, db.Create(&model.Model{ModelName: "enrich-long-model"}).Error)

	_, err := EnrichChannelModelMeta(context.Background(), 8207, EnrichOptions{})
	require.NoError(t, err)

	got := readModelMetaRow(t, db, "enrich-long-model").DisplayName
	assert.Equal(t, maxDisplayNameRunes, len([]rune(got)), "超长显示名必须截到 varchar(256)")
	assert.Equal(t, strings.Repeat("字", maxDisplayNameRunes), got)
}

// TestEnrichRejectsNonEnvelopeBody 钉住 RC-1 的回归护栏:上游载荷不是信封
// ({success,data})时必须报错,不得静默当成空结果 —— 那个静默正是用户所报故障的
// 直接原因,原实现用 `_` 丢掉了这个错误。
func TestEnrichRejectsNonEnvelopeBody(t *testing.T) {
	db := setupChannelMetaEnrichTest(t)
	srv, _ := newEnrichServer(t, func() string { return `[]` })
	seedEnrichChannel(t, db, 8208, srv.URL, "enrich-basic-model")
	require.NoError(t, db.Create(&model.Model{ModelName: "enrich-basic-model"}).Error)

	_, err := EnrichChannelModelMeta(context.Background(), 8208, EnrichOptions{})
	require.Error(t, err, "裸数组载荷必须报错,不得静默成功")

	assert.Empty(t, readModelMetaRow(t, db, "enrich-basic-model").DisplayName)
}

// TestEnrichSkipsChannelWithoutExplicitBaseURL 钉住 SSRF 守卫:只对**显式**配了
// base_url 的渠道外呼。绝不能依赖 GetBaseURL() —— 它会按渠道类型回退到厂商默认
// 域名,那样「保存一个渠道」就等于替服务器决定去打真实厂商。
//
// 假上游在 --srv,渠道指向它;若守卫失效,计数器会立刻非零。
func TestEnrichSkipsChannelWithoutExplicitBaseURL(t *testing.T) {
	db := setupChannelMetaEnrichTest(t)
	srv, hits := newEnrichServer(t, func() string { return channelMetaEnrichFixture(t) })
	_ = srv
	require.NoError(t, db.Create(&model.Channel{
		Id: 8209, Type: 1, Name: "no-base-url-ch", Status: 1, Models: "enrich-basic-model",
	}).Error)
	require.NoError(t, db.Create(&model.Model{ModelName: "enrich-basic-model"}).Error)

	res, err := EnrichChannelModelMeta(context.Background(), 8209, EnrichOptions{})
	require.NoError(t, err, "没配 base_url 是常态,不该报错")

	assert.Zero(t, atomic.LoadInt32(hits), "未显式配置 base_url 的渠道不得发起任何外呼")
	assert.Contains(t, res.Skipped["enrich-basic-model"], "base_url")
	assert.Empty(t, readModelMetaRow(t, db, "enrich-basic-model").DisplayName)
}

// TestEnrichSkipsSuspiciousEntry 钉住哨兵条目不被采信:model_ratio=37.5 是上游拿
// 占位数据应付的标志,它的显示名同样不可信。
func TestEnrichSkipsSuspiciousEntry(t *testing.T) {
	db := setupChannelMetaEnrichTest(t)
	srv, _ := newEnrichServer(t, func() string { return channelMetaEnrichFixture(t) })
	seedEnrichChannel(t, db, 8210, srv.URL, "enrich-sentinel-model")
	require.NoError(t, db.Create(&model.Model{ModelName: "enrich-sentinel-model"}).Error)

	res, err := EnrichChannelModelMeta(context.Background(), 8210, EnrichOptions{})
	require.NoError(t, err)

	assert.Empty(t, readModelMetaRow(t, db, "enrich-sentinel-model").DisplayName)
	assert.Contains(t, res.Skipped["enrich-sentinel-model"], "占位")
}

// —— 状态级联 ——

// TestCascadeDisablesModelRemovedFromAllChannels 钉住移除方向:该模型已无任何
// 启用渠道提供 → status 置 0。
func TestCascadeDisablesModelRemovedFromAllChannels(t *testing.T) {
	db := setupChannelMetaEnrichTest(t)
	require.NoError(t, db.Create(&model.Model{ModelName: "cascade-gone", UpdatedTime: 1000}).Error)
	require.Equal(t, 1, readModelMetaRow(t, db, "cascade-gone").Status, "夹具失败:新行应当已启用")

	changed, err := SyncModelStatusWithChannels([]string{"cascade-gone"})
	require.NoError(t, err)

	assert.Equal(t, []string{"cascade-gone"}, changed)
	assert.Equal(t, 0, readModelMetaRow(t, db, "cascade-gone").Status)
}

// TestCascadeKeepsModelStillCoveredByAnotherChannel 钉住不许误停:从渠道 A 移除
// 但渠道 B 仍提供时,status 必须保持 1。
func TestCascadeKeepsModelStillCoveredByAnotherChannel(t *testing.T) {
	db := setupChannelMetaEnrichTest(t)
	seedCascadeChannel(t, db, 8301, 1)
	seedCascadeAbility(t, db, 8301, "cascade-shared")
	require.NoError(t, db.Create(&model.Model{ModelName: "cascade-shared"}).Error)

	changed, err := SyncModelStatusWithChannels([]string{"cascade-shared"})
	require.NoError(t, err)

	assert.Empty(t, changed, "仍被别的启用渠道提供,不得停用")
	assert.Equal(t, 1, readModelMetaRow(t, db, "cascade-shared").Status)
}

// TestCascadeReenablesModelAddedBackToChannel 钉住本次决策变更的核心:模型的
// status=0(无论之前是谁禁的)只要重新被启用渠道提供,就自动回到 1。渠道为准,
// 不看历史。
func TestCascadeReenablesModelAddedBackToChannel(t *testing.T) {
	db := setupChannelMetaEnrichTest(t)
	// SyncOfficial 必须显式写 1 并用哨兵断言钉住:Model.Insert 的二段式写回
	// (model/model_meta.go:79)会把传进来的 sync_official 强制落库,不写就是 0 ——
	// 而那会让级联把这行当 No Sync 整行跳过,测试红的理由就变成了夹具而不是实现。
	require.NoError(t, (&model.Model{ModelName: "cascade-back", Status: 0, SyncOfficial: 1}).Insert())
	fixture := readModelMetaRow(t, db, "cascade-back")
	require.Equal(t, 0, fixture.Status, "夹具失败:禁用行没造出来")
	require.Equal(t, 1, fixture.SyncOfficial, "夹具失败:这行必须跟随自动同步,否则级联会跳过它")

	// 加回渠道:能力行出现。
	seedCascadeChannel(t, db, 8302, 1)
	seedCascadeAbility(t, db, 8302, "cascade-back")

	changed, err := SyncModelStatusWithChannels([]string{"cascade-back"})
	require.NoError(t, err)

	assert.Equal(t, []string{"cascade-back"}, changed)
	assert.Equal(t, 1, readModelMetaRow(t, db, "cascade-back").Status, "渠道加回必须自动启用")
}

// TestCascadeSkipsNoSyncRow 钉住 No Sync 的双向豁免:既不被自动停用,也不被自动
// 启用。管理员切到 No Sync 就是「这行我接管了」。
func TestCascadeSkipsNoSyncRow(t *testing.T) {
	db := setupChannelMetaEnrichTest(t)
	require.NoError(t, (&model.Model{ModelName: "frozen-untouched", Status: 1, SyncOfficial: 0}).Insert())
	require.NoError(t, (&model.Model{ModelName: "frozen-stays-off", Status: 0, SyncOfficial: 0}).Insert())
	require.Equal(t, 0, readModelMetaRow(t, db, "frozen-untouched").SyncOfficial, "夹具失败")

	// frozen-stays-off 被渠道提供(应被启用),frozen-untouched 无渠道提供(应被停用);
	// 两者都是 No Sync,所以两边都不许动。
	seedCascadeChannel(t, db, 8303, 1)
	seedCascadeAbility(t, db, 8303, "frozen-stays-off")

	changed, err := SyncModelStatusWithChannels([]string{"frozen-untouched", "frozen-stays-off"})
	require.NoError(t, err)

	assert.Empty(t, changed, "No Sync 行双向都不动")
	assert.Equal(t, 1, readModelMetaRow(t, db, "frozen-untouched").Status, "No Sync 行不得被自动停用")
	assert.Equal(t, 0, readModelMetaRow(t, db, "frozen-stays-off").Status, "No Sync 行不得被自动启用")
}

// TestCascadeIgnoresMetaOnlyModel 钉死「不做全表 reconcile」。只在 models 表里、
// 既无渠道也无目录条目的行是合法的既有状态(见 controller 的
// TestCatalogOverviewIncludesModelRowsMissingFromAbilitiesAndCatalog);一旦有人把
// 级联改成按 ability 有无扫全表,这批运营方手工配过的行会被整片误停,且每 30 分钟
// 的巡检都会重犯一次。
func TestCascadeIgnoresMetaOnlyModel(t *testing.T) {
	db := setupChannelMetaEnrichTest(t)
	require.NoError(t, db.Create(&model.Model{ModelName: "meta-only-model"}).Error)
	seedCascadeChannel(t, db, 8304, 1)
	seedCascadeAbility(t, db, 8304, "cascade-other")

	changed, err := SyncModelStatusWithChannels([]string{"cascade-other"})
	require.NoError(t, err)

	assert.NotContains(t, changed, "meta-only-model")
	assert.Equal(t, 1, readModelMetaRow(t, db, "meta-only-model").Status,
		"不在增量集合里的行不得被扫到")
}

// TestCascadeWritesOnlyOnChange 钉住「只在值真变化时才写」:值没变就不碰
// updated_time,免得每次保存渠道都把时间戳推一遍。
//
// 夹具显式把 UpdatedTime 设成 1000 而不是靠 Create 落当前秒 —— 后者在快速测试里
// 与「写入后的时间」可能相同,断言就变成假绿了。
func TestCascadeWritesOnlyOnChange(t *testing.T) {
	db := setupChannelMetaEnrichTest(t)
	seedCascadeChannel(t, db, 8305, 1)
	seedCascadeAbility(t, db, 8305, "cascade-stable")
	require.NoError(t, db.Create(&model.Model{ModelName: "cascade-stable", UpdatedTime: 1000}).Error)
	require.Equal(t, int64(1000), readModelMetaRow(t, db, "cascade-stable").UpdatedTime, "夹具失败")

	changed, err := SyncModelStatusWithChannels([]string{"cascade-stable"})
	require.NoError(t, err)

	assert.Empty(t, changed)
	assert.Equal(t, int64(1000), readModelMetaRow(t, db, "cascade-stable").UpdatedTime,
		"值未变时不得写库,updated_time 不该被推进")
}

// TestCascadeFailsSafeOnQueryError 钉住 fail-safe:覆盖集合查不出来时一个都不写。
// 反过来(把空集当成「无渠道覆盖」)会因为一次查询失败把在线模型整片停用,且不可逆。
func TestCascadeFailsSafeOnQueryError(t *testing.T) {
	db := setupChannelMetaEnrichTest(t)
	require.NoError(t, db.Create(&model.Model{ModelName: "cascade-failsafe"}).Error)
	// 抽掉 abilities 表,让覆盖查询必然失败。
	require.NoError(t, db.Migrator().DropTable(&model.Ability{}))

	changed, err := SyncModelStatusWithChannels([]string{"cascade-failsafe"})
	require.Error(t, err, "覆盖查询失败必须把错误交出去")
	assert.Nil(t, changed)
	assert.Equal(t, 1, readModelMetaRow(t, db, "cascade-failsafe").Status,
		"查询失败时一个都不许写 —— 误停是不可逆的对外表现")
}

// —— 渠道启停 → 状态镜像(经 common.ChannelModelsStatusCascade 桥) ——

// setupChannelStatusMirrorTest 在元信息夹具之上补齐渠道启停链路的前置条件:
// 关掉记忆缓存(UpdateChannelStatus 缓存分支要 CacheGetChannel,单测没有缓存),
// 并用计数桥替换 init 注入的真桥 —— 还原时放回去的是真桥本身。
func setupChannelStatusMirrorTest(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupChannelMetaEnrichTest(t)

	prevCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = prevCache })
	return db
}

// recordCascadeCalls 把桥换成记录器,返回指向调用历史的指针。
func recordCascadeCalls(t *testing.T) *[][]string {
	t.Helper()
	calls := &[][]string{}
	prev := common.ChannelModelsStatusCascade
	common.ChannelModelsStatusCascade = func(models []string) {
		*calls = append(*calls, models)
	}
	t.Cleanup(func() { common.ChannelModelsStatusCascade = prev })
	return calls
}

// TestChannelStatusCascadeHookOnlyFiresOnChannelLevelChange 钉住桥的触发时机:
// 渠道整体状态迁移恰好触发一次、参数是渠道全部模型;多 key 渠道仅单个 key 被
// 禁时渠道状态没变,绝不触发 —— 否则只是抖掉一个坏 key 就会把元信息页整片翻白。
func TestChannelStatusCascadeHookOnlyFiresOnChannelLevelChange(t *testing.T) {
	db := setupChannelStatusMirrorTest(t)
	calls := recordCascadeCalls(t)

	require.NoError(t, db.Create(&model.Channel{
		Id: 8501, Type: 1, Name: "mirror-hook-single", Status: 1, Models: "m-hook-a,m-hook-b",
	}).Error)
	multiKey := &model.Channel{
		Id: 8502, Type: 1, Name: "mirror-hook-multi", Key: "key-a\nkey-b",
		Status: 1, Models: "m-hook-multi",
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
			MultiKeyMode: constant.MultiKeyModePolling,
		},
	}
	require.NoError(t, db.Create(multiKey).Error)

	// 单 key 渠道整体禁用 → 触发一次,参数是渠道全部模型。
	require.True(t, model.UpdateChannelStatus(8501, "", common.ChannelStatusManuallyDisabled, "test"))
	require.Len(t, *calls, 1)
	assert.ElementsMatch(t, []string{"m-hook-a", "m-hook-b"}, (*calls)[0])

	// 多 key 渠道仅禁一个 key:changed 仍为 true(key 状态落库了),但渠道级
	// 状态未迁移,桥不得触发。
	require.True(t, model.UpdateChannelStatus(8502, "key-a", common.ChannelStatusAutoDisabled, "key rejected"))
	require.Len(t, *calls, 1, "key 级禁用不许触发镜像")

	// 渠道整体启用回来 → 再触发一次。
	require.True(t, model.UpdateChannelStatus(8501, "", common.ChannelStatusEnabled, ""))
	require.Len(t, *calls, 2)
}

// TestChannelDisableMirrorsModelStatus 渠道停用方向的端到端(真桥):失去全部
// 启用渠道覆盖的模型自动置禁用,仍被别的启用渠道提供的保持启用。
func TestChannelDisableMirrorsModelStatus(t *testing.T) {
	db := setupChannelStatusMirrorTest(t)

	require.NoError(t, db.Create(&model.Channel{
		Id: 8511, Type: 1, Name: "mirror-disable-a", Status: 1, Models: "m-sole,m-shared",
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id: 8512, Type: 1, Name: "mirror-disable-b", Status: 1, Models: "m-shared",
	}).Error)
	seedCascadeAbility(t, db, 8511, "m-sole")
	seedCascadeAbility(t, db, 8511, "m-shared")
	seedCascadeAbility(t, db, 8512, "m-shared")
	require.NoError(t, db.Create(&model.Model{ModelName: "m-sole"}).Error)
	require.NoError(t, db.Create(&model.Model{ModelName: "m-shared"}).Error)

	require.True(t, model.UpdateChannelStatus(8511, "", common.ChannelStatusManuallyDisabled, "test"))

	assert.Equal(t, 0, readModelMetaRow(t, db, "m-sole").Status, "唯一启用渠道被停用,应自动禁用")
	assert.Equal(t, 1, readModelMetaRow(t, db, "m-shared").Status, "仍被渠道 B 提供,不得误停")
}

// TestChannelEnableMirrorsModelStatus 渠道启用方向的端到端(真桥):被禁用的模型
// 因渠道恢复提供而自动回到启用。渠道为准,与「加回模型自动启用」同一语义。
func TestChannelEnableMirrorsModelStatus(t *testing.T) {
	db := setupChannelStatusMirrorTest(t)

	require.NoError(t, db.Create(&model.Channel{
		Id: 8521, Type: 1, Name: "mirror-enable", Status: 2, Models: "m-revive",
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group: "default", Model: "m-revive", ChannelId: 8521, Enabled: false,
	}).Error)
	// SyncOfficial 必须显式钉 1(Insert 的二段式写回),否则级联把行当 No Sync
	// 整行跳过,测试红的理由就成了夹具而不是实现。
	require.NoError(t, (&model.Model{ModelName: "m-revive", Status: 0, SyncOfficial: 1}).Insert())
	require.Equal(t, 0, readModelMetaRow(t, db, "m-revive").Status, "夹具失败:禁用行没造出来")

	require.True(t, model.UpdateChannelStatus(8521, "", common.ChannelStatusEnabled, ""))

	assert.Equal(t, 1, readModelMetaRow(t, db, "m-revive").Status, "渠道恢复提供,模型应自动启用")
}

// TestChannelTagDisableMirrorsModelStatus 按标签批量停用的端到端(真桥):镜像
// 的受影响集合是该 tag 下全部渠道模型的并集(去重)。
func TestChannelTagDisableMirrorsModelStatus(t *testing.T) {
	db := setupChannelStatusMirrorTest(t)

	tag := "mirror-tag"
	require.NoError(t, db.Create(&model.Channel{
		Id: 8531, Type: 1, Name: "mirror-tag-a", Status: 1, Tag: &tag, Models: "m-tag-sole,m-dup",
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id: 8532, Type: 1, Name: "mirror-tag-b", Status: 1, Tag: &tag, Models: "m-tag-other,m-dup",
	}).Error)
	seedCascadeAbility(t, db, 8531, "m-tag-sole")
	seedCascadeAbility(t, db, 8531, "m-dup")
	seedCascadeAbility(t, db, 8532, "m-tag-other")
	seedCascadeAbility(t, db, 8532, "m-dup")
	for _, name := range []string{"m-tag-sole", "m-dup", "m-tag-other"} {
		require.NoError(t, db.Create(&model.Model{ModelName: name}).Error)
	}

	require.NoError(t, model.DisableChannelByTag(tag))

	for _, name := range []string{"m-tag-sole", "m-dup", "m-tag-other"} {
		assert.Equal(t, 0, readModelMetaRow(t, db, name).Status,
			"tag 内渠道全部停用后 %s 应自动禁用", name)
	}
}
