package dto

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// 画布模型的参数(aspect_ratio / resolution / Midjourney 的 action 等)不在
// ImageRequest 的字段表里,会被 UnmarshalJSON 收进 Extra。默认序列化必须把它们
// 丢掉(官方 OpenAI 收到不认识的参数会 400),只有调用方显式打开 PassthroughExtra
// 才回传给上游。
func TestImageRequestExtraPassthrough(t *testing.T) {
	raw := []byte(`{
		"model": "lec-ac-image-2",
		"prompt": "a cat",
		"aspect_ratio": "9:16",
		"resolution": "2K",
		"action": "imagine",
		"n": 1
	}`)

	var req ImageRequest
	require.NoError(t, json.Unmarshal(raw, &req))
	require.Equal(t, "lec-ac-image-2", req.Model)
	require.JSONEq(t, `"9:16"`, string(req.Extra["aspect_ratio"]))
	require.JSONEq(t, `"2K"`, string(req.Extra["resolution"]))
	require.JSONEq(t, `"imagine"`, string(req.Extra["action"]))
	require.NotContains(t, req.Extra, "model", "已声明字段不能落进 Extra")
	require.NotContains(t, req.Extra, "prompt", "已声明字段不能落进 Extra")

	// 默认:未声明字段照旧丢弃(官方渠道口径不变)
	plain, err := json.Marshal(req)
	require.NoError(t, err)
	var plainMap map[string]any
	require.NoError(t, json.Unmarshal(plain, &plainMap))
	require.NotContains(t, plainMap, "aspect_ratio")
	require.NotContains(t, plainMap, "resolution")
	require.NotContains(t, plainMap, "action")
	require.Equal(t, "lec-ac-image-2", plainMap["model"])
	require.Equal(t, "a cat", plainMap["prompt"])
	require.EqualValues(t, 1, plainMap["n"])

	// 打开透传:未声明字段原样带回,已声明字段不受影响
	req.PassthroughExtra = true
	mergedJSON, err := json.Marshal(req)
	require.NoError(t, err)
	var merged map[string]any
	require.NoError(t, json.Unmarshal(mergedJSON, &merged))
	require.Equal(t, "9:16", merged["aspect_ratio"])
	require.Equal(t, "2K", merged["resolution"])
	require.Equal(t, "imagine", merged["action"])
	require.Equal(t, "lec-ac-image-2", merged["model"])
	require.EqualValues(t, 1, merged["n"])
	require.NotContains(t, merged, "PassthroughExtra", "控制位不能出现在 body 里")
	require.NotContains(t, merged, "Extra", "Extra 必须被平铺,不能作为嵌套字段出现")
}

// 没有额外字段时,打开透传也不该改变 body —— 避免给上游塞一个空对象或多余 key。
func TestImageRequestPassthroughWithoutExtraIsUnchanged(t *testing.T) {
	req := ImageRequest{Model: "gpt-image-1", Prompt: "x", PassthroughExtra: true}
	plain := ImageRequest{Model: "gpt-image-1", Prompt: "x"}

	a, err := json.Marshal(req)
	require.NoError(t, err)
	b, err := json.Marshal(plain)
	require.NoError(t, err)
	require.JSONEq(t, string(b), string(a))
}
