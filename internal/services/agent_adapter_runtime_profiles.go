package services

import (
	"context"
	"encoding/json"
	"errors"
	"etalon-server/internal/domain/models"
	api "etalon-server/internal/transport/http/dtos"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"gorm.io/datatypes"
)

const (
	defaultAdapterRunCommand          = "run"
	defaultAdapterRunOperation        = "collect"
	defaultAdapterRunTimeoutSeconds   = 45
	defaultAdapterScheduleIntervalSec = 300
)

type adapterCommandPayload struct {
	AdapterID string `json:"adapter_id"`
}

func sanitizeAdapterRuntimeProfiles(profiles []api.AgentAdapterRuntimeProfileDTO) []api.AgentAdapterRuntimeProfileDTO {
	if len(profiles) == 0 {
		return nil
	}

	out := make([]api.AgentAdapterRuntimeProfileDTO, 0, len(profiles))
	seen := make(map[string]struct{}, len(profiles))
	for _, profile := range profiles {
		normalized := sanitizeAdapterRuntimeProfile(profile)
		if normalized.AdapterID == "" {
			continue
		}
		if _, exists := seen[normalized.AdapterID]; exists {
			continue
		}
		seen[normalized.AdapterID] = struct{}{}
		out = append(out, normalized)
	}

	slices.SortFunc(out, func(left, right api.AgentAdapterRuntimeProfileDTO) int {
		return strings.Compare(left.AdapterID, right.AdapterID)
	})
	return slices.Clip(out)
}

func sanitizeAdapterRuntimeProfile(profile api.AgentAdapterRuntimeProfileDTO) api.AgentAdapterRuntimeProfileDTO {
	profile.AdapterID = normalizeLower(profile.AdapterID)
	profile.Command = normalizeLower(defaultStr(profile.Command, defaultAdapterRunCommand))
	profile.Operation = normalizeLower(defaultStr(profile.Operation, defaultAdapterRunOperation))
	if profile.TimeoutSeconds <= 0 {
		profile.TimeoutSeconds = defaultAdapterRunTimeoutSeconds
	}

	profile.Devices = sanitizeAdapterRuntimeDevices(profile.Devices)
	profile.Schedule = sanitizeAdapterRuntimeSchedule(profile.Schedule)
	return profile
}

func sanitizeAdapterRuntimeDevices(devices []api.AgentAdapterRuntimeDeviceDTO) []api.AgentAdapterRuntimeDeviceDTO {
	if len(devices) == 0 {
		return nil
	}

	out := make([]api.AgentAdapterRuntimeDeviceDTO, 0, len(devices))
	for _, device := range devices {
		normalized := sanitizeAdapterRuntimeDevice(device)
		if isEmptyAdapterRuntimeDevice(normalized) {
			continue
		}
		out = append(out, normalized)
	}
	return slices.Clip(out)
}

func sanitizeAdapterRuntimeDevice(device api.AgentAdapterRuntimeDeviceDTO) api.AgentAdapterRuntimeDeviceDTO {
	device.Label = strings.TrimSpace(device.Label)
	device.ConnectionType = normalizeLower(firstNonEmptyString(device.ConnectionType, device.Transport))
	device.Transport = device.ConnectionType
	device.IP = strings.TrimSpace(device.IP)
	device.COMPort = strings.TrimSpace(device.COMPort)
	device.BaudRate = strings.TrimSpace(device.BaudRate)
	device.Model = strings.TrimSpace(device.Model)
	if device.Port < 0 {
		device.Port = 0
	}
	device.DriverHints = cloneJSONMap(device.DriverHints)
	device.ExtraParams = cloneJSONMap(device.ExtraParams)
	return device
}

func sanitizeAdapterRuntimeSchedule(schedule api.AgentAdapterRuntimeScheduleDTO) api.AgentAdapterRuntimeScheduleDTO {
	if !schedule.Enabled {
		if schedule.IntervalSeconds < 0 {
			schedule.IntervalSeconds = 0
		}
		schedule.LastRunAt = nil
		schedule.NextRunAt = nil
		return schedule
	}
	if schedule.IntervalSeconds <= 0 {
		schedule.IntervalSeconds = defaultAdapterScheduleIntervalSec
	}
	return schedule
}

