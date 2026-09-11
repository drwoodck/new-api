package common

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	hosttypes "github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// defaultTierResolutionByChannel 是渠道的默认输出档位：请求未显式携带分辨率时
// 用它选档。与 defaultVideoDurationByChannel 同理 —— 计费必须按上游实际会生成
// 的分辨率选档；分辨率取不到会触发档位表 Unavailable 硬报错（宁可拒绝也不要
// 按错误档位收费），管理员配档表时应把渠道默认档一并配进去。
// 没有固定默认输出的渠道不在此表（请求必须显式携带分辨率）。
var defaultTierResolutionByChannel = map[int]string{
	constant.ChannelTypeSora:     "720p",
	constant.ChannelTypeOpenAI:   "720p", // OpenAI 兼容第三方走 Sora 适配器，默认同 Sora
	constant.ChannelTypeGemini:   "720p", // Veo 计费基准分辨率
	constant.ChannelTypeVertexAi: "720p",
	constant.ChannelTypeVidu:     "1080p", // vidu 请求侧缺省 1080p
}

// BuildTaskTierInput 从任务请求里按渠道语义归一化出档位选择输入。
// 提交（预扣）热路径专用：分辨率/时长/模式全部取自请求本身或渠道默认，
// 不依赖任何上游返回。档位表没配的模型不调用本函数（调用方先查档表存在）。
func BuildTaskTierInput(c *gin.Context, info *RelayInfo) hosttypes.TierInput {
	req, err := GetTaskRequest(c)
	if err != nil {
		return hosttypes.TierInput{}
	}
	return BuildTierInputFromRequest(info.ChannelType, req, ResolveTaskVideoDuration(c, info.ChannelType))
}

// BuildTierInputFromRequest 是不依赖 gin context 的纯函数版本。
//
// 抽出来是为了让档位诊断(controller 侧)复用**完全相同**的渠道取值逻辑 ——
// 诊断必须和计费走同一条路径,否则"诊断说能命中、计费说不能"比没有诊断更糟。
// 除时长改为入参外,与 BuildTaskTierInput 逐行等价。
func BuildTierInputFromRequest(channelType int, req TaskSubmitReq, durationSeconds int) hosttypes.TierInput {
	input := hosttypes.TierInput{
		DurationSeconds: durationSeconds,
		Mode:            strings.ToLower(strings.TrimSpace(req.Mode)),
	}

	var resolution string
	switch channelType {
	case constant.ChannelTypeSora, constant.ChannelTypeOpenAI:
		// 优先用请求显式携带的 resolution（画布就是这么发的），认不出再退回
		// 按 req.Size（"720x1280"）短边换算。
		//
		// 只认以 "p" 结尾的值：图片模型用的是同一个字段名，但发的是尺寸档
		// （"1K"/"2K"/"4K"），那属于 image_size 维度。以 "p" 结尾这个判据把
		// 两者干净分开，不会把图片的 4K 误当成视频分辨率。
		if r := resolutionFromDimensionString(req.Resolution); strings.HasSuffix(r, "p") {
			resolution = r
		} else {
			resolution = resolutionFromDimensionString(req.Size)
		}
	case constant.ChannelTypeDoubaoVideo, constant.ChannelTypeVolcEngine:
		// 豆包系（Seedance）请求侧分辨率在 metadata["resolution"]（"1080p"）。
		resolution = metadataResolution(req.Metadata)
	case constant.ChannelTypeGemini, constant.ChannelTypeVertexAi:
		// Veo：metadata["resolution"] > req.Size("WxH") > 渠道默认 720p。
		resolution = metadataResolution(req.Metadata)
		if resolution == "" {
			resolution = resolutionFromDimensionString(req.Size)
		}
	case constant.ChannelTypeAli:
		// 通义万相：req.Size 用 "*" 分隔（"832*480"）。
		resolution = resolutionFromDimensionString(req.Size)
	case constant.ChannelTypeKling:
		// Kling 的 size 只有画幅（aspect ratio）没有分辨率语义；档位表达走 mode。
		if mode := metadataMode(req.Metadata); mode != "" {
			input.Mode = mode
		}
	default:
		// jimeng/hailuo 等其余渠道：认 metadata，其次认 req.Size，最后渠道默认。
		resolution = metadataResolution(req.Metadata)
		if resolution == "" {
			resolution = resolutionFromDimensionString(req.Size)
		}
	}
	if resolution == "" {
		resolution = defaultTierResolutionByChannel[channelType]
	}
	input.Resolution = resolutionFromDimensionString(resolution)
	// 图像尺寸档（image_size tier）：来自 metadata["image_size"]，大写化与
	// NormalizeTierKey 一致。视频渠道一般不携带，留空即"该维度无输入"。
	input.ImageSize = normalizeImageSizeKey(metadataImageSize(req.Metadata))
	return input
}

