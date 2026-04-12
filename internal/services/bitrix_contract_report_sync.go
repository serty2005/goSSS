package services

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"etalon-server/internal/domain/bitrix"
	contractsvc "etalon-server/internal/services/contract"
)

type ServicePointContractSyncResult struct {
	Processed      int
	Created        int
	Updated        int
	AppliedCreated int
	AppliedUpdated int
	Skipped        int
	DryRun         bool
	Conflicts      []ServicePointContractConflict
	Resolved       []ServicePointContractResolution
}

type ServicePointContractResolution struct {
	B24ElementID     int64
	ServicePointCode string
	ServicePointName string
	ContractOn       bool
	ContractType     string
	StartDate        *time.Time
	EndDate          *time.Time
	ClientOrder      string
}

type ServicePointContractConflict struct {
	ConflictType         string
	ServicePointName     string
	ContractorID         string
	MatchedPointIDs      []int64
	MappedPointIDs       []int64
	DeletionCandidateIDs []int64
}

// SyncServicePointsFromDailyReport применяет ежедневный отчет к точкам обслуживания в Bitrix24.
func (s *bitrixSyncService) SyncServicePointsFromDailyReport(ctx context.Context, rows []contractsvc.ContractReportRow) (*ServicePointContractSyncResult, error) {
	if !s.IsEnabled() {
		return nil, fmt.Errorf("синхронизация с Bitrix24 отключена или не настроена")
	}
	if len(rows) == 0 {
		return &ServicePointContractSyncResult{}, nil
	}
	dryRun := s.isContractReportDryRun()
	s.log.Info("Bitrix24: начата синхронизация точек из ежедневного отчёта", "rows_received", len(rows), "dry_run", dryRun)

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

	result := &ServicePointContractSyncResult{
		Processed: len(rows),
		DryRun:    dryRun,
		Conflicts: make([]ServicePointContractConflict, 0, 8),
		Resolved:  make([]ServicePointContractResolution, 0, len(rows)),
	}

	for _, row := range contractsvc.AggregateContractReportRows(rows) {
		if row.ServicePointCode == "" || row.ServicePointName == "" {
			result.Skipped++
			continue
		}

		if exactMatches := statesByCode[normalizeCell(row.ServicePointCode)]; len(exactMatches) == 1 {
			applied, err := s.applyServicePointReportUpdate(ctx, iblockType, iblockID, exactMatches[0], row, oneCMeta, contractMeta, dryRun)
			if err != nil {
				return nil, err
			}
			result.Updated++
			if applied {
				result.AppliedUpdated++
			}
			result.Resolved = append(result.Resolved, toContractResolution(exactMatches[0].ID, row))
			continue
		}

		nameMatches := statesByName[normalizePointName(row.ServicePointName)]
		switch len(nameMatches) {
		case 0:
			pointID, applied, err := s.applyServicePointReportCreate(ctx, iblockType, iblockID, row, oneCMeta, contractMeta, dryRun)
			if err != nil {
				return nil, err
			}
			result.Created++
			if applied {
				result.AppliedCreated++
				result.Resolved = append(result.Resolved, toContractResolution(pointID, row))
			}
		case 1:
			applied, err := s.applyServicePointReportUpdate(ctx, iblockType, iblockID, nameMatches[0], row, oneCMeta, contractMeta, dryRun)
			if err != nil {
				return nil, err
			}
			result.Updated++
			if applied {
				result.AppliedUpdated++
			}
			result.Resolved = append(result.Resolved, toContractResolution(nameMatches[0].ID, row))
		default:
			conflictIDs := collectAllConflictPointIDs(nameMatches)
			deletionCandidateIDs := collectDeletionCandidatePointIDs(nameMatches, mappedPointIDs)
			if len(deletionCandidateIDs) == 0 {
				result.Skipped++
				continue
			}
			result.Conflicts = append(result.Conflicts, ServicePointContractConflict{
				ConflictType:         "duplicate_name",
				ServicePointName:     row.ServicePointName,
				ContractorID:         row.ContractorID,
				MatchedPointIDs:      conflictIDs,
				MappedPointIDs:       collectMappedPointIDs(nameMatches, mappedPointIDs),
				DeletionCandidateIDs: deletionCandidateIDs,
			})
			result.Skipped++
		}
	}

	result.Conflicts = mergeServicePointConflicts(result.Conflicts, buildDuplicateNameConflicts(statesByName, mappedPointIDs))

	for _, resolved := range result.Resolved {
		contractOn := resolved.ContractOn
		contractType := resolved.ContractType
		clientOrder := resolved.ClientOrder
		if err := s.repo.UpdateServicePointSyncData(ctx, &bitrix.ServicePoint{
			B24ElementID:  resolved.B24ElementID,
			Name:          resolved.ServicePointName,
			OneCCode:      &resolved.ServicePointCode,
			ContractOn:    &contractOn,
			ContractType:  &contractType,
			ContractStart: resolved.StartDate,
			ContractEnd:   resolved.EndDate,
			ClientOrder:   &clientOrder,
		}); err != nil {
			s.log.Warn("не удалось обновить локальные данные точки Bitrix24 после sync отчёта", "point_id", resolved.B24ElementID, "error", err)
		}
	}

	if _, err := s.RefreshServicePoints(ctx); err != nil {
		s.log.Warn("не удалось обновить локальный кэш точек Bitrix после обработки ежедневного отчёта", "error", err)
	}
	s.log.Info(
		"Bitrix24: синхронизация точек из ежедневного отчёта завершена",
		"rows_processed", result.Processed,
		"planned_created_points", result.Created,
		"planned_updated_points", result.Updated,
		"applied_created_points", result.AppliedCreated,
		"applied_updated_points", result.AppliedUpdated,
		"uploaded_to_bitrix", result.AppliedCreated+result.AppliedUpdated,
		"skipped_rows", result.Skipped,
		"conflicts_count", len(result.Conflicts),
		"deletion_candidates", countDeletionCandidates(result.Conflicts),
		"resolved_points", len(result.Resolved),
		"dry_run", result.DryRun,
	)

	return result, nil
}

