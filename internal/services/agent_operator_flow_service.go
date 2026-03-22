package services

import (
	"context"
	"encoding/json"
	"errors"
	"etalon-server/internal/domain/models"
	api "etalon-server/internal/transport/http/dtos"
	"fmt"
	"slices"
	"strings"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type AgentOperatorFlowService interface {
	ResolveAgentAdapterManifests(ctx context.Context, agent *models.Agent) ([]api.AdapterManifestDTO, error)
	BuildOperatorFlow(ctx context.Context, agent *models.Agent) (*api.AgentOperatorFlowDTO, error)
	SaveAdapterSelection(ctx context.Context, agentUUID string, req api.SaveAgentAdapterSelectionRequestDTO, actor string) error
	SaveCOMSignatureRule(ctx context.Context, req api.UpsertAgentCOMSignatureRuleRequestDTO, actor string) error
	EnqueueAdapterRun(ctx context.Context, agentUUID string, req api.EnqueueAgentAdapterRunRequestDTO, actor string) error
	EnsureScheduledAdapterRuns(ctx context.Context, agent *models.Agent) error
}

type AgentAdapterManifestResolver interface {
	ResolveAgentAdapterManifests(ctx context.Context, agent *models.Agent) ([]api.AdapterManifestDTO, error)
}

type agentOperatorFlowService struct {
	db             *gorm.DB
	defaultChannel string
}

func NewAgentOperatorFlowService(db *gorm.DB, defaultChannels ...string) AgentOperatorFlowService {
	defaultChannel := agentAdapterChannelStable
	if len(defaultChannels) > 0 {
		defaultChannel = normalizeAgentAdapterChannel(defaultChannels[0])
	}
	return &agentOperatorFlowService{
		db:             db,
		defaultChannel: defaultChannel,
	}
}

type profileDefinition struct {
	Key     string
	Title   string
	Summary string
}

type adapterCatalogItem struct {
	AdapterID       string
	AdapterType     string
	ProtocolVersion string
	TargetOS        string
}

var machineProfiles = map[string]profileDefinition{
	"unknown": {
		Key:     "unknown",
		Title:   "Недостаточно данных",
		Summary: "Сервер ещё не получил достаточный inventory для уверенного подбора профиля машины.",
	},
	"service-workstation": {
		Key:     "service-workstation",
		Title:   "Сервисная рабочая станция",
		Summary: "Обычная рабочая станция без явных признаков кассового ПО и фискального оборудования.",
	},
	"pos-workstation": {
		Key:     "pos-workstation",
		Title:   "POS-станция",
		Summary: "На машине есть признаки iiko/syrve и кассовой роли, но фискальный адаптер пока не определён.",
	},
	"fiscal-workstation": {
		Key:     "fiscal-workstation",
		Title:   "Фискальная станция",
		Summary: "На машине обнаружены признаки фискального оборудования, но без явной POS-роли.",
	},
	"hybrid-pos-fiscal": {
		Key:     "hybrid-pos-fiscal",
		Title:   "Гибридная POS/фискальная станция",
		Summary: "На машине одновременно видны признаки кассового ПО и локального фискального оборудования.",
	},
}

var adapterCatalog = map[string]adapterCatalogItem{
	"fiscal-atol": {
		AdapterID:       "fiscal-atol",
		AdapterType:     "fiscal-atol",
		ProtocolVersion: "1",
		TargetOS:        "windows",
	},
	"fiscal-mitsu": {
		AdapterID:       "fiscal-mitsu",
		AdapterType:     "fiscal-mitsu",
		ProtocolVersion: "1",
		TargetOS:        "windows",
	},
	"fiscal-shtrih": {
		AdapterID:       "fiscal-shtrih",
		AdapterType:     "fiscal-shtrih",
		ProtocolVersion: "1",
		TargetOS:        "windows",
	},
}

var deviceTypeToAdapter = map[string]string{
	"fiscal":        "fiscal-atol",
	"fiscal-atol":   "fiscal-atol",
	"fiscal-mitsu":  "fiscal-mitsu",
	"fiscal-shtrih": "fiscal-shtrih",
}

func (s *agentOperatorFlowService) BuildOperatorFlow(ctx context.Context, agent *models.Agent) (*api.AgentOperatorFlowDTO, error) {
	if agent == nil {
		return nil, nil
	}

	inventory, inventoryWarning := decodeInventorySnapshot(agent.LatestInventorySnapshot)
	savedConfig, configWarning := decodeAgentConfig(agent.Config)
	availableAdapters, err := s.listPublishedAdapterOptions(ctx)
	if err != nil {
		return nil, err
	}
	selectedAdapterIDs, legacySelection := selectedAdapterIDsFromConfig(savedConfig)
	effectiveManifests, selectionWarnings, err := s.resolveSelectedAdapterManifests(ctx, selectedAdapterIDs)
	if err != nil {
		return nil, err
	}
	if len(effectiveManifests) == 0 && len(savedConfig.SelectedAdapterIDs) == 0 && len(savedConfig.AdapterManifests) > 0 {
		effectiveManifests = normalizeManifestList(savedConfig.AdapterManifests)
	}

	signatureRules, err := s.loadSignatureRules(ctx, inventory)
	if err != nil {
		return nil, err
	}

	var meaningfulSnapshot *normalizedHeartbeatState
	if len(agent.LastMeaningfulHeartbeatState) > 0 {
		var decoded normalizedHeartbeatState
		if err := json.Unmarshal(agent.LastMeaningfulHeartbeatState, &decoded); err == nil {
			meaningfulSnapshot = &decoded
		}
	}

	recommendedProfile, recommendedReasons, recommendedManifests, signatureCandidates, warnings := buildMachineRecommendation(agent, inventory, meaningfulSnapshot, signatureRules)
	recommendedAdapterIDs := manifestAdapterIDs(recommendedManifests)
	if inventoryWarning != "" {
		warnings = append(warnings, inventoryWarning)
	}
	if configWarning != "" {
		warnings = append(warnings, configWarning)
	}
	if len(selectionWarnings) > 0 {
		warnings = append(warnings, selectionWarnings...)
	}
	if legacySelection {
		warnings = append(warnings, "У агента ещё сохранён legacy-набор adapter_manifests. Следующее сохранение переведёт конфигурацию на selected_adapter_ids.")
	}
	if len(availableAdapters) == 0 {
		warnings = append(warnings, "Каталог опубликованных адаптеров пока пуст.")
	}

	meaningfulState, _ := decodeJSONAny(agent.LastMeaningfulHeartbeatState)
	savedProfile := buildSavedProfileDTO(savedConfig.MachineProfile)
	savedReasons := []string(nil)
	savedManifests := []api.AdapterManifestDTO(nil)
	savedRuntimeProfiles := sanitizeAdapterRuntimeProfiles(savedConfig.AdapterRuntimeProfiles)
	adapterStatuses, adapterStatusWarning := decodeAdapterStatuses(agent.LatestAdapterStatuses)
	if savedConfig.MachineProfile != nil {
		savedReasons = slices.Clone(savedConfig.MachineProfile.Reasons)
	}
	if len(savedConfig.AdapterManifests) > 0 {
		savedManifests = normalizeManifestList(savedConfig.AdapterManifests)
	}
	if adapterStatusWarning != "" {
		warnings = append(warnings, adapterStatusWarning)
	}
	savedRuntimeProfiles = enrichRuntimeProfilesWithStatus(savedRuntimeProfiles, adapterStatuses, time.Now().UTC())
	warnings = append(warnings, buildRuntimeProfileWarnings(selectedAdapterIDs, savedRuntimeProfiles, adapterStatuses, time.Now().UTC())...)

	return &api.AgentOperatorFlowDTO{
		MeaningfulHeartbeat: api.AgentHeartbeatMeaningfulStateDTO{
			Fingerprint:               strings.TrimSpace(agent.LastMeaningfulHeartbeatFingerprint),
			LastMeaningfulHeartbeatAt: agent.LastMeaningfulHeartbeatAt,
			LastMeaningfulObservedAt:  agent.LastMeaningfulObservedAt,
			LastMeaningfulState:       meaningfulState,
		},
		AvailableAdapters:           slices.Clip(availableAdapters),
		SelectedAdapterIDs:          slices.Clip(selectedAdapterIDs),
		RecommendedAdapterIDs:       slices.Clip(recommendedAdapterIDs),
		RecommendedProfile:          recommendedProfile,
		RecommendedReasons:          slices.Clip(recommendedReasons),
		RecommendedAdapterManifests: slices.Clip(recommendedManifests),
		SavedProfile:                savedProfile,
		SavedReasons:                slices.Clip(savedReasons),
		SavedAdapterManifests:       slices.Clip(savedManifests),
		EffectiveAdapterManifests:   slices.Clip(effectiveManifests),
		SavedAdapterRuntimeProfiles: slices.Clip(savedRuntimeProfiles),
		SignatureCandidates:         slices.Clip(signatureCandidates),
		Warnings:                    slices.Clip(uniqueNonEmptyStrings(warnings)),
	}, nil
}

func (s *agentOperatorFlowService) SaveAdapterSelection(ctx context.Context, agentUUID string, req api.SaveAgentAdapterSelectionRequestDTO, actor string) error {
	agentUUID = strings.TrimSpace(agentUUID)
	if agentUUID == "" {
		return errors.New("uuid агента обязателен")
	}
	_ = actor

	var agent models.Agent
	if err := s.db.WithContext(ctx).Where("uuid = ?", agentUUID).First(&agent).Error; err != nil {
		return err
	}

	selectedAdapterIDs, err := s.validateSelectedAdapterIDs(ctx, req.SelectedAdapterIDs)
	if err != nil {
		return err
	}
	runtimeProfiles := sanitizeAdapterRuntimeProfiles(req.RuntimeProfiles)
	for _, profile := range runtimeProfiles {
		if !slices.Contains(selectedAdapterIDs, profile.AdapterID) {
			return fmt.Errorf("runtime-профиль %s нельзя сохранить без выбора адаптера", profile.AdapterID)
		}
	}

	config, err := decodeAgentConfigForWrite(agent.Config)
	if err != nil {
		return err
	}
	config.MachineProfile = nil
	config.SelectedAdapterIDs = selectedAdapterIDs
	config.AdapterManifests = nil
	config.AdapterRuntimeProfiles = runtimeProfiles

	raw, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("не удалось сериализовать конфигурацию агента: %w", err)
	}

	return s.db.WithContext(ctx).
		Model(&models.Agent{}).
		Where("uuid = ?", agentUUID).
		Update("config", datatypes.JSON(raw)).
		Error
}

