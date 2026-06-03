package models

import (
	"etalon-server/internal/domain/common"
	"time"
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

const (
	ArticleTypeWiki        = "wiki"
	ArticleTypeReleaseNote = "release_note"
	ArticleTypeCompanyNews = "company_news"
	ArticleTypeIncident    = "incident_note"
	ArticleTypeInternalDoc = "internal_doc"
)

const (
	ArticleStatusDraft     = "draft"
	ArticleStatusPublished = "published"
	ArticleStatusArchived  = "archived"
)

const (
	ArticleContentMarkdown   = "markdown"
	ArticleContentTipTapJSON = "tiptap_json"
)

type Article struct {
	common.Base
	Slug          string        `json:"slug" gorm:"type:text;uniqueIndex"`
	Title         string        `json:"title" gorm:"type:text;not null;index"`
	Summary       string        `json:"summary" gorm:"type:text"`
	Content       string        `json:"content" gorm:"type:text;not null"`
	ContentFormat string        `json:"content_format" gorm:"type:varchar(32);not null;default:'markdown'"`
	Type          string        `json:"type" gorm:"type:varchar(32);not null;default:'wiki';index"`
	Status        string        `json:"status" gorm:"type:varchar(32);not null;default:'draft';index"`
	ProjectKey    string        `json:"project_key" gorm:"type:varchar(96);index"`
	Version       string        `json:"version" gorm:"type:varchar(96);index"`
	Tags          string        `json:"tags" gorm:"type:text"`
	IsPinned      bool          `json:"is_pinned" gorm:"not null;default:false;index"`
	PublishedAt   *time.Time    `json:"published_at,omitempty" gorm:"index"`
	AuthorID      *uint         `json:"author_id,omitempty" gorm:"index"`
	AuthorName    string        `json:"author_name" gorm:"type:text"`
	Links         []ArticleLink `json:"links,omitempty" gorm:"foreignKey:ArticleID;constraint:OnDelete:CASCADE"`
}

type ArticleLink struct {
	ID         uint   `json:"id" gorm:"primaryKey"`
	ArticleID  string `json:"article_id" gorm:"type:text;not null;index:idx_article_links_entity,priority:3;index"`
	EntityType string `json:"entity_type" gorm:"type:varchar(32);not null;index:idx_article_links_entity,priority:1"`
	EntityID   string `json:"entity_id" gorm:"type:text;not null;index:idx_article_links_entity,priority:2"`
}
