// internal/handlers/search_handler.go
package handlers

import (
	"context"
	"etalon-server/internal/api"
	"etalon-server/internal/logger"
	"etalon-server/internal/models"
	"etalon-server/internal/repositories"
	"etalon-server/internal/utils"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
)

// SearchHandler обрабатывает поисковые запросы.
type SearchHandler struct {
	logger          logger.LoggerInterface
	companyRepo     repositories.CompanyRepo
	serverRepo      repositories.ServerRepo
	workstationRepo repositories.WorkstationRepo
	frRepo          repositories.FiscalRegisterRepo
	linkRepo        repositories.LinkRepo
}

// NewSearchHandler создает новый экземпляр обработчика.
func NewSearchHandler(
	logger logger.LoggerInterface,
	companyRepo repositories.CompanyRepo,
	serverRepo repositories.ServerRepo,
	workstationRepo repositories.WorkstationRepo,
	frRepo repositories.FiscalRegisterRepo,
	linkRepo repositories.LinkRepo,
) *SearchHandler {
	return &SearchHandler{logger, companyRepo, serverRepo, workstationRepo, frRepo, linkRepo}
}

// RegisterRoutes регистрирует роут для поиска.
func (h *SearchHandler) RegisterRoutes(r chi.Router) {
	r.Get("/search", h.Search)
}

