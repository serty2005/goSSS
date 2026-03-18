package services

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"etalon-server/internal/domain/bitrix"
	b24 "etalon-server/internal/infra/plugins/bitrix"
	"etalon-server/internal/pkg/spreadsheet"
)

const (
	bitrixServicePointContractProperty = "PROPERTY_361"
	bitrixServicePointOneCCodeProperty = "PROPERTY_681"
)

type ServicePointImportColumn struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type ServicePointImportPreview struct {
	HeaderRow  int                        `json:"header_row"`
	Columns    []ServicePointImportColumn `json:"columns"`
	SampleRows []map[string]string        `json:"sample_rows"`
	TotalRows  int                        `json:"total_rows"`
}

type ServicePointImportMapping struct {
	CodeColumn     string `json:"code_column"`
	NameColumn     string `json:"name_column"`
	ContractColumn string `json:"contract_column"`
}

type ServicePointSyncAction string

const (
	ServicePointSyncActionCreate    ServicePointSyncAction = "create"
	ServicePointSyncActionUpdate    ServicePointSyncAction = "update"
	ServicePointSyncActionDelete    ServicePointSyncAction = "delete"
	ServicePointSyncActionUnchanged ServicePointSyncAction = "unchanged"
	ServicePointSyncActionSkipped   ServicePointSyncAction = "skipped"
	ServicePointSyncActionAmbiguous ServicePointSyncAction = "ambiguous"
)

type ServicePointSyncPlanItem struct {
	Key             string                 `json:"key"`
	Row             int                    `json:"row"`
	Name            string                 `json:"name"`
	OneCCode        string                 `json:"one_c_code"`
	ContractLabel   string                 `json:"contract_label,omitempty"`
	Action          ServicePointSyncAction `json:"action"`
	Reason          string                 `json:"reason,omitempty"`
	B24ElementID    *int64                 `json:"b24_element_id,omitempty"`
	CurrentCode     string                 `json:"current_code,omitempty"`
	CurrentContract string                 `json:"current_contract,omitempty"`
	MatchedPointIDs []int64                `json:"matched_point_ids,omitempty"`
	AutoApply       bool                   `json:"auto_apply,omitempty"`
}

type ServicePointSyncPreview struct {
	ProcessedRows int                        `json:"processed_rows"`
	ToCreate      int                        `json:"to_create"`
	ToUpdate      int                        `json:"to_update"`
	ToDelete      int                        `json:"to_delete"`
	Unchanged     int                        `json:"unchanged"`
	Skipped       int                        `json:"skipped"`
	Ambiguous     int                        `json:"ambiguous"`
	Items         []ServicePointSyncPlanItem `json:"items"`
}

type ServicePointSyncApplyResult struct {
	ProcessedRows int      `json:"processed_rows"`
	Created       int      `json:"created"`
	Updated       int      `json:"updated"`
	Deleted       int      `json:"deleted"`
	Unchanged     int      `json:"unchanged"`
	Skipped       int      `json:"skipped"`
	Ambiguous     int      `json:"ambiguous"`
	AppliedKeys   []string `json:"applied_keys,omitempty"`
	Errors        []string `json:"errors,omitempty"`
}

type ServicePointSyncApplyOptions struct {
	SelectedRows []int    `json:"selected_rows,omitempty"`
	SelectedKeys []string `json:"selected_keys,omitempty"`
}

type importedServicePointRow struct {
	Row         int
	Name        string
	OneCCode    string
	ContractOn  *bool
	ContractRaw string
}

type bitrixListFieldMeta struct {
	FieldID       string
	Multiple      bool
	ListValueToID map[string]string
}

type bitrixServicePointState struct {
	ID                  int64
	Name                string
	Properties          map[string]interface{}
	CurrentCode         string
	CurrentContract     *bool
	CurrentContractType string
	FilledFields        int
}

