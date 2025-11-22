package handlers

import (
	"encoding/json"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/workstation"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/middleware"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"
)

// CrudHandler обрабатывает CRUD-запросы.
type CrudHandler struct {
	db              *gorm.DB
	companyRepo     company.Repository
	companyService  company.Service
	serverRepo      server.Repository
	workstationRepo workstation.Repository
	frRepo          fiscal.Repository
}

// NewCrudHandler создает новый экземпляр обработчика.
func NewCrudHandler(db *gorm.DB, companyRepo company.Repository, companyService company.Service, serverRepo server.Repository, workstationRepo workstation.Repository, frRepo fiscal.Repository) *CrudHandler {
	return &CrudHandler{db, companyRepo, companyService, serverRepo, workstationRepo, frRepo}
}

// RegisterRoutes регистрирует CRUD роуты.
func (h *CrudHandler) RegisterRoutes(r chi.Router) {
	r.Route("/companies", func(r chi.Router) {
		r.Get("/{id}", h.GetCompany)
		r.Post("/", h.CreateCompany)
		r.Put("/{id}", h.UpdateCompany)
		r.Delete("/{id}", h.DeleteCompany)
	})

	r.Route("/servers", func(r chi.Router) {
		r.Get("/", h.GetServersList) // Список с пагинацией
		r.Get("/{id}", h.GetServer)
		r.Post("/", h.CreateServer)
		r.Put("/{id}", h.UpdateServer)
		r.Delete("/{id}", h.DeleteServer)
	})

	r.Route("/workstations", func(r chi.Router) {
		r.Get("/", h.GetWorkstationsList) // Список с пагинацией
		r.Get("/{id}", h.GetWorkstation)
		r.Post("/", h.CreateWorkstation)
		r.Put("/{id}", h.UpdateWorkstation)
		r.Delete("/{id}", h.DeleteWorkstation)
	})

	r.Route("/fiscal-registers", func(r chi.Router) {
		r.Get("/", h.GetFiscalRegistersList) // Список с пагинацией
		r.Get("/{id}", h.GetFiscalRegister)
		r.Post("/", h.CreateFiscalRegister)
		r.Put("/{id}", h.UpdateFiscalRegister)
		r.Delete("/{id}", h.DeleteFiscalRegister)
	})
}

func (h *CrudHandler) GetCompany(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	id := chi.URLParam(r, "id")
	company, err := h.companyRepo.GetByID(r.Context(), id)
	if err != nil {
		log.Error("Failed to get company", "id", id, "error", err)
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
	log := middleware.GetLogger(r.Context())
	var dto api.CompanyCreateDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	company := &company.Company{
		Title:          dto.Title,
		Address:        dto.Address,
		AdditionalName: dto.AdditionalName,
		// ParentID: dto.ParentID // Предполагаем, что DTO тоже будет обновлен для передачи ParentID
	}
	company.MetaClass = "ou$company"

	err := h.db.Transaction(func(tx *gorm.DB) error {
		return h.companyRepo.Create(r.Context(), company)
	})
	if err != nil {
		log.Error("Failed to create company", "error", err)
		RespondWithError(w, http.StatusInternalServerError, "Failed to create company")
		return
	}
	RespondWithJSON(w, http.StatusCreated, company)
}

func (h *CrudHandler) UpdateCompany(w http.ResponseWriter, r *http.Request) {
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
		updated, txErr = h.companyRepo.Update(r.Context(), id, updateData)
		return txErr
	})
	if err != nil {
		log.Error("Failed to update company", "id", id, "error", err)
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
	log := middleware.GetLogger(r.Context())
	id := chi.URLParam(r, "id")
	var deleted bool
	err := h.db.Transaction(func(tx *gorm.DB) error {
		var txErr error
		deleted, txErr = h.companyRepo.Delete(r.Context(), id)
		return txErr
	})

	if err != nil {
		log.Error("Failed to delete company", "id", id, "error", err)
		RespondWithError(w, http.StatusInternalServerError, "Failed to delete company")
		return
	}
	if !deleted {
		RespondWithError(w, http.StatusNotFound, "Company not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// === СЕРВЕРЫ ===

func (h *CrudHandler) GetServersList(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	// Параметры пагинации
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 50
	}
	offset, err := strconv.Atoi(offsetStr)
	if err != nil || offset < 0 {
		offset = 0
	}

	// Получаем общее количество
	var total int64
	h.db.Model(&server.Server{}).Count(&total)

	// Получаем данные с пагинацией
	var servers []server.Server
	err = h.db.Limit(limit).Offset(offset).Order("created_at desc").Find(&servers).Error
	if err != nil {
		log.Error("Failed to get servers", "error", err)
		RespondWithError(w, http.StatusInternalServerError, "Failed to retrieve servers")
		return
	}

	response := api.PaginatedResponse{
		Data:    servers,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
		HasNext: int64(offset+limit) < total,
		HasPrev: offset > 0,
	}

	RespondWithJSON(w, http.StatusOK, response)
}

func (h *CrudHandler) GetServer(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	id := chi.URLParam(r, "id")
	server, err := h.serverRepo.GetByID(r.Context(), id)
	if err != nil {
		log.Error("Failed to get server", "id", id, "error", err)
		RespondWithError(w, http.StatusInternalServerError, "Failed to retrieve server")
		return
	}
	if server == nil {
		RespondWithError(w, http.StatusNotFound, "Server not found")
		return
	}
	RespondWithJSON(w, http.StatusOK, server)
}

func (h *CrudHandler) CreateServer(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	var dto api.ServerCreateDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	server := &server.Server{
		UniqueID:      dto.UniqueID,
		CRMid:         dto.CRMid,
		Teamviewer:    dto.Teamviewer,
		RDP:           dto.RDP,
		Anydesk:       dto.Anydesk,
		IP:            dto.IP,
		DeviceName:    dto.DeviceName,
		ServerName:    dto.ServerName,
		ServerVersion: dto.ServerVersion,
		Description:   dto.Description,
		OwnerID:       dto.OwnerID,
		Status:        "unknown",
	}
	server.MetaClass = "objectBase$Server"

	err := h.db.Transaction(func(tx *gorm.DB) error {
		return h.serverRepo.Create(r.Context(), tx, server)
	})
	if err != nil {
		log.Error("Failed to create server", "error", err)
		RespondWithError(w, http.StatusInternalServerError, "Failed to create server")
		return
	}
	RespondWithJSON(w, http.StatusCreated, server)
}

func (h *CrudHandler) UpdateServer(w http.ResponseWriter, r *http.Request) {
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
		updated, txErr = h.serverRepo.Update(r.Context(), tx, id, updateData)
		return txErr
	})
	if err != nil {
		log.Error("Failed to update server", "id", id, "error", err)
		RespondWithError(w, http.StatusInternalServerError, "Failed to update server")
		return
	}
	if !updated {
		RespondWithError(w, http.StatusNotFound, "Server not found or no changes applied")
		return
	}
	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "server updated successfully"})
}

