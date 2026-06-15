package services

import (
	"context"
	"encoding/json"
	"errors"
	"etalon-server/internal/domain/models"
	api "etalon-server/internal/transport/http/dtos"
	"fmt"
	"maps"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm/clause"
)

const (
	defaultAdapterRunCommand          = "run"
	defaultAdapterRunOperation        = "collect"
	defaultAdapterRunTimeoutSeconds   = 45
	defaultAdapterScheduleIntervalSec = 300

	// sagaIDDateFormat — компонент даты в saga_id для daily-детерминизма.
	// Один и тот же агент + адаптер в течение суток получают одинаковый saga_id,
	// что позволяет partial unique index отсеивать дубли на уровне БД.
	sagaIDDateFormat = "2006-01-02"
)

// generateSagaID создаёт детерминированный идентификатор саги для scheduled-запуска.
// Формат: {agent_uuid}/{adapter_id}/{date} — это позволяет partial unique index
// гарантировать, что в одни сутки для одного агента и адаптера создаётся
// не более одной scheduled-команды.
func generateSagaID(agentUUID, adapterID string) *string {
	id := agentUUID + "/" + adapterID + "/" + time.Now().UTC().Format(sagaIDDateFormat)
	return &id
}

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
	device.ConnectionType = normalizeLower(firstNonEmptyString(device.ConnectionType, device.Transport, inferRuntimeDeviceConnectionType(device)))
	device.Transport = device.ConnectionType
	device.Address = strings.TrimSpace(device.Address)
	device.IP = strings.TrimSpace(device.IP)
	device.COMPort = strings.TrimSpace(device.COMPort)
	device.BaudRate = strings.TrimSpace(device.BaudRate)
	device.Model = strings.TrimSpace(device.Model)
	if device.Port < 0 {
		device.Port = 0
	}
	if device.Address == "" {
		device.Address = runtimeDeviceAddressFromFields(device)
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
		strings.TrimSpace(device.Address) == "" &&
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
	deviceIndex := index + 1
	if _, err := normalizeRuntimeDeviceForExecution(device); err != nil {
		return fmt.Errorf("устройство #%d: %w", deviceIndex, err)
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
		for index, device := range profile.Devices {
			normalizedDevice, err := normalizeRuntimeDeviceForExecution(device)
			if err != nil {
				return api.AgentAdapterRunCommandPayloadDTO{}, fmt.Errorf("устройство #%d: %w", index+1, err)
			}

			item := map[string]any{
				"transport":       normalizedDevice.ConnectionType,
				"connection_type": normalizedDevice.ConnectionType,
			}
			if normalizedDevice.Label != "" {
				item["label"] = normalizedDevice.Label
			}
			if normalizedDevice.IP != "" {
				item["ip"] = normalizedDevice.IP
			}
			if normalizedDevice.Port > 0 {
				item["port"] = normalizedDevice.Port
			}
			if normalizedDevice.COMPort != "" {
				item["com_port"] = normalizedDevice.COMPort
			}
			if normalizedDevice.BaudRate != "" {
				item["baudrate"] = normalizedDevice.BaudRate
			}
			if normalizedDevice.Model != "" {
				item["model"] = normalizedDevice.Model
			}
			if len(normalizedDevice.DriverHints) > 0 {
				item["driver_hints"] = cloneJSONMap(normalizedDevice.DriverHints)
			}
			if len(normalizedDevice.ExtraParams) > 0 {
				item["extra_params"] = cloneJSONMap(normalizedDevice.ExtraParams)
			}
			devices = append(devices, item)
		}
		payload.DeviceParams = map[string]any{"devices": devices}
	}

	return payload, nil
}

func normalizeRuntimeDeviceForExecution(device api.AgentAdapterRuntimeDeviceDTO) (api.AgentAdapterRuntimeDeviceDTO, error) {
	device = sanitizeAdapterRuntimeDevice(device)
	switch device.ConnectionType {
	case "tcp":
		address := strings.TrimSpace(firstNonEmptyString(device.Address, runtimeDeviceAddressFromFields(device)))
		if address == "" {
			return api.AgentAdapterRuntimeDeviceDTO{}, errors.New("для tcp-подключения обязателен address в формате ip[:port]")
		}

		host, port, hasPort, err := parseTCPRuntimeAddress(address)
		if err != nil {
			return api.AgentAdapterRuntimeDeviceDTO{}, fmt.Errorf("не удалось разобрать tcp address %q: %w", address, err)
		}

		device.Address = formatTCPRuntimeAddress(host, port, hasPort)
		device.IP = host
		if hasPort {
			device.Port = port
		} else {
			device.Port = 0
		}
		device.COMPort = ""
		return device, nil
	case "com":
		address := strings.TrimSpace(firstNonEmptyString(device.Address, device.COMPort))
		if address == "" {
			return api.AgentAdapterRuntimeDeviceDTO{}, errors.New("для com-подключения обязателен address в формате COMx")
		}

		comPort, err := normalizeCOMRuntimeAddress(address)
		if err != nil {
			return api.AgentAdapterRuntimeDeviceDTO{}, err
		}

		device.Address = comPort
		device.COMPort = comPort
		device.IP = ""
		device.Port = 0
		return device, nil
	default:
		return api.AgentAdapterRuntimeDeviceDTO{}, errors.New("connection_type должен быть tcp или com")
	}
}

