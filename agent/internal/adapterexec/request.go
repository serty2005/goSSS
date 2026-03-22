package adapterexec

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"etalon-agent/internal/adapters"
	"etalon-agent/internal/protocol"

	"github.com/google/uuid"
)

type PreparedRunRequest struct {
	AdapterID string
	Command   string
	Operation string
	Timeout   time.Duration
	Input     json.RawMessage
}

type adapterRunPayload interface {
	GetAdapterID() string
	GetCommand() string
	GetOperation() string
	GetTimeout() string
	GetTimeoutSeconds() int
	GetProtocolVersion() string
	GetRequestID() string
	GetDeviceParams() json.RawMessage
	GetPayload() json.RawMessage
}

func PrepareRunRequest(req protocol.AdapterRunTaskPayload) (PreparedRunRequest, error) {
	return prepareRunRequest(adapterRunPayloadAdapter(req))
}

func PrepareSagaRunRequest(req protocol.SagaAdapterStepInput) (PreparedRunRequest, error) {
	return prepareRunRequest(sagaAdapterRunPayloadAdapter(req))
}

func ToRunRequest(req PreparedRunRequest) adapters.RunRequest {
	return adapters.RunRequest{
		AdapterID: req.AdapterID,
		Command:   req.Command,
		Timeout:   req.Timeout,
		Input:     cloneRawMessage(req.Input),
	}
}

func prepareRunRequest(req adapterRunPayload) (PreparedRunRequest, error) {
	adapterID := strings.TrimSpace(req.GetAdapterID())
	if adapterID == "" {
		return PreparedRunRequest{}, fmt.Errorf("в payload запуска адаптера отсутствует adapter_id")
	}

	command := strings.TrimSpace(req.GetCommand())
	if command == "" {
		command = "run"
	}

	timeout, err := resolveTimeout(req.GetTimeout(), req.GetTimeoutSeconds())
	if err != nil {
		return PreparedRunRequest{}, err
	}

	commandInput, err := buildCommandInput(
		command,
		strings.TrimSpace(req.GetOperation()),
		timeout,
		strings.TrimSpace(req.GetProtocolVersion()),
		strings.TrimSpace(req.GetRequestID()),
		req.GetDeviceParams(),
		req.GetPayload(),
	)
	if err != nil {
		return PreparedRunRequest{}, err
	}

	return PreparedRunRequest{
		AdapterID: adapterID,
		Command:   command,
		Operation: strings.TrimSpace(req.GetOperation()),
		Timeout:   timeout,
		Input:     commandInput,
	}, nil
}

func buildCommandInput(command, operation string, timeout time.Duration, protocolVersion, requestID string, deviceParams, payload json.RawMessage) (json.RawMessage, error) {
	if protocolVersion == "" {
		protocolVersion = "1"
	}
	if requestID == "" {
		requestID = uuid.NewString()
	}

	envelope := protocol.AdapterCommandInputDTO{
		ProtocolVersion: protocolVersion,
		RequestID:       requestID,
		TimeoutSeconds:  int(timeout / time.Second),
	}
	if envelope.TimeoutSeconds <= 0 {
		envelope.TimeoutSeconds = 1
	}

	switch strings.ToLower(strings.TrimSpace(command)) {
	case "run":
		if operation == "" {
			return nil, fmt.Errorf("для команды run обязательно поле operation")
		}
		envelope.TaskType = operation
		envelope.Payload = resolvePayload(deviceParams, payload)
	case "describe":
	case "health":
	default:
		envelope.TaskType = operation
		envelope.Payload = resolvePayload(deviceParams, payload)
	}

	raw, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("не удалось сериализовать payload адаптера: %w", err)
	}
	return raw, nil
}

func resolvePayload(deviceParams, payload json.RawMessage) json.RawMessage {
	for _, candidate := range []json.RawMessage{deviceParams, payload} {
		if len(strings.TrimSpace(string(candidate))) > 0 {
			return cloneRawMessage(candidate)
		}
	}
	return json.RawMessage(`{}`)
}

func resolveTimeout(raw string, seconds int) (time.Duration, error) {
	if seconds > 0 {
		return time.Duration(seconds) * time.Second, nil
	}

	value := strings.TrimSpace(raw)
	if value == "" {
		return 30 * time.Second, nil
	}

	if duration, err := time.ParseDuration(value); err == nil {
		if duration <= 0 {
			return 0, fmt.Errorf("timeout должен быть больше нуля")
		}
		return duration, nil
	}

	parsedSeconds, err := strconv.Atoi(value)
	if err != nil || parsedSeconds <= 0 {
		return 0, fmt.Errorf("не удалось разобрать timeout %q", value)
	}
	return time.Duration(parsedSeconds) * time.Second, nil
}

func cloneRawMessage(value json.RawMessage) json.RawMessage {
	if value == nil {
		return nil
	}
	return append(json.RawMessage(nil), value...)
}

type adapterRunPayloadAdapter protocol.AdapterRunTaskPayload

func (a adapterRunPayloadAdapter) GetAdapterID() string {
	return a.AdapterID
}

func (a adapterRunPayloadAdapter) GetCommand() string {
	return a.Command
}

func (a adapterRunPayloadAdapter) GetOperation() string {
	return a.Operation
}

func (a adapterRunPayloadAdapter) GetTimeout() string {
	return a.Timeout
}

func (a adapterRunPayloadAdapter) GetTimeoutSeconds() int {
	return a.TimeoutSeconds
}

func (a adapterRunPayloadAdapter) GetProtocolVersion() string {
	return a.ProtocolVersion
}

func (a adapterRunPayloadAdapter) GetRequestID() string {
	return a.RequestID
}

func (a adapterRunPayloadAdapter) GetDeviceParams() json.RawMessage {
	return a.DeviceParams
}

func (a adapterRunPayloadAdapter) GetPayload() json.RawMessage {
	return a.Payload
}

type sagaAdapterRunPayloadAdapter protocol.SagaAdapterStepInput

func (a sagaAdapterRunPayloadAdapter) GetAdapterID() string {
	return a.AdapterID
}

func (a sagaAdapterRunPayloadAdapter) GetCommand() string {
	return a.Command
}

func (a sagaAdapterRunPayloadAdapter) GetOperation() string {
	return a.Operation
}

func (a sagaAdapterRunPayloadAdapter) GetTimeout() string {
	return a.Timeout
}

func (a sagaAdapterRunPayloadAdapter) GetTimeoutSeconds() int {
	return a.TimeoutSeconds
}

func (a sagaAdapterRunPayloadAdapter) GetProtocolVersion() string {
	return a.ProtocolVersion
}

func (a sagaAdapterRunPayloadAdapter) GetRequestID() string {
	return a.RequestID
}

func (a sagaAdapterRunPayloadAdapter) GetDeviceParams() json.RawMessage {
	return a.DeviceParams
}

func (a sagaAdapterRunPayloadAdapter) GetPayload() json.RawMessage {
	return a.Payload
}
