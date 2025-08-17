package services

import (
	"context"
	"etalon-server/internal/repositories"
	"etalon-server/internal/utils"
	"sync"

	"go.uber.org/zap"
)

// SyncService отвечает за логику синхронизации данных.
type SyncService interface {
	SyncAllData(ctx context.Context, fullSync bool)
}

type syncServiceImpl struct {
	sdClient        ServiceDeskClient
	companyRepo     repositories.CompanyRepo
	serverRepo      repositories.ServerRepo
	workstationRepo repositories.WorkstationRepo
	frRepo          repositories.FiscalRegisterRepo
	logger          *zap.Logger
	workerCount     int
	isSyncing       bool
	mu              sync.Mutex
}

// NewSyncService создает новый экземпляр сервиса синхронизации.
func NewSyncService(
	sdClient ServiceDeskClient,
	companyRepo repositories.CompanyRepo,
	serverRepo repositories.ServerRepo,
	workstationRepo repositories.WorkstationRepo,
	frRepo repositories.FiscalRegisterRepo,
	logger *zap.Logger,
	workerCount int,
) SyncService {
	return &syncServiceImpl{
		sdClient:        sdClient,
		companyRepo:     companyRepo,
		serverRepo:      serverRepo,
		workstationRepo: workstationRepo,
		frRepo:          frRepo,
		logger:          logger,
		workerCount:     workerCount,
	}
}

// SyncAllData запускает полную или инкрементальную синхронизацию.
func (s *syncServiceImpl) SyncAllData(ctx context.Context, fullSync bool) {
	s.mu.Lock()
	if s.isSyncing {
		s.logger.Warn("Sync is already in progress.")
		s.mu.Unlock()
		return
	}
	s.isSyncing = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.isSyncing = false
		s.mu.Unlock()
	}()

	s.logger.Info("Starting data synchronization", zap.Bool("fullSync", fullSync))

	s.syncCompanies(ctx)
	s.syncServers(ctx)
	s.syncWorkstations(ctx)
	s.syncFiscalRegisters(ctx)

	s.logger.Info("Data synchronization finished.")
}

// syncCompanies синхронизирует компании.
func (s *syncServiceImpl) syncCompanies(ctx context.Context) {
	s.logger.Info("Syncing companies...")
	remoteList, err := s.sdClient.FetchEntityList(ctx, "ou$company", false)
	if err != nil {
		s.logger.Error("Failed to fetch remote company list", zap.Error(err))
		return
	}

	localMap, err := s.companyRepo.GetAllUUIDsAndDates(ctx)
	if err != nil {
		s.logger.Error("Failed to fetch local companies", zap.Error(err))
		return
	}

	for _, remoteItem := range remoteList {
		select {
		case <-ctx.Done():
			s.logger.Info("Context cancelled, stopping company sync.")
			return
		default:
		}

		remoteUUID, _ := remoteItem["UUID"].(string)
		if remoteUUID == "" {
			continue
		}

		remoteLMDStr, _ := remoteItem["lastModifiedDate"].(string)
		remoteLMD := utils.ParseServiceDeskTime(remoteLMDStr)

		localCompany, exists := localMap[remoteUUID]
		if !exists {
			s.fetchAndCreateCompany(ctx, remoteUUID)
		} else if remoteLMD != nil && (localCompany.LastModifiedDate == nil || remoteLMD.After(*localCompany.LastModifiedDate)) {
			s.fetchAndUpdateCompany(ctx, remoteUUID)
		}
	}
}

func (s *syncServiceImpl) fetchAndCreateCompany(ctx context.Context, uuid string) {
	details, err := s.sdClient.FetchEntityDetails(ctx, uuid, "ou$company")
	if err != nil {
		s.logger.Error("Failed to fetch details for new company", zap.String("uuid", uuid), zap.Error(err))
		return
	}
	company, err := DataToCompany(ctx, details, s.sdClient, s.logger)
	if err != nil {
		s.logger.Error("Failed to map data for new company", zap.String("uuid", uuid), zap.Error(err))
		return
	}
	// Теперь сервис не управляет транзакциями, он просто вызывает метод репозитория.
	// Репозиторий сам решит, использовать транзакцию или нет.
	if err := s.companyRepo.Create(ctx, nil, company); err != nil {
		s.logger.Error("Failed to create company in DB", zap.String("uuid", uuid), zap.Error(err))
	} else {
		s.logger.Info("Successfully created company", zap.String("uuid", uuid))
	}
}

