package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"github.com/gin-gonic/gin"
)

// GetCanvasCatalogMetaAdmin 下发画布目录表单需要的元数据:画布客户端支持的
// 契约清单与 capability→contract 映射。此前前端硬编码了一份同样的映射
// (canvas-catalog-form-dialog.tsx 的 CONTRACT_BY_CAPABILITY),与后端
// constant/canvas_contract.go 双源漂移 —— 现在统一由这里下发。
func GetCanvasCatalogMetaAdmin(c *gin.Context) {
	common.ApiSuccess(c, gin.H{
		"supported_contracts":    constant.SupportedContracts(),
		"capability_to_contract": constant.CapabilityToContractMap(),
	})
}
