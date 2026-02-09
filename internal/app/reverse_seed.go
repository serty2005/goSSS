package app

import "os"

func (a *Application) ReverseSeedDBAndExit() {
	a.Logger.Info("Запуск в режиме обратного сидера (выгрузка мок-данных из БД)...")
	outputDir := "./tools/seeder/mock_data"
	if err := a.Seeder.ExportDatabaseToMockData(outputDir, a.Config.TicketStoragePath); err != nil {
		a.Logger.Fatal("Ошибка при обратном сидировании", "error", err)
	}
	a.Logger.Info("Выгрузка мок-данных завершена успешно", "path", outputDir)
	os.Exit(0)
}
