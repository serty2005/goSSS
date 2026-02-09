package handlers

import (
	"encoding/json"
	"errors"
	"etalon-server/internal/contextkeys"
	"etalon-server/internal/domain"
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
	r.Get("/filters", h.Filters)
	r.Post("/", h.Create) // Создание внутреннего тикета
	r.Get("/{id}", h.GetDetails)
	r.Post("/{id}/link", h.LinkAsset)
	r.Post("/{id}/comments", h.AddComment)
	r.Post("/{id}/refresh-comments", h.RefreshCommentsFromServiceDesk)
	r.Post("/{id}/connection-copy", h.RecordConnectionCopy)
	r.Patch("/{id}/status", h.ChangeStatus)
	r.Patch("/{id}/description", h.UpdateDescription)
	r.Patch("/{id}/assign", h.Assign)
	r.Patch("/{id}/company", h.ChangeCompany)
}

// Create создает новый тикет (внутренний).
func (h *TicketHandler) Create(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	var dto api.TicketCreateInternalDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректный JSON")
		return
	}

	// Получаем ID текущего пользователя
	userID := getUserIDFromContext(r)
	if userID == 0 {
		response.RespondWithError(w, http.StatusUnauthorized, "ID пользователя не найден в контексте")
		return
	}

	ticket, err := h.service.CreateInternal(r.Context(), dto, userID)
	if err != nil {
		if errors.Is(err, services.ErrReporterNotFound) {
			response.RespondWithError(w, http.StatusUnauthorized, "Пользователь из сессии не найден, выполните вход заново")
			return
		}
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
		response.RespondWithError(w, http.StatusBadRequest, "Некорректный JSON")
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

func (h *TicketHandler) AddComment(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var dto struct {
		Comment string `json:"comment"`
	}
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректный JSON")
		return
	}

	userID := getUserIDFromContext(r)
	if userID == 0 {
		response.RespondWithError(w, http.StatusUnauthorized, "ID пользователя не найден в контексте")
		return
	}
	if strings.TrimSpace(dto.Comment) == "" {
		response.RespondWithError(w, http.StatusBadRequest, "Комментарий обязателен")
		return
	}

	if err := h.service.AddComment(r.Context(), id, dto.Comment, userID); err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.RespondWithJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

func (h *TicketHandler) RefreshCommentsFromServiceDesk(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	added, err := h.service.RefreshCommentsFromServiceDesk(r.Context(), id)
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	response.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"status": "ok",
		"added":  added,
	})
}

