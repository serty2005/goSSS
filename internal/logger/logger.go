package logger

import (
	"os"

	"github.com/natefinch/lumberjack"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// New инициализирует логгер zap.
// Он может писать логи как в консоль, так и в файл с ротацией.
func New(logPath string, disableFileLogging bool) *zap.Logger {
	// Конфигурация для логгирования в консоль
	consoleCore := zapcore.NewCore(
		zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig()),
		zapcore.Lock(os.Stdout),
		zap.InfoLevel,
	)

	cores := []zapcore.Core{consoleCore}

	// Конфигурация для логгирования в файл с ротацией
	if !disableFileLogging {
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
			zap.InfoLevel,
		)
		cores = append(cores, fileCore)
	}

	// Объединяем ядра для вывода в несколько мест
	core := zapcore.NewTee(cores...)

	// Создаем логгер с опцией AddCaller для вывода имени файла и строки
	logger := zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))

	return logger
}
