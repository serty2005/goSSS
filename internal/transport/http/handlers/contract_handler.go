package handlers

import (
	"encoding/json"
	"errors"
	"etalon-server/internal/domain"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/contract"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/middleware"
	"etalon-server/internal/transport/http/response"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// ContractHandler обрабатывает HTTP-запросы для контрактов.
type ContractHandler struct {
	service contract.Service
}

// NewContractHandler создает новый экземпляр обработчика.
func NewContractHandler(service contract.Service) *ContractHandler {
	return &ContractHandler{service: service}
}

// RegisterRoutes регистрирует роуты для контрактов.
func (h *ContractHandler) RegisterRoutes(r chi.Router) {
	r.Get("/contracts", h.ListCompanyContracts)
	r.Route("/contracts", func(r chi.Router) {
		r.Get("/{id}", h.GetContract)
		r.Post("/", h.CreateContract)
		r.Put("/{id}", h.UpdateContract)
		r.Delete("/{id}", h.DeleteContract)
	})
}

func (h *ContractHandler) GetContract(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	id := chi.URLParam(r, "id")

	contractModel, err := h.service.GetContract(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			log.Error("не найдена запись", "error", err)
			response.RespondWithError(w, http.StatusNotFound, "Not Found")
			return
		}
		log.Error("get failed", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Internal Error")
		return
	}

	dto := toContractResponseDTO(*contractModel)

	response.RespondWithJSON(w, http.StatusOK, dto)
}

func (h *ContractHandler) ListCompanyContracts(w http.ResponseWriter, r *http.Request) {
	companyID := strings.TrimSpace(r.URL.Query().Get("company_id"))
	if companyID == "" {
		response.RespondWithError(w, http.StatusBadRequest, "Не указан идентификатор компании")
		return
	}

	items, err := h.service.ListCompanyContracts(r.Context(), companyID)
	if err != nil {
		middleware.GetLogger(r.Context()).Error("Не удалось получить историю контрактов компании", "company_id", companyID, "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось получить историю контрактов компании")
		return
	}

	dtos := make([]api.ContractResponseDTO, 0, len(items))
	for _, item := range items {
		dtos = append(dtos, toContractResponseDTO(item))
	}
	response.RespondWithJSON(w, http.StatusOK, dtos)
}

func toContractResponseDTO(contractModel contract.Contract) api.ContractResponseDTO {
	return api.ContractResponseDTO{
		ID:               contractModel.ID,
		State:            contractModel.State,
		StateStartTime:   contractModel.StateStartTime,
		LastModifiedDate: contractModel.LastModifiedDate,
		CreatedAt:        contractModel.CreatedAt,
		UpdatedAt:        contractModel.UpdatedAt,
		Services:         parseContractServices(contractModel.Services),
		Recipients:       parseContractRecipients(contractModel.Recipients),
		Companies:        toContractCompaniesDTO(contractModel.Companies),
		ServiceLevel:     contractModel.ServiceLevel,
	}
}

func parseContractServices(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}

	var stringSlice []string
	if err := json.Unmarshal(raw, &stringSlice); err == nil {
		return stringSlice
	}

	var interfaceSlice []interface{}
	if err := json.Unmarshal(raw, &interfaceSlice); err == nil {
		result := make([]string, 0, len(interfaceSlice))
		for _, item := range interfaceSlice {
			if item == nil {
				continue
			}
			result = append(result, fmt.Sprint(item))
		}
		return result
	}

	var keyedServices map[string]interface{}
	if err := json.Unmarshal(raw, &keyedServices); err == nil {
		result := make([]string, 0, len(keyedServices))
		for _, value := range keyedServices {
			if value == nil {
				continue
			}
			result = append(result, fmt.Sprint(value))
		}
		return result
	}

	var singleValue string
	if err := json.Unmarshal(raw, &singleValue); err == nil && singleValue != "" {
		return []string{singleValue}
	}

	return nil
}

func parseContractRecipients(raw []byte) []string {
	return parseContractServices(raw)
}

func toContractCompaniesDTO(items []company.Company) []api.ContractCompanyDTO {
	result := make([]api.ContractCompanyDTO, 0, len(items))
	for _, item := range items {
		title := ""
		if item.Title != nil {
			title = strings.TrimSpace(*item.Title)
		}
		if title == "" {
			title = item.ID
		}
		result = append(result, api.ContractCompanyDTO{
			ID:    item.ID,
			Title: title,
		})
	}
	return result
}

func (h *ContractHandler) CreateContract(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	var dto api.ContractCreateDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	contractModel, err := h.service.CreateContract(r.Context(), &dto)
	if err != nil {
		log.Error("Failed to create contract", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Failed to create contract")
		return
	}
	response.RespondWithJSON(w, http.StatusCreated, contractModel)
}

func (h *ContractHandler) UpdateContract(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	id := chi.URLParam(r, "id")
	var updateData map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updateData); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	err := h.service.UpdateContract(r.Context(), id, updateData)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			log.Error("не найдена запись", "error", err)
			response.RespondWithError(w, http.StatusNotFound, "Not Found")
			return
		}
		log.Error("update failed", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Internal Error")
		return
	}
	response.RespondWithJSON(w, http.StatusOK, map[string]string{"message": "contract updated successfully"})
}

func (h *ContractHandler) DeleteContract(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	id := chi.URLParam(r, "id")

	err := h.service.DeleteContract(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			log.Error("не найдена запись", "error", err)
			response.RespondWithError(w, http.StatusNotFound, "Not Found")
			return
		}
		log.Error("delete failed", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Internal Error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
