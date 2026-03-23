package handlers

import (
	"errors"
	"etalon-server/internal/services"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/middleware"
	"etalon-server/internal/transport/http/response"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

type IntegrationSyncHandler struct {
	service services.IntegrationSyncControlService
}

func NewIntegrationSyncHandler(service services.IntegrationSyncControlService) *IntegrationSyncHandler {
	return &IntegrationSyncHandler{service: service}
}

func (h *IntegrationSyncHandler) RegisterRoutes(r chi.Router) {
	r.Route("/{provider}/sync", func(r chi.Router) {
		r.Get("/incoming-events", h.ListIncomingEvents)
		r.Get("/outgoing-events", h.ListOutgoingEvents)
		r.Get("/incoming-events/{id}", h.GetIncomingEvent)
		r.Get("/outgoing-events/{id}", h.GetOutgoingEvent)
		r.Post("/incoming-events/{id}/replay", h.ReplayIncomingEvent)
	})
}

func (h *IntegrationSyncHandler) ListIncomingEvents(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.service == nil {
		response.RespondWithError(w, http.StatusServiceUnavailable, "контур контроля синхронизации интеграций недоступен")
		return
	}
	provider := chi.URLParam(r, "provider")
	filter := services.IntegrationSyncEventListFilter{
		Status: parseStringCSV(r.URL.Query().Get("status")),
		Limit:  parseQueryInt(r, "limit", 50),
		Offset: parseQueryInt(r, "offset", 0),
	}
	items, total, err := h.service.ListIncomingEvents(r.Context(), provider, filter)
	if err != nil {
		h.respondSyncError(w, r, err)
		return
	}
	response.RespondWithJSON(w, http.StatusOK, api.PaginatedResponse{
		Data:    items,
		Total:   total,
		Limit:   filter.Limit,
		Offset:  filter.Offset,
		HasNext: int64(filter.Offset+len(items)) < total,
		HasPrev: filter.Offset > 0,
	})
}

func (h *IntegrationSyncHandler) ListOutgoingEvents(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.service == nil {
		response.RespondWithError(w, http.StatusServiceUnavailable, "контур контроля синхронизации интеграций недоступен")
		return
	}
	provider := chi.URLParam(r, "provider")
	filter := services.IntegrationSyncEventListFilter{
		Status: parseStringCSV(r.URL.Query().Get("status")),
		Limit:  parseQueryInt(r, "limit", 50),
		Offset: parseQueryInt(r, "offset", 0),
	}
	items, total, err := h.service.ListOutgoingEvents(r.Context(), provider, filter)
	if err != nil {
		h.respondSyncError(w, r, err)
		return
	}
	response.RespondWithJSON(w, http.StatusOK, api.PaginatedResponse{
		Data:    items,
		Total:   total,
		Limit:   filter.Limit,
		Offset:  filter.Offset,
		HasNext: int64(filter.Offset+len(items)) < total,
		HasPrev: filter.Offset > 0,
	})
}

func (h *IntegrationSyncHandler) GetIncomingEvent(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.service == nil {
		response.RespondWithError(w, http.StatusServiceUnavailable, "контур контроля синхронизации интеграций недоступен")
		return
	}
	item, err := h.service.GetIncomingEvent(r.Context(), chi.URLParam(r, "provider"), chi.URLParam(r, "id"))
	if err != nil {
		h.respondSyncError(w, r, err)
		return
	}
	response.RespondWithJSON(w, http.StatusOK, item)
}

func (h *IntegrationSyncHandler) GetOutgoingEvent(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.service == nil {
		response.RespondWithError(w, http.StatusServiceUnavailable, "контур контроля синхронизации интеграций недоступен")
		return
	}
	item, err := h.service.GetOutgoingEvent(r.Context(), chi.URLParam(r, "provider"), chi.URLParam(r, "id"))
	if err != nil {
		h.respondSyncError(w, r, err)
		return
	}
	response.RespondWithJSON(w, http.StatusOK, item)
}

func (h *IntegrationSyncHandler) ReplayIncomingEvent(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.service == nil {
		response.RespondWithError(w, http.StatusServiceUnavailable, "контур контроля синхронизации интеграций недоступен")
		return
	}
	if err := h.service.ReplayIncomingEvent(r.Context(), chi.URLParam(r, "provider"), chi.URLParam(r, "id")); err != nil {
		h.respondSyncError(w, r, err)
		return
	}
	response.RespondWithJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (h *IntegrationSyncHandler) respondSyncError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, services.ErrIntegrationProviderNotSupported):
		status = http.StatusNotFound
	case errors.Is(err, services.ErrIntegrationEventNotFound):
		status = http.StatusNotFound
	}
	middleware.GetLogger(r.Context()).Error("ошибка административного контура синхронизации интеграций", "error", err)
	response.RespondWithError(w, status, err.Error())
}

func parseQueryInt(r *http.Request, key string, fallback int) int {
	value := strings.TrimSpace(r.URL.Query().Get(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	if parsed < 0 {
		return 0
	}
	return parsed
}
