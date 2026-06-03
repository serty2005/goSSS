package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"etalon-server/internal/contextkeys"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/user"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/response"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"
)

type ArticleHandler struct {
	db       *gorm.DB
	userRepo user.Repository
}

func NewArticleHandler(db *gorm.DB, userRepo user.Repository) *ArticleHandler {
	return &ArticleHandler{db: db, userRepo: userRepo}
}

type articleLinkDTO struct {
	EntityType string `json:"entity_type"`
	EntityID   string `json:"entity_id"`
}

type articleDTO struct {
	ID            string           `json:"id"`
	Slug          string           `json:"slug"`
	Title         string           `json:"title"`
	Summary       string           `json:"summary"`
	Content       string           `json:"content"`
	ContentFormat string           `json:"content_format"`
	Type          string           `json:"type"`
	Status        string           `json:"status"`
	ProjectKey    string           `json:"project_key,omitempty"`
	Version       string           `json:"version,omitempty"`
	Tags          []string         `json:"tags"`
	IsPinned      bool             `json:"is_pinned"`
	PublishedAt   *time.Time       `json:"published_at,omitempty"`
	AuthorID      *uint            `json:"author_id,omitempty"`
	AuthorName    string           `json:"author_name"`
	Links         []articleLinkDTO `json:"links,omitempty"`
	CreatedAt     time.Time        `json:"created_at"`
	UpdatedAt     time.Time        `json:"updated_at"`
}

type articlePayload struct {
	Slug          string           `json:"slug"`
	Title         string           `json:"title"`
	Summary       string           `json:"summary"`
	Content       string           `json:"content"`
	ContentFormat string           `json:"content_format"`
	Type          string           `json:"type"`
	Status        string           `json:"status"`
	ProjectKey    string           `json:"project_key"`
	Version       string           `json:"version"`
	Tags          []string         `json:"tags"`
	IsPinned      bool             `json:"is_pinned"`
	Links         []articleLinkDTO `json:"links"`
}

var allowedArticleEntityTypes = map[string]string{
	"Company":        "companies",
	"Server":         "servers",
	"Workstation":    "workstations",
	"FiscalRegister": "fiscal_registers",
	"Ticket":         "tickets",
}

func (h *ArticleHandler) List(w http.ResponseWriter, r *http.Request) {
	filter := articleListFilter{
		Term:       strings.TrimSpace(r.URL.Query().Get("term")),
		Type:       strings.TrimSpace(r.URL.Query().Get("type")),
		Status:     strings.TrimSpace(r.URL.Query().Get("status")),
		Tag:        strings.TrimSpace(r.URL.Query().Get("tag")),
		ProjectKey: strings.TrimSpace(r.URL.Query().Get("project_key")),
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	if offset < 0 {
		offset = 0
	}

	tx := h.filteredArticlesQuery(r.Context(), filter, h.canManageArticles(r), h.currentUserID(r))
	var total int64
	if err := tx.Distinct("articles.id").Count(&total).Error; err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось получить количество публикаций")
		return
	}

	var items []models.Article
	if err := tx.Distinct("articles.*").
		Preload("Links").
		Order("articles.is_pinned DESC").
		Order("COALESCE(articles.published_at, articles.updated_at) DESC").
		Limit(limit).
		Offset(offset).
		Find(&items).Error; err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось получить публикации")
		return
	}

	result := make([]articleDTO, 0, len(items))
	for _, item := range items {
		result = append(result, toArticleDTO(item))
	}
	response.RespondWithJSON(w, http.StatusOK, api.PaginatedResponse{
		Data:    result,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
		HasNext: int64(offset+len(result)) < total,
		HasPrev: offset > 0,
	})
}

func (h *ArticleHandler) Featured(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 20 {
		limit = 6
	}

	var items []models.Article
	if err := h.db.WithContext(r.Context()).
		Model(&models.Article{}).
		Where("status = ?", models.ArticleStatusPublished).
		Preload("Links").
		Order("is_pinned DESC").
		Order("COALESCE(published_at, updated_at) DESC").
		Limit(limit).
		Find(&items).Error; err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось получить избранные публикации")
		return
	}

	result := make([]articleDTO, 0, len(items))
	for _, item := range items {
		result = append(result, toArticleDTO(item))
	}
	response.RespondWithJSON(w, http.StatusOK, result)
}

