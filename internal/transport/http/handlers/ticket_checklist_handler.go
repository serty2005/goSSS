package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"etalon-server/internal/core/events"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/domain/user"
	"etalon-server/internal/services"
	"etalon-server/internal/transport/http/middleware"
	"etalon-server/internal/transport/http/response"
	"etalon-server/pkg/eventbus"

	"github.com/go-chi/chi/v5"
)

// TicketChecklistHandler обслуживает чеклисты тикетов и шаблоны чеклистов.
type TicketChecklistHandler struct {
	service  services.TicketChecklistService
	eventBus eventbus.EventBus
}

func NewTicketChecklistHandler(service services.TicketChecklistService, eventBus eventbus.EventBus) *TicketChecklistHandler {
	return &TicketChecklistHandler{service: service, eventBus: eventBus}
}

// RegisterTicketRoutes регистрирует маршруты чеклиста внутри /tickets.
func (h *TicketChecklistHandler) RegisterTicketRoutes(r chi.Router) {
	r.Get("/{id}/checklist", h.List)
	r.Post("/{id}/checklist/items", h.CreateItems)
	r.Patch("/{id}/checklist/items/{itemID}", h.UpdateItem)
	r.Delete("/{id}/checklist/items/{itemID}", h.DeleteItem)
	r.Post("/{id}/checklist/reorder", h.Reorder)
	r.Post("/{id}/checklist/apply-template", h.ApplyTemplate)
}

type checklistItemCreateRequest struct {
	Title       string  `json:"title"`
	ParentID    *string `json:"parent_id"`
	AssigneeIDs []uint  `json:"assignee_ids"`
}

type checklistItemUpdateRequest struct {
	Title       *string `json:"title"`
	IsDone      *bool   `json:"is_done"`
	AssigneeIDs *[]uint `json:"assignee_ids"`
}

type checklistReorderRequest struct {
	ParentID   *string  `json:"parent_id"`
	OrderedIDs []string `json:"ordered_ids"`
}

type checklistApplyTemplateRequest struct {
	TemplateID string `json:"template_id"`
}

type checklistTemplateRequest struct {
	Title       string                          `json:"title"`
	Description string                          `json:"description"`
	Items       []tickets.ChecklistTemplateNode `json:"items"`
	IsActive    *bool                           `json:"is_active"`
}

func (h *TicketChecklistHandler) List(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.List(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		h.respondError(w, r, err, "Не удалось получить чеклист")
		return
	}
	response.RespondWithJSON(w, http.StatusOK, items)
}

func (h *TicketChecklistHandler) CreateItems(w http.ResponseWriter, r *http.Request) {
	ticketID := chi.URLParam(r, "id")
	var req checklistItemCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректный JSON")
		return
	}
	userID := getUserIDFromContext(r)
	items, err := h.service.CreateItems(r.Context(), ticketID, services.ChecklistItemCreateInput{
		Title:       req.Title,
		ParentID:    req.ParentID,
		AssigneeIDs: req.AssigneeIDs,
	}, userID)
	if err != nil {
		h.respondError(w, r, err, "Не удалось добавить пункт чеклиста")
		return
	}
	h.publishChecklistUpdated(ticketID, userID, "Изменён чеклист")
	response.RespondWithJSON(w, http.StatusCreated, items)
}

func (h *TicketChecklistHandler) UpdateItem(w http.ResponseWriter, r *http.Request) {
	ticketID := chi.URLParam(r, "id")
	var req checklistItemUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректный JSON")
		return
	}
	userID := getUserIDFromContext(r)
	item, err := h.service.UpdateItem(r.Context(), ticketID, chi.URLParam(r, "itemID"), services.ChecklistItemUpdateInput{
		Title:       req.Title,
		IsDone:      req.IsDone,
		AssigneeIDs: req.AssigneeIDs,
	}, userID)
	if err != nil {
		h.respondError(w, r, err, "Не удалось изменить пункт чеклиста")
		return
	}
	h.publishChecklistUpdated(ticketID, userID, "Изменён чеклист")
	response.RespondWithJSON(w, http.StatusOK, item)
}

