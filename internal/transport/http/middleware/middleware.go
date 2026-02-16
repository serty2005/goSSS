package middleware

import (
	"context"
	"etalon-server/internal/contextkeys"
	"etalon-server/internal/domain/user"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/transport/http/response"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/golang-jwt/jwt/v5"
)

// LoggerInjector внедряет логгер с request-id в контекст запроса.
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

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(statusCode int) {
	r.status = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *statusRecorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(p)
	r.bytes += n
	return n, err
}

// DebugHTTPIOMiddleware логирует входящие/исходящие HTTP-данные на уровне debug.
// Логи записываются только если в конфигурации включен debug-уровень логгера.
func DebugHTTPIOMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log := GetLogger(r.Context())
			startedAt := time.Now()
			rec := &statusRecorder{ResponseWriter: w}

			log.Debug("HTTP входящий запрос",
				"method", r.Method,
				"path", r.URL.Path,
				"query", r.URL.RawQuery,
				"remote_addr", r.RemoteAddr,
				"user_agent", r.UserAgent(),
				"content_length", r.ContentLength,
			)

			next.ServeHTTP(rec, r)

			duration := time.Since(startedAt)
			status := rec.status
			if status == 0 {
				status = http.StatusOK
			}

			log.Debug("HTTP исходящий ответ",
				"method", r.Method,
				"path", r.URL.Path,
				"status", status,
				"response_bytes", rec.bytes,
				"duration_ms", duration.Milliseconds(),
			)
		})
	}
}

// GetLogger извлекает логгер из контекста.
func GetLogger(ctx context.Context) logger.LoggerInterface {
	if l, ok := ctx.Value(contextkeys.LoggerContextKey).(logger.LoggerInterface); ok && l != nil {
		return l
	}
	return logger.NewSlogLogger("", "fallback", "error", true)
}

// JwtAuthMiddleware проверяет JWT токен и сохраняет данные пользователя в контексте.
func JwtAuthMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var tokenString string

			authHeader := r.Header.Get("Authorization")
			if authHeader != "" {
				parts := strings.Split(authHeader, " ")
				if len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
					tokenString = parts[1]
				}
			}

			if tokenString == "" {
				tokenString = r.URL.Query().Get("token")
			}

			if tokenString == "" {
				response.RespondWithError(w, http.StatusUnauthorized, "Отсутствует токен авторизации")
				return
			}

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

// AdminRequiredMiddleware оставлен для совместимости.
func AdminRequiredMiddleware(next http.Handler) http.Handler {
	return RequireAnyRole(user.RoleAdmin)(next)
}

// RequireAnyRole проверяет наличие хотя бы одной разрешённой роли.
func RequireAnyRole(allowedRoles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			roles, ok := r.Context().Value(contextkeys.UserRolesContextKey).([]string)
			if !ok {
				response.RespondWithError(w, http.StatusForbidden, "Не удалось определить роли пользователя")
				return
			}

			allowed := make(map[string]struct{}, len(allowedRoles))
			for _, role := range allowedRoles {
				allowed[role] = struct{}{}
			}

			for _, role := range roles {
				if _, exists := allowed[role]; exists {
					next.ServeHTTP(w, r)
					return
				}
			}

			response.RespondWithError(w, http.StatusForbidden, "Доступ запрещён для вашей должности")
		})
	}
}

// AgentAuthMiddleware проверяет Bearer токен для агентов.
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
