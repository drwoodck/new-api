package constant

type TaskPlatform string

const (
	TaskPlatformSuno       TaskPlatform = "suno"
	TaskPlatformMidjourney              = "mj"

	// 异步生图平台。这两个上游的图片接口一律先返回任务 ID，没有同步模式，
	// 所以渠道类型仍是 OpenAI（1）也没法走同步图片链路，必须走任务管线。
	TaskPlatformToapisImage   TaskPlatform = "toapis_image"
	TaskPlatformToapisMJImage TaskPlatform = "toapis_mj_image"
	TaskPlatformRightapiImage TaskPlatform = "rightapi_image"
)

// 异步生图渠道的上游实现标识，配置在 channels.settings 的 async_image_provider 字段。
//
// 不复用 channel_type 的原因：这两家都是 OpenAI 兼容网关，渠道里还可能有同一个
// base_url 的同步模型；按渠道一个字段选择上游实现，既不新增渠道类型、也不影响
// 既有的渠道分发与分组能力（abilities）。
const (
	AsyncImageProviderToapis   = "toapis"
	AsyncImageProviderToapisMJ = "toapis_mj"
	AsyncImageProviderRightapi = "rightapi"
)

// AsyncImageTaskPlatform 把渠道配置里的上游标识映射成任务平台。
// 空值或未知值返回 false —— 调用方据此判断该渠道不是异步生图渠道。
func AsyncImageTaskPlatform(provider string) (TaskPlatform, bool) {
	switch provider {
	case AsyncImageProviderToapis:
		return TaskPlatformToapisImage, true
	case AsyncImageProviderToapisMJ:
		return TaskPlatformToapisMJImage, true
	case AsyncImageProviderRightapi:
		return TaskPlatformRightapiImage, true
	}
	return "", false
}

const (
	SunoActionMusic  = "MUSIC"
	SunoActionLyrics = "LYRICS"

	TaskActionGenerate          = "generate"
	TaskActionTextGenerate      = "textGenerate"
	TaskActionFirstTailGenerate = "firstTailGenerate"
	TaskActionReferenceGenerate = "referenceGenerate"
	TaskActionRemix             = "remixGenerate"
)

var SunoModel2Action = map[string]string{
	"suno_music":  SunoActionMusic,
	"suno_lyrics": SunoActionLyrics,
}
