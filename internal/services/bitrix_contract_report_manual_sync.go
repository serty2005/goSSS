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
	Key                 string                 `json:"key"`
	Action              ServicePointSyncAction `json:"action"`
	ServicePointName    string                 `json:"service_point_name"`
	ServicePointCode    string                 `json:"service_point_code"`
	ContractorID        string                 `json:"contractor_id,omitempty"`
	ContractType        string                 `json:"contract_type,omitempty"`
	B24ElementID        *int64                 `json:"b24_element_id,omitempty"`
	CurrentCode         string                 `json:"current_code,omitempty"`
	CurrentContractType string                 `json:"current_contract_type,omitempty"`
	MatchedPointIDs     []int64                `json:"matched_point_ids,omitempty"`
	FilledFields        int                    `json:"filled_fields,omitempty"`
	IsMapped            bool                   `json:"is_mapped,omitempty"`
	Reason              string                 `json:"reason,omitempty"`
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
	SelectedKeys []string `json:"selected_keys,omitempty"`
}

type ContractReportSyncExecuteResult struct {
	Processed   int      `json:"processed"`
	Created     int      `json:"created"`
	Updated     int      `json:"updated"`
	Deleted     int      `json:"deleted"`
	AppliedKeys []string `json:"applied_keys,omitempty"`
	Errors      []string `json:"errors,omitempty"`
}

type contractReportSyncPlan struct {
	preview       *ContractReportSyncPreview
	iblockID      int
	iblockType    string
	oneCMeta      *bitrixListFieldMeta
	contractMeta  *bitrixListFieldMeta
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
	plan, err := s.buildContractReportSyncPlan(ctx, rows)
	if err != nil {
		return nil, err
	}

	selectedKeys := uniqueStrings(options.SelectedKeys)
	result := &ContractReportSyncExecuteResult{
		AppliedKeys: make([]string, 0, len(selectedKeys)),
		Errors:      make([]string, 0, len(selectedKeys)),
	}
	if len(selectedKeys) == 0 {
		return result, nil
	}

	commands := make([]b24.ListElementBatchCommand, 0, len(selectedKeys))
	for index, key := range selectedKeys {
		if target, ok := plan.upsertTargets[key]; ok {
			fields, buildErr := s.buildContractReportUpsertFields(target, plan.oneCMeta, plan.contractMeta)
			if buildErr != nil {
				result.Errors = append(result.Errors, buildErr.Error())
				continue
			}

			command := b24.ListElementBatchCommand{
				Key:          key,
				IBlockTypeID: plan.iblockType,
				IBlockID:     plan.iblockID,
				Fields:       fields,
			}
			if target.State == nil {
				command.Action = b24.ListElementBatchActionAdd
				command.ElementCode = fmt.Sprintf("autogen_%d_%d", time.Now().UnixNano(), index)
			} else {
				command.Action = b24.ListElementBatchActionUpdate
				command.ElementID = target.State.ID
			}
			commands = append(commands, command)
			continue
		}

		if target, ok := plan.deleteTargets[key]; ok {
			commands = append(commands, b24.ListElementBatchCommand{
				Key:          key,
				Action:       b24.ListElementBatchActionDelete,
				IBlockTypeID: plan.iblockType,
				IBlockID:     plan.iblockID,
				ElementID:    target.ID,
			})
			continue
		}

		result.Errors = append(result.Errors, fmt.Sprintf("элемент очереди %q больше не актуален", key))
	}

	if len(commands) == 0 {
		return result, nil
	}

	batchResult, err := s.client.ListsElementBatch(ctx, commands)
	if err != nil {
		return nil, err
	}

	appliedPoints := make([]bitrix.ServicePoint, 0, len(commands))
	for _, command := range commands {
		result.Processed++
		if cmdErr, failed := batchResult.Errors[command.Key]; failed {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", command.Key, cmdErr))
			continue
		}

		result.AppliedKeys = append(result.AppliedKeys, command.Key)
		if target, ok := plan.upsertTargets[command.Key]; ok {
			pointID := command.ElementID
			if command.Action == b24.ListElementBatchActionAdd {
				pointID = batchResult.CreatedIDs[command.Key]
				result.Created++
			} else {
				result.Updated++
			}
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

		if command.Action == b24.ListElementBatchActionDelete {
			result.Deleted++
		}
	}

	if len(result.AppliedKeys) > 0 {
		if _, refreshErr := s.RefreshServicePoints(ctx); refreshErr != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("не удалось обновить локальный кэш точек Bitrix24: %v", refreshErr))
		}
		for _, point := range appliedPoints {
			if syncErr := s.repo.UpdateServicePointSyncData(ctx, &point); syncErr != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("не удалось сохранить локальные данные точки %d: %v", point.B24ElementID, syncErr))
			}
		}
	}

	return result, nil
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
	preview.ReportRows = len(aggregatedRows)
	for _, row := range aggregatedRows {
		if row.ServicePointName == "" || row.ServicePointCode == "" {
			preview.BlockedRows++
			continue
		}

		key := contractReportSyncUpsertKey(row)
		codeMatches := statesByCode[normalizeCell(row.ServicePointCode)]
		switch len(codeMatches) {
		case 1:
			if item, target, changed := buildContractReportUpsertItem(key, row, &codeMatches[0]); changed {
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
			item, target, _ := buildContractReportUpsertItem(key, row, nil)
			preview.UpsertItems = append(preview.UpsertItems, item)
			upsertTargets[key] = target
			preview.ToCreate++
		case 1:
			if item, target, changed := buildContractReportUpsertItem(key, row, &nameMatches[0]); changed {
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

func buildContractReportUpsertItem(
	key string,
	row contractsvc.ContractReportRow,
	state *bitrixServicePointState,
) (ContractReportSyncPlanItem, contractReportSyncUpsertTarget, bool) {
	item := ContractReportSyncPlanItem{
		Key:              key,
		ServicePointName: row.ServicePointName,
		ServicePointCode: row.ServicePointCode,
		ContractorID:     row.ContractorID,
		ContractType:     row.ContractType,
	}
	target := contractReportSyncUpsertTarget{Row: row, State: state}

	if state == nil {
		item.Action = ServicePointSyncActionCreate
		item.Reason = "точка отсутствует в Bitrix24"
		return item, target, true
	}

	item.Action = ServicePointSyncActionUpdate
	item.B24ElementID = &state.ID
	item.CurrentCode = state.CurrentCode
	item.CurrentContractType = state.CurrentContractType

	needUpdate := false
	if normalizeCell(state.Name) != normalizeCell(row.ServicePointName) {
		needUpdate = true
	}
	if normalizeCell(state.CurrentCode) != normalizeCell(row.ServicePointCode) {
		needUpdate = true
	}
	if normalizeContractType(state.CurrentContractType) != normalizeContractType(row.ContractType) {
		needUpdate = true
	}
	if !needUpdate {
		return ContractReportSyncPlanItem{}, target, false
	}

	item.Reason = "данные в Bitrix24 отличаются от последнего отчета"
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
	oneCMeta *bitrixListFieldMeta,
	contractMeta *bitrixListFieldMeta,
) (map[string]any, error) {
	fields := map[string]any{
		"NAME": target.Row.ServicePointName,
	}
	if target.State != nil {
		for propKey, propValue := range target.State.Properties {
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
