package handlers

import (
	"context"
	"encoding/json"
	"etalon-server/internal/contextkeys"
	"etalon-server/internal/core/gateways"
	contractdom "etalon-server/internal/domain/contract"
	userdom "etalon-server/internal/domain/user"
	"etalon-server/internal/infra/config"
	contractsvc "etalon-server/internal/services/contract"
	api "etalon-server/internal/transport/http/dtos"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"etalon-server/internal/services"
	"etalon-server/internal/transport/http/response"

	"github.com/go-chi/chi/v5"
	"gorm.io/datatypes"
)

type BitrixHandler struct {
	service      services.BitrixSyncService
	contractRepo contractdom.Repository
	contractSync gateways.ContractGateway
	userRepo     userdom.Repository
	cfg          *config.Config
}

func NewBitrixHandler(
	service services.BitrixSyncService,
	contractRepo contractdom.Repository,
	contractSync gateways.ContractGateway,
	userRepo userdom.Repository,
	cfg *config.Config,
) *BitrixHandler {
	return &BitrixHandler{
		service:      service,
		contractRepo: contractRepo,
		contractSync: contractSync,
		userRepo:     userRepo,
		cfg:          cfg,
	}
}

func (h *BitrixHandler) RegisterRoutes(r chi.Router) {
	r.Get("/service-points", h.ListServicePoints)
	r.Get("/service-points/contract-sync/state", h.GetContractSyncState)
	r.Get("/service-points/contract-sync/runs/{runID}", h.GetContractSyncRun)
	r.Post("/service-points/contract-sync/refresh", h.RefreshContractSyncState)
	r.Post("/service-points/contract-sync/execute", h.ExecuteContractSync)
	r.Post("/service-points/refresh", h.RefreshServicePoints)
	r.Get("/users/suggest", h.SuggestUser)
	r.Post("/users/refresh", h.RefreshUsers)
	r.Post("/service-points/import/preview", h.PreviewServicePointsImport)
	r.Post("/service-points/import/sync-preview", h.PreviewServicePointsSync)
	r.Post("/service-points/import/apply", h.ImportServicePoints)
}

func (h *BitrixHandler) ListServicePoints(w http.ResponseWriter, r *http.Request) {
	term := strings.TrimSpace(r.URL.Query().Get("term"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	randomIfEmptyRaw := strings.TrimSpace(r.URL.Query().Get("random_if_empty"))
	randomIfEmpty := randomIfEmptyRaw == "1" || strings.EqualFold(randomIfEmptyRaw, "true")

	var items interface{}
	if term != "" || limit > 0 || offset > 0 || randomIfEmpty {
		result, searchErr := h.service.SearchServicePoints(r.Context(), term, limit, offset, randomIfEmpty)
		if searchErr != nil {
			response.RespondWithError(w, http.StatusInternalServerError, searchErr.Error())
			return
		}
		items = result
	} else {
		result, listErr := h.service.ListServicePoints(r.Context())
		if listErr != nil {
			response.RespondWithError(w, http.StatusInternalServerError, listErr.Error())
			return
		}
		items = result
	}
	response.RespondWithJSON(w, http.StatusOK, items)
}

func (h *BitrixHandler) RefreshServicePoints(w http.ResponseWriter, r *http.Request) {
	count, err := h.service.RefreshServicePoints(r.Context())
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"status": "ok",
		"count":  count,
	})
}

func (h *BitrixHandler) GetContractSyncState(w http.ResponseWriter, r *http.Request) {
	payload, err := h.buildContractSyncState(r.Context())
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.RespondWithJSON(w, http.StatusOK, payload)
}

func (h *BitrixHandler) GetContractSyncRun(w http.ResponseWriter, r *http.Request) {
	if h.contractRepo == nil {
		response.RespondWithError(w, http.StatusInternalServerError, "репозиторий контрактов не инициализирован")
		return
	}

	runID := strings.TrimSpace(chi.URLParam(r, "runID"))
	if runID == "" {
		response.RespondWithError(w, http.StatusBadRequest, "не передан идентификатор прогона")
		return
	}

	run, err := h.contractRepo.GetServicePointSyncRunByID(r.Context(), runID)
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if run == nil {
		response.RespondWithError(w, http.StatusNotFound, "прогон синхронизации не найден")
		return
	}

	payload, err := mapContractSyncRunDetails(*run)
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.RespondWithJSON(w, http.StatusOK, payload)
}

func (h *BitrixHandler) RefreshContractSyncState(w http.ResponseWriter, r *http.Request) {
	if h.contractSync == nil {
		response.RespondWithError(w, http.StatusInternalServerError, "воркер контрактной синхронизации не инициализирован")
		return
	}
	if err := h.contractSync.RefreshLatestReport(r.Context()); err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	payload, err := h.buildContractSyncState(r.Context())
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.RespondWithJSON(w, http.StatusOK, payload)
}

