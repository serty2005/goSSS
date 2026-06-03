// internal/handlers/search_handler.go
package handlers

import (
	"context"
	"encoding/json"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/repositories"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/domain/user"
	"etalon-server/internal/domain/workstation"
	"etalon-server/internal/pkg/utils"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/middleware"
	"etalon-server/internal/transport/http/response"
	"etalon-server/internal/transport/http/validators"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
)

// SearchHandler обрабатывает поисковые запросы.
type SearchHandler struct {
	companyRepo     company.Repository
	serverRepo      server.Repository
	workstationRepo workstation.Repository
	frRepo          fiscal.Repository
	linkRepo        repositories.LinkRepo
	ticketRepo      tickets.TicketRepository
}

// NewSearchHandler создает новый экземпляр обработчика.
func NewSearchHandler(
	companyRepo company.Repository,
	serverRepo server.Repository,
	workstationRepo workstation.Repository,
	frRepo fiscal.Repository,
	linkRepo repositories.LinkRepo,
	ticketRepo tickets.TicketRepository,
) *SearchHandler {
	return &SearchHandler{companyRepo, serverRepo, workstationRepo, frRepo, linkRepo, ticketRepo}
}

// RegisterRoutes регистрирует роут для поиска.
func (h *SearchHandler) RegisterRoutes(r chi.Router) {
	r.Get("/search", h.Search)
}