func (h *ArticleHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	var item models.Article
	if err := h.db.WithContext(r.Context()).
		Preload("Links").
		Where("id = ? OR slug = ?", id, id).
		First(&item).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.RespondWithError(w, http.StatusNotFound, "Публикация не найдена")
			return
		}
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось получить публикацию")
		return
	}
	if !h.canReadArticle(r, item) {
		response.RespondWithError(w, http.StatusForbidden, "Недостаточно прав для просмотра публикации")
		return
	}
	response.RespondWithJSON(w, http.StatusOK, toArticleDTO(item))
}

func (h *ArticleHandler) Create(w http.ResponseWriter, r *http.Request) {
	var payload articlePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректное тело запроса")
		return
	}
	normalized, err := h.normalizeArticlePayload(payload, true)
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}
	authorID, authorName := h.resolveAuthor(r.Context())
	now := time.Now()
	status := normalized.Status
	var publishedAt *time.Time
	if status == models.ArticleStatusPublished {
		publishedAt = &now
	}

	item := &models.Article{
		Slug:          normalized.Slug,
		Title:         normalized.Title,
		Summary:       normalized.Summary,
		Content:       normalized.Content,
		ContentFormat: normalized.ContentFormat,
		Type:          normalized.Type,
		Status:        status,
		ProjectKey:    normalized.ProjectKey,
		Version:       normalized.Version,
		Tags:          joinArticleTags(normalized.Tags),
		IsPinned:      normalized.IsPinned,
		PublishedAt:   publishedAt,
		AuthorID:      authorID,
		AuthorName:    authorName,
	}

	err = h.db.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(item).Error; err != nil {
			return err
		}
		if err := replaceArticleLinks(tx, item.ID, normalized.Links); err != nil {
			return err
		}
		return tx.Preload("Links").First(item, "id = ?", item.ID).Error
	})
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось создать публикацию")
		return
	}
	response.RespondWithJSON(w, http.StatusCreated, toArticleDTO(*item))
}

func (h *ArticleHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	var payload articlePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректное тело запроса")
		return
	}
	normalized, err := h.normalizeArticlePayload(payload, false)
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	var item models.Article
	err = h.db.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ?", id).First(&item).Error; err != nil {
			return err
		}
		if !h.canEditArticle(r, item) {
			return errArticleForbidden
		}

		oldStatus := item.Status
		item.Slug = normalized.Slug
		item.Title = normalized.Title
		item.Summary = normalized.Summary
		item.Content = normalized.Content
		item.ContentFormat = normalized.ContentFormat
		item.Type = normalized.Type
		item.Status = normalized.Status
		item.ProjectKey = normalized.ProjectKey
		item.Version = normalized.Version
		item.Tags = joinArticleTags(normalized.Tags)
		item.IsPinned = normalized.IsPinned
		if oldStatus != models.ArticleStatusPublished && item.Status == models.ArticleStatusPublished {
			now := time.Now()
			item.PublishedAt = &now
		}
		if err := tx.Save(&item).Error; err != nil {
			return err
		}
		if err := replaceArticleLinks(tx, item.ID, normalized.Links); err != nil {
			return err
		}
		return tx.Preload("Links").First(&item, "id = ?", item.ID).Error
	})
	if err != nil {
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			response.RespondWithError(w, http.StatusNotFound, "Публикация не найдена")
		case errors.Is(err, errArticleForbidden):
			response.RespondWithError(w, http.StatusForbidden, "Недостаточно прав для изменения публикации")
		default:
			response.RespondWithError(w, http.StatusInternalServerError, "Не удалось обновить публикацию")
		}
		return
	}
	response.RespondWithJSON(w, http.StatusOK, toArticleDTO(item))
}

func (h *ArticleHandler) Publish(w http.ResponseWriter, r *http.Request) {
	h.changeStatus(w, r, models.ArticleStatusPublished)
}

func (h *ArticleHandler) Archive(w http.ResponseWriter, r *http.Request) {
	h.changeStatus(w, r, models.ArticleStatusArchived)
}

