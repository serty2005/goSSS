package seeder

import (
	"encoding/json"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/tickets"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type fileManifestSeedItem struct {
	UUID          string   `json:"uuid"`
	FileID        string   `json:"file_id"`
	StorageKey    string   `json:"storage_key"`
	OriginalName  string   `json:"original_name"`
	MimeType      string   `json:"mime_type"`
	Size          int64    `json:"size"`
	Checksum      string   `json:"checksum"`
	ExportPath    string   `json:"export_path"`
	TicketUUIDs   []string `json:"ticket_uuids"`
	CommentUUIDs  []string `json:"comment_uuids"`
	RelationTypes []string `json:"relation_types"`
	MissingSource bool     `json:"missing_source"`
}

func (s *Seeder) seedTicketFilesFromMock(tx *gorm.DB, seedRes *ticketSeedResult) error {
	if seedRes == nil || len(seedRes.ticketIDByExternal) == 0 {
		return nil
	}

	manifestPath := filepath.Join("tools", "seeder", "mock_data", "files_manifest.json")
	if _, err := os.Stat(manifestPath); err != nil {
		s.logger.Warn("Файл манифеста файлов не найден, пропускаем сидинг файлов", "file", manifestPath)
		return nil
	}

	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("не удалось прочитать манифест файлов %s: %w", manifestPath, err)
	}

	var manifest []fileManifestSeedItem
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return fmt.Errorf("не удалось распарсить манифест файлов %s: %w", manifestPath, err)
	}
	if len(manifest) == 0 {
		return nil
	}

	storageRoot := strings.TrimSpace(os.Getenv("TICKET_STORAGE_PATH"))
	if storageRoot == "" {
		storageRoot = filepath.Join("storage", "tickets")
	}
	if err := os.MkdirAll(storageRoot, 0o755); err != nil {
		return fmt.Errorf("не удалось создать директорию файлов тикетов %s: %w", storageRoot, err)
	}

	mockRoot := filepath.Join("tools", "seeder", "mock_data")
	now := time.Now()

	fileAssets := make([]tickets.FileAsset, 0, len(manifest))
	extLinks := make([]models.ExternalSystemLink, 0, len(manifest))
	fileLinks := make([]tickets.TicketFileLink, 0, len(manifest)*2)
	publicURLByFileUUID := make(map[string]string, len(manifest))
	createdLinkKeys := make(map[string]struct{})

	for _, item := range manifest {
		fileUUID := strings.TrimSpace(item.UUID)
		if fileUUID == "" {
			continue
		}

		fileID := normalizeSeedID(item.FileID)
		ticketID := resolvePrimaryTicketID(item, seedRes)
		if ticketID == "" {
			ticketID = "orphan"
		}

		ext := resolveFileExtension(item)
		storageKey := filepath.ToSlash(filepath.Join(ticketID, fileID+ext))
		if strings.TrimSpace(storageKey) == "" {
			continue
		}

		asset := tickets.FileAsset{
			ID:           fileID,
			StorageKey:   storageKey,
			OriginalName: fallbackOriginalName(item.OriginalName, fileUUID+ext),
			MimeType:     strings.TrimSpace(item.MimeType),
			Size:         item.Size,
			Checksum:     strings.TrimSpace(item.Checksum),
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		fileAssets = append(fileAssets, asset)

		extLinks = append(extLinks, models.ExternalSystemLink{
			InternalID:      fileID,
			SystemName:      "naumen",
			ServiceDeskUUID: fileUUID,
			EntityType:      "File",
			LastSyncedAt:    now,
		})

		publicURLByFileUUID[fileUUID] = "/api/static/tickets/" + storageKey

		if err := s.copySeedFileToStorage(mockRoot, storageRoot, item, storageKey, fileUUID, ext); err != nil {
			return err
		}

		relationTypes := normalizeRelationTypes(item.RelationTypes)
		ticketIDs := resolveTicketIDs(item.TicketUUIDs, seedRes.ticketIDByExternal)

		for _, relationType := range relationTypes {
			if relationType == tickets.RelationTypeInlineComment && len(item.CommentUUIDs) > 0 {
				for _, rawCommentUUID := range item.CommentUUIDs {
					commentUUID := strings.TrimSpace(rawCommentUUID)
					if commentUUID == "" {
						continue
					}
					commentTicketID := seedRes.commentTicketByExt[commentUUID]
					if commentTicketID == "" {
						commentTicketID = ticketID
					}
					if commentTicketID == "" || commentTicketID == "orphan" {
						continue
					}
					commentUUIDCopy := commentUUID
					if !appendFileLinkIfNew(
						&fileLinks,
						createdLinkKeys,
						commentTicketID,
						fileID,
						relationType,
						&commentUUIDCopy,
						now,
					) {
						continue
					}
				}
				continue
			}

			for _, ticketLinkID := range ticketIDs {
				if ticketLinkID == "" {
					continue
				}
				appendFileLinkIfNew(
					&fileLinks,
					createdLinkKeys,
					ticketLinkID,
					fileID,
					relationType,
					nil,
					now,
				)
			}
		}
	}

	if len(fileAssets) > 0 {
		if err := tx.CreateInBatches(fileAssets, 200).Error; err != nil {
			return fmt.Errorf("не удалось записать file_assets: %w", err)
		}
	}
	if len(extLinks) > 0 {
		if err := tx.CreateInBatches(extLinks, 200).Error; err != nil {
			return fmt.Errorf("не удалось записать external_system_links для файлов: %w", err)
		}
	}
	if len(fileLinks) > 0 {
		if err := tx.CreateInBatches(fileLinks, 500).Error; err != nil {
			return fmt.Errorf("не удалось записать ticket_file_links: %w", err)
		}
	}

	if err := rewriteSeededFileLinksInTexts(tx, publicURLByFileUUID); err != nil {
		return err
	}

	s.logger.Info("Сидинг файлов тикетов завершен", "files", len(fileAssets), "links", len(fileLinks))
	return nil
}

