package sora

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSoraBuildRequestBodyReturnsReplayablePassThroughBody(t *testing.T) {
	payload := []byte("opaque-sora-request-body")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(payload))
	c.Request.Header.Set("Content-Type", "application/octet-stream")
	defer common.CleanupBodyStorage(c)

	info := &relaycommon.RelayInfo{}
	body, err := (&TaskAdaptor{}).BuildRequestBody(c, info)
	require.NoError(t, err)
	replayable, ok := body.(common.ReplayableBody)
	require.True(t, ok)

	sent, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.Equal(t, payload, sent)
	assert.EqualValues(t, len(payload), replayable.Size())

	replayBody, err := replayable.NewReader()
	require.NoError(t, err)
	replay, err := io.ReadAll(replayBody)
	require.NoError(t, err)
	require.NoError(t, replayBody.Close())
	assert.Equal(t, payload, replay)
}

// TestSoraParseTaskResultCarriesActualResolution sora 完成响应里的实际输出尺寸
// 必须归一化为档位键透传到 TaskInfo.Resolution（档位计费按实际档结算的依据）。
func TestSoraParseTaskResultCarriesActualResolution(t *testing.T) {
	adaptor := &TaskAdaptor{}
	body := []byte(`{"id":"t1","status":"completed","seconds":"10","size":"720x1280"}`)

	result, err := adaptor.ParseTaskResult(body)

	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, result.Status)
	require.Equal(t, 10, result.DurationSeconds)
	require.Equal(t, "720p", result.Resolution, "720x1280 必须归一化为 720p 档位键")
}

// TestSoraParseTaskResultMissingSizeLeavesResolutionEmpty 上游未返回尺寸时
// Resolution 保持空（结算回退请求档）。
func TestSoraParseTaskResultMissingSizeLeavesResolutionEmpty(t *testing.T) {
	adaptor := &TaskAdaptor{}
	body := []byte(`{"id":"t1","status":"completed","seconds":"10"}`)

	result, err := adaptor.ParseTaskResult(body)

	require.NoError(t, err)
	require.Empty(t, result.Resolution)
}
