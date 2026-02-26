// Package models содержит доменные модели данных для системы Etalon-Server.
// Файл agent_observation_models.go определяет структуры для обработки наблюдений агентов.
package models

import (
	"time"

	"gorm.io/datatypes"
)

// ===========================================================================
// Константы статусов наблюдений
// ===========================================================================

// Константы статусов AgentObservation.
// Статусы определяют этап жизненного цикла обработки данных от агента.
const (
	// AgentObservationStatusProcessing — наблюдение находится в процессе обработки.
	// Начальный статус после регистрации данных от агента.
	AgentObservationStatusProcessing = "PROCESSING"

	// AgentObservationStatusApplied — наблюдение успешно применено.
	// Данные были использованы для создания или обновления сущностей (РС, ФР).
	AgentObservationStatusApplied = "APPLIED"

	// AgentObservationStatusStaged — наблюдение отправлено в кандидаты.
	// Используется, когда сервер не найден и требуется ручное подтверждение.
	AgentObservationStatusStaged = "STAGED"

	// AgentObservationStatusIgnored — наблюдение отклонено.
	// Причина: локальный IP-адрес, отсутствие полезных данных или другие критерии фильтрации.
	AgentObservationStatusIgnored = "IGNORED"

	// AgentObservationStatusIgnoredStale — наблюдение отклонено как устаревшее.
	// Причина: получены более свежие данные от того же агента.
	AgentObservationStatusIgnoredStale = "IGNORED_STALE"

	// AgentObservationStatusError — ошибка при обработке наблюдения.
	// Детали ошибки сохраняются в поле ErrorText.
	AgentObservationStatusError = "ERROR"
)

// Константы статусов Candidate.
// Определяют этап жизненного цикла кандидата на подключение к системе.
const (
	// CandidateStatusNew — новый кандидат, ожидает обработки.
	// Начальный статус при создании кандидата из наблюдения.
	CandidateStatusNew = "NEW"

	// CandidateStatusInReview — кандидат на рассмотрении.
	// Оператор начал работу с кандидатом, но решение ещё не принято.
	CandidateStatusInReview = "IN_REVIEW"

	// CandidateStatusApproved — кандидат одобрен и подключён.
	// Сущности (сервер, РС, ФР) созданы и привязаны к компании.
	CandidateStatusApproved = "APPROVED"

	// CandidateStatusRejected — кандидат отклонён.
	// Оператор отказал в подключении, данные не применены.
	CandidateStatusRejected = "REJECTED"

	// CandidateStatusCancelled — кандидат отменён.
	// Автоматическая отмена при обнаружении дубликата или других условиях.
	CandidateStatusCancelled = "CANCELLED"
)

// ===========================================================================
// AgentObservation — запись о наблюдении агента
// ===========================================================================

