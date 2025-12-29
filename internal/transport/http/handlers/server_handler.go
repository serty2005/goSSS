package handlers

import (
	"encoding/json"
	"errors"
	"etalon-server/internal/domain"
	"etalon-server/internal/domain/server"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/middleware"
	"etalon-server/internal/transport/http/response"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

type ServerHandler struct {
	service server.Service
}

func NewServerHandler(service server.Service) *ServerHandler {
	return &ServerHandler{service: service}
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
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	items, _, err := h.service.List(r.Context(), limit, offset)
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
	response.RespondWithJSON(w, http.StatusOK, item)
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
	response.RespondWithJSON(w, http.StatusCreated, item)
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
