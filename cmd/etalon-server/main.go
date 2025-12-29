package main

import (
	"etalon-server/internal/app"
	"flag"
	"log"
)

// @title Etalon Server API
// @version 1.0.0
// @description API documentation for Etalon Server ServiceDesk system

func main() {
	// Обработка флагов командной строки
	seedFlag := flag.Bool("seed", false, "Наполнить базу данных тестовыми данными из файлов и выйти.")
	flag.Parse()

	// Инициализация всего приложения
	application, err := app.New()
	if err != nil {
		log.Fatalf("Не удалось инициализировать приложение: %v", err)
	}

	// Если передан флаг --seed, запускаем наполнение и выходим
	if *seedFlag {
		application.SeedDBAndExit()
	}

	// Запуск сервера и фоновых сервисов в обычном режиме
	application.Run()
}
