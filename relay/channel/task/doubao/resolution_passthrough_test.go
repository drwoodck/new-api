package doubao

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

// TestDoubaoParseTaskResultCarriesActualResolution 豆包完成响应里的实际分辨率
// 必须归一化为档位键透传到 TaskInfo.Resolution（档位计费按实际档结算的依据）。
func TestDoubaoParseTaskResultCarriesActualResolution(t *testing.T) {
	adaptor := &TaskAdaptor{}
	body := []byte(`{"id":"t1","status":"succeeded","resolution":"1080p","duration":10,
		"content":{"video_url":"https://example.com/v.mp4"},"usage":{"completion_tokens":1,"total_tokens":2}}`)

	result, err := adaptor.ParseTaskResult(body)

	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, result.Status)
	require.Equal(t, 10, result.DurationSeconds)
	require.Equal(t, "1080p", result.Resolution)
}

// TestDoubaoParseTaskResultMissingResolutionLeavesEmpty 上游未返回分辨率时保持空。
func TestDoubaoParseTaskResultMissingResolutionLeavesEmpty(t *testing.T) {
	adaptor := &TaskAdaptor{}
	body := []byte(`{"id":"t1","status":"succeeded","duration":10,
		"content":{"video_url":"https://example.com/v.mp4"},"usage":{"completion_tokens":1,"total_tokens":2}}`)

	result, err := adaptor.ParseTaskResult(body)

	require.NoError(t, err)
	require.Empty(t, result.Resolution)
}
