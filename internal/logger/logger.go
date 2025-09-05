package logger

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/natefinch/lumberjack"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// New инициализирует логгер zap.
// Он может писать логи как в консоль, так и в файл с ротацией.
func New(logDir, loggerName, logLevel string, disableFileLogging bool) *zap.Logger {
	level := getZapLevel(logLevel)
	// Конфигурация для логгирования в консоль
	consoleCore := zapcore.NewCore(
		zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig()),
		zapcore.Lock(os.Stdout),
		level,
	)

	cores := []zapcore.Core{consoleCore}

	// Конфигурация для логгирования в файл с ротацией
	if !disableFileLogging {
		// Создаем директорию для логов, если ее нет
		if err := os.MkdirAll(logDir, 0755); err != nil {
			// В случае ошибки просто не будем писать в файл
			consoleLogger := zap.New(consoleCore)
			consoleLogger.Error("Не удалось создать директорию для логов, файловое логирование отключено.", zap.String("dir", logDir), zap.Error(err))
		} else {
			logPath := filepath.Join(logDir, loggerName+".log")
			fileWriter := zapcore.AddSync(&lumberjack.Logger{
				Filename:   logPath,
				MaxSize:    10, // megabytes
				MaxBackups: 3,
				MaxAge:     28, // days
				Compress:   true,
			})

			fileCore := zapcore.NewCore(
				zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
				fileWriter,
				level,
			)
			cores = append(cores, fileCore)
		}
	}

	// Объединяем ядра для вывода в несколько мест
	core := zapcore.NewTee(cores...)

	// Создаем логгер с опцией AddCaller для вывода имени файла и строки
	logger := zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel), zap.Fields(zap.String("logger", loggerName)))

	return logger
}

// getZapLevel преобразует строковый уровень логирования в zapcore.Level.
func getZapLevel(level string) zapcore.Level {
	switch strings.ToLower(level) {
	case "debug":
		return zap.DebugLevel
	case "info":
		return zap.InfoLevel
	case "warn", "warning":
		return zap.WarnLevel
	case "error":
		return zap.ErrorLevel
	case "fatal":
		return zap.FatalLevel
	default:
		return zap.InfoLevel
	}
}
