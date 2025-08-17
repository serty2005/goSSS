package handlers

import (
	"etalon-server/internal/api"
	"etalon-server/internal/repositories"
	"net/http"
	"strconv"
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

// Search выполняет поиск по всем сущностям.
func (h *SearchHandler) Search(w http.ResponseWriter, r *http.Request) {
	term := r.URL.Query().Get("term")
	if term == "" {
		RespondWithError(w, http.StatusBadRequest, "Search term is required")
		return
	}

	showInactive, _ := strconv.ParseBool(r.URL.Query().Get("show_inactive"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	ctx := r.Context()
	var wg sync.WaitGroup
	var mu sync.Mutex
	var searchResult api.SearchResultDTO
	errChan := make(chan error, 4)

	wg.Add(4)

	go func() {
		defer wg.Done()
		companies, err := h.companyRepo.Search(ctx, term, showInactive, limit, offset)
		if err != nil {
			errChan <- err
			return
		}
		mu.Lock()
		for _, c := range companies {
			searchResult.Companies = append(searchResult.Companies, api.CompanySearchResultDTO{
				ServiceDeskUUID: *c.ServiceDeskUUID, Title: c.Title, Address: c.Address,
				AdditionalName: c.AdditionalName, ActiveContract: c.ActiveContract,
			})
		}
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		servers, err := h.serverRepo.Search(ctx, term, limit, offset)
		if err != nil {
			errChan <- err
			return
		}
		mu.Lock()
		for _, s := range servers {
			searchResult.Servers = append(searchResult.Servers, api.ServerSearchResultDTO{
				ServiceDeskUUID: *s.ServiceDeskUUID, DeviceName: s.DeviceName, IP: s.IP, UniqueID: s.UniqueID,
				Teamviewer: s.Teamviewer, RDP: s.RDP, Anydesk: s.Anydesk, Litemanager: s.Litemanager, OwnerServiceDeskUUID: s.OwnerServiceDeskUUID,
			})
		}
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		workstations, err := h.workstationRepo.Search(ctx, term, limit, offset)
		if err != nil {
			errChan <- err
			return
		}
		mu.Lock()
		for _, ws := range workstations {
			searchResult.Workstations = append(searchResult.Workstations, api.WorkstationSearchResultDTO{
				ServiceDeskUUID: *ws.ServiceDeskUUID, DeviceName: ws.DeviceName, Teamviewer: ws.Teamviewer,
				Anydesk: ws.Anydesk, Litemanager: ws.Litemanager, Description: ws.Description, OwnerServiceDeskUUID: ws.OwnerServiceDeskUUID,
			})
		}
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		frs, err := h.frRepo.Search(ctx, term, limit, offset)
		if err != nil {
			errChan <- err
			return
		}
		mu.Lock()
		for _, fr := range frs {
			searchResult.FiscalRegisters = append(searchResult.FiscalRegisters, api.FiscalRegisterSearchResultDTO{
				ServiceDeskUUID: *fr.ServiceDeskUUID, RNKKT: fr.RNKKT, ModelKKT: fr.ModelKKT, FNExpireDate: fr.FNExpireDate,
				FRSerialNumber: fr.FRSerialNumber, FNNumber: fr.FNNumber, LegalName: fr.LegalName, OwnerServiceDeskUUID: fr.OwnerServiceDeskUUID,
			})
		}
		mu.Unlock()
	}()

	wg.Wait()
	close(errChan)

	for err := range errChan {
		h.logger.Error("Search error", zap.Error(err))
		RespondWithError(w, http.StatusInternalServerError, "An error occurred during search")
		return
	}

	RespondWithJSON(w, http.StatusOK, searchResult)
}