func (h *BitrixHandler) buildContractSyncState(ctx context.Context) (api.ContractSyncStateDTO, error) {
	if h.contractRepo == nil {
		return api.ContractSyncStateDTO{}, fmt.Errorf("репозиторий контрактов не инициализирован")
	}

	imports, err := h.contractRepo.ListMailImports(ctx, 20)
	if err != nil {
		return api.ContractSyncStateDTO{}, err
	}

	conflicts, err := h.contractRepo.ListServicePointSyncConflicts(ctx)
	if err != nil {
		return api.ContractSyncStateDTO{}, err
	}
	runs, err := h.contractRepo.ListServicePointSyncRuns(ctx, 20)
	if err != nil {
		return api.ContractSyncStateDTO{}, err
	}

	items := make([]api.ContractMailImportDTO, 0, len(imports))
	for _, item := range imports {
		items = append(items, mapMailImportToDTO(item))
	}

	payload := api.ContractSyncStateDTO{
		RecentImports: items,
		RecentRuns:    mapContractSyncRunSummaries(runs),
		AutoSync:      h.buildContractSyncAutoExecutionDTO(),
		UpsertItems:   make([]api.ContractSyncQueueItemDTO, 0),
		DeleteItems:   make([]api.ContractSyncQueueItemDTO, 0),
	}
	if len(items) > 0 {
		payload.LatestImport = &items[0]
	}
	_ = conflicts

	activeImports, rows, err := findActiveReportImports(imports)
	if err != nil {
		return api.ContractSyncStateDTO{}, err
	}
	if len(activeImports) == 0 {
		return payload, nil
	}

	preview, err := h.service.PreviewContractReportSync(ctx, rows)
	if err != nil {
		return api.ContractSyncStateDTO{}, err
	}

	payload.ActiveReportImports = mapMailImportsToDTO(activeImports)
	activeImportDTO := mapMailImportToDTO(activeImports[0])
	payload.ActiveReportImport = &activeImportDTO
	payload.ReportRows = preview.ReportRows
	payload.ToCreate = preview.ToCreate
	payload.ToUpdate = preview.ToUpdate
	payload.ToDelete = preview.ToDelete
	payload.BlockedRows = preview.BlockedRows
	payload.BlockedItems = mapContractSyncBlockedItems(preview.BlockedItems)
	payload.UpsertItems = mapContractSyncQueueItems(preview.UpsertItems)
	payload.DeleteItems = mapContractSyncQueueItems(preview.DeleteItems)
	return payload, nil
}

func (h *BitrixHandler) ExecuteContractSync(w http.ResponseWriter, r *http.Request) {
	if h.contractRepo == nil {
		response.RespondWithError(w, http.StatusInternalServerError, "репозиторий контрактов не инициализирован")
		return
	}

	imports, err := h.contractRepo.ListMailImports(r.Context(), 50)
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	activeImports, rows, err := findActiveReportImports(imports)
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(activeImports) == 0 {
		response.RespondWithError(w, http.StatusBadRequest, "нет успешно обработанного отчёта для синхронизации")
		return
	}

	preview, previewErr := h.service.PreviewContractReportSync(r.Context(), rows)
	if previewErr != nil {
		response.RespondWithError(w, http.StatusInternalServerError, previewErr.Error())
		return
	}

	var payload services.ContractReportSyncExecuteOptions
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "не удалось прочитать тело запроса")
		return
	}

	startedAt := time.Now().UTC()
	result, err := h.service.ExecuteContractReportSync(r.Context(), rows, payload)
	completedAt := time.Now().UTC()
	actor := h.resolveContractSyncRunActor(r)
	if err != nil {
		h.storeContractSyncRun(
			r.Context(),
			buildContractSyncRunStoreInput(
				contractdom.ServicePointSyncRunModeManual,
				contractdom.ServicePointSyncRunStatusFailed,
				activeImports,
				filterSelectedContractSyncQueueItems(payload.SelectedKeys, payload.QueueItems),
				preview,
				nil,
				actor,
				startedAt,
				&completedAt,
				err.Error(),
			),
		)
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.storeContractSyncRun(
		r.Context(),
		buildContractSyncRunStoreInput(
			contractdom.ServicePointSyncRunModeManual,
			contractSyncRunStatusFromResult(result),
			activeImports,
			filterSelectedContractSyncQueueItems(payload.SelectedKeys, payload.QueueItems),
			preview,
			result,
			actor,
			startedAt,
			&completedAt,
			"",
		),
	)

	response.RespondWithJSON(w, http.StatusOK, api.ContractSyncExecuteResultDTO{
		Processed:    result.Processed,
		Created:      result.Created,
		Updated:      result.Updated,
		Deleted:      result.Deleted,
		AppliedKeys:  result.AppliedKeys,
		Errors:       result.Errors,
		ErrorDetails: mapContractSyncExecuteErrors(result.ErrorDetails),
	})
}

func (h *BitrixHandler) RefreshUsers(w http.ResponseWriter, r *http.Request) {
	count, err := h.service.RefreshUsers(r.Context())
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	users, err := h.service.ListCachedUsers(r.Context())
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	items := make([]api.BitrixDirectoryUserDTO, 0, len(users))
	for _, item := range users {
		items = append(items, api.BitrixDirectoryUserDTO{
			B24UserID:  item.B24UserID,
			Name:       item.Name,
			Active:     item.Active,
			LastName:   item.LastName,
			FirstName:  item.FirstName,
			SecondName: item.SecondName,
			Email:      item.Email,
			Phone:      item.Phone,
			LastSeenAt: item.LastSeenAt,
			UpdatedAt:  item.UpdatedAt,
		})
	}

	response.RespondWithJSON(w, http.StatusOK, api.BitrixUsersRefreshDTO{
		Status: "ok",
		Count:  count,
		Users:  items,
	})
}

