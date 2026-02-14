package company

import (
	"context"
	"encoding/json"
	domain "etalon-server/internal/domain"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/interfaces"
	"etalon-server/internal/domain/repositories"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/workstation"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/pkg/utils"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/validators"
	"fmt"
	"sync"
)

type serviceImpl struct {
	logger          logger.LoggerInterface
	tm              interfaces.Transactor
	companyRepo     company.Repository
	serverRepo      server.Repository
	workstationRepo workstation.Repository
	frRepo          fiscal.Repository
	linkRepo        repositories.LinkRepo
}

// NewService создает сервис с зависимостями от репозиториев оборудования.
func NewService(
	logger logger.LoggerInterface,
	tm interfaces.Transactor,
	companyRepo company.Repository,
	serverRepo server.Repository,
	workstationRepo workstation.Repository,
	frRepo fiscal.Repository,
	linkRepo repositories.LinkRepo,
) company.Service {
	return &serviceImpl{
		logger:          logger,
		tm:              tm,
		companyRepo:     companyRepo,
		serverRepo:      serverRepo,
		workstationRepo: workstationRepo,
		frRepo:          frRepo,
		linkRepo:        linkRepo,
	}
}

func (s *serviceImpl) CreateCompany(ctx context.Context, dto *api.CompanyCreateDTO) (*company.Company, error) {
	entity := &company.Company{
		Title:          dto.Title,
		Address:        dto.Address,
		AdditionalName: dto.AdditionalName,
	}

	err := s.tm.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.companyRepo.Create(txCtx, entity); err != nil {
			s.logger.Error("failed to create company", "error", err)
			return err
		}
		return nil
	})

	if err != nil {
		return nil, err
	}
	return entity, nil
}

func (s *serviceImpl) UpdateCompany(ctx context.Context, id string, data map[string]interface{}) error {
	delete(data, "id")
	delete(data, "created_at")
	delete(data, "updated_at")
	delete(data, "deleted_at")

	return s.tm.WithinTransaction(ctx, func(txCtx context.Context) error {
		updated, err := s.companyRepo.Update(txCtx, id, data)
		if err != nil {
			return err
		}
		if !updated {
			return domain.ErrNotFound
		}
		return nil
	})
}

func (s *serviceImpl) DeleteCompany(ctx context.Context, id string) error {
	return s.tm.WithinTransaction(ctx, func(txCtx context.Context) error {
		deleted, err := s.companyRepo.Delete(txCtx, id)
		if err != nil {
			return err
		}
		if !deleted {
			return domain.ErrNotFound
		}
		return nil
	})
}

func (s *serviceImpl) GetCompany(ctx context.Context, id string) (*company.Company, error) {
	return s.companyRepo.GetByID(ctx, id)
}

func (s *serviceImpl) SearchCompanies(ctx context.Context, term string, limit, offset int) ([]company.Company, error) {
	return s.companyRepo.Search(ctx, term, true, limit, offset)
}

