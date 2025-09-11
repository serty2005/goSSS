package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"etalon-server/internal/core/workers"
	"etalon-server/internal/domain"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/repositories"
	"etalon-server/internal/pkg/utils"
	"etalon-server/internal/services"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/middleware"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"
)

// TaskHandler обрабатывает запросы, связанные с задачами сверки и поиском дубликатов.
type TaskHandler struct {
	db              *gorm.DB
	resolutionSvc   services.TaskResolutionService
	sdEditor        workers.SDEditorWorker
	serverRepo      repositories.ServerRepo
	workstationRepo repositories.WorkstationRepo
	frRepo          repositories.FiscalRegisterRepo
	linkRepo        repositories.LinkRepo
}

// NewTaskHandler создает новый экземпляр обработчика.
func NewTaskHandler(
	db *gorm.DB,
	resolutionSvc services.TaskResolutionService,
	sdEditor workers.SDEditorWorker,
	serverRepo repositories.ServerRepo,
	workstationRepo repositories.WorkstationRepo,
	frRepo repositories.FiscalRegisterRepo,
	linkRepo repositories.LinkRepo,
) *TaskHandler {
	return &TaskHandler{
		db:              db,
		resolutionSvc:   resolutionSvc,
		sdEditor:        sdEditor,
		serverRepo:      serverRepo,
		workstationRepo: workstationRepo,
		frRepo:          frRepo,
		linkRepo:        linkRepo,
	}
}

// RegisterRoutes регистрирует роуты для задач и дубликатов.
func (h *TaskHandler) RegisterRoutes(r chi.Router) {
	r.Get("/tasks", h.GetTasks)
	r.Post("/tasks/{id}/resolve", h.ResolveTask)
	r.Post("/tasks/{id}/create-entity-in-sd", h.createEntityFromTask)
	r.Get("/duplicates", h.GetDuplicates)
}

// GetTasks возвращает список задач сверки с фильтрацией и пагинацией.
func (h *TaskHandler) GetTasks(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	status := r.URL.Query().Get("status")
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

	var tasks []models.ReconciliationTask
	query := h.db.Model(&models.ReconciliationTask{})
	if status != "" {
		query = query.Where("status = ?", status)
	}
	err = query.Limit(limit).Offset(offset).Order("created_at desc").Find(&tasks).Error
	if err != nil {
		log.Error("Не удалось получить задачи из БД", "error", err)
		middleware.RespondWithError(w, http.StatusInternalServerError, "Ошибка получения списка задач")
		return
	}

	taskDTOs := make([]api.TaskDTO, 0, len(tasks))
	for _, task := range tasks {
		dto, err := h.buildTaskDTO(r.Context(), task)
		if err != nil {
			log.Warn("Не удалось собрать DTO для задачи", "task_id", task.ID, "error", err)
			continue // Пропускаем задачу, если не можем ее обогатить
		}
		taskDTOs = append(taskDTOs, *dto)
	}
	middleware.RespondWithJSON(w, http.StatusOK, taskDTOs)
}

// buildTaskDTO - это метод-фабрика, который преобразует модель задачи в DTO, обогащая ее данными.
func (h *TaskHandler) buildTaskDTO(ctx context.Context, task models.ReconciliationTask) (*api.TaskDTO, error) {
	dto := api.TaskDTO{
		ID:         task.ID,
		TaskType:   task.TaskType,
		EntityType: task.EntityType,
		EntityUUID: task.EntityUUID,
		Status:     task.Status,
		Comment:    task.Comment,
		CreatedAt:  task.CreatedAt,
		UpdatedAt:  task.UpdatedAt,
	}

	var detailsObject interface{}
	var err error

	// Логика определения и обогащения деталей задачи
	switch domain.TaskType(task.TaskType) {
	case domain.TaskAddEquipment:
		detailsObject, err = h.enrichAddEquipmentDetails(task)
	case domain.TaskNeedUpdate, domain.TaskDataConflict, domain.TaskResolveDuplicate:
		detailsObject = task.Details // Для этих типов пока оставляем как есть
	default:
		// Для задач, привязанных к существующим сущностям, обогащаем данными сущности
		detailsObject, err = h.enrichWithExistingEntity(ctx, task.EntityType, task.EntityUUID)
	}

	if err != nil {
		return nil, fmt.Errorf("ошибка обогащения деталей: %w", err)
	}

	// Преобразуем обогащенные детали в map[string]interface{}
	var detailsMap map[string]interface{}
	detailsBytes, _ := json.Marshal(detailsObject)
	json.Unmarshal(detailsBytes, &detailsMap)

	// Добавляем внешний ID, если он есть
	if task.EntityType != "" && task.EntityUUID != "" && domain.TaskType(task.TaskType) != domain.TaskAddEquipment {
		link, _ := h.linkRepo.GetByInternalID(ctx, nil, "naumen", task.EntityUUID)
		if link != nil {
			detailsMap["externalUUID"] = link.ServiceDeskUUID
		}
	}

	dto.Details = detailsMap
	return &dto, nil
}

