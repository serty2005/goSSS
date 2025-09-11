// Файл: internal/transport/http/handlers/sync_handler.go
package handlers

import (
	"etalon-server/internal/pkg/seeder"
	"etalon-server/internal/transport/http/middleware"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// SyncHandler обрабатывает запросы, связанные с наполнением базы.
type SyncHandler struct {
	seeder    *seeder.Seeder
	seederKey string
}

// NewSyncHandler создает новый обработчик синхронизации.
func NewSyncHandler(seeder *seeder.Seeder, seederKey string) *SyncHandler {
	return &SyncHandler{
		seeder:    seeder,
		seederKey: seederKey,
	}
}

// RegisterRoutes регистрирует роуты для этого обработчика.
func (h *SyncHandler) RegisterRoutes(router chi.Router) {
	router.Post("/seed", h.TriggerSeed)
}

// TriggerSeed запускает фоновое наполнение базы данных из мок-файлов.
func (h *SyncHandler) TriggerSeed(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	log.Info("Получен запрос на запуск наполнения БД", "method", r.Method, "path", r.URL.Path)

	key := r.URL.Query().Get("key")
	if key == "" || key != h.seederKey {
		log.Warn("Попытка запуска наполнения БД с неверным ключом", "remote_addr", r.RemoteAddr)
		RespondWithError(w, http.StatusUnauthorized, "Неверный или отсутствует ключ доступа")
		return
	}

	log.Info("Ключ доступа проверен успешно, запускаем наполнение БД в фоне")

	go func() {
		log.Info("Начало фонового процесса наполнения БД")
		// Создаем мок-клиент, который будет читать данные из локальных файлов
		mockClient := seeder.NewMockServiceDeskClient(log, "./tools/seeder/mock_data")
		if err := h.seeder.SeedDatabase(mockClient); err != nil {
			log.Error("Процесс наполнения БД завершился с ошибкой", "error", err)
		} else {
			log.Info("Процесс наполнения БД успешно завершен")
		}
	}()

	log.Info("Ответ отправлен клиенту, процесс запущен в фоне")

	RespondWithJSON(w, http.StatusAccepted, map[string]string{
		"message": "Наполнение базы данных запущено в фоновом режиме",
	})
}
