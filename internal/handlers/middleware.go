package handlers

import (
	"context"
	"etalon-server/internal/config"
	"etalon-server/internal/contextkeys" // ИЗМЕНЕНИЕ: Новый импорт
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// JwtAuthMiddleware проверяет JWT токен и добавляет информацию о пользователе в контекст.
func JwtAuthMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				RespondWithError(w, http.StatusUnauthorized, "Отсутствует заголовок Authorization")
				return
			}

			tokenString := strings.TrimPrefix(authHeader, "Bearer ")
			if tokenString == authHeader {
				RespondWithError(w, http.StatusUnauthorized, "Неверный формат токена")
				return
			}

			token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
				if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, fmt.Errorf("неожиданный метод подписи: %v", token.Header["alg"])
				}
				return []byte(cfg.JWTSecret), nil
			})

			if err != nil || !token.Valid {
				RespondWithError(w, http.StatusUnauthorized, "Невалидный токен")
				return
			}

			if claims, ok := token.Claims.(jwt.MapClaims); ok {
				ctx := r.Context()
				// Извлекаем ID
				if sub, ok := claims["sub"].(string); ok {
					// ИЗМЕНЕНИЕ: Используем константу из contextkeys
					ctx = context.WithValue(ctx, contextkeys.UserIDContextKey, sub)
				} else {
					RespondWithError(w, http.StatusUnauthorized, "Невалидный sub в токене")
					return
				}
				// Извлекаем роли
				if roles, ok := claims["roles"].([]interface{}); ok {
					var rolesStr []string
					for _, r := range roles {
						if role, ok := r.(string); ok {
							rolesStr = append(rolesStr, role)
						}
					}
					// ИЗМЕНЕНИЕ: Используем константу из contextkeys
					ctx = context.WithValue(ctx, contextkeys.UserRolesContextKey, rolesStr)
				} else {
					RespondWithError(w, http.StatusUnauthorized, "Невалидные roles в токене")
					return
				}

				next.ServeHTTP(w, r.WithContext(ctx))
			} else {
				RespondWithError(w, http.StatusUnauthorized, "Невалидные claims в токене")
			}
		})
	}
}

// AdminRequiredMiddleware проверяет, есть ли у пользователя роль "admin".
func AdminRequiredMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// ИЗМЕНЕНИЕ: Используем константу из contextkeys
		roles, ok := r.Context().Value(contextkeys.UserRolesContextKey).([]string)
		if !ok {
			RespondWithError(w, http.StatusForbidden, "Не удалось определить роли пользователя")
			return
		}

		isAdmin := false
		for _, role := range roles {
			if role == "admin" {
				isAdmin = true
				break
			}
		}

		if !isAdmin {
			RespondWithError(w, http.StatusForbidden, "Доступ запрещен: требуется роль администратора")
			return
		}

		next.ServeHTTP(w, r)
	})
}

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