func runtimeDeviceAddressFromFields(device api.AgentAdapterRuntimeDeviceDTO) string {
	switch normalizeLower(device.ConnectionType) {
	case "tcp":
		host := strings.TrimSpace(device.IP)
		if host == "" {
			return ""
		}
		return formatTCPRuntimeAddress(host, device.Port, device.Port > 0)
	case "com":
		if strings.TrimSpace(device.COMPort) == "" {
			return ""
		}

		comPort, err := normalizeCOMRuntimeAddress(device.COMPort)
		if err != nil {
			return strings.TrimSpace(device.COMPort)
		}
		return comPort
	default:
		return ""
	}
}

func inferRuntimeDeviceConnectionType(device api.AgentAdapterRuntimeDeviceDTO) string {
	switch {
	case strings.TrimSpace(device.COMPort) != "":
		return "com"
	case strings.TrimSpace(device.IP) != "" || device.Port > 0:
		return "tcp"
	case strings.HasPrefix(strings.ToUpper(strings.TrimSpace(device.Address)), "COM"):
		return "com"
	case strings.TrimSpace(device.Address) != "":
		return "tcp"
	default:
		return ""
	}
}

func parseTCPRuntimeAddress(raw string) (host string, port int, hasPort bool, err error) {
	address := strings.TrimSpace(raw)
	if address == "" {
		return "", 0, false, errors.New("tcp address пустой")
	}

	if strings.HasPrefix(address, "[") {
		if strings.HasSuffix(address, "]") {
			host = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(address, "["), "]"))
			if host == "" {
				return "", 0, false, errors.New("в tcp address отсутствует хост")
			}
			return host, 0, false, nil
		}

		hostPart, portPart, splitErr := net.SplitHostPort(address)
		if splitErr != nil {
			return "", 0, false, splitErr
		}

		parsedPort, parseErr := parseTCPRuntimePort(portPart)
		if parseErr != nil {
			return "", 0, false, parseErr
		}

		host = strings.TrimSpace(hostPart)
		if host == "" {
			return "", 0, false, errors.New("в tcp address отсутствует хост")
		}
		return host, parsedPort, true, nil
	}

	switch strings.Count(address, ":") {
	case 0:
		return address, 0, false, nil
	case 1:
		hostPart, portPart, _ := strings.Cut(address, ":")
		host = strings.TrimSpace(hostPart)
		if host == "" {
			return "", 0, false, errors.New("в tcp address отсутствует хост")
		}

		parsedPort, parseErr := parseTCPRuntimePort(portPart)
		if parseErr != nil {
			return "", 0, false, parseErr
		}
		return host, parsedPort, true, nil
	default:
		return address, 0, false, nil
	}
}

func parseTCPRuntimePort(raw string) (int, error) {
	portText := strings.TrimSpace(raw)
	if portText == "" {
		return 0, errors.New("в tcp address отсутствует port")
	}

	port, err := strconv.Atoi(portText)
	if err != nil {
		return 0, fmt.Errorf("port %q не является числом", portText)
	}
	if port <= 0 || port > 65535 {
		return 0, fmt.Errorf("port %d вне диапазона 1..65535", port)
	}
	return port, nil
}

func formatTCPRuntimeAddress(host string, port int, hasPort bool) string {
	if !hasPort || port <= 0 {
		return strings.TrimSpace(host)
	}
	return net.JoinHostPort(strings.TrimSpace(host), strconv.Itoa(port))
}

func normalizeCOMRuntimeAddress(raw string) (string, error) {
	value := strings.ToUpper(strings.TrimSpace(raw))
	if value == "" {
		return "", errors.New("для com-подключения обязателен address в формате COMx")
	}
	if !strings.HasPrefix(value, "COM") {
		return "", fmt.Errorf("для com-подключения ожидается address в формате COMx, получено %q", raw)
	}

	portNumber := strings.TrimSpace(strings.TrimPrefix(value, "COM"))
	if portNumber == "" {
		return "", fmt.Errorf("для com-подключения ожидается address в формате COMx, получено %q", raw)
	}

	number, err := strconv.Atoi(portNumber)
	if err != nil || number <= 0 {
		return "", fmt.Errorf("для com-подключения ожидается address в формате COMx, получено %q", raw)
	}
	return fmt.Sprintf("COM%d", number), nil
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

	// Ручной запуск — без saga_id, дублирование запрещено через hasPendingAdapterRunCommand.
	return s.enqueueAdapterRunLocked(ctx, &agent, normalizeLower(req.AdapterID), false, nil)
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

		if err := s.enqueueAdapterRunLocked(ctx, agent, adapterID, true, generateSagaID(agent.UUID, adapterID)); err != nil {
			return err
		}
	}

	return nil
}

func (s *agentOperatorFlowService) enqueueAdapterRunLocked(ctx context.Context, agent *models.Agent, adapterID string, skipIfPending bool, sagaID *string) error {
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

	// Проверяем наличие pending-команды для адаптера. При scheduled-запуске
	// это оптимизация, а ON CONFLICT DO NOTHING на partial unique index —
	// финальная страховка от гонки двух подов agent-gateway.
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

	cmd := models.AgentCommand{
		AgentUUID: agent.UUID,
		Type:      "run_adapter",
		Payload:   datatypes.JSON(raw),
		Status:    "new",
		SagaID:    sagaID,
	}

	if sagaID != nil {
		// Scheduled-запуск: ON CONFLICT DO NOTHING — если два пода agent-gateway
		// одновременно создают команду с одинаковым saga_id, только одна succeeded.
		return s.db.WithContext(ctx).
			Clauses(clause.OnConflict{DoNothing: true}).
			Create(&cmd).Error
	}

	return s.db.WithContext(ctx).Create(&cmd).Error
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
