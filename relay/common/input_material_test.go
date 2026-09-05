package common

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectInputMaterials(t *testing.T) {
	tests := []struct {
		name string
		req  TaskSubmitReq
		want []string // 期望的 "type:url" 序列(顺序无关,比较用集合)
	}{
		{
			name: "Images计图片",
			req:  TaskSubmitReq{Images: []string{"https://a/1.png", "https://a/2.png"}},
			want: []string{"image:https://a/1.png", "image:https://a/2.png"},
		},
		{
			name: "metadata content 数组豆包形态",
			req: TaskSubmitReq{Metadata: map[string]interface{}{
				"content": []interface{}{
					map[string]interface{}{"type": "video_url", "video_url": map[string]interface{}{"url": "https://v/a.mp4"}},
					map[string]interface{}{"type": "text", "text": "hi"},
					map[string]interface{}{"type": "audio_url", "audio_url": "https://a/b.mp3"},
				},
			}},
			want: []string{"video:https://v/a.mp4", "audio:https://a/b.mp3"},
		},
		{
			name: "content 条目 type 声明素材类型且地址在 url 键",
			req: TaskSubmitReq{Metadata: map[string]interface{}{
				"content": []interface{}{
					map[string]interface{}{"type": "video_url", "url": "https://v/typed.mp4"},
					map[string]interface{}{"type": "audio_url", "url": "https://a/typed.mp3"},
					map[string]interface{}{"type": "text", "text": "hi"},
				},
			}},
			want: []string{"video:https://v/typed.mp4", "audio:https://a/typed.mp3"},
		},
		{
			name: "同条目两种形态指向同一 URL 去重",
			req: TaskSubmitReq{Metadata: map[string]interface{}{
				"content": []interface{}{
					map[string]interface{}{"type": "video_url", "video_url": "https://v/dup.mp4", "url": "https://v/dup.mp4"},
				},
			}},
			want: []string{"video:https://v/dup.mp4"},
		},
		{
			name: "metadata 顶层 video_url/audio_url",
			req: TaskSubmitReq{Metadata: map[string]interface{}{
				"video_url": "https://v/top.mp4",
				"audio_url": "https://a/top.mp3",
			}},
			want: []string{"video:https://v/top.mp4", "audio:https://a/top.mp3"},
		},
		{
			name: "input_reference 计视频",
			req:  TaskSubmitReq{InputReference: "https://v/ref.mp4"},
			want: []string{"video:https://v/ref.mp4"},
		},
		{
			name: "data URI 内联视频",
			req: TaskSubmitReq{Metadata: map[string]interface{}{
				"video_url": "data:video/mp4;base64,AAAA",
			}},
			want: []string{"video:data:video/mp4;base64,AAAA"},
		},
		{
			name: "重复URL去重,空串跳过",
			req: TaskSubmitReq{
				Images:         []string{"https://a/1.png", "", "https://a/1.png"},
				InputReference: "https://v/ref.mp4",
				Metadata: map[string]interface{}{
					"video_url": "https://v/ref.mp4",
				},
			},
			want: []string{"image:https://a/1.png", "video:https://v/ref.mp4"},
		},
		{
			name: "无素材",
			req:  TaskSubmitReq{Prompt: "cat"},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectInputMaterials(tt.req)
			gotSet := make(map[string]bool, len(got))
			for _, m := range got {
				gotSet[m.MaterialType+":"+m.URL] = true
			}
			require.Len(t, got, len(tt.want))
			for _, w := range tt.want {
				assert.True(t, gotSet[w], "missing %s", w)
			}
			for _, m := range got {
				assert.Empty(t, m.Seconds)
				assert.Empty(t, m.Source)
			}
		})
	}
}

func TestValidateInputMaterialCount(t *testing.T) {
	// 空字符串与重复 URL 不计入素材,必须用互不相同的非空 URL 才能触达上限边界。
	images := make([]string, MaxInputMaterialCount)
	for i := range images {
		images[i] = fmt.Sprintf("https://a/%d.png", i)
	}
	req := TaskSubmitReq{Images: images}
	require.Nil(t, ValidateInputMaterialCount(req))

	images = append(images, "https://a/overflow.png")
	taskErr := ValidateInputMaterialCount(TaskSubmitReq{Images: images})
	require.NotNil(t, taskErr)
	assert.Equal(t, "input material count 101 exceeds the limit 100", taskErr.Message)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
}

// TestValidateInputMaterialCountTypedShape 数量守卫必须覆盖 type 声明形态的
// content 条目,否则该形态可绕过素材数上限。
func TestValidateInputMaterialCountTypedShape(t *testing.T) {
	content := make([]interface{}, MaxInputMaterialCount)
	for i := range content {
		content[i] = map[string]interface{}{"type": "video_url", "url": fmt.Sprintf("https://v/%d.mp4", i)}
	}
	req := TaskSubmitReq{Metadata: map[string]interface{}{"content": content}}
	require.Nil(t, ValidateInputMaterialCount(req))

	content = append(content, map[string]interface{}{"type": "video_url", "url": "https://v/overflow.mp4"})
	taskErr := ValidateInputMaterialCount(TaskSubmitReq{Metadata: map[string]interface{}{"content": content}})
	require.NotNil(t, taskErr)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
}