func (s *agentOperatorFlowService) SaveCOMSignatureRule(ctx context.Context, req api.UpsertAgentCOMSignatureRuleRequestDTO, actor string) error {
	signatureKey := normalizeLower(req.SignatureKey)
	if signatureKey == "" {
		return errors.New("signature_key обязателен")
	}

	suggestedAdapter := normalizeLower(req.SuggestedAdapter)
	deviceType := normalizeLower(req.DeviceType)
	if deviceType == "" {
		deviceType = normalizeLower(adapterToDeviceType(suggestedAdapter))
	}
	if deviceType == "" {
		return errors.New("device_type обязателен")
	}

	rule := models.AgentCOMSignatureRule{
		SignatureKey:     signatureKey,
		DeviceType:       deviceType,
		Label:            strings.TrimSpace(req.Label),
		Confidence:       defaultStr(normalizeLower(req.Confidence), "high"),
		ProfileHint:      normalizeLower(req.ProfileHint),
		SuggestedAdapter: suggestedAdapter,
		Source:           "operator",
		Notes:            strings.TrimSpace(req.Notes),
		UpdatedBy:        strings.TrimSpace(actor),
	}

	var existing models.AgentCOMSignatureRule
	err := s.db.WithContext(ctx).Where("signature_key = ?", signatureKey).First(&existing).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		rule.CreatedBy = strings.TrimSpace(actor)
		return s.db.WithContext(ctx).Create(&rule).Error
	case err != nil:
		return err
	default:
		existing.DeviceType = rule.DeviceType
		existing.Label = rule.Label
		existing.Confidence = rule.Confidence
		existing.ProfileHint = rule.ProfileHint
		existing.SuggestedAdapter = rule.SuggestedAdapter
		existing.Source = rule.Source
		existing.Notes = rule.Notes
		existing.UpdatedBy = rule.UpdatedBy
		return s.db.WithContext(ctx).Save(&existing).Error
	}
}

