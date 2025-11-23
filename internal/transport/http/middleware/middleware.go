package middleware

import (
	"context"
	"encoding/json"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/transport/http/dtos"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/golang-jwt/jwt/v5"
)

// ContextKey - тип для ключей контекста, чтобы избежать коллизий.
type ContextKey string

// Константы для ключей, используемых в context.Context
const (
	UserRolesContextKey ContextKey = "userRoles"
	UserIDContextKey    ContextKey = "userID"
	LoggerContextKey    ContextKey = "logger"
)

// LoggerInjector — это middleware, которое внедряет логгер с request-id в контекст запроса.
func LoggerInjector(baseLogger logger.LoggerInterface) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := middleware.GetReqID(r.Context())
			ctxLogger := baseLogger.With("request_id", requestID)
			ctx := context.WithValue(r.Context(), LoggerContextKey, ctxLogger)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetLogger извлекает логгер из контекста. Если логгер не найден, возвращает no-op логгер.
func GetLogger(ctx context.Context) logger.LoggerInterface {
	if l, ok := ctx.Value(LoggerContextKey).(logger.LoggerInterface); ok && l != nil {
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
				RespondWithError(w, http.StatusUnauthorized, "Отсутствует токен авторизации")
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
				RespondWithError(w, http.StatusUnauthorized, "Невалидный токен")
				return
			}

			// 5. Извлечение claims и сохранение в контекст
			if claims, ok := token.Claims.(jwt.MapClaims); ok {
				ctx := r.Context()
				if sub, ok := claims["sub"].(string); ok {
					ctx = context.WithValue(ctx, UserIDContextKey, sub)
				} else {
					RespondWithError(w, http.StatusUnauthorized, "Невалидный sub в токене")
					return
				}
				if roles, ok := claims["roles"].([]interface{}); ok {
					var rolesStr []string
					for _, rl := range roles {
						if role, ok := rl.(string); ok {
							rolesStr = append(rolesStr, role)
						}
					}
					ctx = context.WithValue(ctx, UserRolesContextKey, rolesStr)
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
		roles, ok := r.Context().Value(UserRolesContextKey).([]string)
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

// RespondWithError и RespondWithJSON теперь тоже часть этого пакета
func RespondWithError(w http.ResponseWriter, code int, message string) {
	RespondWithJSON(w, code, dtos.ErrorResponseDTO{Error: message})
}

func RespondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	response, err := json.Marshal(payload)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(response)
}
