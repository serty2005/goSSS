package handlers

import (
	"encoding/json"
	contractdom "etalon-server/internal/domain/contract"
	contractsvc "etalon-server/internal/services/contract"
	api "etalon-server/internal/transport/http/dtos"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"etalon-server/internal/services"
	"etalon-server/internal/transport/http/response"

	"github.com/go-chi/chi/v5"
)

type BitrixHandler struct {
	service      services.BitrixSyncService
	contractRepo contractdom.Repository
}

func NewBitrixHandler(service services.BitrixSyncService, contractRepo contractdom.Repository) *BitrixHandler {
	return &BitrixHandler{
		service:      service,
		contractRepo: contractRepo,
	}
}

func (h *BitrixHandler) RegisterRoutes(r chi.Router) {
	r.Get("/service-points", h.ListServicePoints)
	r.Get("/service-points/contract-sync/state", h.GetContractSyncState)
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
	if h.contractRepo == nil {
		response.RespondWithError(w, http.StatusInternalServerError, "репозиторий контрактов не инициализирован")
		return
	}

	imports, err := h.contractRepo.ListMailImports(r.Context(), 20)
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	conflicts, err := h.contractRepo.ListServicePointSyncConflicts(r.Context())
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	items := make([]api.ContractMailImportDTO, 0, len(imports))
	for _, item := range imports {
		items = append(items, api.ContractMailImportDTO{
			ID:             item.ID,
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
		})
	}

	payload := api.ContractSyncStateDTO{
		RecentImports: items,
		UpsertItems:   make([]api.ContractSyncQueueItemDTO, 0),
		DeleteItems:   make([]api.ContractSyncQueueItemDTO, 0),
	}
	if len(items) > 0 {
		payload.LatestImport = &items[0]
	}
	_ = conflicts

	activeImport := findActiveReportImport(imports)
	if activeImport == nil {
		response.RespondWithJSON(w, http.StatusOK, payload)
		return
	}

	rows, err := decodeContractReportRows(activeImport.ReportRows)
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	preview, err := h.service.PreviewContractReportSync(r.Context(), rows)
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	activeImportDTO := mapMailImportToDTO(*activeImport)
	payload.ActiveReportImport = &activeImportDTO
	payload.ReportRows = preview.ReportRows
	payload.ToCreate = preview.ToCreate
	payload.ToUpdate = preview.ToUpdate
	payload.ToDelete = preview.ToDelete
	payload.BlockedRows = preview.BlockedRows
	payload.UpsertItems = mapContractSyncQueueItems(preview.UpsertItems)
	payload.DeleteItems = mapContractSyncQueueItems(preview.DeleteItems)

	response.RespondWithJSON(w, http.StatusOK, payload)
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
	activeImport := findActiveReportImport(imports)
	if activeImport == nil {
		response.RespondWithError(w, http.StatusBadRequest, "нет успешно обработанного отчёта для синхронизации")
		return
	}

	rows, err := decodeContractReportRows(activeImport.ReportRows)
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var payload services.ContractReportSyncExecuteOptions
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "не удалось прочитать тело запроса")
		return
	}

	result, err := h.service.ExecuteContractReportSync(r.Context(), rows, payload)
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

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
	response.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"status": "ok",
		"count":  count,
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

func findActiveReportImport(items []contractdom.MailImport) *contractdom.MailImport {
	for i := range items {
		if items[i].Status != contractdom.MailImportStatusProcessed {
			continue
		}
		if len(items[i].ReportRows) == 0 {
			continue
		}
		return &items[i]
	}
	return nil
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