func (h *CrudHandler) DeleteServer(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	id := chi.URLParam(r, "id")
	var deleted bool
	err := h.db.Transaction(func(tx *gorm.DB) error {
		var txErr error
		deleted, txErr = h.serverRepo.Delete(r.Context(), tx, id)
		return txErr
	})

	if err != nil {
		log.Error("Failed to delete server", "id", id, "error", err)
		RespondWithError(w, http.StatusInternalServerError, "Failed to delete server")
		return
	}
	if !deleted {
		RespondWithError(w, http.StatusNotFound, "Server not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *CrudHandler) GetWorkstationsList(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	// Параметры пагинации
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 50
	}
	offset, err := strconv.Atoi(offsetStr)
	if err != nil || offset < 0 {
		offset = 0
	}

	// Получаем общее количество
	var total int64
	h.db.Model(&workstation.Workstation{}).Count(&total)

	// Получаем данные с пагинацией
	var workstations []workstation.Workstation
	err = h.db.Limit(limit).Offset(offset).Order("created_at desc").Find(&workstations).Error
	if err != nil {
		log.Error("Failed to get workstations", "error", err)
		RespondWithError(w, http.StatusInternalServerError, "Failed to retrieve workstations")
		return
	}

	response := api.PaginatedResponse{
		Data:    workstations,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
		HasNext: int64(offset+limit) < total,
		HasPrev: offset > 0,
	}

	RespondWithJSON(w, http.StatusOK, response)
}

// === WORKSTATION CRUD METHODS ===

func (h *CrudHandler) GetWorkstation(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	id := chi.URLParam(r, "id")
	workstation, err := h.workstationRepo.GetByID(r.Context(), id)
	if err != nil {
		log.Error("Failed to get workstation", "id", id, "error", err)
		RespondWithError(w, http.StatusInternalServerError, "Failed to retrieve workstation")
		return
	}
	if workstation == nil {
		RespondWithError(w, http.StatusNotFound, "Workstation not found")
		return
	}
	RespondWithJSON(w, http.StatusOK, workstation)
}

func (h *CrudHandler) CreateWorkstation(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	var dto api.WorkstationCreateDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	workstation := &workstation.Workstation{
		Teamviewer:   dto.Teamviewer,
		Anydesk:      dto.Anydesk,
		Litemanager:  dto.Litemanager,
		DeviceName:   dto.DeviceName,
		Description:  dto.Description,
		OwnerID:      dto.OwnerID,
		HealthStatus: "ok",
	}
	workstation.MetaClass = "objectBase$Workstation"

	err := h.db.Transaction(func(tx *gorm.DB) error {
		return h.workstationRepo.Create(r.Context(), tx, workstation)
	})
	if err != nil {
		log.Error("Failed to create workstation", "error", err)
		RespondWithError(w, http.StatusInternalServerError, "Failed to create workstation")
		return
	}
	RespondWithJSON(w, http.StatusCreated, workstation)
}

func (h *CrudHandler) UpdateWorkstation(w http.ResponseWriter, r *http.Request) {
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
		updated, txErr = h.workstationRepo.Update(r.Context(), tx, id, updateData)
		return txErr
	})
	if err != nil {
		log.Error("Failed to update workstation", "id", id, "error", err)
		RespondWithError(w, http.StatusInternalServerError, "Failed to update workstation")
		return
	}
	if !updated {
		RespondWithError(w, http.StatusNotFound, "Workstation not found or no changes applied")
		return
	}
	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "workstation updated successfully"})
}

