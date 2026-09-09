package model

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"gorm.io/gorm"
)

type CanvasCatalogModel struct {
	Id           int    `json:"id"`
	RemoteID     string `json:"remote_id" gorm:"size:128;not null;index"`
	DisplayName  string `json:"display_name" gorm:"size:256;not null"`
	Capabilities string `json:"capabilities" gorm:"size:256;not null"`
	// Enabled 必须是指针。它带 default:true,而 GORM 在 Create 时会把 bool 零值
	// 当成「未设置」交给数据库默认值 —— 实测三种写法(裸 Create、Select("*")、
	// 点名 Select)生成的 SQL 都是 `enabled` VALUES (true),即 enabled=false
	// 根本插不进去。管理员传 `"enabled": false`(先建好、暂不上架)会建出一个
	// 已上架的条目,而条目一上架客户端立刻能看到并下单。
	// 指针区分得开三态:nil = 未提供(取默认 true)、&false、&true。
	// 读取时一律走 IsEnabled(),不要直接解引用。
	Enabled        *bool          `json:"enabled" gorm:"default:true"`
	Description    string         `json:"description,omitempty" gorm:"type:text"`
	Pricing        string         `json:"pricing,omitempty" gorm:"size:128"`
	Limitations    string         `json:"limitations,omitempty" gorm:"type:text"`
	Contract       string         `json:"contract" gorm:"size:64;not null"`
	ParamSchema    string         `json:"param_schema,omitempty" gorm:"type:text"`
	SchemaOverride string         `json:"schema_override,omitempty" gorm:"type:text"`
	RequiresVocab  int            `json:"requires_vocab" gorm:"default:1"`
	SortOrder      int            `json:"sort_order" gorm:"default:0"`
	CreatedTime    int64          `json:"created_time" gorm:"bigint"`
	UpdatedTime    int64          `json:"updated_time" gorm:"bigint"`
	DeletedAt      gorm.DeletedAt `json:"-" gorm:"index"`
}

// IsEnabled 读取启用状态。Enabled 是指针(见字段注释),nil 表示调用方未提供,
// 按 default:true 的语义视为启用。所有判断都该走这里,不要直接解引用。
func (c *CanvasCatalogModel) IsEnabled() bool {
	return c.Enabled == nil || *c.Enabled
}

// IsCanvasReady 判定这条目录条目是不是画布客户端真正能用的("已配置完成"页
// vs "未配置"页的分流依据)——三项都是画布客户端不会把这一条整条跳过的最低
// 要求(见 sync/catalog.rs 的 apply_remote_catalog:未知 contract → 整条跳过;
// display_name 空 → 下拉里没名字可选):
//  1. contract 非空且在画布支持清单内;
//  2. display_name 非空。
//
// 刻意不看价格:契约可用性与"配没配价"是两件独立的事。没配价的模型仍算
// ready(仍在"已配置"页,只是分组价格列显示缺失),否则改个价会让模型在
// 两页之间跳来跳去,读起来莫名其妙。也不看 Enabled(软下线状态)——
// 一个被运营方停用的条目依然是"已配置完成"过的,停用是它自己的独立状态,
// 不是"从未配置"。
func IsCanvasReady(m *CanvasCatalogModel) bool {
	if m == nil {
		return false
	}
	if strings.TrimSpace(m.DisplayName) == "" {
		return false
	}
	return constant.IsSupportedContract(m.Contract)
}

func (c *CanvasCatalogModel) Insert() error {
	now := common.GetTimestamp()
	c.CreatedTime = now
	c.UpdatedTime = now
	return DB.Create(c).Error
}

// Update 更新目录条目。显式 Select 白名单可写字段,规避 GORM"结构体形式 Updates
// 跳过零值字段"的坑——否则 Enabled=false / 空字符串等零值永远无法写回数据库,
// 且顺带保护 Id/CreatedTime/DeletedAt 不被意外清零。
//
// description 不在白名单里:说明真源已统一到 models 表(2026-09-04 spec 3.7),
// 目录侧对存量文字只读——编辑表单不再携带它,任何 update 都不得覆盖。
func (c *CanvasCatalogModel) Update() error {
	c.UpdatedTime = common.GetTimestamp()
	return DB.Model(&CanvasCatalogModel{}).Where("id = ?", c.Id).
		Select(
			"remote_id", "display_name", "capabilities", "enabled",
			"pricing", "limitations", "contract",
			"param_schema", "schema_override", "requires_vocab",
			"sort_order", "updated_time",
		).
		Updates(c).Error
}

func DeleteCanvasCatalogModel(id int) error {
	return DB.Delete(&CanvasCatalogModel{}, id).Error
}