func normalizeSeedID(raw string) string {
	id := strings.TrimSpace(raw)
	if id == "" {
		return uuid.New().String()
	}
	if _, err := uuid.Parse(id); err != nil {
		return uuid.New().String()
	}
	return id
}

func resolvePrimaryTicketID(item fileManifestSeedItem, seedRes *ticketSeedResult) string {
	for _, ticketUUID := range item.TicketUUIDs {
		if ticketID := seedRes.ticketIDByExternal[strings.TrimSpace(ticketUUID)]; ticketID != "" {
			return ticketID
		}
	}
	for _, commentUUID := range item.CommentUUIDs {
		if ticketID := seedRes.commentTicketByExt[strings.TrimSpace(commentUUID)]; ticketID != "" {
			return ticketID
		}
	}
	return ""
}

func resolveFileExtension(item fileManifestSeedItem) string {
	candidates := []string{item.OriginalName, item.ExportPath, item.StorageKey}
	for _, candidate := range candidates {
		ext := strings.ToLower(filepath.Ext(strings.TrimSpace(candidate)))
		if ext != "" {
			return ext
		}
	}
	return ".bin"
}

func fallbackOriginalName(raw string, fallback string) string {
	value := strings.TrimSpace(raw)
	if value != "" {
		return value
	}
	return fallback
}

func normalizeRelationTypes(relationTypes []string) []string {
	if len(relationTypes) == 0 {
		return []string{tickets.RelationTypeDirectTicketAttachment}
	}
	set := make(map[string]struct{}, len(relationTypes))
	for _, rt := range relationTypes {
		rt = strings.TrimSpace(rt)
		if rt != "" {
			set[rt] = struct{}{}
		}
	}
	if len(set) == 0 {
		return []string{tickets.RelationTypeDirectTicketAttachment}
	}
	result := make([]string, 0, len(set))
	for rt := range set {
		result = append(result, rt)
	}
	sort.Strings(result)
	return result
}

