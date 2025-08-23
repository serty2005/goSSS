package handlers

import (
	"etalon-server/internal/api"
	"etalon-server/internal/models"
	"etalon-server/internal/repositories"
	"etalon-server/internal/utils"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// SearchHandler обрабатывает поисковые запросы.
type SearchHandler struct {
	logger          *zap.Logger
	companyRepo     repositories.CompanyRepo
	serverRepo      repositories.ServerRepo
	workstationRepo repositories.WorkstationRepo
	frRepo          repositories.FiscalRegisterRepo
}

// NewSearchHandler создает новый экземпляр обработчика.
func NewSearchHandler(
	logger *zap.Logger,
	companyRepo repositories.CompanyRepo,
	serverRepo repositories.ServerRepo,
	workstationRepo repositories.WorkstationRepo,
	frRepo repositories.FiscalRegisterRepo,
) *SearchHandler {
	return &SearchHandler{logger, companyRepo, serverRepo, workstationRepo, frRepo}
}

// RegisterRoutes регистрирует роут для поиска.
func (h *SearchHandler) RegisterRoutes(r chi.Router) {
	r.Get("/search", h.Search)
}

// Search выполняет финальный, UI-ориентированный, owner-centric поиск.
func (h *SearchHandler) Search(w http.ResponseWriter, r *http.Request) {
	term := r.URL.Query().Get("term")
	if term == "" {
		RespondWithError(w, http.StatusBadRequest, "Поисковый запрос не может быть пустым")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	ctx := r.Context()
	log := h.logger.With(zap.String("search_term", term))

	// --- Шаг 1: Найти все сущности, напрямую совпадающие с поисковым запросом ---
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

	// --- Шаг 2: Собрать уникальный список ID всех затронутых владельцев ---
	ownerUUIDs := make(map[string]bool)
	for _, company := range initialCompanies {
		ownerUUIDs[*company.ServiceDeskUUID] = true
	}
	for _, server := range initialServers {
		if server.OwnerServiceDeskUUID != nil {
			ownerUUIDs[*server.OwnerServiceDeskUUID] = true
		}
	}
	for _, ws := range initialWorkstations {
		if ws.OwnerServiceDeskUUID != nil {
			ownerUUIDs[*ws.OwnerServiceDeskUUID] = true
		}
	}
	for _, fr := range initialFRs {
		if fr.OwnerServiceDeskUUID != nil {
			ownerUUIDs[*fr.OwnerServiceDeskUUID] = true
		}
	}

	if len(ownerUUIDs) == 0 {
		log.Info("Не найдено совпадений или связанных владельцев.")
		RespondWithJSON(w, http.StatusOK, api.FinalSearchResponseDTO{SearchResults: []api.SearchGroupDTO{}})
		return
	}

	uuids := make([]string, 0, len(ownerUUIDs))
	for uuid := range ownerUUIDs {
		uuids = append(uuids, uuid)
	}

	// --- Шаг 3: Загрузить ВСЕ данные для найденных владельцев ---
	var allOwnerCompanies []models.Company
	var allOwnerServers []models.Server
	var allOwnerWorkstations []models.Workstation
	var allOwnerFRs []models.FiscalRegister

	wg.Add(4)
	go func() { defer wg.Done(); allOwnerCompanies, _ = h.companyRepo.GetByUUIDs(ctx, uuids) }()
	go func() { defer wg.Done(); allOwnerServers, _ = h.serverRepo.FindByOwnerUUIDs(ctx, uuids) }()
	go func() { defer wg.Done(); allOwnerWorkstations, _ = h.workstationRepo.FindByOwnerUUIDs(ctx, uuids) }()
	go func() { defer wg.Done(); allOwnerFRs, _ = h.frRepo.FindByOwnerUUIDs(ctx, uuids) }()
	wg.Wait()

	// --- Шаг 4: Сформировать финальную сгруппированную структуру ---

	// Преобразуем оборудование в мапы для быстрого доступа
	serversByOwner := groupServersByOwner(allOwnerServers)
	workstationsByOwner := groupWorkstationsByOwner(allOwnerWorkstations)
	frsByOwner := groupFRsByOwner(allOwnerFRs)

	finalResponse := api.FinalSearchResponseDTO{}

	// Создаем группу для каждой найденной компании-владельца
	for _, owner := range allOwnerCompanies {
		ownerID := *owner.ServiceDeskUUID

		group := api.SearchGroupDTO{
			Owner: api.OwnerFullDTO{
				UUID:           ownerID,
				Name:           utils.SafeStringDereference(owner.Title),
				Address:        owner.Address,
				ActiveContract: owner.ActiveContract,
				AdditionalInfo: owner.AdditionalName,
			},
			FoundEntities: []api.FoundEntityDTO{},
		}

		// Добавляем все оборудование, принадлежащее этому владельцу
		if servers, ok := serversByOwner[ownerID]; ok {
			group.FoundEntities = append(group.FoundEntities, servers...)
		}
		if workstations, ok := workstationsByOwner[ownerID]; ok {
			group.FoundEntities = append(group.FoundEntities, workstations...)
		}
		if frs, ok := frsByOwner[ownerID]; ok {
			group.FoundEntities = append(group.FoundEntities, frs...)
		}

		finalResponse.SearchResults = append(finalResponse.SearchResults, group)
	}

	RespondWithJSON(w, http.StatusOK, finalResponse)
}

// --- Вспомогательные функции-группировщики ---

func groupServersByOwner(servers []models.Server) map[string][]api.FoundEntityDTO {
	result := make(map[string][]api.FoundEntityDTO)
	for _, s := range servers {
		if s.OwnerServiceDeskUUID != nil {
			ownerID := *s.OwnerServiceDeskUUID

			// Формируем ссылку на партнерский кабинет
			var partnersLink *string
			clientIdStr := utils.SafeStringDereference(s.CabinetLink)
			// Проверяем, что clientIdStr не пустой и не содержит 'N/A'
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
					UUID:         *s.ServiceDeskUUID,
					DeviceName:   s.DeviceName,
					IP:           s.IP,
					Status:       s.Status,
					Anydesk:      s.Anydesk,
					Teamviewer:   s.Teamviewer,
					RDP:          s.RDP,
					Litemanager:  s.Litemanager,
					UniqueID:     s.UniqueID,
					PartnersLink: partnersLink,
				},
			})
		}
	}
	return result
}

