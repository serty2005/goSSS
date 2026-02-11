package handlers

import (
	"encoding/json"
	"errors"
	"etalon-server/internal/domain"
	"etalon-server/internal/domain/fiscal"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/middleware"
	"etalon-server/internal/transport/http/response"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

type FiscalHandler struct {
	service fiscal.Service
}

func NewFiscalHandler(service fiscal.Service) *FiscalHandler {
	return &FiscalHandler{service: service}
}

func (h *FiscalHandler) RegisterRoutes(r chi.Router) {
	r.Route("/fiscals", func(r chi.Router) {
		r.Get("/", h.List)
		r.Get("/{id}", h.Get)
		r.Post("/", h.Create)
		r.Put("/{id}", h.Update)
		r.Delete("/{id}", h.Delete)
	})
}

func (h *FiscalHandler) List(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	term := r.URL.Query().Get("term")
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	var (
		items []fiscal.FiscalRegister
		err   error
	)
	if term != "" {
		items, err = h.service.Search(r.Context(), term, limit, offset)
	} else {
		items, _, err = h.service.List(r.Context(), limit, offset)
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
	response.RespondWithJSON(w, http.StatusOK, api.PaginatedResponse{Data: items, Limit: limit, Offset: offset})
}

func (h *FiscalHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	item, err := h.service.Get(r.Context(), id)
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, "Internal Error")
		return
	}
	if item == nil {
		response.RespondWithError(w, http.StatusNotFound, "Not Found")
		return
	}
	response.RespondWithJSON(w, http.StatusOK, item)
}

func (h *FiscalHandler) Create(w http.ResponseWriter, r *http.Request) {
	var dto api.FiscalRegisterCreateDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Invalid Body")
		return
	}
	item, err := h.service.Create(r.Context(), &dto)
	if err != nil {
		middleware.GetLogger(r.Context()).Error("create failed", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Creation Failed")
		return
	}
	response.RespondWithJSON(w, http.StatusCreated, item)
}

func (h *FiscalHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var data map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}
	err := h.service.Update(r.Context(), id, data)
	if err != nil {
		middleware.GetLogger(r.Context()).Error("update failed", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Update Failed")
		return
	}
	response.RespondWithJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *FiscalHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.service.Delete(r.Context(), id); err != nil {
		middleware.GetLogger(r.Context()).Error("delete failed", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Delete Failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
