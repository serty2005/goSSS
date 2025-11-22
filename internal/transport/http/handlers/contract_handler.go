package handlers

import (
	"encoding/json"
	"etalon-server/internal/domain/contract"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/middleware"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// ContractHandler обрабатывает HTTP-запросы для контрактов.
type ContractHandler struct {
	service contract.Service
}

// NewContractHandler создает новый экземпляр обработчика.
// Обрати внимание: мы убрали *gorm.DB и Repository из зависимостей.
func NewContractHandler(service contract.Service) *ContractHandler {
	return &ContractHandler{service: service}
}

// RegisterRoutes регистрирует роуты для контрактов.
func (h *ContractHandler) RegisterRoutes(r chi.Router) {
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

	contract, err := h.service.GetContract(r.Context(), id)
	if err != nil {
		log.Error("Failed to get contract", "id", id, "error", err)
		RespondWithError(w, http.StatusInternalServerError, "Failed to retrieve contract")
		return
	}
	if contract == nil {
		RespondWithError(w, http.StatusNotFound, "Contract not found")
		return
	}
	RespondWithJSON(w, http.StatusOK, contract)
}

func (h *ContractHandler) CreateContract(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	var dto api.ContractCreateDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Делегируем всю логику сервису
	contract, err := h.service.CreateContract(r.Context(), &dto)
	if err != nil {
		log.Error("Failed to create contract", "error", err)
		RespondWithError(w, http.StatusInternalServerError, "Failed to create contract")
		return
	}
	RespondWithJSON(w, http.StatusCreated, contract)
}

func (h *ContractHandler) UpdateContract(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	id := chi.URLParam(r, "id")
	var updateData map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updateData); err != nil {
		RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	err := h.service.UpdateContract(r.Context(), id, updateData)
	if err != nil {
		log.Error("Failed to update contract", "id", id, "error", err)
		// Здесь можно улучшить обработку ошибок (например, различать 404 и 500)
		if err.Error() == "contract not found" {
			RespondWithError(w, http.StatusNotFound, "Contract not found")
		} else {
			RespondWithError(w, http.StatusInternalServerError, "Failed to update contract")
		}
		return
	}
	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "contract updated successfully"})
}

func (h *ContractHandler) DeleteContract(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	id := chi.URLParam(r, "id")

	err := h.service.DeleteContract(r.Context(), id)
	if err != nil {
		log.Error("Failed to delete contract", "id", id, "error", err)
		if err.Error() == "contract not found" {
			RespondWithError(w, http.StatusNotFound, "Contract not found")
		} else {
			RespondWithError(w, http.StatusInternalServerError, "Failed to delete contract")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
