package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"etalon-server/internal/domain/models"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"

	"gorm.io/gorm"
)

type agentAdapterCatalogSyncService struct {
	db             *gorm.DB
	logger         logger.LoggerInterface
	store          AgentAdapterObjectStore
	enabled        bool
	catalogKey     string
	defaultChannel string
	interval       time.Duration
	publicBaseURL  string
	now            func() time.Time
}

type noopAgentAdapterCatalogSyncService struct{}

type loadedAgentAdapterCatalog struct {
	adaptersCount int
	releases      []models.AgentAdapterRelease
	channels      []loadedAgentAdapterChannel
}

type loadedAgentAdapterChannel struct {
	AdapterID  string
	Channel    string
	ReleaseKey string
}

func NewAgentAdapterCatalogSyncService(
	db *gorm.DB,
	log logger.LoggerInterface,
	store AgentAdapterObjectStore,
	cfg *config.Config,
) AgentAdapterCatalogSyncService {
	if cfg == nil || !cfg.AgentAdapterCatalog.Enabled || store == nil {
		return noopAgentAdapterCatalogSyncService{}
	}

	defaultChannel := normalizeAgentAdapterChannel(cfg.AgentAdapterCatalog.DefaultChannel)
	return &agentAdapterCatalogSyncService{
		db:             db,
		logger:         log,
		store:          store,
		enabled:        true,
		catalogKey:     normalizeObjectKey(cfg.AgentAdapterCatalog.CatalogKey),
		defaultChannel: defaultChannel,
		interval:       max(time.Minute, cfg.AgentAdapterCatalog.SyncInterval),
		publicBaseURL:  normalizePublicBaseURL(cfg.AgentAdapterCatalog.PublicBaseURL),
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (noopAgentAdapterCatalogSyncService) Start(context.Context) {}

func (noopAgentAdapterCatalogSyncService) Refresh(context.Context) (AgentAdapterCatalogRefreshResult, error) {
	return AgentAdapterCatalogRefreshResult{}, ErrAgentAdapterCatalogDisabled
}

func (s *agentAdapterCatalogSyncService) Start(ctx context.Context) {
	if !s.enabled {
		return
	}

	s.runRefresh(ctx, "startup")

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runRefresh(ctx, "scheduled")
		}
	}
}

func (s *agentAdapterCatalogSyncService) Refresh(ctx context.Context) (AgentAdapterCatalogRefreshResult, error) {
	if !s.enabled {
		return AgentAdapterCatalogRefreshResult{}, ErrAgentAdapterCatalogDisabled
	}

	loadedCatalog, err := s.loadCatalog(ctx)
	if err != nil {
		return AgentAdapterCatalogRefreshResult{}, err
	}

	return s.applyCatalog(ctx, loadedCatalog)
}

func (s *agentAdapterCatalogSyncService) runRefresh(ctx context.Context, reason string) {
	result, err := s.Refresh(ctx)
	if err != nil {
		s.logger.Warn("Не удалось синхронизировать каталог адаптеров из S3",
			"reason", reason,
			"catalog_key", s.catalogKey,
			"error", err,
		)
		return
	}

	s.logger.Info("Каталог адаптеров из S3 синхронизирован",
		"reason", reason,
		"catalog_key", s.catalogKey,
		"adapters", result.AdaptersCount,
		"releases_upserted", result.ReleasesUpserted,
		"channels_upserted", result.ChannelsUpserted,
		"releases_deleted", result.ReleasesDeleted,
		"channels_deleted", result.ChannelsDeleted,
	)
}