func isEmptyAdapterRuntimeDevice(device api.AgentAdapterRuntimeDeviceDTO) bool {
	return strings.TrimSpace(device.Label) == "" &&
		normalizeLower(device.ConnectionType) == "" &&
		strings.TrimSpace(device.IP) == "" &&
		strings.TrimSpace(device.COMPort) == "" &&
		strings.TrimSpace(device.BaudRate) == "" &&
		device.Port == 0 &&
		strings.TrimSpace(device.Model) == "" &&
		len(device.DriverHints) == 0 &&
		len(device.ExtraParams) == 0
}

func cloneJSONMap(value map[string]any) map[string]any {
	if len(value) == 0 {
		return nil
	}

	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}

	var cloned map[string]any
	if err := json.Unmarshal(raw, &cloned); err != nil {
		return nil
	}
	return maps.Clone(cloned)
}

func buildRuntimeProfileWarnings(selectedAdapterIDs []string, profiles []api.AgentAdapterRuntimeProfileDTO, statuses []api.AdapterStatusDTO, now time.Time) []string {
	if len(selectedAdapterIDs) == 0 {
		return nil
	}

	warnings := make([]string, 0)
	profilesByAdapterID := runtimeProfilesByAdapterID(profiles)
	statusesByAdapterID := adapterStatusesByAdapterID(statuses)
	for _, adapterID := range selectedAdapterIDs {
		profile, ok := profilesByAdapterID[adapterID]
		if !ok {
			warnings = append(warnings, fmt.Sprintf("Для выбранного адаптера %s ещё не сохранены параметры подключения.", adapterID))
			continue
		}

		if err := validateRuntimeProfileForExecution(profile); err != nil {
			warnings = append(warnings, fmt.Sprintf("Профиль запуска %s пока неполон: %s.", adapterID, err.Error()))
			continue
		}

		if profile.Schedule.Enabled {
			status := statusesByAdapterID[adapterID]
			if status.LastRunAt == nil {
				warnings = append(warnings, fmt.Sprintf("Для адаптера %s включено расписание, но успешных запусков ещё не было.", adapterID))
				continue
			}

			nextRunAt := status.LastRunAt.Add(time.Duration(profile.Schedule.IntervalSeconds) * time.Second)
			if !nextRunAt.After(now.UTC()) {
				warnings = append(warnings, fmt.Sprintf("Для адаптера %s уже подошло время планового запуска.", adapterID))
			}
		}
	}

	return uniqueNonEmptyStrings(warnings)
}

func runtimeProfilesByAdapterID(profiles []api.AgentAdapterRuntimeProfileDTO) map[string]api.AgentAdapterRuntimeProfileDTO {
	if len(profiles) == 0 {
		return map[string]api.AgentAdapterRuntimeProfileDTO{}
	}

	out := make(map[string]api.AgentAdapterRuntimeProfileDTO, len(profiles))
	for _, profile := range profiles {
		if profile.AdapterID == "" {
			continue
		}
		out[profile.AdapterID] = profile
	}
	return out
}

func adapterStatusesByAdapterID(statuses []api.AdapterStatusDTO) map[string]api.AdapterStatusDTO {
	if len(statuses) == 0 {
		return map[string]api.AdapterStatusDTO{}
	}

	out := make(map[string]api.AdapterStatusDTO, len(statuses))
	for _, status := range statuses {
		adapterID := normalizeLower(status.AdapterID)
		if adapterID == "" {
			continue
		}
		out[adapterID] = status
	}
	return out
}

func enrichRuntimeProfilesWithStatus(profiles []api.AgentAdapterRuntimeProfileDTO, statuses []api.AdapterStatusDTO, now time.Time) []api.AgentAdapterRuntimeProfileDTO {
	if len(profiles) == 0 {
		return nil
	}

	statusesByAdapterID := adapterStatusesByAdapterID(statuses)
	out := make([]api.AgentAdapterRuntimeProfileDTO, 0, len(profiles))
	for _, profile := range profiles {
		enriched := profile
		status, ok := statusesByAdapterID[profile.AdapterID]
		if profile.Schedule.Enabled && ok && status.LastRunAt != nil {
			lastRunAt := status.LastRunAt.UTC()
			nextRunAt := lastRunAt.Add(time.Duration(profile.Schedule.IntervalSeconds) * time.Second)
			enriched.Schedule.LastRunAt = &lastRunAt
			if nextRunAt.After(now.UTC()) {
				enriched.Schedule.NextRunAt = &nextRunAt
			} else {
				nowUTC := now.UTC()
				enriched.Schedule.NextRunAt = &nowUTC
			}
		}
		out = append(out, enriched)
	}

	return slices.Clip(out)
}