func (s *bitrixSyncService) PreviewServicePointsImport(_ context.Context, fileName string, content []byte) (*ServicePointImportPreview, error) {
	rows, err := parseSpreadsheetRows(fileName, content)
	if err != nil {
		return nil, err
	}

	headerRow, columns, err := detectColumns(rows)
	if err != nil {
		return nil, err
	}

	sampleRows := make([]map[string]string, 0, 10)
	totalRows := 0
	for i := headerRow + 1; i < len(rows); i++ {
		row := rows[i]
		cells := make(map[string]string, len(columns))
		hasData := false
		for _, col := range columns {
			idx, convErr := excelColumnKeyToIndex(col.Key)
			if convErr != nil {
				continue
			}
			value := normalizeCell(getCellValue(row, idx))
			cells[col.Key] = value
			if value != "" {
				hasData = true
			}
		}
		if !hasData {
			continue
		}
		totalRows++
		if len(sampleRows) < 10 {
			sampleRows = append(sampleRows, cells)
		}
	}

	return &ServicePointImportPreview{
		HeaderRow:  headerRow + 1,
		Columns:    columns,
		SampleRows: sampleRows,
		TotalRows:  totalRows,
	}, nil
}

func (s *bitrixSyncService) PreviewServicePointsSync(ctx context.Context, fileName string, content []byte, mapping ServicePointImportMapping) (*ServicePointSyncPreview, error) {
	if !s.IsEnabled() {
		return nil, errors.New("синхронизация с Bitrix24 отключена или не настроена")
	}

	parsedRows, err := parseImportedServicePointRows(fileName, content, mapping)
	if err != nil {
		return nil, err
	}

	plan, err := s.buildSyncPlan(ctx, parsedRows)
	if err != nil {
		return nil, err
	}

	return plan, nil
}

