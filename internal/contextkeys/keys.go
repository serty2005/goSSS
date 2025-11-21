package contextkeys

// ContextKey - тип для ключей контекста, чтобы избежать коллизий.
type ContextKey string

// Константы для ключей, используемых в context.Context для передачи данных между middleware и сервисами.
const (
	UserRolesContextKey ContextKey = "userRoles"
	UserIDContextKey    ContextKey = "userID"
	LoggerContextKey    ContextKey = "logger"
	TransactionKey      ContextKey = "tx"
)
