package middleware

import (
	"context"
	"etalon-server/internal/contextkeys"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/transport/http/response"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/golang-jwt/jwt/v5"
)

// LoggerInjector — это middleware, которое внедряет логгер с request-id в контекст запроса.
func LoggerInjector(baseLogger logger.LoggerInterface) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := middleware.GetReqID(r.Context())
			ctxLogger := baseLogger.With("request_id", requestID)
			ctx := context.WithValue(r.Context(), contextkeys.LoggerContextKey, ctxLogger)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetLogger извлекает логгер из контекста. Если логгер не найден, возвращает no-op логгер.
func GetLogger(ctx context.Context) logger.LoggerInterface {
	if l, ok := ctx.Value(contextkeys.LoggerContextKey).(logger.LoggerInterface); ok && l != nil {
		return l
	}
	// Возвращаем "пустой" логгер, чтобы избежать паники, если что-то пошло не так
	return logger.NewSlogLogger("", "fallback", "error", true)
}

// JwtAuthMiddleware проверяет JWT токен и добавляет информацию о пользователе в контекст.
func JwtAuthMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var tokenString string

			// 1. Сначала пробуем извлечь токен из заголовка Authorization
			authHeader := r.Header.Get("Authorization")
			if authHeader != "" {
				// Проверяем формат "Bearer <token>"
				parts := strings.Split(authHeader, " ")
				if len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
					tokenString = parts[1]
				}
			}

			// 2. Если в заголовке токена нет, ищем в Query параметрах (для SSE)
			if tokenString == "" {
				tokenString = r.URL.Query().Get("token")
			}

			// 3. Если токен все еще не найден — ошибка
			if tokenString == "" {
				response.RespondWithError(w, http.StatusUnauthorized, "Отсутствует токен авторизации")
				return
			}

			// 4. Парсинг и валидация токена
			token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
				if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, fmt.Errorf("неожиданный метод подписи: %v", token.Header["alg"])
				}
				return []byte(cfg.JWTSecret), nil
			})

			if err != nil || !token.Valid {
				response.RespondWithError(w, http.StatusUnauthorized, "Невалидный токен")
				return
			}

			// 5. Извлечение claims и сохранение в контекст
			if claims, ok := token.Claims.(jwt.MapClaims); ok {
				ctx := r.Context()
				if sub, ok := claims["sub"].(string); ok {
					ctx = context.WithValue(ctx, contextkeys.UserIDContextKey, sub)
				} else {
					response.RespondWithError(w, http.StatusUnauthorized, "Невалидный sub в токене")
					return
				}
				if roles, ok := claims["roles"].([]interface{}); ok {
					var rolesStr []string
					for _, rl := range roles {
						if role, ok := rl.(string); ok {
							rolesStr = append(rolesStr, role)
						}
					}
					ctx = context.WithValue(ctx, contextkeys.UserRolesContextKey, rolesStr)
				} else {
					response.RespondWithError(w, http.StatusUnauthorized, "Невалидные roles в токене")
					return
				}
				next.ServeHTTP(w, r.WithContext(ctx))
			} else {
				response.RespondWithError(w, http.StatusUnauthorized, "Невалидные claims в токене")
			}
		})
	}
}

// AdminRequiredMiddleware проверяет, есть ли у пользователя роль "admin".
func AdminRequiredMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		roles, ok := r.Context().Value(contextkeys.UserRolesContextKey).([]string)
		if !ok {
			response.RespondWithError(w, http.StatusForbidden, "Не удалось определить роли пользователя")
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
			response.RespondWithError(w, http.StatusForbidden, "Доступ запрещен: требуется роль администратора")
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
				response.RespondWithError(w, http.StatusInternalServerError, "Сервер не настроен для аутентификации агентов")
				return
			}

			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				response.RespondWithError(w, http.StatusUnauthorized, "Отсутствует заголовок Authorization")
				return
			}

			headerParts := strings.Split(authHeader, " ")
			if len(headerParts) != 2 || strings.ToLower(headerParts[0]) != "bearer" {
				response.RespondWithError(w, http.StatusUnauthorized, "Неверный формат заголовка Authorization")
				return
			}

			token := headerParts[1]
			if token != apiKey {
				response.RespondWithError(w, http.StatusUnauthorized, "Неверный ключ API агента")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
