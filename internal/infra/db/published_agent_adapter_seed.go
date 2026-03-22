package db

import (
	"etalon-server/internal/domain/models"
	"strings"

	"gorm.io/gorm"
)

// EnsureDefaultPublishedAgentAdapters подготавливает минимальный demo-каталог опубликованных адаптеров.
// URL и sha256 здесь тестовые и должны быть заменены перед реальной поставкой бинарников.
func EnsureDefaultPublishedAgentAdapters(db *gorm.DB) error {
	defaults := []models.PublishedAgentAdapter{
		{
			AdapterID:       "fiscal-atol",
			Title:           "Фискальный адаптер АТОЛ",
			Description:     "Опубликованный demo-manifest для локальной проверки выдачи адаптера АТОЛ.",
			Published:       true,
			Version:         "0.1.0-demo",
			AdapterType:     "fiscal-atol",
			TargetOS:        "windows",
			TargetArch:      "amd64",
			ProtocolVersion: "1",
			DownloadURL:     "https://example.test/adapters/fiscal-atol-0.1.0-demo.exe",
			SHA256:          strings.Repeat("a1", 32),
			FileName:        "fiscal-atol-0.1.0-demo.exe",
		},
		{
			AdapterID:       "fiscal-mitsu",
			Title:           "Фискальный адаптер Mitsu",
			Description:     "Опубликованный demo-manifest для локальной проверки выдачи адаптера Mitsu.",
			Published:       true,
			Version:         "0.1.0-demo",
			AdapterType:     "fiscal-mitsu",
			TargetOS:        "windows",
			TargetArch:      "amd64",
			ProtocolVersion: "1",
			DownloadURL:     "https://example.test/adapters/fiscal-mitsu-0.1.0-demo.exe",
			SHA256:          strings.Repeat("b2", 32),
			FileName:        "fiscal-mitsu-0.1.0-demo.exe",
		},
		{
			AdapterID:       "fiscal-shtrih",
			Title:           "Фискальный адаптер Штрих",
			Description:     "Опубликованный demo-manifest для локальной проверки выдачи адаптера Штрих.",
			Published:       true,
			Version:         "0.1.0-demo",
			AdapterType:     "fiscal-shtrih",
			TargetOS:        "windows",
			TargetArch:      "amd64",
			ProtocolVersion: "1",
			DownloadURL:     "https://example.test/adapters/fiscal-shtrih-0.1.0-demo.exe",
			SHA256:          strings.Repeat("c3", 32),
			FileName:        "fiscal-shtrih-0.1.0-demo.exe",
		},
	}

	for _, item := range defaults {
		if err := db.
			Where(models.PublishedAgentAdapter{AdapterID: item.AdapterID}).
			Attrs(item).
			FirstOrCreate(&models.PublishedAgentAdapter{}).
			Error; err != nil {
			return err
		}
	}

	return nil
}
