package handlers

import (
	"encoding/json"
	"etalon-server/internal/domain/workstation"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/middleware"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

type WSHandler struct {
	service workstation.Service
}

func NewWSHandler(service workstation.Service) *WSHandler {
	return &WSHandler{service: service}
}

func (h *WSHandler) RegisterRoutes(r chi.Router) {
	r.Route("/workstations", func(r chi.Router) {
		r.Get("/", h.List)
		r.Get("/{id}", h.Get)
		r.Post("/", h.Create)
		r.Put("/{id}", h.Update)
		r.Delete("/{id}", h.Delete)
	})
}

func (h *WSHandler) List(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	items, _, err := h.service.List(r.Context(), limit, offset)
	if err != nil {
		middleware.GetLogger(r.Context()).Error("list failed", "error", err)
		RespondWithError(w, http.StatusInternalServerError, "Internal Error")
		return
	}
	RespondWithJSON(w, http.StatusOK, api.PaginatedResponse{Data: items, Limit: limit, Offset: offset})
}

func (h *WSHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	item, err := h.service.Get(r.Context(), id)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "Internal Error")
		return
	}
	if item == nil {
		RespondWithError(w, http.StatusNotFound, "Not Found")
		return
	}
	RespondWithJSON(w, http.StatusOK, item)
}

func (h *WSHandler) Create(w http.ResponseWriter, r *http.Request) {
	var dto api.WorkstationCreateDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		RespondWithError(w, http.StatusBadRequest, "Invalid Body")
		return
	}
	item, err := h.service.Create(r.Context(), &dto)
	if err != nil {
		middleware.GetLogger(r.Context()).Error("create failed", "error", err)
		RespondWithError(w, http.StatusInternalServerError, "Creation Failed")
		return
	}
	RespondWithJSON(w, http.StatusCreated, item)
}

func (h *WSHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var data map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		RespondWithError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}
	err := h.service.Update(r.Context(), id, data)
	if err != nil {
		middleware.GetLogger(r.Context()).Error("update failed", "error", err)
		RespondWithError(w, http.StatusInternalServerError, "Update Failed")
		return
	}
	RespondWithJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *WSHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.service.Delete(r.Context(), id); err != nil {
		middleware.GetLogger(r.Context()).Error("delete failed", "error", err)
		RespondWithError(w, http.StatusInternalServerError, "Delete Failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