func (s *bitrixSyncService) ImportServicePoints(
	ctx context.Context,
	fileName string,
	content []byte,
	mapping ServicePointImportMapping,
	options ServicePointSyncApplyOptions,
) (*ServicePointSyncApplyResult, error) {
	if !s.IsEnabled() {
		return nil, errors.New("синхронизация с Bitrix24 отключена или не настроена")
	}

	parsedRows, err := parseImportedServicePointRows(fileName, content, mapping)
	if err != nil {
		return nil, err
	}

	plan, err := s.buildSyncPlan(ctx, parsedRows)
	if err != nil {
		return nil, err
	}

	iblockID := s.cfg.BitrixServicePointsIBlockID
	iblockType, err := s.client.ListsGetIblockTypeID(ctx, iblockID)
	if err != nil {
		return nil, fmt.Errorf("не удалось определить тип списка Bitrix24: %w", err)
	}

	oneCMeta, err := s.loadBitrixFieldMeta(ctx, iblockType, iblockID, bitrixServicePointOneCCodeProperty)
	if err != nil {
		return nil, err
	}
	contractMeta, err := s.loadBitrixFieldMeta(ctx, iblockType, iblockID, bitrixServicePointContractProperty)
	if err != nil {
		return nil, err
	}

	statesByName, err := s.fetchBitrixServicePointState(ctx, iblockType, iblockID)
	if err != nil {
		return nil, err
	}
	statesByID := make(map[int64]bitrixServicePointState)
	for _, items := range statesByName {
		for _, item := range items {
			statesByID[item.ID] = item
		}
	}

	selectedSet := make(map[string]struct{}, len(options.SelectedKeys)+len(options.SelectedRows))
	for _, key := range options.SelectedKeys {
		if trimmed := strings.TrimSpace(key); trimmed != "" {
			selectedSet[trimmed] = struct{}{}
		}
	}
	for _, row := range options.SelectedRows {
		if row > 0 {
			selectedSet[syncPlanRowKey(row)] = struct{}{}
		}
	}

	result := &ServicePointSyncApplyResult{
		AppliedKeys: make([]string, 0, 64),
		Errors:      make([]string, 0, 16),
	}

	for _, item := range plan.Items {
		result.ProcessedRows++
		switch item.Action {
		case ServicePointSyncActionUnchanged:
			result.Unchanged++
		case ServicePointSyncActionSkipped:
			result.Skipped++
		case ServicePointSyncActionAmbiguous:
			result.Ambiguous++
		}

		switch item.Action {
		case ServicePointSyncActionCreate:
			if len(selectedSet) > 0 {
				if _, ok := selectedSet[item.Key]; !ok {
					result.Skipped++
					continue
				}
			}
			fields := map[string]interface{}{
				"NAME": item.Name,
			}
			fields[bitrixServicePointOneCCodeProperty] = prepareBitrixFieldValue(oneCMeta, item.OneCCode)
			if contractOn, ok := contractLabelToBool(item.ContractLabel); ok {
				contractValue, convErr := prepareContractBoolFieldValue(contractMeta, contractOn)
				if convErr != nil {
					result.Errors = append(result.Errors, convErr.Error())
					continue
				}
				fields[bitrixServicePointContractProperty] = contractValue
			}

			elementCode := fmt.Sprintf("autogen_%d", time.Now().UnixNano())
			if _, createErr := s.client.ListsElementAdd(ctx, iblockType, iblockID, elementCode, fields); createErr != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("не удалось создать точку %q: %v", item.Name, createErr))
				continue
			}
			result.Created++
			result.AppliedKeys = append(result.AppliedKeys, item.Key)

		case ServicePointSyncActionUpdate:
			if item.B24ElementID == nil {
				result.Errors = append(result.Errors, fmt.Sprintf("не указан ID Bitrix для точки %q", item.Name))
				continue
			}
			state, ok := statesByID[*item.B24ElementID]
			if !ok {
				result.Errors = append(result.Errors, fmt.Sprintf("не найдена точка Bitrix для обновления %q", item.Name))
				continue
			}

			fields := map[string]interface{}{
				"NAME": state.Name,
			}
			for propKey, propValue := range state.Properties {
				fields[propKey] = normalizePropertyValueForWrite(propValue)
			}
			fields[bitrixServicePointOneCCodeProperty] = prepareBitrixFieldValue(oneCMeta, item.OneCCode)
			if contractOn, ok := contractLabelToBool(item.ContractLabel); ok {
				contractValue, convErr := prepareContractBoolFieldValue(contractMeta, contractOn)
				if convErr != nil {
					result.Errors = append(result.Errors, convErr.Error())
					continue
				}
				fields[bitrixServicePointContractProperty] = contractValue
			}

			if updateErr := s.client.ListsElementUpdate(ctx, iblockType, iblockID, state.ID, fields); updateErr != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("не удалось обновить точку %q: %v", item.Name, updateErr))
				continue
			}
			result.Updated++
			result.AppliedKeys = append(result.AppliedKeys, item.Key)

		case ServicePointSyncActionDelete:
			if item.B24ElementID == nil {
				result.Errors = append(result.Errors, fmt.Sprintf("не указан ID Bitrix для удаления точки %q", item.Name))
				continue
			}
			if len(selectedSet) > 0 {
				if _, ok := selectedSet[item.Key]; !ok {
					result.Skipped++
					continue
				}
			}
			if deleteErr := s.client.ListsElementDelete(ctx, iblockType, iblockID, *item.B24ElementID); deleteErr != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("не удалось удалить точку %q: %v", item.Name, deleteErr))
				continue
			}
			result.Deleted++
			result.AppliedKeys = append(result.AppliedKeys, item.Key)
		}
	}

	if _, err := s.RefreshServicePoints(ctx); err != nil {
		s.log.Warn("не удалось обновить локальный кэш точек Bitrix после синхронизации", "error", err)
	}
	_ = s.syncLocalOneCData(ctx, parsedRows)

	if len(result.Errors) == 0 {
		result.Errors = nil
	}
	if len(result.AppliedKeys) == 0 {
		result.AppliedKeys = nil
	}

	return result, nil
}

