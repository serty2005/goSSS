package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"etalon-agent/internal/client"
	"etalon-agent/internal/config"
	"etalon-agent/internal/runtime"
	"etalon-agent/internal/services"
)

var AgentVersion = "0.1.0-dev"

func main() {
	log.SetOutput(os.Stdout)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	cfg, err := config.LoadFromEnv(AgentVersion)
	if err != nil {
		log.Fatalf("Ошибка загрузки конфигурации: %v", err)
	}

	uuidService, err := services.NewUUIDService(cfg.DataDir)
	if err != nil {
		log.Fatalf("Ошибка инициализации UUID агента: %v", err)
	}

	httpClient := client.NewServiceDeskClient(cfg.ServerURL, cfg.APIKey)
	app, err := runtime.NewAgent(cfg, uuidService, httpClient)
	if err != nil {
		log.Fatalf("Ошибка создания агента: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Printf("Запуск агента версии %s (uuid=%s)", cfg.AgentVersion, uuidService.Get())
	if err := app.Run(ctx); err != nil {
		log.Fatalf("Агент завершился с ошибкой: %v", err)
	}
	log.Println("Агент остановлен")
}