func (h *BitrixHandler) SuggestUser(w http.ResponseWriter, r *http.Request) {
	firstName := strings.TrimSpace(r.URL.Query().Get("first_name"))
	lastName := strings.TrimSpace(r.URL.Query().Get("last_name"))
	fullName := strings.TrimSpace(r.URL.Query().Get("full_name"))
	if firstName == "" || lastName == "" {
		response.RespondWithJSON(w, http.StatusOK, map[string]interface{}{"suggestion": nil})
		return
	}

	items, err := h.service.SearchBitrixUsersByName(r.Context(), firstName, lastName, fullName)
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(items) == 0 {
		response.RespondWithJSON(w, http.StatusOK, map[string]interface{}{"suggestion": nil})
		return
	}

	found := items[0]
	name := strings.TrimSpace(strings.Join([]string{found.LastName, found.FirstName, found.SecondName}, " "))
	response.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"suggestion": &api.BitrixUserSuggestionDTO{
			B24UserID: found.B24UserID,
			Name:      name,
		},
	})
}

func (h *BitrixHandler) PreviewServicePointsImport(w http.ResponseWriter, r *http.Request) {
	fileName, content, err := extractImportFile(r)
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	preview, err := h.service.PreviewServicePointsImport(r.Context(), fileName, content)
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	response.RespondWithJSON(w, http.StatusOK, preview)
}

func (h *BitrixHandler) ImportServicePoints(w http.ResponseWriter, r *http.Request) {
	fileName, content, err := extractImportFile(r)
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	mapping := services.ServicePointImportMapping{
		CodeColumn:     r.FormValue("code_column"),
		NameColumn:     r.FormValue("name_column"),
		ContractColumn: r.FormValue("contract_column"),
	}

	if mapping.CodeColumn == "" || mapping.NameColumn == "" || mapping.ContractColumn == "" {
		var payload services.ServicePointImportMapping
		if decodeErr := json.NewDecoder(r.Body).Decode(&payload); decodeErr == nil {
			mapping = payload
		}
	}

	options := services.ServicePointSyncApplyOptions{
		SelectedRows: parseSelectedRows(r.FormValue("selected_rows")),
		SelectedKeys: parseSelectedKeys(r.FormValue("selected_keys")),
	}

	result, err := h.service.ImportServicePoints(r.Context(), fileName, content, mapping, options)
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	response.RespondWithJSON(w, http.StatusOK, result)
}

func (h *BitrixHandler) PreviewServicePointsSync(w http.ResponseWriter, r *http.Request) {
	fileName, content, err := extractImportFile(r)
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	mapping := services.ServicePointImportMapping{
		CodeColumn:     r.FormValue("code_column"),
		NameColumn:     r.FormValue("name_column"),
		ContractColumn: r.FormValue("contract_column"),
	}

	preview, err := h.service.PreviewServicePointsSync(r.Context(), fileName, content, mapping)
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	response.RespondWithJSON(w, http.StatusOK, preview)
}

func extractImportFile(r *http.Request) (string, []byte, error) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		return "", nil, fmt.Errorf("не удалось прочитать multipart-запрос: %w", err)
	}

	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		return "", nil, fmt.Errorf("не передан файл в поле file: %w", err)
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		return "", nil, fmt.Errorf("не удалось прочитать файл: %w", err)
	}
	if len(content) == 0 {
		return "", nil, fmt.Errorf("передан пустой файл")
	}

	return fileHeader.Filename, content, nil
}

func parseSelectedRows(raw string) []int {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}

	var values []int
	if strings.HasPrefix(trimmed, "[") {
		var parsed []int
		if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
			for _, row := range parsed {
				if row > 0 {
					values = append(values, row)
				}
			}
			return values
		}
	}

	parts := strings.Split(trimmed, ",")
	for _, part := range parts {
		row, err := strconv.Atoi(strings.TrimSpace(part))
		if err == nil && row > 0 {
			values = append(values, row)
		}
	}
	if len(values) == 0 {
		return nil
	}
	return values
}

func parseSelectedKeys(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}

	if strings.HasPrefix(trimmed, "[") {
		var parsed []string
		if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
			return uniqueSelectedKeys(parsed)
		}
	}

	return uniqueSelectedKeys(strings.Split(trimmed, ","))
}

