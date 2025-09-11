// <-- НОВЫЙ ФАЙЛ -->
package handlers

import (
	"encoding/json"
	"errors"
	"etalon-server/internal/services"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/middleware"
	"net/http"

	"github.com/go-chi/chi/v5"
)

type AuthHandler struct {
	authService services.AuthService
}

func NewAuthHandler(authService services.AuthService) *AuthHandler {
	return &AuthHandler{authService}
}

func (h *AuthHandler) RegisterRoutes(r chi.Router) {
	r.Post("/login", h.login)
}

func (h *AuthHandler) login(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	log.Info("Начало обработки запроса авторизации", "method", r.Method, "path", r.URL.Path)

	var dto api.LoginRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		log.Debug("Ошибка декодирования тела запроса авторизации", "error", err)
		RespondWithError(w, http.StatusBadRequest, "Неверный формат запроса")
		return
	}

	log.Debug("Декодирован запрос авторизации", "username", dto.Username)

	response, err := h.authService.Login(r.Context(), dto.Username, dto.Password)
	if err != nil {
		if errors.Is(err, services.ErrInvalidCredentials) {
			log.Info("Неудачная попытка авторизации - неверные учетные данные", "username", dto.Username)
			RespondWithError(w, http.StatusUnauthorized, "Неверное имя пользователя или пароль")
		} else {
			log.Error("Ошибка входа в систему", "username", dto.Username, "error", err)
			RespondWithError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
		return
	}

	log.Info("Успешная авторизация пользователя", "username", dto.Username)
	RespondWithJSON(w, http.StatusOK, response)
}
