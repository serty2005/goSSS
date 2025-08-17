package handlers

import (
	"context"
	"encoding/json"
	"etalon-server/internal/api"
	"etalon-server/internal/seeder"
	"etalon-server/internal/services"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// SyncHandler обрабатывает запросы, связанные с синхронизацией и наполнением базы.
type SyncHandler struct {
	logger      *zap.Logger
	syncService services.SyncService
	seeder      *seeder.Seeder
	seederKey   string
}

// NewSyncHandler создает новый обработчик синхронизации.
func NewSyncHandler(logger *zap.Logger, syncService services.SyncService, seeder *seeder.Seeder, seederKey string) *SyncHandler {
	return &SyncHandler{
		logger:      logger,
		syncService: syncService,
		seeder:      seeder,
		seederKey:   seederKey,
	}
}

// RegisterRoutes регистрирует роуты для этого обработчика.
func (h *SyncHandler) RegisterRoutes(router chi.Router) {
	router.Post("/servicedesk", h.TriggerSync)
	router.Post("/seed", h.TriggerSeed)
}

// TriggerSync запускает фоновую синхронизацию с ServiceDesk.
func (h *SyncHandler) TriggerSync(w http.ResponseWriter, r *http.Request) {
	var req api.SyncRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "Неверное тело запроса")
		return
	}

	go h.syncService.SyncAllData(context.Background(), req.Full)

	RespondWithJSON(w, http.StatusAccepted, map[string]string{
		"message": "Синхронизация запущена",
	})
}

// TriggerSeed запускает фоновое наполнение базы данных из мок-файлов.
func (h *SyncHandler) TriggerSeed(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == "" || key != h.seederKey {
		RespondWithError(w, http.StatusUnauthorized, "Неверный или отсутствует ключ доступа")
		return
	}

	go func() {
		h.logger.Info("Запуск наполнения БД через API...")
		mockClient := seeder.NewMockServiceDeskClient(h.logger, "./tools/seeder/mock_data")
		if err := h.seeder.SeedDatabase(mockClient); err != nil {
			h.logger.Error("Процесс наполнения БД завершился с ошибкой", zap.Error(err))
		} else {
			h.logger.Info("Процесс наполнения БД, запущенный через API, успешно завершен.")
		}
	}()

	RespondWithJSON(w, http.StatusAccepted, map[string]string{
		"message": "Наполнение базы данных запущено в фоновом режиме",
	})
}
