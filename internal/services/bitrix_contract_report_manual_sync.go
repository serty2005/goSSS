package services

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"etalon-server/internal/domain/bitrix"
	b24 "etalon-server/internal/infra/plugins/bitrix"
	contractsvc "etalon-server/internal/services/contract"
)

type ContractReportSyncPlanItem struct {
	Key                 string                        `json:"key"`
	Action              ServicePointSyncAction        `json:"action"`
	ServicePointName    string                        `json:"service_point_name"`
	ServicePointCode    string                        `json:"service_point_code"`
	ContractorID        string                        `json:"contractor_id,omitempty"`
	ContractorName      string                        `json:"contractor_name,omitempty"`
	ContractType        string                        `json:"contract_type,omitempty"`
	B24ElementID        *int64                        `json:"b24_element_id,omitempty"`
	CurrentName         string                        `json:"current_name,omitempty"`
	CurrentCode         string                        `json:"current_code,omitempty"`
	CurrentContractType string                        `json:"current_contract_type,omitempty"`
	ChangeSet           []ContractReportSyncFieldDiff `json:"change_set,omitempty"`
	MatchedPointIDs     []int64                       `json:"matched_point_ids,omitempty"`
	FilledFields        int                           `json:"filled_fields,omitempty"`
	IsMapped            bool                          `json:"is_mapped,omitempty"`
	Reason              string                        `json:"reason,omitempty"`
}

type ContractReportSyncFieldDiff struct {
	Field        string `json:"field"`
	Label        string `json:"label"`
	CurrentValue string `json:"current_value,omitempty"`
	NextValue    string `json:"next_value,omitempty"`
}

type ContractReportSyncPreview struct {
	ReportRows  int                          `json:"report_rows"`
	ToCreate    int                          `json:"to_create"`
	ToUpdate    int                          `json:"to_update"`
	ToDelete    int                          `json:"to_delete"`
	BlockedRows int                          `json:"blocked_rows"`
	UpsertItems []ContractReportSyncPlanItem `json:"upsert_items"`
	DeleteItems []ContractReportSyncPlanItem `json:"delete_items"`
}

type ContractReportSyncExecuteOptions struct {
	SelectedKeys []string                     `json:"selected_keys,omitempty"`
	QueueItems   []ContractReportSyncPlanItem `json:"queue_items,omitempty"`
}

type ContractReportSyncErrorDetail struct {
	Key              string                 `json:"key"`
	Action           ServicePointSyncAction `json:"action"`
	ServicePointName string                 `json:"service_point_name,omitempty"`
	ServicePointCode string                 `json:"service_point_code,omitempty"`
	B24ElementID     *int64                 `json:"b24_element_id,omitempty"`
	Message          string                 `json:"message"`
}

type ContractReportSyncExecuteResult struct {
	Processed    int                             `json:"processed"`
	Created      int                             `json:"created"`
	Updated      int                             `json:"updated"`
	Deleted      int                             `json:"deleted"`
	AppliedKeys  []string                        `json:"applied_keys,omitempty"`
	Errors       []string                        `json:"errors,omitempty"`
	ErrorDetails []ContractReportSyncErrorDetail `json:"error_details,omitempty"`
}

type contractReportSyncPlan struct {
	preview       *ContractReportSyncPreview
	iblockID      int
	iblockType    string
	oneCMeta      *bitrixListFieldMeta
	contractMeta  *bitrixListFieldMeta
	queueSnapshot map[string]ContractReportSyncPlanItem
	upsertTargets map[string]contractReportSyncUpsertTarget
	deleteTargets map[string]bitrixServicePointState
}

type contractReportSyncUpsertTarget struct {
	Row   contractsvc.ContractReportRow
	State *bitrixServicePointState
}

func (s *bitrixSyncService) PreviewContractReportSync(ctx context.Context, rows []contractsvc.ContractReportRow) (*ContractReportSyncPreview, error) {
	plan, err := s.buildContractReportSyncPlan(ctx, rows)
	if err != nil {
		return nil, err
	}
	return plan.preview, nil
}

