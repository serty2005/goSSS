package services

import (
	"context"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/services"
	"etalon-server/internal/domain/workstation"
	"etalon-server/internal/infra/logger"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// OwnerResolverService реализует логику определения владельца
// для network-hub серверов.
//
// Алгоритм работы:
//  1. Получение списка дочерних компаний hub-компании
//  2. Поиск существующей РС по remote IDs среди дочерних компаний
//  3. Поиск существующего ФР по serial number среди дочерних компаний
//  4. Анализ результатов:
//     - РС и ФР у одного владельца → уверенное определение
//     - РС у одного владельца, ФР у другого → конфликт
//     - Нет совпадений → не определено
type OwnerResolverService struct {
	logger        logger.LoggerInterface
	db            *gorm.DB
	companyRepo   company.Repository
	workstationWS workstation.Repository
	fiscalRepo    fiscal.Repository
}

// NewOwnerResolverService создаёт новый экземпляр сервиса определения владельца.
func NewOwnerResolverService(
	logger logger.LoggerInterface,
	db *gorm.DB,
	companyRepo company.Repository,
	workstationWS workstation.Repository,
	fiscalRepo fiscal.Repository,
) *OwnerResolverService {
	return &OwnerResolverService{
		logger:        logger,
		db:            db,
		companyRepo:   companyRepo,
		workstationWS: workstationWS,
		fiscalRepo:    fiscalRepo,
	}
}

// Resolve определяет владельца для данных наблюдения на network-hub сервере.
//
// Параметры:
//   - ctx: контекст для отмены операции
//   - hubCompanyID: ID hub-компании (владелец сервера)
//   - teamviewerID: TeamViewer ID для поиска РС
//   - litemanagerID: LiteManager ID для поиска РС
//   - anydeskID: AnyDesk ID для поиска РС
//   - serialNumber: серийный номер ФР для поиска
func (s *OwnerResolverService) Resolve(
	ctx context.Context,
	hubCompanyID string,
	teamviewerID, litemanagerID, anydeskID, serialNumber string,
) (*services.OwnerResolution, error) {
	hubCompanyID = strings.TrimSpace(hubCompanyID)
	if hubCompanyID == "" {
		return &services.OwnerResolution{Confident: false}, nil
	}

	// Нормализация входных данных
	teamviewerID = normRemoteID(teamviewerID)
	litemanagerID = normRemoteID(litemanagerID)
	anydeskID = normRemoteID(anydeskID)
	serialNumber = normalizeSerial(serialNumber)

	s.logger.Debug("OwnerResolver: начало разрешения владельца",
		"hub_company_id", hubCompanyID,
		"teamviewer_id", teamviewerID,
		"litemanager_id", litemanagerID,
		"anydesk_id", anydeskID,
		"serial_number", serialNumber,
	)

	// Получение дочерних компаний hub-компании
	children, err := s.companyRepo.GetChildren(ctx, hubCompanyID)
	if err != nil {
		return nil, fmt.Errorf("ошибка получения дочерних компаний: %w", err)
	}

	if len(children) == 0 {
		s.logger.Debug("OwnerResolver: у hub-компании нет дочерних компаний",
			"hub_company_id", hubCompanyID,
		)
		return &services.OwnerResolution{Confident: false}, nil
	}

	childIDs := make([]string, 0, len(children))
	childNames := make(map[string]string)
	for _, child := range children {
		childIDs = append(childIDs, child.ID)
		if child.Title != nil {
			childNames[child.ID] = *child.Title
		}
	}

	s.logger.Debug("OwnerResolver: поиск среди дочерних компаний",
		"children_count", len(children),
		"child_ids", childIDs,
	)

	// Поиск РС по remote IDs среди дочерних компаний
	wsMatch := s.findWorkstationAmongChildren(ctx, childIDs, teamviewerID, litemanagerID, anydeskID)

	// Поиск ФР по serial среди дочерних компаний
	frMatch := s.findFiscalAmongChildren(ctx, childIDs, serialNumber)

	// Анализ результатов
	result := &services.OwnerResolution{
		WSMatch: wsMatch,
		FRMatch: frMatch,
	}

	// Нет совпадений
	if wsMatch == nil && frMatch == nil {
		s.logger.Debug("OwnerResolver: совпадения не найдены",
			"hub_company_id", hubCompanyID,
		)
		return result, nil
	}

	// Только РС найдена
	if wsMatch != nil && frMatch == nil {
		result.OwnerID = wsMatch.OwnerID
		result.Confident = true
		s.logger.Info("OwnerResolver: владелец определён по РС",
			"hub_company_id", hubCompanyID,
			"owner_id", result.OwnerID,
			"match_by", wsMatch.MatchBy,
			"match_value", wsMatch.MatchValue,
		)
		return result, nil
	}

	// Только ФР найден
	if wsMatch == nil && frMatch != nil {
		result.OwnerID = frMatch.OwnerID
		result.Confident = true
		s.logger.Info("OwnerResolver: владелец определён по ФР",
			"hub_company_id", hubCompanyID,
			"owner_id", result.OwnerID,
			"match_by", frMatch.MatchBy,
			"match_value", frMatch.MatchValue,
		)
		return result, nil
	}

	// РС и ФР найдены — проверяем конфликт
	if wsMatch.OwnerID == frMatch.OwnerID {
		// Один владелец — уверенное определение
		result.OwnerID = wsMatch.OwnerID
		result.Confident = true
		s.logger.Info("OwnerResolver: владелец определён по РС и ФР",
			"hub_company_id", hubCompanyID,
			"owner_id", result.OwnerID,
			"ws_match_by", wsMatch.MatchBy,
			"fr_match_by", frMatch.MatchBy,
		)
		return result, nil
	}

	// Конфликт: РС у одного владельца, ФР у другого
	result.HasConflict = true
	wsOwnerName := childNames[wsMatch.OwnerID]
	frOwnerName := childNames[frMatch.OwnerID]
	result.ConflictInfo = fmt.Sprintf(
		"Конфликт владельцев: РС найдена у компании \"%s\" (по %s), ФР найден у компании \"%s\" (по serial)",
		wsOwnerName, wsMatch.MatchBy,
		frOwnerName,
	)
	result.WSOwnerCandidates = []string{wsMatch.OwnerID}
	result.FROwnerCandidates = []string{frMatch.OwnerID}
	result.Confident = false

	s.logger.Warn("OwnerResolver: обнаружен конфликт владельцев",
		"hub_company_id", hubCompanyID,
		"ws_owner_id", wsMatch.OwnerID,
		"ws_owner_name", wsOwnerName,
		"fr_owner_id", frMatch.OwnerID,
		"fr_owner_name", frOwnerName,
	)

	return result, nil
}

// findWorkstationAmongChildren ищет рабочую станцию по remote IDs среди дочерних компаний.
// Возвращает первое найденное совпадение.
func (s *OwnerResolverService) findWorkstationAmongChildren(
	ctx context.Context,
	childIDs []string,
	teamviewerID, litemanagerID, anydeskID string,
) *services.OwnerMatch {
	// Поиск по TeamViewer
	if teamviewerID != "" {
		var ws workstation.Workstation
		err := s.db.WithContext(ctx).
			Where("teamviewer = ? AND owner_id IN ?", teamviewerID, childIDs).
			First(&ws).Error
		if err == nil && ws.OwnerID != nil {
			return &services.OwnerMatch{
				OwnerID:    *ws.OwnerID,
				EntityType: "Workstation",
				EntityID:   ws.ID,
				MatchBy:    "teamviewer",
				MatchValue: teamviewerID,
			}
		}
	}

	// Поиск по LiteManager
	if litemanagerID != "" {
		var ws workstation.Workstation
		err := s.db.WithContext(ctx).
			Where("litemanager = ? AND owner_id IN ?", litemanagerID, childIDs).
			First(&ws).Error
		if err == nil && ws.OwnerID != nil {
			return &services.OwnerMatch{
				OwnerID:    *ws.OwnerID,
				EntityType: "Workstation",
				EntityID:   ws.ID,
				MatchBy:    "litemanager",
				MatchValue: litemanagerID,
			}
		}
	}

	// Поиск по AnyDesk
	if anydeskID != "" {
		var ws workstation.Workstation
		err := s.db.WithContext(ctx).
			Where("anydesk = ? AND owner_id IN ?", anydeskID, childIDs).
			First(&ws).Error
		if err == nil && ws.OwnerID != nil {
			return &services.OwnerMatch{
				OwnerID:    *ws.OwnerID,
				EntityType: "Workstation",
				EntityID:   ws.ID,
				MatchBy:    "anydesk",
				MatchValue: anydeskID,
			}
		}
	}

	return nil
}

// findFiscalAmongChildren ищет фискальный регистратор по serial number среди дочерних компаний.
func (s *OwnerResolverService) findFiscalAmongChildren(
	ctx context.Context,
	childIDs []string,
	serialNumber string,
) *services.OwnerMatch {
	if serialNumber == "" {
		return nil
	}

	var fr fiscal.FiscalRegister
	err := s.db.WithContext(ctx).
		Where("fr_serial_normalized = ? AND owner_id IN ?", serialNumber, childIDs).
		First(&fr).Error
	if err == nil && fr.OwnerID != nil {
		return &services.OwnerMatch{
			OwnerID:    *fr.OwnerID,
			EntityType: "FiscalRegister",
			EntityID:   fr.ID,
			MatchBy:    "serial",
			MatchValue: serialNumber,
		}
	}

	return nil
}

// normRemoteID нормализует remote ID (TeamViewer, LiteManager, AnyDesk).
// Удаляет пробелы и игнорирует значение "none".
func normRemoteID(v string) string {
	v = strings.TrimSpace(v)
	if strings.EqualFold(v, "none") || v == "" {
		return ""
	}
	return strings.ReplaceAll(v, " ", "")
}

// normalizeSerial нормализует серийный номер ФР.
// Приводит к верхнему регистру и удаляет пробелы.
func normalizeSerial(v string) string {
	v = strings.ToUpper(strings.TrimSpace(v))
	return strings.ReplaceAll(v, " ", "")
}

// Убеждаемся, что OwnerResolverService реализует интерфейс OwnerResolver
var _ services.OwnerResolver = (*OwnerResolverService)(nil)

// GetOwnerResolutionSource возвращает источник определения владельца для истории.
func GetOwnerResolutionSource(resolution *services.OwnerResolution) string {
	if resolution == nil {
		return ""
	}
	if resolution.HasConflict {
		return models.OwnerChangeSourceNetworkConflict
	}
	if resolution.WSMatch != nil && resolution.FRMatch != nil {
		return models.OwnerChangeSourceNetworkAutoBoth
	}
	if resolution.WSMatch != nil {
		return models.OwnerChangeSourceNetworkAutoWS
	}
	if resolution.FRMatch != nil {
		return models.OwnerChangeSourceNetworkAutoFR
	}
	return models.OwnerChangeSourceNetworkAuto
}
