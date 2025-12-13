package handlers

import (
	"encoding/json"
	"etalon-server/internal/contextkeys"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/services"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/middleware"
	"etalon-server/internal/transport/http/response"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

type TicketHandler struct {
	service services.TicketService
}

func NewTicketHandler(service services.TicketService) *TicketHandler {
	return &TicketHandler{service: service}
}

func (h *TicketHandler) RegisterRoutes(r chi.Router) {
	r.Get("/", h.List)
	r.Post("/", h.Create) // Создание внутреннего тикета
	r.Get("/{id}", h.GetDetails)
	r.Post("/{id}/link", h.LinkAsset)
	r.Patch("/{id}/status", h.ChangeStatus)
	r.Patch("/{id}/assign", h.Assign)
}

// Create создает новый тикет (внутренний).
func (h *TicketHandler) Create(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	var dto api.TicketCreateInternalDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	// Получаем ID текущего пользователя
	userID := getUserIDFromContext(r)
	if userID == 0 {
		response.RespondWithError(w, http.StatusUnauthorized, "User ID not found in context")
		return
	}

	ticket, err := h.service.CreateInternal(r.Context(), dto, userID)
	if err != nil {
		log.Error("Failed to create ticket", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Failed to create ticket")
		return
	}
	response.RespondWithJSON(w, http.StatusCreated, ticket)
}

func (h *TicketHandler) ChangeStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var dto api.TicketStatusChangeDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	userID := getUserIDFromContext(r)
	ticket, err := h.service.ChangeStatus(r.Context(), id, dto.Status, dto.Comment, userID)
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.RespondWithJSON(w, http.StatusOK, ticket)
}

func (h *TicketHandler) Assign(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var dto api.TicketAssignDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	userID := getUserIDFromContext(r)
	ticket, err := h.service.Assign(r.Context(), id, dto.AssigneeID, userID)
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.RespondWithJSON(w, http.StatusOK, ticket)
}

// List возвращает список заявок с фильтрацией.
func (h *TicketHandler) List(w http.ResponseWriter, r *http.Request) {

	// Парсинг параметров пагинации
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 50
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}

	// Парсинг фильтров
	filter := tickets.TicketFilter{
		Limit:       limit,
		Offset:      offset,
		CompanyID:   r.URL.Query().Get("company_id"),
		SearchQuery: r.URL.Query().Get("search"),
		SortBy:      r.URL.Query().Get("sort_by"),
	}

	// Фильтр по оборудованию
	if statusStr := r.URL.Query().Get("status"); statusStr != "" {
		filter.Statuses = strings.Split(statusStr, ",")
	}
	if assetID := r.URL.Query().Get("asset_id"); assetID != "" {
		filter.AssetID = &assetID
	}

	// Фильтр по статусам (через запятую)
	if statusStr := r.URL.Query().Get("status"); statusStr != "" {
		filter.Statuses = strings.Split(statusStr, ",")
	}

	items, total, err := h.service.List(r.Context(), filter)
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, "Error listing tickets")
		return
	}

	// Маппинг в TicketListDTO
	dtos := make([]api.TicketListDTO, len(items))
	for i, item := range items {
		dtos[i] = api.TicketListDTO{
			ID:              item.ID,
			Number:          item.Number,
			ServiceDeskUUID: item.ServiceDeskUUID,
			Status:          item.Status,
			Subject:         item.Subject,
			CompanyID:       item.CompanyID,
			// LastActivityDate is basically UpdatedAt or CreatedAt
			LastActivityDate: item.UpdatedAt,
		}
	}

	response.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"data":  dtos,
		"total": total,
	})
}

// GetDetails возвращает полную информацию о заявке.
func (h *TicketHandler) GetDetails(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	details, err := h.service.GetDetails(r.Context(), id)
	if err != nil {
		middleware.GetLogger(r.Context()).Error("Failed to get ticket details", "id", id, "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Ошибка получения деталей заявки")
		return
	}
	if details == nil {
		response.RespondWithError(w, http.StatusNotFound, "Заявка не найдена")
		return
	}

	response.RespondWithJSON(w, http.StatusOK, details)
}

// LinkAssetRequest тело запроса для привязки.
type LinkAssetRequest struct {
	AssetID   string `json:"asset_id"`
	AssetType string `json:"asset_type"` // Server, FiscalRegister, Workstation
}

// LinkAsset привязывает заявку к оборудованию.
func (h *TicketHandler) LinkAsset(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		AssetID   string `json:"asset_id"`
		AssetType string `json:"asset_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}
	err := h.service.LinkToAsset(r.Context(), id, req.AssetID, req.AssetType)
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.RespondWithJSON(w, http.StatusOK, map[string]string{"status": "linked"})
}

func getUserIDFromContext(r *http.Request) uint {
	// Используем contextkeys
	userIDStr, ok := r.Context().Value(contextkeys.UserIDContextKey).(string)
	if !ok {
		return 0
	}
	id, _ := strconv.ParseUint(userIDStr, 10, 32)
	return uint(id)
}