// AgentObservation представляет единичное наблюдение от агента мониторинга.
//
// Назначение:
//   - Хранение истории всех полученных данных от агентов
//   - Обеспечение идемпотентности обработки (через PayloadHash)
//   - Связывание входящих данных с созданными/найденными сущностями
//
// Жизненный цикл:
//  1. Создаётся при получении данных от агента (status=PROCESSING)
//  2. Обрабатывается в ApplyObservation() с поиском/созданием сущностей
//  3. Финальный статус: APPLIED, STAGED, IGNORED, IGNORED_STALE или ERROR
//
// Связи:
//   - Может ссылаться на Workstation (если найдена/создана)
//   - Может ссылаться на Candidate (если создан кандидат)
//   - Может ссылаться на NetworkCandidate (для network-hub серверов)
//   - Может ссылаться на FiscalRegister (если найден/создан)
//
// Пример использования:
//
//	obs := &AgentObservation{
//	    Source:      "12345.json",
//	    ObservedAt:  time.Now(),
//	    ServerKey:   &serverKey,
//	    PayloadJSON: payload,
//	    Status:      AgentObservationStatusProcessing,
//	}
type AgentObservation struct {
	// ID — первичный ключ записи.
	// Генерируется автоматически при создании.
	ID uint `gorm:"primaryKey" json:"id"`

	// Source — источник данных.
	// Формат: имя файла (например, "12345.json") или UUID агента для HTTP API.
	// Используется для трассировки и логирования.
	Source string `gorm:"type:text;index" json:"source"`

	// ObservedAt — время наблюдения на агенте.
	// Источник: поле current_time из AgentDataDTO.
	// Используется для определения актуальности данных и защиты от устаревших наблюдений.
	ObservedAt time.Time `gorm:"index" json:"observed_at"`

	// ServerKey — уникальный ключ сервера.
	// Формат: UUID, вычисленный на основе URL/IP сервера.
	// Источник: преобразуется из url_rms AgentDataDTO.
	// Может быть nil, если сервер не определён.
	ServerKey *string `gorm:"type:text;index" json:"server_key"`

	// ServerCRMID — идентификатор сервера в CRM-системе.
	// Источник: поле crm_id из AgentDataDTO.
	// Используется для поиска существующего сервера при обработке.
	ServerCRMID *string `gorm:"column:server_crm_id;type:text;index" json:"server_crm_id"`

	// PayloadJSON — полные данные наблюдения в формате JSON.
	// Содержит весь payload, полученный от агента.
	// Используется для аудита и повторной обработки при необходимости.
	PayloadJSON datatypes.JSON `gorm:"type:jsonb" json:"payload_json"`

	// PayloadHash — хеш данных для идемпотентности.
	// Формат: SHA256 от сериализованного payload.
	// Уникальный индекс обеспечивает защиту от повторной обработки одинаковых данных.
	PayloadHash string `gorm:"type:char(64);uniqueIndex" json:"payload_hash"`

	// Status — текущий статус обработки наблюдения.
	// Возможные значения: PROCESSING, APPLIED, STAGED, IGNORED, IGNORED_STALE, ERROR.
	// Индекс используется для выборки наблюдений по статусу.
	Status string `gorm:"type:varchar(32);index" json:"status"`

	// ErrorText — текст ошибки при обработке.
	// Заполняется только при статусе ERROR.
	// Содержит детальную информацию об ошибке для диагностики.
	ErrorText *string `gorm:"type:text" json:"error_text"`

	// WorkstationID — идентификатор найденной или созданной рабочей станции.
	// Заполняется после успешного поиска/создания РС.
	// Формат: UUID рабочей станции.
	WorkstationID *string `gorm:"type:text;index" json:"workstation_id"`

	// CandidateID — идентификатор созданного кандидата.
	// Заполняется, если сервер не найден и создан кандидат для ручного подтверждения.
	CandidateID *uint `gorm:"index" json:"candidate_id"`

	// NetworkCandidateID — идентификатор network-кандидата.
	// Заполняется для network-hub серверов, когда невозможно автоматически определить владельца.
	NetworkCandidateID *uint `gorm:"index" json:"network_candidate_id"`

	// FRID — идентификатор найденного или созданного фискального регистратора.
	// Заполняется после успешного поиска/создания ФР.
	// Формат: серийный номер ФР (нормализованный).
	FRID *string `gorm:"type:text;index" json:"fr_id"`

	// CreatedAt — время создания записи.
	// Автоматически устанавливается GORM при создании.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt — время последнего обновления записи.
	// Автоматически обновляется GORM при изменении.
	UpdatedAt time.Time `json:"updated_at"`
}

// ===========================================================================
// Candidate — кандидат на подключение
// ===========================================================================

// Candidate представляет кандидата на подключение к системе.
//
// Назначение:
//   - Хранение данных о потенциальных серверах/РС/ФР, требующих ручного подтверждения
//   - Связывание staging-данных с процессом принятия решения
//
// Условия создания:
//   - Сервер не найден по CRM ID или server_key
//   - Отсутствуют remote IDs для идентификации рабочей станции
//
// Связи:
//   - CandidateWorkstationStaging — данные РС для подтверждения
//   - CandidateFiscalStaging — данные ФР для подтверждения
//   - Ticket — заявка в ServiceDesk (опционально)
//
// Пример использования:
//
//	candidate := &Candidate{
//	    ServerKey:   &serverKey,
//	    ServerCRMID: &crmID,
//	    Status:      CandidateStatusNew,
//	}
type Candidate struct {
	// ID — первичный ключ записи.
	// Генерируется автоматически при создании.
	ID uint `gorm:"primaryKey" json:"id"`

	// ServerKey — уникальный ключ сервера.
	// Формат: UUID, вычисленный на основе URL/IP сервера.
	// Может быть nil, если сервер не определён.
	ServerKey *string `gorm:"type:text;index" json:"server_key"`

	// ServerCRMID — идентификатор сервера в CRM-системе.
	// Источник: поле crm_id из AgentDataDTO.
	ServerCRMID *string `gorm:"column:server_crm_id;type:text;index" json:"server_crm_id"`

	// ServerURL — URL или IP-адрес сервера.
	// Источник: поле url_rms из AgentDataDTO.
	// Используется для отображения в UI и поиска существующих серверов.
	ServerURL *string `gorm:"type:text" json:"server_url"`

	// Status — текущий статус кандидата.
	// Возможные значения: NEW, IN_REVIEW, APPROVED, REJECTED, CANCELLED.
	// Индекс используется для фильтрации кандидатов по статусу.
	Status string `gorm:"type:varchar(32);index" json:"status"`

	// TicketID — идентификатор заявки в ServiceDesk.
	// Заполняется при создании заявки для согласования кандидата.
	TicketID *uint `gorm:"index" json:"ticket_id"`

	// Meta — дополнительные метаданные кандидата в формате JSON.
	// Содержит информацию, не вошедшую в основные поля.
	Meta datatypes.JSON `gorm:"type:jsonb" json:"meta"`

	// ExistingServerID — идентификатор существующего сервера.
	// Заполняется, если найден похожий сервер в системе.
	// Используется для отображения возможных дубликатов.
	ExistingServerID *string `gorm:"type:text;index" json:"existing_server_id"`

	// ApprovedCompanyID — идентификатор компании после подтверждения.
	// Заполняется при одобрении кандидата (status=APPROVED).
	// Определяет, к какой компании привязаны созданные сущности.
	ApprovedCompanyID *string `gorm:"type:text;index" json:"approved_company_id"`

	// ApprovedServerID — идентификатор сервера после подтверждения.
	// Заполняется при одобрении кандидата (status=APPROVED).
	ApprovedServerID *string `gorm:"type:text;index" json:"approved_server_id"`

	// CreatedAt — время создания записи.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt — время последнего обновления записи.
	UpdatedAt time.Time `json:"updated_at"`
}

