package services

import (
	"context"
	"etalon-server/internal/domain/models"
	api "etalon-server/internal/transport/http/dtos"
	"fmt"
	"slices"
	"strings"

	"gorm.io/gorm"
)

type agentAdapterCatalogState struct {
	AdapterID      string
	DefaultRelease *models.AgentAdapterRelease
	StableRelease  *models.AgentAdapterRelease
	LatestRelease  *models.AgentAdapterRelease
	FallbackRelease *models.AgentAdapterRelease
}

func (s *agentOperatorFlowService) ResolveAgentAdapterManifests(ctx context.Context, agent *models.Agent) ([]api.AdapterManifestDTO, error) {
	if agent == nil || len(agent.Config) == 0 {
		return []api.AdapterManifestDTO{}, nil
	}

	config, err := decodeAgentConfigForWrite(agent.Config)
	if err != nil {
		return []api.AdapterManifestDTO{}, err
	}

	selectedAdapterIDs, _ := selectedAdapterIDsFromConfig(config)
	manifests, _, err := s.resolveSelectedAdapterManifests(ctx, selectedAdapterIDs)
	if err != nil {
		return []api.AdapterManifestDTO{}, err
	}
	if len(manifests) > 0 || len(config.SelectedAdapterIDs) > 0 {
		return manifests, nil
	}
	if len(config.AdapterManifests) == 0 {
		return []api.AdapterManifestDTO{}, nil
	}

	return normalizeManifestList(config.AdapterManifests), nil
}