// Search выполняет финальный, UI-ориентированный, owner-centric поиск.
func (h *SearchHandler) Search(w http.ResponseWriter, r *http.Request) {
	h.logger.Info("Получен поисковый запрос", "method", r.Method, "path", r.URL.Path, "remote_addr", r.RemoteAddr)

	term := r.URL.Query().Get("term")
	if term == "" {
		h.logger.Warn("Попытка поиска с пустым запросом", "remote_addr", r.RemoteAddr)
		RespondWithError(w, http.StatusBadRequest, "Поисковый запрос не может быть пустым")
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	h.logger.Debug("Параметры поиска", "search_term", term, "limit", limit)

	ctx := r.Context()
	log := h.logger.With("search_term", term, "limit", limit)

	log.Info("Начало выполнения поискового запроса")

	var wg sync.WaitGroup
	var initialCompanies []models.Company
	var initialServers []models.Server
	var initialWorkstations []models.Workstation
	var initialFRs []models.FiscalRegister
	wg.Add(4)
	go func() { defer wg.Done(); initialCompanies, _ = h.companyRepo.Search(ctx, term, true, limit, 0) }()
	go func() { defer wg.Done(); initialServers, _ = h.serverRepo.Search(ctx, term, limit, 0) }()
	go func() { defer wg.Done(); initialWorkstations, _ = h.workstationRepo.Search(ctx, term, limit, 0) }()
	go func() { defer wg.Done(); initialFRs, _ = h.frRepo.Search(ctx, term, limit, 0) }()
	wg.Wait()

	ownerIDs := make(map[string]bool)
	for _, company := range initialCompanies {
		ownerIDs[company.ID] = true
	}
	for _, server := range initialServers {
		if server.OwnerID != nil {
			ownerIDs[*server.OwnerID] = true
		}
	}
	for _, ws := range initialWorkstations {
		if ws.OwnerID != nil {
			ownerIDs[*ws.OwnerID] = true
		}
	}
	for _, fr := range initialFRs {
		if fr.OwnerID != nil {
			ownerIDs[*fr.OwnerID] = true
		}
	}

	if len(ownerIDs) == 0 {
		log.Info("Поисковый запрос выполнен, результатов не найдено", "search_term", term)
		RespondWithJSON(w, http.StatusOK, api.FinalSearchResponseDTO{SearchResults: []api.SearchGroupDTO{}})
		return
	}

	idsToEnrich := make([]string, 0, len(ownerIDs))
	for id := range ownerIDs {
		idsToEnrich = append(idsToEnrich, id)
	}
	for _, id := range idsToEnrich {
		parents, _ := h.companyRepo.GetAllParentIDs(ctx, id)
		for _, parentID := range parents {
			ownerIDs[parentID] = true
		}
	}

	allOwnerIDs := make([]string, 0, len(ownerIDs))
	for id := range ownerIDs {
		allOwnerIDs = append(allOwnerIDs, id)
	}

	var allOwnerCompanies []models.Company
	var allOwnerServers []models.Server
	var allOwnerWorkstations []models.Workstation
	var allOwnerFRs []models.FiscalRegister
	wg.Add(4)
	go func() { defer wg.Done(); allOwnerCompanies, _ = h.companyRepo.GetByIDs(ctx, allOwnerIDs) }()
	go func() { defer wg.Done(); allOwnerServers, _ = h.serverRepo.FindByOwnerIDs(ctx, allOwnerIDs) }()
	go func() { defer wg.Done(); allOwnerWorkstations, _ = h.workstationRepo.FindByOwnerIDs(ctx, allOwnerIDs) }()
	go func() { defer wg.Done(); allOwnerFRs, _ = h.frRepo.FindByOwnerIDs(ctx, allOwnerIDs) }()
	wg.Wait()

	serversByOwner := h.groupServersByOwner(ctx, allOwnerServers)
	workstationsByOwner := h.groupWorkstationsByOwner(ctx, allOwnerWorkstations)
	frsByOwner := h.groupFRsByOwner(ctx, allOwnerFRs)

	finalResponse := api.FinalSearchResponseDTO{}
	initialOwnerIDs := make(map[string]struct{})
	for _, id := range idsToEnrich {
		initialOwnerIDs[id] = struct{}{}
	}

	for _, owner := range allOwnerCompanies {
		ownerID := owner.ID
		if _, ok := initialOwnerIDs[ownerID]; !ok {
			continue
		}

		link, _ := h.linkRepo.GetByInternalID(ctx, nil, "naumen", owner.ID)
		var externalUUID *string
		if link != nil {
			externalUUID = &link.ServiceDeskUUID
		}

		group := api.SearchGroupDTO{
			Owner: api.OwnerFullDTO{
				UUID:            ownerID,
				ServiceDeskUUID: externalUUID, // ИСПРАВЛЕНИЕ: Теперь это поле существует
				Name:            utils.SafeStringDereference(owner.Title),
				Address:         owner.Address,
				ActiveContract:  owner.ActiveContract,
				AdditionalInfo:  owner.AdditionalName,
			},
			FoundEntities: []api.FoundEntityDTO{},
		}

		currentAndParentIDs := map[string]struct{}{ownerID: {}}
		parents, _ := h.companyRepo.GetAllParentIDs(ctx, ownerID)
		for _, pID := range parents {
			currentAndParentIDs[pID] = struct{}{}
		}

		for id := range currentAndParentIDs {
			if servers, ok := serversByOwner[id]; ok {
				group.FoundEntities = append(group.FoundEntities, servers...)
			}
			if id == ownerID {
				if workstations, ok := workstationsByOwner[id]; ok {
					group.FoundEntities = append(group.FoundEntities, workstations...)
				}
				if frs, ok := frsByOwner[id]; ok {
					group.FoundEntities = append(group.FoundEntities, frs...)
				}
			}
		}
		finalResponse.SearchResults = append(finalResponse.SearchResults, group)
	}
	log.Info("Поиск завершен успешно", "groups_count", len(finalResponse.SearchResults), "search_term", term)
	RespondWithJSON(w, http.StatusOK, finalResponse)
}

// --- Вспомогательные функции-группировщики ---

func (h *SearchHandler) groupServersByOwner(ctx context.Context, servers []models.Server) map[string][]api.FoundEntityDTO {
	result := make(map[string][]api.FoundEntityDTO)
	for _, s := range servers {
		if s.OwnerID != nil {
			ownerID := *s.OwnerID

			// Обогащаем внешним ID
			link, _ := h.linkRepo.GetByInternalID(ctx, nil, "naumen", s.ID)
			var externalUUID *string
			if link != nil {
				externalUUID = &link.ServiceDeskUUID
			}

			// Формируем ссылку на партнерский кабинет
			var partnersLink *string
			clientIdStr := utils.SafeStringDereference(s.CabinetLink)
			if clientIdStr != "" && clientIdStr != "N/A" {
				var link string
				ipStr := utils.SafeStringDereference(s.IP)
				if strings.Contains(strings.ToLower(ipStr), "syrve") {
					link = fmt.Sprintf("https://pp.syrve.com/en/cabinet/client-area/index.html?clientId=%s", clientIdStr)
				} else {
					link = fmt.Sprintf("https://pp.iiko.ru/ru/cabinet/client-area/index.html?clientId=%s", clientIdStr)
				}
				partnersLink = &link
			}

			result[ownerID] = append(result[ownerID], api.FoundEntityDTO{
				EntityType: "Server",
				Data: api.ServerRichDTO{
					UUID:            s.ID,
					ServiceDeskUUID: externalUUID,
					DeviceName:      s.DeviceName,
					IP:              s.IP,
					Status:          s.Status,
					Anydesk:         s.Anydesk,
					Teamviewer:      s.Teamviewer,
					RDP:             s.RDP,
					Litemanager:     s.Litemanager,
					UniqueID:        s.UniqueID,
					PartnersLink:    partnersLink,
				},
			})
		}
	}
	return result
}

func (h *SearchHandler) groupWorkstationsByOwner(ctx context.Context, workstations []models.Workstation) map[string][]api.FoundEntityDTO {
	result := make(map[string][]api.FoundEntityDTO)
	for _, ws := range workstations {
		if ws.OwnerID != nil {
			ownerID := *ws.OwnerID

			// Обогащаем внешним ID
			link, _ := h.linkRepo.GetByInternalID(ctx, nil, "naumen", ws.ID)
			var externalUUID *string
			if link != nil {
				externalUUID = &link.ServiceDeskUUID
			}

			result[ownerID] = append(result[ownerID], api.FoundEntityDTO{
				EntityType: "Workstation",
				Data: api.WorkstationRichDTO{
					UUID:            ws.ID,
					ServiceDeskUUID: externalUUID,
					DeviceName:      ws.DeviceName,
					Status:          ws.Status,
					Anydesk:         ws.Anydesk,
					Teamviewer:      ws.Teamviewer,
					Litemanager:     ws.Litemanager,
				},
			})
		}
	}
	return result
}

func (h *SearchHandler) groupFRsByOwner(ctx context.Context, frs []models.FiscalRegister) map[string][]api.FoundEntityDTO {
	result := make(map[string][]api.FoundEntityDTO)
	for _, fr := range frs {
		if fr.OwnerID != nil {
			ownerID := *fr.OwnerID

			// Обогащаем внешним ID
			link, _ := h.linkRepo.GetByInternalID(ctx, nil, "naumen", fr.ID)
			var externalUUID *string
			if link != nil {
				externalUUID = &link.ServiceDeskUUID
			}

			result[ownerID] = append(result[ownerID], api.FoundEntityDTO{
				EntityType: "FiscalRegister",
				Data: api.FiscalRegisterRichDTO{
					UUID:               fr.ID,
					ServiceDeskUUID:    externalUUID,
					Status:             fr.Status,
					RNKKT:              fr.RNKKT,
					ModelKKT:           fr.ModelKKT,
					FNRegistrationDate: fr.KKTRegDate,
					FNExpireDate:       fr.FNExpireDate,
					DriverVersion:      fr.DriverVersion,
					FRFirmware:         fr.FRFirmware,
					FRDownloader:       fr.FRDownloader,
					OrganizationName:   fr.LegalName,
					INN:                fr.INN,
					SerialNumber:       fr.FRSerialNumber,
					IsMarkingActive:    true,
					IsExciseActive:     false,
				},
			})
		}
	}
	return result
}