func (s *bitrixSyncService) buildSyncPlan(ctx context.Context, rows []importedServicePointRow) (*ServicePointSyncPreview, error) {
	iblockID := s.cfg.BitrixServicePointsIBlockID
	iblockType, err := s.client.ListsGetIblockTypeID(ctx, iblockID)
	if err != nil {
		return nil, fmt.Errorf("не удалось определить тип списка Bitrix24: %w", err)
	}

	statesByName, err := s.fetchBitrixServicePointState(ctx, iblockType, iblockID)
	if err != nil {
		return nil, err
	}
	mappings, err := s.repo.ListCompanyServicePointMappings(ctx)
	if err != nil {
		return nil, err
	}
	mappedPointIDs := make(map[int64]struct{}, len(mappings))
	for _, mapping := range mappings {
		if mapping.BitrixServicePointID > 0 {
			mappedPointIDs[mapping.BitrixServicePointID] = struct{}{}
		}
	}

	preview := &ServicePointSyncPreview{
		Items: make([]ServicePointSyncPlanItem, 0, len(rows)),
	}
	reportNames := make(map[string]struct{}, len(rows))
	matchedPointIDs := make(map[int64]struct{}, len(rows))

	for _, row := range rows {
		preview.ProcessedRows++
		if row.Name == "" || row.OneCCode == "" {
			preview.Skipped++
			preview.Items = append(preview.Items, ServicePointSyncPlanItem{
				Key:           syncPlanRowKey(row.Row),
				Row:           row.Row,
				Name:          row.Name,
				OneCCode:      row.OneCCode,
				ContractLabel: contractBoolToLabel(row.ContractOn),
				Action:        ServicePointSyncActionSkipped,
				Reason:        "пустое название точки или код 1С",
			})
			continue
		}

		reportNames[normalizePointName(row.Name)] = struct{}{}
		matches := statesByName[normalizePointName(row.Name)]
		if len(matches) == 0 {
			preview.ToCreate++
			preview.Items = append(preview.Items, ServicePointSyncPlanItem{
				Key:           syncPlanRowKey(row.Row),
				Row:           row.Row,
				Name:          row.Name,
				OneCCode:      row.OneCCode,
				ContractLabel: contractBoolToLabel(row.ContractOn),
				Action:        ServicePointSyncActionCreate,
				Reason:        "точка отсутствует в Bitrix24",
			})
			continue
		}
		if len(matches) > 1 {
			conflictIDs := collectDeletionCandidatePointIDs(matches, mappedPointIDs)
			if len(conflictIDs) == 0 {
				preview.Skipped++
				preview.Items = append(preview.Items, ServicePointSyncPlanItem{
					Key:             syncPlanRowKey(row.Row),
					Row:             row.Row,
					Name:            row.Name,
					OneCCode:        row.OneCCode,
					ContractLabel:   contractBoolToLabel(row.ContractOn),
					Action:          ServicePointSyncActionSkipped,
					Reason:          "дубли найдены, но все конфликтующие точки уже закреплены за компаниями",
					MatchedPointIDs: collectAllPointIDs(matches),
				})
				continue
			}
			preview.Ambiguous++
			preview.Items = append(preview.Items, ServicePointSyncPlanItem{
				Key:             syncPlanRowKey(row.Row),
				Row:             row.Row,
				Name:            row.Name,
				OneCCode:        row.OneCCode,
				ContractLabel:   contractBoolToLabel(row.ContractOn),
				Action:          ServicePointSyncActionAmbiguous,
				Reason:          "в Bitrix24 найдено несколько точек с одинаковым NAME",
				MatchedPointIDs: conflictIDs,
			})
			continue
		}

		state := matches[0]
		matchedPointIDs[state.ID] = struct{}{}
		desiredContract := contractBoolToLabel(row.ContractOn)
		currentContract := contractBoolToLabel(state.CurrentContract)
		needUpdate := false
		if normalizeCell(state.CurrentCode) != normalizeCell(row.OneCCode) {
			needUpdate = true
		}
		if row.ContractOn != nil && currentContract != desiredContract {
			needUpdate = true
		}

		if needUpdate {
			preview.ToUpdate++
			id := state.ID
			preview.Items = append(preview.Items, ServicePointSyncPlanItem{
				Key:             syncPlanRowKey(row.Row),
				Row:             row.Row,
				Name:            row.Name,
				OneCCode:        row.OneCCode,
				ContractLabel:   desiredContract,
				Action:          ServicePointSyncActionUpdate,
				B24ElementID:    &id,
				CurrentCode:     state.CurrentCode,
				CurrentContract: currentContract,
				AutoApply:       true,
			})
			continue
		}

		preview.Unchanged++
		id := state.ID
		preview.Items = append(preview.Items, ServicePointSyncPlanItem{
			Key:             syncPlanRowKey(row.Row),
			Row:             row.Row,
			Name:            row.Name,
			OneCCode:        row.OneCCode,
			ContractLabel:   desiredContract,
			Action:          ServicePointSyncActionUnchanged,
			B24ElementID:    &id,
			CurrentCode:     state.CurrentCode,
			CurrentContract: currentContract,
		})
	}

	for _, state := range uniqueServicePointStates(statesByName) {
		if _, matched := matchedPointIDs[state.ID]; matched {
			continue
		}
		if _, mapped := mappedPointIDs[state.ID]; mapped {
			continue
		}
		if _, exists := reportNames[normalizePointName(state.Name)]; exists {
			continue
		}

		preview.ToDelete++
		stateID := state.ID
		preview.Items = append(preview.Items, ServicePointSyncPlanItem{
			Key:             syncPlanDeleteKey(stateID),
			Name:            state.Name,
			OneCCode:        state.CurrentCode,
			Action:          ServicePointSyncActionDelete,
			Reason:          "точка есть в Bitrix24, но отсутствует в загруженном отчёте",
			B24ElementID:    &stateID,
			CurrentCode:     state.CurrentCode,
			CurrentContract: contractBoolToLabel(state.CurrentContract),
		})
	}

	preview.ProcessedRows = len(preview.Items)
	return preview, nil
}

