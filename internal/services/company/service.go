package company

import (
	"context"
	"encoding/json"
	domain "etalon-server/internal/domain"
	"etalon-server/internal/domain/bitrix"
	"etalon-server/internal/domain/company"
	contractdom "etalon-server/internal/domain/contract"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/interfaces"
	"etalon-server/internal/domain/repositories"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/workstation"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/pkg/utils"
	contractsvc "etalon-server/internal/services/contract"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/validators"
	"fmt"
	"strings"
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
	bitrixRepo      bitrix.Repository
	contractSvc     contractdom.Service
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
	bitrixRepo bitrix.Repository,
	contractSvc contractdom.Service,
) company.Service {
	return &serviceImpl{
		logger:          logger,
		tm:              tm,
		companyRepo:     companyRepo,
		serverRepo:      serverRepo,
		workstationRepo: workstationRepo,
		frRepo:          frRepo,
		linkRepo:        linkRepo,
		bitrixRepo:      bitrixRepo,
		contractSvc:     contractSvc,
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

func (s *serviceImpl) SearchCompanies(ctx context.Context, term string, limit, offset int) ([]company.Company, int64, error) {
	return s.companyRepo.SearchWithTotal(ctx, term, true, limit, offset)
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
					IikoWebLink:       srv.IikoWebLink,
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
					Rustdesk:         ws.Rustdesk,
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

func (s *serviceImpl) ListBitrixMappings(ctx context.Context, term string, limit, offset int) ([]company.BitrixMappingRow, error) {
	companies, err := s.companyRepo.Search(ctx, term, true, limit, offset)
	if err != nil {
		return nil, err
	}
	if len(companies) == 0 {
		return []company.BitrixMappingRow{}, nil
	}

	companyIDs := make([]string, 0, len(companies))
	for _, item := range companies {
		companyIDs = append(companyIDs, item.ID)
	}

	mappings, err := s.bitrixRepo.ListCompanyServicePointMappingsByCompanyIDs(ctx, companyIDs)
	if err != nil {
		return nil, err
	}

	mappingByCompanyID := make(map[string]bitrix.CompanyServicePointMapping, len(mappings))
	pointIDs := make([]int64, 0, len(mappings))
	for _, item := range mappings {
		mappingByCompanyID[item.CompanyID] = item
		pointIDs = append(pointIDs, item.BitrixServicePointID)
	}

	points, err := s.bitrixRepo.ListServicePointsByIDs(ctx, pointIDs)
	if err != nil {
		return nil, err
	}
	pointByID := make(map[int64]bitrix.ServicePoint, len(points))
	for _, item := range points {
		pointByID[item.B24ElementID] = item
	}

	result := make([]company.BitrixMappingRow, 0, len(companies))
	for _, item := range companies {
		row := company.BitrixMappingRow{
			Company: item,
		}
		mapping, ok := mappingByCompanyID[item.ID]
		if ok {
			id := mapping.BitrixServicePointID
			row.BitrixServicePointID = &id
			if point, pointOK := pointByID[id]; pointOK {
				name := point.Name
				row.BitrixServicePointName = &name
				row.BitrixServicePointCode = point.OneCCode
				row.BitrixServicePointStatus = point.ContractOn
			}
		}
		result = append(result, row)
	}

	return result, nil
}

func (s *serviceImpl) GetBitrixMappingByCompanyID(ctx context.Context, companyID string) (*company.BitrixMappingRow, error) {
	normalizedCompanyID := strings.TrimSpace(companyID)
	if normalizedCompanyID == "" {
		return nil, nil
	}

	comp, err := s.companyRepo.GetByID(ctx, normalizedCompanyID)
	if err != nil {
		return nil, err
	}
	if comp == nil {
		return nil, domain.ErrNotFound
	}

	row := &company.BitrixMappingRow{Company: *comp}
	mapping, err := s.bitrixRepo.GetCompanyServicePointMappingByCompanyID(ctx, normalizedCompanyID)
	if err != nil {
		return nil, err
	}
	if mapping == nil || mapping.BitrixServicePointID <= 0 {
		return row, nil
	}

	id := mapping.BitrixServicePointID
	row.BitrixServicePointID = &id
	point, err := s.bitrixRepo.GetServicePointByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if point != nil {
		name := point.Name
		row.BitrixServicePointName = &name
		row.BitrixServicePointCode = point.OneCCode
		row.BitrixServicePointStatus = point.ContractOn
	}
	return row, nil
}

func (s *serviceImpl) UpdateBitrixMapping(ctx context.Context, companyID *string, bitrixServicePointID *int64) error {
	normalizedCompanyID := ""
	if companyID != nil {
		normalizedCompanyID = strings.TrimSpace(*companyID)
	}

	var normalizedPointID *int64
	if bitrixServicePointID != nil && *bitrixServicePointID > 0 {
		id := *bitrixServicePointID
		normalizedPointID = &id
	}

	if normalizedCompanyID == "" && normalizedPointID == nil {
		return fmt.Errorf("не переданы данные для обновления сопоставления")
	}

	if normalizedCompanyID != "" {
		comp, err := s.companyRepo.GetByID(ctx, normalizedCompanyID)
		if err != nil {
			return err
		}
		if comp == nil {
			return domain.ErrNotFound
		}
	}

	if normalizedPointID != nil {
		point, err := s.bitrixRepo.GetServicePointByID(ctx, *normalizedPointID)
		if err != nil {
			return err
		}
		if point == nil {
			return domain.ErrNotFound
		}
	}

	return s.tm.WithinTransaction(ctx, func(txCtx context.Context) error {
		switch {
		case normalizedCompanyID != "" && normalizedPointID != nil:
			if err := s.bitrixRepo.DeleteCompanyServicePointMappingByCompanyID(txCtx, normalizedCompanyID); err != nil {
				return err
			}
			if err := s.bitrixRepo.DeleteCompanyServicePointMappingByPointID(txCtx, *normalizedPointID); err != nil {
				return err
			}
			return s.bitrixRepo.UpsertCompanyServicePointMapping(txCtx, &bitrix.CompanyServicePointMapping{
				CompanyID:            normalizedCompanyID,
				BitrixServicePointID: *normalizedPointID,
			})
		case normalizedCompanyID != "":
			return s.bitrixRepo.DeleteCompanyServicePointMappingByCompanyID(txCtx, normalizedCompanyID)
		default:
			return s.bitrixRepo.DeleteCompanyServicePointMappingByPointID(txCtx, *normalizedPointID)
		}
	})
}

func (s *serviceImpl) SyncBitrixContract(ctx context.Context, companyID string) error {
	if s.bitrixRepo == nil {
		return fmt.Errorf("репозиторий Bitrix24 не настроен")
	}
	if s.contractSvc == nil {
		return fmt.Errorf("сервис контрактов не настроен")
	}

	normalizedCompanyID := strings.TrimSpace(companyID)
	if normalizedCompanyID == "" {
		return fmt.Errorf("не передан идентификатор компании")
	}

	if _, err := s.companyRepo.GetByID(ctx, normalizedCompanyID); err != nil {
		return err
	}

	mapping, err := s.bitrixRepo.GetCompanyServicePointMappingByCompanyID(ctx, normalizedCompanyID)
	if err != nil {
		return err
	}
	if mapping == nil || mapping.BitrixServicePointID <= 0 {
		return fmt.Errorf("для компании не настроено сопоставление с точкой Bitrix24")
	}

	point, err := s.bitrixRepo.GetServicePointByID(ctx, mapping.BitrixServicePointID)
	if err != nil {
		return err
	}
	if point == nil {
		return fmt.Errorf("сопоставленная точка Bitrix24 не найдена")
	}

	snapshot := contractsvc.BuildDailySnapshotFromBitrixServicePoint(normalizedCompanyID, *point)
	snapshot.SourceHash = buildBitrixPointContractSourceHash(*point)
	return s.contractSvc.SyncDailySnapshots(ctx, []contractdom.DailyCompanyContractSnapshot{snapshot})
}

func buildBitrixPointContractSourceHash(point bitrix.ServicePoint) string {
	return fmt.Sprintf("bitrix-service-point:%d:%d", point.B24ElementID, point.UpdatedAt.UTC().UnixNano())
}