func (s *agentOperatorFlowService) loadSignatureRules(ctx context.Context, inventory *api.InventorySnapshotDTO) (map[string]models.AgentCOMSignatureRule, error) {
	keys := make([]string, 0)
	if inventory != nil {
		for _, port := range inventory.COMPorts {
			key := normalizeLower(port.SignatureKey)
			if key == "" {
				continue
			}
			keys = append(keys, key)
		}
	}
	keys = uniqueNonEmptyStrings(keys)
	if len(keys) == 0 {
		return map[string]models.AgentCOMSignatureRule{}, nil
	}

	var rules []models.AgentCOMSignatureRule
	if err := s.db.WithContext(ctx).Where("signature_key IN ?", keys).Find(&rules).Error; err != nil {
		return nil, err
	}

	out := make(map[string]models.AgentCOMSignatureRule, len(rules))
	for _, rule := range rules {
		out[normalizeLower(rule.SignatureKey)] = rule
	}
	return out, nil
}

func buildMachineRecommendation(
	agent *models.Agent,
	inventory *api.InventorySnapshotDTO,
	meaningfulSnapshot *normalizedHeartbeatState,
	signatureRules map[string]models.AgentCOMSignatureRule,
) (api.AgentMachineProfileDTO, []string, []api.AdapterManifestDTO, []api.AgentCOMSignatureCandidateDTO, []string) {
	if inventory == nil {
		profile := buildProfileDTO(machineProfiles["unknown"], "recommendation", nil, "")
		return profile, []string{"Inventory snapshot ещё не получен."}, nil, nil, nil
	}

	reasons := make([]string, 0, 8)
	warnings := make([]string, 0, 4)
	candidates := make([]api.AgentCOMSignatureCandidateDTO, 0, len(inventory.COMPorts))
	recommendedAdapters := make([]string, 0, 4)

	hasCashSignal := false
	if inventory.HostInfo != nil {
		if url := strings.TrimSpace(inventory.HostInfo.CashServerURL); url != "" {
			hasCashSignal = true
			reasons = append(reasons, fmt.Sprintf("В host_info найден cash_server_url: %s.", url))
		}
		if product := strings.TrimSpace(inventory.HostInfo.CashServerProduct); product != "" {
			hasCashSignal = true
			reasons = append(reasons, fmt.Sprintf("В host_info найден cash_server_product: %s.", product))
		}
	}

	hasFiscalSignal := meaningfulSnapshot != nil && strings.TrimSpace(meaningfulSnapshot.Fiscal.SerialNumber) != ""
	if hasFiscalSignal {
		reasons = append(reasons, fmt.Sprintf("В meaningful heartbeat уже зафиксирован серийный номер ФР: %s.", strings.TrimSpace(meaningfulSnapshot.Fiscal.SerialNumber)))
	}
	if meaningfulSnapshot != nil && strings.TrimSpace(meaningfulSnapshot.Fiscal.ModelName) != "" {
		hasFiscalSignal = true
		reasons = append(reasons, fmt.Sprintf("В meaningful heartbeat уже зафиксирована модель ФР: %s.", strings.TrimSpace(meaningfulSnapshot.Fiscal.ModelName)))
	}
	if strings.TrimSpace(agent.MachineFingerprint) != "" {
		reasons = append(reasons, "Для машины уже зафиксирован bootstrap machine_fingerprint.")
	}

	for _, component := range inventory.KnownComponents {
		if !component.Detected {
			continue
		}
		key := normalizeLower(component.Key)
		switch key {
		case "iiko-front", "iiko-cashserver", "syrve-front", "syrve-cashserver":
			hasCashSignal = true
			reasons = append(reasons, fmt.Sprintf("В known_components обнаружен %s.", component.Key))
		case "atol-drivers-10":
			hasFiscalSignal = true
			recommendedAdapters = append(recommendedAdapters, "fiscal-atol")
			reasons = append(reasons, "В known_components обнаружен atol-drivers-10, это сильный признак использования АТОЛ.")
		}
	}

	for _, port := range inventory.COMPorts {
		candidate := api.AgentCOMSignatureCandidateDTO{
			PortName:             strings.TrimSpace(port.Name),
			FriendlyName:         strings.TrimSpace(firstNonEmptyString(port.FriendlyName, port.Description, port.Device)),
			SignatureKey:         normalizeLower(port.SignatureKey),
			VendorID:             normalizeLower(port.VendorID),
			ProductID:            normalizeLower(port.ProductID),
			ClassificationLabel:  strings.TrimSpace(classificationValue(port.Classification, func(v *api.InventoryCOMPortClassificationDTO) string { return v.Label })),
			ClassificationSource: normalizeLower(classificationValue(port.Classification, func(v *api.InventoryCOMPortClassificationDTO) string { return v.Source })),
			DeviceType:           normalizeLower(classificationValue(port.Classification, func(v *api.InventoryCOMPortClassificationDTO) string { return v.DeviceType })),
			SuggestedAdapter:     normalizeLower(classificationValue(port.Classification, func(v *api.InventoryCOMPortClassificationDTO) string { return v.SuggestedAdapter })),
		}

		if rule, ok := signatureRules[candidate.SignatureKey]; ok {
			ruleDTO := buildRuleDTO(rule)
			candidate.ExistingRule = &ruleDTO
			if candidate.DeviceType == "" {
				candidate.DeviceType = normalizeLower(rule.DeviceType)
			}
			if candidate.SuggestedAdapter == "" {
				candidate.SuggestedAdapter = normalizeLower(rule.SuggestedAdapter)
			}
			reasons = append(reasons, fmt.Sprintf("Для COM-сигнатуры %s уже есть серверное правило.", candidate.SignatureKey))
		} else if candidate.SignatureKey != "" {
			warnings = append(warnings, fmt.Sprintf("Для COM-сигнатуры %s ещё нет серверного правила.", candidate.SignatureKey))
		}

		if candidate.DeviceType != "" || candidate.SuggestedAdapter != "" || candidate.SignatureKey != "" {
			candidates = append(candidates, candidate)
		}

		adapterID := normalizeLower(firstNonEmptyString(candidate.SuggestedAdapter, deviceTypeToAdapter[candidate.DeviceType]))
		if adapterID != "" {
			recommendedAdapters = append(recommendedAdapters, adapterID)
			hasFiscalSignal = true
			reasons = append(reasons, fmt.Sprintf("COM-порт %s указывает на адаптер %s.", firstNonEmptyString(candidate.PortName, candidate.SignatureKey), adapterID))
		} else if candidate.DeviceType != "" && strings.Contains(candidate.DeviceType, "fiscal") {
			hasFiscalSignal = true
			reasons = append(reasons, fmt.Sprintf("COM-порт %s классифицирован как %s.", firstNonEmptyString(candidate.PortName, candidate.SignatureKey), candidate.DeviceType))
		}
	}

	recommendedAdapters = uniqueNonEmptyStrings(recommendedAdapters)
	profileKey := resolveProfileKey(hasCashSignal, hasFiscalSignal)
	profile := buildProfileDTO(machineProfiles[profileKey], "recommendation", nil, "")
	if hasCashSignal && len(recommendedAdapters) == 0 {
		warnings = append(warnings, "Профиль POS-станции определён, но отдельный POS-адаптер пока не опубликован в серверном каталоге.")
	}
	if hasFiscalSignal && len(recommendedAdapters) == 0 {
		warnings = append(warnings, "Есть признаки фискального оборудования, но не удалось однозначно подобрать adapter_manifest без дополнительного правила.")
	}

	return profile, uniqueNonEmptyStrings(reasons), buildManifestTemplates(recommendedAdapters), slices.Clip(candidates), uniqueNonEmptyStrings(warnings)
}

