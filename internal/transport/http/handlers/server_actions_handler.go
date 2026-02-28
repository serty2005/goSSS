package handlers

import (
	"encoding/json"
	"errors"
	"etalon-server/internal/domain"
	"etalon-server/internal/infra/iiko"
	"etalon-server/internal/services"
	"etalon-server/internal/transport/http/middleware"
	"etalon-server/internal/transport/http/response"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// ServerActionsHandler обрабатывает специфичные действия над серверами.
type ServerActionsHandler struct {
	actionsSvc services.ServerActionsService
}

// NewServerActionsHandler создает новый экземпляр обработчика.
func NewServerActionsHandler(actionsSvc services.ServerActionsService) *ServerActionsHandler {
	return &ServerActionsHandler{
		actionsSvc: actionsSvc,
	}
}

// RegisterRoutes регистрирует роуты для действий с серверами.
func (h *ServerActionsHandler) RegisterRoutes(r chi.Router) {
	r.Post("/servers/{id}/license", h.installLicense)
	r.Post("/servers/{id}/poll", h.pollServerStatus)
	r.Post("/servers/{serverID}/additional_owners", h.addAdditionalOwner)
	r.Delete("/servers/{serverID}/additional_owners/{companyID}", h.removeAdditionalOwner)
}

type installLicenseRequestDTO struct {
	Login            string `json:"login"`
	Password         string `json:"password"`
	FallbackPassword string `json:"fallback_password,omitempty"`
	UniqueID         string `json:"unique_id"`
}

type installLicenseResponseDTO struct {
	Message string                         `json:"message"`
	Result  *services.InstallLicenseResult `json:"result,omitempty"`
}

func (h *ServerActionsHandler) installLicense(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	serverID := chi.URLParam(r, "id")
	if serverID == "" {
		response.RespondWithError(w, http.StatusBadRequest, "ID сервера не указан")
		return
	}

	var dto installLicenseRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Неверный формат тела запроса")
		return
	}
	if strings.TrimSpace(dto.Login) == "" {
		response.RespondWithError(w, http.StatusBadRequest, "Поле 'login' обязательно для заполнения")
		return
	}
	if strings.TrimSpace(dto.Password) == "" {
		response.RespondWithError(w, http.StatusBadRequest, "Поле 'password' обязательно для заполнения")
		return
	}
	if strings.TrimSpace(dto.UniqueID) == "" {
		response.RespondWithError(w, http.StatusBadRequest, "Поле 'unique_id' обязательно для заполнения")
		return
	}

	result, err := h.actionsSvc.InstallLicense(
		r.Context(),
		serverID,
		dto.Login,
		dto.Password,
		dto.FallbackPassword,
		dto.UniqueID,
	)
	if err != nil {
		var httpErr *iiko.HttpError
		if errors.As(err, &httpErr) {
			if httpErr.StatusCode == http.StatusUnauthorized || httpErr.StatusCode == http.StatusForbidden {
				log.Warn("Ошибка авторизации при установке лицензии", "serverID", serverID, "error", err)
				response.RespondWithError(w, http.StatusUnauthorized, "Неверный логин или пароль для доступа к iikoRMS серверу")
				return
			}
		}

		if errors.Is(err, domain.ErrNotFound) {
			response.RespondWithError(w, http.StatusNotFound, "Сервер с указанным ID не найден")
		} else if strings.Contains(strings.ToLower(err.Error()), "обязателен") || strings.Contains(strings.ToLower(err.Error()), "доступна только") {
			response.RespondWithError(w, http.StatusBadRequest, err.Error())
		} else {
			log.Error("Ошибка при установке лицензии", "serverID", serverID, "error", err)
			response.RespondWithError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера при установке лицензии")
		}
		return
	}
	response.RespondWithJSON(w, http.StatusOK, installLicenseResponseDTO{
		Message: "Лицензия успешно установлена и сервер подтвердил запуск",
		Result:  result,
	})
}

// pollServerStatus обрабатывает запрос на принудительный асинхронный опрос статуса сервера.
func (h *ServerActionsHandler) pollServerStatus(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	serverID := chi.URLParam(r, "id")
	if serverID == "" {
		response.RespondWithError(w, http.StatusBadRequest, "ID сервера не указан")
		return
	}

	err := h.actionsSvc.PollSingleServer(r.Context(), serverID)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrRateLimitExceeded):
			response.RespondWithError(w, http.StatusTooManyRequests, "Превышен лимит запросов на опрос статуса для этого сервера (не более 3 раз в 2 минуты)")
		case errors.Is(err, services.ErrCloudPollingSkipped):
			response.RespondWithError(w, http.StatusBadRequest, "Для cloud-адресов iikoWeb/syrve.app опрос отключен")
		case errors.Is(err, domain.ErrNotFound):
			response.RespondWithError(w, http.StatusNotFound, "Сервер с указанным ID не найден")
		default:
			log.Error("Ошибка при запуске принудительного опроса", "serverID", serverID, "error", err)
			response.RespondWithError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
		return
	}
	response.RespondWithJSON(w, http.StatusAccepted, map[string]string{"message": "Задача на опрос статуса сервера принята в обработку"})
}

type additionalOwnerRequestDTO struct {
	CompanyID string `json:"company_id"`
}

// addAdditionalOwner обрабатывает запрос на добавление дополнительного владельца.
func (h *ServerActionsHandler) addAdditionalOwner(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	serverID := chi.URLParam(r, "serverID")
	var dto additionalOwnerRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Неверный формат тела запроса")
		return
	}
	if serverID == "" || dto.CompanyID == "" {
		response.RespondWithError(w, http.StatusBadRequest, "ID сервера и компании обязательны")
		return
	}

	log.Debug("Запрос на добавление допвладельца для сервера", "serverID", serverID, "CompanyID", dto.CompanyID)

	err := h.actionsSvc.AddAdditionalOwner(r.Context(), serverID, dto.CompanyID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			response.RespondWithError(w, http.StatusNotFound, "Сервер или компания не найдены")
		} else {
			log.Error("Ошибка при добавлении дополнительного владельца", "error", err)
			response.RespondWithError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
		return
	}
	response.RespondWithJSON(w, http.StatusOK, map[string]string{"message": "Дополнительный владелец успешно добавлен"})
}

// removeAdditionalOwner обрабатывает запрос на удаление дополнительного владельца.
func (h *ServerActionsHandler) removeAdditionalOwner(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	serverID := chi.URLParam(r, "serverID")
	companyID := chi.URLParam(r, "companyID")

	if serverID == "" || companyID == "" {
		response.RespondWithError(w, http.StatusBadRequest, "ID сервера и компании обязательны")
		return
	}
	log.Debug("Запрос на удаление допвладельца у сервера", "serverID", serverID, "companyID", companyID)

	err := h.actionsSvc.RemoveAdditionalOwner(r.Context(), serverID, companyID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			response.RespondWithError(w, http.StatusNotFound, "Сервер или компания не найдены")
		} else {
			log.Error("Ошибка при удалении дополнительного владельца", "error", err)
			response.RespondWithError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
