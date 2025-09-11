package main

import (
	"etalon-server/internal/infra/logger"
)

func main() {
	// Тестируем новый логгер
	log := logger.NewSlogLogger("./logs", "test", "info", true)

	log.Info("Тестовое сообщение инфо", "key", "value")
	log.Debug("Тестовое сообщение дебаг", "debug_key", "debug_value") // Не должно выводиться при уровне info
	log.Error("Тестовое сообщение ошибка", "error_key", "error_value")
	log.Warn("Тестовое сообщение предупреждение", "warn_key", "warn_value")

	// Тест контекста
	ctxLog := log.With("component", "test_component")
	ctxLog.Info("Сообщение с контекстом", "ctx_key", "ctx_value")

	// Тест Fatal (завершит программу)
	// log.Fatal("Фатальная ошибка", "fatal_key", "fatal_value")
}