func (h *ArticleHandler) Delete(w http.ResponseWriter, r *http.Request) {
	h.changeStatus(w, r, models.ArticleStatusArchived)
}

func (h *ArticleHandler) changeStatus(w http.ResponseWriter, r *http.Request, status string) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	var item models.Article
	err := h.db.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ?", id).First(&item).Error; err != nil {
			return err
		}
		if !h.canEditArticle(r, item) {
			return errArticleForbidden
		}
		item.Status = status
		if status == models.ArticleStatusPublished && item.PublishedAt == nil {
			now := time.Now()
			item.PublishedAt = &now
		}
		if err := tx.Save(&item).Error; err != nil {
			return err
		}
		return tx.Preload("Links").First(&item, "id = ?", item.ID).Error
	})
	if err != nil {
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			response.RespondWithError(w, http.StatusNotFound, "Публикация не найдена")
		case errors.Is(err, errArticleForbidden):
			response.RespondWithError(w, http.StatusForbidden, "Недостаточно прав для изменения публикации")
		default:
			response.RespondWithError(w, http.StatusInternalServerError, "Не удалось изменить статус публикации")
		}
		return
	}
	response.RespondWithJSON(w, http.StatusOK, toArticleDTO(item))
}

type articleListFilter struct {
	Term       string
	Type       string
	Status     string
	Tag        string
	ProjectKey string
}

func (h *ArticleHandler) filteredArticlesQuery(ctx context.Context, filter articleListFilter, canManage bool, currentUserID *uint) *gorm.DB {
	tx := h.db.WithContext(ctx).Model(&models.Article{})
	if canManage {
		if filter.Status != "" {
			tx = tx.Where("articles.status = ?", filter.Status)
		}
	} else if currentUserID != nil {
		tx = tx.Where("(articles.status = ? OR articles.author_id = ?)", models.ArticleStatusPublished, *currentUserID)
		if filter.Status != "" {
			tx = tx.Where("articles.status = ?", filter.Status)
		}
	} else {
		tx = tx.Where("articles.status = ?", models.ArticleStatusPublished)
	}
	if filter.Type != "" {
		tx = tx.Where("articles.type = ?", filter.Type)
	}
	if filter.ProjectKey != "" {
		tx = tx.Where("articles.project_key = ?", filter.ProjectKey)
	}
	if filter.Tag != "" {
		tx = tx.Where("articles.tags ILIKE ?", "%"+filter.Tag+"%")
	}
	if filter.Term != "" {
		pattern := "%" + filter.Term + "%"
		tx = tx.Where(
			h.db.Where("articles.title ILIKE ?", pattern).
				Or("articles.summary ILIKE ?", pattern).
				Or("articles.content ILIKE ?", pattern).
				Or("articles.tags ILIKE ?", pattern).
				Or("articles.project_key ILIKE ?", pattern).
				Or("articles.version ILIKE ?", pattern),
		)
	}
	return tx
}

func (h *ArticleHandler) normalizeArticlePayload(payload articlePayload, create bool) (articlePayload, error) {
	title := strings.TrimSpace(payload.Title)
	content := strings.TrimSpace(payload.Content)
	if title == "" {
		return articlePayload{}, fmt.Errorf("Заголовок публикации обязателен")
	}
	if content == "" {
		return articlePayload{}, fmt.Errorf("Содержимое публикации обязательно")
	}

	articleType := strings.TrimSpace(payload.Type)
	if articleType == "" {
		articleType = models.ArticleTypeWiki
	}
	if !isAllowedArticleType(articleType) {
		return articlePayload{}, fmt.Errorf("Неподдерживаемый тип публикации: %s", articleType)
	}
	status := strings.TrimSpace(payload.Status)
	if status == "" {
		status = models.ArticleStatusDraft
	}
	if !isAllowedArticleStatus(status) {
		return articlePayload{}, fmt.Errorf("Неподдерживаемый статус публикации: %s", status)
	}
	contentFormat := strings.TrimSpace(payload.ContentFormat)
	if contentFormat == "" {
		contentFormat = models.ArticleContentMarkdown
	}
	if contentFormat != models.ArticleContentMarkdown && contentFormat != models.ArticleContentTipTapJSON {
		return articlePayload{}, fmt.Errorf("Неподдерживаемый формат содержимого: %s", contentFormat)
	}
	projectKey := strings.TrimSpace(payload.ProjectKey)
	version := strings.TrimSpace(payload.Version)
	if articleType == models.ArticleTypeReleaseNote && (projectKey == "" || version == "") {
		return articlePayload{}, fmt.Errorf("Для release notes обязательны проект и версия")
	}

	links, err := h.normalizeArticleLinks(payload.Links)
	if err != nil {
		return articlePayload{}, err
	}
	slug := strings.TrimSpace(payload.Slug)
	if slug == "" {
		slug = slugifyArticleTitle(title)
	}
	if create {
		slug = h.uniqueArticleSlug(slug)
	}

	return articlePayload{
		Slug:          slug,
		Title:         title,
		Summary:       strings.TrimSpace(payload.Summary),
		Content:       content,
		ContentFormat: contentFormat,
		Type:          articleType,
		Status:        status,
		ProjectKey:    projectKey,
		Version:       version,
		Tags:          normalizeArticleTags(payload.Tags),
		IsPinned:      payload.IsPinned,
		Links:         links,
	}, nil
}