func (s *bitrixSyncService) ExecuteContractReportSync(
	ctx context.Context,
	rows []contractsvc.ContractReportRow,
	options ContractReportSyncExecuteOptions,
) (*ContractReportSyncExecuteResult, error) {
	selectedKeys := uniqueStrings(options.SelectedKeys)
	result := &ContractReportSyncExecuteResult{
		AppliedKeys:  make([]string, 0, len(selectedKeys)),
		Errors:       make([]string, 0, len(selectedKeys)),
		ErrorDetails: make([]ContractReportSyncErrorDetail, 0, len(selectedKeys)),
	}
	if len(selectedKeys) == 0 {
		return result, nil
	}

	plan, err := s.buildContractReportExecutePlan(ctx, rows, options)
	if err != nil {
		return nil, err
	}

	currentUpdateElements, err := s.loadCurrentContractReportUpdateElements(ctx, plan, selectedKeys)
	if err != nil {
		return nil, err
	}

	appliedPoints := make([]bitrix.ServicePoint, 0, len(selectedKeys))
	deletedPointIDs := make([]int64, 0, len(selectedKeys))
	for index, key := range selectedKeys {
		snapshot, hasSnapshot := plan.queueSnapshot[key]
		if target, ok := plan.upsertTargets[key]; ok {
			var currentElement *b24.ListElement
			if target.State != nil {
				element, exists := currentUpdateElements[target.State.ID]
				if !exists {
					appendContractReportSyncItemError(result, snapshot, fmt.Sprintf("не удалось получить текущее состояние точки %d", target.State.ID))
					continue
				}
				currentElement = &element
			}

			fields, buildErr := s.buildContractReportUpsertFields(target, currentElement, plan.oneCMeta, plan.contractMeta)
			if buildErr != nil {
				if hasSnapshot {
					appendContractReportSyncItemError(result, snapshot, buildErr.Error())
				} else {
					result.Errors = append(result.Errors, buildErr.Error())
				}
				continue
			}

			result.Processed++
			pointID := int64(0)
			action := b24.ListElementBatchActionAdd
			if target.State == nil {
				elementCode := fmt.Sprintf("autogen_%d_%d", time.Now().UnixNano(), index)
				s.log.Debug(
					"Bitrix24 manual sync: выполняем create",
					"queue_key", key,
					"element_code", elementCode,
					"service_point_name", target.Row.ServicePointName,
					"service_point_code", target.Row.ServicePointCode,
					"fields_count", len(fields),
				)
				pointID, err = s.client.ListsElementAdd(ctx, plan.iblockType, plan.iblockID, elementCode, fields)
				if err != nil {
					appendContractReportSyncItemError(result, snapshot, err.Error())
					continue
				}
				result.Created++
			} else {
				action = b24.ListElementBatchActionUpdate
				pointID = target.State.ID
				s.log.Debug(
					"Bitrix24 manual sync: выполняем update",
					"queue_key", key,
					"element_id", pointID,
					"service_point_name", target.Row.ServicePointName,
					"service_point_code", target.Row.ServicePointCode,
					"fields_count", len(fields),
				)
				if err := s.client.ListsElementUpdate(ctx, plan.iblockType, plan.iblockID, pointID, fields); err != nil {
					appendContractReportSyncItemError(result, snapshot, err.Error())
					continue
				}
				result.Updated++
			}

			result.AppliedKeys = append(result.AppliedKeys, key)
			s.log.Debug(
				"Bitrix24 manual sync: операция upsert выполнена",
				"queue_key", key,
				"action", action,
				"element_id", pointID,
			)
			appliedPoints = append(appliedPoints, bitrix.ServicePoint{
				B24ElementID:  pointID,
				Name:          target.Row.ServicePointName,
				OneCCode:      nullableStringValue(target.Row.ServicePointCode),
				ContractOn:    contractTypeToBool(target.Row.ContractType),
				ContractType:  nullableStringValue(target.Row.ContractType),
				ContractStart: target.Row.StartDate,
				ContractEnd:   target.Row.EndDate,
				ClientOrder:   nullableStringValue(target.Row.ClientOrder),
			})
			continue
		}

		if target, ok := plan.deleteTargets[key]; ok {
			result.Processed++
			s.log.Debug(
				"Bitrix24 manual sync: выполняем delete",
				"queue_key", key,
				"element_id", target.ID,
				"service_point_name", target.Name,
				"service_point_code", target.CurrentCode,
			)
			if err := s.client.ListsElementDelete(ctx, plan.iblockType, plan.iblockID, target.ID); err != nil {
				appendContractReportSyncItemError(result, snapshot, err.Error())
				continue
			}
			result.AppliedKeys = append(result.AppliedKeys, key)
			result.Deleted++
			deletedPointIDs = append(deletedPointIDs, target.ID)
			continue
		}

		if hasSnapshot {
			appendContractReportSyncItemError(result, snapshot, "элемент очереди больше не актуален")
			continue
		}
		result.Errors = append(result.Errors, fmt.Sprintf("элемент очереди %q больше не актуален", key))
	}

	if len(result.AppliedKeys) > 0 {
		for _, point := range appliedPoints {
			if syncErr := s.repo.UpdateServicePointSyncData(ctx, &point); syncErr != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("не удалось сохранить локальные данные точки %d: %v", point.B24ElementID, syncErr))
			}
		}
		if len(deletedPointIDs) > 0 {
			for _, pointID := range deletedPointIDs {
				if deleteMappingErr := s.repo.DeleteCompanyServicePointMappingByPointID(ctx, pointID); deleteMappingErr != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("не удалось удалить локальную привязку точки %d: %v", pointID, deleteMappingErr))
				}
			}
			if deletePointsErr := s.repo.DeleteServicePointsByIDs(ctx, deletedPointIDs); deletePointsErr != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("не удалось удалить точки из локального кэша Bitrix24: %v", deletePointsErr))
			}
		}
	}

	return result, nil
}