func resolveProfileKey(hasCashSignal, hasFiscalSignal bool) string {
	switch {
	case hasCashSignal && hasFiscalSignal:
		return "hybrid-pos-fiscal"
	case hasCashSignal:
		return "pos-workstation"
	case hasFiscalSignal:
		return "fiscal-workstation"
	default:
		return "service-workstation"
	}
}

func buildManifestTemplates(adapterIDs []string) []api.AdapterManifestDTO {
	if len(adapterIDs) == 0 {
		return nil
	}

	out := make([]api.AdapterManifestDTO, 0, len(adapterIDs))
	for _, adapterID := range adapterIDs {
		item, ok := adapterCatalog[normalizeLower(adapterID)]
		if !ok {
			continue
		}
		out = append(out, api.AdapterManifestDTO{
			AdapterID:       item.AdapterID,
			AdapterType:     item.AdapterType,
			ProtocolVersion: item.ProtocolVersion,
			TargetOS:        item.TargetOS,
		})
	}
	return slices.Clip(out)
}

func sanitizeMachineProfile(profile api.AgentMachineProfileDTO) api.AgentMachineProfileDTO {
	profile.Key = normalizeLower(profile.Key)
	profile.Title = strings.TrimSpace(profile.Title)
	profile.Summary = strings.TrimSpace(profile.Summary)
	profile.Source = normalizeLower(profile.Source)

	if definition, ok := machineProfiles[profile.Key]; ok {
		if profile.Title == "" {
			profile.Title = definition.Title
		}
		if profile.Summary == "" {
			profile.Summary = definition.Summary
		}
	}
	if profile.Source == "" {
		profile.Source = "operator"
	}
	return profile
}