// GetCanvasCatalog 返回全部目录行(含禁用),按 SortOrder/DisplayName 排序。
//
// 本函数**不做分组过滤**,而且分组过滤不该放在这里。原先的 groupFilter 形参
// 从未被引用,读起来像未完成的脚手架,已删除 —— 留一个永不生效的参数比没有
// 参数更容易误导下一个人。
//
// 分组可见性在 controller 层以独立的 group_visible 字段附加到线上格式,
// 既不删行也不改写 Enabled。两条理由:
//  1. 删行会让客户端无法区分「已下线」与「已删除」(软下线契约,见下方注释);
//  2. 改写 Enabled 会连带清空用户的选择 —— 画布把它写进本地 models.enabled,
//     而那一列同时是用户自己的模型勾选开关,且同步时无条件覆写。
//     详见 controller/canvas_catalog.go 的 GroupVisible 字段注释。
func GetCanvasCatalog() ([]CanvasCatalogModel, int64, error) {
	var models []CanvasCatalogModel
	// 契约要求服务端不得过滤停用项:客户端靠"条目还在目录但 enabled=false"
	// 做软下线(保留行、置灰、不可新发起),过滤掉停用项会让客户端无法区分
	// "停用"与"已删除"。分组过滤同理不能删行,理由见上。
	err := DB.Order("sort_order ASC, display_name ASC").Find(&models).Error
	if err != nil {
		return nil, 0, err
	}
	// catalog_version = count of all rows (enabled + disabled) as monotonic proxy
	var totalCount int64
	if err := DB.Model(&CanvasCatalogModel{}).Count(&totalCount).Error; err != nil {
		return nil, 0, err
	}
	return models, totalCount, nil
}

// GetAllCanvasCatalogModelsAdmin 管理端用:返回全部条目(含禁用),不分页。
func GetAllCanvasCatalogModelsAdmin() ([]CanvasCatalogModel, error) {
	var models []CanvasCatalogModel
	err := DB.Order("sort_order ASC, display_name ASC").Find(&models).Error
	return models, err
}

// GetCanvasCatalogModelByID 按 ID 查单条,管理端编辑前加载用。
func GetCanvasCatalogModelByID(id int) (*CanvasCatalogModel, error) {
	var m CanvasCatalogModel
	if err := DB.First(&m, id).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

// GetCanvasCatalogModelByRemoteID 按 remote_id 查目录条目。未找到返回 (nil, nil) ——
// 与 GetModelGroupPrice 的"未配置不是 error"语义一致。工作台开闸时按
// remote_id=模型名 定位目录条目用。
func GetCanvasCatalogModelByRemoteID(remoteID string) (*CanvasCatalogModel, error) {
	var m CanvasCatalogModel
	err := DB.Where("remote_id = ?", remoteID).First(&m).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &m, nil
}

// IsCanvasCatalogRemoteIDDuplicated 检查 remote_id 是否与其它条目冲突(排除自身 ID)。
func IsCanvasCatalogRemoteIDDuplicated(id int, remoteID string) (bool, error) {
	if remoteID == "" {
		return false, nil
	}
	var cnt int64
	err := DB.Model(&CanvasCatalogModel{}).Where("remote_id = ? AND id <> ?", remoteID, id).Count(&cnt).Error
	return cnt > 0, err
}

// GetModelMetaDescriptionMap 按模型名批量取 models 表的说明(说明统一真源,
// 2026-09-04 spec 3.7)。只做精确名匹配:画布目录的 remote_id 就是计费用的
// 字面模型名(controller/canvas_catalog.go 已断言),不走 models.name_rule 的
// 前缀/包含规则 —— 目录条目与模型行是一对一的,模糊匹配反而会串行。
// 空说明不入 map,调用方据此回退目录存量文字。
func GetModelMetaDescriptionMap(modelNames []string) (map[string]string, error) {
	out := make(map[string]string, len(modelNames))
	if len(modelNames) == 0 {
		return out, nil
	}
	var rows []Model
	if err := DB.Select("model_name", "description").
		Where("model_name IN ?", modelNames).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		if r.Description != "" {
			out[r.ModelName] = r.Description
		}
	}
	return out, nil
}

// GetModelMetaDisplayNameMap 按模型名批量取 models 表的显示名称。
// 用于画布模型目录自动读取模型显示名称。
// 空显示名称不入 map,调用方据此回退 remote_id 作为显示名称。
func GetModelMetaDisplayNameMap(modelNames []string) (map[string]string, error) {
	out := make(map[string]string, len(modelNames))
	if len(modelNames) == 0 {
		return out, nil
	}
	var rows []Model
	if err := DB.Select("model_name", "display_name").
		Where("model_name IN ?", modelNames).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		if r.DisplayName != "" {
			out[r.ModelName] = r.DisplayName
		}
	}
	return out, nil
}