func (h *ArticleHandler) normalizeArticleLinks(links []articleLinkDTO) ([]articleLinkDTO, error) {
	unique := make(map[string]articleLinkDTO)
	for _, link := range links {
		entityType, err := normalizeArticleEntityType(link.EntityType)
		if err != nil {
			return nil, err
		}
		entityID := strings.TrimSpace(link.EntityID)
		if entityID == "" {
			return nil, fmt.Errorf("entity_id обязателен")
		}
		exists, err := h.articleEntityExists(entityType, entityID)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, fmt.Errorf("Сущность %s с ID %s не найдена", entityType, entityID)
		}
		unique[entityType+":"+entityID] = articleLinkDTO{EntityType: entityType, EntityID: entityID}
	}
	result := make([]articleLinkDTO, 0, len(unique))
	for _, item := range unique {
		result = append(result, item)
	}
	return result, nil
}

func normalizeArticleEntityType(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return "", fmt.Errorf("entity_type обязателен")
	case "company":
		return "Company", nil
	case "server":
		return "Server", nil
	case "workstation":
		return "Workstation", nil
	case "fiscalregister", "fiscal":
		return "FiscalRegister", nil
	case "ticket":
		return "Ticket", nil
	default:
		return "", fmt.Errorf("Неподдерживаемый entity_type: %s", value)
	}
}

func (h *ArticleHandler) articleEntityExists(entityType, entityID string) (bool, error) {
	table, ok := allowedArticleEntityTypes[entityType]
	if !ok {
		return false, fmt.Errorf("Неподдерживаемый entity_type: %s", entityType)
	}
	var count int64
	if err := h.db.Table(table).Where("id = ?", entityID).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func replaceArticleLinks(tx *gorm.DB, articleID string, links []articleLinkDTO) error {
	if err := tx.Where("article_id = ?", articleID).Delete(&models.ArticleLink{}).Error; err != nil {
		return err
	}
	if len(links) == 0 {
		return nil
	}
	items := make([]models.ArticleLink, 0, len(links))
	for _, link := range links {
		items = append(items, models.ArticleLink{
			ArticleID:  articleID,
			EntityType: link.EntityType,
			EntityID:   link.EntityID,
		})
	}
	return tx.Create(&items).Error
}

func isAllowedArticleType(value string) bool {
	return slices.Contains([]string{
		models.ArticleTypeWiki,
		models.ArticleTypeReleaseNote,
		models.ArticleTypeCompanyNews,
		models.ArticleTypeIncident,
		models.ArticleTypeInternalDoc,
	}, value)
}

func isAllowedArticleStatus(value string) bool {
	return slices.Contains([]string{
		models.ArticleStatusDraft,
		models.ArticleStatusPublished,
		models.ArticleStatusArchived,
	}, value)
}

func normalizeArticleTags(tags []string) []string {
	result := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		normalized := strings.TrimSpace(tag)
		if normalized == "" {
			continue
		}
		key := strings.ToLower(normalized)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, normalized)
	}
	return result
}