func buildSavedProfileDTO(profile *api.AgentMachineProfileConfigDTO) *api.AgentMachineProfileDTO {
	if profile == nil {
		return nil
	}
	out := api.AgentMachineProfileDTO{
		Key:         normalizeLower(profile.Key),
		Title:       strings.TrimSpace(profile.Title),
		Summary:     strings.TrimSpace(profile.Summary),
		Source:      normalizeLower(profile.Source),
		ConfirmedAt: profile.ConfirmedAt,
		ConfirmedBy: strings.TrimSpace(profile.ConfirmedBy),
	}
	if definition, ok := machineProfiles[out.Key]; ok {
		if out.Title == "" {
			out.Title = definition.Title
		}
		if out.Summary == "" {
			out.Summary = definition.Summary
		}
	}
	return &out
}

func buildProfileDTO(def profileDefinition, source string, confirmedAt *time.Time, confirmedBy string) api.AgentMachineProfileDTO {
	return api.AgentMachineProfileDTO{
		Key:         def.Key,
		Title:       def.Title,
		Summary:     def.Summary,
		Source:      source,
		ConfirmedAt: confirmedAt,
		ConfirmedBy: confirmedBy,
	}
}

func buildRuleDTO(rule models.AgentCOMSignatureRule) api.AgentCOMSignatureRuleDTO {
	return api.AgentCOMSignatureRuleDTO{
		ID:               rule.ID,
		SignatureKey:     strings.TrimSpace(rule.SignatureKey),
		DeviceType:       strings.TrimSpace(rule.DeviceType),
		Label:            strings.TrimSpace(rule.Label),
		Confidence:       strings.TrimSpace(rule.Confidence),
		ProfileHint:      strings.TrimSpace(rule.ProfileHint),
		SuggestedAdapter: strings.TrimSpace(rule.SuggestedAdapter),
		Source:           strings.TrimSpace(rule.Source),
		Notes:            strings.TrimSpace(rule.Notes),
		UpdatedAt:        rule.UpdatedAt,
		UpdatedBy:        strings.TrimSpace(rule.UpdatedBy),
	}
}

