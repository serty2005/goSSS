package handlers

import (
	"net/http"

	"etalon-server/internal/services"
	"etalon-server/internal/transport/http/response"

	"github.com/go-chi/chi/v5"
)

type BitrixHandler struct {
	service services.BitrixSyncService
}

func NewBitrixHandler(service services.BitrixSyncService) *BitrixHandler {
	return &BitrixHandler{service: service}
}

func (h *BitrixHandler) RegisterRoutes(r chi.Router) {
	r.Get("/service-points", h.ListServicePoints)
	r.Post("/service-points/refresh", h.RefreshServicePoints)
	r.Post("/sync/pull", h.PullSync)
}

func (h *BitrixHandler) ListServicePoints(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.ListServicePoints(r.Context())
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.RespondWithJSON(w, http.StatusOK, items)
}

func (h *BitrixHandler) RefreshServicePoints(w http.ResponseWriter, r *http.Request) {
	count, err := h.service.RefreshServicePoints(r.Context())
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"status": "ok",
		"count":  count,
	})
}

func (h *BitrixHandler) PullSync(w http.ResponseWriter, r *http.Request) {
	deals, comments, err := h.service.PullFromBitrix(r.Context())
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"status":            "ok",
		"deals_updated":     deals,
		"comments_imported": comments,
	})
}