// ===========================================================================
// CandidateStatusHistory — история изменений статуса кандидата
// ===========================================================================

// CandidateStatusHistory представляет запись об изменении статуса кандидата.
//
// Назначение:
//   - Аудит всех изменений статуса кандидата
//   - Отслеживание истории принятия решений
//
// Создаётся автоматически при каждом изменении статуса Candidate.
type CandidateStatusHistory struct {
	// ID — первичный ключ записи.
	ID uint `gorm:"primaryKey" json:"id"`

	// CandidateID — идентификатор кандидата.
	// Внешний ключ на Candidate.ID.
	CandidateID uint `gorm:"index;not null" json:"candidate_id"`

	// FromStatus — предыдущий статус кандидата.
	// Может быть nil при первичном создании кандидата.
	FromStatus *string `gorm:"type:varchar(32)" json:"from_status"`

	// ToStatus — новый статус кандидата.
	// Обязательное поле, содержит целевой статус.
	ToStatus string `gorm:"type:varchar(32);index" json:"to_status"`

	// Reason — причина изменения статуса.
	// Содержит комментарий оператора или системное сообщение.
	Reason *string `gorm:"type:text" json:"reason"`

	// CreatedAt — время создания записи.
	// Индекс используется для сортировки по хронологии.
	CreatedAt time.Time `gorm:"index" json:"created_at"`
}

// ===========================================================================
// CandidateWorkstationStaging — staging-данные рабочей станции
// ===========================================================================

// CandidateWorkstationStaging содержит данные рабочей станции для подтверждения.
//
// Назначение:
//   - Временное хранение данных РС до подтверждения кандидата
//   - Отображение информации о РС в UI принятия решений
//
// Источник данных:
//   - Поля AgentDataDTO: hostname, teamviewer_id, litemanager_id, anydesk_id, agent_uuid
//
// Связи:
//   - Candidate — родительская запись кандидата
//   - AgentObservation — исходное наблюдение
//
// После подтверждения кандидата данные используются для создания Workstation.
type CandidateWorkstationStaging struct {
	// ID — первичный ключ записи.
	ID uint `gorm:"primaryKey" json:"id"`

	// CandidateID — идентификатор кандидата.
	// Внешний ключ на Candidate.ID.
	CandidateID uint `gorm:"index;not null" json:"candidate_id"`

	// ObservationID — идентификатор исходного наблюдения.
	// Внешний ключ на AgentObservation.ID.
	ObservationID uint `gorm:"index;not null" json:"observation_id"`

	// ObservedAt — время наблюдения.
	// Используется для определения актуальности данных.
	ObservedAt time.Time `gorm:"index" json:"observed_at"`

	// Hostname — имя хоста рабочей станции.
	// Источник: поле hostname из AgentDataDTO.
	Hostname *string `gorm:"type:text" json:"hostname"`

	// AgentUUID — уникальный идентификатор агента.
	// Источник: поле agent_uuid из AgentDataDTO.
	// Используется для идентификации источника данных.
	AgentUUID *string `gorm:"type:text;index" json:"agent_uuid"`

	// WorkstationUUID — уникальный идентификатор рабочей станции.
	// Вычисляется на основе remote IDs (TV:LM).
	// Используется для проверки на дубликаты.
	WorkstationUUID *string `gorm:"type:text" json:"workstation_uuid"`

	// TeamviewerID — идентификатор TeamViewer.
	// Источник: поле teamviewer_id из AgentDataDTO.
	// Один из ключевых идентификаторов для поиска существующей РС.
	TeamviewerID *string `gorm:"type:text;index" json:"teamviewer_id"`

	// LitemanagerID — идентификатор LiteManager.
	// Источник: поле litemanager_id из AgentDataDTO.
	// Один из ключевых идентификаторов для поиска существующей РС.
	LitemanagerID *string `gorm:"type:text;index" json:"litemanager_id"`

	// RustdeskID — идентификатор RustDesk.
	// Источник: поле rustdesk_id из AgentDataDTO.
	// Один из ключевых идентификаторов для поиска существующей РС.
	RustdeskID *string `gorm:"type:text;index" json:"rustdesk_id"`

	// AnydeskID — идентификатор AnyDesk.
	// Источник: поле anydesk_id из AgentDataDTO.
	// Один из ключевых идентификаторов для поиска существующей РС.
	AnydeskID *string `gorm:"type:text;index" json:"anydesk_id"`

	// URLRms — URL или IP-адрес RMS-сервера.
	// Источник: поле url_rms из AgentDataDTO.
	// Используется для связи с сервером.
	URLRms *string `gorm:"column:url_rms;type:text" json:"url_rms"`

	// CreatedAt — время создания записи.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt — время последнего обновления записи.
	UpdatedAt time.Time `json:"updated_at"`
}

