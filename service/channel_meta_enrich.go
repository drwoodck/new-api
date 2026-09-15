package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

// 本文件有两条**互相独立**的链路,刻意不合并:
//
//   - EnrichChannelModelMeta(富化):要联网,异步跑。填 models 表的元信息列。
//   - SyncModelStatusWithChannels(级联):纯 DB,同步跑。只写 models.status。
//
// 分开的理由是失败模式不同:上游超时或不可达时,状态镜像必须照常工作。把级联塞进
// 富化的 goroutine 里,会让「在渠道里删掉一个模型」在元信息页毫无反应 —— 而那件事
// 根本不需要网络,坏起来会是最难定位的一类 bug。两个函数各自的结构体与 SET 子句里
// 都不出现对方的列。

// —— 总开关 ——

// channelMetaEnrichEnabled 是「渠道路由上游元信息」总开关,默认开。与
// catalog_draft.go 的 catalogAutoDraftEnabled 同款破环桥:model 包加载选项时经
// common.SetChannelMetaEnrichEnabledOption 写这里的运行态(model 不能反向 import
// service,会成环)。
var channelMetaEnrichEnabled = true

// SetChannelMetaEnrichEnabled 设置「渠道元信息自动同步」总开关。
func SetChannelMetaEnrichEnabled(enabled bool) {
	channelMetaEnrichEnabled = enabled
}

// IsChannelMetaEnrichEnabled 返回「渠道元信息自动同步」总开关当前值。
func IsChannelMetaEnrichEnabled() bool {
	return channelMetaEnrichEnabled
}

func init() {
	common.SetChannelMetaEnrichEnabledOption = SetChannelMetaEnrichEnabled
}

const (
	// channelMetaEnrichTimeout 是单次富化的网络预算。它比渠道保存本身长得多,但只
	// 在后台 goroutine 里跑,不阻塞用户请求。
	channelMetaEnrichTimeout = 20 * time.Second

	// 以下上限与 Model 的列长逐字对齐(model/model_meta.go:27-30)。SQLite 不校验
	// 列长(照收),MySQL 非宽松模式报 1406、PostgreSQL 报 22001 —— 不钳制就会造出
	// 「只在 SQLite 通过」的假绿:本地测试全绿、线上整条 UPDATE 失败。
	// description 是 text,不需要钳制。
	maxDisplayNameRunes = 256
	maxIconRunes        = 128
	maxTagsRunes        = 255
)

// EnrichOptions 控制一次富化的范围。
type EnrichOptions struct {
	// ModelNames 为空表示「该渠道当前 Models 的全部」。
	ModelNames []string
}

// EnrichResult 汇报一次富化的结果,供日志与测试观察。
type EnrichResult struct {
	// Filled: model -> 本次真正写入的列名(按字典序,便于断言)。
	Filled map[string][]string
	// Skipped: model -> 跳过原因。**不静默**跳过是本类型存在的理由:
	// 「上游没有这个模型」在用户看来与「功能坏了」完全一样。
	Skipped map[string]string
	// Source 是实际外呼的 baseURL(空表示本次没有外呼)。
	Source string
}

