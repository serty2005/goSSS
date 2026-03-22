package services

import (
	"context"
	"etalon-server/internal/domain/models"
	api "etalon-server/internal/transport/http/dtos"
	"fmt"
	"slices"
	"strings"
)

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

func (s *agentOperatorFlowService) SavePublishedAdapterCatalog(ctx context.Context, adapters []models.PublishedAgentAdapter) error {
	for _, adapter := range adapters {
		normalized := normalizePublishedAdapter(adapter)
		if err := s.db.WithContext(ctx).
			Where("adapter_id = ?", normalized.AdapterID).
			Assign(normalized).
			FirstOrCreate(&models.PublishedAgentAdapter{}).
			Error; err != nil {
			return err
		}
	}

	return nil
}

func (s *agentOperatorFlowService) listPublishedAdapterOptions(ctx context.Context) ([]api.PublishedAgentAdapterOptionDTO, error) {
	var items []models.PublishedAgentAdapter
	if err := s.db.WithContext(ctx).
		Order("published DESC, title ASC, adapter_id ASC").
		Find(&items).
		Error; err != nil {
		return nil, err
	}

	out := make([]api.PublishedAgentAdapterOptionDTO, 0, len(items))
	for _, item := range items {
		normalized := normalizePublishedAdapter(item)
		out = append(out, buildPublishedAdapterOption(normalized))
	}

	return slices.Clip(out), nil
}

func (s *agentOperatorFlowService) validateSelectedAdapterIDs(ctx context.Context, values []string) ([]string, error) {
	selectedAdapterIDs := normalizeAdapterIDList(values)
	if len(selectedAdapterIDs) == 0 {
		return nil, nil
	}

	catalog, err := s.loadPublishedAdapterMap(ctx, selectedAdapterIDs)
	if err != nil {
		return nil, err
	}

	for _, adapterID := range selectedAdapterIDs {
		item, ok := catalog[adapterID]
		switch {
		case !ok:
			return nil, fmt.Errorf("адаптер %s отсутствует в server-side каталоге published adapters", adapterID)
		case !item.Published:
			return nil, fmt.Errorf("адаптер %s не опубликован и не может быть выбран", adapterID)
		}

		if missingFields := publishedAdapterMissingManifestFields(item); len(missingFields) > 0 {
			return nil, fmt.Errorf("адаптер %s нельзя выбрать: manifest неполон (%s)", adapterID, strings.Join(missingFields, ", "))
		}
	}

	return selectedAdapterIDs, nil
}

func (s *agentOperatorFlowService) resolveSelectedAdapterManifests(ctx context.Context, values []string) ([]api.AdapterManifestDTO, []string, error) {
	selectedAdapterIDs := normalizeAdapterIDList(values)
	if len(selectedAdapterIDs) == 0 {
		return nil, nil, nil
	}

	catalog, err := s.loadPublishedAdapterMap(ctx, selectedAdapterIDs)
	if err != nil {
		return nil, nil, err
	}

	out := make([]api.AdapterManifestDTO, 0, len(selectedAdapterIDs))
	warnings := make([]string, 0)
	for _, adapterID := range selectedAdapterIDs {
		item, ok := catalog[adapterID]
		if !ok {
			warnings = append(warnings, fmt.Sprintf("Сохранённый адаптер %s отсутствует в server-side каталоге published adapters.", adapterID))
			continue
		}
		if !item.Published {
			warnings = append(warnings, fmt.Sprintf("Сохранённый адаптер %s больше не опубликован и не будет выдан агенту.", adapterID))
			continue
		}

		missingFields := publishedAdapterMissingManifestFields(item)
		if len(missingFields) > 0 {
			warnings = append(warnings, fmt.Sprintf("Сохранённый адаптер %s пропущен: manifest неполон (%s).", adapterID, strings.Join(missingFields, ", ")))
			continue
		}

		out = append(out, publishedAdapterToManifest(item))
	}

	return slices.Clip(out), uniqueNonEmptyStrings(warnings), nil
}

func (s *agentOperatorFlowService) loadPublishedAdapterMap(ctx context.Context, adapterIDs []string) (map[string]models.PublishedAgentAdapter, error) {
	adapterIDs = normalizeAdapterIDList(adapterIDs)
	if len(adapterIDs) == 0 {
		return map[string]models.PublishedAgentAdapter{}, nil
	}

	var items []models.PublishedAgentAdapter
	if err := s.db.WithContext(ctx).
		Where("adapter_id IN ?", adapterIDs).
		Find(&items).
		Error; err != nil {
		return nil, err
	}

	out := make(map[string]models.PublishedAgentAdapter, len(items))
	for _, item := range items {
		normalized := normalizePublishedAdapter(item)
		out[normalized.AdapterID] = normalized
	}

	return out, nil
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

func buildPublishedAdapterOption(item models.PublishedAgentAdapter) api.PublishedAgentAdapterOptionDTO {
	disabledReason := publishedAdapterDisabledReason(item)
	statusText := "Готов к выдаче"
	switch {
	case !item.Published:
		statusText = "Не опубликован"
	case disabledReason != "":
		statusText = "Manifest неполон"
	}

	return api.PublishedAgentAdapterOptionDTO{
		AdapterID:      item.AdapterID,
		Title:          item.Title,
		Description:    item.Description,
		Published:      item.Published,
		Selectable:     disabledReason == "",
		StatusText:     statusText,
		DisabledReason: disabledReason,
		Version:        item.Version,
		AdapterType:    item.AdapterType,
		TargetOS:       item.TargetOS,
		TargetArch:     item.TargetArch,
	}
}

func publishedAdapterDisabledReason(item models.PublishedAgentAdapter) string {
	switch {
	case !item.Published:
		return "Адаптер не опубликован."
	default:
		missingFields := publishedAdapterMissingManifestFields(item)
		if len(missingFields) == 0 {
			return ""
		}
		return fmt.Sprintf("Manifest неполон: %s.", strings.Join(missingFields, ", "))
	}
}

func publishedAdapterMissingManifestFields(item models.PublishedAgentAdapter) []string {
	missingFields := make([]string, 0, 8)
	if strings.TrimSpace(item.AdapterID) == "" {
		missingFields = append(missingFields, "adapter_id")
	}
	if strings.TrimSpace(item.Version) == "" {
		missingFields = append(missingFields, "version")
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
	return missingFields
}

func publishedAdapterToManifest(item models.PublishedAgentAdapter) api.AdapterManifestDTO {
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