func (s *syncServiceImpl) fetchAndUpdateCompany(ctx context.Context, uuid string) {
	details, err := s.sdClient.FetchEntityDetails(ctx, uuid, "ou$company")
	if err != nil {
		s.logger.Error("Failed to fetch details for updating company", zap.String("uuid", uuid), zap.Error(err))
		return
	}
	company, err := DataToCompany(ctx, details, s.sdClient, s.logger)
	if err != nil {
		s.logger.Error("Failed to map data for updating company", zap.String("uuid", uuid), zap.Error(err))
		return
	}
	updateData := map[string]interface{}{
		"Title":                 company.Title,
		"Address":               company.Address,
		"ActiveContract":        company.ActiveContract,
		"LastModifiedDate":      company.LastModifiedDate,
		"AdditionalName":        company.AdditionalName,
		"ParentServiceDeskUUID": company.ParentServiceDeskUUID,
	}
	if _, err := s.companyRepo.Update(ctx, nil, uuid, updateData); err != nil {
		s.logger.Error("Failed to update company in DB", zap.String("uuid", uuid), zap.Error(err))
	} else {
		s.logger.Info("Successfully updated company", zap.String("uuid", uuid))
	}
}

// syncServers синхронизирует серверы.
func (s *syncServiceImpl) syncServers(ctx context.Context) {
	s.logger.Info("Syncing servers...")
	remoteList, err := s.sdClient.FetchEntityList(ctx, "objectBase$Server", false)
	if err != nil {
		s.logger.Error("Failed to fetch remote server list", zap.Error(err))
		return
	}

	localMap, err := s.serverRepo.GetAllUUIDsAndDates(ctx)
	if err != nil {
		s.logger.Error("Failed to fetch local servers", zap.Error(err))
		return
	}

	for _, remoteItem := range remoteList {
		select {
		case <-ctx.Done():
			s.logger.Info("Context cancelled, stopping server sync.")
			return
		default:
		}
		remoteUUID, _ := remoteItem["UUID"].(string)
		if remoteUUID == "" {
			continue
		}
		remoteLMDStr, _ := remoteItem["lastModifiedDate"].(string)
		remoteLMD := utils.ParseServiceDeskTime(remoteLMDStr)
		localServer, exists := localMap[remoteUUID]
		if !exists {
			s.fetchAndCreateServer(ctx, remoteUUID)
		} else if remoteLMD != nil && (localServer.LastModifiedDate == nil || remoteLMD.After(*localServer.LastModifiedDate)) {
			s.fetchAndUpdateServer(ctx, remoteUUID)
		}
	}
}

func (s *syncServiceImpl) fetchAndCreateServer(ctx context.Context, uuid string) {
	details, err := s.sdClient.FetchEntityDetails(ctx, uuid, "objectBase$Server")
	if err != nil {
		s.logger.Error("Failed to fetch details for new server", zap.String("uuid", uuid), zap.Error(err))
		return
	}
	server, err := DataToServer(details)
	if err != nil {
		s.logger.Warn("Failed to map data for new server", zap.String("uuid", uuid), zap.Error(err))
		return
	}
	if err := s.serverRepo.Create(ctx, nil, server); err != nil {
		s.logger.Error("Failed to create server in DB", zap.String("uuid", uuid), zap.Error(err))
	} else {
		s.logger.Info("Successfully created server", zap.String("uuid", uuid))
	}
}