func uniqueSelectedKeys(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		key := strings.TrimSpace(raw)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func mapMailImportToDTO(item contractdom.MailImport) api.ContractMailImportDTO {
	return api.ContractMailImportDTO{
		ID:             item.ID,
		Source:         mailImportSourceKey(item),
		MessageID:      item.MessageID,
		AttachmentName: item.AttachmentName,
		AttachmentHash: item.AttachmentHash,
		ReceivedAt:     item.ReceivedAt,
		Status:         item.Status,
		ErrorText:      item.ErrorText,
		ProcessedAt:    item.ProcessedAt,
		RowsCount:      countMailImportRows(item.ReportRows),
		CreatedAt:      item.CreatedAt,
		UpdatedAt:      item.UpdatedAt,
	}
}

func mapMailImportsToDTO(items []contractdom.MailImport) []api.ContractMailImportDTO {
	result := make([]api.ContractMailImportDTO, 0, len(items))
	for _, item := range items {
		result = append(result, mapMailImportToDTO(item))
	}
	return result
}

func findActiveReportImports(items []contractdom.MailImport) ([]contractdom.MailImport, []contractsvc.ContractReportRow, error) {
	activeImports := make([]contractdom.MailImport, 0, 2)
	mergedRows := make([]contractsvc.ContractReportRow, 0, 1024)
	seenSources := make(map[string]struct{}, 2)
	seenImports := make(map[string]struct{}, 2)
	for i := range items {
		if items[i].Status != contractdom.MailImportStatusProcessed {
			continue
		}
		if len(items[i].ReportRows) == 0 {
			continue
		}

		rows, err := decodeContractReportRows(items[i].ReportRows)
		if err != nil {
			return nil, nil, err
		}

		sources := reportImportSourceKeys(rows)
		newSources := make([]string, 0, len(sources))
		for _, source := range sources {
			if _, exists := seenSources[source]; exists {
				continue
			}
			seenSources[source] = struct{}{}
			newSources = append(newSources, source)
		}
		if len(newSources) == 0 {
			continue
		}

		importKey := mailImportIdentityKey(items[i])
		if _, exists := seenImports[importKey]; exists {
			continue
		}
		seenImports[importKey] = struct{}{}
		activeImports = append(activeImports, items[i])

		mergedRows = append(mergedRows, filterContractReportRowsBySources(rows, newSources)...)
	}
	if len(activeImports) == 0 {
		return nil, nil, nil
	}
	mergedRows = contractsvc.AggregateContractReportRows(mergedRows)
	return activeImports, mergedRows, nil
}

func decodeContractReportRows(raw []byte) ([]contractsvc.ContractReportRow, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	rows := make([]contractsvc.ContractReportRow, 0)
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("не удалось декодировать сохранённый отчёт: %w", err)
	}
	return rows, nil
}

func countMailImportRows(raw []byte) int {
	rows, err := decodeContractReportRows(raw)
	if err != nil {
		return 0
	}
	return len(rows)
}

func reportImportSourceKey(rows []contractsvc.ContractReportRow) string {
	sources := reportImportSourceKeys(rows)
	if len(sources) == 0 {
		return ""
	}
	return strings.Join(sources, ",")
}

func reportImportSourceKeys(rows []contractsvc.ContractReportRow) []string {
	sourceSet := make(map[string]struct{}, 2)
	for _, row := range rows {
		if source := reportImportSourceKeyByCode(row.ServicePointCode); source != "" {
			sourceSet[source] = struct{}{}
		}
		if source := reportImportSourceKeyByCode(row.ContractorID); source != "" {
			sourceSet[source] = struct{}{}
		}
	}
	if len(sourceSet) == 0 && len(rows) > 0 {
		sourceSet["ru"] = struct{}{}
	}
	sources := make([]string, 0, len(sourceSet))
	for source := range sourceSet {
		sources = append(sources, source)
	}
	slices.Sort(sources)
	return sources
}

func filterContractReportRowsBySources(rows []contractsvc.ContractReportRow, sources []string) []contractsvc.ContractReportRow {
	if len(rows) == 0 || len(sources) == 0 {
		return nil
	}
	sourceSet := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		if strings.TrimSpace(source) == "" {
			continue
		}
		sourceSet[source] = struct{}{}
	}
	if len(sourceSet) == 0 {
		return nil
	}

	filtered := make([]contractsvc.ContractReportRow, 0, len(rows))
	for _, row := range rows {
		for _, source := range reportImportRowSourceKeys(row) {
			if _, ok := sourceSet[source]; ok {
				filtered = append(filtered, row)
				break
			}
		}
	}
	return filtered
}

func reportImportRowSourceKeys(row contractsvc.ContractReportRow) []string {
	sourceSet := make(map[string]struct{}, 2)
	if source := reportImportSourceKeyByCode(row.ServicePointCode); source != "" {
		sourceSet[source] = struct{}{}
	}
	if source := reportImportSourceKeyByCode(row.ContractorID); source != "" {
		sourceSet[source] = struct{}{}
	}
	if len(sourceSet) == 0 {
		sourceSet["ru"] = struct{}{}
	}

	sources := make([]string, 0, len(sourceSet))
	for source := range sourceSet {
		sources = append(sources, source)
	}
	slices.Sort(sources)
	return sources
}

func reportImportSourceKeyByCode(code string) string {
	normalizedCode := strings.ToLower(strings.TrimSpace(code))
	switch {
	case strings.HasPrefix(normalizedCode, "id"):
		return "id"
	case strings.HasPrefix(normalizedCode, "ru"):
		return "ru"
	case normalizedCode == "":
		return ""
	default:
		return "ru"
	}
}

