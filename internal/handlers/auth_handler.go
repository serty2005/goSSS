// <-- НОВЫЙ ФАЙЛ -->
package handlers

import (
	"encoding/json"
	"errors"
	"etalon-server/internal/api"
	"etalon-server/internal/logger"
	"etalon-server/internal/services"
	"net/http"

	"github.com/go-chi/chi/v5"
)

type AuthHandler struct {
	logger      logger.LoggerInterface
	authService services.AuthService
}

func NewAuthHandler(logger logger.LoggerInterface, authService services.AuthService) *AuthHandler {
	return &AuthHandler{logger, authService}
}

func (h *AuthHandler) RegisterRoutes(r chi.Router) {
	r.Post("/login", h.login)
}

func (h *AuthHandler) login(w http.ResponseWriter, r *http.Request) {
	h.logger.Info("Начало обработки запроса авторизации", "method", r.Method, "path", r.URL.Path)

	var dto api.LoginRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		h.logger.Debug("Ошибка декодирования тела запроса авторизации", "error", err)
		RespondWithError(w, http.StatusBadRequest, "Неверный формат запроса")
		return
	}

	h.logger.Debug("Декодирован запрос авторизации", "username", dto.Username)

	response, err := h.authService.Login(r.Context(), dto.Username, dto.Password)
	if err != nil {
		if errors.Is(err, services.ErrInvalidCredentials) {
			h.logger.Info("Неудачная попытка авторизации - неверные учетные данные", "username", dto.Username)
			RespondWithError(w, http.StatusUnauthorized, "Неверное имя пользователя или пароль")
		} else {
			h.logger.Error("Ошибка входа в систему", "username", dto.Username, "error", err)
			RespondWithError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
		return
	}

	h.logger.Info("Успешная авторизация пользователя", "username", dto.Username)
	RespondWithJSON(w, http.StatusOK, response)
}
