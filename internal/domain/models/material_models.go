package models

import (
	"etalon-server/internal/domain/common"
)

type Material struct {
	common.Base
	AuthorID   *uint          `json:"author_id,omitempty" gorm:"index"`
	AuthorName string         `json:"author_name" gorm:"type:text"`
	Subject    string         `json:"subject" gorm:"type:text;not null"`
	Content    string         `json:"content" gorm:"type:text;not null"`
	Links      []MaterialLink `json:"links,omitempty" gorm:"foreignKey:MaterialID;constraint:OnDelete:CASCADE"`
}

type MaterialLink struct {
	ID         uint   `json:"id" gorm:"primaryKey"`
	MaterialID string `json:"material_id" gorm:"type:text;not null;index:idx_material_links_entity,priority:3;index"`
	EntityType string `json:"entity_type" gorm:"type:varchar(32);not null;index:idx_material_links_entity,priority:1"`
	EntityID   string `json:"entity_id" gorm:"type:text;not null;index:idx_material_links_entity,priority:2"`
}