func mapContractSyncQueueItems(items []services.ContractReportSyncPlanItem) []api.ContractSyncQueueItemDTO {
	result := make([]api.ContractSyncQueueItemDTO, 0, len(items))
	for _, item := range items {
		result = append(result, api.ContractSyncQueueItemDTO{
			Key:                 item.Key,
			Action:              string(item.Action),
			ServicePointName:    item.ServicePointName,
			ServicePointCode:    item.ServicePointCode,
			ContractorID:        item.ContractorID,
			ContractorName:      item.ContractorName,
			ContractType:        item.ContractType,
			B24ElementID:        item.B24ElementID,
			CurrentName:         item.CurrentName,
			CurrentCode:         item.CurrentCode,
			CurrentContractType: item.CurrentContractType,
			ChangeSet:           mapContractSyncFieldDiffs(item.ChangeSet),
			MatchedPointIDs:     item.MatchedPointIDs,
			FilledFields:        item.FilledFields,
			IsMapped:            item.IsMapped,
			Reason:              item.Reason,
		})
	}
	return result
}

func mapContractSyncFieldDiffs(items []services.ContractReportSyncFieldDiff) []api.ContractSyncFieldDiffDTO {
	result := make([]api.ContractSyncFieldDiffDTO, 0, len(items))
	for _, item := range items {
		result = append(result, api.ContractSyncFieldDiffDTO{
			Field:        item.Field,
			Label:        item.Label,
			CurrentValue: item.CurrentValue,
			NextValue:    item.NextValue,
		})
	}
	return result
}

func mapContractSyncBlockedItems(items []services.ContractReportSyncBlockedItem) []api.ContractSyncBlockedItemDTO {
	result := make([]api.ContractSyncBlockedItemDTO, 0, len(items))
	for _, item := range items {
		result = append(result, api.ContractSyncBlockedItemDTO{
			Key:              item.Key,
			ServicePointName: item.ServicePointName,
			ServicePointCode: item.ServicePointCode,
			ContractorID:     item.ContractorID,
			ContractorName:   item.ContractorName,
			Reason:           item.Reason,
			ResolutionHint:   item.ResolutionHint,
			MatchedPointIDs:  item.MatchedPointIDs,
		})
	}
	return result
}

func mapContractSyncExecuteErrors(items []services.ContractReportSyncErrorDetail) []api.ContractSyncExecuteErrorDTO {
	result := make([]api.ContractSyncExecuteErrorDTO, 0, len(items))
	for _, item := range items {
		result = append(result, api.ContractSyncExecuteErrorDTO{
			Key:              item.Key,
			Action:           string(item.Action),
			ServicePointName: item.ServicePointName,
			ServicePointCode: item.ServicePointCode,
			B24ElementID:     item.B24ElementID,
			Message:          item.Message,
		})
	}
	return result
}

type contractSyncRunActor struct {
	ActorType   string
	ActorUserID *uint
	ActorName   *string
}

type contractSyncRunStoreInput struct {
	Mode          string
	Status        string
	Actor         contractSyncRunActor
	StartedAt     time.Time
	CompletedAt   *time.Time
	Note          *string
	ActiveImports []contractdom.MailImport
	Preview       *services.ContractReportSyncPreview
	QueueItems    []services.ContractReportSyncPlanItem
	Result        *services.ContractReportSyncExecuteResult
}

func (h *BitrixHandler) buildContractSyncAutoExecutionDTO() *api.ContractSyncAutoExecutionDTO {
	if h.cfg == nil {
		return nil
	}
	return &api.ContractSyncAutoExecutionDTO{
		Enabled:           h.cfg.EnableContractBitrixAutoSync,
		IntervalMinutes:   int(h.cfg.ContractSyncInterval / time.Minute),
		AppliesCreates:    true,
		AppliesUpdates:    true,
		AppliesDeletes:    h.cfg.ContractBitrixAutoSyncApplyDeletes,
		TriggerLabel:      "После планового почтового прогона",
		SafetyDescription: "Автоматический режим применяет create/update. Удаления и спорные строки можно оставить только на ручное подтверждение.",
	}
}

func (h *BitrixHandler) resolveContractSyncRunActor(r *http.Request) contractSyncRunActor {
	actor := contractSyncRunActor{
		ActorType: contractdom.ServicePointSyncRunActorSystem,
		ActorName: nullableStringPtr("Система"),
	}
	if r == nil {
		return actor
	}

	rawUserID, ok := r.Context().Value(contextkeys.UserIDContextKey).(string)
	if !ok || strings.TrimSpace(rawUserID) == "" {
		return actor
	}

	parsedUserID, err := strconv.ParseUint(strings.TrimSpace(rawUserID), 10, 32)
	if err != nil || parsedUserID == 0 {
		return actor
	}

	userID := uint(parsedUserID)
	actor.ActorType = contractdom.ServicePointSyncRunActorUser
	actor.ActorUserID = &userID
	if h.userRepo == nil {
		actor.ActorName = nullableStringPtr(fmt.Sprintf("Пользователь #%d", userID))
		return actor
	}

	userItem, err := h.userRepo.GetByID(r.Context(), userID)
	if err != nil || userItem == nil {
		actor.ActorName = nullableStringPtr(fmt.Sprintf("Пользователь #%d", userID))
		return actor
	}

	name := strings.TrimSpace(userItem.FullName)
	if name == "" {
		name = strings.TrimSpace(userItem.Username)
	}
	if name == "" {
		name = fmt.Sprintf("Пользователь #%d", userID)
	}
	actor.ActorName = nullableStringPtr(name)
	return actor
}