// collectAllPointIDs возвращает идентификаторы всех точек из набора состояний без фильтрации.
func collectAllPointIDs(items []bitrixServicePointState) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		if item.ID > 0 {
			ids = append(ids, item.ID)
		}
	}
	return ids
}

// uniqueServicePointStates разворачивает индекс точек по имени в плоский список без дублей по ID.
func uniqueServicePointStates(statesByName map[string][]bitrixServicePointState) []bitrixServicePointState {
	result := make([]bitrixServicePointState, 0, len(statesByName))
	seen := make(map[int64]struct{}, len(statesByName))
	for _, items := range statesByName {
		for _, item := range items {
			if _, exists := seen[item.ID]; exists {
				continue
			}
			seen[item.ID] = struct{}{}
			result = append(result, item)
		}
	}
	return result
}

// syncPlanRowKey формирует стабильный ключ строки плана для действий по строкам отчета.
func syncPlanRowKey(row int) string {
	return fmt.Sprintf("row:%d", row)
}

// syncPlanDeleteKey формирует стабильный ключ для удаления существующей точки Bitrix24.
func syncPlanDeleteKey(pointID int64) string {
	return fmt.Sprintf("delete:%d", pointID)
}

func (s *bitrixSyncService) fetchBitrixServicePointState(ctx context.Context, iblockType string, iblockID int) (map[string][]bitrixServicePointState, error) {
	statesByName := make(map[string][]bitrixServicePointState)
	items, err := s.client.ListsElementGetAll(ctx, iblockType, iblockID, nil)
	if err != nil {
		return nil, fmt.Errorf("не удалось выгрузить точки Bitrix24: %w", err)
	}

	cachedPoints := make([]bitrix.ServicePoint, 0, len(items))
	for _, item := range items {
		currentCode := normalizeCell(extractPropertyFirstValue(item.Properties[bitrixServicePointOneCCodeProperty]))
		rawContractType := normalizeCell(extractPropertyFirstValue(item.Properties[bitrixServicePointContractProperty]))
		normalizedContractType := normalizeContractType(rawContractType)
		contractOn := contractTypeToBool(normalizedContractType)
		state := bitrixServicePointState{
			ID:                  item.ID,
			Name:                item.Name,
			Properties:          item.Properties,
			CurrentCode:         currentCode,
			CurrentContract:     contractOn,
			CurrentContractType: rawContractType,
			FilledFields:        countFilledElementFields(item),
		}
		normalizedName := normalizePointName(item.Name)
		if normalizedName == "" {
			continue
		}
		statesByName[normalizedName] = append(statesByName[normalizedName], state)
		cachedPoints = append(cachedPoints, bitrix.ServicePoint{
			B24ElementID: item.ID,
			Name:         item.Name,
			OneCCode:     nullableStringValue(currentCode),
			ContractOn:   contractOn,
			ContractType: nullableStringValue(normalizedContractType),
			RawJSON:      item.RawJSON,
			UpdatedAt:    time.Now(),
		})
	}

	if err := s.repo.ReplaceServicePoints(ctx, cachedPoints); err != nil {
		s.log.Warn("не удалось обновить локальный кэш точек Bitrix24 по данным preview", "error", err)
	}

	return statesByName, nil
}

