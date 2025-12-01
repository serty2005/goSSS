package handlers

import (
	"encoding/json"
	"errors"
	"etalon-server/internal/domain"
	"etalon-server/internal/domain/company"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/middleware"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

type CompanyHandler struct {
	service company.Service
}

func NewCompanyHandler(service company.Service) *CompanyHandler {
	return &CompanyHandler{service: service}
}

func (h *CompanyHandler) RegisterRoutes(r chi.Router) {
	r.Route("/companies", func(r chi.Router) {
		r.Get("/", h.Search) // Добавим возможность поиска/листинга
		r.Get("/{id}", h.Get)
		r.Post("/", h.Create)
		r.Put("/{id}", h.Update)
		r.Delete("/{id}", h.Delete)
		r.Get("/{id}/infrastructure", h.GetInfrastructure)
	})
}

func (h *CompanyHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	comp, err := h.service.GetCompany(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			middleware.GetLogger(r.Context()).Error("не найдена запись", "error", err)
			RespondWithError(w, http.StatusNotFound, "Not Found")
			return
		}
		middleware.GetLogger(r.Context()).Error("get failed", "error", err)
		RespondWithError(w, http.StatusInternalServerError, "Internal Error")
		return
	}
	RespondWithJSON(w, http.StatusOK, comp)
}

func (h *CompanyHandler) Search(w http.ResponseWriter, r *http.Request) {
	term := r.URL.Query().Get("term")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	comps, err := h.service.SearchCompanies(r.Context(), term, limit, offset)
	if err != nil {
		middleware.GetLogger(r.Context()).Error("failed to search companies", "error", err)
		RespondWithError(w, http.StatusInternalServerError, "Internal Error")
		return
	}
	RespondWithJSON(w, http.StatusOK, comps)
}

func (h *CompanyHandler) Create(w http.ResponseWriter, r *http.Request) {
	var dto api.CompanyCreateDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	comp, err := h.service.CreateCompany(r.Context(), &dto)
	if err != nil {
		middleware.GetLogger(r.Context()).Error("failed to create company", "error", err)
		RespondWithError(w, http.StatusInternalServerError, "Creation failed")
		return
	}
	RespondWithJSON(w, http.StatusCreated, comp)
}

func (h *CompanyHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var data map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		RespondWithError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	err := h.service.UpdateCompany(r.Context(), id, data)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			middleware.GetLogger(r.Context()).Error("не найдена запись", "error", err)
			RespondWithError(w, http.StatusNotFound, "Not Found")
			return
		}
		middleware.GetLogger(r.Context()).Error("update failed", "error", err)
		RespondWithError(w, http.StatusInternalServerError, "Internal Error")
		return
	}
	RespondWithJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *CompanyHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	err := h.service.DeleteCompany(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			middleware.GetLogger(r.Context()).Error("не найдена запись", "error", err)
			RespondWithError(w, http.StatusNotFound, "Not Found")
			return
		}
		middleware.GetLogger(r.Context()).Error("delete failed", "error", err)
		RespondWithError(w, http.StatusInternalServerError, "Internal Error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetInfrastructure возвращает список оборудования для компании.
func (h *CompanyHandler) GetInfrastructure(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		RespondWithError(w, http.StatusBadRequest, "Company ID is required")
		return
	}

	items, err := h.service.GetInfrastructure(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			middleware.GetLogger(r.Context()).Warn("company not found for infrastructure request", "id", id)
			RespondWithError(w, http.StatusNotFound, "Company not found")
			return
		}
		middleware.GetLogger(r.Context()).Error("failed to get company infrastructure", "error", err)
		RespondWithError(w, http.StatusInternalServerError, "Internal Error")
		return
	}

	// Если оборудования нет, items будет пустым слайсом (инициализирован в сервисе),
	// json.Marshal сериализует его как [] (не null).
	RespondWithJSON(w, http.StatusOK, items)
}
