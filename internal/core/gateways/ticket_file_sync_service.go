package gateways

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"etalon-server/internal/domain/integration"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	naumenFileUUIDRegexFS   = regexp.MustCompile(`(?i)download\?uuid=(file\$[a-zA-Z0-9]+)`)
	scriptTagRegexFS        = regexp.MustCompile(`(?is)<script.*?>.*?</script>`)
	htmlEventDoubleQuotedFS = regexp.MustCompile(`(?i)\son\w+\s*=\s*"[^"]*"`)
	htmlEventSingleQuotedFS = regexp.MustCompile(`(?i)\son\w+\s*=\s*'[^']*'`)
	jsProtocolRegexFS       = regexp.MustCompile(`(?i)javascript:`)
)

type ticketProvider interface {
	GetFilesBySource(ctx context.Context, sourceUUID string) ([]integration.RemoteFile, error)
	GetFilesBySources(ctx context.Context, sourceUUIDs []string) (map[string][]integration.RemoteFile, error)
	DownloadFile(ctx context.Context, fileUUID string) ([]byte, string, error)
}

type ticketFileRepository interface {
	UpsertFileAsset(ctx context.Context, file *tickets.FileAsset) (*tickets.FileAsset, error)
	GetFileAssetByID(ctx context.Context, id string) (*tickets.FileAsset, error)
	UpsertTicketFileLink(ctx context.Context, link *tickets.TicketFileLink) error
	GetTicketFileLinksByRelation(ctx context.Context, ticketID string, relationTypes []string) ([]tickets.TicketFileLink, error)
}

type fileLinkRepository interface {
	GetByExternalID(ctx context.Context, tx *gorm.DB, systemName, externalID string) (*models.ExternalSystemLink, error)
	Upsert(ctx context.Context, tx *gorm.DB, link *models.ExternalSystemLink) error
}

type ticketFileSyncService struct {
	cfg        *config.Config
	logger     logger.LoggerInterface
	ticketRepo ticketFileRepository
	linkRepo   fileLinkRepository
}

func newTicketFileSyncService(
	cfg *config.Config,
	logger logger.LoggerInterface,
	ticketRepo ticketFileRepository,
	linkRepo fileLinkRepository,
) *ticketFileSyncService {
	return &ticketFileSyncService{
		cfg:        cfg,
		logger:     logger,
		ticketRepo: ticketRepo,
		linkRepo:   linkRepo,
	}
}

func (s *ticketFileSyncService) ProcessInlineContent(
	ctx context.Context,
	provider ticketProvider,
	ticketID string,
	ticketExternalUUID string,
	raw string,
	relationType string,
	commentUUID *string,
) string {
	if raw == "" {
		return raw
	}

	matches := naumenFileUUIDRegexFS.FindAllStringSubmatch(raw, -1)
	if len(matches) == 0 {
		return sanitizeHTMLFS(raw)
	}

	updated := raw
	seen := make(map[string]struct{})
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		fileUUID := strings.TrimSpace(m[1])
		if fileUUID == "" {
			continue
		}
		if _, ok := seen[fileUUID]; ok {
			continue
		}
		seen[fileUUID] = struct{}{}

		publicURL, asset, err := s.ensureFileAsset(ctx, provider, ticketID, fileUUID, "", "", 0)
		if err != nil {
			s.logger.Warn("Ошибка загрузки inline-файла", "ticket_external_uuid", ticketExternalUUID, "file_uuid", fileUUID, "error", err)
			continue
		}
		if asset != nil {
			link := &tickets.TicketFileLink{
				TicketID:     ticketID,
				FileID:       asset.ID,
				RelationType: relationType,
				CommentUUID:  commentUUID,
			}
			if err := s.ticketRepo.UpsertTicketFileLink(ctx, link); err != nil {
				s.logger.Warn("Ошибка upsert связи inline-файла", "ticket_id", ticketID, "file_id", asset.ID, "error", err)
			}
		}

		updated = strings.ReplaceAll(updated, "./download?uuid="+fileUUID, publicURL)
		updated = strings.ReplaceAll(updated, "/download?uuid="+fileUUID, publicURL)
		updated = strings.ReplaceAll(updated, "download?uuid="+fileUUID, publicURL)
	}

	return sanitizeHTMLFS(updated)
}