// Search выполняет owner-centric поиск.
func (h *SearchHandler) Search(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	log.Info("Получен поисковый запрос", "method", r.Method, "path", r.URL.Path, "remote_addr", r.RemoteAddr)

	term := r.URL.Query().Get("term")
	if term == "" {
		log.Warn("Попытка поиска с пустым запросом", "remote_addr", r.RemoteAddr)
		response.RespondWithError(w, http.StatusBadRequest, "Поисковый запрос не может быть пустым")
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	showInactive := parseBoolParam(r.URL.Query().Get("show_inactive"))

	log.Debug("Параметры поиска", "search_term", term, "limit", limit)

	ctx := r.Context()
	log = log.With("search_term", term, "limit", limit)

	log.Debug("Начало выполнения поискового запроса")

	var wg sync.WaitGroup
	var initialCompanies []company.Company
	var initialServers []server.Server
	var initialWorkstations []workstation.Workstation
	var initialFRs []fiscal.FiscalRegister
	var matchedTickets []tickets.Ticket
	var ticketSearchErr error
	ticketLimit := min(limit, 50)
	wg.Add(5)
	go func() { defer wg.Done(); initialCompanies, _ = h.companyRepo.Search(ctx, term, showInactive, limit, 0) }()
	go func() { defer wg.Done(); initialServers, _ = h.serverRepo.Search(ctx, term, limit, 0, nil, nil, nil) }()
	go func() { defer wg.Done(); initialWorkstations, _ = h.workstationRepo.Search(ctx, term, limit, 0) }()
	go func() { defer wg.Done(); initialFRs, _ = h.frRepo.Search(ctx, term, limit, 0) }()
	go func() {
		defer wg.Done()
		if h.ticketRepo == nil {
			return
		}
		matchedTickets, ticketSearchErr = h.ticketRepo.Find(ctx, tickets.TicketFilter{
			SearchQuery: term,
			ArchiveMode: "all",
			Limit:       ticketLimit,
			SortBy:      "updated_at desc",
		})
	}()
	wg.Wait()
	if ticketSearchErr != nil {
		log.Warn("Не удалось выполнить поиск по тикетам", "error", ticketSearchErr)
	}

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
	matchedTicketIDs := make([]string, 0, len(matchedTickets))
	matchedTicketsByCompany := make(map[string][]tickets.Ticket)
	var matchedTicketsWithoutCompany []tickets.Ticket
	for _, ticket := range matchedTickets {
		matchedTicketIDs = append(matchedTicketIDs, ticket.ID)
		companyID := strings.TrimSpace(ticket.CompanyID)
		if companyID == "" {
			matchedTicketsWithoutCompany = append(matchedTicketsWithoutCompany, ticket)
			continue
		}
		ownerIDs[companyID] = true
		matchedTicketsByCompany[companyID] = append(matchedTicketsByCompany[companyID], ticket)
	}

	if len(ownerIDs) == 0 {
		log.Debug("Поисковый запрос выполнен, результатов не найдено", "search_term", term)
		finalResponse := api.FinalSearchResponseDTO{SearchResults: []api.SearchGroupDTO{}}
		if len(matchedTicketsWithoutCompany) > 0 {
			finalResponse.TicketResultsWithoutCompany = h.mapTicketsToSearchDTO(ctx, matchedTicketsWithoutCompany)
		}
		response.RespondWithJSON(w, http.StatusOK, finalResponse)
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

	var allOwnerCompanies []company.Company
	var allOwnerServers []server.Server
	var allOwnerWorkstations []workstation.Workstation
	var allOwnerFRs []fiscal.FiscalRegister
	wg.Add(4)
	go func() { defer wg.Done(); allOwnerCompanies, _ = h.companyRepo.GetByIDs(ctx, allOwnerIDs) }()
	go func() { defer wg.Done(); allOwnerServers, _ = h.serverRepo.FindByOwnerIDs(ctx, allOwnerIDs) }()
	go func() { defer wg.Done(); allOwnerWorkstations, _ = h.workstationRepo.FindByOwnerIDs(ctx, allOwnerIDs) }()
	go func() { defer wg.Done(); allOwnerFRs, _ = h.frRepo.FindByOwnerIDs(ctx, allOwnerIDs) }()
	wg.Wait()

	sort.SliceStable(allOwnerCompanies, func(i, j int) bool {
		leftIsParent := allOwnerCompanies[i].ParentID == nil || strings.TrimSpace(*allOwnerCompanies[i].ParentID) == ""
		rightIsParent := allOwnerCompanies[j].ParentID == nil || strings.TrimSpace(*allOwnerCompanies[j].ParentID) == ""
		if leftIsParent != rightIsParent {
			return leftIsParent
		}
		leftTitle := strings.ToLower(utils.SafeStringDereference(allOwnerCompanies[i].Title))
		rightTitle := strings.ToLower(utils.SafeStringDereference(allOwnerCompanies[j].Title))
		if leftTitle != rightTitle {
			return leftTitle < rightTitle
		}
		return allOwnerCompanies[i].ID < allOwnerCompanies[j].ID
	})

	serversByOwner := h.groupServersByOwner(ctx, allOwnerServers)
	workstationsByOwner := h.groupWorkstationsByOwner(ctx, allOwnerWorkstations)
	frsByOwner := h.groupFRsByOwner(ctx, allOwnerFRs)

	finalResponse := api.FinalSearchResponseDTO{}
	initialOwnerIDs := make(map[string]struct{})
	for _, id := range idsToEnrich {
		initialOwnerIDs[id] = struct{}{}
	}

	// Кэшируем информацию о родителях, чтобы не делать лишних запросов к БД
	parentCompanyCache := make(map[string]*company.Company)

	for _, owner := range allOwnerCompanies {
		ownerID := owner.ID
		if _, ok := initialOwnerIDs[ownerID]; !ok {
			continue
		}
		if !showInactive {
			if owner.ActiveContract == nil || !*owner.ActiveContract {
				continue
			}
		}

		link, _ := h.linkRepo.GetByInternalID(ctx, nil, "naumen", owner.ID)
		var externalUUID *string
		if link != nil {
			externalUUID = &link.ServiceDeskUUID
		}

		var parentInfo *api.ParentInfo
		if owner.ParentID != nil && *owner.ParentID != "" {
			parent, exists := parentCompanyCache[*owner.ParentID]
			if !exists {
				parent, _ = h.companyRepo.GetByID(ctx, *owner.ParentID)
				if parent != nil {
					parentCompanyCache[*owner.ParentID] = parent
				}
			}
			if parent != nil {
				parentInfo = &api.ParentInfo{
					UUID: parent.ID,
					Name: *parent.Title,
				}
			}
		}

		group := api.SearchGroupDTO{
			Owner: api.OwnerFullDTO{
				UUID:            ownerID,
				ServiceDeskUUID: externalUUID,
				Name:            utils.SafeStringDereference(owner.Title),
				Address:         owner.Address,
				ActiveContract:  owner.ActiveContract,
				AdditionalInfo:  owner.AdditionalName,
				ParentInfo:      parentInfo,
			},
			FoundEntities: []api.FoundEntityDTO{},
		}
		if ticketsForOwner := matchedTicketsByCompany[ownerID]; len(ticketsForOwner) > 0 {
			group.MatchedTickets = h.mapTicketsToSearchDTO(ctx, ticketsForOwner)
		}
		group.ActiveTickets = h.loadActiveTicketSearchDTOs(ctx, ownerID, matchedTicketIDs, log)

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
	response.RespondWithJSON(w, http.StatusOK, finalResponse)
}

func parseBoolParam(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

// --- Вспомогательные функции-группировщики ---

func (h *SearchHandler) groupServersByOwner(ctx context.Context, servers []server.Server) map[string][]api.FoundEntityDTO {
	result := make(map[string][]api.FoundEntityDTO)
	for _, s := range servers {
		if s.OwnerID != nil {
			ownerID := *s.OwnerID

			link, _ := h.linkRepo.GetByInternalID(ctx, nil, "naumen", s.ID)
			var externalUUID *string
			if link != nil {
				externalUUID = &link.ServiceDeskUUID
			}

			partnersLink := validators.BuildPartnersPortalLink(
				utils.SafeStringDereference(s.CabinetLink),
				utils.SafeStringDereference(s.IP),
			)

			var statusDetails interface{}
			_ = json.Unmarshal(s.StatusDetails, &statusDetails)

			result[ownerID] = append(result[ownerID], api.FoundEntityDTO{
				EntityType: "Server",
				Data: api.ServerRichDTO{
					UUID:              s.ID,
					ServiceDeskUUID:   externalUUID,
					DeviceName:        s.DeviceName,
					LastUpdatedBy:     s.LastUpdatedBy,
					LastModifiedDate:  s.LastModifiedDate,
					UpdatedAt:         s.UpdatedAt,
					IP:                s.IP,
					OperationalStatus: s.Status,
					HealthStatus:      s.HealthStatus,
					StatusDetails:     statusDetails,
					Anydesk:           s.Anydesk,
					Teamviewer:        s.Teamviewer,
					RDP:               s.RDP,
					Litemanager:       s.Litemanager,
					UniqueID:          s.UniqueID,
					CRMid:             s.CRMid,
					IikoWebLink:       s.IikoWebLink,
					PartnersLink:      partnersLink,
					ServerName:        s.ServerName,
					ServerVersion:     s.ServerVersion,
					ServerEdition:     s.ServerEdition,
					LastPolledAt:      s.LastPolledAt,
				},
			})
		}
	}
	return result
}

func (h *SearchHandler) groupWorkstationsByOwner(ctx context.Context, workstations []workstation.Workstation) map[string][]api.FoundEntityDTO {
	result := make(map[string][]api.FoundEntityDTO)
	for _, ws := range workstations {
		if ws.OwnerID != nil {
			ownerID := *ws.OwnerID

			link, _ := h.linkRepo.GetByInternalID(ctx, nil, "naumen", ws.ID)
			var externalUUID *string
			if link != nil {
				externalUUID = &link.ServiceDeskUUID
			}

			var statusDetails interface{}
			_ = json.Unmarshal(ws.StatusDetails, &statusDetails)

			result[ownerID] = append(result[ownerID], api.FoundEntityDTO{
				EntityType: "Workstation",
				Data: api.WorkstationRichDTO{
					UUID:             ws.ID,
					ServiceDeskUUID:  externalUUID,
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
			})
		}
	}
	return result
}

func (h *SearchHandler) groupFRsByOwner(ctx context.Context, frs []fiscal.FiscalRegister) map[string][]api.FoundEntityDTO {
	result := make(map[string][]api.FoundEntityDTO)
	for _, fr := range frs {
		if fr.OwnerID != nil {
			ownerID := *fr.OwnerID

			link, _ := h.linkRepo.GetByInternalID(ctx, nil, "naumen", fr.ID)
			var externalUUID *string
			if link != nil {
				externalUUID = &link.ServiceDeskUUID
			}

			var statusDetails interface{}
			_ = json.Unmarshal(fr.StatusDetails, &statusDetails)

			result[ownerID] = append(result[ownerID], api.FoundEntityDTO{
				EntityType: "FiscalRegister",
				Data: api.FiscalRegisterRichDTO{
					UUID:               fr.ID,
					ServiceDeskUUID:    externalUUID,
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
			})
		}
	}
	return result
}

func (h *SearchHandler) loadActiveTicketSearchDTOs(ctx context.Context, ownerID string, matchedTicketIDs []string, log anyLogger) []api.TicketSearchDTO {
	if h.ticketRepo == nil {
		return nil
	}
	activeTickets, err := h.ticketRepo.Find(ctx, tickets.TicketFilter{
		CompanyID: ownerID,
		ExcludeStatuses: []string{
			tickets.StatusResolved,
			tickets.StatusClosed,
			tickets.StatusSpam,
			tickets.StatusExecution,
		},
		Limit:  10,
		SortBy: "updated_at desc",
	})
	if err != nil {
		log.Warn("Не удалось получить активные тикеты компании", "company_id", ownerID, "error", err)
		return nil
	}

	matched := make(map[string]struct{}, len(matchedTicketIDs))
	for _, id := range matchedTicketIDs {
		matched[id] = struct{}{}
	}
	filtered := make([]tickets.Ticket, 0, len(activeTickets))
	for _, ticket := range activeTickets {
		if _, exists := matched[ticket.ID]; exists {
			continue
		}
		filtered = append(filtered, ticket)
		if len(filtered) == 5 {
			break
		}
	}
	return h.mapTicketsToSearchDTO(ctx, filtered)
}

type anyLogger interface {
	Warn(msg string, args ...any)
}

func (h *SearchHandler) mapTicketsToSearchDTO(ctx context.Context, items []tickets.Ticket) []api.TicketSearchDTO {
	if len(items) == 0 {
		return []api.TicketSearchDTO{}
	}
	ticketIDs := make([]string, 0, len(items))
	for _, item := range items {
		ticketIDs = append(ticketIDs, item.ID)
	}
	lastComments, err := h.ticketRepo.GetLastComments(ctx, ticketIDs)
	if err != nil {
		lastComments = map[string]tickets.LastCommentInfo{}
	}

	result := make([]api.TicketSearchDTO, 0, len(items))
	for _, item := range items {
		lastComment := lastComments[item.ID]
		lastCommentText := ""
		if !lastComment.IsPrivate {
			lastCommentText = lastComment.Text
		}
		result = append(result, api.TicketSearchDTO{
			ID:           item.ID,
			Number:       item.Number,
			Subject:      item.Subject,
			Description:  item.Description,
			Status:       item.Status,
			CompanyID:    item.CompanyID,
			CompanyName:  item.CompanyName,
			AssigneeName: formatTicketUserName(item.Assignee),
			ReporterName: resolveTicketReporterName(item),
			LastComment:  lastCommentText,
			LastActivity: item.UpdatedAt,
			CreatedAt:    item.CreatedAt,
			UpdatedAt:    item.UpdatedAt,
			IsArchived:   item.IsArchived,
		})
	}
	return result
}

func formatTicketUserName(item *user.User) string {
	if item == nil {
		return ""
	}
	name := strings.TrimSpace(item.FullName)
	if name != "" {
		return name
	}
	name = strings.TrimSpace(strings.Join([]string{item.FirstName, item.LastName}, " "))
	if name != "" {
		return name
	}
	return strings.TrimSpace(item.Username)
}