func (s *agentAdapterCatalogSyncService) loadCatalog(ctx context.Context) (loadedAgentAdapterCatalog, error) {
	index, err := s.loadCatalogIndex(ctx)
	if err != nil {
		return loadedAgentAdapterCatalog{}, err
	}

	releaseByLookupKey := make(map[string]models.AgentAdapterRelease)
	releaseKeyToLookupKey := make(map[string]string)
	adapterIDs := make([]string, 0, len(index.Adapters))

	for _, adapterEntry := range index.Adapters {
		if adapterEntry.AdapterID == "" {
			return loadedAgentAdapterCatalog{}, errors.New("catalog/index.json содержит запись адаптера без adapter_id")
		}
		adapterIDs = append(adapterIDs, adapterEntry.AdapterID)

		for _, releaseEntry := range adapterEntry.Releases {
			if releaseEntry.ReleaseKey == "" {
				return loadedAgentAdapterCatalog{}, fmt.Errorf("catalog/index.json содержит release без release_key для адаптера %s", adapterEntry.AdapterID)
			}

			releaseManifest, err := s.loadReleaseManifest(ctx, adapterEntry, releaseEntry)
			if err != nil {
				return loadedAgentAdapterCatalog{}, err
			}

			lookupKey := adapterReleaseLookupKey(
				releaseManifest.AdapterID,
				releaseManifest.Version,
				releaseManifest.TargetOS,
				releaseManifest.TargetArch,
			)
			if _, exists := releaseByLookupKey[lookupKey]; exists {
				return loadedAgentAdapterCatalog{}, fmt.Errorf(
					"catalog/index.json содержит дублирующий релиз %s/%s/%s/%s",
					releaseManifest.AdapterID,
					releaseManifest.Version,
					releaseManifest.TargetOS,
					releaseManifest.TargetArch,
				)
			}

			releaseByLookupKey[lookupKey] = releaseManifest
			releaseKeyToLookupKey[releaseEntry.ReleaseKey] = lookupKey
		}
	}

	channels, err := s.loadChannels(ctx, uniqueNonEmptyStrings(adapterIDs), releaseKeyToLookupKey)
	if err != nil {
		return loadedAgentAdapterCatalog{}, err
	}

	releases := make([]models.AgentAdapterRelease, 0, len(releaseByLookupKey))
	for _, release := range releaseByLookupKey {
		releases = append(releases, release)
	}
	slices.SortFunc(releases, func(left, right models.AgentAdapterRelease) int {
		switch {
		case left.AdapterID != right.AdapterID:
			return strings.Compare(left.AdapterID, right.AdapterID)
		case left.Version != right.Version:
			return strings.Compare(left.Version, right.Version)
		case left.TargetOS != right.TargetOS:
			return strings.Compare(left.TargetOS, right.TargetOS)
		default:
			return strings.Compare(left.TargetArch, right.TargetArch)
		}
	})

	slices.SortFunc(channels, func(left, right loadedAgentAdapterChannel) int {
		switch {
		case left.AdapterID != right.AdapterID:
			return strings.Compare(left.AdapterID, right.AdapterID)
		default:
			return strings.Compare(left.Channel, right.Channel)
		}
	})

	return loadedAgentAdapterCatalog{
		adaptersCount: len(uniqueNonEmptyStrings(adapterIDs)),
		releases:      slices.Clip(releases),
		channels:      slices.Clip(channels),
	}, nil
}

func (s *agentAdapterCatalogSyncService) loadCatalogIndex(ctx context.Context) (AgentAdapterCatalogIndex, error) {
	raw, err := s.store.GetObject(ctx, s.catalogKey)
	if err != nil {
		if errors.Is(err, ErrAgentAdapterObjectNotFound) {
			return AgentAdapterCatalogIndex{SchemaVersion: 1}, nil
		}
		return AgentAdapterCatalogIndex{}, fmt.Errorf("не удалось загрузить %s: %w", s.catalogKey, err)
	}

	var index AgentAdapterCatalogIndex
	if err := json.Unmarshal(raw, &index); err != nil {
		return AgentAdapterCatalogIndex{}, fmt.Errorf("catalog/index.json содержит невалидный JSON: %w", err)
	}

	return normalizeAgentAdapterCatalogIndex(index), nil
}