func (h *TicketHandler) RecordConnectionCopy(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var dto struct {
		Label string `json:"label"`
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректный JSON")
		return
	}

	userID := getUserIDFromContext(r)
	if userID == 0 {
		response.RespondWithError(w, http.StatusUnauthorized, "ID пользователя не найден в контексте")
		return
	}
	if strings.TrimSpace(dto.Value) == "" {
		response.RespondWithError(w, http.StatusBadRequest, "Значение обязательно")
		return
	}

	if err := h.service.RecordConnectionCopy(r.Context(), id, dto.Label, dto.Value, userID); err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.RespondWithJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

// UpdateDescription обновляет описание тикета.
func (h *TicketHandler) UpdateDescription(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var dto struct {
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректный JSON")
		return
	}

	userID := getUserIDFromContext(r)
	if userID == 0 {
		response.RespondWithError(w, http.StatusUnauthorized, "ID пользователя не найден в контексте")
		return
	}

	ticket, err := h.service.UpdateDescription(r.Context(), id, dto.Description, userID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			response.RespondWithError(w, http.StatusNotFound, "Не найдено")
			return
		}
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.RespondWithJSON(w, http.StatusOK, ticket)
}

func (h *TicketHandler) Assign(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var dto api.TicketAssignDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректный JSON")
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

func (h *TicketHandler) ChangeCompany(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var dto api.TicketChangeCompanyDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректный JSON")
		return
	}

	companyID := strings.TrimSpace(dto.CompanyID)
	if companyID == "" {
		response.RespondWithError(w, http.StatusBadRequest, "company_id обязателен")
		return
	}

	userID := getUserIDFromContext(r)
	if userID == 0 {
		response.RespondWithError(w, http.StatusUnauthorized, "ID пользователя не найден в контексте")
		return
	}

	ticket, err := h.service.ChangeCompany(r.Context(), id, companyID, userID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			response.RespondWithError(w, http.StatusNotFound, "Не найдено")
			return
		}
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.RespondWithJSON(w, http.StatusOK, ticket)
}

// List возвращает список заявок с фильтрацией.
func (h *TicketHandler) List(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())

	// Получаем параметры пагинации.
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 50
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}

	// Собираем фильтры.
	filter := tickets.TicketFilter{
		Limit:       limit,
		Offset:      offset,
		CompanyID:   r.URL.Query().Get("company_id"),
		SearchQuery: r.URL.Query().Get("search"),
		SortBy:      r.URL.Query().Get("sort_by"),
	}

	// Фильтр по статусам.
	if statusStr := r.URL.Query().Get("status"); statusStr != "" {
		filter.Statuses = expandStatuses(strings.Split(statusStr, ","))
	}
	if assetID := r.URL.Query().Get("asset_id"); assetID != "" {
		filter.AssetID = &assetID
	}

	items, total, err := h.service.List(r.Context(), filter)
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, "Ошибка получения списка заявок")
		return
	}

	// Получаем комментарии (для вывода в списке).
	ticketIDs := make([]string, 0, len(items))
	for _, item := range items {
		ticketIDs = append(ticketIDs, item.ID)
	}
	lastComments, err := h.service.GetLastComments(r.Context(), ticketIDs)
	if err != nil {
		log.Error("Failed to get last comments", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Ошибка получения списка заявок")
		return
	}

	// Преобразуем в TicketListDTO.
	dtos := make([]api.TicketListDTO, len(items))
	for i, item := range items {
		var assignee *struct {
			ID       uint   `json:"id"`
			FullName string `json:"fullName"`
		}
		if item.Assignee != nil {
			assignee = &struct {
				ID       uint   `json:"id"`
				FullName string `json:"fullName"`
			}{
				ID:       item.Assignee.ID,
				FullName: item.Assignee.FullName,
			}
		}
		dtos[i] = api.TicketListDTO{
			ID:                item.ID,
			Number:            item.Number,
			ServiceDeskUUID:   item.ServiceDeskUUID,
			Status:            item.Status,
			Subject:           item.Subject,
			Description:       item.Description,
			LastComment:       lastComments[item.ID].Text,
			LastCommentAuthor: lastComments[item.ID].AuthorName,
			CompanyID:         item.CompanyID,
			CompanyName:       item.CompanyName,
			ContractID:        item.ContractID,
			IsCommonContract:  item.IsCommonContract,
			Assignee:          assignee,
			// LastActivityDate is basically UpdatedAt or CreatedAt
			LastActivityDate: item.UpdatedAt,
			CreatedAt:        item.CreatedAt,
		}
	}

	hasNext := int64(offset+limit) < total
	hasPrev := offset > 0
	response.RespondWithJSON(w, http.StatusOK, api.PaginatedResponse{
		Data:    dtos,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
		HasNext: hasNext,
		HasPrev: hasPrev,
	})
}

// GetDetails возвращает полную информацию о заявке.

// Filters возвращает агрегированные значения для фильтров.
func (h *TicketHandler) Filters(w http.ResponseWriter, r *http.Request) {
	filter := tickets.TicketFilter{
		SearchQuery: r.URL.Query().Get("search"),
	}
	if statusStr := r.URL.Query().Get("status"); statusStr != "" {
		filter.Statuses = expandStatuses(strings.Split(statusStr, ","))
	}

	items, err := h.service.GetCompanyFilters(r.Context(), filter)
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, "Ошибка получения фильтров заявок")
		return
	}

	response.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"companies": items,
	})
}

func expandStatuses(items []string) []string {
	if len(items) == 0 {
		return items
	}
	seen := make(map[string]struct{})
	var out []string
	for _, raw := range items {
		status := strings.TrimSpace(raw)
		if status == "" {
			continue
		}
		legacy := map[string][]string{
			"new":         {"registered"},
			"in_progress": {"inprogress"},
			"pending":     {"wait"},
		}
		if _, ok := seen[status]; !ok {
			seen[status] = struct{}{}
			out = append(out, status)
		}
		if legacyStatuses, ok := legacy[status]; ok {
			for _, legacyStatus := range legacyStatuses {
				if _, ok := seen[legacyStatus]; ok {
					continue
				}
				seen[legacyStatus] = struct{}{}
				out = append(out, legacyStatus)
			}
		}
	}
	return out
}

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
		response.RespondWithError(w, http.StatusBadRequest, "Некорректный JSON")
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
