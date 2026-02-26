package services

import (
	"context"
)

// OwnerMatch представляет найденное совпадение при поиске владельца.
// Используется для отслеживания источника определения владельца.
type OwnerMatch struct {
	// OwnerID — ID компании-владельца
	OwnerID string
	// EntityType — тип сущности: "Workstation" или "FiscalRegister"
	EntityType string
	// EntityID — ID сущности (РС или ФР)
	EntityID string
	// MatchBy — способ поиска: "teamviewer", "litemanager", "anydesk", "serial"
	MatchBy string
	// MatchValue — значение, по которому найдено совпадение
	MatchValue string
}

// OwnerResolution результат разрешения владельца для network-hub сервера.
// Содержит информацию о найденных совпадениях и конфликтах.
type OwnerResolution struct {
	// OwnerID — определённый владелец (пустой если не определён)
	OwnerID string
	// Confident — уверенное определение (один владелец, нет конфликта)
	Confident bool
	// WSMatch — совпадение по рабочей станции (если есть)
	WSMatch *OwnerMatch
	// FRMatch — совпадение по фискальному регистратору (если есть)
	FRMatch *OwnerMatch
	// HasConflict — конфликт: РС и ФР найдены у разных владельцев
	HasConflict bool
	// ConflictInfo — описание конфликта для отображения оператору
	ConflictInfo string
	// WSOwnerCandidates — все кандидаты-владельцы по РС (для информации)
	WSOwnerCandidates []string
	// FROwnerCandidates — все кандидаты-владельцы по ФР (для информации)
	FROwnerCandidates []string
}

// OwnerResolver интерфейс для определения владельца сущностей
// при обработке данных от агентов на network-hub серверах.
//
// Алгоритм разрешения:
//  1. Поиск РС по remote IDs (TeamViewer, LiteManager, AnyDesk) среди дочерних компаний
//  2. Поиск ФР по serial number среди дочерних компаний
//  3. Если найдены РС и ФР у одного владельца — уверенное определение
//  4. Если РС у одного владельца, ФР у другого — конфликт
//  5. Если нет совпадений или несколько кандидатов — не определено
type OwnerResolver interface {
	// Resolve определяет владельца для данных наблюдения.
	//
	// Параметры:
	//   - ctx: контекст для отмены операции
	//   - hubCompanyID: ID hub-компании (владелец сервера)
	//   - teamviewerID: TeamViewer ID для поиска РС
	//   - litemanagerID: LiteManager ID для поиска РС
	//   - anydeskID: AnyDesk ID для поиска РС
	//   - serialNumber: серийный номер ФР для поиска
	//
	// Возвращает:
	//   - *OwnerResolution: результат разрешения
	//   - error: ошибка БД
	Resolve(ctx context.Context, hubCompanyID string, teamviewerID, litemanagerID, rustdeskID, anydeskID, serialNumber string) (*OwnerResolution, error)
}