func (s *syncServiceImpl) fetchAndUpdateServer(ctx context.Context, uuid string) {
	details, err := s.sdClient.FetchEntityDetails(ctx, uuid, "objectBase$Server")
	if err != nil {
		s.logger.Error("Failed to fetch details for updating server", zap.String("uuid", uuid), zap.Error(err))
		return
	}
	server, err := DataToServer(details)
	if err != nil {
		s.logger.Warn("Failed to map data for updating server", zap.String("uuid", uuid), zap.Error(err))
		return
	}
	updateData := map[string]interface{}{
		"UniqueID":             server.UniqueID,
		"Teamviewer":           server.Teamviewer,
		"RDP":                  server.RDP,
		"Anydesk":              server.Anydesk,
		"IP":                   server.IP,
		"CabinetLink":          server.CabinetLink,
		"DeviceName":           server.DeviceName,
		"LastModifiedDate":     server.LastModifiedDate,
		"Litemanager":          server.Litemanager,
		"IikoVersion":          server.IikoVersion,
		"Description":          server.Description,
		"OwnerServiceDeskUUID": server.OwnerServiceDeskUUID,
	}
	if _, err := s.serverRepo.Update(ctx, nil, uuid, updateData); err != nil {
		s.logger.Error("Failed to update server in DB", zap.String("uuid", uuid), zap.Error(err))
	} else {
		s.logger.Info("Successfully updated server", zap.String("uuid", uuid))
	}
}

// syncWorkstations синхронизирует рабочие станции.
func (s *syncServiceImpl) syncWorkstations(ctx context.Context) {
	s.logger.Info("Syncing workstations...")
	remoteList, err := s.sdClient.FetchEntityList(ctx, "objectBase$Workstation", false)
	if err != nil {
		s.logger.Error("Failed to fetch remote workstation list", zap.Error(err))
		return
	}
	localMap, err := s.workstationRepo.GetAllUUIDsAndDates(ctx)
	if err != nil {
		s.logger.Error("Failed to fetch local workstations", zap.Error(err))
		return
	}
	for _, remoteItem := range remoteList {
		remoteUUID, _ := remoteItem["UUID"].(string)
		if remoteUUID == "" {
			continue
		}
		remoteLMDStr, _ := remoteItem["lastModifiedDate"].(string)
		remoteLMD := utils.ParseServiceDeskTime(remoteLMDStr)
		localWs, exists := localMap[remoteUUID]
		if !exists {
			s.fetchAndCreateWorkstation(ctx, remoteUUID)
		} else if remoteLMD != nil && (localWs.LastModifiedDate == nil || remoteLMD.After(*localWs.LastModifiedDate)) {
			s.fetchAndAndUpdateWorkstation(ctx, remoteUUID)
		}
	}
}

func (s *syncServiceImpl) fetchAndCreateWorkstation(ctx context.Context, uuid string) {
	details, err := s.sdClient.FetchEntityDetails(ctx, uuid, "objectBase$Workstation")
	if err != nil {
		s.logger.Error("Failed to fetch details for new workstation", zap.String("uuid", uuid), zap.Error(err))
		return
	}
	ws, err := DataToWorkstation(details)
	if err != nil {
		s.logger.Warn("Failed to map data for new workstation", zap.String("uuid", uuid), zap.Error(err))
		return
	}
	if err := s.workstationRepo.Create(ctx, nil, ws); err != nil {
		s.logger.Error("Failed to create workstation in DB", zap.String("uuid", uuid), zap.Error(err))
	} else {
		s.logger.Info("Successfully created workstation", zap.String("uuid", uuid))
	}
}

func (s *syncServiceImpl) fetchAndAndUpdateWorkstation(ctx context.Context, uuid string) {
	details, err := s.sdClient.FetchEntityDetails(ctx, uuid, "objectBase$Workstation")
	if err != nil {
		s.logger.Error("Failed to fetch details for updating workstation", zap.String("uuid", uuid), zap.Error(err))
		return
	}
	ws, err := DataToWorkstation(details)
	if err != nil {
		s.logger.Warn("Failed to map data for updating workstation", zap.String("uuid", uuid), zap.Error(err))
		return
	}
	updateData := map[string]interface{}{
		"Teamviewer":           ws.Teamviewer,
		"Anydesk":              ws.Anydesk,
		"Litemanager":          ws.Litemanager,
		"DeviceName":           ws.DeviceName,
		"LastModifiedDate":     ws.LastModifiedDate,
		"Description":          ws.Description,
		"OwnerServiceDeskUUID": ws.OwnerServiceDeskUUID,
	}
	if _, err := s.workstationRepo.Update(ctx, nil, uuid, updateData); err != nil {
		s.logger.Error("Failed to update workstation in DB", zap.String("uuid", uuid), zap.Error(err))
	} else {
		s.logger.Info("Successfully updated workstation", zap.String("uuid", uuid))
	}
}