func decodeInventorySnapshot(raw datatypes.JSON) (*api.InventorySnapshotDTO, string) {
	if len(raw) == 0 {
		return nil, ""
	}
	var out api.InventorySnapshotDTO
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, "Не удалось разобрать latest_inventory из базы."
	}
	return &out, ""
}

func decodeAdapterStatuses(raw datatypes.JSON) ([]api.AdapterStatusDTO, string) {
	if len(raw) == 0 {
		return nil, ""
	}
	var out []api.AdapterStatusDTO
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, "Не удалось разобрать latest_adapter_statuses из базы."
	}
	return out, ""
}

func decodeAgentConfig(raw datatypes.JSON) (api.AgentConfigDTO, string) {
	if len(raw) == 0 {
		return api.AgentConfigDTO{}, ""
	}
	var out api.AgentConfigDTO
	if err := json.Unmarshal(raw, &out); err != nil {
		return api.AgentConfigDTO{}, "Не удалось разобрать сохранённую конфигурацию агента."
	}
	return out, ""
}

func decodeAgentConfigForWrite(raw datatypes.JSON) (api.AgentConfigDTO, error) {
	if len(raw) == 0 {
		return api.AgentConfigDTO{}, nil
	}
	var out api.AgentConfigDTO
	if err := json.Unmarshal(raw, &out); err != nil {
		return api.AgentConfigDTO{}, fmt.Errorf("не удалось разобрать текущую конфигурацию агента: %w", err)
	}
	return out, nil
}

