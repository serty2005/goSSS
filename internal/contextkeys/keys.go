package contextkeys

import "context"

// ContextKey - тип для ключей контекста, чтобы избежать коллизий.
type ContextKey string

// Константы для ключей, используемых в context.Context
const (
	UserRolesContextKey ContextKey = "userRoles"
	UserIDContextKey    ContextKey = "userID" // Хранит string (из JWT sub) или uint после парсинга
	LoggerContextKey    ContextKey = "logger"
	TransactionKey      ContextKey = "tx"
	TraceID             ContextKey = "trace_id" // Сквозной идентификатор трассировки
)

// GetTraceID извлекает trace_id из контекста.
func GetTraceID(ctx context.Context) string {
	if v := ctx.Value(TraceID); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// WithTraceID добавляет trace_id в контекст.
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, TraceID, traceID)
}