func validateRuntimeProfileForExecution(profile api.AgentAdapterRuntimeProfileDTO) error {
	profile = sanitizeAdapterRuntimeProfile(profile)
	if profile.AdapterID == "" {
		return errors.New("adapter_id обязателен")
	}

	switch profile.Command {
	case "", defaultAdapterRunCommand:
		if len(profile.Devices) == 0 {
			return errors.New("не задано ни одного устройства")
		}
		for index, device := range profile.Devices {
			if err := validateRuntimeDeviceForExecution(device, index); err != nil {
				return err
			}
		}
	case "describe", "health":
	default:
		return fmt.Errorf("неподдерживаемая команда %q", profile.Command)
	}

	if profile.Schedule.Enabled && profile.Schedule.IntervalSeconds <= 0 {
		return errors.New("для расписания interval_seconds должен быть больше нуля")
	}
	return nil
}

func validateRuntimeDeviceForExecution(device api.AgentAdapterRuntimeDeviceDTO, index int) error {
	device = sanitizeAdapterRuntimeDevice(device)
	deviceIndex := index + 1
	switch device.ConnectionType {
	case "tcp":
		if device.IP == "" {
			return fmt.Errorf("устройство #%d: для tcp-подключения обязателен ip", deviceIndex)
		}
		if device.Port < 0 {
			return fmt.Errorf("устройство #%d: port не может быть отрицательным", deviceIndex)
		}
	case "com":
		if device.COMPort == "" {
			return fmt.Errorf("устройство #%d: для com-подключения обязателен com_port", deviceIndex)
		}
	default:
		return fmt.Errorf("устройство #%d: connection_type должен быть tcp или com", deviceIndex)
	}
	return nil
}

func buildAdapterRunCommandPayload(profile api.AgentAdapterRuntimeProfileDTO) (api.AgentAdapterRunCommandPayloadDTO, error) {
	profile = sanitizeAdapterRuntimeProfile(profile)
	if err := validateRuntimeProfileForExecution(profile); err != nil {
		return api.AgentAdapterRunCommandPayloadDTO{}, err
	}

	payload := api.AgentAdapterRunCommandPayloadDTO{
		AdapterID:      profile.AdapterID,
		Command:        profile.Command,
		Operation:      profile.Operation,
		TimeoutSeconds: profile.TimeoutSeconds,
	}

	if profile.Command == "run" {
		devices := make([]map[string]any, 0, len(profile.Devices))
		for _, device := range profile.Devices {
			item := map[string]any{
				"transport":       device.ConnectionType,
				"connection_type": device.ConnectionType,
			}
			if device.Label != "" {
				item["label"] = device.Label
			}
			if device.IP != "" {
				item["ip"] = device.IP
			}
			if device.Port > 0 {
				item["port"] = device.Port
			}
			if device.COMPort != "" {
				item["com_port"] = device.COMPort
			}
			if device.BaudRate != "" {
				item["baudrate"] = device.BaudRate
			}
			if device.Model != "" {
				item["model"] = device.Model
			}
			if len(device.DriverHints) > 0 {
				item["driver_hints"] = cloneJSONMap(device.DriverHints)
			}
			if len(device.ExtraParams) > 0 {
				item["extra_params"] = cloneJSONMap(device.ExtraParams)
			}
			devices = append(devices, item)
		}
		payload.DeviceParams = map[string]any{"devices": devices}
	}

	return payload, nil
}

func (s *agentOperatorFlowService) EnqueueAdapterRun(ctx context.Context, agentUUID string, req api.EnqueueAgentAdapterRunRequestDTO, actor string) error {
	agentUUID = strings.TrimSpace(agentUUID)
	if agentUUID == "" {
		return errors.New("uuid агента обязателен")
	}
	_ = actor

	var agent models.Agent
	if err := s.db.WithContext(ctx).Where("uuid = ?", agentUUID).First(&agent).Error; err != nil {
		return err
	}

	return s.enqueueAdapterRunLocked(ctx, &agent, normalizeLower(req.AdapterID), false)
}