func resolveTicketIDs(ticketUUIDs []string, ticketIDByExternal map[string]string) []string {
	set := make(map[string]struct{}, len(ticketUUIDs))
	for _, rawUUID := range ticketUUIDs {
		ticketID := ticketIDByExternal[strings.TrimSpace(rawUUID)]
		if ticketID != "" {
			set[ticketID] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for ticketID := range set {
		out = append(out, ticketID)
	}
	sort.Strings(out)
	return out
}

func appendFileLinkIfNew(
	links *[]tickets.TicketFileLink,
	created map[string]struct{},
	ticketID string,
	fileID string,
	relationType string,
	commentUUID *string,
	now time.Time,
) bool {
	commentKey := ""
	if commentUUID != nil {
		commentKey = strings.TrimSpace(*commentUUID)
	}
	key := ticketID + "|" + fileID + "|" + relationType + "|" + commentKey
	if _, ok := created[key]; ok {
		return false
	}
	created[key] = struct{}{}

	link := tickets.TicketFileLink{
		ID:           uuid.New().String(),
		TicketID:     ticketID,
		FileID:       fileID,
		RelationType: relationType,
		CommentUUID:  commentUUID,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	*links = append(*links, link)
	return true
}

func (s *Seeder) copySeedFileToStorage(
	mockRoot string,
	storageRoot string,
	item fileManifestSeedItem,
	storageKey string,
	fileUUID string,
	ext string,
) error {
	if item.MissingSource {
		s.logger.Warn("Пропуск копирования файла: источник отсутствует в экспорте", "file_uuid", fileUUID)
		return nil
	}

	sourceRelPath := strings.TrimSpace(item.ExportPath)
	if sourceRelPath == "" {
		sourceRelPath = filepath.ToSlash(filepath.Join("files", fileUUID+ext))
	}
	sourcePath := filepath.Join(mockRoot, filepath.FromSlash(sourceRelPath))
	targetPath := filepath.Join(storageRoot, filepath.FromSlash(storageKey))

	if err := copyFileWithDirs(sourcePath, targetPath); err != nil {
		if os.IsNotExist(err) {
			s.logger.Warn("Файл из мока не найден, пропускаем копирование", "file_uuid", fileUUID, "path", sourcePath)
			return nil
		}
		return fmt.Errorf("не удалось скопировать файл %s в %s: %w", sourcePath, targetPath, err)
	}
	return nil
}

func copyFileWithDirs(sourcePath string, targetPath string) error {
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}

	targetFile, err := os.Create(targetPath)
	if err != nil {
		return err
	}
	defer targetFile.Close()

	_, err = io.Copy(targetFile, sourceFile)
	return err
}

func rewriteSeededFileLinksInTexts(tx *gorm.DB, publicURLByFileUUID map[string]string) error {
	if len(publicURLByFileUUID) == 0 {
		return nil
	}

	for fileUUID, publicURL := range publicURLByFileUUID {
		if strings.TrimSpace(fileUUID) == "" || strings.TrimSpace(publicURL) == "" {
			continue
		}

		variants := []string{
			"./download?uuid=" + fileUUID,
			"/download?uuid=" + fileUUID,
			"download?uuid=" + fileUUID,
		}

		for _, oldLink := range variants {
			if err := tx.Model(&tickets.Ticket{}).
				Where("description LIKE ? OR result LIKE ?", "%"+oldLink+"%", "%"+oldLink+"%").
				Updates(map[string]any{
					"description": gorm.Expr("REPLACE(description, ?, ?)", oldLink, publicURL),
					"result":      gorm.Expr("REPLACE(result, ?, ?)", oldLink, publicURL),
				}).Error; err != nil {
				return fmt.Errorf("не удалось переписать ссылки в тикетах: %w", err)
			}

			if err := tx.Model(&tickets.TicketComment{}).
				Where("text LIKE ?", "%"+oldLink+"%").
				Update("text", gorm.Expr("REPLACE(text, ?, ?)", oldLink, publicURL)).Error; err != nil {
				return fmt.Errorf("не удалось переписать ссылки в комментариях: %w", err)
			}
		}
	}

	return nil
}
