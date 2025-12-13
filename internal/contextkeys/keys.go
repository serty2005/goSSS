package contextkeys

// ContextKey - тип для ключей контекста, чтобы избежать коллизий.
type ContextKey string

// Константы для ключей, используемых в context.Context
const (
	UserRolesContextKey ContextKey = "userRoles"
	UserIDContextKey    ContextKey = "userID" // Хранит string (из JWT sub) или uint после парсинга
	LoggerContextKey    ContextKey = "logger"
	TransactionKey      ContextKey = "tx"
)
