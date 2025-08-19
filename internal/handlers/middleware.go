package handlers

import (
	"net/http"
	"strings"
)

// AgentAuthMiddleware проверяет наличие и правильность Bearer токена для агентов.
func AgentAuthMiddleware(apiKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if apiKey == "" {
				// Если ключ не задан на сервере, считаем это ошибкой конфигурации.
				RespondWithError(w, http.StatusInternalServerError, "Сервер не настроен для аутентификации агентов")
				return
			}

			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				RespondWithError(w, http.StatusUnauthorized, "Отсутствует заголовок Authorization")
				return
			}

			headerParts := strings.Split(authHeader, " ")
			if len(headerParts) != 2 || strings.ToLower(headerParts[0]) != "bearer" {
				RespondWithError(w, http.StatusUnauthorized, "Неверный формат заголовка Authorization")
				return
			}

			token := headerParts[1]
			if token != apiKey {
				RespondWithError(w, http.StatusUnauthorized, "Неверный ключ API агента")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
