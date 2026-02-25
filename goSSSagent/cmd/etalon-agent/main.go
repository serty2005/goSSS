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
)

var AgentVersion = "0.1.0-dev"

func main() {
	log.SetOutput(os.Stdout)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	cfg, err := config.Load(AgentVersion)
	if err != nil {
		log.Fatalf("Ошибка загрузки конфигурации агента: %v", err)
	}

	httpClient := client.NewServiceDeskClient(cfg.ServerURL)
	app, err := runtime.NewAgent(cfg, httpClient)
	if err != nil {
		log.Fatalf("Ошибка создания агента: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Printf("Запуск %s версии %s", cfg.AgentProcessName, cfg.AgentVersion)
	if err := app.Run(ctx); err != nil {
		log.Fatalf("Агент завершился с ошибкой: %v", err)
	}
	log.Println("Агент остановлен")
}
