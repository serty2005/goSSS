package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"etalon-server/internal/domain/tickets"
)

// ticketStaticPathPattern находит ссылки на локальные файлы тикетов в HTML/тексте.
var ticketStaticPathPattern = regexp.MustCompile(`/(?:api/)?static/tickets/([^"'\s<>?#)]+)`)

// NormalizeTicketUploadRelation приводит тип связи загружаемого файла к поддерживаемому значению.
func NormalizeTicketUploadRelation(value string) (string, bool) {
	switch strings.TrimSpace(value) {
	case "", "direct", tickets.RelationTypeDirectTicketAttachment:
		return tickets.RelationTypeDirectTicketAttachment, true
	case "comment", tickets.RelationTypeInlineComment:
		return tickets.RelationTypeInlineComment, true
	case "description", tickets.RelationTypeInlineDescription:
		return tickets.RelationTypeInlineDescription, true
	default:
		return "", false
	}
}

// extractTicketStorageKeys возвращает уникальные ключи хранилища файлов тикета, на которые ссылается текст.
func extractTicketStorageKeys(ticketID string, text string) []string {
	prefix := strings.TrimSpace(ticketID) + "/"
	if prefix == "/" || strings.TrimSpace(text) == "" {
		return nil
	}
	matches := ticketStaticPathPattern.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}
	result := make([]string, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		key := strings.TrimSpace(match[1])
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}
	return result
}

// bindCommentInlineFiles связывает загруженные inline-файлы с сохраненным комментарием.
func (s *ticketServiceImpl) bindCommentInlineFiles(ctx context.Context, ticketID string, commentUUID string, text string) {
	keys := extractTicketStorageKeys(ticketID, text)
	if len(keys) == 0 || strings.TrimSpace(commentUUID) == "" {
		return
	}
	fileIDs := make([]string, 0, len(keys))
	for _, key := range keys {
		asset, err := s.ticketRepo.GetFileAssetByStorageKey(ctx, key)
		if err != nil {
			s.logger.Warn("не удалось получить файл комментария", "ticket_id", ticketID, "storage_key", key, "error", err)
			continue
		}
		if asset != nil {
			fileIDs = append(fileIDs, asset.ID)
		}
	}
	if err := s.ticketRepo.BindPendingInlineFiles(ctx, ticketID, tickets.RelationTypeInlineComment, commentUUID, fileIDs); err != nil {
		s.logger.Warn("не удалось привязать файлы к комментарию", "ticket_id", ticketID, "comment_uuid", commentUUID, "error", err)
	}
}

// cleanupRemovedInlineFiles удаляет из хранилища inline-файлы, которые пропали из текста
// и больше нигде в тикете не используются. Прямые вложения тикета не затрагиваются.
func (s *ticketServiceImpl) cleanupRemovedInlineFiles(ctx context.Context, ticketID string, oldText string, newText string) {
	oldKeys := extractTicketStorageKeys(ticketID, oldText)
	if len(oldKeys) == 0 {
		return
	}
	kept := make(map[string]struct{})
	for _, key := range extractTicketStorageKeys(ticketID, newText) {
		kept[key] = struct{}{}
	}
	removed := make([]string, 0, len(oldKeys))
	for _, key := range oldKeys {
		if _, ok := kept[key]; !ok {
			removed = append(removed, key)
		}
	}
	if len(removed) == 0 {
		return
	}

	usedKeys, err := s.collectUsedTicketStorageKeys(ctx, ticketID)
	if err != nil {
		s.logger.Warn("не удалось проверить использование файлов тикета", "ticket_id", ticketID, "error", err)
		return
	}

	for _, key := range removed {
		if _, used := usedKeys[key]; used {
			continue
		}
		s.deleteInlineFile(ctx, ticketID, key)
	}
}

// collectUsedTicketStorageKeys собирает ключи файлов, на которые ссылаются описание, результат и комментарии тикета.
func (s *ticketServiceImpl) collectUsedTicketStorageKeys(ctx context.Context, ticketID string) (map[string]struct{}, error) {
	used := make(map[string]struct{})
	ticket, err := s.ticketRepo.GetByID(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	if ticket != nil {
		for _, key := range extractTicketStorageKeys(ticketID, ticket.Description) {
			used[key] = struct{}{}
		}
		for _, key := range extractTicketStorageKeys(ticketID, ticket.Result) {
			used[key] = struct{}{}
		}
	}
	comments, err := s.ticketRepo.GetComments(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	for _, comment := range comments {
		for _, key := range extractTicketStorageKeys(ticketID, comment.Text) {
			used[key] = struct{}{}
		}
	}
	return used, nil
}

func (s *ticketServiceImpl) deleteInlineFile(ctx context.Context, ticketID string, storageKey string) {
	asset, err := s.ticketRepo.GetFileAssetByStorageKey(ctx, storageKey)
	if err != nil {
		s.logger.Warn("не удалось получить файл тикета для удаления", "ticket_id", ticketID, "storage_key", storageKey, "error", err)
		return
	}
	if asset == nil {
		return
	}
	links, err := s.ticketRepo.GetTicketFileLinksByFileID(ctx, asset.ID)
	if err != nil {
		s.logger.Warn("не удалось получить связи файла тикета", "ticket_id", ticketID, "file_id", asset.ID, "error", err)
		return
	}
	if len(links) == 0 {
		return
	}
	for _, link := range links {
		if link.TicketID != ticketID {
			return
		}
		switch link.RelationType {
		case tickets.RelationTypeInlineComment, tickets.RelationTypeInlineDescription:
		default:
			return
		}
	}

	if err := s.ticketRepo.DeleteFileAssetWithLinks(ctx, asset.ID); err != nil {
		s.logger.Warn("не удалось удалить запись файла тикета", "ticket_id", ticketID, "file_id", asset.ID, "error", err)
		return
	}
	if s.cfg == nil || strings.TrimSpace(s.cfg.TicketStoragePath) == "" {
		return
	}
	absPath := filepath.Join(s.cfg.TicketStoragePath, filepath.FromSlash(asset.StorageKey))
	if err := os.Remove(absPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		s.logger.Warn("не удалось удалить файл тикета из хранилища", "ticket_id", ticketID, "path", absPath, "error", err)
	}
}
