package services

import (
	"context"
	"etalon-server/internal/api"
	"etalon-server/internal/repositories"
	"etalon-server/internal/utils"
	"etalon-server/internal/validators"

	"go.uber.org/zap"
)

// MatchedEntity результат работы EntityMatcherService.
type MatchedEntity struct {
	Entity     interface{} // Найденная сущность (*models.Server, *models.Workstation, etc.)
	EntityType string      // 'Server', 'Workstation', 'FiscalRegister'
	OwnerUUID  string
}

// EntityMatcherService определяет интерфейс для сервиса идентификации сущностей по данным от агента.
type EntityMatcherService interface {
	FindEntityByAgentData(ctx context.Context, data *api.AgentDataDTO) *MatchedEntity
}

type entityMatcherServiceImpl struct {
	logger          *zap.Logger
	serverRepo      repositories.ServerRepo
	workstationRepo repositories.WorkstationRepo
	frRepo          repositories.FiscalRegisterRepo
}

// NewEntityMatcherService создает новый экземпляр сервиса.
func NewEntityMatcherService(
	logger *zap.Logger,
	serverRepo repositories.ServerRepo,
	workstationRepo repositories.WorkstationRepo,
	frRepo repositories.FiscalRegisterRepo,
) EntityMatcherService {
	return &entityMatcherServiceImpl{logger, serverRepo, workstationRepo, frRepo}
}

// FindEntityByAgentData выполняет "водопадную" логику поиска.
func (s *entityMatcherServiceImpl) FindEntityByAgentData(ctx context.Context, data *api.AgentDataDTO) *MatchedEntity {
	log := s.logger.With(zap.String("agent_hostname", data.Hostname))

	normalizedIP := validators.ValidateIPAddress(data.URLRms)

	// Приоритет 1: Поиск по Серверу
	if server, _ := s.serverRepo.FindByCRMidOrIP(ctx, data.CRMID, utils.SafeStringDereference(normalizedIP)); server != nil {
		log.Info("Найдено совпадение по Серверу", zap.String("uuid", *server.ServiceDeskUUID))
		return &MatchedEntity{
			Entity:     server,
			EntityType: "Server",
			OwnerUUID:  utils.SafeStringDereference(server.OwnerServiceDeskUUID),
		}
	}

	// Приоритет 2: Поиск по Рабочей станции
	if ws, _ := s.workstationRepo.FindByRemoteIDs(ctx, data.TeamviewerID, data.AnydeskID, data.LitemanagerID); ws != nil {
		log.Info("Найдено совпадение по Рабочей станции", zap.String("uuid", *ws.ServiceDeskUUID))
		return &MatchedEntity{
			Entity:     ws,
			EntityType: "Workstation",
			OwnerUUID:  utils.SafeStringDereference(ws.OwnerServiceDeskUUID),
		}
	}

	// Приоритет 3: Поиск по Фискальному регистратору
	if fr, _ := s.frRepo.FindBySerialNumber(ctx, data.SerialNumber); fr != nil {
		log.Info("Найдено совпадение по Фискальному регистратору", zap.String("uuid", *fr.ServiceDeskUUID))
		return &MatchedEntity{
			Entity:     fr,
			EntityType: "FiscalRegister",
			OwnerUUID:  utils.SafeStringDereference(fr.OwnerServiceDeskUUID),
		}
	}

	log.Warn("Не найдено совпадений ни по одному из приоритетов.")
	return nil
}