func (s *ticketFileSyncService) SyncDirectTicketFiles(
	ctx context.Context,
	provider ticketProvider,
	ticketID string,
	ticketExternalUUID string,
) error {
	remoteFiles, err := provider.GetFilesBySource(ctx, ticketExternalUUID)
	if err != nil {
		return err
	}
	return s.SyncDirectTicketFilesWithRemote(ctx, provider, ticketID, remoteFiles)
}

func (s *ticketFileSyncService) SyncDirectTicketFilesWithRemote(
	ctx context.Context,
	provider ticketProvider,
	ticketID string,
	remoteFiles []integration.RemoteFile,
) error {
	inlineLinks, err := s.ticketRepo.GetTicketFileLinksByRelation(ctx, ticketID, []string{
		tickets.RelationTypeInlineDescription,
		tickets.RelationTypeInlineComment,
		tickets.RelationTypeInlineResult,
	})
	if err != nil {
		return err
	}

	inlineFileIDs := make(map[string]struct{}, len(inlineLinks))
	for _, link := range inlineLinks {
		if strings.TrimSpace(link.FileID) != "" {
			inlineFileIDs[link.FileID] = struct{}{}
		}
	}

	for _, rf := range remoteFiles {
		fileUUID := strings.TrimSpace(rf.UUID)
		if fileUUID == "" {
			continue
		}

		externalLink, linkErr := s.linkRepo.GetByExternalID(ctx, nil, "naumen", fileUUID)
		if linkErr == nil && externalLink != nil {
			if _, skip := inlineFileIDs[externalLink.InternalID]; skip {
				continue
			}
		}

		_, asset, fileErr := s.ensureFileAsset(ctx, provider, ticketID, fileUUID, rf.Name, rf.MimeType, rf.Size)
		if fileErr != nil {
			s.logger.Warn("Ошибка загрузки вложения тикета", "ticket_id", ticketID, "file_uuid", fileUUID, "error", fileErr)
			continue
		}
		if asset == nil {
			continue
		}
		if _, skip := inlineFileIDs[asset.ID]; skip {
			continue
		}

		link := &tickets.TicketFileLink{
			TicketID:     ticketID,
			FileID:       asset.ID,
			RelationType: tickets.RelationTypeDirectTicketAttachment,
		}
		if err := s.ticketRepo.UpsertTicketFileLink(ctx, link); err != nil {
			s.logger.Warn("Ошибка upsert связи direct-вложения", "ticket_id", ticketID, "file_id", asset.ID, "error", err)
		}
	}

	return nil
}

