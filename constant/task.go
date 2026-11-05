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

	// TaskPlatformSyncImage 是「同步上游、但被包装成任务」的图片平台。
	//
	// 用于出图动辄 2–5 分钟的同步渠道：请求本身还是发同一套 /v1/images/generations，
	// 由服务端在后台跑完再把结果写回任务行 —— 客户端拿到 task_id 即返回，
	// 不会撞上 Cloudflare 100 秒源站超时(524)。
	TaskPlatformSyncImage TaskPlatform = "sync_image"
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

	// AsyncImageProviderSyncTask 表示「上游其实是同步接口，但请求要按任务跑」。
	//
	// 这是给慢同步渠道用的（出图 2–5 分钟，同步转发必然被 Cloudflare 100 秒掐成
	// 524）：命中时服务端立刻返回 task_id，后台把同步调用跑完再落结果。
	// 它刻意不做成默认行为：快渠道（3–10 秒出图）走任务反而多一次轮询延迟，
	// 所以只对在 channels.settings 里显式配置了它的渠道/模型生效。
	AsyncImageProviderSyncTask = "sync_task"
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
	case AsyncImageProviderSyncTask:
		return TaskPlatformSyncImage, true
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