func joinArticleTags(tags []string) string {
	return strings.Join(normalizeArticleTags(tags), ",")
}

func splitArticleTags(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	return normalizeArticleTags(strings.Split(raw, ","))
}

func slugifyArticleTitle(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "ё", "е")
	normalized = regexp.MustCompile(`[^a-z0-9а-я]+`).ReplaceAllString(normalized, "-")
	normalized = strings.Trim(normalized, "-")
	if normalized == "" {
		return "article"
	}
	return normalized
}

func (h *ArticleHandler) uniqueArticleSlug(base string) string {
	slug := slugifyArticleTitle(base)
	for i := 0; ; i++ {
		candidate := slug
		if i > 0 {
			candidate = fmt.Sprintf("%s-%d", slug, i+1)
		}
		var count int64
		if err := h.db.Model(&models.Article{}).Where("slug = ?", candidate).Count(&count).Error; err != nil || count == 0 {
			return candidate
		}
	}
}

var errArticleForbidden = errors.New("недостаточно прав")

func (h *ArticleHandler) canReadArticle(r *http.Request, item models.Article) bool {
	if item.Status == models.ArticleStatusPublished {
		return true
	}
	return h.canEditArticle(r, item)
}

func (h *ArticleHandler) canEditArticle(r *http.Request, item models.Article) bool {
	if h.canManageArticles(r) {
		return true
	}
	currentUserID := h.currentUserID(r)
	return currentUserID != nil && item.AuthorID != nil && *currentUserID == *item.AuthorID
}

func (h *ArticleHandler) canManageArticles(r *http.Request) bool {
	roles, _ := r.Context().Value(contextkeys.UserRolesContextKey).([]string)
	return slices.Contains(roles, user.RoleAdmin) || slices.Contains(roles, user.RoleSupportSpecialist)
}

func (h *ArticleHandler) currentUserID(r *http.Request) *uint {
	rawUserID := r.Context().Value(contextkeys.UserIDContextKey)
	if rawUserID == nil {
		return nil
	}
	parsed, err := strconv.ParseUint(strings.TrimSpace(fmt.Sprintf("%v", rawUserID)), 10, 32)
	if err != nil || parsed == 0 {
		return nil
	}
	return new(uint(parsed))
}

func (h *ArticleHandler) resolveAuthor(ctx context.Context) (*uint, string) {
	rawUserID := ctx.Value(contextkeys.UserIDContextKey)
	fallback := "Сотрудник"
	if rawUserID == nil {
		return nil, fallback
	}
	parsed, err := strconv.ParseUint(strings.TrimSpace(fmt.Sprintf("%v", rawUserID)), 10, 32)
	if err != nil || parsed == 0 {
		return nil, fallback
	}
	userID := uint(parsed)
	if h.userRepo == nil {
		return &userID, fallback
	}
	profile, err := h.userRepo.GetByID(ctx, userID)
	if err != nil || profile == nil {
		return &userID, fallback
	}
	name := strings.TrimSpace(profile.FullName)
	if name == "" {
		name = strings.TrimSpace(profile.Username)
	}
	if name == "" {
		name = fallback
	}
	return &userID, name
}

func toArticleDTO(item models.Article) articleDTO {
	links := make([]articleLinkDTO, 0, len(item.Links))
	for _, link := range item.Links {
		links = append(links, articleLinkDTO{
			EntityType: link.EntityType,
			EntityID:   link.EntityID,
		})
	}
	return articleDTO{
		ID:            item.ID,
		Slug:          item.Slug,
		Title:         item.Title,
		Summary:       item.Summary,
		Content:       item.Content,
		ContentFormat: item.ContentFormat,
		Type:          item.Type,
		Status:        item.Status,
		ProjectKey:    item.ProjectKey,
		Version:       item.Version,
		Tags:          splitArticleTags(item.Tags),
		IsPinned:      item.IsPinned,
		PublishedAt:   item.PublishedAt,
		AuthorID:      item.AuthorID,
		AuthorName:    item.AuthorName,
		Links:         links,
		CreatedAt:     item.CreatedAt,
		UpdatedAt:     item.UpdatedAt,
	}
}
