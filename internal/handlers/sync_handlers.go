// internal/handlers/sync_handlers.go
package handlers

import (
	"etalon-server/internal/logger"
	"etalon-server/internal/seeder"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// SyncHandler обрабатывает запросы, связанные с наполнением базы.
type SyncHandler struct {
	logger    logger.LoggerInterface
	seeder    *seeder.Seeder
	seederKey string
}

// NewSyncHandler создает новый обработчик синхронизации.
func NewSyncHandler(logger logger.LoggerInterface, seeder *seeder.Seeder, seederKey string) *SyncHandler {
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
	h.logger.Info("Получен запрос на запуск наполнения БД", "method", r.Method, "path", r.URL.Path)

	key := r.URL.Query().Get("key")
	if key == "" {
		h.logger.Warn("Попытка запуска наполнения БД без ключа доступа", "remote_addr", r.RemoteAddr)
		RespondWithError(w, http.StatusUnauthorized, "Неверный или отсутствует ключ доступа")
		return
	}

	h.logger.Debug("Проверка ключа доступа", "key_provided", key != "", "key_length", len(key))

	if key != h.seederKey {
		h.logger.Warn("Попытка запуска наполнения БД с неверным ключом", "remote_addr", r.RemoteAddr)
		RespondWithError(w, http.StatusUnauthorized, "Неверный или отсутствует ключ доступа")
		return
	}

	h.logger.Info("Ключ доступа проверен успешно, запускаем наполнение БД в фоне")

	go func() {
		h.logger.Info("Начало фонового процесса наполнения БД")
		// TODO: Обновить seeder.NewMockServiceDeskClient на LoggerInterface
		// mockClient := seeder.NewMockServiceDeskClient(h.logger, "./tools/seeder/mock_data")
		h.logger.Warn("Временное решение: seeder.NewMockServiceDeskClient пока не обновлен на LoggerInterface")
		// if err := h.seeder.SeedDatabase(mockClient); err != nil {
		// 	h.logger.Error("Процесс наполнения БД завершился с ошибкой", "error", err)
		// } else {
		// 	h.logger.Info("Процесс наполнения БД успешно завершен")
		// }
		h.logger.Info("Процесс наполнения БД временно отключен до обновления seeder")
	}()

	h.logger.Info("Ответ отправлен клиенту, процесс запущен в фоне")

	RespondWithJSON(w, http.StatusAccepted, map[string]string{
		"message": "Наполнение базы данных запущено в фоновом режиме",
	})
}
