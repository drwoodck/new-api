package dto

import (
	"encoding/json"
)

type TaskError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Data       any    `json:"data"`
	StatusCode int    `json:"-"`
	LocalError bool   `json:"-"`
	Error      error  `json:"-"`
}

type TaskData interface {
	SunoDataResponse | []SunoDataResponse | string | any
}

const TaskSuccessCode = "success"

type TaskResponse[T TaskData] struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Data    T      `json:"data"`
}

func (t *TaskResponse[T]) IsSuccess() bool {
	return t.Code == TaskSuccessCode
}

type TaskDto struct {
	ID         int64           `json:"id"`
	CreatedAt  int64           `json:"created_at"`
	UpdatedAt  int64           `json:"updated_at"`
	TaskID     string          `json:"task_id"`
	Platform   string          `json:"platform"`
	UserId     int             `json:"user_id"`
	Group      string          `json:"group"`
	ChannelId  int             `json:"channel_id"`
	Quota      int             `json:"quota"`
	Action     string          `json:"action"`
	Status     string          `json:"status"`
	FailReason string          `json:"fail_reason"`
	ResultURL  string          `json:"result_url,omitempty"` // 任务结果 URL（视频地址等）
	SubmitTime int64           `json:"submit_time"`
	StartTime  int64           `json:"start_time"`
	FinishTime int64           `json:"finish_time"`
	Progress   string          `json:"progress"`
	Properties any             `json:"properties"`
	Username   string          `json:"username,omitempty"`
	Data       json.RawMessage `json:"data"`
	// ModelName 发起时请求的模型名,后台按模型筛选用(存的是 tasks.model_name 列)
	ModelName string `json:"model_name,omitempty"`
	// PrivateData 计费上下文与上游任务 ID 等排查信息,投影见 TaskPrivateDataDto
	PrivateData *TaskPrivateDataDto `json:"private_data,omitempty"`
}

// TaskPrivateDataDto 是 model.TaskPrivateData 的对外投影。
//
// 刻意逐字段列出、不直接复用 model 的结构体:model.TaskPrivateData.Key 存的是
// **上游渠道的 API key**(由 relayInfo.ChannelMeta.ApiKey 写入,源结构体注释原文
// 「禁止返回给用户,内部可能包含key等隐私信息」)。整块复制再靠 omitempty 挡是
// 挡不住的 —— 复制时漏一次就是长期泄露,而这里没列的字段从类型上就出不去。
//
// 另外 dto 是底层包,反向 import 重量级的 model 会把整条依赖链拖进来,这也是
// 不复用源类型的理由之一。
type TaskPrivateDataDto struct {
	UpstreamTaskID string `json:"upstream_task_id,omitempty"`
	ResultURL      string `json:"result_url,omitempty"`
	BillingSource  string `json:"billing_source,omitempty"`
	TokenId        int    `json:"token_id,omitempty"`
	NodeName       string `json:"node_name,omitempty"`
	// BillingContext 提交时的计费参数快照(单价/倍率/档位/素材费),当前全是
	// 可公开的数字,用 any 原样透出供对账。**若将来往那个结构体里加密钥类字段,
	// 必须改成投影类型** —— any 会连带把它发出去。
	BillingContext any    `json:"billing_context,omitempty"`
	ArtifactPath   string `json:"artifact_path,omitempty"`
	ArtifactNode   string `json:"artifact_node,omitempty"`
}

type FetchReq struct {
	IDs []string `json:"ids"`
}