// EnrichChannelModelMeta 从渠道上游的 /api/pricing 拉一次定价页,把命中模型的
// 显示名/说明/图标/标签/供应商**补进** models 表的空列。
//
// 三条硬约束:
//
//  1. **只填空,绝不覆盖**。人工改过的值不会被冲掉 —— 这是「元信息页可手工维护」
//     与「自动获取」能共存的前提。
//  2. **不碰 status**。本函数的 SET 子句里根本没有 status,启停是
//     SyncModelStatusWithChannels 的职责。两个函数各管各的列,不存在优先级之争。
//  3. **sync_official = 0 的行整行跳过**(元信息页上的 No Sync)。管理员把某行切成
//     No Sync 就是「这行我接管了」,富化与级联都不再碰它。
//
// 上游不可达、success=false、载荷不是信封时返回 error,不静默当成空结果。
func EnrichChannelModelMeta(ctx context.Context, channelID int, opts EnrichOptions) (*EnrichResult, error) {
	result := &EnrichResult{
		Filled:  map[string][]string{},
		Skipped: map[string]string{},
	}

	channel, err := model.GetChannelById(channelID, true)
	if err != nil {
		return nil, fmt.Errorf("查询渠道失败: %w", err)
	}

	names := dedupeTrimmedNames(opts.ModelNames)
	if len(names) == 0 {
		names = dedupeTrimmedNames(channel.GetModels())
	}
	if len(names) == 0 {
		return result, nil
	}

	// 只对**显式**配了 base_url 的渠道外呼。绝不能依赖 GetBaseURL() —— 它会按渠道
	// 类型回退到厂商默认域名(OpenAI、Sora 等),那样「保存一个渠道」就等于替服务器
	// 决定去打真实厂商,SSRF 面白白扩大一圈。没配 base_url 的渠道本来也没有可读的
	// 定价页。注意 PrefetchUpstreamPricing 内部自己会回退,所以这道守卫必须在这里
	// 先做,不能推给它。
	if channel.BaseURL == nil || strings.TrimSpace(*channel.BaseURL) == "" {
		for _, name := range names {
			result.Skipped[name] = "渠道未显式配置 base_url,跳过外呼"
		}
		return result, nil
	}

	// 显式传 names(而非留空):PrefetchUpstreamPricing 的缓存 key 含模型名哈希,
	// 传具体集合能让「同一渠道不同批次」各自命中缓存,而不是互相顶掉。
	entries, source, err := PrefetchUpstreamPricing(ctx, channelID, names)
	if err != nil {
		return nil, err
	}
	result.Source = source

	var rows []model.Model
	if err := model.DB.Where("model_name IN ?", names).Find(&rows).Error; err != nil {
		return nil, err
	}

	now := common.GetTimestamp()
	for i := range rows {
		row := &rows[i]
		if row.SyncOfficial == 0 {
			result.Skipped[row.ModelName] = "该行是 No Sync,自动同步不碰"
			continue
		}
		entry, ok := entries[row.ModelName]
		if !ok {
			result.Skipped[row.ModelName] = "上游定价页没有这个模型"
			continue
		}
		// 未过校验的条目整条不可信(数值越界 / billing_expr 编译失败),哨兵条目
		// (model_ratio=37.5)则是上游在拿占位数据应付 —— 两种情况下它的显示名同样
		// 不该被采信。宁可留空让管理员手填,也好过把占位串写进元信息页。
		if !entry.Valid {
			result.Skipped[row.ModelName] = fmt.Sprintf("上游条目未通过校验: %s", entry.Error)
			continue
		}
		if entry.Suspicious {
			result.Skipped[row.ModelName] = "上游条目疑似占位数据(model_ratio=37.5),不采信"
			continue
		}

		updates := map[string]interface{}{}
		// 每个字段都先看本行是不是空的 —— 只填空是这个函数的全部语义。
		if row.DisplayName == "" && strings.TrimSpace(entry.DisplayName) != "" {
			updates["display_name"] = clampRunes(entry.DisplayName, maxDisplayNameRunes)
		}
		if row.Description == "" && strings.TrimSpace(entry.Description) != "" {
			updates["description"] = entry.Description
		}
		if row.Icon == "" && strings.TrimSpace(entry.Icon) != "" {
			updates["icon"] = clampRunes(entry.Icon, maxIconRunes)
		}
		if row.Tags == "" && strings.TrimSpace(entry.Tags) != "" {
			updates["tags"] = clampRunes(entry.Tags, maxTagsRunes)
		}
		if row.VendorID == 0 && strings.TrimSpace(entry.VendorName) != "" {
			if vendorID := lookupVendorIDByName(entry.VendorName); vendorID != 0 {
				updates["vendor_id"] = vendorID
			}
		}
		if len(updates) == 0 {
			continue
		}

		updates["updated_time"] = now
		if err := model.DB.Model(&model.Model{}).Where("id = ?", row.Id).Updates(updates).Error; err != nil {
			return result, err
		}
		delete(updates, "updated_time")
		filled := make([]string, 0, len(updates))
		for column := range updates {
			filled = append(filled, column)
		}
		sort.Strings(filled)
		result.Filled[row.ModelName] = filled
	}

	return result, nil
}