func (s *bitrixSyncService) loadBitrixFieldMeta(ctx context.Context, iblockType string, iblockID int, fieldID string) (*bitrixListFieldMeta, error) {
	field, err := s.client.ListsFieldGet(ctx, iblockType, iblockID, fieldID)
	if err != nil {
		return nil, fmt.Errorf("не удалось получить метаданные поля %s: %w", fieldID, err)
	}

	meta := &bitrixListFieldMeta{
		FieldID:       fieldID,
		Multiple:      strings.EqualFold(strings.TrimSpace(toString(field["MULTIPLE"])), "Y"),
		ListValueToID: make(map[string]string),
	}

	displayValues, _ := field["DISPLAY_VALUES_FORM"].(map[string]interface{})
	for id, labelAny := range displayValues {
		label := normalizeCell(toString(labelAny))
		if label == "" {
			continue
		}
		meta.ListValueToID[strings.ToLower(label)] = id
	}

	return meta, nil
}

func (s *bitrixSyncService) syncLocalOneCData(ctx context.Context, rows []importedServicePointRow) error {
	servicePoints, err := s.repo.ListServicePoints(ctx)
	if err != nil {
		return err
	}

	pointsByName := make(map[string][]int, len(servicePoints))
	for i := range servicePoints {
		normalized := normalizePointName(servicePoints[i].Name)
		if normalized == "" {
			continue
		}
		pointsByName[normalized] = append(pointsByName[normalized], i)
	}

	for _, row := range rows {
		if row.Name == "" || row.OneCCode == "" {
			continue
		}
		indexes := pointsByName[normalizePointName(row.Name)]
		if len(indexes) != 1 {
			continue
		}
		point := servicePoints[indexes[0]]
		if err := s.repo.UpdateServicePointOneCData(ctx, point.B24ElementID, row.OneCCode, row.ContractOn); err != nil {
			s.log.Warn("не удалось обновить локальные данные 1С для точки Bitrix", "point_name", row.Name, "error", err)
		}
	}

	return nil
}

func parseImportedServicePointRows(fileName string, content []byte, mapping ServicePointImportMapping) ([]importedServicePointRow, error) {
	if err := validateImportMapping(mapping); err != nil {
		return nil, err
	}

	rows, err := parseSpreadsheetRows(fileName, content)
	if err != nil {
		return nil, err
	}

	headerRow, _, err := detectColumns(rows)
	if err != nil {
		return nil, err
	}

	codeIndex, err := excelColumnKeyToIndex(mapping.CodeColumn)
	if err != nil {
		return nil, fmt.Errorf("некорректная колонка кода: %w", err)
	}
	nameIndex, err := excelColumnKeyToIndex(mapping.NameColumn)
	if err != nil {
		return nil, fmt.Errorf("некорректная колонка названия: %w", err)
	}
	contractIndex, err := excelColumnKeyToIndex(mapping.ContractColumn)
	if err != nil {
		return nil, fmt.Errorf("некорректная колонка контракта: %w", err)
	}

	parsed := make([]importedServicePointRow, 0, len(rows)-headerRow)
	for i := headerRow + 1; i < len(rows); i++ {
		row := rows[i]
		name := normalizeCell(getCellValue(row, nameIndex))
		oneCCode := normalizeCell(getCellValue(row, codeIndex))
		contractRaw := normalizeCell(getCellValue(row, contractIndex))
		if name == "" && oneCCode == "" && contractRaw == "" {
			continue
		}

		parsed = append(parsed, importedServicePointRow{
			Row:         i + 1,
			Name:        name,
			OneCCode:    oneCCode,
			ContractOn:  parseContractStatus(contractRaw),
			ContractRaw: contractRaw,
		})
	}

	return parsed, nil
}

func parseSpreadsheetRows(fileName string, content []byte) ([][]string, error) {
	return spreadsheet.ParseRows(fileName, content)
}

