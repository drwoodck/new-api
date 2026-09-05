package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLogTaskConsumptionQuotaIncludesMaterialFee 钉住日志/用量统计口径：提交
// 日志的 quota 与用户/渠道用量必须与实扣（task.Quota）同口径，即生成费+素材
// 费；素材明细与素材额度单独保留在 other 里。
func TestLogTaskConsumptionQuotaIncludesMaterialFee(t *testing.T) {
	truncate(t)
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations", nil)
	c.Set("token_name", "tok")

	const userID, channelID = 60, 60
	seedUser(t, userID, 100000)
	seedChannel(t, channelID)

	info := &relaycommon.RelayInfo{
		UserId:          userID,
		OriginModelName: "material-log-model",
		UsingGroup:      "default",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: channelID},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{Action: constant.TaskActionGenerate},
	}
	info.PriceData = types.PriceData{
		Quota:          5000,
		GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
	}
	info.PriceData.MaterialQuota = 2000
	info.PriceData.Materials = []types.ResolvedInputMaterial{
		{MaterialType: types.MaterialTypeImage, URL: "https://x/1.png", UnitPrice: 0.004, Source: "explicit"},
	}

	LogTaskConsumption(c, info)

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, 7000, log.Quota) // 生成 5000 + 素材 2000
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Equal(t, 7000, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, int64(7000), getChannelUsedQuota(t, channelID))

	var other map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(log.Other), &other))
	assert.Equal(t, float64(2000), other["material_quota"])
	require.Contains(t, other, "input_materials")
	require.Len(t, other["input_materials"].([]interface{}), 1)
}

// TestSanitizeMaterialsForLogTruncatesDataURLs 钉住持久化/日志的 data URI
// 截断：http(s) URL 原样保留，data: URI 截断为前 128 字符并附注原始长度；
// 计费字段（时长/单价）不受影响，入参不被修改。
func TestSanitizeMaterialsForLogTruncatesDataURLs(t *testing.T) {
	rawURL := "data:video/mp4;base64," + strings.Repeat("A", 300)
	materials := []types.ResolvedInputMaterial{
		{MaterialType: types.MaterialTypeVideo, URL: rawURL, Seconds: 5, UnitPrice: 0.1, Source: "inline_measured"},
		{MaterialType: types.MaterialTypeImage, URL: "https://x/1.png", UnitPrice: 0.01, Source: "explicit"},
	}

	out := SanitizeMaterialsForLog(materials)

	require.Len(t, out, 2)
	assert.Equal(t, rawURL[:128]+"...(322 chars)", out[0].URL)
	assert.Equal(t, 5.0, out[0].Seconds, "计费字段不受截断影响")
	assert.Equal(t, 0.1, out[0].UnitPrice)
	assert.Equal(t, "https://x/1.png", out[1].URL, "http(s) URL 原样保留")

	// 入参不被修改
	assert.Len(t, materials[0].URL, 322)

	// 短 data URI 与空清单
	short := SanitizeMaterialsForLog([]types.ResolvedInputMaterial{
		{MaterialType: types.MaterialTypeVideo, URL: "data:video/mp4;base64,AAAA"},
	})
	assert.Equal(t, "data:video/mp4;base64,AAAA", short[0].URL)
	assert.Nil(t, SanitizeMaterialsForLog(nil))
}