// —— 异步触发(按渠道合并) ——

type channelMetaEnrichRequest struct {
	// all 为真表示「该渠道当前 Models 的全部」。它必须与 names 分开记:一个空集合
	// 如果和「全部」用同一个表示,请求会在排空循环里被当成无事可做丢掉。
	all   bool
	names map[string]struct{}
}

var (
	channelMetaEnrichMu      sync.Mutex
	channelMetaEnrichPending = map[int]*channelMetaEnrichRequest{}
	channelMetaEnrichRunning = map[int]bool{}
)

// TriggerChannelMetaEnrichAsync 在后台富化指定模型的元信息。modelNames 为空表示
// 该渠道当前的全部模型。
//
// 同一个渠道同时只跑一个 goroutine:后来到的请求并进待办集,由正在跑的那个**接着
// 再排空一轮**。不直接丢弃是刻意的 —— 保存渠道时恰好有一次富化在途,是很容易撞上
// 的时序,而丢掉的后果是「新加的模型没有显示名」,用户只会看到功能时灵时不灵。
//
// 它是同步返回的:调用方(controller)拿不到也无需等结果,失败只落日志。
func TriggerChannelMetaEnrichAsync(channelID int, modelNames []string) {
	if !IsChannelMetaEnrichEnabled() {
		return
	}

	channelMetaEnrichMu.Lock()
	req := channelMetaEnrichPending[channelID]
	if req == nil {
		req = &channelMetaEnrichRequest{names: map[string]struct{}{}}
		channelMetaEnrichPending[channelID] = req
	}
	if len(modelNames) == 0 {
		req.all = true
	}
	for _, raw := range modelNames {
		if name := strings.TrimSpace(raw); name != "" {
			req.names[name] = struct{}{}
		}
	}
	if channelMetaEnrichRunning[channelID] {
		channelMetaEnrichMu.Unlock()
		return
	}
	channelMetaEnrichRunning[channelID] = true
	channelMetaEnrichMu.Unlock()

	gopool.Go(func() { drainChannelMetaEnrich(channelID) })
}

// drainChannelMetaEnrich 反复取走待办并富化,直到没有新请求为止。
func drainChannelMetaEnrich(channelID int) {
	for {
		channelMetaEnrichMu.Lock()
		req := channelMetaEnrichPending[channelID]
		channelMetaEnrichPending[channelID] = nil
		channelMetaEnrichMu.Unlock()

		if req != nil && (req.all || len(req.names) > 0) {
			var names []string
			if !req.all {
				names = make([]string, 0, len(req.names))
				for name := range req.names {
					names = append(names, name)
				}
				sort.Strings(names)
			}

			// 必须用 context.Background():c.Request.Context() 会在响应写完后立刻
			// 取消,富化于是静默不生效 —— 表现和「功能没做」一模一样。
			ctx, cancel := context.WithTimeout(context.Background(), channelMetaEnrichTimeout)
			res, err := EnrichChannelModelMeta(ctx, channelID, EnrichOptions{ModelNames: names})
			cancel()

			if err != nil {
				common.SysError(fmt.Sprintf("模型元信息富化失败: channel_id=%d err=%v", channelID, err))
			} else if len(res.Filled) > 0 || len(res.Skipped) > 0 {
				common.SysLog(fmt.Sprintf("模型元信息富化完成: channel_id=%d 填充=%d 跳过=%d source=%s",
					channelID, len(res.Filled), len(res.Skipped), res.Source))
			}
		}

		channelMetaEnrichMu.Lock()
		next := channelMetaEnrichPending[channelID]
		if next == nil || (!next.all && len(next.names) == 0) {
			// 排空且无人再来 —— 摘掉运行标记。释放与检查在同一把锁里,所以不会有
			// 请求落在「已摘标记」与「已退出循环」之间被吞掉。
			delete(channelMetaEnrichPending, channelID)
			delete(channelMetaEnrichRunning, channelID)
			channelMetaEnrichMu.Unlock()
			return
		}
		channelMetaEnrichMu.Unlock()
	}
}

