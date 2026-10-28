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

const (
	toapisChannelName = "ToAPIs 异步生图"
	toapisMJName      = "ToAPIs Midjourney"
)

// ToapisAdaptor talks to ToAPIs' unified async image API:
//
//	POST /v1/images/generations            → {id, object:"generation.task", status}
//	GET  /v1/images/generations/{id}       → {status, result.data[].url}
//
// The upstream never answers synchronously, so submit always returns a task id.
type ToapisAdaptor struct {
	taskcommon.BaseBilling
	adaptorBase
}

func (a *ToapisAdaptor) Init(info *relaycommon.RelayInfo) {
	a.initBase(info)
}

func (a *ToapisAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
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

func (a *ToapisAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return a.baseURL + "/v1/images/generations", nil
}

func (a *ToapisAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return nil
}

func (a *ToapisAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	body, raw, err := readJSONBody(c)
	if err != nil {
		return nil, err
	}
	body["model"] = info.UpstreamModelName
	// ToAPIs is async by nature; the flag only exists because the canvas sends
	// it for OpenAI-compatible relays. Drop it so the upstream never has to
	// interpret a parameter it does not document.
	delete(body, "async")
	// ToAPIs takes reference images as reference_images (image_urls is the
	// legacy alias); normalise whatever the client used.
	if refs := referenceImages(body); len(refs) > 0 {
		body["reference_images"] = imageListToJSON(refs)
	}
	encoded, err := common.Marshal(body)
	if err != nil {
		// Fall back to the untouched body rather than failing the request.
		return bytes.NewReader(raw), nil
	}
	return bytes.NewReader(encoded), nil
}

func (a *ToapisAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

func (a *ToapisAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (string, []byte, *dto.TaskError) {
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

func (a *ToapisAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok || strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("invalid task_id")
	}
	uri := fmt.Sprintf("%s/v1/images/generations/%s", strings.TrimSuffix(baseUrl, "/"), taskID)
	return a.fetchTask(uri, key, proxy)
}

func (a *ToapisAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var payload taskPayload
	if err := common.Unmarshal(respBody, &payload); err != nil {
		return nil, fmt.Errorf("unmarshal task result failed: %w", err)
	}
	return buildTaskInfo(&payload, true, true)
}

func (a *ToapisAdaptor) GetModelList() []string {
	return ModelList
}

func (a *ToapisAdaptor) GetChannelName() string {
	return toapisChannelName
}

// ToapisMJAdaptor is the Midjourney sibling of ToapisAdaptor: same task
// lifecycle, different submit/query paths and a different result shape
// (grid_image_url / image_urls instead of result.data[].url).
//
// Only the default `imagine` action is supported here; the action family
// (upscale / variation / pan / zoom / modal …) needs an action-aware client
// contract and is tracked separately.
type ToapisMJAdaptor struct {
	taskcommon.BaseBilling
	adaptorBase
}

func (a *ToapisMJAdaptor) Init(info *relaycommon.RelayInfo) {
	a.initBase(info)
}

func (a *ToapisMJAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
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

func (a *ToapisMJAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return a.baseURL + "/v1/midjourney/generations", nil
}

func (a *ToapisMJAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return nil
}

func (a *ToapisMJAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	body, raw, err := readJSONBody(c)
	if err != nil {
		return nil, err
	}
	body["model"] = info.UpstreamModelName
	delete(body, "async")
	if refs := referenceImages(body); len(refs) > 0 {
		body["image_urls"] = imageListToJSON(refs)
	}
	encoded, err := common.Marshal(body)
	if err != nil {
		return bytes.NewReader(raw), nil
	}
	return bytes.NewReader(encoded), nil
}

func (a *ToapisMJAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

func (a *ToapisMJAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (string, []byte, *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
	}
	_ = resp.Body.Close()

	// The MJ submit endpoint answers with the plain task object as well.
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

func (a *ToapisMJAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok || strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("invalid task_id")
	}
	uri := fmt.Sprintf("%s/v1/midjourney/%s", strings.TrimSuffix(baseUrl, "/"), taskID)
	return a.fetchTask(uri, key, proxy)
}

func (a *ToapisMJAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var payload taskPayload
	if err := common.Unmarshal(respBody, &payload); err != nil {
		return nil, fmt.Errorf("unmarshal task result failed: %w", err)
	}
	return buildTaskInfo(&payload, true, true)
}

func (a *ToapisMJAdaptor) GetModelList() []string {
	return MJModelList
}

func (a *ToapisMJAdaptor) GetChannelName() string {
	return toapisMJName
}

var _ channel.TaskAdaptor = (*ToapisAdaptor)(nil)
var _ channel.TaskAdaptor = (*ToapisMJAdaptor)(nil)