func detectColumns(rows [][]string) (int, []ServicePointImportColumn, error) {
	if len(rows) == 0 {
		return 0, nil, errors.New("в файле нет строк")
	}

	limit := len(rows)
	if limit > 30 {
		limit = 30
	}

	bestRow := -1
	bestCount := 0
	for i := 0; i < limit; i++ {
		count := 0
		for _, cell := range rows[i] {
			if normalizeCell(cell) != "" {
				count++
			}
		}
		if count > bestCount {
			bestCount = count
			bestRow = i
		}
	}

	if bestRow < 0 || bestCount < 2 {
		return 0, nil, errors.New("не удалось определить строку заголовков")
	}

	columns := make([]ServicePointImportColumn, 0, bestCount)
	for idx, cell := range rows[bestRow] {
		name := normalizeCell(cell)
		if name == "" {
			continue
		}
		columns = append(columns, ServicePointImportColumn{Key: indexToExcelColumnKey(idx), Name: name})
	}

	if len(columns) < 2 {
		return 0, nil, errors.New("в строке заголовков недостаточно колонок")
	}

	return bestRow, columns, nil
}

func validateImportMapping(mapping ServicePointImportMapping) error {
	code := strings.ToUpper(strings.TrimSpace(mapping.CodeColumn))
	name := strings.ToUpper(strings.TrimSpace(mapping.NameColumn))
	contract := strings.ToUpper(strings.TrimSpace(mapping.ContractColumn))

	if code == "" || name == "" || contract == "" {
		return errors.New("не выбраны все обязательные колонки")
	}
	if code == name || code == contract || name == contract {
		return errors.New("колонки кода, названия и контракта должны быть разными")
	}

	return nil
}

func indexToExcelColumnKey(index int) string {
	if index < 0 {
		return ""
	}

	n := index + 1
	result := ""
	for n > 0 {
		remainder := (n - 1) % 26
		result = string(rune('A'+remainder)) + result
		n = (n - 1) / 26
	}
	return result
}

func excelColumnKeyToIndex(key string) (int, error) {
	normalized := strings.ToUpper(strings.TrimSpace(key))
	if normalized == "" {
		return 0, errors.New("пустое имя колонки")
	}

	value := 0
	for _, ch := range normalized {
		if ch < 'A' || ch > 'Z' {
			return 0, fmt.Errorf("неверный формат колонки %q", key)
		}
		value = value*26 + int(ch-'A'+1)
	}
	return value - 1, nil
}

func getCellValue(row []string, index int) string {
	if index < 0 || index >= len(row) {
		return ""
	}
	return row[index]
}

func trimRightEmpty(values []string) []string {
	last := len(values)
	for last > 0 {
		if normalizeCell(values[last-1]) != "" {
			break
		}
		last--
	}
	if last == 0 {
		return []string{}
	}
	return values[:last]
}

func normalizeCell(value string) string {
	replacer := strings.NewReplacer("\u00a0", " ", "\t", " ", "\r", " ", "\n", " ", "\x00", "")
	normalized := replacer.Replace(value)
	return strings.Join(strings.Fields(strings.TrimSpace(normalized)), " ")
}

func normalizePointName(name string) string {
	normalized := strings.ToLower(normalizeCell(name))
	normalized = strings.ReplaceAll(normalized, "ё", "е")
	return normalized
}

func parseContractStatus(raw string) *bool {
	value := strings.ToLower(normalizeCell(raw))
	if value == "" {
		return nil
	}

	trueWords := map[string]struct{}{
		"да":            {},
		"yes":           {},
		"true":          {},
		"1":             {},
		"активный":      {},
		"активен":       {},
		"обслуживается": {},
		"действует":     {},
	}
	falseWords := map[string]struct{}{
		"нет":              {},
		"no":               {},
		"false":            {},
		"0":                {},
		"не обслуживается": {},
		"неактивный":       {},
		"не активен":       {},
		"закрыт":           {},
	}

	if _, ok := trueWords[value]; ok {
		v := true
		return &v
	}
	if _, ok := falseWords[value]; ok {
		v := false
		return &v
	}

	if strings.Contains(value, "нет") || strings.Contains(value, "не обслуж") {
		v := false
		return &v
	}
	if strings.Contains(value, "да") || strings.Contains(value, "актив") || strings.Contains(value, "обслуж") {
		v := true
		return &v
	}

	return nil
}

