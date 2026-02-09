package db

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/tickets"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func ensureTicketFileIndexes(db *gorm.DB) error {
	queries := []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_external_links_system_external_unique
			ON external_system_links (system_name, service_desk_uuid)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_ticket_file_links_unique_relation
			ON ticket_file_links (ticket_id, file_id, relation_type, COALESCE(comment_uuid, ''))`,
	}
	for _, q := range queries {
		if err := db.Exec(q).Error; err != nil {
			return err
		}
	}
	return nil
}

func migrateLegacyAttachments(db *gorm.DB) error {
	if !db.Migrator().HasTable("attachments") {
		return nil
	}

	var count int64
	if err := db.Model(&tickets.Attachment{}).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return nil
	}

	var rows []tickets.Attachment
	if err := db.Find(&rows).Error; err != nil {
		return err
	}

	return db.Transaction(func(tx *gorm.DB) error {
		for _, legacy := range rows {
			storageKey := normalizeLegacyStorageKey(legacy.FilePath)
			if storageKey == "" {
				continue
			}

			absPath := filepath.Clean(filepath.Join("storage", "tickets", storageKey))
			checksum := ""
			if data, err := os.ReadFile(absPath); err == nil {
				sum := sha256.Sum256(data)
				checksum = hex.EncodeToString(sum[:])
			}

			fileAsset := tickets.FileAsset{
				StorageKey:   storageKey,
				OriginalName: coalesceString(legacy.FileName, filepath.Base(storageKey)),
				MimeType:     legacy.MimeType,
				Size:         legacy.Size,
				Checksum:     checksum,
			}

			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "storage_key"}},
				DoUpdates: clause.AssignmentColumns([]string{"original_name", "mime_type", "size", "checksum", "updated_at"}),
			}).Create(&fileAsset).Error; err != nil {
				return err
			}

			var persisted tickets.FileAsset
			if err := tx.Where("storage_key = ?", storageKey).First(&persisted).Error; err != nil {
				return err
			}

			commentUUID := (*string)(nil)
			relationType := tickets.RelationTypeDirectTicketAttachment
			if legacy.EntityType != "" && !strings.EqualFold(legacy.EntityType, "Ticket") {
				// Legacy-данные могут содержать нестандартные entity_type, но для восстановления
				// переносим их как прямые вложения тикета без потери файла.
				relationType = tickets.RelationTypeDirectTicketAttachment
			}

			link := tickets.TicketFileLink{
				TicketID:     legacy.EntityID,
				FileID:       persisted.ID,
				RelationType: relationType,
				CommentUUID:  commentUUID,
			}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&link).Error; err != nil {
				return err
			}

			if fileUUID, ok := extractLegacyNaumenFileUUID(legacy.FilePath); ok {
				extLink := models.ExternalSystemLink{
					InternalID:      persisted.ID,
					SystemName:      "naumen",
					ServiceDeskUUID: fileUUID,
					EntityType:      "File",
				}
				if err := tx.Clauses(clause.OnConflict{
					Columns:   []clause.Column{{Name: "system_name"}, {Name: "service_desk_uuid"}},
					DoUpdates: clause.AssignmentColumns([]string{"internal_id", "entity_type", "last_synced_at"}),
				}).Create(&extLink).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func normalizeLegacyStorageKey(path string) string {
	p := strings.TrimSpace(path)
	if p == "" {
		return ""
	}
	p = strings.ReplaceAll(p, "\\", "/")
	for _, prefix := range []string{"/api/static/tickets/", "/static/tickets/"} {
		if strings.HasPrefix(p, prefix) {
			return strings.TrimPrefix(p, prefix)
		}
	}
	p = strings.TrimPrefix(p, "./storage/tickets/")
	p = strings.TrimPrefix(p, "storage/tickets/")
	p = strings.TrimPrefix(p, "/storage/tickets/")
	return strings.TrimPrefix(p, "/")
}

func extractLegacyNaumenFileUUID(path string) (string, bool) {
	p := strings.ReplaceAll(strings.TrimSpace(path), "\\", "/")
	if p == "" {
		return "", false
	}
	base := filepath.Base(p)
	if strings.HasPrefix(base, "file$") {
		name := strings.TrimSuffix(base, filepath.Ext(base))
		if name != "" {
			return name, true
		}
	}
	return "", false
}

func coalesceString(v, fallback string) string {
	if strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}

func validateStorageKey(storageKey string) error {
	if strings.Contains(storageKey, "..") {
		return fmt.Errorf("некорректный storage_key: %s", storageKey)
	}
	return nil
}
