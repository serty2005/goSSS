package handlers

import (
	"encoding/json"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/repositories"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/middleware"
	"net/http"

	"github.com/go-chi/chi/v5"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// ContractHandler обрабатывает CRUD-запросы для контрактов.
type ContractHandler struct {
	db           *gorm.DB
	contractRepo repositories.ContractRepo
}

// NewContractHandler создает новый экземпляр обработчика контрактов.
func NewContractHandler(db *gorm.DB, contractRepo repositories.ContractRepo) *ContractHandler {
	return &ContractHandler{db, contractRepo}
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
	contract, err := h.contractRepo.GetByID(r.Context(), id)
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

	// Конвертируем map в datatypes.JSON
	servicesJSON, _ := json.Marshal(dto.Services)
	recipientsJSON, _ := json.Marshal(dto.Recipients)

	contract := &models.Contract{
		State:          dto.State,
		StateStartTime: dto.StateStartTime,
		Services:       datatypes.JSON(servicesJSON),
		Recipients:     datatypes.JSON(recipientsJSON),
		ServiceLevel:   dto.ServiceLevel,
	}
	contract.MetaClass = "agreement$agreement"

	err := h.db.Transaction(func(tx *gorm.DB) error {
		// Создаем контракт
		if err := h.contractRepo.Create(r.Context(), tx, contract); err != nil {
			return err
		}

		// Создаем связи с компаниями
		if len(dto.CompanyIDs) > 0 {
			for _, companyID := range dto.CompanyIDs {
				link := models.CompanyContract{
					CompanyID:  companyID,
					ContractID: contract.ID,
				}
				if err := tx.Create(&link).Error; err != nil {
					return err
				}
			}
		}

		return nil
	})

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

	// Удаляем поля, которые не должны обновляться вручную через API
	delete(updateData, "id")
	delete(updateData, "meta_class")
	delete(updateData, "created_at")
	delete(updateData, "updated_at")
	delete(updateData, "deleted_at")

	var updated bool
	err := h.db.Transaction(func(tx *gorm.DB) error {
		var txErr error
		updated, txErr = h.contractRepo.Update(r.Context(), tx, id, updateData)
		return txErr
	})
	if err != nil {
		log.Error("Failed to update contract", "id", id, "error", err)
		RespondWithError(w, http.StatusInternalServerError, "Failed to update contract")
		return
	}
	if !updated {
		RespondWithError(w, http.StatusNotFound, "Contract not found or no changes applied")
		return
	}
	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "contract updated successfully"})
}

func (h *ContractHandler) DeleteContract(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	id := chi.URLParam(r, "id")
	var deleted bool
	err := h.db.Transaction(func(tx *gorm.DB) error {
		var txErr error
		deleted, txErr = h.contractRepo.Delete(r.Context(), tx, id)
		return txErr
	})

	if err != nil {
		log.Error("Failed to delete contract", "id", id, "error", err)
		RespondWithError(w, http.StatusInternalServerError, "Failed to delete contract")
		return
	}
	if !deleted {
		RespondWithError(w, http.StatusNotFound, "Contract not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