// SavePublishedAdapterCatalog сохраняет старый server-side published catalog
// в новую release/channel-модель для совместимости со старыми сценариями импорта.
func (s *agentOperatorFlowService) SavePublishedAdapterCatalog(ctx context.Context, adapters []models.PublishedAgentAdapter) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, adapter := range adapters {
			normalized := normalizePublishedAdapter(adapter)
			release := normalizeAgentAdapterReleaseModel(models.AgentAdapterRelease{
				AdapterID:       normalized.AdapterID,
				Version:         normalized.Version,
				Title:           normalized.Title,
				Description:     normalized.Description,
				AdapterType:     normalized.AdapterType,
				TargetOS:        normalized.TargetOS,
				TargetArch:      normalized.TargetArch,
				ProtocolVersion: normalized.ProtocolVersion,
				FileName:        normalized.FileName,
				DownloadURL:     normalized.DownloadURL,
				SHA256:          normalized.SHA256,
				SourceKey:       sourceKeyFromDownloadURL(normalized.DownloadURL, normalized.FileName, normalized.AdapterID),
				Published:       normalized.Published,
				CreatedAt:       normalized.CreatedAt,
				UpdatedAt:       normalized.UpdatedAt,
			})

			var persisted models.AgentAdapterRelease
			err := tx.Where(
				"adapter_id = ? AND version = ? AND target_os = ? AND target_arch = ?",
				release.AdapterID,
				release.Version,
				release.TargetOS,
				release.TargetArch,
			).Assign(release).FirstOrCreate(&persisted).Error
			if err != nil {
				return err
			}

			for _, channelName := range []string{agentAdapterChannelStable, agentAdapterChannelLatest} {
				channel := models.AgentAdapterChannel{
					AdapterID: release.AdapterID,
					Channel:   channelName,
					ReleaseID: persisted.ID,
				}
				if err := tx.Where("adapter_id = ? AND channel = ?", channel.AdapterID, channel.Channel).
					Assign(channel).
					FirstOrCreate(&models.AgentAdapterChannel{}).
					Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (s *agentOperatorFlowService) listPublishedAdapterOptions(ctx context.Context) ([]api.PublishedAgentAdapterOptionDTO, error) {
	states, err := s.loadAdapterCatalogState(ctx, nil)
	if err != nil {
		return nil, err
	}

	out := make([]api.PublishedAgentAdapterOptionDTO, 0, len(states))
	for _, state := range states {
		out = append(out, buildPublishedAdapterOption(state, s.defaultChannel))
	}

	slices.SortFunc(out, func(left, right api.PublishedAgentAdapterOptionDTO) int {
		switch {
		case left.Selectable != right.Selectable:
			if left.Selectable {
				return -1
			}
			return 1
		case left.Title != right.Title:
			return strings.Compare(left.Title, right.Title)
		default:
			return strings.Compare(left.AdapterID, right.AdapterID)
		}
	})

	return slices.Clip(out), nil
}

func (s *agentOperatorFlowService) validateSelectedAdapterIDs(ctx context.Context, values []string) ([]string, error) {
	selectedAdapterIDs := normalizeAdapterIDList(values)
	if len(selectedAdapterIDs) == 0 {
		return nil, nil
	}

	catalog, err := s.loadAdapterCatalogStateMap(ctx, selectedAdapterIDs)
	if err != nil {
		return nil, err
	}

	for _, adapterID := range selectedAdapterIDs {
		state, ok := catalog[adapterID]
		switch {
		case !ok:
			return nil, fmt.Errorf("адаптер %s отсутствует в server-side каталоге релизов", adapterID)
		case state.DefaultRelease == nil:
			return nil, fmt.Errorf("адаптер %s нельзя выбрать: канал %s не назначен", adapterID, s.defaultChannel)
		}

		if missingFields := agentAdapterReleaseMissingManifestFields(*state.DefaultRelease); len(missingFields) > 0 {
			return nil, fmt.Errorf("адаптер %s нельзя выбрать: manifest неполон (%s)", adapterID, strings.Join(missingFields, ", "))
		}
		if !state.DefaultRelease.Published {
			return nil, fmt.Errorf("адаптер %s не опубликован и не может быть выбран", adapterID)
		}
	}

	return selectedAdapterIDs, nil
}

func (s *agentOperatorFlowService) resolveSelectedAdapterManifests(ctx context.Context, values []string) ([]api.AdapterManifestDTO, []string, error) {
	selectedAdapterIDs := normalizeAdapterIDList(values)
	if len(selectedAdapterIDs) == 0 {
		return nil, nil, nil
	}

	catalog, err := s.loadAdapterCatalogStateMap(ctx, selectedAdapterIDs)
	if err != nil {
		return nil, nil, err
	}

	out := make([]api.AdapterManifestDTO, 0, len(selectedAdapterIDs))
	warnings := make([]string, 0)
	for _, adapterID := range selectedAdapterIDs {
		state, ok := catalog[adapterID]
		if !ok {
			warnings = append(warnings, fmt.Sprintf("Сохранённый адаптер %s отсутствует в server-side каталоге релизов.", adapterID))
			continue
		}
		if state.DefaultRelease == nil {
			warnings = append(warnings, fmt.Sprintf("Сохранённый адаптер %s пропущен: канал %s не назначен.", adapterID, s.defaultChannel))
			continue
		}

		missingFields := agentAdapterReleaseMissingManifestFields(*state.DefaultRelease)
		if len(missingFields) > 0 {
			warnings = append(warnings, fmt.Sprintf("Сохранённый адаптер %s пропущен: manifest неполон (%s).", adapterID, strings.Join(missingFields, ", ")))
			continue
		}
		if !state.DefaultRelease.Published {
			warnings = append(warnings, fmt.Sprintf("Сохранённый адаптер %s больше не опубликован и не будет выдан агенту.", adapterID))
			continue
		}

		out = append(out, agentAdapterReleaseToManifest(*state.DefaultRelease))
	}

	return slices.Clip(out), uniqueNonEmptyStrings(warnings), nil
}

func (s *agentOperatorFlowService) loadAdapterCatalogStateMap(ctx context.Context, adapterIDs []string) (map[string]agentAdapterCatalogState, error) {
	states, err := s.loadAdapterCatalogState(ctx, adapterIDs)
	if err != nil {
		return nil, err
	}

	out := make(map[string]agentAdapterCatalogState, len(states))
	for _, state := range states {
		out[state.AdapterID] = state
	}
	return out, nil
}

func (s *agentOperatorFlowService) loadAdapterCatalogState(ctx context.Context, adapterIDs []string) ([]agentAdapterCatalogState, error) {
	adapterIDs = normalizeAdapterIDList(adapterIDs)

	var releases []models.AgentAdapterRelease
	releaseQuery := s.db.WithContext(ctx)
	if len(adapterIDs) > 0 {
		releaseQuery = releaseQuery.Where("adapter_id IN ?", adapterIDs)
	}
	if err := releaseQuery.Find(&releases).Error; err != nil {
		return nil, err
	}

	channelNames := uniqueNonEmptyStrings([]string{
		s.defaultChannel,
		agentAdapterChannelStable,
		agentAdapterChannelLatest,
	})

	var channels []models.AgentAdapterChannel
	channelQuery := s.db.WithContext(ctx).Preload("Release").Where("channel IN ?", channelNames)
	if len(adapterIDs) > 0 {
		channelQuery = channelQuery.Where("adapter_id IN ?", adapterIDs)
	}
	if err := channelQuery.Find(&channels).Error; err != nil {
		return nil, err
	}

	stateMap := make(map[string]agentAdapterCatalogState, max(len(releases), len(channels)))
	for _, release := range releases {
		release = normalizeAgentAdapterReleaseModel(release)
		state := stateMap[release.AdapterID]
		state.AdapterID = release.AdapterID
		state.FallbackRelease = pickPreferredRelease(state.FallbackRelease, &release)
		stateMap[release.AdapterID] = state
	}

	for _, channel := range channels {
		if channel.Release.ID == 0 {
			continue
		}
		release := normalizeAgentAdapterReleaseModel(channel.Release)
		state := stateMap[channel.AdapterID]
		state.AdapterID = channel.AdapterID
		state.FallbackRelease = pickPreferredRelease(state.FallbackRelease, &release)

		switch normalizeAgentAdapterChannel(channel.Channel) {
		case agentAdapterChannelStable:
			state.StableRelease = cloneAgentAdapterRelease(release)
		case agentAdapterChannelLatest:
			state.LatestRelease = cloneAgentAdapterRelease(release)
		}
		if normalizeAgentAdapterChannel(channel.Channel) == s.defaultChannel {
			state.DefaultRelease = cloneAgentAdapterRelease(release)
		}
		stateMap[channel.AdapterID] = state
	}

	states := make([]agentAdapterCatalogState, 0, len(stateMap))
	for _, state := range stateMap {
		if state.AdapterID == "" {
			continue
		}
		states = append(states, state)
	}

	slices.SortFunc(states, func(left, right agentAdapterCatalogState) int {
		return strings.Compare(left.AdapterID, right.AdapterID)
	})

	return slices.Clip(states), nil
}

func normalizeAgentAdapterReleaseModel(item models.AgentAdapterRelease) models.AgentAdapterRelease {
	item.AdapterID = normalizeLower(item.AdapterID)
	item.Version = strings.TrimSpace(item.Version)
	item.Title = strings.TrimSpace(item.Title)
	item.Description = strings.TrimSpace(item.Description)
	item.AdapterType = normalizeLower(item.AdapterType)
	item.TargetOS = normalizeLower(item.TargetOS)
	item.TargetArch = normalizeLower(item.TargetArch)
	item.ProtocolVersion = strings.TrimSpace(item.ProtocolVersion)
	item.FileName = strings.TrimSpace(item.FileName)
	item.DownloadURL = strings.TrimSpace(item.DownloadURL)
	item.SHA256 = normalizeLower(item.SHA256)
	item.SourceKey = normalizeObjectKey(item.SourceKey)
	return item
}

func buildPublishedAdapterOption(state agentAdapterCatalogState, defaultChannel string) api.PublishedAgentAdapterOptionDTO {
	preferredRelease := pickPreferredRelease(state.DefaultRelease, state.StableRelease)
	preferredRelease = pickPreferredRelease(preferredRelease, state.LatestRelease)
	preferredRelease = pickPreferredRelease(preferredRelease, state.FallbackRelease)

	if preferredRelease == nil {
		return api.PublishedAgentAdapterOptionDTO{
			AdapterID:      state.AdapterID,
			Title:          state.AdapterID,
			Published:      false,
			Selectable:     false,
			StatusText:     fmt.Sprintf("Канал %s не назначен", defaultChannel),
			DisabledReason: fmt.Sprintf("Канал %s не назначен.", defaultChannel),
		}
	}

	disabledReason := agentAdapterDisabledReason(state, defaultChannel)
	statusText := "Готов к выдаче"
	missingFields := []string(nil)
	if state.DefaultRelease != nil {
		missingFields = agentAdapterReleaseMissingManifestFields(*state.DefaultRelease)
	}
	switch {
	case state.DefaultRelease == nil:
		statusText = fmt.Sprintf("Канал %s не назначен", defaultChannel)
	case len(missingFields) > 0:
		statusText = "Manifest неполон"
	case !state.DefaultRelease.Published:
		statusText = "Не опубликован"
	}

	return api.PublishedAgentAdapterOptionDTO{
		AdapterID:      state.AdapterID,
		Title:          defaultStr(preferredRelease.Title, state.AdapterID),
		Description:    preferredRelease.Description,
		Published:      state.DefaultRelease != nil && state.DefaultRelease.Published,
		Selectable:     disabledReason == "",
		StatusText:     statusText,
		DisabledReason: disabledReason,
		Version:        releaseField(state.DefaultRelease, func(item *models.AgentAdapterRelease) string { return item.Version }),
		StableVersion:  releaseField(state.StableRelease, func(item *models.AgentAdapterRelease) string { return item.Version }),
		LatestVersion:  releaseField(state.LatestRelease, func(item *models.AgentAdapterRelease) string { return item.Version }),
		AdapterType:    releaseField(preferredRelease, func(item *models.AgentAdapterRelease) string { return item.AdapterType }),
		TargetOS:       releaseField(preferredRelease, func(item *models.AgentAdapterRelease) string { return item.TargetOS }),
		TargetArch:     releaseField(preferredRelease, func(item *models.AgentAdapterRelease) string { return item.TargetArch }),
	}
}

func agentAdapterDisabledReason(state agentAdapterCatalogState, defaultChannel string) string {
	switch {
	case state.DefaultRelease == nil:
		return fmt.Sprintf("Канал %s не назначен.", defaultChannel)
	default:
		missingFields := agentAdapterReleaseMissingManifestFields(*state.DefaultRelease)
		if len(missingFields) > 0 {
			return fmt.Sprintf("Manifest неполон: %s.", strings.Join(missingFields, ", "))
		}
		if !state.DefaultRelease.Published {
			return fmt.Sprintf("Релиз канала %s не опубликован.", defaultChannel)
		}
		return ""
	}
}

func agentAdapterReleaseMissingManifestFields(item models.AgentAdapterRelease) []string {
	missingFields := make([]string, 0, 10)
	if strings.TrimSpace(item.AdapterID) == "" {
		missingFields = append(missingFields, "adapter_id")
	}
	if strings.TrimSpace(item.Version) == "" {
		missingFields = append(missingFields, "version")
	}
	if strings.TrimSpace(item.Title) == "" {
		missingFields = append(missingFields, "title")
	}
	if strings.TrimSpace(item.AdapterType) == "" {
		missingFields = append(missingFields, "adapter_type")
	}
	if strings.TrimSpace(item.TargetOS) == "" {
		missingFields = append(missingFields, "target_os")
	}
	if strings.TrimSpace(item.TargetArch) == "" {
		missingFields = append(missingFields, "target_arch")
	}
	if strings.TrimSpace(item.ProtocolVersion) == "" {
		missingFields = append(missingFields, "protocol_version")
	}
	if strings.TrimSpace(item.DownloadURL) == "" {
		missingFields = append(missingFields, "download_url")
	}
	if strings.TrimSpace(item.SHA256) == "" {
		missingFields = append(missingFields, "sha256")
	}
	if strings.TrimSpace(item.FileName) == "" {
		missingFields = append(missingFields, "file_name")
	}
	if strings.TrimSpace(item.SourceKey) == "" {
		missingFields = append(missingFields, "source_key")
	}
	return missingFields
}

func agentAdapterReleaseToManifest(item models.AgentAdapterRelease) api.AdapterManifestDTO {
	return api.AdapterManifestDTO{
		AdapterID:       item.AdapterID,
		AdapterType:     item.AdapterType,
		Version:         item.Version,
		TargetOS:        item.TargetOS,
		TargetArch:      item.TargetArch,
		ProtocolVersion: item.ProtocolVersion,
		DownloadURL:     item.DownloadURL,
		SHA256:          item.SHA256,
		FileName:        item.FileName,
	}
}

func cloneAgentAdapterRelease(item models.AgentAdapterRelease) *models.AgentAdapterRelease {
	copyRelease := item
	return &copyRelease
}

func pickPreferredRelease(current, next *models.AgentAdapterRelease) *models.AgentAdapterRelease {
	switch {
	case next == nil:
		return current
	case current == nil:
		return cloneAgentAdapterRelease(*next)
	case current.Published != next.Published:
		if next.Published {
			return cloneAgentAdapterRelease(*next)
		}
		return current
	case !next.UpdatedAt.Equal(current.UpdatedAt):
		if next.UpdatedAt.After(current.UpdatedAt) {
			return cloneAgentAdapterRelease(*next)
		}
		return current
	case next.Version > current.Version:
		return cloneAgentAdapterRelease(*next)
	default:
		return current
	}
}

func releaseField(release *models.AgentAdapterRelease, getter func(*models.AgentAdapterRelease) string) string {
	if release == nil {
		return ""
	}
	return getter(release)
}

func normalizePublishedAdapter(item models.PublishedAgentAdapter) models.PublishedAgentAdapter {
	item.AdapterID = normalizeLower(item.AdapterID)
	item.Title = strings.TrimSpace(item.Title)
	item.Description = strings.TrimSpace(item.Description)
	item.Version = strings.TrimSpace(item.Version)
	item.AdapterType = normalizeLower(item.AdapterType)
	item.TargetOS = normalizeLower(item.TargetOS)
	item.TargetArch = normalizeLower(item.TargetArch)
	item.ProtocolVersion = strings.TrimSpace(item.ProtocolVersion)
	item.DownloadURL = strings.TrimSpace(item.DownloadURL)
	item.SHA256 = normalizeLower(item.SHA256)
	item.FileName = strings.TrimSpace(item.FileName)
	return item
}

func selectedAdapterIDsFromConfig(config api.AgentConfigDTO) ([]string, bool) {
	selectedAdapterIDs := normalizeAdapterIDList(config.SelectedAdapterIDs)
	if len(selectedAdapterIDs) > 0 {
		return selectedAdapterIDs, false
	}
	if len(config.AdapterManifests) == 0 {
		return nil, false
	}

	legacyAdapterIDs := make([]string, 0, len(config.AdapterManifests))
	for _, item := range config.AdapterManifests {
		legacyAdapterIDs = append(legacyAdapterIDs, item.AdapterID)
	}

	legacyAdapterIDs = normalizeAdapterIDList(legacyAdapterIDs)
	return legacyAdapterIDs, len(legacyAdapterIDs) > 0
}

func manifestAdapterIDs(items []api.AdapterManifestDTO) []string {
	if len(items) == 0 {
		return nil
	}

	adapterIDs := make([]string, 0, len(items))
	for _, item := range items {
		adapterIDs = append(adapterIDs, item.AdapterID)
	}
	return normalizeAdapterIDList(adapterIDs)
}

func normalizeAdapterIDList(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized := normalizeLower(value)
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}

	slices.Sort(out)
	return slices.Clip(out)
}
