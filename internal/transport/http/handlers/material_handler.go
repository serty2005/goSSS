package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"etalon-server/internal/contextkeys"
	"etalon-server/internal/domain"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/user"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/response"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"
)

type MaterialHandler struct {
	db       *gorm.DB
	userRepo user.Repository
}

func NewMaterialHandler(db *gorm.DB, userRepo user.Repository) *MaterialHandler {
	return &MaterialHandler{db: db, userRepo: userRepo}
}

func (h *MaterialHandler) RegisterRoutes(r chi.Router) {
	r.Route("/materials", func(r chi.Router) {
		r.Get("/", h.List)
		r.Post("/", h.Create)
		r.Get("/{id}", h.Get)
		r.Put("/{id}", h.Update)
		r.Delete("/{id}", h.Delete)
	})
}

type materialEntityRefDTO struct {
	EntityType string `json:"entity_type"`
	EntityID   string `json:"entity_id"`
}

type materialDTO struct {
	ID         string                 `json:"id"`
	AuthorID   *uint                  `json:"author_id,omitempty"`
	AuthorName string                 `json:"author_name"`
	Subject    string                 `json:"subject"`
	Content    string                 `json:"content"`
	EntityRefs []materialEntityRefDTO `json:"entity_refs"`
	CreatedAt  string                 `json:"created_at"`
	UpdatedAt  string                 `json:"updated_at"`
}

type materialPayload struct {
	Subject    string                 `json:"subject"`
	Content    string                 `json:"content"`
	EntityRefs []materialEntityRefDTO `json:"entity_refs"`
}

var allowedMaterialEntityTypes = map[string]string{
	"Company":        "companies",
	"Server":         "servers",
	"Workstation":    "workstations",
	"FiscalRegister": "fiscal_registers",
}

func (h *MaterialHandler) List(w http.ResponseWriter, r *http.Request) {
	entityType := strings.TrimSpace(r.URL.Query().Get("entity_type"))
	entityID := strings.TrimSpace(r.URL.Query().Get("entity_id"))
	term := strings.TrimSpace(r.URL.Query().Get("term"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	tx := h.db.WithContext(r.Context()).Model(&models.Material{})
	if entityType != "" || entityID != "" {
		if entityType == "" || entityID == "" {
			response.RespondWithError(w, http.StatusBadRequest, "entity_type Рё entity_id РґРѕР»Р¶РЅС‹ Р±С‹С‚СЊ СѓРєР°Р·Р°РЅС‹ РІРјРµСЃС‚Рµ")
			return
		}
		normalizedType, err := normalizeMaterialEntityType(entityType)
		if err != nil {
			response.RespondWithError(w, http.StatusBadRequest, err.Error())
			return
		}
		tx = tx.Joins("JOIN material_links ON material_links.material_id = materials.id").
			Where("material_links.entity_type = ? AND material_links.entity_id = ?", normalizedType, entityID)
	}

	if term != "" {
		pattern := "%" + term + "%"
		tx = tx.Where("materials.subject ILIKE ? OR materials.content ILIKE ?", pattern, pattern)
	}

	var total int64
	if err := tx.Distinct("materials.id").Count(&total).Error; err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, "Internal Error")
		return
	}

	var items []models.Material
	if err := tx.
		Distinct("materials.*").
		Preload("Links").
		Order("materials.updated_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&items).Error; err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, "Internal Error")
		return
	}

	result := make([]materialDTO, 0, len(items))
	for _, item := range items {
		result = append(result, toMaterialDTO(item))
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

func (h *MaterialHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	var item models.Material
	err := h.db.WithContext(r.Context()).
		Preload("Links").
		Where("id = ?", id).
		First(&item).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.RespondWithError(w, http.StatusNotFound, "Not Found")
			return
		}
		response.RespondWithError(w, http.StatusInternalServerError, "Internal Error")
		return
	}
	response.RespondWithJSON(w, http.StatusOK, toMaterialDTO(item))
}