func (s *agentAdapterCatalogSyncService) loadReleaseManifest(
	ctx context.Context,
	adapterEntry AgentAdapterCatalogIndexAdapter,
	releaseEntry AgentAdapterCatalogIndexRelease,
) (models.AgentAdapterRelease, error) {
	raw, err := s.store.GetObject(ctx, releaseEntry.ReleaseKey)
	if err != nil {
		return models.AgentAdapterRelease{}, fmt.Errorf("не удалось загрузить release manifest %s: %w", releaseEntry.ReleaseKey, err)
	}

	var manifest AgentAdapterReleaseManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return models.AgentAdapterRelease{}, fmt.Errorf("release manifest %s содержит невалидный JSON: %w", releaseEntry.ReleaseKey, err)
	}

	manifest = normalizeAgentAdapterReleaseManifest(manifest)
	manifest.AdapterID = defaultStr(manifest.AdapterID, adapterEntry.AdapterID)
	manifest.Version = defaultStr(manifest.Version, releaseEntry.Version)
	manifest.TargetOS = defaultStr(manifest.TargetOS, releaseEntry.TargetOS)
	manifest.TargetArch = defaultStr(manifest.TargetArch, releaseEntry.TargetArch)
	if manifest.SourceKey == "" && manifest.FileName != "" {
		manifest.SourceKey = normalizeObjectKey(path.Join(path.Dir(releaseEntry.ReleaseKey), manifest.FileName))
	}
	if manifest.DownloadURL == "" {
		manifest.DownloadURL = buildAgentAdapterDownloadURL(s.publicBaseURL, manifest.SourceKey)
	}

	published, err := s.validatePublishedRelease(ctx, manifest, releaseEntry.ReleaseKey)
	if err != nil {
		return models.AgentAdapterRelease{}, err
	}
	manifest.Published = manifest.Published && published

	return models.AgentAdapterRelease{
		AdapterID:       manifest.AdapterID,
		Version:         manifest.Version,
		Title:           manifest.Title,
		Description:     manifest.Description,
		AdapterType:     manifest.AdapterType,
		TargetOS:        manifest.TargetOS,
		TargetArch:      manifest.TargetArch,
		ProtocolVersion: manifest.ProtocolVersion,
		FileName:        manifest.FileName,
		DownloadURL:     manifest.DownloadURL,
		SHA256:          manifest.SHA256,
		SourceKey:       manifest.SourceKey,
		Published:       manifest.Published,
		CreatedAt:       manifest.CreatedAt,
		UpdatedAt:       manifest.UpdatedAt,
	}, nil
}

