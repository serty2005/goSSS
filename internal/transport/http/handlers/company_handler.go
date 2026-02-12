package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"etalon-server/internal/contextkeys"
	"etalon-server/internal/domain"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/user"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/middleware"
	"etalon-server/internal/transport/http/response"
	"net/http"
	"strconv"
	"strings"

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
			response.RespondWithError(w, http.StatusNotFound, "Not Found")
			return
		}
		middleware.GetLogger(r.Context()).Error("get failed", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Internal Error")
		return
	}
	response.RespondWithJSON(w, http.StatusOK, toCompanyResponseDTO(*comp))
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
		response.RespondWithError(w, http.StatusInternalServerError, "Internal Error")
		return
	}
	items := make([]companyResponseDTO, 0, len(comps))
	for _, comp := range comps {
		items = append(items, toCompanyResponseDTO(comp))
	}
	response.RespondWithJSON(w, http.StatusOK, items)
}

func (h *CompanyHandler) Create(w http.ResponseWriter, r *http.Request) {
	var dto api.CompanyCreateDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	comp, err := h.service.CreateCompany(r.Context(), &dto)
	if err != nil {
		middleware.GetLogger(r.Context()).Error("failed to create company", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Creation failed")
		return
	}
	response.RespondWithJSON(w, http.StatusCreated, toCompanyResponseDTO(*comp))
}

func (h *CompanyHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var data map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	if hasRole(r.Context(), user.RoleSupportSpecialist) {
		if _, exists := data["active_contract"]; exists {
			response.RespondWithError(w, http.StatusForbidden, "Специалист не может менять статус контракта компании")
			return
		}
		if _, exists := data["contract_type"]; exists {
			response.RespondWithError(w, http.StatusForbidden, "Специалист не может менять тип контракта компании")
			return
		}
	}

	err := h.service.UpdateCompany(r.Context(), id, data)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			middleware.GetLogger(r.Context()).Error("не найдена запись", "error", err)
			response.RespondWithError(w, http.StatusNotFound, "Not Found")
			return
		}
		middleware.GetLogger(r.Context()).Error("update failed", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Internal Error")
		return
	}
	response.RespondWithJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func hasRole(ctx context.Context, roleName string) bool {
	roles, ok := ctx.Value(contextkeys.UserRolesContextKey).([]string)
	if !ok {
		return false
	}

	for _, role := range roles {
		if role == roleName {
			return true
		}
	}

	return false
}

func (h *CompanyHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	err := h.service.DeleteCompany(r.Context(), id)
	if err != nil {
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

// GetInfrastructure возвращает список оборудования для компании.
func (h *CompanyHandler) GetInfrastructure(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		response.RespondWithError(w, http.StatusBadRequest, "Company ID is required")
		return
	}

	items, err := h.service.GetInfrastructure(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			middleware.GetLogger(r.Context()).Warn("company not found for infrastructure request", "id", id)
			response.RespondWithError(w, http.StatusNotFound, "Company not found")
			return
		}
		middleware.GetLogger(r.Context()).Error("failed to get company infrastructure", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Internal Error")
		return
	}

	// Если оборудования нет, items будет пустым слайсом (инициализирован в сервисе),
	// json.Marshal сериализует его как [] (не null).
	response.RespondWithJSON(w, http.StatusOK, items)
}

type companyResponseDTO struct {
	ID               string  `json:"id"`
	Title            string  `json:"title"`
	Address          *string `json:"address,omitempty"`
	AdditionalName   *string `json:"additional_name,omitempty"`
	ActiveContract   *bool   `json:"active_contract,omitempty"`
	ParentID         *string `json:"parent_id,omitempty"`
	ParentTitle      *string `json:"parent_title,omitempty"`
	ContractID       *string `json:"contract_id,omitempty"`
	ContractType     *string `json:"contract_type,omitempty"`
	LastModifiedDate *string `json:"last_modified_date,omitempty"`
}

func toCompanyResponseDTO(comp company.Company) companyResponseDTO {
	var title string
	if comp.Title != nil {
		title = strings.TrimSpace(*comp.Title)
	}
	var lastModifiedDate *string
	if comp.LastModifiedDate != nil {
		formatted := comp.LastModifiedDate.Format("2006-01-02T15:04:05Z07:00")
		lastModifiedDate = &formatted
	}
	return companyResponseDTO{
		ID:               comp.ID,
		Title:            title,
		Address:          comp.Address,
		AdditionalName:   comp.AdditionalName,
		ActiveContract:   comp.ActiveContract,
		ParentID:         comp.ParentID,
		ParentTitle:      comp.ParentTitle,
		ContractID:       comp.ContractID,
		ContractType:     comp.ContractType,
		LastModifiedDate: lastModifiedDate,
	}
}
