// <-- НОВЫЙ ФАЙЛ -->
package handlers

import (
	"encoding/json"
	"errors"
	"etalon-server/internal/api"
	"etalon-server/internal/services"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

type AuthHandler struct {
	logger      *zap.Logger
	authService services.AuthService
}

func NewAuthHandler(logger *zap.Logger, authService services.AuthService) *AuthHandler {
	return &AuthHandler{logger, authService}
}

func (h *AuthHandler) RegisterRoutes(r chi.Router) {
	r.Post("/login", h.login)
}

func (h *AuthHandler) login(w http.ResponseWriter, r *http.Request) {
	var dto api.LoginRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		RespondWithError(w, http.StatusBadRequest, "Неверный формат запроса")
		return
	}

	response, err := h.authService.Login(r.Context(), dto.Username, dto.Password)
	if err != nil {
		if errors.Is(err, services.ErrInvalidCredentials) {
			RespondWithError(w, http.StatusUnauthorized, "Неверное имя пользователя или пароль")
		} else {
			h.logger.Error("Ошибка входа в систему", zap.String("username", dto.Username), zap.Error(err))
			RespondWithError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
		return
	}

	RespondWithJSON(w, http.StatusOK, response)
}