func (s *agentOperatorFlowService) EnsureScheduledAdapterRuns(ctx context.Context, agent *models.Agent) error {
	if agent == nil {
		return nil
	}

	config, err := decodeAgentConfigForWrite(agent.Config)
	if err != nil {
		return err
	}

	selectedAdapterIDs, _ := selectedAdapterIDsFromConfig(config)
	if len(selectedAdapterIDs) == 0 || len(config.AdapterRuntimeProfiles) == 0 {
		return nil
	}

	profilesByAdapterID := runtimeProfilesByAdapterID(sanitizeAdapterRuntimeProfiles(config.AdapterRuntimeProfiles))
	statuses, _ := decodeAdapterStatuses(agent.LatestAdapterStatuses)
	statusesByAdapterID := adapterStatusesByAdapterID(statuses)
	now := time.Now().UTC()
	for _, adapterID := range selectedAdapterIDs {
		profile, ok := profilesByAdapterID[adapterID]
		if !ok || !profile.Schedule.Enabled {
			continue
		}
		if profile.Schedule.IntervalSeconds <= 0 {
			continue
		}
		if err := validateRuntimeProfileForExecution(profile); err != nil {
			continue
		}

		status, ok := statusesByAdapterID[adapterID]
		if ok && status.LastRunAt != nil {
			nextRunAt := status.LastRunAt.Add(time.Duration(profile.Schedule.IntervalSeconds) * time.Second)
			if nextRunAt.After(now) {
				continue
			}
		}

		if err := s.enqueueAdapterRunLocked(ctx, agent, adapterID, true); err != nil {
			return err
		}
	}

	return nil
}

func (s *agentOperatorFlowService) enqueueAdapterRunLocked(ctx context.Context, agent *models.Agent, adapterID string, skipIfPending bool) error {
	if agent == nil {
		return errors.New("агент не найден")
	}

	config, err := decodeAgentConfigForWrite(agent.Config)
	if err != nil {
		return err
	}

	selectedAdapterIDs, _ := selectedAdapterIDsFromConfig(config)
	if !slices.Contains(selectedAdapterIDs, adapterID) {
		return fmt.Errorf("адаптер %s не выбран для агента", adapterID)
	}

	profilesByAdapterID := runtimeProfilesByAdapterID(sanitizeAdapterRuntimeProfiles(config.AdapterRuntimeProfiles))
	profile, ok := profilesByAdapterID[adapterID]
	if !ok {
		return fmt.Errorf("для адаптера %s не сохранены параметры подключения", adapterID)
	}

	commandPayload, err := buildAdapterRunCommandPayload(profile)
	if err != nil {
		return fmt.Errorf("не удалось подготовить запуск %s: %w", adapterID, err)
	}

	pending, err := s.hasPendingAdapterRunCommand(ctx, agent.UUID, adapterID)
	if err != nil {
		return err
	}
	if pending {
		if skipIfPending {
			return nil
		}
		return fmt.Errorf("для адаптера %s уже есть незавершённая команда запуска", adapterID)
	}

	raw, err := json.Marshal(commandPayload)
	if err != nil {
		return fmt.Errorf("не удалось сериализовать payload команды запуска: %w", err)
	}

	return s.db.WithContext(ctx).Create(&models.AgentCommand{
		AgentUUID: agent.UUID,
		Type:      "run_adapter",
		Payload:   datatypes.JSON(raw),
		Status:    "new",
	}).Error
}

func (s *agentOperatorFlowService) hasPendingAdapterRunCommand(ctx context.Context, agentUUID, adapterID string) (bool, error) {
	var commands []models.AgentCommand
	if err := s.db.WithContext(ctx).
		Where("agent_uuid = ? AND status IN ?", agentUUID, []string{"new", "sent"}).
		Where("type IN ?", []string{"run_adapter", "adapter_run"}).
		Find(&commands).Error; err != nil {
		return false, err
	}

	for _, command := range commands {
		var payload adapterCommandPayload
		if err := json.Unmarshal(command.Payload, &payload); err != nil {
			continue
		}
		if normalizeLower(payload.AdapterID) == adapterID {
			return true, nil
		}
	}
	return false, nil
}