// ===========================================================================
// CandidateFiscalStaging — staging-данные фискального регистратора
// ===========================================================================

// CandidateFiscalStaging содержит данные фискального регистратора для подтверждения.
//
// Назначение:
//   - Временное хранение данных ФР до подтверждения кандидата
//   - Отображение информации о ФР в UI принятия решений
//
// Источник данных:
//   - Поля AgentDataDTO: serial_number, rn_kkt, model_name, inn, fn_number и др.
//
// Связи:
//   - Candidate — родительская запись кандидата
//   - AgentObservation — исходное наблюдение
//
// После подтверждения кандидата данные используются для создания FiscalRegister.
type CandidateFiscalStaging struct {
	// ID — первичный ключ записи.
	ID uint `gorm:"primaryKey" json:"id"`

	// CandidateID — идентификатор кандидата.
	// Внешний ключ на Candidate.ID.
	CandidateID uint `gorm:"index;not null" json:"candidate_id"`

	// ObservationID — идентификатор исходного наблюдения.
	// Внешний ключ на AgentObservation.ID.
	ObservationID uint `gorm:"index;not null" json:"observation_id"`

	// ObservedAt — время наблюдения.
	// Используется для определения актуальности данных.
	ObservedAt time.Time `gorm:"index" json:"observed_at"`

	// SerialNumber — серийный номер фискального регистратора.
	// Источник: поле serial_number из AgentDataDTO.
	// Основной идентификатор ФР.
	SerialNumber *string `gorm:"column:serial_number;type:text" json:"serial_number"`

	// SerialNormalized — нормализованный серийный номер.
	// Формат: серийный номер без пробелов и спецсимволов.
	// Используется для поиска существующих ФР.
	SerialNormalized *string `gorm:"column:serial_normalized;type:text;index" json:"serial_normalized"`

	// RNKKT — регистрационный номер ККТ.
	// Источник: поле rn_kkt из AgentDataDTO.
	RNKKT *string `gorm:"column:rn_kkt;type:text" json:"rn_kkt"`

	// ModelName — модель ККТ.
	// Источник: поле model_name из AgentDataDTO.
	ModelName *string `gorm:"column:model_name;type:text" json:"model_name"`

	// INN — ИНН организации.
	// Источник: поле inn из AgentDataDTO.
	// Используется для поиска организации-владельца.
	INN *string `gorm:"column:inn;type:text" json:"inn"`

	// FNNumber — номер фискального накопителя.
	// Источник: поле fn_number из AgentDataDTO.
	FNNumber *string `gorm:"column:fn_number;type:text" json:"fn_number"`

	// FNExpireDate — дата окончания действия фискального накопителя.
	// Источник: поле fn_expire_date из AgentDataDTO.
	// Используется для мониторинга срока действия ФН.
	FNExpireDate *time.Time `json:"fn_expire_date"`

	// OrganizationName — название организации.
	// Источник: поле organization_name из AgentDataDTO.
	// Используется для отображения в UI.
	OrganizationName *string `gorm:"type:text" json:"organization_name"`

	// Address — адрес установки ФР.
	// Источник: поле address из AgentDataDTO.
	Address *string `gorm:"type:text" json:"address"`

	// CreatedAt — время создания записи.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt — время последнего обновления записи.
	UpdatedAt time.Time `json:"updated_at"`
}