// buildStatesByCode строит быстрый индекс точек Bitrix24 по идентификатору контрагента.
func buildStatesByCode(statesByName map[string][]bitrixServicePointState) map[string][]bitrixServicePointState {
	statesByCode := make(map[string][]bitrixServicePointState)
	for _, items := range statesByName {
		for _, item := range items {
			for _, code := range servicePointCodeLookupKeys(item.CurrentCode) {
				if code == "" {
					continue
				}
				statesByCode[code] = append(statesByCode[code], item)
			}
		}
	}
	return statesByCode
}

// collectAllConflictPointIDs возвращает ID всех точек в группе дублей по имени.
func collectAllConflictPointIDs(items []bitrixServicePointState) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

// collectMappedPointIDs возвращает ID точек, уже защищенных существующим mapping компании.
func collectMappedPointIDs(items []bitrixServicePointState, mappedPointIDs map[int64]struct{}) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		if _, mapped := mappedPointIDs[item.ID]; !mapped {
			continue
		}
		ids = append(ids, item.ID)
	}
	return ids
}

// collectDeletionCandidatePointIDs возвращает только те дубли, которые можно вынести оператору на удаление.
func collectDeletionCandidatePointIDs(items []bitrixServicePointState, mappedPointIDs map[int64]struct{}) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		if _, mapped := mappedPointIDs[item.ID]; mapped {
			continue
		}
		ids = append(ids, item.ID)
	}
	return ids
}

// buildDuplicateNameConflicts находит полные группы дублей по имени среди всех точек Bitrix24.
func buildDuplicateNameConflicts(
	statesByName map[string][]bitrixServicePointState,
	mappedPointIDs map[int64]struct{},
) []ServicePointContractConflict {
	conflicts := make([]ServicePointContractConflict, 0, 8)
	for _, items := range statesByName {
		if len(items) < 2 {
			continue
		}

		deletionCandidateIDs := collectDeletionCandidatePointIDs(items, mappedPointIDs)
		if len(deletionCandidateIDs) == 0 {
			continue
		}

		conflicts = append(conflicts, ServicePointContractConflict{
			ConflictType:         "duplicate_name",
			ServicePointName:     items[0].Name,
			MatchedPointIDs:      collectAllConflictPointIDs(items),
			MappedPointIDs:       collectMappedPointIDs(items, mappedPointIDs),
			DeletionCandidateIDs: deletionCandidateIDs,
		})
	}
	return conflicts
}

// mergeServicePointConflicts объединяет конфликты по нормализованному имени, чтобы не дублировать одну группу.
func mergeServicePointConflicts(base []ServicePointContractConflict, extra []ServicePointContractConflict) []ServicePointContractConflict {
	if len(extra) == 0 {
		return base
	}

	indexByName := make(map[string]int, len(base))
	for i, item := range base {
		indexByName[normalizePointName(item.ServicePointName)] = i
	}

	for _, item := range extra {
		key := normalizePointName(item.ServicePointName)
		if idx, ok := indexByName[key]; ok {
			base[idx] = mergeSingleServicePointConflict(base[idx], item)
			continue
		}
		indexByName[key] = len(base)
		base = append(base, item)
	}

	return base
}

