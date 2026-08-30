package controller

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// 契约数量上限。端点已认证,但认证用户仍可发任意载荷 ——
// JSON 列无长度约束,一个塞满契约名的请求会拖慢后续每次支持率统计。
// 设计文档里的契约总数是个位数,64 给足余量。
const maxReportedContracts = 64

// install_id 的列宽是 varchar(64) —— 超长要明确拒绝,
// 而不是让 DB 静默截断(MySQL 非严格模式会截断,导致两个装机撞成一行)
const maxInstallIDLen = 64

type contractReportRequest struct {
	InstallID     string   `json:"install_id"`
	ClientVersion string   `json:"client_version"`
	Contracts     []string `json:"contracts"`
	// wire 字段名以画布已提交代码为准(src-tauri/src/relay/contracts.rs
	// report_supported_contracts): supported_vocab,不是 schema_vocab。
	SupportedVocab int `json:"supported_vocab"`
}

func (r *contractReportRequest) validate() error {
	if r.InstallID == "" {
		return errors.New("install_id 不能为空")
	}
	if len(r.InstallID) > maxInstallIDLen {
		return fmt.Errorf("install_id 超过 %d 字符", maxInstallIDLen)
	}
	if r.ClientVersion == "" {
		return errors.New("client_version 不能为空")
	}
	if len(r.Contracts) > maxReportedContracts {
		return fmt.Errorf("契约数量超过上限 %d", maxReportedContracts)
	}
	return nil
}

// ReportClientContracts 接收画布上报的契约支持声明。
//
// 挂在既有的 canvasRoute 组下,复用 TokenAuthReadOnly ——
// 上报是低风险写入(只写自己这一装机的行),不需要完整的 TokenAuth。
func ReportClientContracts(c *gin.Context) {
	var req contractReportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "请求格式错误: "+err.Error())
		return
	}
	if err := req.validate(); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}

	userId := c.GetInt("id")

	report := &model.ClientContractReport{
		InstallID:     req.InstallID,
		ClientVersion: req.ClientVersion,
		SchemaVocab:   req.SupportedVocab,
		UserId:        userId,
	}
	if err := report.SetContracts(req.Contracts); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := report.Upsert(); err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, nil)
}
