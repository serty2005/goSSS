package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"etalon-agent/internal/client"
	"etalon-agent/internal/config"
	"etalon-agent/internal/elevation"
	"etalon-agent/internal/runtime"
)

var AgentVersion = "0.1.0-dev"

type startupOptions struct {
	cleanup       bool
	cleanupAndRun bool
}

func main() {
	log.SetOutput(os.Stdout)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	options, err := parseStartupOptions(os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			log.Print(startupOptionsUsage())
			return
		}
		log.Fatalf("Ошибка разбора аргументов запуска: %v\n%s", err, startupOptionsUsage())
	}

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
	if options.cleanup || options.cleanupAndRun {
		log.Printf(
			"Запущена очистка локальных данных агента: registry_path=HKLM\\%s data_dir=%s",
			cfg.RegistryPath,
			cfg.DataDir,
		)
		if err := runtime.CleanupLocalData(cfg); err != nil {
			log.Fatalf("Ошибка очистки локальных данных агента: %v", err)
		}
		log.Printf("Очистка локальных данных агента завершена")
		if options.cleanup {
			return
		}
		log.Printf("Продолжаю запуск после очистки локальных данных")
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

func parseStartupOptions(args []string) (startupOptions, error) {
	var options startupOptions

	flags := flag.NewFlagSet("etalon-agent", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.BoolVar(&options.cleanup, "cleanup", false, "очистить локальные данные агента и завершить процесс")
	flags.BoolVar(&options.cleanupAndRun, "cleanup-and-run", false, "очистить локальные данные агента и продолжить запуск")

	if err := flags.Parse(args); err != nil {
		return startupOptions{}, err
	}
	if flags.NArg() > 0 {
		return startupOptions{}, fmt.Errorf("неподдерживаемые позиционные аргументы: %s", strings.Join(flags.Args(), ", "))
	}
	if options.cleanup && options.cleanupAndRun {
		return startupOptions{}, fmt.Errorf("нельзя одновременно указывать --cleanup и --cleanup-and-run")
	}
	return options, nil
}

func startupOptionsUsage() string {
	return strings.Join([]string{
		"Поддерживаемые режимы запуска etalon-agent:",
		"  --cleanup          очистить локальные данные агента и завершить процесс",
		"  --cleanup-and-run  очистить локальные данные агента и продолжить запуск с чистого состояния",
	}, "\n")
}
