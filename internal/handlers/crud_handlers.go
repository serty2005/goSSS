package handlers

import (
	"encoding/json"
	"etalon-server/internal/api"
	"etalon-server/internal/models"
	"etalon-server/internal/repositories"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// CrudHandler обрабатывает CRUD-запросы.
type CrudHandler struct {
	logger          *zap.Logger
	db              *gorm.DB
	companyRepo     repositories.CompanyRepo
	serverRepo      repositories.ServerRepo
	workstationRepo repositories.WorkstationRepo
	frRepo          repositories.FiscalRegisterRepo
}

// NewCrudHandler создает новый экземпляр обработчика.
func NewCrudHandler(logger *zap.Logger, db *gorm.DB, companyRepo repositories.CompanyRepo, serverRepo repositories.ServerRepo, workstationRepo repositories.WorkstationRepo, frRepo repositories.FiscalRegisterRepo) *CrudHandler {
	return &CrudHandler{logger, db, companyRepo, serverRepo, workstationRepo, frRepo}
}

// RegisterRoutes регистрирует CRUD роуты.
func (h *CrudHandler) RegisterRoutes(r chi.Router) {
	r.Route("/companies", func(r chi.Router) {
		r.Get("/{uuid}", h.GetCompany)
		r.Post("/", h.CreateCompany)
		r.Put("/{uuid}", h.UpdateCompany)
		r.Delete("/{uuid}", h.DeleteCompany)
	})
	// Аналогичные роуты для других сущностей могут быть добавлены здесь
}

func (h *CrudHandler) GetCompany(w http.ResponseWriter, r *http.Request) {
	uuid := chi.URLParam(r, "uuid")
	company, err := h.companyRepo.GetByUUID(r.Context(), uuid)
	if err != nil {
		h.logger.Error("Failed to get company", zap.String("uuid", uuid), zap.Error(err))
		RespondWithError(w, http.StatusInternalServerError, "Failed to retrieve company")
		return
	}
	if company == nil {
		RespondWithError(w, http.StatusNotFound, "Company not found")
		return
	}
	RespondWithJSON(w, http.StatusOK, company)
}

func (h *CrudHandler) CreateCompany(w http.ResponseWriter, r *http.Request) {
	var dto api.CompanyCreateDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// TODO: Добавить валидацию DTO
	company := &models.Company{
		Title:                 dto.Title,
		Address:               dto.Address,
		AdditionalName:        dto.AdditionalName,
		ParentServiceDeskUUID: dto.ParentServiceDeskUUID,
	}
	company.MetaClass = "ou$company"

	err := h.db.Transaction(func(tx *gorm.DB) error {
		return h.companyRepo.Create(r.Context(), tx, company)
	})
	if err != nil {
		h.logger.Error("Failed to create company", zap.Error(err))
		RespondWithError(w, http.StatusInternalServerError, "Failed to create company")
		return
	}
	RespondWithJSON(w, http.StatusCreated, company)
}

func (h *CrudHandler) UpdateCompany(w http.ResponseWriter, r *http.Request) {
	uuid := chi.URLParam(r, "uuid")
	var updateData map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updateData); err != nil {
		RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	// Удаляем поля, которые не должны обновляться вручную через API
	delete(updateData, "uuid")
	delete(updateData, "id")
	delete(updateData, "meta_class")
	delete(updateData, "created_at")
	delete(updateData, "updated_at")
	delete(updateData, "deleted_at")

	var updated bool
	err := h.db.Transaction(func(tx *gorm.DB) error {
		var txErr error
		updated, txErr = h.companyRepo.Update(r.Context(), tx, uuid, updateData)
		return txErr
	})
	if err != nil {
		h.logger.Error("Failed to update company", zap.String("uuid", uuid), zap.Error(err))
		RespondWithError(w, http.StatusInternalServerError, "Failed to update company")
		return
	}
	if !updated {
		RespondWithError(w, http.StatusNotFound, "Company not found or no changes applied")
		return
	}
	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "company updated successfully"})
}

func (h *CrudHandler) DeleteCompany(w http.ResponseWriter, r *http.Request) {
	uuid := chi.URLParam(r, "uuid")
	var deleted bool
	err := h.db.Transaction(func(tx *gorm.DB) error {
		var txErr error
		deleted, txErr = h.companyRepo.Delete(r.Context(), tx, uuid)
		return txErr
	})

	if err != nil {
		h.logger.Error("Failed to delete company", zap.String("uuid", uuid), zap.Error(err))
		RespondWithError(w, http.StatusInternalServerError, "Failed to delete company")
		return
	}
	if !deleted {
		RespondWithError(w, http.StatusNotFound, "Company not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ИЗМЕНЕНИЕ: Вспомогательные функции respondWithError и respondWithJSON полностью удалены из этого файла.
