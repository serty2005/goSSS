package services

import (
	"context"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/infra/logger"
	infrarepos "etalon-server/internal/infra/repositories"
	api "etalon-server/internal/transport/http/dtos"

	"gorm.io/gorm"
)

type CandidateApproveInput struct {
	CandidateID       uint
	CompanyID         string
	ServerID          *string
	ServerCRMID       *string
	ServerURL         *string
	ServerUniqueID    *string
	ServerCabinetLink *string
	ServerName        *string
	ServerDesc        *string
	Comment           *string

	CompanyTitle          *string
	CompanyAddress        *string
	CompanyAdditionalName *string
	CompanyParentID       *string
	ContractMode          *string
	ContractType          *string

	Workstations []CandidateWorkstationInput
}

type CandidateWorkstationInput struct {
	StagingID       *uint
	WorkstationUUID *string
	Name            string
}

type AgentObservationService interface {
	ApplyObservation(ctx context.Context, source string, data *api.AgentDataDTO) (*models.AgentObservation, error)
	ApproveCandidate(ctx context.Context, in CandidateApproveInput) (*models.Candidate, error)
}

type agentObservationStorage interface {
	ApplyObservation(ctx context.Context, source string, data *api.AgentDataDTO) (*models.AgentObservation, error)
	ApproveCandidate(ctx context.Context, in infrarepos.CandidateApproveInput) (*models.Candidate, error)
}

type agentObservationServiceImpl struct {
	storage agentObservationStorage
}

func NewAgentObservationService(logger logger.LoggerInterface, db *gorm.DB) AgentObservationService {
	return &agentObservationServiceImpl{
		storage: infrarepos.NewAgentObservationRepo(logger, db),
	}
}

func (s *agentObservationServiceImpl) ApplyObservation(ctx context.Context, source string, data *api.AgentDataDTO) (*models.AgentObservation, error) {
	return s.storage.ApplyObservation(ctx, source, data)
}

func (s *agentObservationServiceImpl) ApproveCandidate(ctx context.Context, in CandidateApproveInput) (*models.Candidate, error) {
	mapped := infrarepos.CandidateApproveInput{
		CandidateID:           in.CandidateID,
		CompanyID:             in.CompanyID,
		ServerID:              in.ServerID,
		ServerCRMID:           in.ServerCRMID,
		ServerURL:             in.ServerURL,
		ServerUniqueID:        in.ServerUniqueID,
		ServerCabinetLink:     in.ServerCabinetLink,
		ServerName:            in.ServerName,
		ServerDesc:            in.ServerDesc,
		Comment:               in.Comment,
		CompanyTitle:          in.CompanyTitle,
		CompanyAddress:        in.CompanyAddress,
		CompanyAdditionalName: in.CompanyAdditionalName,
		CompanyParentID:       in.CompanyParentID,
		ContractMode:          in.ContractMode,
		ContractType:          in.ContractType,
	}
	if len(in.Workstations) > 0 {
		mapped.Workstations = make([]infrarepos.CandidateWorkstationInput, 0, len(in.Workstations))
		for _, ws := range in.Workstations {
			mapped.Workstations = append(mapped.Workstations, infrarepos.CandidateWorkstationInput{
				StagingID:       ws.StagingID,
				WorkstationUUID: ws.WorkstationUUID,
				Name:            ws.Name,
			})
		}
	}
	return s.storage.ApproveCandidate(ctx, mapped)
}