// —— 状态级联(同步、纯 DB) ——

// SyncModelStatusWithChannels 把 changedModels 的 models.status 对齐到「当前是否
// 仍被任何启用渠道提供」。双向:
//
//	从渠道移除且再无启用渠道提供 → status = 0
//	从渠道移除但仍有别的启用渠道提供 → status 保持 1
//	加回渠道或新增 → status = 1(不看历史上是谁禁的,渠道为准)
//
// 判据只锚定**入参这一个增量集合**,绝不做「全表按 ability 有无 reconcile」。后者会
// 把「只在 models 表里、既没接渠道也没建目录条目」的那批行全部误停 —— 那是合法的
// 既有状态(见 controller/canvas_catalog_overview_test.go 的
// TestCatalogOverviewIncludesModelRowsMissingFromAbilitiesAndCatalog),而且每 30
// 分钟的巡检会重犯一次。
//
// sync_official = 0 的行整行跳过:管理员接管了它,自动流程既不禁用它也不启用它。
// 只在值真的变化时才写,免得每次保存渠道都把 updated_time 推一遍。
//
// 返回本次实际改了状态的模型名。
func SyncModelStatusWithChannels(changedModels []string) ([]string, error) {
	names := dedupeTrimmedNames(changedModels)
	if len(names) == 0 {
		return nil, nil
	}

	covered, err := model.GetEnabledModelsAmong(names)
	if err != nil {
		// 查不到覆盖集合就一个都不写。宁可这次不同步,也不能因为一次查询失败把
		// 「仍被别的渠道提供」的模型误停成禁用 —— 那是不可逆的对外表现。
		return nil, err
	}

	var rows []model.Model
	if err := model.DB.Where("model_name IN ?", names).Find(&rows).Error; err != nil {
		return nil, err
	}

	now := common.GetTimestamp()
	changed := make([]string, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		if row.SyncOfficial == 0 {
			continue
		}
		want := 0
		if _, ok := covered[row.ModelName]; ok {
			want = 1
		}
		if row.Status == want {
			continue
		}
		if err := model.DB.Model(&model.Model{}).Where("id = ?", row.Id).
			Updates(map[string]interface{}{"status": want, "updated_time": now}).Error; err != nil {
			return changed, err
		}
		changed = append(changed, row.ModelName)
	}
	return changed, nil
}

// —— 内部小工具 ——

// dedupeTrimmedNames 去空白、去重,顺序保持首次出现的次序。
func dedupeTrimmedNames(raw []string) []string {
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		name := strings.TrimSpace(item)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

// clampRunes 按 rune(不是字节)截断,与列长的语义一致。
func clampRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max])
}

// lookupVendorIDByName 把上游给的供应商名映射到本地 vendor 表的主键。查不到返回 0
// (调用方据此不写 vendor_id),不报错 —— 上游有个本地没登记过的供应商,不该让整次
// 富化失败。
func lookupVendorIDByName(name string) int {
	var vendor model.Vendor
	if err := model.DB.Where("name = ?", name).First(&vendor).Error; err != nil {
		return 0
	}
	return vendor.Id
}