func buildContractSyncRunStoreInput(
	mode string,
	status string,
	activeImports []contractdom.MailImport,
	queueItems []services.ContractReportSyncPlanItem,
	preview *services.ContractReportSyncPreview,
	result *services.ContractReportSyncExecuteResult,
	actor contractSyncRunActor,
	startedAt time.Time,
	completedAt *time.Time,
	note string,
) contractSyncRunStoreInput {
	return contractSyncRunStoreInput{
		Mode:          mode,
		Status:        status,
		Actor:         actor,
		StartedAt:     startedAt,
		CompletedAt:   completedAt,
		Note:          nullableStringPtr(note),
		ActiveImports: activeImports,
		Preview:       preview,
		QueueItems:    queueItems,
		Result:        result,
	}
}

func (h *BitrixHandler) storeContractSyncRun(ctx context.Context, input contractSyncRunStoreInput) {
	if h.contractRepo == nil {
		return
	}

	activeImportsJSON, err := marshalContractSyncRunActiveImports(input.ActiveImports)
	if err != nil {
		return
	}
	queueItemsJSON, err := marshalContractSyncRunQueueItems(input.QueueItems)
	if err != nil {
		return
	}
	errorsJSON, err := marshalContractSyncRunErrors(input.Result)
	if err != nil {
		return
	}
	errorDetailsJSON, err := marshalContractSyncRunErrorDetails(input.Result)
	if err != nil {
		return
	}

	run := &contractdom.ServicePointSyncRun{
		Mode:          strings.TrimSpace(input.Mode),
		Status:        strings.TrimSpace(input.Status),
		ActorType:     strings.TrimSpace(input.Actor.ActorType),
		ActorUserID:   input.Actor.ActorUserID,
		ActorName:     input.Actor.ActorName,
		Note:          input.Note,
		StartedAt:     input.StartedAt,
		CompletedAt:   input.CompletedAt,
		ActiveImports: datatypes.JSON(activeImportsJSON),
		QueueItems:    datatypes.JSON(queueItemsJSON),
		Errors:        datatypes.JSON(errorsJSON),
		ErrorDetails:  datatypes.JSON(errorDetailsJSON),
	}
	if input.Preview != nil {
		run.ReportRows = input.Preview.ReportRows
		run.ToCreate = input.Preview.ToCreate
		run.ToUpdate = input.Preview.ToUpdate
		run.ToDelete = input.Preview.ToDelete
		run.BlockedRows = input.Preview.BlockedRows
	}
	if input.Result != nil {
		run.Processed = input.Result.Processed
		run.Created = input.Result.Created
		run.Updated = input.Result.Updated
		run.Deleted = input.Result.Deleted
	}
	run.LastUpdatedBy = contractSyncRunUpdatedBy(input)

	_ = h.contractRepo.CreateServicePointSyncRun(ctx, run)
}

func contractSyncRunUpdatedBy(input contractSyncRunStoreInput) string {
	if strings.TrimSpace(input.Mode) == contractdom.ServicePointSyncRunModeAutomatic {
		return "contract_sync_auto"
	}
	if input.Actor.ActorUserID != nil && *input.Actor.ActorUserID > 0 {
		return fmt.Sprintf("user:%d", *input.Actor.ActorUserID)
	}
	return "contract_sync_manual"
}

func contractSyncRunStatusFromResult(result *services.ContractReportSyncExecuteResult) string {
	if result == nil {
		return contractdom.ServicePointSyncRunStatusFailed
	}
	if len(result.Errors) > 0 || len(result.ErrorDetails) > 0 {
		return contractdom.ServicePointSyncRunStatusPartial
	}
	return contractdom.ServicePointSyncRunStatusSuccess
}

