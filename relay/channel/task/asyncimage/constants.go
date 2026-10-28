package asyncimage

// ModelList lists the ToAPIs async image models this adaptor is known to serve.
// It only feeds channel model suggestions; routing is driven by the channel's
// configured model list and model mapping.
var ModelList = []string{
	"gpt-image-2",
	"gpt-image-2-vip",
	"gpt-image-2-official",
	"gpt-image-2.5-flare",
	"gpt-image-2.5-sunburst",
	"gpt-image-2.5-flare-vip",
	"gpt-image-2.5-sunburst-vip",
	"gpt-image-2.5-flare-official",
	"gpt-image-2.5-sunburst-official",
	"doubao-seedream-5-0-pro",
	"gemini-nano-banana-2.1",
	"gemini-nano-banana-2.1-vip",
	"gemini-nano-banana-2.1-official",
}

// MJModelList lists the ToAPIs Midjourney model ids.
var MJModelList = []string{"midjourney"}

// RightapiModelList lists the rightapi.ai async image models.
var RightapiModelList = []string{
	"gpt-image-2",
	"gpt-image-2.5-flare",
	"gpt-image-2.5-sunburst",
}