func (s *ticketFileSyncService) ensureFileAsset(
	ctx context.Context,
	provider ticketProvider,
	ticketID string,
	fileUUID string,
	preferredName string,
	preferredMime string,
	remoteSize int64,
) (string, *tickets.FileAsset, error) {
	var existing *tickets.FileAsset
	link, err := s.linkRepo.GetByExternalID(ctx, nil, "naumen", fileUUID)
	if err != nil {
		return "", nil, err
	}
	if link != nil {
		existing, err = s.ticketRepo.GetFileAssetByID(ctx, link.InternalID)
		if err != nil {
			return "", nil, err
		}
	}

	if existing == nil {
		existing = &tickets.FileAsset{ID: uuid.New().String()}
	} else if strings.TrimSpace(existing.ID) == "" {
		existing.ID = uuid.New().String()
	}

	cleanName := sanitizeFileNameFS(preferredName)
	ext := strings.ToLower(filepath.Ext(cleanName))
	if ext == "" {
		ext = strings.ToLower(filepath.Ext(existing.OriginalName))
	}
	if ext == "" {
		ext = extensionByMimeFS(preferredMime)
	}
	if ext == "" {
		ext = ".bin"
	}

	if strings.TrimSpace(existing.StorageKey) == "" {
		existing.StorageKey = filepath.ToSlash(filepath.Join(ticketID, existing.ID+ext))
	}

	absPath := filepath.Join(s.cfg.TicketStoragePath, filepath.FromSlash(existing.StorageKey))
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		return "", nil, err
	}

	shouldDownload := s.shouldDownloadFile(existing, absPath, remoteSize)
	downloaded := false
	checksum := strings.TrimSpace(existing.Checksum)
	var fileBytes []byte
	var contentType string

	if shouldDownload {
		fileBytes, contentType, err = provider.DownloadFile(ctx, fileUUID)
		if err != nil {
			return "", nil, err
		}
		downloaded = true

		sum := sha256.Sum256(fileBytes)
		checksum = hex.EncodeToString(sum[:])

		needWrite := true
		if st, statErr := os.Stat(absPath); statErr == nil {
			existingChecksum := existing.Checksum
			if existingChecksum == "" {
				if b, readErr := os.ReadFile(absPath); readErr == nil {
					hash := sha256.Sum256(b)
					existingChecksum = hex.EncodeToString(hash[:])
				}
			}
			if existingChecksum != "" && existingChecksum == checksum {
				needWrite = false
			} else if st.Size() == int64(len(fileBytes)) {
				if b, readErr := os.ReadFile(absPath); readErr == nil {
					hash := sha256.Sum256(b)
					if hex.EncodeToString(hash[:]) == checksum {
						needWrite = false
					}
				}
			}
		}

		if needWrite {
			if err := os.WriteFile(absPath, fileBytes, 0644); err != nil {
				return "", nil, err
			}
		}
	} else {
		s.logger.Info("Пропуск загрузки файла: метаданные не изменились", "file_uuid", fileUUID, "storage_key", existing.StorageKey)
	}

	if cleanName == "" {
		if existing.OriginalName != "" {
			cleanName = existing.OriginalName
		} else {
			cleanName = sanitizeFileNameFS(fileUUID + ext)
		}
	}

	finalMime := strings.TrimSpace(preferredMime)
	if finalMime == "" {
		if downloaded {
			finalMime = strings.TrimSpace(contentType)
		} else {
			finalMime = strings.TrimSpace(existing.MimeType)
		}
	}
	if finalMime == "" {
		finalMime = mime.TypeByExtension(ext)
	}

	finalSize := remoteSize
	if finalSize <= 0 {
		if downloaded {
			finalSize = int64(len(fileBytes))
		} else if st, statErr := os.Stat(absPath); statErr == nil {
			finalSize = st.Size()
		} else {
			finalSize = existing.Size
		}
	}

	existing.OriginalName = cleanName
	existing.MimeType = finalMime
	existing.Size = finalSize
	existing.Checksum = checksum

	persisted, err := s.ticketRepo.UpsertFileAsset(ctx, existing)
	if err != nil {
		return "", nil, err
	}

	extLink := &models.ExternalSystemLink{
		InternalID:      persisted.ID,
		SystemName:      "naumen",
		ServiceDeskUUID: fileUUID,
		EntityType:      "File",
		LastSyncedAt:    time.Now(),
	}
	if err := s.linkRepo.Upsert(ctx, nil, extLink); err != nil {
		return "", nil, err
	}

	publicURL := fmt.Sprintf("/api/static/tickets/%s", filepath.ToSlash(persisted.StorageKey))
	return publicURL, persisted, nil
}

func (s *ticketFileSyncService) shouldDownloadFile(
	existing *tickets.FileAsset,
	absPath string,
	remoteSize int64,
) bool {
	if existing == nil || strings.TrimSpace(existing.StorageKey) == "" {
		return true
	}

	st, err := os.Stat(absPath)
	if err != nil || st.IsDir() {
		return true
	}

	if remoteSize > 0 {
		if existing.Size > 0 {
			return existing.Size != remoteSize
		}
		return st.Size() != remoteSize
	}
	return false
}

func sanitizeFileNameFS(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	name = filepath.Base(name)
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	return name
}

func extensionByMimeFS(mimeType string) string {
	if mimeType == "" {
		return ""
	}
	base := strings.TrimSpace(strings.Split(mimeType, ";")[0])
	exts, err := mime.ExtensionsByType(base)
	if err != nil || len(exts) == 0 {
		return ""
	}
	return exts[0]
}

func sanitizeHTMLFS(raw string) string {
	clean := scriptTagRegexFS.ReplaceAllString(raw, "")
	clean = htmlEventDoubleQuotedFS.ReplaceAllString(clean, "")
	clean = htmlEventSingleQuotedFS.ReplaceAllString(clean, "")
	clean = jsProtocolRegexFS.ReplaceAllString(clean, "")
	return clean
}
