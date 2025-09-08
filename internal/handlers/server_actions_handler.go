// internal/handlers/server_actions_handler.go
package handlers

import (
	"encoding/json"
	"errors"
	"etalon-server/internal/services"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ServerActionsHandler обрабатывает специфичные действия над серверами.
type ServerActionsHandler struct {
	logger     *zap.Logger
	actionsSvc services.ServerActionsService
}

// NewServerActionsHandler создает новый экземпляр обработчика.
func NewServerActionsHandler(logger *zap.Logger, actionsSvc services.ServerActionsService) *ServerActionsHandler {
	return &ServerActionsHandler{
		logger:     logger,
		actionsSvc: actionsSvc,
	}
}

// RegisterRoutes регистрирует роуты для действий с серверами.
// ИЗМЕНЕНИЕ: В URL теперь ожидается внутренний ID, а не UUID.
func (h *ServerActionsHandler) RegisterRoutes(r chi.Router) {
	r.Post("/servers/{id}/install_license", h.installLicense)
	r.Post("/servers/{id}/poll", h.pollServerStatus)
	r.Post("/servers/{serverID}/additional_owners", h.addAdditionalOwner)
	r.Delete("/servers/{serverID}/additional_owners/{companyID}", h.removeAdditionalOwner)
}

type installLicenseRequestDTO struct {
	UniqueID string `json:"uniqueId"`
}

// installLicense обрабатывает запрос на запуск установки лицензии.
func (h *ServerActionsHandler) installLicense(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "id")
	if serverID == "" {
		RespondWithError(w, http.StatusBadRequest, "ID сервера не указан")
		return
	}

	var dto installLicenseRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		RespondWithError(w, http.StatusBadRequest, "Неверный формат тела запроса")
		return
	}
	if dto.UniqueID == "" {
		RespondWithError(w, http.StatusBadRequest, "Поле 'uniqueId' обязательно для заполнения")
		return
	}

	err := h.actionsSvc.InstallLicense(r.Context(), serverID, dto.UniqueID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			RespondWithError(w, http.StatusNotFound, "Сервер с указанным ID не найден")
		} else {
			h.logger.Error("Ошибка при вызове заглушки установки лицензии", zap.String("serverID", serverID), zap.Error(err))
			RespondWithError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
		return
	}
	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "Команда на установку лицензии отправлена успешно"})
}

// pollServerStatus обрабатывает запрос на принудительный асинхронный опрос статуса сервера.
func (h *ServerActionsHandler) pollServerStatus(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "id")
	if serverID == "" {
		RespondWithError(w, http.StatusBadRequest, "ID сервера не указан")
		return
	}

	err := h.actionsSvc.PollSingleServer(r.Context(), serverID)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrRateLimitExceeded):
			RespondWithError(w, http.StatusTooManyRequests, "Превышен лимит запросов на опрос статуса для этого сервера (не более 3 раз в 2 минуты)")
		case errors.Is(err, gorm.ErrRecordNotFound):
			RespondWithError(w, http.StatusNotFound, "Сервер с указанным ID не найден")
		default:
			h.logger.Error("Ошибка при запуске принудительного опроса", zap.String("serverID", serverID), zap.Error(err))
			RespondWithError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
		return
	}
	RespondWithJSON(w, http.StatusAccepted, map[string]string{"message": "Задача на опрос статуса сервера принята в обработку"})
}

type additionalOwnerRequestDTO struct {
	CompanyID string `json:"company_id"` // ИЗМЕНЕНИЕ: Поле переименовано
}

// addAdditionalOwner обрабатывает запрос на добавление дополнительного владельца.
func (h *ServerActionsHandler) addAdditionalOwner(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	var dto additionalOwnerRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		RespondWithError(w, http.StatusBadRequest, "Неверный формат тела запроса")
		return
	}
	if serverID == "" || dto.CompanyID == "" {
		RespondWithError(w, http.StatusBadRequest, "ID сервера и компании обязательны")
		return
	}

	err := h.actionsSvc.AddAdditionalOwner(r.Context(), serverID, dto.CompanyID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			RespondWithError(w, http.StatusNotFound, "Сервер или компания не найдены")
		} else {
			h.logger.Error("Ошибка при добавлении дополнительного владельца", zap.Error(err))
			RespondWithError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
		return
	}
	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "Дополнительный владелец успешно добавлен"})
}

// removeAdditionalOwner обрабатывает запрос на удаление дополнительного владельца.
func (h *ServerActionsHandler) removeAdditionalOwner(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	companyID := chi.URLParam(r, "companyID")

	if serverID == "" || companyID == "" {
		RespondWithError(w, http.StatusBadRequest, "ID сервера и компании обязательны")
		return
	}

	err := h.actionsSvc.RemoveAdditionalOwner(r.Context(), serverID, companyID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			RespondWithError(w, http.StatusNotFound, "Сервер или компания не найдены")
		} else {
			h.logger.Error("Ошибка при удалении дополнительного владельца", zap.Error(err))
			RespondWithError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
