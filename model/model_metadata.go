package model

import (
	"fmt"
	"time"

	"gorm.io/gorm/clause"
)

// ModelMetadata 存储模型的前端配置元数据(param_schema)。
//
// model_name 必须等于 canvas_catalog_models.remote_id —— 目录端点正是拿
// remote_id 去这张表里查 schema 的(见 controller.toWireModel),对不上就查不到,
// 表现为「画布节点面板始终是模板默认参数」而没有任何报错。
type ModelMetadata struct {
	ModelName   string  `gorm:"primaryKey;type:varchar(255)" json:"model_name"`
	ParamSchema *string `gorm:"type:text" json:"param_schema,omitempty"`
	// MediaConfig 是模型的**参考素材形态**配置,形状对应画布 profile 的
	// request_shape.media(如 {"wrap":"url_array","field":"images"})。
	//
	// 与 param_schema 同表同源:两者都是「这个模型对外长什么样」的一部分,
	// 一起随目录下发。它决定画布把参考图/视频/音频**放进请求的哪个字段、
	// 用什么形态** —— 各上游供应商这一格差异极大(数组 / 拼接串 / multipart /
	// 按类型分桶),而 kungai 的 Sora 渠道是透传的,所以只能由这里配准。
	//
	// 空 = 用契约模板的默认形态(向后兼容,老数据不受影响)。
	MediaConfig    *string   `gorm:"type:text" json:"media_config,omitempty"`
	EndpointConfig *string   `gorm:"type:text" json:"endpoint_config,omitempty"`
	CreatedAt      time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (ModelMetadata) TableName() string {
	return "model_metadata"
}

// GetModelMetadataMap 批量查询元数据,返回 model_name → 行的映射。
//
// 返回 error 而不是吞掉:调用方(目录端点)需要区分「这批模型都没配 schema」
// 与「查询失败了」。注意调用方对两者都按 fail-open 处理(不降级目录),
// 这里的 error 只用于记日志。
func GetModelMetadataMap(modelNames []string) (map[string]*ModelMetadata, error) {
	if len(modelNames) == 0 {
		return make(map[string]*ModelMetadata), nil
	}

	var rows []ModelMetadata
	if err := DB.Where("model_name IN ?", modelNames).Find(&rows).Error; err != nil {
		return nil, err
	}

	result := make(map[string]*ModelMetadata, len(rows))
	for i := range rows {
		result[rows[i].ModelName] = &rows[i]
	}
	return result, nil
}

// GetModelMetadata 查询单个模型元数据(Web 后台编辑页用)。
func GetModelMetadata(modelName string) (*ModelMetadata, error) {
	var meta ModelMetadata
	if err := DB.Where("model_name = ?", modelName).First(&meta).Error; err != nil {
		return nil, err
	}
	return &meta, nil
}

// UpsertModelMetadata 按主键插入或更新。
//
// 显式指定 OnConflict 的冲突列与更新列,不用 GORM 的 Save —— Save 会连
// created_at 一起覆盖,而且「主键存在则 update」的语义在不同驱动上并不一致。
// 这里只更新内容列,created_at 保持首次写入的值。
func UpsertModelMetadata(metadata *ModelMetadata) error {
	return DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "model_name"}},
		// 每加一个内容列都要在这里登记 —— 漏了它,首次插入有值、之后每次保存
		// 都写不进去(OnConflict 只更新列出来的这些)。
		DoUpdates: clause.AssignmentColumns([]string{"param_schema", "media_config", "endpoint_config", "updated_at"}),
	}).Create(metadata).Error
}

// BatchUpsertModelMetadata 逐条 upsert。
//
// 刻意不用 GORM 的切片批量 Create:切片批量插入在冲突时无法给出「哪一条失败」,
// 而导入脚本最需要的恰恰是这个(model_name 写错时会静默变成新行而不是报错)。
// 调用量是几十条量级,逐条的开销可以忽略。
func BatchUpsertModelMetadata(metadatas []*ModelMetadata) error {
	for _, m := range metadatas {
		if err := UpsertModelMetadata(m); err != nil {
			return fmt.Errorf("upsert %s 失败: %w", m.ModelName, err)
		}
	}
	return nil
}
