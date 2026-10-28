package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// The async image protocol is only usable if the polling endpoint is actually
// registered next to the submit endpoint. This asserts the wiring on the real
// route tree (a missing registration would 404 for every polling client) and,
// as a side effect, proves the new wildcard does not conflict with the existing
// /v1/images/generations POST route — gin panics at registration time on
// conflicts.
func TestAsyncImageRoutesRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetRelayRouter(engine)
	SetVideoRouter(engine)
	SetImageTaskRouter(engine)

	registered := make(map[string]bool, len(engine.Routes()))
	for _, route := range engine.Routes() {
		registered[route.Method+" "+route.Path] = true
	}

	assert.True(t, registered["POST /v1/images/generations"], "sync/async image submit route missing")
	assert.True(t, registered["GET /v1/images/:task_id"], "async image polling route missing")
	assert.True(t, registered["GET /v1/videos/:task_id"], "video polling route must stay registered")
}

// A synchronous request against an async-only channel must fail loudly instead
// of being forwarded to the sync handler, which would echo the upstream task
// object back to the user and still charge for it.
func TestRejectSyncImageOnAsyncChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("plain channel passes through", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)

		assert.False(t, rejectSyncImageOnAsyncChannel(c))
		assert.Equal(t, http.StatusOK, recorder.Code)
		assert.Empty(t, recorder.Body.String())
	})

	t.Run("async channel is rejected with a usable message", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
		common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{AsyncImageProvider: "toapis"})

		assert.True(t, rejectSyncImageOnAsyncChannel(c))
		assert.Equal(t, http.StatusBadRequest, recorder.Code)
		assert.Contains(t, recorder.Body.String(), "async_image_required")
		assert.Contains(t, recorder.Body.String(), "/v1/images/generations?async=true")
	})
}