func (h *CrudHandler) DeleteWorkstation(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	id := chi.URLParam(r, "id")
	var deleted bool
	err := h.db.Transaction(func(tx *gorm.DB) error {
		var txErr error
		deleted, txErr = h.workstationRepo.Delete(r.Context(), tx, id)
		return txErr
	})

	if err != nil {
		log.Error("Failed to delete workstation", "id", id, "error", err)
		RespondWithError(w, http.StatusInternalServerError, "Failed to delete workstation")
		return
	}
	if !deleted {
		RespondWithError(w, http.StatusNotFound, "Workstation not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *CrudHandler) GetFiscalRegistersList(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	// Параметры пагинации
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 50
	}
	offset, err := strconv.Atoi(offsetStr)
	if err != nil || offset < 0 {
		offset = 0
	}

	// Получаем общее количество
	var total int64
	h.db.Model(&fiscal.FiscalRegister{}).Count(&total)

	// Получаем данные с пагинацией
	var fiscalRegisters []fiscal.FiscalRegister
	err = h.db.Limit(limit).Offset(offset).Order("created_at desc").Find(&fiscalRegisters).Error
	if err != nil {
		log.Error("Failed to get fiscal registers", "error", err)
		RespondWithError(w, http.StatusInternalServerError, "Failed to retrieve fiscal registers")
		return
	}

	response := api.PaginatedResponse{
		Data:    fiscalRegisters,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
		HasNext: int64(offset+limit) < total,
		HasPrev: offset > 0,
	}

	RespondWithJSON(w, http.StatusOK, response)
}

// === FISCAL REGISTER CRUD METHODS ===

func (h *CrudHandler) GetFiscalRegister(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	id := chi.URLParam(r, "id")
	fr, err := h.frRepo.GetByID(r.Context(), id)
	if err != nil {
		log.Error("Failed to get fiscal register", "id", id, "error", err)
		RespondWithError(w, http.StatusInternalServerError, "Failed to retrieve fiscal register")
		return
	}
	if fr == nil {
		RespondWithError(w, http.StatusNotFound, "Fiscal register not found")
		return
	}
	RespondWithJSON(w, http.StatusOK, fr)
}

func (h *CrudHandler) CreateFiscalRegister(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	var dto api.FiscalRegisterCreateDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	fr := &fiscal.FiscalRegister{
		ModelKKT:       dto.ModelKKT,
		RNKKT:          dto.RNKKT,
		INN:            dto.INN,
		FRSerialNumber: dto.FRSerialNumber,
		FNNumber:       dto.FNNumber,
		FRDownloader:   dto.FRDownloader,
		FRFirmware:     dto.FRFirmware,
		DriverVersion:  dto.DriverVersion,
		OwnerID:        dto.OwnerID,
		HealthStatus:   "ok",
	}
	fr.MetaClass = "objectBase$FR"

	err := h.db.Transaction(func(tx *gorm.DB) error {
		return h.frRepo.Create(r.Context(), tx, fr)
	})
	if err != nil {
		log.Error("Failed to create fiscal register", "error", err)
		RespondWithError(w, http.StatusInternalServerError, "Failed to create fiscal register")
		return
	}
	RespondWithJSON(w, http.StatusCreated, fr)
}

func (h *CrudHandler) UpdateFiscalRegister(w http.ResponseWriter, r *http.Request) {
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
		updated, txErr = h.frRepo.Update(r.Context(), tx, id, updateData)
		return txErr
	})
	if err != nil {
		log.Error("Failed to update fiscal register", "id", id, "error", err)
		RespondWithError(w, http.StatusInternalServerError, "Failed to update fiscal register")
		return
	}
	if !updated {
		RespondWithError(w, http.StatusNotFound, "Fiscal register not found or no changes applied")
		return
	}
	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "fiscal register updated successfully"})
}

func (h *CrudHandler) DeleteFiscalRegister(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	id := chi.URLParam(r, "id")
	var deleted bool
	err := h.db.Transaction(func(tx *gorm.DB) error {
		var txErr error
		deleted, txErr = h.frRepo.Delete(r.Context(), tx, id)
		return txErr
	})

	if err != nil {
		log.Error("Failed to delete fiscal register", "id", id, "error", err)
		RespondWithError(w, http.StatusInternalServerError, "Failed to delete fiscal register")
		return
	}
	if !deleted {
		RespondWithError(w, http.StatusNotFound, "Fiscal register not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
