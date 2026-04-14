package middleware

import (
	"bufio"
	"context"
	"etalon-server/internal/contextkeys"
	"etalon-server/internal/domain/user"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/transport/http/response"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/golang-jwt/jwt/v5"
)

func TimeoutUnless(timeout time.Duration, skip func(*http.Request) bool) func(http.Handler) http.Handler {
	base := chiMiddleware.Timeout(timeout)
	return func(next http.Handler) http.Handler {
		withTimeout := base(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if skip != nil && skip(r) {
				next.ServeHTTP(w, r)
				return
			}
			withTimeout.ServeHTTP(w, r)
		})
	}
}

// LoggerInjector внедряет логгер с request-id в контекст запроса.
func LoggerInjector(baseLogger logger.LoggerInterface) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := chiMiddleware.GetReqID(r.Context())
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

func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hijacker.Hijack()
}

func (r *statusRecorder) ReadFrom(src io.Reader) (int64, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	if readerFrom, ok := r.ResponseWriter.(io.ReaderFrom); ok {
		n, err := readerFrom.ReadFrom(src)
		r.bytes += int(n)
		return n, err
	}
	return io.Copy(r, src)
}

func (r *statusRecorder) Push(target string, opts *http.PushOptions) error {
	pusher, ok := r.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return pusher.Push(target, opts)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// RequestLoggingMiddleware пишет один итоговый access-log на каждый HTTP-запрос.
func RequestLoggingMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log := GetLogger(r.Context())
			startedAt := time.Now()
			rec := &statusRecorder{ResponseWriter: w}

			next.ServeHTTP(rec, r)

			status := rec.status
			if status == 0 {
				status = http.StatusOK
			}

			args := []any{
				"event", "http_access",
				"method", r.Method,
				"path", r.URL.Path,
				"route", routePattern(r),
				"query", sanitizeQuery(r.URL.RawQuery),
				"status", status,
				"response_bytes", rec.bytes,
				"duration_ms", time.Since(startedAt).Milliseconds(),
				"remote_ip", r.RemoteAddr,
				"user_agent", r.UserAgent(),
				"content_length", r.ContentLength,
			}

			switch {
			case status >= http.StatusInternalServerError:
				log.Error("HTTP запрос завершён", args...)
			case status >= http.StatusBadRequest:
				log.Warn("HTTP запрос завершён", args...)
			default:
				log.Info("HTTP запрос завершён", args...)
			}
		})
	}
}

// Recoverer перехватывает панику и пишет её в JSON-лог.
func Recoverer() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if recovered := recover(); recovered != nil {
					log := GetLogger(r.Context())
					log.Error("Паника в HTTP-обработчике",
						"event", "http_panic",
						"method", r.Method,
						"path", r.URL.Path,
						"route", routePattern(r),
						"query", sanitizeQuery(r.URL.RawQuery),
						"panic", fmt.Sprint(recovered),
						"stacktrace", string(debug.Stack()),
					)

					if rec, ok := w.(*statusRecorder); ok && rec.status != 0 {
						return
					}
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				}
			}()

			next.ServeHTTP(w, r)
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

func routePattern(r *http.Request) string {
	rctx := chi.RouteContext(r.Context())
	if rctx == nil {
		return ""
	}
	pattern := rctx.RoutePattern()
	if pattern != "" {
		return pattern
	}
	return strings.Join(rctx.RoutePatterns, "")
}

func sanitizeQuery(rawQuery string) string {
	if strings.TrimSpace(rawQuery) == "" {
		return ""
	}

	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return ""
	}

	for key := range values {
		if isSensitiveQueryKey(key) {
			values[key] = []string{"[REDACTED]"}
		}
	}

	return values.Encode()
}

func isSensitiveQueryKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "token", "access_token", "refresh_token", "authorization", "password", "secret", "api_key", "apikey", "key":
		return true
	default:
		return false
	}
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
