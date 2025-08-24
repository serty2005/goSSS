// internal/handlers/sync_handlers.go
package handlers

import (
	"context"
	"etalon-server/internal/seeder"
	"etalon-server/internal/services" // Добавляем импорт services
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// SyncHandler обрабатывает запросы, связанные с синхронизацией и наполнением базы.
type SyncHandler struct {
	logger          *zap.Logger
	seeder          *seeder.Seeder
	seederKey       string
	contractSyncSvc services.ContractSyncService // Новая зависимость
}

// NewSyncHandler создает новый обработчик синхронизации.
func NewSyncHandler(logger *zap.Logger, seeder *seeder.Seeder, seederKey string, contractSyncSvc services.ContractSyncService) *SyncHandler {
	return &SyncHandler{
		logger:          logger,
		seeder:          seeder,
		seederKey:       seederKey,
		contractSyncSvc: contractSyncSvc,
	}
}

// RegisterRoutes регистрирует роуты для этого обработчика.
func (h *SyncHandler) RegisterRoutes(router chi.Router) {
	router.Post("/seed", h.TriggerSeed)
	router.Post("/contracts", h.TriggerContractSync) // Новый роут
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

// TriggerContractSync запускает фоновую синхронизацию контрактов.
func (h *SyncHandler) TriggerContractSync(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == "" || key != h.seederKey {
		RespondWithError(w, http.StatusUnauthorized, "Неверный или отсутствует ключ доступа")
		return
	}

	go func() {
		h.logger.Info("Запуск синхронизации контрактов через API...")
		if err := h.contractSyncSvc.RunSyncCycle(context.Background()); err != nil {
			h.logger.Error("Процесс синхронизации контрактов завершился с ошибкой", zap.Error(err))
		} else {
			h.logger.Info("Процесс синхронизации контрактов, запущенный через API, успешно завершен.")
		}
	}()

	RespondWithJSON(w, http.StatusAccepted, map[string]string{
		"message": "Синхронизация контрактов запущена в фоновом режиме",
	})
}