func decodeJSONAny(raw datatypes.JSON) (any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func normalizeManifestList(items []api.AdapterManifestDTO) []api.AdapterManifestDTO {
	if len(items) == 0 {
		return nil
	}

	out := make([]api.AdapterManifestDTO, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		normalized := api.AdapterManifestDTO{
			AdapterID:       normalizeLower(item.AdapterID),
			AdapterType:     normalizeLower(item.AdapterType),
			Version:         strings.TrimSpace(item.Version),
			TargetOS:        normalizeLower(item.TargetOS),
			TargetArch:      normalizeLower(item.TargetArch),
			ProtocolVersion: strings.TrimSpace(item.ProtocolVersion),
			DownloadURL:     strings.TrimSpace(item.DownloadURL),
			SHA256:          normalizeLower(item.SHA256),
			FileName:        strings.TrimSpace(item.FileName),
		}
		if catalogItem, ok := adapterCatalog[normalized.AdapterID]; ok {
			if normalized.AdapterType == "" {
				normalized.AdapterType = catalogItem.AdapterType
			}
			if normalized.ProtocolVersion == "" {
				normalized.ProtocolVersion = catalogItem.ProtocolVersion
			}
			if normalized.TargetOS == "" {
				normalized.TargetOS = catalogItem.TargetOS
			}
		}
		if normalized.AdapterID == "" {
			continue
		}
		if _, exists := seen[normalized.AdapterID]; exists {
			continue
		}
		seen[normalized.AdapterID] = struct{}{}
		out = append(out, normalized)
	}

	slices.SortFunc(out, func(left, right api.AdapterManifestDTO) int {
		return strings.Compare(left.AdapterID, right.AdapterID)
	})
	return slices.Clip(out)
}

func uniqueNonEmptyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func defaultStr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func adapterToDeviceType(adapterID string) string {
	switch normalizeLower(adapterID) {
	case "fiscal-atol":
		return "fiscal-atol"
	case "fiscal-mitsu":
		return "fiscal-mitsu"
	case "fiscal-shtrih":
		return "fiscal-shtrih"
	default:
		return ""
	}
}
