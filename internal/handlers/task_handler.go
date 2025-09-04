package handlers

import (
	"encoding/json"
	"errors"
	"etalon-server/internal/api"
	"etalon-server/internal/models"
	"etalon-server/internal/services"
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
	logger        *zap.Logger
	db            *gorm.DB
	resolutionSvc services.TaskResolutionService // <-- ДОБАВЛЕНО ПОЛЕ
}

// NewTaskHandler создает новый экземпляр обработчика.
func NewTaskHandler(logger *zap.Logger, db *gorm.DB, resolutionSvc services.TaskResolutionService) *TaskHandler {
	return &TaskHandler{
		logger:        logger,
		db:            db,
		resolutionSvc: resolutionSvc,
	}
}

// RegisterRoutes регистрирует роуты для задач и дубликатов.
func (h *TaskHandler) RegisterRoutes(r chi.Router) {
	r.Get("/tasks", h.GetTasks)
	r.Post("/tasks/{id}/resolve", h.ResolveTask)
	r.Get("/duplicates", h.GetDuplicates)
}

// GetTasks возвращает список задач сверки с фильтрацией и пагинацией.
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

	RespondWithJSON(w, http.StatusOK, tasks)
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

	RespondWithJSON(w, http.StatusOK, updatedTask)
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