func (h *TicketChecklistHandler) DeleteItem(w http.ResponseWriter, r *http.Request) {
	ticketID := chi.URLParam(r, "id")
	userID := getUserIDFromContext(r)
	if err := h.service.DeleteItem(r.Context(), ticketID, chi.URLParam(r, "itemID"), userID); err != nil {
		h.respondError(w, r, err, "Не удалось удалить пункт чеклиста")
		return
	}
	h.publishChecklistUpdated(ticketID, userID, "Изменён чеклист")
	response.RespondWithJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *TicketChecklistHandler) Reorder(w http.ResponseWriter, r *http.Request) {
	ticketID := chi.URLParam(r, "id")
	var req checklistReorderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректный JSON")
		return
	}
	if err := h.service.Reorder(r.Context(), ticketID, req.ParentID, req.OrderedIDs); err != nil {
		h.respondError(w, r, err, "Не удалось изменить порядок пунктов чеклиста")
		return
	}
	h.publishChecklistUpdated(ticketID, getUserIDFromContext(r), "Изменён чеклист")
	response.RespondWithJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *TicketChecklistHandler) ApplyTemplate(w http.ResponseWriter, r *http.Request) {
	ticketID := chi.URLParam(r, "id")
	var req checklistApplyTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректный JSON")
		return
	}
	userID := getUserIDFromContext(r)
	items, err := h.service.ApplyTemplate(r.Context(), ticketID, req.TemplateID, userID)
	if err != nil {
		h.respondError(w, r, err, "Не удалось применить шаблон чеклиста")
		return
	}
	h.publishChecklistUpdated(ticketID, userID, "Применён шаблон чеклиста")
	response.RespondWithJSON(w, http.StatusCreated, items)
}

// ListTemplates возвращает шаблоны чеклистов. Параметр all=true доступен только администраторам.
func (h *TicketChecklistHandler) ListTemplates(w http.ResponseWriter, r *http.Request) {
	activeOnly := true
	if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("all")), "true") && hasRole(r.Context(), user.RoleAdmin) {
		activeOnly = false
	}
	items, err := h.service.ListTemplates(r.Context(), activeOnly)
	if err != nil {
		h.respondError(w, r, err, "Не удалось получить шаблоны чеклистов")
		return
	}
	response.RespondWithJSON(w, http.StatusOK, items)
}

func (h *TicketChecklistHandler) CreateTemplate(w http.ResponseWriter, r *http.Request) {
	var req checklistTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректный JSON")
		return
	}
	item, err := h.service.CreateTemplate(r.Context(), toChecklistTemplateInput(req), getUserIDFromContext(r))
	if err != nil {
		h.respondError(w, r, err, "Не удалось создать шаблон чеклиста")
		return
	}
	response.RespondWithJSON(w, http.StatusCreated, item)
}

func (h *TicketChecklistHandler) UpdateTemplate(w http.ResponseWriter, r *http.Request) {
	var req checklistTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректный JSON")
		return
	}
	item, err := h.service.UpdateTemplate(r.Context(), chi.URLParam(r, "templateID"), toChecklistTemplateInput(req))
	if err != nil {
		h.respondError(w, r, err, "Не удалось изменить шаблон чеклиста")
		return
	}
	response.RespondWithJSON(w, http.StatusOK, item)
}

func (h *TicketChecklistHandler) DeleteTemplate(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DeleteTemplate(r.Context(), chi.URLParam(r, "templateID")); err != nil {
		h.respondError(w, r, err, "Не удалось удалить шаблон чеклиста")
		return
	}
	response.RespondWithJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func toChecklistTemplateInput(req checklistTemplateRequest) services.ChecklistTemplateInput {
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	return services.ChecklistTemplateInput{
		Title:       req.Title,
		Description: req.Description,
		Items:       req.Items,
		IsActive:    isActive,
	}
}

func (h *TicketChecklistHandler) respondError(w http.ResponseWriter, r *http.Request, err error, message string) {
	switch {
	case errors.Is(err, services.ErrTicketNotFound),
		errors.Is(err, services.ErrChecklistItemNotFound),
		errors.Is(err, services.ErrChecklistTemplateNotFound):
		response.RespondWithError(w, http.StatusNotFound, message+": "+err.Error())
	case errors.Is(err, services.ErrChecklistValidation):
		response.RespondWithError(w, http.StatusBadRequest, message+": "+err.Error())
	default:
		middleware.GetLogger(r.Context()).Error(message, "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, message+": "+err.Error())
	}
}

func (h *TicketChecklistHandler) publishChecklistUpdated(ticketID string, actorUserID uint, message string) {
	if h.eventBus == nil || strings.TrimSpace(ticketID) == "" {
		return
	}
	var actor *uint
	if actorUserID > 0 {
		value := actorUserID
		actor = &value
	}
	h.eventBus.Publish(eventbus.Event{
		Type: events.TicketUpdated,
		Payload: events.TicketUpdatedPayload{
			TicketID:    ticketID,
			Action:      "ticket_checklist_updated",
			Source:      "ui",
			Message:     message,
			OccurredAt:  time.Now(),
			ActorUserID: actor,
		},
	})
}