// GetChildren возвращает список дочерних компаний для указанной hub-компании.
func (s *serviceImpl) GetChildren(ctx context.Context, companyID string) ([]company.Company, error) {
	// Проверяем существование родительской компании
	comp, err := s.companyRepo.GetByID(ctx, companyID)
	if err != nil {
		if err == domain.ErrNotFound {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("ошибка при проверке компании: %w", err)
	}
	if comp == nil {
		return nil, domain.ErrNotFound
	}

	// Получаем дочерние компании
	children, err := s.companyRepo.GetChildren(ctx, companyID)
	if err != nil {
		return nil, fmt.Errorf("ошибка при получении дочерних компаний: %w", err)
	}

	return children, nil
}

// GetInfrastructure возвращает плоский список оборудования компании.
func (s *serviceImpl) GetInfrastructure(ctx context.Context, companyID string) ([]api.FoundEntityDTO, error) {
	// 1. Проверяем существование компании
	comp, err := s.companyRepo.GetByID(ctx, companyID)
	if err != nil {
		if err == domain.ErrNotFound {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("ошибка при проверке компании: %w", err)
	}
	if comp == nil {
		return nil, domain.ErrNotFound
	}

	ownerIDs := []string{companyID}
	results := make([]api.FoundEntityDTO, 0)
	var mu sync.Mutex
	var wg sync.WaitGroup

	// 2. Параллельно запрашиваем оборудование
	wg.Add(3)

	// --- Серверы ---
	go func() {
		defer wg.Done()
		servers, err := s.serverRepo.FindByOwnerIDs(ctx, ownerIDs)
		if err != nil {
			s.logger.Error("GetInfrastructure: ошибка получения серверов", "error", err)
			return
		}
		for _, srv := range servers {
			// Получаем внешний ID
			link, _ := s.linkRepo.GetByInternalID(ctx, nil, "naumen", srv.ID)
			var extUUID *string
			if link != nil {
				extUUID = &link.ServiceDeskUUID
			}

			// Парсим детали статуса
			var statusDetails interface{}
			_ = json.Unmarshal(srv.StatusDetails, &statusDetails)

			partnersLink := validators.BuildPartnersPortalLink(
				utils.SafeStringDereference(srv.CabinetLink),
				utils.SafeStringDereference(srv.IP),
			)

			dto := api.FoundEntityDTO{
				EntityType: "Server",
				Data: api.ServerRichDTO{
					UUID:              srv.ID,
					ServiceDeskUUID:   extUUID,
					DeviceName:        srv.DeviceName,
					LastUpdatedBy:     srv.LastUpdatedBy,
					LastModifiedDate:  srv.LastModifiedDate,
					UpdatedAt:         srv.UpdatedAt,
					IP:                srv.IP,
					OperationalStatus: srv.Status,
					HealthStatus:      srv.HealthStatus,
					StatusDetails:     statusDetails,
					Anydesk:           srv.Anydesk,
					Teamviewer:        srv.Teamviewer,
					RDP:               srv.RDP,
					Litemanager:       srv.Litemanager,
					UniqueID:          srv.UniqueID,
					CRMid:             srv.CRMid,
					PartnersLink:      partnersLink,
					ServerName:        srv.ServerName,
					ServerVersion:     srv.ServerVersion,
					ServerEdition:     srv.ServerEdition,
					LastPolledAt:      srv.LastPolledAt,
				},
			}
			mu.Lock()
			results = append(results, dto)
			mu.Unlock()
		}
	}()

	// --- Рабочие станции ---
	go func() {
		defer wg.Done()
		workstations, err := s.workstationRepo.FindByOwnerIDs(ctx, ownerIDs)
		if err != nil {
			s.logger.Error("GetInfrastructure: ошибка получения РС", "error", err)
			return
		}
		for _, ws := range workstations {
			link, _ := s.linkRepo.GetByInternalID(ctx, nil, "naumen", ws.ID)
			var extUUID *string
			if link != nil {
				extUUID = &link.ServiceDeskUUID
			}
			var statusDetails interface{}
			_ = json.Unmarshal(ws.StatusDetails, &statusDetails)

			dto := api.FoundEntityDTO{
				EntityType: "Workstation",
				Data: api.WorkstationRichDTO{
					UUID:             ws.ID,
					ServiceDeskUUID:  extUUID,
					DeviceName:       ws.DeviceName,
					LastUpdatedBy:    ws.LastUpdatedBy,
					LastModifiedDate: ws.LastModifiedDate,
					UpdatedAt:        ws.UpdatedAt,
					IsNew:            ws.IsNew,
					HealthStatus:     ws.HealthStatus,
					StatusDetails:    statusDetails,
					Anydesk:          ws.Anydesk,
					Teamviewer:       ws.Teamviewer,
					Litemanager:      ws.Litemanager,
				},
			}
			mu.Lock()
			results = append(results, dto)
			mu.Unlock()
		}
	}()

	// --- Фискальные регистраторы ---
	go func() {
		defer wg.Done()
		frs, err := s.frRepo.FindByOwnerIDs(ctx, ownerIDs)
		if err != nil {
			s.logger.Error("GetInfrastructure: ошибка получения ФР", "error", err)
			return
		}
		for _, fr := range frs {
			link, _ := s.linkRepo.GetByInternalID(ctx, nil, "naumen", fr.ID)
			var extUUID *string
			if link != nil {
				extUUID = &link.ServiceDeskUUID
			}
			var statusDetails interface{}
			_ = json.Unmarshal(fr.StatusDetails, &statusDetails)

			dto := api.FoundEntityDTO{
				EntityType: "FiscalRegister",
				Data: api.FiscalRegisterRichDTO{
					UUID:               fr.ID,
					ServiceDeskUUID:    extUUID,
					LastUpdatedBy:      fr.LastUpdatedBy,
					LastModifiedDate:   fr.LastModifiedDate,
					UpdatedAt:          fr.UpdatedAt,
					HealthStatus:       fr.HealthStatus,
					StatusDetails:      statusDetails,
					RNKKT:              fr.RNKKT,
					ModelKKT:           fr.ModelKKT,
					SerialNumber:       fr.FRSerialNumber,
					FNNumber:           fr.FNNumber,
					FNRegistrationDate: fr.KKTRegDate,
					FNExpireDate:       fr.FNExpireDate,
					DriverVersion:      fr.DriverVersion,
					FRFirmware:         fr.FRFirmware,
					FRDownloader:       fr.FRDownloader,
					OrganizationName:   fr.LegalName,
					INN:                fr.INN,
				},
			}
			mu.Lock()
			results = append(results, dto)
			mu.Unlock()
		}
	}()

	wg.Wait()
	return results, nil
}
