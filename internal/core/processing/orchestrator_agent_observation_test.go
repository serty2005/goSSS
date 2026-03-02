package processing

import (
	"context"
	"etalon-server/internal/core/events"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/services"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/pkg/eventbus"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type stubObservationService struct {
	mu        sync.Mutex
	calls     int
	lastSrc   string
	lastData  *api.AgentDataDTO
	returnErr error
}

func (s *stubObservationService) ApplyObservation(_ context.Context, source string, data *api.AgentDataDTO) (*models.AgentObservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.lastSrc = source
	if data != nil {
		copyData := *data
		s.lastData = &copyData
	}
	return &models.AgentObservation{}, s.returnErr
}

func (s *stubObservationService) ApproveCandidate(_ context.Context, _ services.CandidateApproveInput) (*models.Candidate, error) {
	return nil, nil
}

func (s *stubObservationService) RejectCandidate(_ context.Context, _ services.CandidateRejectInput) (*models.Candidate, error) {
	return nil, nil
}

func (s *stubObservationService) RecalculateCandidates(_ context.Context) (*services.CandidateRecalculationResult, error) {
	return &services.CandidateRecalculationResult{}, nil
}

type stubProcessingEngine struct{}

func (s *stubProcessingEngine) ProcessAgentData(_ context.Context, _ string, _ *api.AgentDataDTO) *ProcessingResult {
	return &ProcessingResult{}
}

func (s *stubProcessingEngine) ProcessDuplicates(_ context.Context, _ events.DuplicatesFoundPayload) *ProcessingResult {
	return &ProcessingResult{}
}

func (s *stubProcessingEngine) ProcessServiceDeskUpdate(_ context.Context, _ bool, _ string, _, _ interface{}) (*ProcessingResult, error) {
	return &ProcessingResult{}, nil
}

func (s *stubProcessingEngine) CompareModelsForUpdate(_ string, _, _ interface{}) (map[string]interface{}, error) {
	return nil, nil
}

func TestHandleAgentObservationRequested_ВызываетСервисНаблюдений(t *testing.T) {
	obs := &stubObservationService{}
	o := NewOrchestrator(logger.New("", "test", "error", true), nil, nil, nil, nil, nil, nil, nil, nil, nil, &stubProcessingEngine{}, obs)

	o.handleAgentObservationRequested(context.Background(), eventbus.Event{
		Type: events.AgentObservationRequested,
		Payload: events.AgentObservationPayload{
			Source: "agent-1",
			Data: api.AgentDataDTO{
				Hostname: "ws-1",
			},
		},
	})

	obs.mu.Lock()
	defer obs.mu.Unlock()
	require.Equal(t, 1, obs.calls)
	require.Equal(t, "agent-1", obs.lastSrc)
	require.NotNil(t, obs.lastData)
	require.Equal(t, "ws-1", obs.lastData.Hostname)
}

func TestHandleAgentObservationRequested_НекорректныйPayloadПропускается(t *testing.T) {
	obs := &stubObservationService{}
	o := NewOrchestrator(logger.New("", "test", "error", true), nil, nil, nil, nil, nil, nil, nil, nil, nil, &stubProcessingEngine{}, obs)

	o.handleAgentObservationRequested(context.Background(), eventbus.Event{
		Type:    events.AgentObservationRequested,
		Payload: "bad",
	})

	obs.mu.Lock()
	defer obs.mu.Unlock()
	require.Equal(t, 0, obs.calls)
}
