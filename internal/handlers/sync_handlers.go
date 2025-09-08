// internal/handlers/sync_handlers.go
package handlers

import (
	"etalon-server/internal/seeder"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// SyncHandler обрабатывает запросы, связанные с наполнением базы.
type SyncHandler struct {
	logger    *zap.Logger
	seeder    *seeder.Seeder
	seederKey string
}

// NewSyncHandler создает новый обработчик синхронизации.
func NewSyncHandler(logger *zap.Logger, seeder *seeder.Seeder, seederKey string) *SyncHandler {
	return &SyncHandler{
		logger:    logger,
		seeder:    seeder,
		seederKey: seederKey,
	}
}

// RegisterRoutes регистрирует роуты для этого обработчика.
func (h *SyncHandler) RegisterRoutes(router chi.Router) {
	router.Post("/seed", h.TriggerSeed)
	// router.Post("/contracts", h.TriggerContractSync) // ИЗМЕНЕНИЕ: Удаляем ручку
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
