package controller

import (
	"encoding/json"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// GetModelMetadataList 获取所有模型元数据（列表页）
func GetModelMetadataList(c *gin.Context) {
	var metadatas []model.ModelMetadata
	err := model.DB.Order("model_name ASC").Find(&metadatas).Error
	if err != nil {
		c.JSON(200, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "data": metadatas})
}

// GetModelMetadataDetail 获取单个模型元数据（编辑页）
func GetModelMetadataDetail(c *gin.Context) {
	modelName := c.Query("model_name")
	if modelName == "" {
		c.JSON(200, gin.H{"success": false, "message": "model_name 参数缺失"})
		return
	}

	meta, err := model.GetModelMetadata(modelName)
	if err != nil {
		c.JSON(200, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "data": meta})
}

// UpdateModelMetadata 创建或更新模型元数据。
//
// **部分更新语义**:请求里没带的字段保持原值,只覆盖传了的。
// 这不是可选的设计偏好 —— 本表有多个并列的内容列(param_schema / media_config),
// 而前端是按字段分别保存的。若每次都拿一个只填了当前字段的结构体去 upsert,
// 其余列会被写成 NULL(OnConflict 只更新列出来的那些,Create 给未赋值字段 NULL),
// 表现为「改 A 字段顺手清掉了 B 字段」。
func UpdateModelMetadata(c *gin.Context) {
	var req struct {
		ModelName   string  `json:"model_name" binding:"required"`
		ParamSchema *string `json:"param_schema"`
		MediaConfig *string `json:"media_config"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(200, gin.H{"success": false, "message": "参数错误: " + err.Error()})
		return
	}

	// 验证 JSON 格式
	if req.ParamSchema != nil && *req.ParamSchema != "" {
		var test interface{}
		if err := json.Unmarshal([]byte(*req.ParamSchema), &test); err != nil {
			c.JSON(200, gin.H{"success": false, "message": "param_schema 不是有效的 JSON: " + err.Error()})
			return
		}
	}
	if req.MediaConfig != nil && *req.MediaConfig != "" {
		var test interface{}
		if err := json.Unmarshal([]byte(*req.MediaConfig), &test); err != nil {
			c.JSON(200, gin.H{"success": false, "message": "media_config 不是有效的 JSON: " + err.Error()})
			return
		}
	}

	meta := &model.ModelMetadata{ModelName: req.ModelName}
	// 行不存在是正常的(首次创建),只有真读到了才带回原值
	if existing, err := model.GetModelMetadata(req.ModelName); err == nil {
		meta.ParamSchema = existing.ParamSchema
		meta.MediaConfig = existing.MediaConfig
	}
	if req.ParamSchema != nil {
		meta.ParamSchema = req.ParamSchema
	}
	if req.MediaConfig != nil {
		meta.MediaConfig = req.MediaConfig
	}

	if err := model.UpsertModelMetadata(meta); err != nil {
		c.JSON(200, gin.H{"success": false, "message": "保存失败: " + err.Error()})
		return
	}

	c.JSON(200, gin.H{"success": true, "message": "保存成功"})
}
