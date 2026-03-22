package main

import (
	"context"
	"etalon-server/internal/infra/adapterstore"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/services"
	"flag"
	"fmt"
	"os"
	"strings"
)

type stringSliceFlag []string

func (s *stringSliceFlag) String() string {
	return strings.Join(*s, ",")
}

func (s *stringSliceFlag) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	*s = append(*s, value)
	return nil
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}

	cfg := config.New()
	if !cfg.AgentAdapterS3Enabled {
		fmt.Fprintln(os.Stderr, "AGENT_ADAPTER_S3_ENABLED должен быть true для publish/promote CLI")
		os.Exit(1)
	}

	log := logger.New("", "adapter-publisher", "info", true)
	ctx := context.Background()
	store, err := adapterstore.NewS3ObjectStore(ctx, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Не удалось инициализировать S3-клиент: %v\n", err)
		os.Exit(1)
	}

	publisher := services.NewAgentAdapterPublisher(log, store, cfg)
	switch os.Args[1] {
	case "publish":
		if err := runPublish(ctx, publisher, os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "Публикация завершилась ошибкой: %v\n", err)
			os.Exit(1)
		}
	case "promote":
		if err := runPromote(ctx, publisher, os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "Переключение канала завершилось ошибкой: %v\n", err)
			os.Exit(1)
		}
	case "-h", "--help", "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Неизвестная команда: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(2)
	}
}

func runPublish(ctx context.Context, publisher services.AgentAdapterPublisher, args []string) error {
	flags := flag.NewFlagSet("publish", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)

	var channels stringSliceFlag
	filePath := flags.String("file", "", "Путь к собранному бинарнику адаптера")
	adapterID := flags.String("adapter-id", "", "Стабильный adapter_id")
	version := flags.String("version", "", "Версия релиза")
	title := flags.String("title", "", "Человекочитаемое название адаптера")
	description := flags.String("description", "", "Описание релиза")
	adapterType := flags.String("adapter-type", "", "Тип адаптера; по умолчанию совпадает с adapter_id")
	targetOS := flags.String("target-os", "", "Целевая ОС, например windows")
	targetArch := flags.String("target-arch", "", "Целевая архитектура, например amd64")
	protocolVersion := flags.String("protocol-version", "1", "Версия протокола адаптера")
	flags.Var(&channels, "promote", "Канал для немедленного продвижения после публикации; флаг можно повторять")

	if err := flags.Parse(args); err != nil {
		return err
	}

	result, err := publisher.Publish(ctx, services.AgentAdapterPublishRequest{
		FilePath:        *filePath,
		AdapterID:       *adapterID,
		Version:         *version,
		Title:           *title,
		Description:     *description,
		AdapterType:     *adapterType,
		TargetOS:        *targetOS,
		TargetArch:      *targetArch,
		ProtocolVersion: *protocolVersion,
		PromoteChannels: channels,
	})
	if err != nil {
		return err
	}

	fmt.Printf("Релиз опубликован.\nrelease_key=%s\nbinary_key=%s\nsha256=%s\ndownload_url=%s\nchannels=%s\n",
		result.ReleaseKey,
		result.BinaryKey,
		result.SHA256,
		result.DownloadURL,
		strings.Join(result.Channels, ","),
	)
	return nil
}

func runPromote(ctx context.Context, publisher services.AgentAdapterPublisher, args []string) error {
	flags := flag.NewFlagSet("promote", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)

	var channels stringSliceFlag
	adapterID := flags.String("adapter-id", "", "Стабильный adapter_id")
	version := flags.String("version", "", "Версия уже опубликованного релиза")
	targetOS := flags.String("target-os", "", "Целевая ОС релиза")
	targetArch := flags.String("target-arch", "", "Целевая архитектура релиза")
	flags.Var(&channels, "channel", "Канал для переключения; флаг можно повторять")

	if err := flags.Parse(args); err != nil {
		return err
	}

	result, err := publisher.Promote(ctx, services.AgentAdapterPromoteRequest{
		AdapterID:  *adapterID,
		Version:    *version,
		TargetOS:   *targetOS,
		TargetArch: *targetArch,
		Channels:   channels,
	})
	if err != nil {
		return err
	}

	fmt.Printf("Каналы обновлены.\nrelease_key=%s\nchannels=%s\n",
		result.ReleaseKey,
		strings.Join(result.Channels, ","),
	)
	return nil
}

func printUsage() {
	fmt.Println(`Использование:
  adapter-publisher publish --file C:\path\adapter.exe --adapter-id fiscal-atol --version 1.2.3 --title "Фискальный адаптер АТОЛ" --target-os windows --target-arch amd64 --promote latest --promote stable
  adapter-publisher promote --adapter-id fiscal-atol --version 1.2.2 --target-os windows --target-arch amd64 --channel stable

Команды:
  publish   Загружает новый versioned release в S3, обновляет catalog/index.json и при необходимости channel pointers
  promote   Переключает stable/latest на уже опубликованный versioned release без перезагрузки бинарника`)
}