func (h *MaterialHandler) Create(w http.ResponseWriter, r *http.Request) {
	var payload materialPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	subject := strings.TrimSpace(payload.Subject)
	content := strings.TrimSpace(payload.Content)
	if subject == "" || content == "" {
		response.RespondWithError(w, http.StatusBadRequest, "РўРµРјР° Рё СЃРѕРґРµСЂР¶Р°РЅРёРµ РѕР±СЏР·Р°С‚РµР»СЊРЅС‹")
		return
	}

	refs, err := h.normalizeRefs(payload.EntityRefs)
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(refs) == 0 {
		response.RespondWithError(w, http.StatusBadRequest, "РќСѓР¶РЅР° С…РѕС‚СЏ Р±С‹ РѕРґРЅР° РїСЂРёРІСЏР·РєР° Рє СЃСѓС‰РЅРѕСЃС‚Рё")
		return
	}

	authorID, authorName := h.resolveAuthor(r.Context())
	item := &models.Material{
		AuthorID:   authorID,
		AuthorName: authorName,
		Subject:    subject,
		Content:    content,
	}

	err = h.db.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(item).Error; err != nil {
			return err
		}
		links := make([]models.MaterialLink, 0, len(refs))
		for _, ref := range refs {
			links = append(links, models.MaterialLink{
				MaterialID: item.ID,
				EntityType: ref.EntityType,
				EntityID:   ref.EntityID,
			})
		}
		if len(links) > 0 {
			if err := tx.Create(&links).Error; err != nil {
				return err
			}
		}
		return tx.Preload("Links").First(item, "id = ?", item.ID).Error
	})
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, "Internal Error")
		return
	}

	response.RespondWithJSON(w, http.StatusCreated, toMaterialDTO(*item))
}

func (h *MaterialHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	var payload materialPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	subject := strings.TrimSpace(payload.Subject)
	content := strings.TrimSpace(payload.Content)
	if subject == "" || content == "" {
		response.RespondWithError(w, http.StatusBadRequest, "РўРµРјР° Рё СЃРѕРґРµСЂР¶Р°РЅРёРµ РѕР±СЏР·Р°С‚РµР»СЊРЅС‹")
		return
	}
	refs, err := h.normalizeRefs(payload.EntityRefs)
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(refs) == 0 {
		response.RespondWithError(w, http.StatusBadRequest, "РќСѓР¶РЅР° С…РѕС‚СЏ Р±С‹ РѕРґРЅР° РїСЂРёРІСЏР·РєР° Рє СЃСѓС‰РЅРѕСЃС‚Рё")
		return
	}

	var item models.Material
	err = h.db.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ?", id).First(&item).Error; err != nil {
			return err
		}
		item.Subject = subject
		item.Content = content
		if err := tx.Save(&item).Error; err != nil {
			return err
		}
		if err := tx.Where("material_id = ?", id).Delete(&models.MaterialLink{}).Error; err != nil {
			return err
		}
		links := make([]models.MaterialLink, 0, len(refs))
		for _, ref := range refs {
			links = append(links, models.MaterialLink{
				MaterialID: id,
				EntityType: ref.EntityType,
				EntityID:   ref.EntityID,
			})
		}
		if err := tx.Create(&links).Error; err != nil {
			return err
		}
		return tx.Preload("Links").First(&item, "id = ?", id).Error
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.RespondWithError(w, http.StatusNotFound, "Not Found")
			return
		}
		response.RespondWithError(w, http.StatusInternalServerError, "Internal Error")
		return
	}

	response.RespondWithJSON(w, http.StatusOK, toMaterialDTO(item))
}

