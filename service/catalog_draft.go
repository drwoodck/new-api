package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
)

// channelTypeToCapability 把「渠道类型 → 画布 capability」收敛为一张显式表。
// 只收录有明确对应关系的任务/图片渠道;未知渠道不猜(返回 ""),条目保持
// capabilities/contract 为空,自然留在「未配置」。
//
// image_gen 盘点结论:new-api 的 OpenAI 风格图片生成路径是模型名驱动而非渠道
// 类型驱动 —— common.IsImageGenerationModel 按模型名(dall-e-*/gpt-image-1/
// imagen-/flux- 等)判定,几乎所有 OpenAI 兼容渠道的 adaptor 都有 ConvertImageRequest
// 透传实现(relay/image_handler.go 只按模型名走到图片模式,不按渠道类型分流);
// 而唯一明确的 gpt-image 系列跑在 ChannelTypeOpenAI 上,该类型已在此表里被
// video_gen 占用(OpenAI 兼容第三方走 Sora 适配器)。没有可独占映射为 image_gen
// 的渠道类型,故不猜、留空手填。
var channelTypeToCapability = map[int]string{
	constant.ChannelTypeSora:        "video_gen",
	constant.ChannelTypeOpenAI:      "video_gen", // OpenAI 兼容第三方走 Sora 适配器
	constant.ChannelTypeAli:         "video_gen",
	constant.ChannelTypeKling:       "video_gen",
	constant.ChannelTypeJimeng:      "video_gen",
	constant.ChannelTypeVertexAi:    "video_gen",
	constant.ChannelTypeVidu:        "video_gen",
	constant.ChannelTypeDoubaoVideo: "video_gen",
	constant.ChannelTypeVolcEngine:  "video_gen",
	constant.ChannelTypeGemini:      "video_gen",
	constant.ChannelTypeMiniMax:     "video_gen",
}

// ChannelTypeToCapability 返回渠道类型对应的画布 capability;未知渠道返回 ""。
func ChannelTypeToCapability(channelType int) string {
	return channelTypeToCapability[channelType]
}

// catalogAutoDraftEnabled 是「巡检自动起草」总开关,默认开。经
// model/option.go 的 CatalogAutoDraftEnabled 选项通过 SetCatalogAutoDraftEnabled
// 运行态改写;model 包不能反向 import service(service import model,会成环),
// 所以选项写入走 common.SetCatalogAutoDraftEnabledOption 这个 init 注册的函数钩子。
var catalogAutoDraftEnabled = true

// SetCatalogAutoDraftEnabled 设置「巡检自动起草」总开关。
func SetCatalogAutoDraftEnabled(enabled bool) {
	catalogAutoDraftEnabled = enabled
}

// IsCatalogAutoDraftEnabled 返回「巡检自动起草」总开关当前值。
func IsCatalogAutoDraftEnabled() bool {
	return catalogAutoDraftEnabled
}

func init() {
	common.SetCatalogAutoDraftEnabledOption = SetCatalogAutoDraftEnabled
}

// DraftCatalogEntries 对巡检新发现、已进渠道的模型自动起草画布目录条目与
// models meta 行。逐模型、逐事务:
//   - canvas_catalog_models 无 remote_id=模型名 条目(软删除也算存在)→ 创建
//     起草条目(enabled=false、display_name=模型名、capabilities/contract 按
//     渠道类型映射推导,未知留空);
//   - models 表无 model_name=模型名 行(软删除也算存在)→ 创建起草 meta 行
//     (status=0,停用/起草态,sync_official=1)。
//
// 幂等:已存在一律跳过,不覆盖任何既有状态。单模型失败记 log 继续,不阻塞
// 其它模型;返回实际起草模型数。总开关关闭时直接 (0, nil)。
func DraftCatalogEntries(channel *model.Channel, addedModels []string) (drafted int, err error) {
	if !IsCatalogAutoDraftEnabled() {
		return 0, nil
	}
	if channel == nil {
		return 0, fmt.Errorf("DraftCatalogEntries: channel is nil")
	}
	capability := ChannelTypeToCapability(channel.Type)
	contract, _ := constant.ContractForCapability(capability)

	seen := make(map[string]struct{}, len(addedModels))
	for _, raw := range addedModels {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}

		created, draftErr := draftCatalogEntryForModel(name, capability, contract)
		if draftErr != nil {
			common.SysError(fmt.Sprintf("画布目录自动起草失败: channel_id=%d model=%s err=%v", channel.Id, name, draftErr))
			continue
		}
		if created {
			drafted++
		}
	}
	return drafted, nil
}