// metadataImageSize 读取请求 metadata["image_size"] 字符串值。
func metadataImageSize(metadata map[string]interface{}) string {
	if metadata == nil {
		return ""
	}
	value, ok := metadata["image_size"]
	if !ok {
		return ""
	}
	s, ok := value.(string)
	if !ok {
		return ""
	}
	return s
}

// normalizeImageSizeKey 把图像尺寸值归一化为档位键("1k"→"1K")。
func normalizeImageSizeKey(raw string) string {
	return strings.ToUpper(strings.TrimSpace(raw))
}

// metadataResolution 读取请求 metadata["resolution"] 字符串值。
func metadataResolution(metadata map[string]interface{}) string {
	if metadata == nil {
		return ""
	}
	value, ok := metadata["resolution"]
	if !ok {
		return ""
	}
	s, ok := value.(string)
	if !ok {
		return ""
	}
	return s
}

// metadataMode 读取请求 metadata["mode"] 字符串值。
func metadataMode(metadata map[string]interface{}) string {
	if metadata == nil {
		return ""
	}
	value, ok := metadata["mode"]
	if !ok {
		return ""
	}
	s, ok := value.(string)
	if !ok {
		return ""
	}
	return s
}

// NormalizeTaskResolution 是 resolutionFromDimensionString 的导出版，
// 供各渠道完成侧（ParseTaskResult）把上游返回的实际分辨率归一化为档位键。
func NormalizeTaskResolution(raw string) string {
	return resolutionFromDimensionString(raw)
}

// resolutionFromDimensionString 把分辨率值归一化为档位键：
//   - "WxH"/"W*H"（分隔符 "x" 或 "*"，如 "720x1280"、"832*480"）按短边映射档位：
//     短边 ≥2000 → "4k"；≥1080 → "1080p"；≥720 → "720p"；≥480 → "480p"；
//   - 已是档位标签风格的值（"720p"/"1080p"/"4k"）小写透传；
//   - 无法解析时返回小写原样 —— 档表里没有该键即 Unavailable 硬报错，
//     不会把请求错配到别的档位。
func resolutionFromDimensionString(size string) string {
	size = strings.ToLower(strings.TrimSpace(size))
	if size == "" {
		return ""
	}
	if strings.HasSuffix(size, "p") || size == "4k" {
		return size
	}
	separator := "x"
	if strings.Contains(size, "*") {
		separator = "*"
	}
	parts := strings.SplitN(size, separator, 2)
	if len(parts) != 2 {
		return size
	}
	width, errW := strconv.Atoi(strings.TrimSpace(parts[0]))
	height, errH := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errW != nil || errH != nil || width <= 0 || height <= 0 {
		return size
	}
	shortEdge := width
	if height < shortEdge {
		shortEdge = height
	}
	switch {
	case shortEdge >= 2000:
		return "4k"
	case shortEdge >= 1080:
		return "1080p"
	case shortEdge >= 720:
		return "720p"
	case shortEdge >= 480:
		return "480p"
	default:
		return size
	}
}
