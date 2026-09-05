package common

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"
)

// MaxInputMaterialCount 是单次请求输入素材总数上限。素材清单进计费快照与
// 计费求和,无上界的清单会放大结算成本;超限以 400 拒绝(与 MaxImageN 同风格)。
const MaxInputMaterialCount = 100

// DetectInputMaterials 从任务请求中收集输入素材清单(类型+地址)。
// 收集范围:
//   - 图片:Image/Images(ValidateBasicTaskRequest 已把 Image 合并进 Images);
//   - 视频/音频:metadata["content"] 数组条目里的 video_url/audio_url(豆包
//     shape 通用化,条目值可为 string 或 {url: string}),兼容顶层
//     metadata["video_url"]/["audio_url"],以及 input_reference(视频)。
//
// 只做收集,不做计价:时长/单价/来源由提交链路(service)填充。
// 同一素材去重(同类型同 URL 只计一次),上限校验由 ValidateInputMaterialCount 负责。
func DetectInputMaterials(req TaskSubmitReq) []types.ResolvedInputMaterial {
	var materials []types.ResolvedInputMaterial
	seen := make(map[string]bool)
	add := func(materialType, url string) {
		url = strings.TrimSpace(url)
		if url == "" {
			return
		}
		key := materialType + "\x00" + url
		if seen[key] {
			return
		}
		seen[key] = true
		materials = append(materials, types.ResolvedInputMaterial{MaterialType: materialType, URL: url})
	}

	for _, img := range req.Images {
		add(types.MaterialTypeImage, img)
	}
	add(types.MaterialTypeVideo, req.InputReference)

	if req.Metadata != nil {
		if contentRaw, ok := req.Metadata["content"]; ok {
			if contentSlice, ok := contentRaw.([]interface{}); ok {
				for _, item := range contentSlice {
					itemMap, ok := item.(map[string]interface{})
					if !ok {
						continue
					}
					if url := extractMaterialRef(itemMap["video_url"]); url != "" {
						add(types.MaterialTypeVideo, url)
					}
					if url := extractMaterialRef(itemMap["audio_url"]); url != "" {
						add(types.MaterialTypeAudio, url)
					}
				}
			}
		}
		if url := extractMaterialRef(req.Metadata["video_url"]); url != "" {
			add(types.MaterialTypeVideo, url)
		}
		if url := extractMaterialRef(req.Metadata["audio_url"]); url != "" {
			add(types.MaterialTypeAudio, url)
		}
	}
	return materials
}

// extractMaterialRef 兼容 string 与 {url: string} 两种素材引用形态。
func extractMaterialRef(raw interface{}) string {
	switch v := raw.(type) {
	case string:
		return v
	case map[string]interface{}:
		if url, ok := v["url"].(string); ok {
			return url
		}
	}
	return ""
}

// ValidateInputMaterialCount 超上限时返回 400 TaskError;nil 表示通过。
func ValidateInputMaterialCount(req TaskSubmitReq) *dto.TaskError {
	count := len(DetectInputMaterials(req))
	if count <= MaxInputMaterialCount {
		return nil
	}
	return createTaskError(
		fmt.Errorf("input material count %d exceeds the limit %d", count, MaxInputMaterialCount),
		"invalid_request", http.StatusBadRequest, true)
}
