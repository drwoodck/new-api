package model

import (
	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type CanvasCatalogModel struct {
	Id              int            `json:"id"`
	RemoteID        string         `json:"remote_id" gorm:"size:128;not null;index"`
	DisplayName     string         `json:"display_name" gorm:"size:256;not null"`
	Capabilities    string         `json:"capabilities" gorm:"size:256;not null"`
	Enabled         bool           `json:"enabled" gorm:"default:true"`
	Description     string         `json:"description,omitempty" gorm:"type:text"`
	Pricing         string         `json:"pricing,omitempty" gorm:"size:128"`
	Limitations     string         `json:"limitations,omitempty" gorm:"type:text"`
	Contract        string         `json:"contract" gorm:"size:64;not null"`
	ParamSchema     string         `json:"param_schema,omitempty" gorm:"type:text"`
	SchemaOverride  string         `json:"schema_override,omitempty" gorm:"type:text"`
	RequiresVocab   int            `json:"requires_vocab" gorm:"default:1"`
	SortOrder       int            `json:"sort_order" gorm:"default:0"`
	CreatedTime     int64          `json:"created_time" gorm:"bigint"`
	UpdatedTime     int64          `json:"updated_time" gorm:"bigint"`
	DeletedAt       gorm.DeletedAt `json:"-" gorm:"index"`
}

func (c *CanvasCatalogModel) Insert() error {
	now := common.GetTimestamp()
	c.CreatedTime = now
	c.UpdatedTime = now
	return DB.Create(c).Error
}

func (c *CanvasCatalogModel) Update() error {
	c.UpdatedTime = common.GetTimestamp()
	return DB.Model(&CanvasCatalogModel{}).Where("id = ?", c.Id).Updates(c).Error
}

func DeleteCanvasCatalogModel(id int) error {
	return DB.Delete(&CanvasCatalogModel{}, id).Error
}

func GetCanvasCatalog(groupFilter []string) ([]CanvasCatalogModel, int64, error) {
	var models []CanvasCatalogModel
	// Phase 1: ignore group filter, return all enabled models sorted by SortOrder
	err := DB.Where("enabled = ?", true).Order("sort_order ASC, display_name ASC").Find(&models).Error
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