// enrichAddEquipmentDetails обогащает детали для задачи типа "add_equipment".
func (h *TaskHandler) enrichAddEquipmentDetails(task models.ReconciliationTask) (interface{}, error) {
	var details struct {
		AgentData       api.AgentDataDTO `json:"agent_data"`
		EtalonOwnerUUID string           `json:"etalon_owner_id"`
	}
	if err := json.Unmarshal(task.Details, &details); err != nil {
		return nil, err
	}

	if domain.EntityType(task.EntityType) == domain.FiscalRegister {
		return struct {
			EtalonOwnerID    string                    `json:"etalon_owner_id"`
			EquipmentData    api.FiscalRegisterRichDTO `json:"equipment_data"`
			OrganizationName string                    `json:"organizationName"`
			SerialNumber     string                    `json:"serialNumber"`
			URLRms           string                    `json:"url_rms"`
			TeamviewerID     string                    `json:"teamviewer_id"`
			AnydeskID        string                    `json:"anydesk_id"`
			LitemanagerID    string                    `json:"litemanager_id"`
			AgentCurrentTime string                    `json:"agent_current_time"`
		}{
			EtalonOwnerID:    details.EtalonOwnerUUID,
			EquipmentData:    agentDataToFiscalRegisterRichDTO(details.AgentData),
			OrganizationName: details.AgentData.OrganizationName,
			SerialNumber:     details.AgentData.SerialNumber,
			URLRms:           details.AgentData.URLRms,
			TeamviewerID:     details.AgentData.TeamviewerID,
			AnydeskID:        details.AgentData.AnydeskID,
			LitemanagerID:    details.AgentData.LitemanagerID,
			AgentCurrentTime: details.AgentData.CurrentTime,
		}, nil
	}
	// Можно добавить логику для других типов оборудования
	return details, nil
}

// enrichWithExistingEntity обогащает детали данными о существующей сущности.
func (h *TaskHandler) enrichWithExistingEntity(ctx context.Context, entityType, entityUUID string) (interface{}, error) {
	switch domain.EntityType(entityType) {
	case domain.FiscalRegister:
		if fr, _ := h.frRepo.GetByID(ctx, entityUUID); fr != nil {
			return h.modelToFiscalRegisterRichDTO(ctx, *fr), nil
		}
	case domain.Server:
		if server, _ := h.serverRepo.GetByID(ctx, entityUUID); server != nil {
			return h.modelToServerRichDTO(ctx, *server), nil
		}
	case domain.Workstation:
		if ws, _ := h.workstationRepo.GetByID(ctx, entityUUID); ws != nil {
			return h.modelToWorkstationRichDTO(ctx, *ws), nil
		}
	}
	return nil, fmt.Errorf("сущность %s с ID %s не найдена", entityType, entityUUID)
}

// ResolveTask изменяет статус задачи и выполняет связанные с решением действия.
func (h *TaskHandler) ResolveTask(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	taskIDStr := chi.URLParam(r, "id")
	taskID, err := strconv.ParseUint(taskIDStr, 10, 32)
	if err != nil {
		RespondWithError(w, http.StatusBadRequest, "Некорректный ID задачи")
		return
	}

	var dto api.ResolveTaskRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		RespondWithError(w, http.StatusBadRequest, "Некорректное тело запроса")
		return
	}

	if dto.Status == "" {
		RespondWithError(w, http.StatusBadRequest, "Поле 'status' обязательно для заполнения")
		return
	}

	updatedTask, err := h.resolutionSvc.Resolve(r.Context(), uint(taskID), &dto)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrTaskNotFound):
			RespondWithError(w, http.StatusNotFound, "Задача не найдена")
		case errors.Is(err, services.ErrTaskAlreadyDone):
			RespondWithError(w, http.StatusConflict, "Задача уже была решена или отклонена")
		case errors.Is(err, services.ErrInvalidPayload):
			RespondWithError(w, http.StatusBadRequest, err.Error())
		default:
			log.Error("Ошибка при решении задачи", "taskID", taskID, "error", err)
			RespondWithError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера при решении задачи")
		}
		return
	}

	// Проверяем статус для корректного HTTP-ответа
	if updatedTask.Status == "pending_sd_action" {
		response := api.AcceptedResponseDTO{
			Message: "Запрос на операцию в ServiceDesk принят в обработку.",
			TaskID:  updatedTask.ID,
		}
		RespondWithJSON(w, http.StatusAccepted, response)
	} else {
		// Для всех остальных случаев (включая 'resolved', 'rejected') возвращаем 200 OK
		RespondWithJSON(w, http.StatusOK, updatedTask)
	}
}