func groupWorkstationsByOwner(workstations []models.Workstation) map[string][]api.FoundEntityDTO {
	result := make(map[string][]api.FoundEntityDTO)
	for _, ws := range workstations {
		if ws.OwnerServiceDeskUUID != nil {
			ownerID := *ws.OwnerServiceDeskUUID
			result[ownerID] = append(result[ownerID], api.FoundEntityDTO{
				EntityType: "Workstation",
				Data: api.WorkstationRichDTO{
					UUID: *ws.ServiceDeskUUID, DeviceName: ws.DeviceName, Status: ws.Status,
					Anydesk: ws.Anydesk, Teamviewer: ws.Teamviewer, Litemanager: ws.Litemanager,
				},
			})
		}
	}
	return result
}

func groupFRsByOwner(frs []models.FiscalRegister) map[string][]api.FoundEntityDTO {
	result := make(map[string][]api.FoundEntityDTO)
	for _, fr := range frs {
		if fr.OwnerServiceDeskUUID != nil {
			ownerID := *fr.OwnerServiceDeskUUID
			result[ownerID] = append(result[ownerID], api.FoundEntityDTO{
				EntityType: "FiscalRegister",
				Data: api.FiscalRegisterRichDTO{
					UUID: *fr.ServiceDeskUUID, RNKKT: fr.RNKKT, ModelKKT: fr.ModelKKT,
					FNExpireDate: fr.FNExpireDate, FNRegistrationDate: fr.KKTRegDate,
					DriverVersion: fr.DriverVersion, FirmwareVersion: fr.FRDownloader,
				},
			})
		}
	}
	return result
}