// syncFiscalRegisters синхронизирует фискальные регистраторы.
func (s *syncServiceImpl) syncFiscalRegisters(ctx context.Context) {
	s.logger.Info("Syncing fiscal registers...")
	remoteList, err := s.sdClient.FetchEntityList(ctx, "objectBase$FR", false)
	if err != nil {
		s.logger.Error("Failed to fetch remote FR list", zap.Error(err))
		return
	}
	localMap, err := s.frRepo.GetAllUUIDsAndDates(ctx)
	if err != nil {
		s.logger.Error("Failed to fetch local FRs", zap.Error(err))
		return
	}
	for _, remoteItem := range remoteList {
		remoteUUID, _ := remoteItem["UUID"].(string)
		if remoteUUID == "" {
			continue
		}
		remoteLMDStr, _ := remoteItem["lastModifiedDate"].(string)
		remoteLMD := utils.ParseServiceDeskTime(remoteLMDStr)
		localFr, exists := localMap[remoteUUID]
		if !exists {
			s.fetchAndCreateFiscalRegister(ctx, remoteUUID)
		} else if remoteLMD != nil && (localFr.LastModifiedDate == nil || remoteLMD.After(*localFr.LastModifiedDate)) {
			s.fetchAndAndUpdateFiscalRegister(ctx, remoteUUID)
		}
	}
}

func (s *syncServiceImpl) fetchAndCreateFiscalRegister(ctx context.Context, uuid string) {
	details, err := s.sdClient.FetchEntityDetails(ctx, uuid, "objectBase$FR")
	if err != nil {
		s.logger.Error("Failed to fetch details for new FR", zap.String("uuid", uuid), zap.Error(err))
		return
	}
	fr, err := DataToFiscalRegister(details)
	if err != nil {
		s.logger.Warn("Failed to map data for new FR", zap.String("uuid", uuid), zap.Error(err))
		return
	}
	if err := s.frRepo.Create(ctx, nil, fr); err != nil {
		s.logger.Error("Failed to create FR in DB", zap.String("uuid", uuid), zap.Error(err))
	} else {
		s.logger.Info("Successfully created FR", zap.String("uuid", uuid))
	}
}

func (s *syncServiceImpl) fetchAndAndUpdateFiscalRegister(ctx context.Context, uuid string) {
	details, err := s.sdClient.FetchEntityDetails(ctx, uuid, "objectBase$FR")
	if err != nil {
		s.logger.Error("Failed to fetch details for updating FR", zap.String("uuid", uuid), zap.Error(err))
		return
	}
	fr, err := DataToFiscalRegister(details)
	if err != nil {
		s.logger.Warn("Failed to map data for updating FR", zap.String("uuid", uuid), zap.Error(err))
		return
	}
	updateData := map[string]interface{}{
		"ModelKKT":             fr.ModelKKT,
		"FFD":                  fr.FFD,
		"FRDownloader":         fr.FRDownloader,
		"RNKKT":                fr.RNKKT,
		"LegalName":            fr.LegalName,
		"FRSerialNumber":       fr.FRSerialNumber,
		"FNNumber":             fr.FNNumber,
		"KKTRegDate":           fr.KKTRegDate,
		"FNExpireDate":         fr.FNExpireDate,
		"LastModifiedDate":     fr.LastModifiedDate,
		"OwnerServiceDeskUUID": fr.OwnerServiceDeskUUID,
	}
	if _, err := s.frRepo.Update(ctx, nil, uuid, updateData); err != nil {
		s.logger.Error("Failed to update FR in DB", zap.String("uuid", uuid), zap.Error(err))
	} else {
		s.logger.Info("Successfully updated FR", zap.String("uuid", uuid))
	}
}