func (h *MaterialHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	err := h.db.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("id = ?", id).Delete(&models.Material{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			response.RespondWithError(w, http.StatusNotFound, "Not Found")
			return
		}
		response.RespondWithError(w, http.StatusInternalServerError, "Internal Error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *MaterialHandler) normalizeRefs(refs []materialEntityRefDTO) ([]materialEntityRefDTO, error) {
	unique := make(map[string]materialEntityRefDTO)
	for _, ref := range refs {
		entityType, err := normalizeMaterialEntityType(ref.EntityType)
		if err != nil {
			return nil, err
		}
		entityID := strings.TrimSpace(ref.EntityID)
		if entityID == "" {
			return nil, fmt.Errorf("entity_id РѕР±СЏР·Р°С‚РµР»РµРЅ")
		}
		exists, err := h.entityExists(entityType, entityID)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, fmt.Errorf("СЃСѓС‰РЅРѕСЃС‚СЊ %s СЃ ID %s РЅРµ РЅР°Р№РґРµРЅР°", entityType, entityID)
		}
		key := entityType + ":" + entityID
		unique[key] = materialEntityRefDTO{
			EntityType: entityType,
			EntityID:   entityID,
		}
	}
	result := make([]materialEntityRefDTO, 0, len(unique))
	for _, item := range unique {
		result = append(result, item)
	}
	return result, nil
}

func normalizeMaterialEntityType(value string) (string, error) {
	key := strings.TrimSpace(value)
	switch strings.ToLower(key) {
	case "company":
		return "Company", nil
	case "server":
		return "Server", nil
	case "workstation":
		return "Workstation", nil
	case "fiscalregister", "fiscal":
		return "FiscalRegister", nil
	default:
		return "", fmt.Errorf("РЅРµРїРѕРґРґРµСЂР¶РёРІР°РµРјС‹Р№ entity_type: %s", key)
	}
}

func (h *MaterialHandler) entityExists(entityType, entityID string) (bool, error) {
	table, ok := allowedMaterialEntityTypes[entityType]
	if !ok {
		return false, fmt.Errorf("РЅРµРїРѕРґРґРµСЂР¶РёРІР°РµРјС‹Р№ entity_type: %s", entityType)
	}
	var count int64
	if err := h.db.Table(table).Where("id = ?", entityID).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (h *MaterialHandler) resolveAuthor(ctx context.Context) (*uint, string) {
	rawUserID := ctx.Value(contextkeys.UserIDContextKey)
	if rawUserID == nil {
		return nil, "РЎРѕС‚СЂСѓРґРЅРёРє"
	}
	userIDStr := strings.TrimSpace(fmt.Sprintf("%v", rawUserID))
	if userIDStr == "" {
		return nil, "РЎРѕС‚СЂСѓРґРЅРёРє"
	}
	parsed, err := strconv.ParseUint(userIDStr, 10, 32)
	if err != nil {
		return nil, "РЎРѕС‚СЂСѓРґРЅРёРє"
	}
	userID := uint(parsed)
	if h.userRepo == nil {
		return &userID, "РЎРѕС‚СЂСѓРґРЅРёРє"
	}
	profile, err := h.userRepo.GetByID(ctx, userID)
	if err != nil || profile == nil {
		return &userID, "РЎРѕС‚СЂСѓРґРЅРёРє"
	}
	name := strings.TrimSpace(profile.FullName)
	if name == "" {
		name = strings.TrimSpace(profile.Username)
	}
	if name == "" {
		name = "РЎРѕС‚СЂСѓРґРЅРёРє"
	}
	return &userID, name
}

func toMaterialDTO(item models.Material) materialDTO {
	entityRefs := make([]materialEntityRefDTO, 0, len(item.Links))
	for _, link := range item.Links {
		entityRefs = append(entityRefs, materialEntityRefDTO{
			EntityType: link.EntityType,
			EntityID:   link.EntityID,
		})
	}
	return materialDTO{
		ID:         item.ID,
		AuthorID:   item.AuthorID,
		AuthorName: item.AuthorName,
		Subject:    item.Subject,
		Content:    item.Content,
		EntityRefs: entityRefs,
		CreatedAt:  item.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:  item.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}
