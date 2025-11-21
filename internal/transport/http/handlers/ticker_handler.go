package handlers

import (
	"encoding/json"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/services"
	"etalon-server/internal/transport/http/middleware"
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
	r.Get("/{id}", h.GetDetails)
	r.Post("/{id}/link", h.LinkAsset)
}

// List возвращает список заявок с фильтрацией.
func (h *TicketHandler) List(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())

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
		Limit:     limit,
		Offset:    offset,
		CompanyID: r.URL.Query().Get("company_id"),
		SortBy:    r.URL.Query().Get("sort_by"),
	}

	// Фильтр по оборудованию
	if assetID := r.URL.Query().Get("asset_id"); assetID != "" {
		filter.AssetID = &assetID
	}
	if assetType := r.URL.Query().Get("asset_type"); assetType != "" {
		filter.AssetType = &assetType
	}

	// Фильтр по статусам (через запятую)
	if statusStr := r.URL.Query().Get("status"); statusStr != "" {
		filter.Statuses = strings.Split(statusStr, ",")
	}

	items, total, err := h.service.List(r.Context(), filter)
	if err != nil {
		log.Error("Failed to list tickets", "error", err)
		RespondWithError(w, http.StatusInternalServerError, "Ошибка получения списка заявок")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"data":   items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// GetDetails возвращает полную информацию о заявке.
func (h *TicketHandler) GetDetails(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	details, err := h.service.GetDetails(r.Context(), id)
	if err != nil {
		middleware.GetLogger(r.Context()).Error("Failed to get ticket details", "id", id, "error", err)
		RespondWithError(w, http.StatusInternalServerError, "Ошибка получения деталей заявки")
		return
	}
	if details == nil {
		RespondWithError(w, http.StatusNotFound, "Заявка не найдена")
		return
	}

	RespondWithJSON(w, http.StatusOK, details)
}

// LinkAssetRequest тело запроса для привязки.
type LinkAssetRequest struct {
	AssetID   string `json:"asset_id"`
	AssetType string `json:"asset_type"` // Server, FiscalRegister, Workstation
}

// LinkAsset привязывает заявку к оборудованию.
func (h *TicketHandler) LinkAsset(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req LinkAssetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "Неверный формат запроса")
		return
	}

	if req.AssetID == "" || req.AssetType == "" {
		RespondWithError(w, http.StatusBadRequest, "AssetID и AssetType обязательны")
		return
	}

	err := h.service.LinkToAsset(r.Context(), id, req.AssetID, req.AssetType)
	if err != nil {
		middleware.GetLogger(r.Context()).Error("Failed to link asset", "ticket_id", id, "error", err)
		RespondWithError(w, http.StatusInternalServerError, "Ошибка привязки оборудования")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