// mergeSingleServicePointConflict сливает данные о дублях по одной точке обслуживания.
func mergeSingleServicePointConflict(left ServicePointContractConflict, right ServicePointContractConflict) ServicePointContractConflict {
	if strings.TrimSpace(left.ServicePointName) == "" {
		left.ServicePointName = right.ServicePointName
	}
	if strings.TrimSpace(left.ContractorID) == "" {
		left.ContractorID = right.ContractorID
	}
	left.MatchedPointIDs = mergeInt64IDs(left.MatchedPointIDs, right.MatchedPointIDs)
	left.MappedPointIDs = mergeInt64IDs(left.MappedPointIDs, right.MappedPointIDs)
	left.DeletionCandidateIDs = mergeInt64IDs(left.DeletionCandidateIDs, right.DeletionCandidateIDs)
	return left
}

// mergeInt64IDs объединяет и сортирует ID без дубликатов.
func mergeInt64IDs(left []int64, right []int64) []int64 {
	if len(left) == 0 && len(right) == 0 {
		return nil
	}

	seen := make(map[int64]struct{}, len(left)+len(right))
	result := make([]int64, 0, len(left)+len(right))
	for _, id := range left {
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	for _, id := range right {
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	slices.Sort(result)
	return result
}

// countDeletionCandidates считает количество точек, попавших в операторский список удаления.
func countDeletionCandidates(conflicts []ServicePointContractConflict) int {
	total := 0
	for _, conflict := range conflicts {
		total += len(conflict.DeletionCandidateIDs)
	}
	return total
}

// applyServicePointReportCreate создает новую точку обслуживания в Bitrix24 по строке отчета.
func (s *bitrixSyncService) applyServicePointReportCreate(
	ctx context.Context,
	iblockType string,
	iblockID int,
	row contractsvc.ContractReportRow,
	oneCMeta *bitrixListFieldMeta,
	contractMeta *bitrixListFieldMeta,
	dryRun bool,
) (int64, bool, error) {
	contractValue, err := prepareContractFieldValue(contractMeta, row.ContractType)
	if err != nil {
		return 0, false, err
	}
	fields := map[string]any{
		"NAME":                             row.ServicePointName,
		bitrixServicePointOneCCodeProperty: prepareBitrixFieldValue(oneCMeta, row.ServicePointCode),
		bitrixServicePointContractProperty: contractValue,
	}

	if dryRun {
		s.log.Info(
			"Bitrix24 dry-run: пропущено создание точки обслуживания",
			"service_point_name", row.ServicePointName,
			"service_point_code", row.ServicePointCode,
			"iblock_type", iblockType,
			"iblock_id", iblockID,
			"fields", fields,
		)
		return 0, false, nil
	}

	elementCode := fmt.Sprintf("autogen_%d", time.Now().UnixNano())
	pointID, err := s.client.ListsElementAdd(ctx, iblockType, iblockID, elementCode, fields)
	if err != nil {
		return 0, false, fmt.Errorf("не удалось создать точку %q в Bitrix24: %w", row.ServicePointName, err)
	}
	return pointID, true, nil
}

// applyServicePointReportUpdate полностью обновляет существующую точку обслуживания в Bitrix24.
func (s *bitrixSyncService) applyServicePointReportUpdate(
	ctx context.Context,
	iblockType string,
	iblockID int,
	state bitrixServicePointState,
	row contractsvc.ContractReportRow,
	oneCMeta *bitrixListFieldMeta,
	contractMeta *bitrixListFieldMeta,
	dryRun bool,
) (bool, error) {
	contractValue, err := prepareContractFieldValue(contractMeta, row.ContractType)
	if err != nil {
		return false, err
	}

	fields := map[string]any{
		"NAME": row.ServicePointName,
	}
	for propKey, propValue := range state.Properties {
		fields[propKey] = normalizePropertyValueForWrite(propValue)
	}
	fields[bitrixServicePointOneCCodeProperty] = prepareBitrixFieldValue(oneCMeta, row.ServicePointCode)
	fields[bitrixServicePointContractProperty] = contractValue

	if dryRun {
		s.log.Info(
			"Bitrix24 dry-run: пропущено обновление точки обслуживания",
			"point_id", state.ID,
			"service_point_name", row.ServicePointName,
			"service_point_code", row.ServicePointCode,
			"iblock_type", iblockType,
			"iblock_id", iblockID,
			"fields", fields,
		)
		return false, nil
	}

	if err := s.client.ListsElementUpdate(ctx, iblockType, iblockID, state.ID, fields); err != nil {
		return false, fmt.Errorf("не удалось обновить точку %q в Bitrix24: %w", row.ServicePointName, err)
	}
	return true, nil
}

// toContractResolution превращает строку отчета в упрощенную запись для следующего этапа sync.
func toContractResolution(pointID int64, row contractsvc.ContractReportRow) ServicePointContractResolution {
	return ServicePointContractResolution{
		B24ElementID:     pointID,
		ServicePointCode: row.ServicePointCode,
		ServicePointName: row.ServicePointName,
		ContractOn:       row.ContractOn,
		ContractType:     row.ContractType,
		StartDate:        row.StartDate,
		EndDate:          row.EndDate,
		ClientOrder:      row.ClientOrder,
	}
}