// createEntityFromTask обрабатывает запрос на создание сущности в ServiceDesk на основе задачи.
func (h *TaskHandler) createEntityFromTask(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	taskIDStr := chi.URLParam(r, "id")
	taskID, err := strconv.ParseUint(taskIDStr, 10, 32)
	if err != nil {
		RespondWithError(w, http.StatusBadRequest, "Некорректный ID задачи")
		return
	}

	var dto api.CreateEntityInSDRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		RespondWithError(w, http.StatusBadRequest, "Неверный формат тела запроса")
		return
	}
	// TODO: Добавить валидацию DTO

	// Вызываем новый асинхронный метод
	updatedTask, err := h.resolutionSvc.RequestSDEntityCreation(r.Context(), uint(taskID), dto.EntityType)

	if err != nil {
		switch {
		case errors.Is(err, services.ErrTaskNotFound):
			RespondWithError(w, http.StatusNotFound, "Задача не найдена")
		case errors.Is(err, services.ErrTaskAlreadyDone):
			RespondWithError(w, http.StatusConflict, "Задача уже находится в обработке или решена")
		default:
			log.Error("Ошибка при отправке запроса на создание сущности в ServiceDesk",
				"taskID", taskID,
				"entityType", dto.EntityType,
				"error", err,
			)
			RespondWithError(w, http.StatusInternalServerError, fmt.Sprintf("Внутренняя ошибка: %v", err))
		}
		return
	}

	// Отвечаем 202 Accepted
	response := api.AcceptedResponseDTO{
		Message: "Запрос на создание сущности в ServiceDesk принят в обработку.",
		TaskID:  updatedTask.ID,
	}
	RespondWithJSON(w, http.StatusAccepted, response)
}

// GetDuplicates находит и возвращает группы дубликатов в формате JSON.
func (h *TaskHandler) GetDuplicates(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	var allGroups []api.DuplicateGroupDTO

	wsFields := []string{"anydesk", "teamviewer", "litemanager"}
	for _, field := range wsFields {
		groups, err := h.findDuplicateGroups(field, "Workstation")
		if err != nil {
			log.Error("Ошибка поиска дубликатов Workstation", "field", field, "error", err)
			RespondWithError(w, http.StatusInternalServerError, "Ошибка поиска дубликатов")
			return
		}
		allGroups = append(allGroups, groups...)
	}

	serverGroups, err := h.findDuplicateGroups("ip", "Server")
	if err != nil {
		log.Error("Ошибка поиска дубликатов Server", "field", "ip", "error", err)
		RespondWithError(w, http.StatusInternalServerError, "Ошибка поиска дубликатов")
		return
	}
	allGroups = append(allGroups, serverGroups...)

	RespondWithJSON(w, http.StatusOK, allGroups)
}

func (h *TaskHandler) findDuplicateGroups(field string, entityType string) ([]api.DuplicateGroupDTO, error) {
	var results []struct {
		Value string
		Count int
	}
	model := h.getModel(entityType)
	if model == nil {
		return nil, fmt.Errorf("неизвестный тип сущности: %s", entityType)
	}

	err := h.db.Model(model).
		Select(fmt.Sprintf("%s as value, count(*) as count", field)).
		Where(fmt.Sprintf("%s IS NOT NULL AND %s != ''", field, field)).
		Group(field).
		Having("count(*) > 1").
		Find(&results).Error

	if err != nil {
		return nil, err
	}

	var groups []api.DuplicateGroupDTO
	for _, res := range results {
		var records []interface{}
		switch entityType {
		case "Workstation":
			var wsRecords []models.Workstation
			h.db.Where(fmt.Sprintf("%s = ?", field), res.Value).Find(&wsRecords)
			for i := range wsRecords {
				records = append(records, wsRecords[i])
			}
		case "Server":
			var srvRecords []models.Server
			h.db.Where(fmt.Sprintf("%s = ?", field), res.Value).Find(&srvRecords)
			for i := range srvRecords {
				records = append(records, srvRecords[i])
			}
		}

		if len(records) < 2 {
			continue
		}

		sort.Slice(records, func(i, j int) bool {
			dateI := getLMDFromInterface(records[i])
			dateJ := getLMDFromInterface(records[j])
			if dateI == nil {
				return false
			}
			if dateJ == nil {
				return true
			}
			return dateI.After(*dateJ)
		})

		groups = append(groups, api.DuplicateGroupDTO{
			Field:      field,
			Value:      res.Value,
			MainRecord: records[0],
			Duplicates: records[1:],
			EntityType: entityType,
		})
	}
	return groups, nil
}

