package handlers

import (
	"encoding/json"
	"errors"
	"etalon-server/internal/domain"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/pkg/utils"
	"etalon-server/internal/services"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/middleware"
	"etalon-server/internal/transport/http/response"
	"etalon-server/internal/transport/http/validators"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

type ServerHandler struct {
	service         server.Service
	deletionService services.EntityDeletionService
}

func NewServerHandler(service server.Service, deletionService services.EntityDeletionService) *ServerHandler {
	return &ServerHandler{service: service, deletionService: deletionService}
}

func (h *ServerHandler) RegisterRoutes(r chi.Router) {
	r.Route("/servers", func(r chi.Router) {
		r.Get("/", h.List)
		r.Get("/{id}", h.Get)
		r.Post("/", h.Create)
		r.Put("/{id}", h.Update)
		r.Delete("/{id}", h.Delete)
	})
}

// @Summary Получение списка серверов
// @Description Возвращает список серверов с пагинацией
// @Tags Servers
// @Accept json
// @Produce json
// @Param limit query int false "Limit"
// @Param offset query int false "Offset"
// @Success 200 {object} api.PaginatedResponse
// @Router /api/servers [get]
func (h *ServerHandler) List(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	term := r.URL.Query().Get("term")
	companyIDs := parseCSVQuery(r.URL.Query().Get("company_ids"))
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	var (
		items []server.Server
		total int64
		err   error
	)
	if term != "" {
		items, total, err = h.service.Search(r.Context(), term, limit, offset, companyIDs)
	} else {
		items, total, err = h.service.List(r.Context(), limit, offset, companyIDs)
	}
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			middleware.GetLogger(r.Context()).Error("не найдена запись", "error", err)
			response.RespondWithError(w, http.StatusNotFound, "Not Found")
			return
		}
		middleware.GetLogger(r.Context()).Error("list failed", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Internal Error")
		return
	}
	dtos := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		dtos = append(dtos, toServerResponse(item))
	}
	hasPrev := offset > 0
	hasNext := int64(offset+len(items)) < total
	response.RespondWithJSON(w, http.StatusOK, api.PaginatedResponse{
		Data:    dtos,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
		HasNext: hasNext,
		HasPrev: hasPrev,
	})
}

func (h *ServerHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	item, err := h.service.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			middleware.GetLogger(r.Context()).Error("не найдена запись", "error", err)
			response.RespondWithError(w, http.StatusNotFound, "Not Found")
			return
		}
		middleware.GetLogger(r.Context()).Error("get failed", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Internal Error")
		return
	}
	if item == nil {
		middleware.GetLogger(r.Context()).Error("не найдена запись", "item == nil ?", item == nil, "error", err)
		response.RespondWithError(w, http.StatusNotFound, "Not Found")
		return
	}
	partnersLink := validators.BuildPartnersPortalLink(
		utils.SafeStringDereference(item.CabinetLink),
		utils.SafeStringDereference(item.IP),
	)
	payload := toServerResponse(*item)
	payload["partners_link"] = partnersLink
	response.RespondWithJSON(w, http.StatusOK, payload)
}

func (h *ServerHandler) Create(w http.ResponseWriter, r *http.Request) {
	var dto api.ServerCreateDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Invalid Body")
		return
	}
	item, err := h.service.Create(r.Context(), &dto)
	if err != nil {
		if errors.Is(err, domain.ErrAlreadyExists) {
			middleware.GetLogger(r.Context()).Error("не найдена запись", "error", err)
			response.RespondWithError(w, http.StatusConflict, "Already Exists")
			return
		}
		middleware.GetLogger(r.Context()).Error("create failed", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Internal Error")
		return
	}
	response.RespondWithJSON(w, http.StatusCreated, toServerResponse(*item))
}

func (h *ServerHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var data map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}
	err := h.service.Update(r.Context(), id, data)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			middleware.GetLogger(r.Context()).Error("не найдена запись", "error", err)
			response.RespondWithError(w, http.StatusNotFound, "Not Found")
			return
		}
		if errors.Is(err, domain.ErrAlreadyExists) {
			middleware.GetLogger(r.Context()).Error("не найдена запись", "error", err)
			response.RespondWithError(w, http.StatusConflict, "Already Exists")
			return
		}
		middleware.GetLogger(r.Context()).Error("update failed", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Internal Error")
		return
	}
	response.RespondWithJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *ServerHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if h.deletionService != nil {
		item, err := h.deletionService.RequestDeletion(r.Context(), services.EntityDeletionRequest{
			EntityType: "Server",
			EntityID:   id,
			Reason:     "Ручное удаление из карточки сущности",
			Source:     models.EntityDeletionSourceManual,
		})
		if err != nil {
			if errors.Is(err, services.ErrDeletionEntityNotFound) {
				response.RespondWithError(w, http.StatusNotFound, "Not Found")
				return
			}
			middleware.GetLogger(r.Context()).Error("delete stage failed", "error", err)
			response.RespondWithError(w, http.StatusInternalServerError, "Delete Stage Failed")
			return
		}
		response.RespondWithJSON(w, http.StatusAccepted, item)
		return
	}
	if err := h.service.Delete(r.Context(), id); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			middleware.GetLogger(r.Context()).Error("не найдена запись", "error", err)
			response.RespondWithError(w, http.StatusNotFound, "Not Found")
			return
		}
		middleware.GetLogger(r.Context()).Error("delete failed", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Internal Error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func toServerResponse(item server.Server) map[string]interface{} {
	var statusDetails interface{}
	if len(item.StatusDetails) > 0 {
		_ = json.Unmarshal(item.StatusDetails, &statusDetails)
	}
	return map[string]interface{}{
		"id":                 item.ID,
		"created_at":         item.CreatedAt,
		"updated_at":         item.UpdatedAt,
		"last_updated_by":    item.LastUpdatedBy,
		"deleted_at":         item.DeletedAt,
		"unique_id":          item.UniqueID,
		"ip":                 item.IP,
		"cabinet_link":       item.CabinetLink,
		"device_name":        item.DeviceName,
		"last_modified_date": item.LastModifiedDate,
		"litemanager":        item.Litemanager,
		"server_version":     item.ServerVersion,
		"description":        item.Description,
		"owner_id":           item.OwnerID,
		"owner_title":        item.OwnerTitle,
		"owner_parent_id":    item.OwnerParentID,
		"owner_parent_title": item.OwnerParentTitle,
		"owner_binding_mode": item.OwnerBindingMode,
		"additional_owners":  item.AdditionalOwners,
		"server_name":        item.ServerName,
		"server_edition":     item.ServerEdition,
		"last_polled_at":     item.LastPolledAt,
		"status":             item.Status,
		"health_status":      item.HealthStatus,
		"status_details":     statusDetails,
		"crm_id":             item.CRMid,
		"rdp":                item.RDP,
		"teamviewer":         item.Teamviewer,
		"anydesk":            item.Anydesk,
	}
}

func parseCSVQuery(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