func filterSelectedContractSyncQueueItems(
	selectedKeys []string,
	items []services.ContractReportSyncPlanItem,
) []services.ContractReportSyncPlanItem {
	keySet := make(map[string]struct{}, len(selectedKeys))
	for _, key := range selectedKeys {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			continue
		}
		keySet[trimmedKey] = struct{}{}
	}
	if len(keySet) == 0 {
		return nil
	}

	filtered := make([]services.ContractReportSyncPlanItem, 0, len(items))
	for _, item := range items {
		if _, ok := keySet[item.Key]; ok {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func mapContractSyncRunSummaries(items []contractdom.ServicePointSyncRun) []api.ContractSyncRunSummaryDTO {
	result := make([]api.ContractSyncRunSummaryDTO, 0, len(items))
	for _, item := range items {
		summary, err := mapContractSyncRunSummary(item)
		if err != nil {
			continue
		}
		result = append(result, summary)
	}
	return result
}

func mapContractSyncRunSummary(item contractdom.ServicePointSyncRun) (api.ContractSyncRunSummaryDTO, error) {
	imports, err := decodeContractSyncRunImports(item.ActiveImports)
	if err != nil {
		return api.ContractSyncRunSummaryDTO{}, err
	}
	return api.ContractSyncRunSummaryDTO{
		ID:            item.ID,
		Status:        item.Status,
		Mode:          item.Mode,
		ActorType:     item.ActorType,
		ActorUserID:   item.ActorUserID,
		ActorName:     strings.TrimSpace(dereferenceString(item.ActorName)),
		Note:          strings.TrimSpace(dereferenceString(item.Note)),
		StartedAt:     item.StartedAt,
		CompletedAt:   item.CompletedAt,
		ReportRows:    item.ReportRows,
		ToCreate:      item.ToCreate,
		ToUpdate:      item.ToUpdate,
		ToDelete:      item.ToDelete,
		BlockedRows:   item.BlockedRows,
		Processed:     item.Processed,
		Created:       item.Created,
		Updated:       item.Updated,
		Deleted:       item.Deleted,
		ActiveImports: imports,
	}, nil
}

func mapContractSyncRunDetails(item contractdom.ServicePointSyncRun) (api.ContractSyncRunDetailsDTO, error) {
	summary, err := mapContractSyncRunSummary(item)
	if err != nil {
		return api.ContractSyncRunDetailsDTO{}, err
	}

	queueItems, err := decodeContractSyncRunQueueItems(item.QueueItems)
	if err != nil {
		return api.ContractSyncRunDetailsDTO{}, err
	}
	errors, err := decodeContractSyncRunErrors(item.Errors)
	if err != nil {
		return api.ContractSyncRunDetailsDTO{}, err
	}
	errorDetails, err := decodeContractSyncRunErrorDetails(item.ErrorDetails)
	if err != nil {
		return api.ContractSyncRunDetailsDTO{}, err
	}

	return api.ContractSyncRunDetailsDTO{
		ContractSyncRunSummaryDTO: summary,
		QueueItems:                queueItems,
		Errors:                    errors,
		ErrorDetails:              errorDetails,
	}, nil
}

func mailImportSourceKey(item contractdom.MailImport) string {
	rows, err := decodeContractReportRows(item.ReportRows)
	if err != nil || len(rows) == 0 {
		return ""
	}
	return reportImportSourceKey(rows)
}

func mailImportIdentityKey(item contractdom.MailImport) string {
	if item.ID != "" {
		return "id:" + item.ID
	}
	if attachmentHash := strings.TrimSpace(item.AttachmentHash); attachmentHash != "" {
		return "attachment:" + attachmentHash
	}
	return strings.Join([]string{
		strings.TrimSpace(item.MessageID),
		strings.TrimSpace(item.AttachmentName),
	}, "|")
}

func marshalContractSyncRunActiveImports(items []contractdom.MailImport) ([]byte, error) {
	snapshots := make([]contractdom.ServicePointSyncRunImportSnapshot, 0, len(items))
	for _, item := range items {
		snapshots = append(snapshots, contractdom.ServicePointSyncRunImportSnapshot{
			ID:             item.ID,
			Source:         mailImportSourceKey(item),
			MessageID:      item.MessageID,
			AttachmentName: item.AttachmentName,
			AttachmentHash: item.AttachmentHash,
			ReceivedAt:     item.ReceivedAt,
			ProcessedAt:    item.ProcessedAt,
			Status:         item.Status,
			RowsCount:      countMailImportRows(item.ReportRows),
		})
	}
	return json.Marshal(snapshots)
}

func marshalContractSyncRunQueueItems(items []services.ContractReportSyncPlanItem) ([]byte, error) {
	snapshots := make([]contractdom.ServicePointSyncRunQueueItemSnapshot, 0, len(items))
	for _, item := range items {
		snapshots = append(snapshots, contractdom.ServicePointSyncRunQueueItemSnapshot{
			Key:                 item.Key,
			Action:              string(item.Action),
			ServicePointName:    item.ServicePointName,
			ServicePointCode:    item.ServicePointCode,
			ContractorID:        item.ContractorID,
			ContractorName:      item.ContractorName,
			ContractType:        item.ContractType,
			B24ElementID:        item.B24ElementID,
			CurrentName:         item.CurrentName,
			CurrentCode:         item.CurrentCode,
			CurrentContractType: item.CurrentContractType,
			ChangeSet:           mapContractSyncRunFieldDiffSnapshots(item.ChangeSet),
			MatchedPointIDs:     item.MatchedPointIDs,
			FilledFields:        item.FilledFields,
			IsMapped:            item.IsMapped,
			Reason:              item.Reason,
		})
	}
	return json.Marshal(snapshots)
}

func mapContractSyncRunFieldDiffSnapshots(items []services.ContractReportSyncFieldDiff) []contractdom.ServicePointSyncRunFieldDiffSnapshot {
	result := make([]contractdom.ServicePointSyncRunFieldDiffSnapshot, 0, len(items))
	for _, item := range items {
		result = append(result, contractdom.ServicePointSyncRunFieldDiffSnapshot{
			Field:        item.Field,
			Label:        item.Label,
			CurrentValue: item.CurrentValue,
			NextValue:    item.NextValue,
		})
	}
	return result
}

func marshalContractSyncRunErrors(result *services.ContractReportSyncExecuteResult) ([]byte, error) {
	if result == nil || len(result.Errors) == 0 {
		return json.Marshal([]string{})
	}
	return json.Marshal(result.Errors)
}

func marshalContractSyncRunErrorDetails(result *services.ContractReportSyncExecuteResult) ([]byte, error) {
	if result == nil || len(result.ErrorDetails) == 0 {
		return json.Marshal([]contractdom.ServicePointSyncRunErrorDetailSnapshot{})
	}

	items := make([]contractdom.ServicePointSyncRunErrorDetailSnapshot, 0, len(result.ErrorDetails))
	for _, item := range result.ErrorDetails {
		items = append(items, contractdom.ServicePointSyncRunErrorDetailSnapshot{
			Key:              item.Key,
			Action:           string(item.Action),
			ServicePointName: item.ServicePointName,
			ServicePointCode: item.ServicePointCode,
			B24ElementID:     item.B24ElementID,
			Message:          item.Message,
		})
	}
	return json.Marshal(items)
}

func decodeContractSyncRunImports(raw []byte) ([]api.ContractMailImportDTO, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	items := make([]contractdom.ServicePointSyncRunImportSnapshot, 0)
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}

	result := make([]api.ContractMailImportDTO, 0, len(items))
	for _, item := range items {
		result = append(result, api.ContractMailImportDTO{
			ID:             item.ID,
			Source:         item.Source,
			MessageID:      item.MessageID,
			AttachmentName: item.AttachmentName,
			AttachmentHash: item.AttachmentHash,
			ReceivedAt:     item.ReceivedAt,
			Status:         item.Status,
			ProcessedAt:    item.ProcessedAt,
			RowsCount:      item.RowsCount,
		})
	}
	return result, nil
}

func decodeContractSyncRunQueueItems(raw []byte) ([]api.ContractSyncQueueItemDTO, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	items := make([]contractdom.ServicePointSyncRunQueueItemSnapshot, 0)
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}

	result := make([]api.ContractSyncQueueItemDTO, 0, len(items))
	for _, item := range items {
		result = append(result, api.ContractSyncQueueItemDTO{
			Key:                 item.Key,
			Action:              item.Action,
			ServicePointName:    item.ServicePointName,
			ServicePointCode:    item.ServicePointCode,
			ContractorID:        item.ContractorID,
			ContractorName:      item.ContractorName,
			ContractType:        item.ContractType,
			B24ElementID:        item.B24ElementID,
			CurrentName:         item.CurrentName,
			CurrentCode:         item.CurrentCode,
			CurrentContractType: item.CurrentContractType,
			ChangeSet:           mapContractSyncRunFieldDiffsToDTO(item.ChangeSet),
			MatchedPointIDs:     item.MatchedPointIDs,
			FilledFields:        item.FilledFields,
			IsMapped:            item.IsMapped,
			Reason:              item.Reason,
		})
	}
	return result, nil
}