func (s *bitrixSyncService) buildContractReportExecutePlan(
	ctx context.Context,
	rows []contractsvc.ContractReportRow,
	options ContractReportSyncExecuteOptions,
) (*contractReportSyncPlan, error) {
	if !s.IsEnabled() {
		return nil, fmt.Errorf("синхронизация с Bitrix24 отключена или не настроена")
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

	aggregatedRows := contractsvc.AggregateContractReportRows(rows)
	rowsByKey := make(map[string]contractsvc.ContractReportRow, len(aggregatedRows))
	for _, row := range aggregatedRows {
		if row.ServicePointName == "" || row.ServicePointCode == "" {
			continue
		}
		rowsByKey[contractReportSyncUpsertKey(row)] = row
	}

	snapshotByKey := make(map[string]ContractReportSyncPlanItem, len(options.QueueItems))
	for _, item := range options.QueueItems {
		snapshotByKey[item.Key] = item
	}

	upsertTargets := make(map[string]contractReportSyncUpsertTarget, len(options.SelectedKeys))
	deleteTargets := make(map[string]bitrixServicePointState, len(options.SelectedKeys))
	for _, key := range uniqueStrings(options.SelectedKeys) {
		snapshot, ok := snapshotByKey[key]
		if !ok {
			return nil, fmt.Errorf("элемент очереди %q отсутствует в снимке UI", key)
		}

		switch snapshot.Action {
		case ServicePointSyncActionCreate:
			row, exists := rowsByKey[key]
			if !exists {
				return nil, fmt.Errorf("для create-элемента %q не найдена строка в последнем отчёте", key)
			}
			upsertTargets[key] = contractReportSyncUpsertTarget{Row: row}
		case ServicePointSyncActionUpdate:
			row, exists := rowsByKey[key]
			if !exists {
				return nil, fmt.Errorf("для update-элемента %q не найдена строка в последнем отчёте", key)
			}
			if snapshot.B24ElementID == nil || *snapshot.B24ElementID <= 0 {
				return nil, fmt.Errorf("для update-элемента %q отсутствует B24ElementID", key)
			}
			upsertTargets[key] = contractReportSyncUpsertTarget{
				Row: row,
				State: &bitrixServicePointState{
					ID:                  *snapshot.B24ElementID,
					Name:                snapshot.ServicePointName,
					CurrentCode:         snapshot.CurrentCode,
					CurrentContractType: snapshot.CurrentContractType,
				},
			}
		case ServicePointSyncActionDelete:
			if snapshot.B24ElementID == nil || *snapshot.B24ElementID <= 0 {
				return nil, fmt.Errorf("для delete-элемента %q отсутствует B24ElementID", key)
			}
			deleteTargets[key] = bitrixServicePointState{
				ID:                  *snapshot.B24ElementID,
				Name:                snapshot.ServicePointName,
				CurrentCode:         snapshot.CurrentCode,
				CurrentContractType: snapshot.CurrentContractType,
			}
		default:
			return nil, fmt.Errorf("элемент очереди %q имеет неподдерживаемое действие %q", key, snapshot.Action)
		}
	}

	return &contractReportSyncPlan{
		iblockID:      iblockID,
		iblockType:    iblockType,
		oneCMeta:      oneCMeta,
		contractMeta:  contractMeta,
		queueSnapshot: snapshotByKey,
		upsertTargets: upsertTargets,
		deleteTargets: deleteTargets,
	}, nil
}

func (s *bitrixSyncService) loadCurrentContractReportUpdateElements(
	ctx context.Context,
	plan *contractReportSyncPlan,
	selectedKeys []string,
) (map[int64]b24.ListElement, error) {
	updateIDs := make([]int64, 0, len(selectedKeys))
	for _, key := range selectedKeys {
		target, ok := plan.upsertTargets[key]
		if !ok || target.State == nil {
			continue
		}
		updateIDs = append(updateIDs, target.State.ID)
	}

	if len(updateIDs) == 0 {
		return map[int64]b24.ListElement{}, nil
	}

	s.log.Debug(
		"Bitrix24 manual sync: загружаем актуальные состояния элементов перед update",
		"element_ids", updateIDs,
		"count", len(updateIDs),
	)
	freshState, err := s.client.ListsElementBatchGetByIDs(ctx, plan.iblockType, plan.iblockID, updateIDs, nil)
	if err != nil {
		return nil, fmt.Errorf("не удалось получить актуальные данные элементов Bitrix24 перед обновлением: %w", err)
	}
	if len(freshState.Errors) > 0 {
		messages := make([]string, 0, len(freshState.Errors))
		for elementID, itemErr := range freshState.Errors {
			messages = append(messages, fmt.Sprintf("%d: %v", elementID, itemErr))
		}
		slices.Sort(messages)
		return nil, fmt.Errorf("не удалось получить актуальные данные элементов Bitrix24 перед обновлением: %s", strings.Join(messages, "; "))
	}

	return freshState.Items, nil
}

func (s *bitrixSyncService) buildContractReportSyncPlan(
	ctx context.Context,
	rows []contractsvc.ContractReportRow,
) (*contractReportSyncPlan, error) {
	if !s.IsEnabled() {
		return nil, fmt.Errorf("синхронизация с Bitrix24 отключена или не настроена")
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
	statesByCode := buildStatesByCode(statesByName)

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

	preview := &ContractReportSyncPreview{
		UpsertItems: make([]ContractReportSyncPlanItem, 0, len(rows)),
		DeleteItems: buildContractReportDeleteItems(statesByName, mappedPointIDs),
	}
	upsertTargets := make(map[string]contractReportSyncUpsertTarget, len(rows))

	aggregatedRows := contractsvc.AggregateContractReportRows(rows)
	contractorNames := s.resolveContractorNames(ctx, aggregatedRows)
	preview.ReportRows = len(aggregatedRows)
	for _, row := range aggregatedRows {
		if row.ServicePointName == "" || row.ServicePointCode == "" {
			preview.BlockedRows++
			continue
		}

		key := contractReportSyncUpsertKey(row)
		contractorName := contractorNames[strings.TrimSpace(row.ContractorID)]
		codeMatches := statesByCode[normalizeCell(row.ServicePointCode)]
		switch len(codeMatches) {
		case 1:
			if item, target, changed := buildContractReportUpsertItem(key, row, &codeMatches[0], contractorName); changed {
				preview.UpsertItems = append(preview.UpsertItems, item)
				upsertTargets[key] = target
				if item.Action == ServicePointSyncActionCreate {
					preview.ToCreate++
				} else {
					preview.ToUpdate++
				}
			}
			continue
		case 0:
		default:
			preview.BlockedRows++
			continue
		}

		nameMatches := statesByName[normalizePointName(row.ServicePointName)]
		switch len(nameMatches) {
		case 0:
			item, target, _ := buildContractReportUpsertItem(key, row, nil, contractorName)
			preview.UpsertItems = append(preview.UpsertItems, item)
			upsertTargets[key] = target
			preview.ToCreate++
		case 1:
			if item, target, changed := buildContractReportUpsertItem(key, row, &nameMatches[0], contractorName); changed {
				preview.UpsertItems = append(preview.UpsertItems, item)
				upsertTargets[key] = target
				if item.Action == ServicePointSyncActionCreate {
					preview.ToCreate++
				} else {
					preview.ToUpdate++
				}
			}
		default:
			preview.BlockedRows++
		}
	}

	slices.SortFunc(preview.UpsertItems, compareContractReportPlanItems)
	slices.SortFunc(preview.DeleteItems, compareContractReportPlanItems)
	preview.ToDelete = len(preview.DeleteItems)

	deleteTargets := make(map[string]bitrixServicePointState, len(preview.DeleteItems))
	for _, item := range preview.DeleteItems {
		if item.B24ElementID == nil {
			continue
		}
		deleteTargets[item.Key] = findStateByID(statesByName, *item.B24ElementID)
	}

	return &contractReportSyncPlan{
		preview:       preview,
		iblockID:      iblockID,
		iblockType:    iblockType,
		oneCMeta:      oneCMeta,
		contractMeta:  contractMeta,
		upsertTargets: upsertTargets,
		deleteTargets: deleteTargets,
	}, nil
}

func (s *bitrixSyncService) resolveContractorNames(ctx context.Context, rows []contractsvc.ContractReportRow) map[string]string {
	if s.companyRepo == nil || len(rows) == 0 {
		return nil
	}

	ids := make([]string, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		id := strings.TrimSpace(row.ContractorID)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil
	}

	companies, err := s.companyRepo.GetByIDs(ctx, ids)
	if err != nil {
		s.log.Warn("Bitrix24 manual sync: не удалось обогатить строки именами компаний", "error", err, "company_ids", len(ids))
		return nil
	}

	names := make(map[string]string, len(companies))
	for _, item := range companies {
		title := ""
		if item.Title != nil {
			title = strings.TrimSpace(*item.Title)
		}
		if title == "" {
			title = strings.TrimSpace(item.ID)
		}
		if title == "" {
			continue
		}
		names[strings.TrimSpace(item.ID)] = title
	}
	return names
}

func buildContractReportUpsertItem(
	key string,
	row contractsvc.ContractReportRow,
	state *bitrixServicePointState,
	contractorName string,
) (ContractReportSyncPlanItem, contractReportSyncUpsertTarget, bool) {
	item := ContractReportSyncPlanItem{
		Key:              key,
		ServicePointName: row.ServicePointName,
		ServicePointCode: row.ServicePointCode,
		ContractorID:     row.ContractorID,
		ContractorName:   contractorName,
		ContractType:     row.ContractType,
	}
	target := contractReportSyncUpsertTarget{Row: row, State: state}

	if state == nil {
		item.Action = ServicePointSyncActionCreate
		item.Reason = "в Bitrix24 не найдено совпадение по коду и названию"
		return item, target, true
	}

	item.Action = ServicePointSyncActionUpdate
	item.B24ElementID = &state.ID
	item.CurrentName = state.Name
	item.CurrentCode = state.CurrentCode
	item.CurrentContractType = state.CurrentContractType

	changeSet := make([]ContractReportSyncFieldDiff, 0, 3)
	if normalizeCell(state.Name) != normalizeCell(row.ServicePointName) {
		changeSet = append(changeSet, ContractReportSyncFieldDiff{
			Field:        "name",
			Label:        "Название",
			CurrentValue: state.Name,
			NextValue:    row.ServicePointName,
		})
	}
	if normalizeCell(state.CurrentCode) != normalizeCell(row.ServicePointCode) {
		changeSet = append(changeSet, ContractReportSyncFieldDiff{
			Field:        "code",
			Label:        "Код точки",
			CurrentValue: state.CurrentCode,
			NextValue:    row.ServicePointCode,
		})
	}
	if normalizeContractType(state.CurrentContractType) != normalizeContractType(row.ContractType) {
		changeSet = append(changeSet, ContractReportSyncFieldDiff{
			Field:        "contract_type",
			Label:        "Тип контракта",
			CurrentValue: state.CurrentContractType,
			NextValue:    row.ContractType,
		})
	}
	if len(changeSet) == 0 {
		return ContractReportSyncPlanItem{}, target, false
	}

	item.ChangeSet = changeSet
	item.Reason = "в Bitrix24 отличаются: " + strings.Join(contractReportSyncDiffLabels(changeSet), ", ")
	return item, target, true
}

func buildContractReportDeleteItems(
	statesByName map[string][]bitrixServicePointState,
	mappedPointIDs map[int64]struct{},
) []ContractReportSyncPlanItem {
	items := make([]ContractReportSyncPlanItem, 0, 16)
	for _, group := range statesByName {
		if len(group) < 2 {
			continue
		}

		hasMapped := false
		maxFilledFields := 0
		for _, state := range group {
			if _, mapped := mappedPointIDs[state.ID]; mapped {
				hasMapped = true
			}
			if state.FilledFields > maxFilledFields {
				maxFilledFields = state.FilledFields
			}
		}

		matchedPointIDs := collectAllConflictPointIDs(group)
		for _, state := range group {
			_, mapped := mappedPointIDs[state.ID]
			reason := ""
			switch {
			case hasMapped && !mapped:
				reason = "точка не сопоставлена с компанией в ServiceDesk"
			case !hasMapped && state.FilledFields < maxFilledFields:
				reason = "точка содержит меньше заполненных данных, чем другие дубли"
			default:
				continue
			}

			stateID := state.ID
			items = append(items, ContractReportSyncPlanItem{
				Key:                 syncPlanDeleteKey(state.ID),
				Action:              ServicePointSyncActionDelete,
				ServicePointName:    state.Name,
				ServicePointCode:    state.CurrentCode,
				ContractType:        state.CurrentContractType,
				B24ElementID:        &stateID,
				CurrentName:         state.Name,
				CurrentCode:         state.CurrentCode,
				CurrentContractType: state.CurrentContractType,
				MatchedPointIDs:     matchedPointIDs,
				FilledFields:        state.FilledFields,
				IsMapped:            mapped,
				Reason:              reason,
			})
		}
	}
	return items
}

func (s *bitrixSyncService) buildContractReportUpsertFields(
	target contractReportSyncUpsertTarget,
	currentElement *b24.ListElement,
	oneCMeta *bitrixListFieldMeta,
	contractMeta *bitrixListFieldMeta,
) (map[string]any, error) {
	fields := map[string]any{
		"NAME": target.Row.ServicePointName,
	}
	if currentElement != nil {
		for propKey, propValue := range currentElement.Properties {
			fields[propKey] = normalizePropertyValueForWrite(propValue)
		}
	}
	contractValue, err := prepareContractFieldValue(contractMeta, target.Row.ContractType)
	if err != nil {
		return nil, err
	}
	fields[bitrixServicePointOneCCodeProperty] = prepareBitrixFieldValue(oneCMeta, target.Row.ServicePointCode)
	fields[bitrixServicePointContractProperty] = contractValue
	return fields, nil
}

func contractReportSyncUpsertKey(row contractsvc.ContractReportRow) string {
	return "report:" + normalizeCell(row.ServicePointCode) + "|" + normalizePointName(row.ServicePointName)
}

func compareContractReportPlanItems(left, right ContractReportSyncPlanItem) int {
	if cmp := strings.Compare(left.ServicePointName, right.ServicePointName); cmp != 0 {
		return cmp
	}
	if cmp := strings.Compare(left.ServicePointCode, right.ServicePointCode); cmp != 0 {
		return cmp
	}
	return strings.Compare(left.Key, right.Key)
}

func findStateByID(statesByName map[string][]bitrixServicePointState, pointID int64) bitrixServicePointState {
	for _, items := range statesByName {
		for _, item := range items {
			if item.ID == pointID {
				return item
			}
		}
	}
	return bitrixServicePointState{}
}

func nullableStringValue(value string) *string {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return nil
	}
	return &normalized
}

func uniqueStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		key := strings.TrimSpace(value)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}
	return result
}

func appendContractReportSyncItemError(result *ContractReportSyncExecuteResult, item ContractReportSyncPlanItem, message string) {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return
	}

	result.Errors = append(result.Errors, fmt.Sprintf("%s: %s", item.Key, trimmed))
	result.ErrorDetails = append(result.ErrorDetails, ContractReportSyncErrorDetail{
		Key:              item.Key,
		Action:           item.Action,
		ServicePointName: item.ServicePointName,
		ServicePointCode: item.ServicePointCode,
		B24ElementID:     item.B24ElementID,
		Message:          trimmed,
	})
}

func contractReportSyncDiffLabels(changeSet []ContractReportSyncFieldDiff) []string {
	labels := make([]string, 0, len(changeSet))
	for _, diff := range changeSet {
		if diff.Label == "" {
			continue
		}
		labels = append(labels, diff.Label)
	}
	return labels
}
