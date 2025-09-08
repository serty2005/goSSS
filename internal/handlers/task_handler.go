package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"etalon-server/internal/api"
	"etalon-server/internal/models"
	"etalon-server/internal/repositories"
	"etalon-server/internal/services"
	"etalon-server/internal/utils"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// TaskHandler обрабатывает запросы, связанные с задачами сверки и поиском дубликатов.
type TaskHandler struct {
	logger          *zap.Logger
	db              *gorm.DB
	resolutionSvc   services.TaskResolutionService
	sdEditorSvc     services.SDEditorService
	serverRepo      repositories.ServerRepo
	workstationRepo repositories.WorkstationRepo
	frRepo          repositories.FiscalRegisterRepo
	linkRepo        repositories.LinkRepo
}

// NewTaskHandler создает новый экземпляр обработчика.
func NewTaskHandler(
	logger *zap.Logger,
	db *gorm.DB,
	resolutionSvc services.TaskResolutionService,
	sdEditorSvc services.SDEditorService,
	serverRepo repositories.ServerRepo,
	workstationRepo repositories.WorkstationRepo,
	frRepo repositories.FiscalRegisterRepo,
	linkRepo repositories.LinkRepo,
) *TaskHandler {
	return &TaskHandler{
		logger:          logger,
		db:              db,
		resolutionSvc:   resolutionSvc,
		sdEditorSvc:     sdEditorSvc,
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

// GetTasks возвращает список задач сверки с фильтрацией, пагинацией и обогащенными данными.
func (h *TaskHandler) GetTasks(w http.ResponseWriter, r *http.Request) {
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
		h.logger.Error("Не удалось получить задачи из БД", zap.Error(err))
		RespondWithError(w, http.StatusInternalServerError, "Ошибка получения списка задач")
		return
	}

	taskDTOs := make([]api.TaskDTO, 0, len(tasks))
	for _, task := range tasks {
		dto := api.TaskDTO{
			ID: task.ID, TaskType: task.TaskType, EntityType: task.EntityType,
			EntityUUID: task.EntityUUID, Status: task.Status, Comment: task.Comment,
			CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt,
		}
		var detailsObject interface{}

		switch task.TaskType {
		case "add_equipment":
			var details struct {
				AgentData       api.AgentDataDTO `json:"agent_data"`
				EtalonOwnerUUID string           `json:"etalon_owner_id"` // Имя поля исправлено
			}
			if err := json.Unmarshal(task.Details, &details); err == nil {
				var richDetails interface{}
				if task.EntityType == "FiscalRegister" {
					richDetails = struct {
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
						EtalonOwnerID:    details.EtalonOwnerUUID, // Используем внутренний ID
						EquipmentData:    agentDataToFiscalRegisterRichDTO(details.AgentData),
						OrganizationName: details.AgentData.OrganizationName,
						SerialNumber:     details.AgentData.SerialNumber,
						URLRms:           details.AgentData.URLRms,
						TeamviewerID:     details.AgentData.TeamviewerID,
						AnydeskID:        details.AgentData.AnydeskID,
						LitemanagerID:    details.AgentData.LitemanagerID,
						AgentCurrentTime: details.AgentData.CurrentTime,
					}
				}
				detailsObject = richDetails
			} else {
				detailsObject = task.Details
			}
		case "need_update", "data_conflict", "resolve_duplicate":
			detailsObject = task.Details
		default:
			var richDetails interface{}
			switch task.EntityType {
			case "FiscalRegister":
				if fr, _ := h.frRepo.GetByID(r.Context(), task.EntityUUID); fr != nil {
					richDetails = h.modelToFiscalRegisterRichDTO(r.Context(), *fr)
				}
			case "Server":
				if server, _ := h.serverRepo.GetByID(r.Context(), task.EntityUUID); server != nil {
					richDetails = h.modelToServerRichDTO(r.Context(), *server)
				}
			case "Workstation":
				if ws, _ := h.workstationRepo.GetByID(r.Context(), task.EntityUUID); ws != nil {
					richDetails = h.modelToWorkstationRichDTO(r.Context(), *ws)
				}
			}
			if richDetails != nil {
				detailsObject = richDetails
			} else {
				detailsObject = task.Details
			}
		}

		var detailsMap map[string]interface{}
		detailsBytes, err := json.Marshal(detailsObject)
		if err == nil {
			json.Unmarshal(detailsBytes, &detailsMap)
		} else if rawJson, ok := detailsObject.(json.RawMessage); ok {
			json.Unmarshal(rawJson, &detailsMap)
		}
		if detailsMap == nil {
			detailsMap = make(map[string]interface{})
		}

		// Добавляем externalUUID в details для задач, связанных с существующими сущностями.
		if task.EntityType != "" && task.EntityUUID != "" && task.TaskType != "add_equipment" {
			link, _ := h.linkRepo.GetByInternalID(r.Context(), nil, "naumen", task.EntityUUID)
			if link != nil {
				detailsMap["externalUUID"] = link.ExternalID
			}
		}
		dto.Details = detailsMap
		taskDTOs = append(taskDTOs, dto)
	}
	RespondWithJSON(w, http.StatusOK, taskDTOs)
}

// ResolveTask изменяет статус задачи и выполняет связанные с решением действия.
func (h *TaskHandler) ResolveTask(w http.ResponseWriter, r *http.Request) {
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
			h.logger.Error("Ошибка при решении задачи", zap.Uint64("taskID", taskID), zap.Error(err))
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
			h.logger.Error("Ошибка при отправке запроса на создание сущности в ServiceDesk",
				zap.Uint64("taskID", taskID),
				zap.String("entityType", dto.EntityType),
				zap.Error(err),
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
	var allGroups []api.DuplicateGroupDTO

	wsFields := []string{"anydesk", "teamviewer", "litemanager"}
	for _, field := range wsFields {
		groups, err := h.findDuplicateGroups(field, "Workstation")
		if err != nil {
			h.logger.Error("Ошибка поиска дубликатов Workstation", zap.String("field", field), zap.Error(err))
			RespondWithError(w, http.StatusInternalServerError, "Ошибка поиска дубликатов")
			return
		}
		allGroups = append(allGroups, groups...)
	}

	serverGroups, err := h.findDuplicateGroups("ip", "Server")
	if err != nil {
		h.logger.Error("Ошибка поиска дубликатов Server", zap.String("field", "ip"), zap.Error(err))
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
		dto.ExternalUUID = &link.ExternalID
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
		dto.ExternalUUID = &link.ExternalID
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
		dto.ExternalUUID = &link.ExternalID
	}
	return dto
}