// draftCatalogEntryForModel 为单个模型起草目录条目与 models meta 行,两表插入
// 在同一事务里,失败整体回滚该模型的起草。返回 created 表示本次是否新建了
// 至少一行(已存在的模型返回 false)。
//
// 2026-09-07: 新增自动从上游获取元信息功能,创建 models 行后立即尝试填充
// description、icon、vendor 等字段。上游获取失败不影响创建流程。
func draftCatalogEntryForModel(name, capability, contract string) (created bool, err error) {
	// 先尝试从上游获取元信息（在事务外执行，避免长时间锁表）
	upstreamInfo, upstreamErr := fetchUpstreamModelInfo(name)
	if upstreamErr != nil {
		common.SysLog(fmt.Sprintf("Failed to fetch upstream info for model %s: %v", name, upstreamErr))
	}

	err = model.DB.Transaction(func(tx *gorm.DB) error {
		// 两行同一事务内创建,时间戳取同一次值(其它创建路径均显式设时间戳,
		// 见 CanvasCatalogModel.Insert / Model.Insert,管理端按此显示创建时间)。
		now := common.GetTimestamp()
		var canvasCnt int64
		if err := tx.Unscoped().Model(&model.CanvasCatalogModel{}).
			Where("remote_id = ?", name).Count(&canvasCnt).Error; err != nil {
			return err
		}
		var metaCnt int64
		if err := tx.Unscoped().Model(&model.Model{}).
			Where("model_name = ?", name).Count(&metaCnt).Error; err != nil {
			return err
		}
		if canvasCnt == 0 {
			disabled := false
			entry := &model.CanvasCatalogModel{
				RemoteID:     name,
				DisplayName:  name,
				Capabilities: capability,
				Enabled:      &disabled,
				Contract:     contract,
				CreatedTime:  now,
				UpdatedTime:  now,
			}
			if err := tx.Create(entry).Error; err != nil {
				return err
			}
			created = true
		}
		if metaCnt == 0 {
			meta := &model.Model{
				ModelName:    name,
				Status:       0,
				SyncOfficial: 1,
				CreatedTime:  now,
				UpdatedTime:  now,
			}

			// 自动填充上游元信息
			if upstreamInfo != nil {
				meta.Description = upstreamInfo.Description
				meta.Icon = upstreamInfo.Icon
				meta.NameRule = upstreamInfo.NameRule
				// 如果有 vendor 信息，尝试查找对应的 vendor_id
				if upstreamInfo.VendorName != "" {
					var vendor model.Vendor
					if err := tx.Where("name = ?", upstreamInfo.VendorName).First(&vendor).Error; err == nil {
						meta.VendorID = vendor.Id
					}
				}
			}

			if err := tx.Create(meta).Error; err != nil {
				return err
			}
			// Status=0 会被 GORM 的 default:1 在 Create 时覆盖为 1,仿
			// model.Model.Insert 的二段式写回真实值,确保起草态(status=0)生效。
			updates := map[string]interface{}{
				"status":        0,
				"sync_official": 1,
			}
			// 保留上游填充的字段
			if upstreamInfo != nil {
				if upstreamInfo.Description != "" {
					updates["description"] = upstreamInfo.Description
				}
				if upstreamInfo.Icon != "" {
					updates["icon"] = upstreamInfo.Icon
				}
				if upstreamInfo.NameRule != 0 {
					updates["name_rule"] = upstreamInfo.NameRule
				}
				if meta.VendorID != 0 {
					updates["vendor_id"] = meta.VendorID
				}
			}
			if err := tx.Model(&model.Model{}).Where("id = ?", meta.Id).
				Updates(updates).Error; err != nil {
				return err
			}
			created = true
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return created, nil
}

// upstreamModelInfo 从上游获取的模型元信息
type upstreamModelInfo struct {
	ModelName   string `json:"model_name"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	VendorName  string `json:"vendor_name"`
	NameRule    int    `json:"name_rule"`
}

// fetchUpstreamModelInfo 从上游 API 获取单个模型的元信息
func fetchUpstreamModelInfo(modelName string) (*upstreamModelInfo, error) {
	// 默认使用中文语言获取元信息
	locale := "zh"
	base := common.GetEnvOrDefaultString("SYNC_UPSTREAM_BASE", "https://basellm.github.io/llm-metadata")
	modelsURL := fmt.Sprintf("%s/api/newapi/%s/models.json", base, locale)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(modelsURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch upstream models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var upstreamModels []upstreamModelInfo
	if err := json.Unmarshal(body, &upstreamModels); err != nil {
		return nil, fmt.Errorf("failed to unmarshal upstream models: %w", err)
	}

	// 查找匹配的模型
	for _, m := range upstreamModels {
		if m.ModelName == modelName {
			return &m, nil
		}
	}

	return nil, fmt.Errorf("model %s not found in upstream", modelName)
}
