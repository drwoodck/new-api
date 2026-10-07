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
	contracts := constant.SupportedContracts()
	mapping := constant.CapabilityToContractMap()

	common.SysLog("canvas_catalog_meta: supported_contracts=" + common.GetJsonString(contracts))
	common.SysLog("canvas_catalog_meta: capability_to_contract=" + common.GetJsonString(mapping))

	common.ApiSuccess(c, gin.H{
		"supported_contracts":    contracts,
		"capability_to_contract": mapping,
	})
}