func mapContractSyncRunFieldDiffsToDTO(items []contractdom.ServicePointSyncRunFieldDiffSnapshot) []api.ContractSyncFieldDiffDTO {
	result := make([]api.ContractSyncFieldDiffDTO, 0, len(items))
	for _, item := range items {
		result = append(result, api.ContractSyncFieldDiffDTO{
			Field:        item.Field,
			Label:        item.Label,
			CurrentValue: item.CurrentValue,
			NextValue:    item.NextValue,
		})
	}
	return result
}

func decodeContractSyncRunErrors(raw []byte) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	items := make([]string, 0)
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func decodeContractSyncRunErrorDetails(raw []byte) ([]api.ContractSyncExecuteErrorDTO, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	items := make([]contractdom.ServicePointSyncRunErrorDetailSnapshot, 0)
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}

	result := make([]api.ContractSyncExecuteErrorDTO, 0, len(items))
	for _, item := range items {
		result = append(result, api.ContractSyncExecuteErrorDTO{
			Key:              item.Key,
			Action:           item.Action,
			ServicePointName: item.ServicePointName,
			ServicePointCode: item.ServicePointCode,
			B24ElementID:     item.B24ElementID,
			Message:          item.Message,
		})
	}
	return result, nil
}

func nullableStringPtr(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func dereferenceString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

type contractConflictDetails struct {
	MatchedPointIDs      []int64 `json:"matched_point_ids"`
	MappedPointIDs       []int64 `json:"mapped_point_ids"`
	DeletionCandidateIDs []int64 `json:"deletion_candidate_ids"`
}

func extractConflictDetails(details []byte) contractConflictDetails {
	if len(details) == 0 {
		return contractConflictDetails{}
	}

	var payload contractConflictDetails
	if err := json.Unmarshal(details, &payload); err != nil {
		return contractConflictDetails{}
	}
	return payload
}
