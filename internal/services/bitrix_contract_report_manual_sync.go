package services

import (
	"context"
	"encoding/json"
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

type ContractReportSyncBlockedItem struct {
	Key              string  `json:"key"`
	ServicePointName string  `json:"service_point_name,omitempty"`
	ServicePointCode string  `json:"service_point_code,omitempty"`
	ContractorID     string  `json:"contractor_id,omitempty"`
	ContractorName   string  `json:"contractor_name,omitempty"`
	Reason           string  `json:"reason"`
	ResolutionHint   string  `json:"resolution_hint,omitempty"`
	MatchedPointIDs  []int64 `json:"matched_point_ids,omitempty"`
}

type ContractReportSyncPreview struct {
	ReportRows   int                             `json:"report_rows"`
	ToCreate     int                             `json:"to_create"`
	ToUpdate     int                             `json:"to_update"`
	ToDelete     int                             `json:"to_delete"`
	BlockedRows  int                             `json:"blocked_rows"`
	BlockedItems []ContractReportSyncBlockedItem `json:"blocked_items,omitempty"`
	UpsertItems  []ContractReportSyncPlanItem    `json:"upsert_items"`
	DeleteItems  []ContractReportSyncPlanItem    `json:"delete_items"`
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
	rebinds       []contractReportDuplicateRebind
}

type contractReportSyncUpsertTarget struct {
	Row   contractsvc.ContractReportRow
	State *bitrixServicePointState
}

type contractReportPreparedOperation struct {
	Key         string
	Snapshot    ContractReportSyncPlanItem
	Action      ServicePointSyncAction
	ElementID   int64
	ElementCode string
	Fields      map[string]any
	Point       *bitrix.ServicePoint
}

type contractReportDuplicateRebind struct {
	CompanyID string
	FromState bitrixServicePointState
	ToState   bitrixServicePointState
}

func (r contractReportDuplicateRebind) key() string {
	return fmt.Sprintf("%s:%d->%d", strings.TrimSpace(r.CompanyID), r.FromState.ID, r.ToState.ID)
}

type contractReportDuplicateResolution struct {
	StatesByName      map[string][]bitrixServicePointState
	StatesByCode      map[string][]bitrixServicePointState
	MappedPointIDs    map[int64]struct{}
	DeleteReasonsByID map[int64]string
	Rebinds           []contractReportDuplicateRebind
}

type contractReportDuplicateGroupDecision struct {
	Keeper          bitrixServicePointState
	DeleteReasons   map[int64]string
	Rebinds         []contractReportDuplicateRebind
	CollapseForSync bool
	LogMessage      string
}

const contractReportSyncBatchThreshold = 10

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

	rebindErrorsByPointID := make(map[int64]string, len(plan.rebinds)*2)
	for _, rebind := range plan.rebinds {
		if err := s.applyContractReportDuplicateRebind(ctx, rebind); err != nil {
			msg := fmt.Sprintf(
				"не удалось перенести сопоставление компании %q с точки %d на более заполненный дубль %d: %v",
				rebind.CompanyID,
				rebind.FromState.ID,
				rebind.ToState.ID,
				err,
			)
			result.Errors = append(result.Errors, msg)
			rebindErrorsByPointID[rebind.FromState.ID] = msg
			rebindErrorsByPointID[rebind.ToState.ID] = msg
		}
	}

	createOps := make([]contractReportPreparedOperation, 0, len(selectedKeys))
	updateOps := make([]contractReportPreparedOperation, 0, len(selectedKeys))
	deleteOps := make([]contractReportPreparedOperation, 0, len(selectedKeys))
	for index, key := range selectedKeys {
		snapshot, hasSnapshot := plan.queueSnapshot[key]
		if pointID, ok := contractReportPlanPointID(key, plan); ok {
			if msg, failed := rebindErrorsByPointID[pointID]; failed {
				if hasSnapshot {
					appendContractReportSyncItemError(result, snapshot, msg)
				} else {
					result.Errors = append(result.Errors, msg)
				}
				continue
			}
		}
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
			if target.State == nil {
				elementCode := fmt.Sprintf("autogen_%d_%d", time.Now().UnixNano(), index)
				point := bitrix.ServicePoint{
					Name:          target.Row.ServicePointName,
					OneCCode:      nullableStringValue(target.Row.ServicePointCode),
					ContractOn:    contractTypeToBool(target.Row.ContractType),
					ContractType:  nullableStringValue(target.Row.ContractType),
					ContractStart: target.Row.StartDate,
					ContractEnd:   target.Row.EndDate,
					ClientOrder:   nullableStringValue(target.Row.ClientOrder),
				}
				createOps = append(createOps, contractReportPreparedOperation{
					Key:         key,
					Snapshot:    snapshot,
					Action:      ServicePointSyncActionCreate,
					ElementCode: elementCode,
					Fields:      fields,
					Point:       &point,
				})
			} else {
				point := bitrix.ServicePoint{
					B24ElementID:  target.State.ID,
					Name:          target.Row.ServicePointName,
					OneCCode:      nullableStringValue(target.Row.ServicePointCode),
					ContractOn:    contractTypeToBool(target.Row.ContractType),
					ContractType:  nullableStringValue(target.Row.ContractType),
					ContractStart: target.Row.StartDate,
					ContractEnd:   target.Row.EndDate,
					ClientOrder:   nullableStringValue(target.Row.ClientOrder),
				}
				updateOps = append(updateOps, contractReportPreparedOperation{
					Key:       key,
					Snapshot:  snapshot,
					Action:    ServicePointSyncActionUpdate,
					ElementID: target.State.ID,
					Fields:    fields,
					Point:     &point,
				})
			}
			continue
		}

		if target, ok := plan.deleteTargets[key]; ok {
			result.Processed++
			deleteOps = append(deleteOps, contractReportPreparedOperation{
				Key:       key,
				Snapshot:  snapshot,
				Action:    ServicePointSyncActionDelete,
				ElementID: target.ID,
			})
			continue
		}

		if hasSnapshot {
			appendContractReportSyncItemError(result, snapshot, "элемент очереди больше не актуален")
			continue
		}
		result.Errors = append(result.Errors, fmt.Sprintf("элемент очереди %q больше не актуален", key))
	}

	appliedPoints := make([]bitrix.ServicePoint, 0, len(createOps)+len(updateOps))
	deletedPointIDs := make([]int64, 0, len(deleteOps))
	s.executeContractReportCreates(ctx, plan, createOps, result, &appliedPoints)
	s.executeContractReportUpdates(ctx, plan, updateOps, result, &appliedPoints)
	s.executeContractReportDeletes(ctx, plan, deleteOps, result, &deletedPointIDs)

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

	statesByName, err := s.fetchBitrixServicePointState(ctx, iblockType, iblockID)
	if err != nil {
		return nil, err
	}
	mappings, err := s.repo.ListCompanyServicePointMappings(ctx)
	if err != nil {
		return nil, err
	}
	duplicateResolution := s.buildContractReportDuplicateResolution(statesByName, mappings, false)

	upsertTargets := make(map[string]contractReportSyncUpsertTarget, len(options.SelectedKeys))
	deleteTargets := make(map[string]bitrixServicePointState, len(options.SelectedKeys))
	selectedKeys := uniqueStrings(options.SelectedKeys)
	for _, key := range selectedKeys {
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
		rebinds:       collectContractReportSelectedRebinds(selectedKeys, snapshotByKey, upsertTargets, deleteTargets, duplicateResolution.Rebinds),
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

	mappings, err := s.repo.ListCompanyServicePointMappings(ctx)
	if err != nil {
		return nil, err
	}
	duplicateResolution := s.buildContractReportDuplicateResolution(statesByName, mappings, true)
	statesByNameForMatch := duplicateResolution.StatesByName
	statesByCode := duplicateResolution.StatesByCode
	mappedPointIDs := duplicateResolution.MappedPointIDs

	preview := &ContractReportSyncPreview{
		BlockedItems: make([]ContractReportSyncBlockedItem, 0, len(rows)),
		UpsertItems:  make([]ContractReportSyncPlanItem, 0, len(rows)),
		DeleteItems:  buildContractReportDeleteItems(statesByName, mappedPointIDs, duplicateResolution.DeleteReasonsByID),
	}
	upsertTargets := make(map[string]contractReportSyncUpsertTarget, len(rows))

	aggregatedRows := contractsvc.AggregateContractReportRows(rows)
	contractorNames := s.resolveContractorNames(ctx, aggregatedRows)
	preview.ReportRows = len(aggregatedRows)
	for _, row := range aggregatedRows {
		contractorName := contractorNames[strings.TrimSpace(row.ContractorID)]
		if row.ServicePointName == "" || row.ServicePointCode == "" {
			preview.BlockedRows++
			preview.BlockedItems = append(preview.BlockedItems, buildContractReportBlockedItem(
				row,
				contractorName,
				"в строке отчёта отсутствует название точки или код",
				"Проверьте исходный почтовый отчёт: у строки должны быть заполнены и название точки, и её код.",
				nil,
			))
			continue
		}

		key := contractReportSyncUpsertKey(row)
		nameMatches := statesByNameForMatch[normalizePointName(row.ServicePointName)]
		switch len(nameMatches) {
		case 0:
			codeMatches := statesByCode[normalizeCell(row.ServicePointCode)]
			if len(codeMatches) == 1 {
				if item, target, changed := buildContractReportUpsertItem(key, row, &codeMatches[0], contractorName); changed {
					preview.UpsertItems = append(preview.UpsertItems, item)
					upsertTargets[key] = target
					preview.ToUpdate++
				}
				continue
			}

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
			preview.BlockedItems = append(preview.BlockedItems, buildContractReportBlockedItem(
				row,
				contractorName,
				"в Bitrix24 найдено несколько точек с одинаковым названием",
				"Удалите или объедините лишние дубли в Bitrix24, затем нажмите «Обновить».",
				collectAllConflictPointIDs(nameMatches),
			))
		}
	}

	slices.SortFunc(preview.BlockedItems, func(left, right ContractReportSyncBlockedItem) int {
		if cmp := strings.Compare(left.ServicePointName, right.ServicePointName); cmp != 0 {
			return cmp
		}
		if cmp := strings.Compare(left.ServicePointCode, right.ServicePointCode); cmp != 0 {
			return cmp
		}
		return strings.Compare(left.Key, right.Key)
	})
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
		rebinds:       duplicateResolution.Rebinds,
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
	deleteReasonsByID map[int64]string,
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
			case deleteReasonsByID[state.ID] != "":
				reason = deleteReasonsByID[state.ID]
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

func (s *bitrixSyncService) buildContractReportDuplicateResolution(
	statesByName map[string][]bitrixServicePointState,
	mappings []bitrix.CompanyServicePointMapping,
	logDecision bool,
) contractReportDuplicateResolution {
	mappingByPointID := make(map[int64]bitrix.CompanyServicePointMapping, len(mappings))
	mappedPointIDs := make(map[int64]struct{}, len(mappings))
	for _, mapping := range mappings {
		if mapping.BitrixServicePointID <= 0 {
			continue
		}
		mappingByPointID[mapping.BitrixServicePointID] = mapping
		mappedPointIDs[mapping.BitrixServicePointID] = struct{}{}
	}

	statesByNameForMatch := make(map[string][]bitrixServicePointState, len(statesByName))
	statesByCode := make(map[string][]bitrixServicePointState, len(statesByName))
	deleteReasonsByID := make(map[int64]string, len(statesByName))
	rebinds := make([]contractReportDuplicateRebind, 0, 8)
	for normalizedName, group := range statesByName {
		decision := buildContractReportDuplicateGroupDecision(group, mappingByPointID)
		if decision.CollapseForSync {
			for _, rebind := range decision.Rebinds {
				delete(mappedPointIDs, rebind.FromState.ID)
				mappedPointIDs[rebind.ToState.ID] = struct{}{}
			}
			for pointID, reason := range decision.DeleteReasons {
				deleteReasonsByID[pointID] = reason
			}
			statesByNameForMatch[normalizedName] = []bitrixServicePointState{decision.Keeper}
			rebinds = append(rebinds, decision.Rebinds...)
			if logDecision && decision.LogMessage != "" {
				s.log.Debug(
					decision.LogMessage,
					"keeper_element_id", decision.Keeper.ID,
					"keeper_filled_fields", decision.Keeper.FilledFields,
					"service_point_name", decision.Keeper.Name,
					"rebinds_count", len(decision.Rebinds),
				)
			}
			for _, state := range group {
				appendEffectiveStateByCode(statesByCode, state.CurrentCode, decision.Keeper)
			}
			continue
		}

		statesByNameForMatch[normalizedName] = slices.Clone(group)
		for _, state := range group {
			appendEffectiveStateByCode(statesByCode, state.CurrentCode, state)
		}
	}

	return contractReportDuplicateResolution{
		StatesByName:      statesByNameForMatch,
		StatesByCode:      statesByCode,
		MappedPointIDs:    mappedPointIDs,
		DeleteReasonsByID: deleteReasonsByID,
		Rebinds:           rebinds,
	}
}

func buildContractReportDuplicateGroupDecision(
	group []bitrixServicePointState,
	mappingByPointID map[int64]bitrix.CompanyServicePointMapping,
) contractReportDuplicateGroupDecision {
	if len(group) < 2 {
		return contractReportDuplicateGroupDecision{}
	}

	if keeper, ok := chooseUniqueActiveContractState(group); ok {
		return contractReportDuplicateGroupDecision{
			Keeper:          keeper,
			DeleteReasons:   buildDuplicateDeleteReasons(group, keeper.ID, "точка считается дублем, потому что в группе только у другой записи контракт активен"),
			Rebinds:         buildContractReportDuplicateRebinds(group, keeper, mappingByPointID),
			CollapseForSync: true,
			LogMessage:      "Bitrix24 manual sync: для группы дублей выбран элемент с единственным активным контрактом",
		}
	}

	if keeper, ok := chooseFullDuplicateKeeper(group, mappingByPointID); ok {
		return contractReportDuplicateGroupDecision{
			Keeper:          keeper,
			DeleteReasons:   buildDuplicateDeleteReasons(group, keeper.ID, "полный дубль: у записей совпадают все конечные значения полей"),
			Rebinds:         buildContractReportDuplicateRebinds(group, keeper, mappingByPointID),
			CollapseForSync: true,
			LogMessage:      "Bitrix24 manual sync: для группы дублей выбран эталонный элемент с полностью совпадающими полями",
		}
	}

	preferred := choosePreferredServicePointState(group)
	mappedStates := collectMappedDuplicateStates(group, mappingByPointID)
	if len(mappedStates) != 1 {
		return contractReportDuplicateGroupDecision{}
	}
	mappedState := mappedStates[0]
	if preferred.ID == mappedState.ID || preferred.FilledFields <= mappedState.FilledFields {
		return contractReportDuplicateGroupDecision{}
	}

	mapping := mappingByPointID[mappedState.ID]
	if strings.TrimSpace(mapping.CompanyID) == "" {
		return contractReportDuplicateGroupDecision{}
	}

	return contractReportDuplicateGroupDecision{
		Keeper:          preferred,
		Rebinds:         buildContractReportDuplicateRebinds(group, preferred, mappingByPointID),
		CollapseForSync: true,
		LogMessage:      "Bitrix24 manual sync: для группы дублей выбран более заполненный элемент",
	}
}

func choosePreferredServicePointState(group []bitrixServicePointState) bitrixServicePointState {
	preferred := group[0]
	for _, state := range group[1:] {
		switch {
		case state.FilledFields > preferred.FilledFields:
			preferred = state
		case state.FilledFields == preferred.FilledFields && state.ID < preferred.ID:
			preferred = state
		}
	}
	return preferred
}

func chooseUniqueActiveContractState(group []bitrixServicePointState) (bitrixServicePointState, bool) {
	activeStates := make([]bitrixServicePointState, 0, 1)
	for _, state := range group {
		if !isActiveContractState(state) {
			continue
		}
		activeStates = append(activeStates, state)
	}
	if len(activeStates) != 1 || len(activeStates) == len(group) {
		return bitrixServicePointState{}, false
	}
	return activeStates[0], true
}

func chooseFullDuplicateKeeper(
	group []bitrixServicePointState,
	mappingByPointID map[int64]bitrix.CompanyServicePointMapping,
) (bitrixServicePointState, bool) {
	if len(group) < 2 {
		return bitrixServicePointState{}, false
	}
	signature := buildServicePointStateSignature(group[0])
	for _, state := range group[1:] {
		if buildServicePointStateSignature(state) != signature {
			return bitrixServicePointState{}, false
		}
	}

	mappedStates := collectMappedDuplicateStates(group, mappingByPointID)
	if len(mappedStates) == 1 {
		return mappedStates[0], true
	}
	return choosePreferredServicePointState(group), true
}

func collectMappedDuplicateStates(
	group []bitrixServicePointState,
	mappingByPointID map[int64]bitrix.CompanyServicePointMapping,
) []bitrixServicePointState {
	mappedStates := make([]bitrixServicePointState, 0, len(group))
	for _, state := range group {
		if _, mapped := mappingByPointID[state.ID]; !mapped {
			continue
		}
		mappedStates = append(mappedStates, state)
	}
	return mappedStates
}

func buildContractReportDuplicateRebinds(
	group []bitrixServicePointState,
	keeper bitrixServicePointState,
	mappingByPointID map[int64]bitrix.CompanyServicePointMapping,
) []contractReportDuplicateRebind {
	rebinds := make([]contractReportDuplicateRebind, 0, len(group))
	for _, state := range group {
		if state.ID == keeper.ID {
			continue
		}
		mapping, mapped := mappingByPointID[state.ID]
		if !mapped || strings.TrimSpace(mapping.CompanyID) == "" {
			continue
		}
		rebinds = append(rebinds, contractReportDuplicateRebind{
			CompanyID: strings.TrimSpace(mapping.CompanyID),
			FromState: state,
			ToState:   keeper,
		})
	}
	return rebinds
}

func buildDuplicateDeleteReasons(group []bitrixServicePointState, keeperID int64, reason string) map[int64]string {
	result := make(map[int64]string, len(group))
	for _, state := range group {
		if state.ID == keeperID {
			continue
		}
		result[state.ID] = reason
	}
	return result
}

func buildServicePointStateSignature(state bitrixServicePointState) string {
	properties := make(map[string]any, len(state.Properties))
	for key, value := range state.Properties {
		normalized := normalizePropertyValueForWrite(value)
		switch item := normalized.(type) {
		case string:
			if item == "" {
				continue
			}
		case []string:
			if len(item) == 0 {
				continue
			}
		}
		properties[key] = normalized
	}

	payload := map[string]any{
		"name":             normalizeCell(state.Name),
		"current_code":     normalizeCell(state.CurrentCode),
		"current_contract": normalizeContractType(state.CurrentContractType),
		"properties":       properties,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Sprintf("%s|%s|%s|%d", normalizeCell(state.Name), normalizeCell(state.CurrentCode), normalizeContractType(state.CurrentContractType), state.ID)
	}
	return string(encoded)
}

func isActiveContractState(state bitrixServicePointState) bool {
	contractOn := contractTypeToBool(state.CurrentContractType)
	return contractOn != nil && *contractOn
}

func appendEffectiveStateByCode(
	statesByCode map[string][]bitrixServicePointState,
	code string,
	state bitrixServicePointState,
) {
	normalizedCode := normalizeCell(code)
	if normalizedCode == "" {
		return
	}
	if slices.ContainsFunc(statesByCode[normalizedCode], func(item bitrixServicePointState) bool {
		return item.ID == state.ID
	}) {
		return
	}
	statesByCode[normalizedCode] = append(statesByCode[normalizedCode], state)
}

func collectContractReportSelectedRebinds(
	selectedKeys []string,
	snapshotByKey map[string]ContractReportSyncPlanItem,
	upsertTargets map[string]contractReportSyncUpsertTarget,
	deleteTargets map[string]bitrixServicePointState,
	rebinds []contractReportDuplicateRebind,
) []contractReportDuplicateRebind {
	if len(selectedKeys) == 0 || len(rebinds) == 0 {
		return nil
	}

	rebindsByPointID := make(map[int64][]contractReportDuplicateRebind, len(rebinds)*2)
	for _, rebind := range rebinds {
		rebindsByPointID[rebind.FromState.ID] = append(rebindsByPointID[rebind.FromState.ID], rebind)
		rebindsByPointID[rebind.ToState.ID] = append(rebindsByPointID[rebind.ToState.ID], rebind)
	}

	result := make([]contractReportDuplicateRebind, 0, len(rebinds))
	seen := make(map[string]struct{}, len(rebinds))
	for _, key := range selectedKeys {
		pointID := int64(0)
		switch {
		case deleteTargets[key].ID > 0:
			pointID = deleteTargets[key].ID
		case upsertTargets[key].State != nil:
			pointID = upsertTargets[key].State.ID
		case snapshotByKey[key].B24ElementID != nil:
			pointID = *snapshotByKey[key].B24ElementID
		}
		for _, rebind := range rebindsByPointID[pointID] {
			if _, exists := seen[rebind.key()]; exists {
				continue
			}
			seen[rebind.key()] = struct{}{}
			result = append(result, rebind)
		}
	}
	return result
}

func contractReportPlanPointID(key string, plan *contractReportSyncPlan) (int64, bool) {
	if plan == nil {
		return 0, false
	}
	if target, ok := plan.deleteTargets[key]; ok && target.ID > 0 {
		return target.ID, true
	}
	if target, ok := plan.upsertTargets[key]; ok && target.State != nil && target.State.ID > 0 {
		return target.State.ID, true
	}
	if snapshot, ok := plan.queueSnapshot[key]; ok && snapshot.B24ElementID != nil && *snapshot.B24ElementID > 0 {
		return *snapshot.B24ElementID, true
	}
	return 0, false
}

func (s *bitrixSyncService) applyContractReportDuplicateRebind(ctx context.Context, rebind contractReportDuplicateRebind) error {
	if s.repo == nil {
		return fmt.Errorf("репозиторий сопоставлений Bitrix24 не настроен")
	}
	if strings.TrimSpace(rebind.CompanyID) == "" {
		return fmt.Errorf("не указан идентификатор компании для переноса сопоставления")
	}
	if rebind.FromState.ID <= 0 || rebind.ToState.ID <= 0 || rebind.FromState.ID == rebind.ToState.ID {
		return nil
	}

	s.log.Info(
		"Bitrix24 manual sync: переносим сопоставление на более заполненный дубль",
		"company_id", rebind.CompanyID,
		"from_element_id", rebind.FromState.ID,
		"to_element_id", rebind.ToState.ID,
		"from_filled_fields", rebind.FromState.FilledFields,
		"to_filled_fields", rebind.ToState.FilledFields,
		"service_point_name", rebind.ToState.Name,
	)

	migratedTickets := int64(0)
	if s.ticketRepo != nil {
		updated, err := s.ticketRepo.RebindBitrixServicePoint(ctx, rebind.FromState.ID, rebind.ToState.ID)
		if err != nil {
			return fmt.Errorf("не удалось перепривязать тикеты: %w", err)
		}
		migratedTickets = updated
	}
	if err := s.repo.UpsertCompanyServicePointMapping(ctx, &bitrix.CompanyServicePointMapping{
		CompanyID:            rebind.CompanyID,
		BitrixServicePointID: rebind.ToState.ID,
	}); err != nil {
		return fmt.Errorf("не удалось обновить сопоставление компании: %w", err)
	}

	s.log.Info(
		"Bitrix24 manual sync: сопоставление перенесено на более заполненный дубль",
		"company_id", rebind.CompanyID,
		"from_element_id", rebind.FromState.ID,
		"to_element_id", rebind.ToState.ID,
		"migrated_tickets", migratedTickets,
	)
	return nil
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

func buildContractReportBlockedItem(
	row contractsvc.ContractReportRow,
	contractorName string,
	reason string,
	resolutionHint string,
	matchedPointIDs []int64,
) ContractReportSyncBlockedItem {
	key := strings.Join([]string{
		"blocked",
		normalizeCell(row.ServicePointCode),
		normalizePointName(row.ServicePointName),
		strings.TrimSpace(reason),
	}, "|")
	return ContractReportSyncBlockedItem{
		Key:              key,
		ServicePointName: row.ServicePointName,
		ServicePointCode: row.ServicePointCode,
		ContractorID:     row.ContractorID,
		ContractorName:   contractorName,
		Reason:           strings.TrimSpace(reason),
		ResolutionHint:   strings.TrimSpace(resolutionHint),
		MatchedPointIDs:  matchedPointIDs,
	}
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

func (s *bitrixSyncService) executeContractReportCreates(
	ctx context.Context,
	plan *contractReportSyncPlan,
	ops []contractReportPreparedOperation,
	result *ContractReportSyncExecuteResult,
	appliedPoints *[]bitrix.ServicePoint,
) {
	for _, op := range ops {
		s.log.Debug(
			"Bitrix24 manual sync: выполняем create",
			"queue_key", op.Key,
			"element_code", op.ElementCode,
			"service_point_name", op.Snapshot.ServicePointName,
			"service_point_code", op.Snapshot.ServicePointCode,
			"fields_count", len(op.Fields),
		)
		pointID, err := s.client.ListsElementAdd(ctx, plan.iblockType, plan.iblockID, op.ElementCode, op.Fields)
		if err != nil {
			appendContractReportSyncItemError(result, op.Snapshot, err.Error())
			continue
		}

		result.Created++
		result.AppliedKeys = append(result.AppliedKeys, op.Key)
		s.log.Debug(
			"Bitrix24 manual sync: операция create выполнена",
			"queue_key", op.Key,
			"element_id", pointID,
		)
		if op.Point != nil {
			point := *op.Point
			point.B24ElementID = pointID
			*appliedPoints = append(*appliedPoints, point)
		}
	}
}

func (s *bitrixSyncService) executeContractReportUpdates(
	ctx context.Context,
	plan *contractReportSyncPlan,
	ops []contractReportPreparedOperation,
	result *ContractReportSyncExecuteResult,
	appliedPoints *[]bitrix.ServicePoint,
) {
	if len(ops) == 0 {
		return
	}

	if len(ops) <= contractReportSyncBatchThreshold {
		for _, op := range ops {
			s.log.Debug(
				"Bitrix24 manual sync: выполняем update",
				"queue_key", op.Key,
				"element_id", op.ElementID,
				"service_point_name", op.Snapshot.ServicePointName,
				"service_point_code", op.Snapshot.ServicePointCode,
				"fields_count", len(op.Fields),
			)
			if err := s.client.ListsElementUpdate(ctx, plan.iblockType, plan.iblockID, op.ElementID, op.Fields); err != nil {
				appendContractReportSyncItemError(result, op.Snapshot, err.Error())
				continue
			}

			result.Updated++
			result.AppliedKeys = append(result.AppliedKeys, op.Key)
			s.log.Debug(
				"Bitrix24 manual sync: операция update выполнена",
				"queue_key", op.Key,
				"element_id", op.ElementID,
			)
			if op.Point != nil {
				*appliedPoints = append(*appliedPoints, *op.Point)
			}
		}
		return
	}

	s.log.Info("Bitrix24 manual sync: выполняем batch update", "count", len(ops))
	commands := make([]b24.ListElementBatchCommand, 0, len(ops))
	for _, op := range ops {
		commands = append(commands, b24.ListElementBatchCommand{
			Key:          op.Key,
			Action:       b24.ListElementBatchActionUpdate,
			IBlockTypeID: plan.iblockType,
			IBlockID:     plan.iblockID,
			ElementID:    op.ElementID,
			Fields:       op.Fields,
		})
	}

	batchResult, err := s.client.ListsElementBatch(ctx, commands)
	if err != nil {
		for _, op := range ops {
			appendContractReportSyncItemError(result, op.Snapshot, err.Error())
		}
		return
	}

	for _, op := range ops {
		if batchErr, failed := batchResult.Errors[op.Key]; failed {
			appendContractReportSyncItemError(result, op.Snapshot, batchErr.Error())
			continue
		}

		result.Updated++
		result.AppliedKeys = append(result.AppliedKeys, op.Key)
		s.log.Debug(
			"Bitrix24 manual sync: операция update выполнена через batch",
			"queue_key", op.Key,
			"element_id", op.ElementID,
		)
		if op.Point != nil {
			*appliedPoints = append(*appliedPoints, *op.Point)
		}
	}
}

func (s *bitrixSyncService) executeContractReportDeletes(
	ctx context.Context,
	plan *contractReportSyncPlan,
	ops []contractReportPreparedOperation,
	result *ContractReportSyncExecuteResult,
	deletedPointIDs *[]int64,
) {
	if len(ops) == 0 {
		return
	}

	if len(ops) <= contractReportSyncBatchThreshold {
		for _, op := range ops {
			s.log.Debug(
				"Bitrix24 manual sync: выполняем delete",
				"queue_key", op.Key,
				"element_id", op.ElementID,
				"service_point_name", op.Snapshot.ServicePointName,
				"service_point_code", op.Snapshot.ServicePointCode,
			)
			if err := s.client.ListsElementDelete(ctx, plan.iblockType, plan.iblockID, op.ElementID); err != nil {
				appendContractReportSyncItemError(result, op.Snapshot, err.Error())
				continue
			}

			result.Deleted++
			result.AppliedKeys = append(result.AppliedKeys, op.Key)
			*deletedPointIDs = append(*deletedPointIDs, op.ElementID)
			s.log.Debug(
				"Bitrix24 manual sync: операция delete выполнена",
				"queue_key", op.Key,
				"element_id", op.ElementID,
			)
		}
		return
	}

	s.log.Info("Bitrix24 manual sync: выполняем batch delete", "count", len(ops))
	commands := make([]b24.ListElementBatchCommand, 0, len(ops))
	for _, op := range ops {
		commands = append(commands, b24.ListElementBatchCommand{
			Key:          op.Key,
			Action:       b24.ListElementBatchActionDelete,
			IBlockTypeID: plan.iblockType,
			IBlockID:     plan.iblockID,
			ElementID:    op.ElementID,
		})
	}

	batchResult, err := s.client.ListsElementBatch(ctx, commands)
	if err != nil {
		for _, op := range ops {
			appendContractReportSyncItemError(result, op.Snapshot, err.Error())
		}
		return
	}

	for _, op := range ops {
		if batchErr, failed := batchResult.Errors[op.Key]; failed {
			appendContractReportSyncItemError(result, op.Snapshot, batchErr.Error())
			continue
		}

		result.Deleted++
		result.AppliedKeys = append(result.AppliedKeys, op.Key)
		*deletedPointIDs = append(*deletedPointIDs, op.ElementID)
		s.log.Debug(
			"Bitrix24 manual sync: операция delete выполнена через batch",
			"queue_key", op.Key,
			"element_id", op.ElementID,
		)
	}
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
