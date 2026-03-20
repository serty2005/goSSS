package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"etalon-agent/internal/client"
	"etalon-agent/internal/config"
	"etalon-agent/internal/elevation"
	"etalon-agent/internal/runtime"
)

var AgentVersion = "0.1.0-dev"

func main() {
	log.SetOutput(os.Stdout)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	relaunched, err := elevation.EnsureAdmin()
	if err != nil {
		log.Fatalf("Не удалось запросить запуск от имени администратора: %v", err)
	}
	if relaunched {
		log.Println("Запрошен перезапуск агента с правами администратора")
		return
	}

	cfg, err := config.Load(AgentVersion)
	if err != nil {
		log.Fatalf("Ошибка загрузки конфигурации агента: %v", err)
	}
	log.Printf(
		"Конфигурация запуска подготовлена: source=встроена_в_бинарник server_url=%s registry_path=HKLM\\%s data_dir=%s adapter_dir=%s",
		cfg.ServerURL,
		cfg.RegistryPath,
		cfg.DataDir,
		cfg.AdapterDir,
	)

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
