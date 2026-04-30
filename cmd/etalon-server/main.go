package main

import (
	"etalon-server/internal/app"
	"etalon-server/internal/infra/logger"
	"flag"
	"os"
)

// @title XenionDesk API
// @version 1.1.1
// @description API documentation for XenionDesk system

func main() {
	bootstrapLog := logger.ConfigureStdLogger("bootstrap", os.Getenv("LOG_LEVEL"))

	seedFlag := flag.Bool("seed", false, "Наполнить базу данных тестовыми данными из файлов и выйти.")
	seedFTPCacheFlag := flag.Bool("seed-ftp-cache", false, "Инициализировать БД из FTP-кэша и выйти.")
	reverseSeedFlag := flag.Bool("reverse-seed", false, "Выгрузить мок-данные из текущей БД и выйти.")
	flag.Parse()

	application, err := app.New()
	if err != nil {
		bootstrapLog.Fatal("Не удалось инициализировать приложение", "error", err)
	}

	// Каждый режим работы завершает программу через os.Exit()
	// поэтому используем if-else для взаимно исключающих режимов
	if *seedFlag {
		application.SeedDBAndExit()
		// SeedDBAndExit вызывает os.Exit(0), код далее не выполняется
	}

	if *seedFTPCacheFlag {
		application.SeedFromFTPCacheAndExit()
		// SeedFromFTPCacheAndExit вызывает os.Exit(0), код далее не выполняется
	}

	if *reverseSeedFlag {
		application.ReverseSeedDBAndExit()
		// ReverseSeedDBAndExit вызывает os.Exit(0), код далее не выполняется
	}

	// Обычный режим работы - запуск HTTP-сервера
	application.Run()

	// Явный выход с кодом успеха после корректного завершения Run()
	os.Exit(0)
}