func (s *agentAdapterCatalogSyncService) validatePublishedRelease(
	ctx context.Context,
	manifest AgentAdapterReleaseManifest,
	releaseKey string,
) (bool, error) {
	if missingFields := releaseManifestMandatoryFields(manifest); len(missingFields) > 0 {
		return false, nil
	}

	if _, err := s.store.StatObject(ctx, manifest.SourceKey); err != nil {
		if errors.Is(err, ErrAgentAdapterObjectNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("не удалось проверить бинарник %s: %w", manifest.SourceKey, err)
	}

	shaKey := normalizeObjectKey(path.Join(path.Dir(releaseKey), "sha256.txt"))
	rawSHA, err := s.store.GetObject(ctx, shaKey)
	if err != nil {
		if errors.Is(err, ErrAgentAdapterObjectNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("не удалось загрузить checksum %s: %w", shaKey, err)
	}

	expectedSHA := normalizeLower(string(rawSHA))
	if expectedSHA == "" {
		return false, nil
	}
	if expectedSHA != manifest.SHA256 {
		return false, nil
	}

	return true, nil
}

func (s *agentAdapterCatalogSyncService) loadChannels(
	ctx context.Context,
	adapterIDs []string,
	releaseKeyToLookupKey map[string]string,
) ([]loadedAgentAdapterChannel, error) {
	channelNames := uniqueNonEmptyStrings([]string{
		agentAdapterChannelStable,
		agentAdapterChannelLatest,
		s.defaultChannel,
	})
	if len(adapterIDs) == 0 || len(channelNames) == 0 {
		return nil, nil
	}

	out := make([]loadedAgentAdapterChannel, 0, len(adapterIDs)*len(channelNames))
	for _, adapterID := range adapterIDs {
		for _, channelName := range channelNames {
			channelKey := buildAgentAdapterChannelKey(adapterID, channelName)
			raw, err := s.store.GetObject(ctx, channelKey)
			if err != nil {
				if errors.Is(err, ErrAgentAdapterObjectNotFound) {
					continue
				}
				return nil, fmt.Errorf("не удалось загрузить channel pointer %s: %w", channelKey, err)
			}

			var pointer AgentAdapterChannelPointer
			if err := json.Unmarshal(raw, &pointer); err != nil {
				return nil, fmt.Errorf("channel pointer %s содержит невалидный JSON: %w", channelKey, err)
			}

			pointer = normalizeAgentAdapterChannelPointer(pointer)
			pointer.AdapterID = defaultStr(pointer.AdapterID, adapterID)
			pointer.Channel = defaultStr(pointer.Channel, channelName)
			if pointer.ReleaseKey == "" {
				if pointer.Version == "" || pointer.TargetOS == "" || pointer.TargetArch == "" {
					return nil, fmt.Errorf("channel pointer %s не содержит release_key и не может быть однозначно разрешён", channelKey)
				}
				pointer.ReleaseKey = buildAgentAdapterReleaseKey(pointer.AdapterID, pointer.Version, pointer.TargetOS, pointer.TargetArch)
			}

			if _, exists := releaseKeyToLookupKey[pointer.ReleaseKey]; !exists {
				return nil, fmt.Errorf(
					"channel pointer %s указывает на релиз %s, отсутствующий в catalog/index.json",
					channelKey,
					pointer.ReleaseKey,
				)
			}

			out = append(out, loadedAgentAdapterChannel{
				AdapterID:  pointer.AdapterID,
				Channel:    pointer.Channel,
				ReleaseKey: pointer.ReleaseKey,
			})
		}
	}

	return out, nil
}

func (s *agentAdapterCatalogSyncService) applyCatalog(
	ctx context.Context,
	loadedCatalog loadedAgentAdapterCatalog,
) (AgentAdapterCatalogRefreshResult, error) {
	tx := s.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return AgentAdapterCatalogRefreshResult{}, tx.Error
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = tx.Rollback()
			panic(recovered)
		}
	}()

	result := AgentAdapterCatalogRefreshResult{
		AdaptersCount: loadedCatalog.adaptersCount,
		RefreshedAt:   s.now(),
	}

	releaseIDByKey := make(map[string]uint, len(loadedCatalog.releases))
	for _, release := range loadedCatalog.releases {
		lookupKey := adapterReleaseLookupKey(release.AdapterID, release.Version, release.TargetOS, release.TargetArch)

		var existing models.AgentAdapterRelease
		err := tx.Where(
			"adapter_id = ? AND version = ? AND target_os = ? AND target_arch = ?",
			release.AdapterID,
			release.Version,
			release.TargetOS,
			release.TargetArch,
		).First(&existing).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			if err := tx.Create(&release).Error; err != nil {
				_ = tx.Rollback()
				return AgentAdapterCatalogRefreshResult{}, err
			}
			releaseIDByKey[lookupKey] = release.ID
		case err != nil:
			_ = tx.Rollback()
			return AgentAdapterCatalogRefreshResult{}, err
		default:
			existing.Title = release.Title
			existing.Description = release.Description
			existing.AdapterType = release.AdapterType
			existing.ProtocolVersion = release.ProtocolVersion
			existing.FileName = release.FileName
			existing.DownloadURL = release.DownloadURL
			existing.SHA256 = release.SHA256
			existing.SourceKey = release.SourceKey
			existing.Published = release.Published
			existing.CreatedAt = chooseCatalogTime(existing.CreatedAt, release.CreatedAt)
			existing.UpdatedAt = chooseCatalogTime(existing.UpdatedAt, release.UpdatedAt)
			if err := tx.Save(&existing).Error; err != nil {
				_ = tx.Rollback()
				return AgentAdapterCatalogRefreshResult{}, err
			}
			releaseIDByKey[lookupKey] = existing.ID
		}

		result.ReleasesUpserted++
	}

	seenChannelKeys := make(map[string]struct{}, len(loadedCatalog.channels))
	for _, channel := range loadedCatalog.channels {
		lookupKey := adapterReleaseLookupKeyFromReleaseKey(channel.ReleaseKey, loadedCatalog.releases)
		releaseID, exists := releaseIDByKey[lookupKey]
		if !exists {
			_ = tx.Rollback()
			return AgentAdapterCatalogRefreshResult{}, fmt.Errorf("не удалось разрешить release_id для channel %s/%s", channel.AdapterID, channel.Channel)
		}

		channelModel := models.AgentAdapterChannel{
			AdapterID: channel.AdapterID,
			Channel:   channel.Channel,
			ReleaseID: releaseID,
			UpdatedAt: result.RefreshedAt,
		}
		channelKey := channelModel.AdapterID + "|" + channelModel.Channel
		seenChannelKeys[channelKey] = struct{}{}

		var existing models.AgentAdapterChannel
		err := tx.Where("adapter_id = ? AND channel = ?", channelModel.AdapterID, channelModel.Channel).First(&existing).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			if err := tx.Create(&channelModel).Error; err != nil {
				_ = tx.Rollback()
				return AgentAdapterCatalogRefreshResult{}, err
			}
		case err != nil:
			_ = tx.Rollback()
			return AgentAdapterCatalogRefreshResult{}, err
		default:
			existing.ReleaseID = channelModel.ReleaseID
			existing.UpdatedAt = channelModel.UpdatedAt
			if err := tx.Save(&existing).Error; err != nil {
				_ = tx.Rollback()
				return AgentAdapterCatalogRefreshResult{}, err
			}
		}

		result.ChannelsUpserted++
	}

	var existingChannels []models.AgentAdapterChannel
	if err := tx.Find(&existingChannels).Error; err != nil {
		_ = tx.Rollback()
		return AgentAdapterCatalogRefreshResult{}, err
	}
	for _, channel := range existingChannels {
		channelKey := channel.AdapterID + "|" + channel.Channel
		if _, exists := seenChannelKeys[channelKey]; exists {
			continue
		}
		if err := tx.Delete(&channel).Error; err != nil {
			_ = tx.Rollback()
			return AgentAdapterCatalogRefreshResult{}, err
		}
		result.ChannelsDeleted++
	}

	seenReleaseKeys := make(map[string]struct{}, len(loadedCatalog.releases))
	for _, release := range loadedCatalog.releases {
		seenReleaseKeys[adapterReleaseLookupKey(release.AdapterID, release.Version, release.TargetOS, release.TargetArch)] = struct{}{}
	}

	var existingReleases []models.AgentAdapterRelease
	if err := tx.Find(&existingReleases).Error; err != nil {
		_ = tx.Rollback()
		return AgentAdapterCatalogRefreshResult{}, err
	}
	for _, release := range existingReleases {
		releaseKey := adapterReleaseLookupKey(release.AdapterID, release.Version, release.TargetOS, release.TargetArch)
		if _, exists := seenReleaseKeys[releaseKey]; exists {
			continue
		}
		if err := tx.Delete(&release).Error; err != nil {
			_ = tx.Rollback()
			return AgentAdapterCatalogRefreshResult{}, err
		}
		result.ReleasesDeleted++
	}

	if err := tx.Commit().Error; err != nil {
		return AgentAdapterCatalogRefreshResult{}, err
	}

	return result, nil
}

func adapterReleaseLookupKeyFromReleaseKey(releaseKey string, releases []models.AgentAdapterRelease) string {
	releaseKey = normalizeObjectKey(releaseKey)
	for _, release := range releases {
		if buildAgentAdapterReleaseKey(release.AdapterID, release.Version, release.TargetOS, release.TargetArch) == releaseKey {
			return adapterReleaseLookupKey(release.AdapterID, release.Version, release.TargetOS, release.TargetArch)
		}
	}
	return ""
}

func chooseCatalogTime(current, next time.Time) time.Time {
	switch {
	case next.IsZero():
		return current
	case current.IsZero():
		return next
	default:
		return next
	}
}
