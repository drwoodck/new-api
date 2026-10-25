package openai

import (
	"encoding/json"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/require"
)

func TestIsOfficialOpenAIImageUpstream(t *testing.T) {
	cases := []struct {
		baseURL  string
		official bool
	}{
		{"", true},                                  // 没配 base_url = 官方默认地址
		{"https://api.openai.com", true},            //
		{"https://api.openai.com/v1", true},         //
		{"https://openai.com", true},                //
		{"https://x.openai.azure.com", true},        // Azure OpenAI
		{"https://api.lecapi.net", false},           // 自建/第三方网关
		{"https://api.paipu.net", false},            //
		{"https://ai.comfly.org", false},            //
		{"api.lecapi.net", false},                   // 没写 scheme 也按自建处理
		{"https://api.openai.com.evil.test", false}, // 后缀伪装必须识别为非官方
	}
	for _, c := range cases {
		require.Equal(t, c.official, isOfficialOpenAIImageUpstream(c.baseURL), "base_url=%q", c.baseURL)
	}
}

// 非官方上游:画布参数要原样带给上游;官方上游:保持丢掉(否则 OpenAI 直接 400)。
func TestConvertImageRequestPassesCanvasParamsOnlyForCustomUpstream(t *testing.T) {
	raw := []byte(`{"model":"lec-ac-image-2","prompt":"a cat","aspect_ratio":"9:16","resolution":"2K"}`)

	for _, tc := range []struct {
		name      string
		baseURL   string
		wantRatio bool
	}{
		{"自建网关中转站", "https://api.kungai.cc", true},
		{"上游 lecapi", "https://api.lecapi.net", true},
		{"官方 OpenAI", "https://api.openai.com", false},
		{"未配置 base_url", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var req dto.ImageRequest
			require.NoError(t, json.Unmarshal(raw, &req))

			info := &relaycommon.RelayInfo{
				RelayMode:   relayconstant.RelayModeImagesGenerations,
				ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: tc.baseURL},
			}
			converted, err := (&Adaptor{}).ConvertImageRequest(nil, info, req)
			require.NoError(t, err)

			body, err := json.Marshal(converted)
			require.NoError(t, err)
			var got map[string]any
			require.NoError(t, json.Unmarshal(body, &got))

			if tc.wantRatio {
				require.Equal(t, "9:16", got["aspect_ratio"], "画布画幅必须透传给上游")
				require.Equal(t, "2K", got["resolution"], "画布分辨率必须透传给上游")
			} else {
				require.NotContains(t, got, "aspect_ratio")
				require.NotContains(t, got, "resolution")
			}
			require.Equal(t, "lec-ac-image-2", got["model"])
			require.Equal(t, "a cat", got["prompt"])
		})
	}
}
