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
	seedFlag := flag.Bool("seed", false, "Наполнить базу данных тестовыми данными из файлов и выйти.")
	reverseSeedFlag := flag.Bool("reverse-seed", false, "Выгрузить мок-данные из текущей БД и выйти.")
	flag.Parse()

	application, err := app.New()
	if err != nil {
		log.Fatalf("Не удалось инициализировать приложение: %v", err)
	}

	if *seedFlag {
		application.SeedDBAndExit()
	}
	if *reverseSeedFlag {
		application.ReverseSeedDBAndExit()
	}

	application.Run()
}
