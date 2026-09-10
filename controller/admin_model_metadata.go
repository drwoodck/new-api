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

// UpdateModelMetadata 创建或更新模型元数据
func UpdateModelMetadata(c *gin.Context) {
	var req struct {
		ModelName   string  `json:"model_name" binding:"required"`
		ParamSchema *string `json:"param_schema"`
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

	meta := &model.ModelMetadata{
		ModelName:   req.ModelName,
		ParamSchema: req.ParamSchema,
	}
	if err := model.UpsertModelMetadata(meta); err != nil {
		c.JSON(200, gin.H{"success": false, "message": "保存失败: " + err.Error()})
		return
	}

	c.JSON(200, gin.H{"success": true, "message": "保存成功"})
}