func contractBoolToLabel(v *bool) string {
	if v == nil {
		return ""
	}
	if *v {
		return "Да"
	}
	return "Нет"
}

func contractBoolToType(v *bool) string {
	if v == nil {
		return ""
	}
	if *v {
		return "TS Standart"
	}
	return "Не активен"
}

func contractLabelToBool(label string) (bool, bool) {
	parsed := parseContractStatus(label)
	if parsed == nil {
		return false, false
	}
	return *parsed, true
}

func prepareBitrixFieldValue(meta *bitrixListFieldMeta, value string) interface{} {
	normalized := normalizeCell(value)
	if meta != nil && meta.Multiple {
		return []string{normalized}
	}
	return normalized
}

func prepareContractBoolFieldValue(meta *bitrixListFieldMeta, contractOn bool) (interface{}, error) {
	return prepareContractFieldValue(meta, contractBoolToType(&contractOn))
}

func prepareContractFieldValue(meta *bitrixListFieldMeta, contractType string) (interface{}, error) {
	label := normalizeContractType(contractType)
	if label == "" {
		label = "Не активен"
	}

	if meta != nil && len(meta.ListValueToID) > 0 {
		id := meta.ListValueToID[strings.ToLower(label)]
		if id == "" {
			return nil, fmt.Errorf("в поле %s отсутствует значение %q", bitrixServicePointContractProperty, label)
		}
		if meta.Multiple {
			return []string{id}, nil
		}
		return id, nil
	}

	if meta != nil && meta.Multiple {
		return []string{label}, nil
	}
	return label, nil
}

func normalizeContractType(value string) string {
	switch strings.ToLower(normalizeCell(value)) {
	case "":
		return ""
	case "нет", "не активен", "неактивен":
		return "Не активен"
	case "да", "ts standart", "ts standard":
		return "TS Standart"
	case "ts cloud":
		return "TS Cloud"
	default:
		return normalizeCell(value)
	}
}

func contractTypeToBool(contractType string) *bool {
	switch normalizeContractType(contractType) {
	case "TS Cloud", "TS Standart":
		v := true
		return &v
	case "Не активен":
		v := false
		return &v
	default:
		return nil
	}
}

func countFilledElementFields(item b24.ListElement) int {
	count := 0
	if normalizeCell(item.Name) != "" {
		count++
	}
	if normalizeCell(item.Code) != "" {
		count++
	}
	for _, value := range item.Properties {
		if normalizeCell(extractPropertyFirstValue(value)) != "" {
			count++
		}
	}
	return count
}

func normalizePropertyValueForWrite(value interface{}) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		values := make([]string, 0, len(v))
		for _, key := range keys {
			item := normalizeCell(toString(v[key]))
			if item != "" {
				values = append(values, item)
			}
		}
		if len(values) == 0 {
			return ""
		}
		if len(values) == 1 {
			return values[0]
		}
		return values
	case []interface{}:
		values := make([]string, 0, len(v))
		for _, item := range v {
			normalized := normalizeCell(toString(item))
			if normalized != "" {
				values = append(values, normalized)
			}
		}
		if len(values) == 0 {
			return ""
		}
		if len(values) == 1 {
			return values[0]
		}
		return values
	default:
		return normalizeCell(toString(v))
	}
}

func extractPropertyFirstValue(value interface{}) string {
	switch v := value.(type) {
	case map[string]interface{}:
		if len(v) == 0 {
			return ""
		}
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			normalized := normalizeCell(toString(v[key]))
			if normalized != "" {
				return normalized
			}
		}
	case []interface{}:
		for _, item := range v {
			normalized := normalizeCell(toString(item))
			if normalized != "" {
				return normalized
			}
		}
	default:
		return normalizeCell(toString(v))
	}

	return ""
}

func toString(v interface{}) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatInt(int64(x), 10)
	case int64:
		return strconv.FormatInt(x, 10)
	case int:
		return strconv.Itoa(x)
	default:
		return ""
	}
}
