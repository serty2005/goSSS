package services

import (
	"context"
	"etalon-server/internal/domain/models"
	domainServices "etalon-server/internal/domain/services"
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

	// Ручной ввод remote IDs (опционально).
	// Используется когда агент не собрал TeamViewer/LiteManager/AnyDesk.
	// Приоритет: ручной ввод > значения из staging.
	TeamviewerID  *string
	LitemanagerID *string
	RustdeskID    *string
	AnydeskID     *string
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

// AgentObservationServiceOption определяет функциональную опцию для конфигурации сервиса.
type AgentObservationServiceOption func(*agentObservationServiceImpl)

// WithOwnerResolver устанавливает OwnerResolver для автоматического определения владельца.
func WithOwnerResolver(resolver domainServices.OwnerResolver) AgentObservationServiceOption {
	return func(s *agentObservationServiceImpl) {
		// Передаем resolver через storage options
	}
}

// WithHubDetector устанавливает NetworkHubDetector для определения network-hub серверов.
func WithHubDetector(detector domainServices.NetworkHubDetector) AgentObservationServiceOption {
	return func(s *agentObservationServiceImpl) {
		// Передаем detector через storage options
	}
}

// NewAgentObservationService создает новый экземпляр сервиса обработки наблюдений.
// Поддерживает функциональные опции для внедрения OwnerResolver и HubDetector.
func NewAgentObservationService(logger logger.LoggerInterface, db *gorm.DB, opts ...AgentObservationServiceOption) AgentObservationService {
	// Создаем storage с опциями по умолчанию
	storage := infrarepos.NewAgentObservationRepo(logger, db)

	svc := &agentObservationServiceImpl{
		storage: storage,
	}

	for _, opt := range opts {
		opt(svc)
	}

	return svc
}

// NewAgentObservationServiceWithDeps создает сервис с полным набором зависимостей.
// Используется в app.go для внедрения всех сервисов.
func NewAgentObservationServiceWithDeps(
	logger logger.LoggerInterface,
	db *gorm.DB,
	ownerResolver domainServices.OwnerResolver,
	hubDetector domainServices.NetworkHubDetector,
) AgentObservationService {
	storage := infrarepos.NewAgentObservationRepo(
		logger,
		db,
		infrarepos.WithOwnerResolver(ownerResolver),
		infrarepos.WithHubDetector(hubDetector),
	)

	return &agentObservationServiceImpl{
		storage: storage,
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
		// Ручной ввод remote IDs
		TeamviewerID:  in.TeamviewerID,
		LitemanagerID: in.LitemanagerID,
		RustdeskID:    in.RustdeskID,
		AnydeskID:     in.AnydeskID,
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