func (h *TaskHandler) getModel(entityType string) interface{} {
	switch entityType {
	case "Workstation":
		return &models.Workstation{}
	case "Server":
		return &models.Server{}
	default:
		return nil
	}
}

func getLMDFromInterface(record interface{}) *time.Time {
	switch v := record.(type) {
	case models.Workstation:
		return v.LastModifiedDate
	case models.Server:
		return v.LastModifiedDate
	case models.FiscalRegister:
		return v.LastModifiedDate
	default:
		return nil
	}
}

// --- Вспомогательные функции-мапперы ---

// agentDataToFiscalRegisterRichDTO преобразует сырые данные от агента в компактный DTO для UI.
func agentDataToFiscalRegisterRichDTO(data api.AgentDataDTO) api.FiscalRegisterRichDTO {
	return api.FiscalRegisterRichDTO{
		RNKKT:              &data.RNM,
		ModelKKT:           &data.ModelName,
		FNExpireDate:       utils.ParseAgentTime(data.DateTimeEnd),
		FNRegistrationDate: utils.ParseAgentTime(data.DateTimeReg),
		DriverVersion:      &data.InstalledDriver,
		FRFirmware:         utils.StringPtr(utils.CalculateFRFirmware(data.Licenses)),
		FRDownloader:       utils.StringPtr(data.BootVersion),
	}
}

// modelToFiscalRegisterRichDTO преобразует модель БД в DTO для UI.
// modelToFiscalRegisterRichDTO преобразует модель БД в DTO для UI, обогащая внешним ID.
func (h *TaskHandler) modelToFiscalRegisterRichDTO(ctx context.Context, fr models.FiscalRegister) api.FiscalRegisterRichDTO {
	dto := api.FiscalRegisterRichDTO{
		UUID:               fr.ID, // ИСПОЛЬЗУЕМ ВНУТРЕННИЙ ID
		Status:             fr.Status,
		RNKKT:              fr.RNKKT,
		ModelKKT:           fr.ModelKKT,
		FNRegistrationDate: fr.KKTRegDate,
		FNExpireDate:       fr.FNExpireDate,
		DriverVersion:      fr.DriverVersion,
		FRFirmware:         fr.FRFirmware,
		FRDownloader:       fr.FRDownloader,
		OrganizationName:   fr.LegalName,
		INN:                fr.INN,
		SerialNumber:       fr.FRSerialNumber,
		IsMarkingActive:    true,
		IsExciseActive:     false,
	}
	link, err := h.linkRepo.GetByInternalID(ctx, nil, "naumen", fr.ID)
	if err == nil && link != nil {
		dto.ServiceDeskUUID = &link.ServiceDeskUUID
	}
	return dto
}

// modelToServerRichDTO преобразует модель БД в DTO для UI, обогащая внешним ID.
func (h *TaskHandler) modelToServerRichDTO(ctx context.Context, server models.Server) api.ServerRichDTO {
	dto := api.ServerRichDTO{
		UUID:        server.ID, // ИСПОЛЬЗУЕМ ВНУТРЕННИЙ ID
		DeviceName:  server.DeviceName,
		IP:          server.IP,
		Status:      server.Status,
		Anydesk:     server.Anydesk,
		Teamviewer:  server.Teamviewer,
		RDP:         server.RDP,
		Litemanager: server.Litemanager,
		UniqueID:    server.UniqueID,
	}
	link, err := h.linkRepo.GetByInternalID(ctx, nil, "naumen", server.ID)
	if err == nil && link != nil {
		dto.ServiceDeskUUID = &link.ServiceDeskUUID
	}
	return dto
}

// modelToWorkstationRichDTO преобразует модель БД в DTO для UI, обогащая внешним ID.
func (h *TaskHandler) modelToWorkstationRichDTO(ctx context.Context, ws models.Workstation) api.WorkstationRichDTO {
	dto := api.WorkstationRichDTO{
		UUID:        ws.ID, // ИСПОЛЬЗУЕМ ВНУТРЕННИЙ ID
		DeviceName:  ws.DeviceName,
		Status:      ws.Status,
		Anydesk:     ws.Anydesk,
		Teamviewer:  ws.Teamviewer,
		Litemanager: ws.Litemanager,
	}
	link, err := h.linkRepo.GetByInternalID(ctx, nil, "naumen", ws.ID)
	if err == nil && link != nil {
		dto.ServiceDeskUUID = &link.ServiceDeskUUID
	}
	return dto
}
