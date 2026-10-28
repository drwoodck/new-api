package relay

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildImageTaskResponse(t *testing.T) {
	t.Run("completed returns every url from the stored payload", func(t *testing.T) {
		task := &model.Task{
			TaskID:     "task_abc",
			Status:     model.TaskStatusSuccess,
			Progress:   "100%",
			Data:       json.RawMessage(`{"id":"img_1","status":"completed","result":{"data":[{"url":"https://a/1.png"},{"url":"https://a/2.png"}]}}`),
			Properties: model.Properties{OriginModelName: "TO-gpt-image-2"},
		}
		raw, err := buildImageTaskResponse(task)
		require.NoError(t, err)

		var payload imageTaskFetchResponse
		require.NoError(t, common.Unmarshal(raw, &payload))
		assert.Equal(t, "task_abc", payload.TaskID)
		assert.Equal(t, "task_abc", payload.ID)
		assert.Equal(t, "completed", payload.Status)
		assert.Equal(t, "TO-gpt-image-2", payload.Model)
		assert.Equal(t, 100, payload.Progress)
		require.Len(t, payload.Data, 2)
		assert.Equal(t, "https://a/1.png", payload.Data[0].URL)
		assert.Nil(t, payload.Error)
	})

	t.Run("in progress carries no data", func(t *testing.T) {
		task := &model.Task{TaskID: "task_run", Status: model.TaskStatusInProgress, Progress: "30%"}
		raw, err := buildImageTaskResponse(task)
		require.NoError(t, err)

		var payload imageTaskFetchResponse
		require.NoError(t, common.Unmarshal(raw, &payload))
		assert.Equal(t, "in_progress", payload.Status)
		assert.Equal(t, 30, payload.Progress)
		assert.Empty(t, payload.Data)
	})

	t.Run("failed exposes the reason", func(t *testing.T) {
		task := &model.Task{TaskID: "task_bad", Status: model.TaskStatusFailure, FailReason: "upstream 422"}
		raw, err := buildImageTaskResponse(task)
		require.NoError(t, err)

		var payload imageTaskFetchResponse
		require.NoError(t, common.Unmarshal(raw, &payload))
		assert.Equal(t, "failed", payload.Status)
		require.NotNil(t, payload.Error)
		assert.Equal(t, "upstream 422", payload.Error.Message)
	})

	t.Run("queued maps to queued", func(t *testing.T) {
		task := &model.Task{TaskID: "task_q", Status: model.TaskStatusQueued}
		raw, err := buildImageTaskResponse(task)
		require.NoError(t, err)

		var payload imageTaskFetchResponse
		require.NoError(t, common.Unmarshal(raw, &payload))
		assert.Equal(t, "queued", payload.Status)
	})

	t.Run("completed without payload urls falls back to the settled result url", func(t *testing.T) {
		task := &model.Task{
			TaskID:      "task_fb",
			Status:      model.TaskStatusSuccess,
			PrivateData: model.TaskPrivateData{ResultURL: "https://a/fallback.png"},
		}
		raw, err := buildImageTaskResponse(task)
		require.NoError(t, err)

		var payload imageTaskFetchResponse
		require.NoError(t, common.Unmarshal(raw, &payload))
		require.Len(t, payload.Data, 1)
		assert.Equal(t, "https://a/fallback.png", payload.Data[0].URL)
	})
}

func TestGetTaskPlatformForAsyncImage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("image submit dispatches by channel setting", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations?async=true", nil)
		c.Set("relay_mode", relayconstant.RelayModeImageSubmit)
		c.Set("channel_type", int(constant.ChannelTypeOpenAI))
		common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{AsyncImageProvider: "toapis"})

		assert.Equal(t, constant.TaskPlatformToapisImage, GetTaskPlatform(c))
	})

	t.Run("rightapi maps to its own platform", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations?async=true", nil)
		c.Set("relay_mode", relayconstant.RelayModeImageSubmit)
		common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{AsyncImageProvider: "rightapi"})

		assert.Equal(t, constant.TaskPlatformRightapiImage, GetTaskPlatform(c))
	})

	t.Run("per-model override selects the midjourney platform", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations?async=true", nil)
		c.Set("relay_mode", relayconstant.RelayModeImageSubmit)
		c.Set("original_model", "TO-midjourney")
		common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{
			AsyncImageProvider:       "toapis",
			AsyncImageProviderModels: map[string]string{"TO-midjourney": "toapis_mj"},
		})

		assert.Equal(t, constant.TaskPlatformToapisMJImage, GetTaskPlatform(c))
	})

	t.Run("without a provider the channel type still decides", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations?async=true", nil)
		c.Set("relay_mode", relayconstant.RelayModeImageSubmit)
		c.Set("channel_type", int(constant.ChannelTypeOpenAI))

		assert.Equal(t, constant.TaskPlatform("1"), GetTaskPlatform(c))
	})

	t.Run("a plain video request is not hijacked by the setting", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
		c.Set("relay_mode", relayconstant.RelayModeVideoSubmit)
		c.Set("channel_type", int(constant.ChannelTypeOpenAI))
		common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{AsyncImageProvider: "toapis"})

		assert.Equal(t, constant.TaskPlatform("1"), GetTaskPlatform(c))
	})
}

func TestGetTaskAdaptorForAsyncImagePlatforms(t *testing.T) {
	assert.NotNil(t, GetTaskAdaptor(constant.TaskPlatformToapisImage))
	assert.NotNil(t, GetTaskAdaptor(constant.TaskPlatformToapisMJImage))
	assert.NotNil(t, GetTaskAdaptor(constant.TaskPlatformRightapiImage))
}
