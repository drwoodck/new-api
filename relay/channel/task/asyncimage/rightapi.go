package asyncimage

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

const rightapiChannelName = "Right Code 异步生图"

// RightapiAdaptor talks to rightapi.ai's OpenAI-compatible image endpoint:
//
//	POST /v1/images/generations  (requires "async": true) → {task_id, status}
//	GET  /v1/tasks/{task_id}     → {status, progress} while running,
//	                                {data[].url} (no status) when finished
//
// The "finished payload has no status field" quirk is why completion cannot be
// derived from a status word alone.
type RightapiAdaptor struct {
	taskcommon.BaseBilling
	adaptorBase
}

func (a *RightapiAdaptor) Init(info *relaycommon.RelayInfo) {
	a.initBase(info)
}

func (a *RightapiAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if _, taskErr := parseTaskSubmitReq(c); taskErr != nil {
		return taskErr
	}
	body, _, err := readJSONBody(c)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	if taskErr := rejectMultipleImages(body); taskErr != nil {
		return taskErr
	}
	info.Action = constant.TaskActionGenerate
	return nil
}

func (a *RightapiAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return a.baseURL + "/v1/images/generations", nil
}

func (a *RightapiAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return nil
}

func (a *RightapiAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	body, raw, err := readJSONBody(c)
	if err != nil {
		return nil, err
	}
	body["model"] = info.UpstreamModelName
	// This upstream refuses to return a task id unless async is explicitly set.
	body["async"] = true
	// Keep whatever reference fields the client used: rightapi documents
	// `image` for edits and does not normalise aliases, so a rewrite here would
	// silently drop references.
	encoded, err := common.Marshal(body)
	if err != nil {
		return bytes.NewReader(raw), nil
	}
	return bytes.NewReader(encoded), nil
}

func (a *RightapiAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

func (a *RightapiAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (string, []byte, *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
	}
	_ = resp.Body.Close()

	var payload taskPayload
	if err := common.Unmarshal(responseBody, &payload); err != nil {
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
	}
	upstreamTaskID := payload.upstreamTaskID()
	if upstreamTaskID == "" {
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("upstream returned no task id: %s", responseBody), "invalid_response", http.StatusInternalServerError)
	}

	writePublicSubmitResponse(c, info, info.PublicTaskID)
	return upstreamTaskID, responseBody, nil
}

func (a *RightapiAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok || strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("invalid task_id")
	}
	uri := fmt.Sprintf("%s/v1/tasks/%s", strings.TrimSuffix(baseUrl, "/"), taskID)
	return a.fetchTask(uri, key, proxy)
}

func (a *RightapiAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var payload taskPayload
	if err := common.Unmarshal(respBody, &payload); err != nil {
		return nil, fmt.Errorf("unmarshal task result failed: %w", err)
	}
	// requiredStatus=false: a finished rightapi payload has no status field at
	// all — presence of data[].url / candidates[].… is the completion signal.
	return buildTaskInfo(&payload, true, false)
}

func (a *RightapiAdaptor) GetModelList() []string {
	return RightapiModelList
}

func (a *RightapiAdaptor) GetChannelName() string {
	return rightapiChannelName
}

var _ channel.TaskAdaptor = (*RightapiAdaptor)(nil)
