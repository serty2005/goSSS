package db

import (
	"errors"
	"net/url"
	"path"
	"strings"

	"etalon-server/internal/domain/models"
	"etalon-server/internal/infra/config"

	"gorm.io/gorm"
)

// MigrateLegacyPublishedAgentAdapters переносит старые записи PublishedAgentAdapter
// в release/channel-модель, не меняя текущий выбор оператора по adapter_id.
func MigrateLegacyPublishedAgentAdapters(db *gorm.DB) error {
	var legacyItems []models.PublishedAgentAdapter
	if err := db.Find(&legacyItems).Error; err != nil {
		return err
	}

	for _, legacyItem := range legacyItems {
		release := models.AgentAdapterRelease{
			AdapterID:       normalizeLegacyValue(legacyItem.AdapterID),
			Version:         strings.TrimSpace(legacyItem.Version),
			Title:           strings.TrimSpace(legacyItem.Title),
			Description:     strings.TrimSpace(legacyItem.Description),
			AdapterType:     normalizeLegacyValue(legacyItem.AdapterType),
			TargetOS:        normalizeLegacyValue(legacyItem.TargetOS),
			TargetArch:      normalizeLegacyValue(legacyItem.TargetArch),
			ProtocolVersion: strings.TrimSpace(legacyItem.ProtocolVersion),
			FileName:        strings.TrimSpace(legacyItem.FileName),
			DownloadURL:     strings.TrimSpace(legacyItem.DownloadURL),
			SHA256:          normalizeLegacyValue(legacyItem.SHA256),
			SourceKey:       sourceKeyFromLegacyDownloadURL(legacyItem.DownloadURL, legacyItem.FileName, legacyItem.AdapterID),
			Published:       legacyItem.Published,
			CreatedAt:       legacyItem.CreatedAt,
			UpdatedAt:       legacyItem.UpdatedAt,
		}

		var existingRelease models.AgentAdapterRelease
		err := db.Where(
			"adapter_id = ? AND version = ? AND target_os = ? AND target_arch = ?",
			release.AdapterID,
			release.Version,
			release.TargetOS,
			release.TargetArch,
		).First(&existingRelease).Error
		switch {
		case err == nil:
			release.ID = existingRelease.ID
		case errors.Is(err, gorm.ErrRecordNotFound):
			if err := db.Create(&release).Error; err != nil {
				return err
			}
		case err != nil:
			return err
		}

		for _, channelName := range []string{"stable", "latest"} {
			channel := models.AgentAdapterChannel{
				AdapterID: release.AdapterID,
				Channel:   channelName,
				ReleaseID: release.ID,
				UpdatedAt: release.UpdatedAt,
			}
			if err := db.Where("adapter_id = ? AND channel = ?", channel.AdapterID, channel.Channel).
				Attrs(channel).
				FirstOrCreate(&models.AgentAdapterChannel{}).
				Error; err != nil {
				return err
			}
		}
	}

	return nil
}

// EnsureDefaultAgentAdapterCatalog подготавливает demo-каталог для локальной разработки,
// если S3 source of truth отключён и в новой release/channel-модели ещё нет записей.
func EnsureDefaultAgentAdapterCatalog(cfg *config.Config, db *gorm.DB) error {
	if cfg != nil && cfg.AgentAdapterS3Enabled {
		return nil
	}

	var count int64
	if err := db.Model(&models.AgentAdapterRelease{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	defaults := defaultDemoAgentAdapterReleases()
	for _, release := range defaults {
		current := release
		if err := db.Where(
			"adapter_id = ? AND version = ? AND target_os = ? AND target_arch = ?",
			current.AdapterID,
			current.Version,
			current.TargetOS,
			current.TargetArch,
		).Attrs(current).FirstOrCreate(&models.AgentAdapterRelease{}).Error; err != nil {
			return err
		}

		var persisted models.AgentAdapterRelease
		if err := db.Where(
			"adapter_id = ? AND version = ? AND target_os = ? AND target_arch = ?",
			current.AdapterID,
			current.Version,
			current.TargetOS,
			current.TargetArch,
		).First(&persisted).Error; err != nil {
			return err
		}

		for _, channelName := range []string{"stable", "latest"} {
			channel := models.AgentAdapterChannel{
				AdapterID: persisted.AdapterID,
				Channel:   channelName,
				ReleaseID: persisted.ID,
			}
			if err := db.Where("adapter_id = ? AND channel = ?", channel.AdapterID, channel.Channel).
				Assign(channel).
				FirstOrCreate(&models.AgentAdapterChannel{}).
				Error; err != nil {
				return err
			}
		}
	}

	return nil
}

// EnsureDefaultPublishedAgentAdapters сохранён как совместимый враппер для старых тестов.
func EnsureDefaultPublishedAgentAdapters(db *gorm.DB) error {
	return EnsureDefaultAgentAdapterCatalog(&config.Config{}, db)
}

func defaultDemoAgentAdapterReleases() []models.AgentAdapterRelease {
	return []models.AgentAdapterRelease{
		buildDemoRelease(
			"fiscal-atol",
			"Фискальный адаптер АТОЛ",
			"Demo-релиз для локальной проверки server-side выдачи адаптера АТОЛ.",
			"a1",
		),
		buildDemoRelease(
			"fiscal-mitsu",
			"Фискальный адаптер Mitsu",
			"Demo-релиз для локальной проверки server-side выдачи адаптера Mitsu.",
			"b2",
		),
		buildDemoRelease(
			"fiscal-shtrih",
			"Фискальный адаптер Штрих",
			"Demo-релиз для локальной проверки server-side выдачи адаптера Штрих.",
			"c3",
		),
	}
}

func buildDemoRelease(adapterID, title, description, shaChunk string) models.AgentAdapterRelease {
	fileName := adapterID + "-0.1.0-demo.exe"
	sourceKey := path.Join("adapters", adapterID, "releases", "0.1.0-demo", "windows", "amd64", fileName)
	return models.AgentAdapterRelease{
		AdapterID:       adapterID,
		Version:         "0.1.0-demo",
		Title:           title,
		Description:     description,
		AdapterType:     adapterID,
		TargetOS:        "windows",
		TargetArch:      "amd64",
		ProtocolVersion: "1",
		FileName:        fileName,
		DownloadURL:     "https://example.test/agents/" + sourceKey,
		SHA256:          strings.Repeat(shaChunk, 32),
		SourceKey:       sourceKey,
		Published:       true,
	}
}

func normalizeLegacyValue(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func sourceKeyFromLegacyDownloadURL(downloadURL, fileName, adapterID string) string {
	parsed, err := url.Parse(strings.TrimSpace(downloadURL))
	if err == nil {
		if sourceKey := strings.Trim(strings.TrimSpace(parsed.Path), "/"); sourceKey != "" {
			return sourceKey
		}
	}

	fileName = strings.TrimSpace(fileName)
	if fileName == "" {
		return ""
	}
	return path.Join("legacy", normalizeLegacyValue(adapterID), fileName)
}
