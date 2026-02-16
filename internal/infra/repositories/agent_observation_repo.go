package repositories

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"etalon-server/internal/contextkeys"
	"etalon-server/internal/domain/common"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/contract"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/server"
	domainServices "etalon-server/internal/domain/services"
	"etalon-server/internal/domain/workstation"
	"etalon-server/internal/infra/logger"
	api "etalon-server/internal/transport/http/dtos"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type CandidateApproveInput struct {
	CandidateID       uint
	CompanyID         string
	ServerID          *string
	ServerCRMID       *string
	ServerURL         *string
	ServerUniqueID    *string
	ServerCabinetLink *string
	ServerName        *string
	ServerDesc        *string
	Comment           *string

	CompanyTitle          *string
	CompanyAddress        *string
	CompanyAdditionalName *string
	CompanyParentID       *string
	ContractMode          *string
	ContractType          *string

	Workstations []CandidateWorkstationInput

	// Р учной ввод remote IDs (опционально).
	// РСЃРїРѕР»СЊР·СѓРµС‚СЃСЏ когда агент не собрал TeamViewer/LiteManager/AnyDesk.
	// Приоритет: ручной ввод > значения из staging.
	TeamviewerID  *string
	LitemanagerID *string
	AnydeskID     *string
}

// CandidateWorkstationInput описывает имя станции, заданное оператором при подтверждении кандидата.
type CandidateWorkstationInput struct {
	StagingID       *uint
	WorkstationUUID *string
	Name            string
}

// AgentObservationService определяет интерфейс для обработки наблюдений от агентов мониторинга.
// Р еализует паттерн Service Layer, скрывая сложную логику сопоставления и создания сущностей.
type AgentObservationService interface {
	// ApplyObservation обрабатывает данные, полученные от агента мониторинга.
	// Выполняет поиск существующих сущностей, создание/обновление Workstation и FiscalRegister,
	// или создание кандидата для ручной обработки оператором.
	ApplyObservation(ctx context.Context, source string, data *api.AgentDataDTO) (*models.AgentObservation, error)

	// ApproveCandidate подтверждает кандидата оператором.
	// Создает компанию, сервер и привязывает все staged-наблюдения.
	ApproveCandidate(ctx context.Context, in CandidateApproveInput) (*models.Candidate, error)
}

// agentObservationRepo реализует логику обработки наблюдений от агентов.
// Отвечает за:
// - Р егистрацию наблюдений с идемпотентностью по payload_hash
// - Поиск существующих сущностей (Server, Workstation, FiscalRegister)
// - Создание/обновление сущностей с защитой от устаревших данных
// - Определение владельца для network-hub серверов
// - Создание кандидатов для ручной обработки оператором
type agentObservationRepo struct {
	logger        logger.LoggerInterface
	db            *gorm.DB
	negativeCache negativeCache // Кэш отрицательных результатов поиска сервера
	ownerResolver domainServices.OwnerResolver
	hubDetector   domainServices.NetworkHubDetector
}

// negativeCacheEntry представляет запись в кэше отрицательных результатов.
// Хранит время добавления для проверки TTL.
type negativeCacheEntry struct {
	cachedAt time.Time
}

// negativeCache вЂ” thread-safe кэш для хранения отрицательных результатов поиска сервера.
// Ключ: комбинация serverKey|normalizedRMS, значение: negativeCacheEntry с временем кэширования.
type negativeCache struct {
	entries sync.Map
	ttl     time.Duration
}

// get проверяет наличие записи в кэше и её актуальность по TTL.
// Возвращает true, если запись существует и не истёк TTL.
func (c *negativeCache) get(key string) bool {
	if c == nil {
		return false
	}
	val, ok := c.entries.Load(key)
	if !ok {
		return false
	}
	entry, ok := val.(negativeCacheEntry)
	if !ok {
		return false
	}
	// Проверка TTL
	return time.Since(entry.cachedAt) < c.ttl
}

// set добавляет запись в кэш с текущим временем.
func (c *negativeCache) set(key string) {
	if c == nil {
		return
	}
	c.entries.Store(key, negativeCacheEntry{cachedAt: time.Now()})
}

// buildNegativeCacheKey строит ключ кэша из serverKey и normalizedRMS.
// РСЃРїРѕР»СЊР·СѓРµС‚СЃСЏ для идентификации уникального поискового запроса.
func buildNegativeCacheKey(serverKey, normalizedRMS string) string {
	return fmt.Sprintf("%s|%s", serverKey, normalizedRMS)
}

// AgentObservationRepoOption определяет функциональную опцию для конфигурации репозитория.
type AgentObservationRepoOption func(*agentObservationRepo)

// WithOwnerResolver устанавливает OwnerResolver для автоматического определения владельца.
func WithOwnerResolver(resolver domainServices.OwnerResolver) AgentObservationRepoOption {
	return func(r *agentObservationRepo) {
		r.ownerResolver = resolver
	}
}

// WithHubDetector устанавливает NetworkHubDetector для определения network-hub серверов.
func WithHubDetector(detector domainServices.NetworkHubDetector) AgentObservationRepoOption {
	return func(r *agentObservationRepo) {
		r.hubDetector = detector
	}
}

// NewAgentObservationRepo создает новый экземпляр репозитория обработки наблюдений.
// Поддерживает функциональные опции для внедрения OwnerResolver и HubDetector.
func NewAgentObservationRepo(logger logger.LoggerInterface, db *gorm.DB, opts ...AgentObservationRepoOption) *agentObservationRepo {
	r := &agentObservationRepo{
		logger: logger,
		db:     db,
		negativeCache: negativeCache{
			ttl: 3 * time.Minute, // TTL кэша отрицательных результатов
		},
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// ApplyObservation обрабатывает данные, полученные от агента мониторинга.
//
// Алгоритм обработки:
// 1. Вычисление хеша payload для идемпотентности (дубликаты пропускаются)
// 2. Создание записи AgentObservation со статусом PROCESSING
// 3. Проверка на локальный адрес (игнорируем 127.x, 10.x, 192.168.x, 172.16-31.x)
// 4. Проверка на устаревшие данные (сравнение observed_at с agent.last_observed_at)
// 5. Поиск сервера по CRM ID, server_key или IP/URL
// 6. Если сервер вЂ” network-hub, попытка автоматического определения владельца
// 7. Поиск/создание Workstation по remote IDs (TeamViewer, LiteManager, AnyDesk)
// 8. Поиск/создание FiscalRegister по серийному номеру
// 9. Если сервер не найден или нет remote IDs вЂ” создание кандидата
//
// Параметры:
//   - ctx: контекст для отмены операции
//   - source: источник данных (имя файла или UUID агента)
//   - data: DTO с данными от агента
//
// Возвращает:
//   - *models.AgentObservation: созданное наблюдение с заполненными связями
//   - error: ошибка валидации или БД
//
// Возможные статусы результата:
//   - APPLIED: данные успешно применены (созданы/обновлены сущности)
//   - STAGED: создан кандидат для ручной обработки
//   - IGNORED: отклонено (локальный адрес)
//   - IGNORED_STALE: отклонено как устаревшее
//   - ERROR: ошибка обработки
func (s *agentObservationRepo) ApplyObservation(ctx context.Context, source string, data *api.AgentDataDTO) (*models.AgentObservation, error) {
	// Валидация входных данных
	if data == nil {
		return nil, errors.New("пустой payload")
	}

	// РР·РІР»РµРєР°РµРј trace_id из контекста для сквозной трассировки
	traceID := contextkeys.GetTraceID(ctx)
	if traceID == "" {
		traceID = uuid.New().String()
	}

	// Логирование входящих данных для трассировки
	log := s.logger.With(
		"trace_id", traceID,
		"operation", "apply_observation",
		"source", source,
	)

	log.Info("Начало обработки наблюдения",
		"agent_uuid", strings.TrimSpace(data.AgentUUID),
		"hostname", strings.TrimSpace(data.Hostname),
		"crm_id", strings.TrimSpace(data.CRMID),
		"url_rms", strings.TrimSpace(data.URLRms),
		"serial_number", strings.TrimSpace(data.SerialNumber),
		"teamviewer_id", normRID(data.TeamviewerID),
		"litemanager_id", normRID(data.LitemanagerID),
		"anydesk_id", normRID(data.AnydeskID),
	)

	// Нормализация и подготовка данных
	observedAt := parseObservedAt(data.CurrentTime)
	normalizedRMS := normalizeRMS(data.URLRms)
	serverKey := buildServerKey(normalizedRMS)
	hash, payloadJSON, err := payloadDigest(data)
	if err != nil {
		return nil, fmt.Errorf("ошибка вычисления хеша payload: %w", err)
	}

	s.logger.Debug("Данные нормализованы",
		"source", source,
		"observed_at", observedAt.UTC().Format(time.RFC3339),
		"normalized_rms", normalizedRMS,
		"server_key", serverKey,
		"payload_hash", hash[:16]+"...", // первые 16 символов для читаемости
	)

	obs := &models.AgentObservation{}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Создание записи наблюдения с идемпотентностью по payload_hash
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&models.AgentObservation{
			Source:      source,
			ObservedAt:  observedAt,
			ServerKey:   strPtr(serverKey),
			ServerCRMID: strPtr(strings.TrimSpace(data.CRMID)),
			PayloadJSON: payloadJSON,
			PayloadHash: hash,
			Status:      models.AgentObservationStatusProcessing,
		}).Error; err != nil {
			return fmt.Errorf("ошибка создания наблюдения: %w", err)
		}

		// Получение созданной или существующей записи по хешу
		if err := tx.Where("payload_hash = ?", hash).First(obs).Error; err != nil {
			return fmt.Errorf("ошибка получения наблюдения по хешу: %w", err)
		}

		s.logger.Info("Наблюдение зарегистрировано",
			"observation_id", obs.ID,
			"current_status", obs.Status,
			"server_key", ptrValue(obs.ServerKey),
			"server_crm_id", ptrValue(obs.ServerCRMID),
		)

		// Обогащаем логгер observation_id для последующих логов
		log = log.With("observation_id", obs.ID)

		// Проверка на повторную обработку (идемпотентность)
		if obs.Status == models.AgentObservationStatusApplied || obs.Status == models.AgentObservationStatusStaged || obs.Status == models.AgentObservationStatusIgnored || obs.Status == models.AgentObservationStatusIgnoredStale {
			s.logger.Debug("Повторное наблюдение пропущено",
				"observation_id", obs.ID,
				"status", obs.Status,
			)
			return nil
		}

		// Проверка на локальный адрес сервера
		if isLocalRMS(normalizedRMS) {
			msg := "локальный адрес исключен"
			obs.Status = models.AgentObservationStatusIgnored
			obs.ErrorText = &msg
			s.logger.Info("Наблюдение отклонено: локальный адрес",
				"observation_id", obs.ID,
				"normalized_rms", normalizedRMS,
			)
			return tx.Save(obs).Error
		}

		// Поиск сервера по CRM ID, server_key или IP/URL
		srv, err := s.findServer(tx, data.CRMID, serverKey, normalizedRMS, source)
		if err != nil {
			return fmt.Errorf("ошибка поиска сервера: %w", err)
		}

		if srv != nil {
			s.logger.Info("Сервер найден для наблюдения",
				"observation_id", obs.ID,
				"server_id", srv.ID,
				"server_owner_id", ptrValue(srv.OwnerID),
				"server_crm_id", ptrValue(srv.CRMid),
			)
		} else {
			s.logger.Info("Сервер не найден для наблюдения",
				"observation_id", obs.ID,
				"server_key", serverKey,
				"crm_id", strings.TrimSpace(data.CRMID),
				"normalized_rms", normalizedRMS,
			)
		}

		// Проверка на устаревшие данные от того же агента
		staleByAgent, agentLastObservedAt, err := s.isStaleByAgentStream(tx, source, data, observedAt)
		if err != nil {
			return fmt.Errorf("ошибка проверки устаревания: %w", err)
		}
		if staleByAgent {
			msg := fmt.Sprintf("устаревшие данные агента: observed_at=%s, last_observed_at=%s", observedAt.UTC().Format(time.RFC3339), agentLastObservedAt.UTC().Format(time.RFC3339))
			obs.Status = models.AgentObservationStatusIgnoredStale
			obs.ErrorText = &msg
			s.logger.Info("Наблюдение отклонено: устаревшие данные агента",
				"observation_id", obs.ID,
				"source", source,
				"observed_at", observedAt.UTC().Format(time.RFC3339),
				"agent_last_observed_at", agentLastObservedAt.UTC().Format(time.RFC3339),
			)
			return tx.Save(obs).Error
		}
		updater := resolveAgentUpdater(source, data)

		// Обработка network-hub серверов (автоматическое определение владельца)
		if srv != nil {
			// РСЃРїРѕР»СЊР·СѓРµРј инжектированный HubDetector или fallback на inline метод
			var isHub bool
			var err error
			if s.hubDetector != nil {
				isHub, err = s.hubDetector.IsNetworkHubServer(ctx, srv)
			} else {
				isHub, err = s.isNetworkHubServer(tx, srv)
			}
			if err != nil {
				return fmt.Errorf("ошибка проверки network-hub: %w", err)
			}

			if isHub {
				s.logger.Debug("Сервер является network-hub, попытка определения владельца",
					"observation_id", obs.ID,
					"server_id", srv.ID,
					"hub_company_id", ptrValue(srv.OwnerID),
				)

				// РСЃРїРѕР»СЊР·СѓРµРј инжектированный OwnerResolver или fallback на inline метод
				var resolution *domainServices.OwnerResolution
				hubCompanyID := strings.TrimSpace(ptrValue(srv.OwnerID))
				if s.ownerResolver != nil {
					resolution, err = s.ownerResolver.Resolve(ctx, hubCompanyID,
						normRID(data.TeamviewerID),
						normRID(data.LitemanagerID),
						normRID(data.AnydeskID),
						normalizeSerial(data.SerialNumber),
					)
				} else {
					// Fallback на inline метод для обратной совместимости
					var ownerID string
					var confident bool
					ownerID, confident, err = s.resolveNetworkOwner(tx, hubCompanyID, data)
					if err == nil {
						resolution = &domainServices.OwnerResolution{
							OwnerID:   ownerID,
							Confident: confident,
						}
					}
				}
				if err != nil {
					return fmt.Errorf("ошибка определения владельца: %w", err)
				}

				// Обработка конфликта владельцев
				if resolution != nil && resolution.HasConflict {
					s.logger.Warn("Обнаружен конфликт владельцев для network-hub",
						"observation_id", obs.ID,
						"server_id", srv.ID,
						"ws_owner_id", resolution.WSMatch != nil,
						"fr_owner_id", resolution.FRMatch != nil,
					)
					// Создаем network-candidate с информацией о конфликте
					nc, err := s.stageNetworkCandidateWithConflict(tx, obs, data, observedAt, normalizedRMS, serverKey, srv, resolution)
					if err != nil {
						return fmt.Errorf("ошибка создания network-candidate с конфликтом: %w", err)
					}
					obs.NetworkCandidateID = &nc.ID
					obs.Status = models.AgentObservationStatusStaged
					s.logger.Info("Наблюдение отправлено в network-candidate (конфликт владельцев)",
						"observation_id", obs.ID,
						"network_candidate_id", nc.ID,
						"hub_company_id", ptrValue(srv.OwnerID),
					)
					return tx.Save(obs).Error
				}

				if resolution != nil && resolution.Confident && strings.TrimSpace(resolution.OwnerID) != "" {
					s.logger.Info("Владелец автоматически определен для network-hub",
						"observation_id", obs.ID,
						"server_id", srv.ID,
						"resolved_owner_id", resolution.OwnerID,
					)

					ownerRef := strPtr(resolution.OwnerID)
					ws, staleWS, err := s.applyWorkstation(tx, obs.ID, srv, data, observedAt, false, ownerRef, models.OwnerChangeSourceNetworkAuto, updater)
					if err != nil {
						return fmt.Errorf("ошибка применения рабочей станции: %w", err)
					}
					obs.WorkstationID = &ws.ID

					if err := s.upsertAgent(tx, source, data, ws.ID, observedAt); err != nil {
						return fmt.Errorf("ошибка обновления агента: %w", err)
					}

					frApplied := false
					frStale := false
					if strings.TrimSpace(data.SerialNumber) != "" {
						fr, staleFR, err := s.applyFiscal(tx, obs.ID, srv, ws, data, observedAt, false, ownerRef, models.OwnerChangeSourceNetworkAuto, updater)
						if err != nil {
							return fmt.Errorf("ошибка применения Р¤Р : %w", err)
						}
						if fr != nil {
							obs.FRID = &fr.ID
							frApplied = true
							frStale = staleFR
						}
					}

					if staleWS && (!frApplied || frStale) {
						obs.Status = models.AgentObservationStatusIgnoredStale
					} else {
						obs.Status = models.AgentObservationStatusApplied
					}
					if err := tx.Save(obs).Error; err != nil {
						return err
					}
					return s.resolveConflicts(tx, obs)
				}

				// Невозможно автоматически определить владельца вЂ” создаем network-candidate
				nc, err := s.stageNetworkCandidate(tx, obs, data, observedAt, normalizedRMS, serverKey, srv)
				if err != nil {
					return fmt.Errorf("ошибка создания network-candidate: %w", err)
				}
				obs.NetworkCandidateID = &nc.ID
				obs.Status = models.AgentObservationStatusStaged
				s.logger.Info("Наблюдение отправлено в network-candidate (владелец не определен)",
					"observation_id", obs.ID,
					"network_candidate_id", nc.ID,
					"hub_company_id", ptrValue(srv.OwnerID),
				)
				return tx.Save(obs).Error
			}
		}

		// Создание кандидата для ручного подтверждения оператором.
		//
		// Это намеренное поведение в двух случаях:
		// 1. srv == nil вЂ” сервер не найден в системе, требуется создание нового сервера
		// 2. !hasRemoteID(data) вЂ” агент не собрал ни один remote ID (TeamViewer, LiteManager, AnyDesk).
		//    Без remote ID невозможно идентифицировать рабочую станцию.
		//    Администратор должен вручную указать remote IDs при принятии кандидата на АО.
		if srv == nil || !hasRemoteID(data) {
			// Определяем причину staging для информативного логирования
			var reason string
			switch {
			case srv == nil && !hasRemoteID(data):
				reason = "сервер не найден и отсутствуют remote IDs"
			case srv == nil:
				reason = "сервер не найден в системе"
			case !hasRemoteID(data):
				reason = "отсутствуют remote IDs (TeamViewer/LiteManager/AnyDesk) вЂ” невозможно идентифицировать Р С"
			}

			s.logger.Info("Создание кандидата для ручного подтверждения",
				"observation_id", obs.ID,
				"reason", reason,
				"server_found", srv != nil,
				"teamviewer_id", normRID(data.TeamviewerID),
				"litemanager_id", normRID(data.LitemanagerID),
				"anydesk_id", normRID(data.AnydeskID),
				"server_key", serverKey,
				"crm_id", strings.TrimSpace(data.CRMID),
			)

			c, err := s.stage(tx, obs, data, observedAt, normalizedRMS, serverKey, srv)
			if err != nil {
				return fmt.Errorf("ошибка создания кандидата: %w", err)
			}
			obs.CandidateID = &c.ID
			obs.Status = models.AgentObservationStatusStaged
			s.logger.Info("Наблюдение отправлено в staging",
				"observation_id", obs.ID,
				"candidate_id", c.ID,
				"reason", reason,
			)
			return tx.Save(obs).Error
		}

		// Стандартная обработка: создание/обновление Workstation и FiscalRegister
		ws, staleWS, err := s.applyWorkstation(tx, obs.ID, srv, data, observedAt, false, nil, "", updater)
		if err != nil {
			return fmt.Errorf("ошибка применения рабочей станции: %w", err)
		}

		obs.WorkstationID = &ws.ID

		if err := s.upsertAgent(tx, source, data, ws.ID, observedAt); err != nil {
			return fmt.Errorf("ошибка обновления агента: %w", err)
		}

		frApplied := false
		frStale := false
		if strings.TrimSpace(data.SerialNumber) != "" {
			fr, staleFR, err := s.applyFiscal(tx, obs.ID, srv, ws, data, observedAt, false, nil, "", updater)
			if err != nil {
				return fmt.Errorf("ошибка применения Р¤Р : %w", err)
			}
			if fr != nil {
				obs.FRID = &fr.ID
				frApplied = true
				frStale = staleFR
			}
		}

		// Определение финального статуса
		if staleWS && (!frApplied || frStale) {
			obs.Status = models.AgentObservationStatusIgnoredStale
		} else {
			obs.Status = models.AgentObservationStatusApplied
		}

		if err := tx.Save(obs).Error; err != nil {
			return err
		}

		s.logger.Info("Наблюдение успешно применено",
			"observation_id", obs.ID,
			"status", obs.Status,
			"workstation_id", ptrValue(obs.WorkstationID),
			"fr_id", ptrValue(obs.FRID),
			"stale_workstation", staleWS,
			"stale_fiscal", frStale,
		)

		return s.resolveConflicts(tx, obs)
	})

	if err != nil {
		s.logger.Error("Ошибка применения наблюдения",
			"error", err,
			"source", source,
			"payload_hash", hash,
		)
		if obs.ID != 0 {
			_ = s.db.WithContext(ctx).Model(&models.AgentObservation{}).Where("id = ?", obs.ID).Updates(map[string]interface{}{"status": models.AgentObservationStatusError, "error_text": err.Error()}).Error
		}
		return nil, err
	}

	return obs, nil
}

// ApproveCandidate подтверждает кандидата оператором.
//
// Алгоритм:
// 1. Получение кандидата из БД
// 2. Создание или получение компании (по ID или создание новой)
// 3. Создание или получение сервера (по ID, CRM ID или server_key)
// 4. Обновление сервера данными от оператора
// 5. Обработка всех staged-наблюдений (создание Р С и Р¤Р )
// 6. Переименование Р С по указанию оператора
// 7. Обновление статуса кандидата на APPROVED
// 8. Закрытие связанных задач сверки
//
// Параметры:
//   - in: входные данные с ID кандидата, данными компании и сервера
//
// Возвращает:
//   - *models.Candidate: обновленный кандидат
//   - error: ошибка валидации или БД
func (s *agentObservationRepo) ApproveCandidate(ctx context.Context, in CandidateApproveInput) (*models.Candidate, error) {
	isManual := in.CandidateID == 0
	s.logger.Info("Начато подтверждение кандидата",
		"candidate_id", in.CandidateID,
		"company_id", strings.TrimSpace(in.CompanyID),
		"server_id", ptrValue(in.ServerID),
		"is_manual", isManual,
	)

	var out models.Candidate
	var approvedServerID string
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if !isManual {
			if err := tx.Where("id = ?", in.CandidateID).First(&out).Error; err != nil {
				return err
			}
		}
		companyID, err := s.ensureCompany(tx, in)
		if err != nil {
			return err
		}
		in.CompanyID = companyID
		s.logger.Info("Подтверждение кандидата: компания подготовлена",
			"candidate_id", in.CandidateID,
			"company_id", companyID,
		)

		serverProvided := !isManual || hasServerInput(in)
		var srv *server.Server
		if serverProvided {
			srv, err = s.ensureServer(tx, &out, in)
			if err != nil {
				return err
			}
			approvedServerID = srv.ID
			serverUpdates := map[string]interface{}{
				"owner_id": in.CompanyID,
			}
			if out.ServerKey != nil && strings.TrimSpace(*out.ServerKey) != "" {
				serverUpdates["server_key"] = valOrNil(out.ServerKey)
			}
			if v := valOrNil(in.ServerCRMID); v != nil {
				serverUpdates["crm_id"] = v
			}
			if v := valOrNil(in.ServerURL); v != nil {
				serverUpdates["ip"] = v
			}
			if v := valOrNil(in.ServerName); v != nil {
				serverUpdates["device_name"] = v
			}
			if v := valOrNil(in.ServerDesc); v != nil {
				serverUpdates["description"] = v
			}
			if v := valOrNil(in.ServerUniqueID); v != nil {
				serverUpdates["unique_id"] = v
			}
			if cabinetID := extractCabinetClientID(ptrValue(in.ServerCabinetLink)); cabinetID != "" {
				serverUpdates["cabinet_link"] = cabinetID
			}
			if err := tx.Model(&server.Server{}).Where("id = ?", srv.ID).Updates(serverUpdates).Error; err != nil {
				return err
			}
			s.logger.Info("Подтверждение кандидата: сервер подготовлен",
				"candidate_id", in.CandidateID,
				"server_id", srv.ID,
				"server_crm_id", ptrValue(in.ServerCRMID),
				"server_url", ptrValue(in.ServerURL),
			)
		}

		if isManual {
			return nil
		}

		var staged []models.AgentObservation
		if err := tx.Where("candidate_id = ?", out.ID).Order("observed_at asc").Find(&staged).Error; err != nil {
			return err
		}
		s.logger.Info("Подтверждение кандидата: найдено staged-наблюдений",
			"candidate_id", in.CandidateID,
			"staged_count", len(staged),
		)

		stagingToWS := make(map[uint]string)
		for _, so := range staged {
			var payload api.AgentDataDTO
			if err := json.Unmarshal(so.PayloadJSON, &payload); err != nil {
				continue
			}

			// Применение ручного ввода remote IDs (приоритет над значениями из staging)
			if in.TeamviewerID != nil || in.LitemanagerID != nil || in.AnydeskID != nil {
				s.logger.Info("Применение ручного ввода remote IDs",
					"candidate_id", in.CandidateID,
					"observation_id", so.ID,
					"manual_teamviewer", ptrValue(in.TeamviewerID),
					"manual_litemanager", ptrValue(in.LitemanagerID),
					"manual_anydesk", ptrValue(in.AnydeskID),
					"original_teamviewer", normRID(payload.TeamviewerID),
					"original_litemanager", normRID(payload.LitemanagerID),
					"original_anydesk", normRID(payload.AnydeskID),
				)
			}
			if in.TeamviewerID != nil {
				payload.TeamviewerID = *in.TeamviewerID
			}
			if in.LitemanagerID != nil {
				payload.LitemanagerID = *in.LitemanagerID
			}
			if in.AnydeskID != nil {
				payload.AnydeskID = *in.AnydeskID
			}

			obsAt := so.ObservedAt
			if obsAt.IsZero() {
				obsAt = parseObservedAt(payload.CurrentTime)
			}
			updater := resolveAgentUpdater(so.Source, &payload)
			ws, _, err := s.applyWorkstation(tx, so.ID, srv, &payload, obsAt, true, nil, models.OwnerChangeSourceCandidateApprove, updater)
			if err != nil {
				return err
			}
			if ws != nil && strings.TrimSpace(payload.SerialNumber) != "" {
				if _, _, err := s.applyFiscal(tx, so.ID, srv, ws, &payload, obsAt, true, nil, models.OwnerChangeSourceCandidateApprove, updater); err != nil {
					return err
				}
			}
			if ws != nil {
				var wsStages []models.CandidateWorkstationStaging
				if err := tx.Where("candidate_id = ? AND observation_id = ?", out.ID, so.ID).Find(&wsStages).Error; err != nil {
					return err
				}
				for _, wsStage := range wsStages {
					stagingToWS[wsStage.ID] = ws.ID
				}
			}
			if err := tx.Model(&models.AgentObservation{}).Where("id = ?", so.ID).Updates(map[string]interface{}{"status": models.AgentObservationStatusApplied, "candidate_id": nil}).Error; err != nil {
				return err
			}
			s.logger.Info("Подтверждение кандидата: staged-наблюдение обработано",
				"candidate_id", in.CandidateID,
				"observation_id", so.ID,
			)
		}
		if err := s.renameApprovedWorkstations(tx, out.ID, in.Workstations, stagingToWS); err != nil {
			return err
		}

		from := out.Status
		if err := tx.Model(&out).Updates(map[string]interface{}{
			"status":              models.CandidateStatusApproved,
			"approved_company_id": in.CompanyID,
			"approved_server_id":  srv.ID,
		}).Error; err != nil {
			return err
		}
		reason := "Подтверждение кандидата оператором"
		if msg := strings.TrimSpace(ptrValue(in.Comment)); msg != "" {
			reason = fmt.Sprintf("%s: %s", reason, msg)
		}
		if err := tx.Create(&models.CandidateStatusHistory{CandidateID: out.ID, FromStatus: strPtr(from), ToStatus: models.CandidateStatusApproved, Reason: &reason}).Error; err != nil {
			return err
		}
		return tx.Model(&models.ReconciliationTask{}).
			Where("task_type = ? AND entity_uuid = ? AND status IN ?", "candidate_connection", fmt.Sprintf("candidate:%d", out.ID), []string{"new", "pending_sd_action", "sd_error"}).
			Updates(map[string]interface{}{"status": "resolved", "comment": "Кандидат подтвержден"}).Error
	})
	if err != nil {
		return nil, err
	}
	if isManual {
		out.Status = models.CandidateStatusApproved
		out.ApprovedCompanyID = strPtr(in.CompanyID)
		out.ApprovedServerID = strPtr(approvedServerID)
		s.logger.Info("Р учное подтверждение завершено",
			"company_id", in.CompanyID,
			"server_id", approvedServerID,
		)
		return &out, nil
	}
	if err := s.db.WithContext(ctx).Where("id = ?", in.CandidateID).First(&out).Error; err != nil {
		return nil, err
	}
	s.logger.Info("Подтверждение кандидата завершено",
		"candidate_id", out.ID,
		"status", out.Status,
		"approved_company_id", ptrValue(out.ApprovedCompanyID),
		"approved_server_id", ptrValue(out.ApprovedServerID),
	)
	return &out, nil
}

// ensureCompany создает или возвращает существующую компанию для подтверждения кандидата.
func (s *agentObservationRepo) ensureCompany(tx *gorm.DB, in CandidateApproveInput) (string, error) {
	if strings.TrimSpace(in.CompanyID) != "" {
		var existing company.Company
		if err := tx.Where("id = ?", strings.TrimSpace(in.CompanyID)).First(&existing).Error; err != nil {
			return "", err
		}
		return existing.ID, nil
	}
	if strings.TrimSpace(ptrValue(in.CompanyTitle)) == "" {
		return "", errors.New("необходимо указать company_id или company.title")
	}
	newCompany := company.Company{
		Title:          strPtr(ptrValue(in.CompanyTitle)),
		Address:        strPtr(ptrValue(in.CompanyAddress)),
		AdditionalName: strPtr(ptrValue(in.CompanyAdditionalName)),
		ParentID:       strPtr(ptrValue(in.CompanyParentID)),
	}
	if err := tx.Create(&newCompany).Error; err != nil {
		return "", err
	}
	if err := s.applyContractForNewCompany(tx, newCompany.ID, newCompany.ParentID, in); err != nil {
		return "", err
	}
	return newCompany.ID, nil
}

// applyContractForNewCompany применяет первый подходящий активный контракт для новой компании.
func (s *agentObservationRepo) applyContractForNewCompany(tx *gorm.DB, companyID string, parentID *string, in CandidateApproveInput) error {
	mode := strings.ToLower(strings.TrimSpace(ptrValue(in.ContractMode)))
	if mode == "" {
		return errors.New("для нового контракта необходимо выбрать режим")
	}

	switch mode {
	case "inherit_parent":
		parentCompanyID := strings.TrimSpace(ptrValue(parentID))
		if parentCompanyID == "" {
			return errors.New("для наследования родительского контракта необходима родительская компания")
		}
		contractID, err := s.findActiveContractIDByCompany(tx, parentCompanyID)
		if err != nil {
			return err
		}
		if err := s.linkContractToCompany(tx, contractID, companyID); err != nil {
			return err
		}
	case "new":
		contractType := strings.TrimSpace(ptrValue(in.ContractType))
		if contractType == "" {
			return errors.New("для нового контракта нужно выбрать тип обслуживания")
		}
		state := "active"
		services, _ := json.Marshal([]string{contractType})
		newContract := contract.Contract{
			State:        &state,
			Services:     datatypes.JSON(services),
			ServiceLevel: -1,
		}
		if err := tx.Create(&newContract).Error; err != nil {
			return err
		}
		if err := s.linkContractToCompany(tx, newContract.ID, companyID); err != nil {
			return err
		}
	default:
		return errors.New("неизвестный сценарий контракта")
	}

	active := true
	return tx.Model(&company.Company{}).Where("id = ?", companyID).Update("active_contract", active).Error
}

// findActiveContractIDByCompany возвращает ID первого подходящего активного контракта компании.
func (s *agentObservationRepo) findActiveContractIDByCompany(tx *gorm.DB, companyID string) (string, error) {
	var contractID string
	err := tx.Table("contracts").
		Select("contracts.id").
		Joins("JOIN company_contracts ON company_contracts.contract_id = contracts.id").
		Where("company_contracts.company_id = ? AND contracts.state = ?", companyID, "active").
		Order("contracts.updated_at DESC").
		Limit(1).
		Scan(&contractID).Error
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(contractID) == "" {
		return "", errors.New("у компании нет подходящего активного контракта")
	}
	return contractID, nil
}

// linkContractToCompany связывает контракт с компанией через company_contracts.
func (s *agentObservationRepo) linkContractToCompany(tx *gorm.DB, contractID, companyID string) error {
	return tx.Exec(
		"INSERT INTO company_contracts (contract_id, company_id) VALUES (?, ?) ON CONFLICT DO NOTHING",
		contractID,
		companyID,
	).Error
}

// renameApprovedWorkstations переименовывает рабочие станции, которые были заданы оператором при подтверждении.
func (s *agentObservationRepo) renameApprovedWorkstations(tx *gorm.DB, candidateID uint, rows []CandidateWorkstationInput, stagingToWS map[uint]string) error {
	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		if name == "" {
			continue
		}
		var targetID string
		if row.StagingID != nil {
			if mapped, ok := stagingToWS[*row.StagingID]; ok {
				targetID = mapped
			}
			if targetID == "" {
				var stage models.CandidateWorkstationStaging
				if err := tx.Where("id = ? AND candidate_id = ?", *row.StagingID, candidateID).First(&stage).Error; err == nil {
					if stage.WorkstationUUID != nil {
						targetID = strings.TrimSpace(*stage.WorkstationUUID)
					}
				}
			}
		}
		if targetID == "" && row.WorkstationUUID != nil {
			targetID = strings.TrimSpace(*row.WorkstationUUID)
		}
		if targetID == "" {
			continue
		}
		if err := tx.Model(&workstation.Workstation{}).Where("id = ?", targetID).Updates(map[string]interface{}{"device_name": name, "is_new": false}).Error; err != nil {
			return err
		}
	}
	return nil
}

// ensureServer находит существующий сервер или создает новый при подтверждении кандидата.
//
// Порядок поиска:
// 1. По ServerID из входных параметров
// 2. По ExistingServerID из кандидата
// 3. По ServerCRMID из входных параметров
// 4. По ServerKey из кандидата
//
// Если сервер не найден вЂ” создается новая запись.
func (s *agentObservationRepo) ensureServer(tx *gorm.DB, c *models.Candidate, in CandidateApproveInput) (*server.Server, error) {
	var srv server.Server
	if in.ServerID != nil && strings.TrimSpace(*in.ServerID) != "" {
		if err := tx.Where("id = ?", *in.ServerID).First(&srv).Error; err != nil {
			return nil, err
		}
		return &srv, nil
	}
	if c.ExistingServerID != nil && strings.TrimSpace(*c.ExistingServerID) != "" {
		if err := tx.Where("id = ?", strings.TrimSpace(*c.ExistingServerID)).First(&srv).Error; err == nil {
			return &srv, nil
		}
	}
	if in.ServerCRMID != nil && strings.TrimSpace(*in.ServerCRMID) != "" {
		if err := tx.Where("crm_id = ?", strings.TrimSpace(*in.ServerCRMID)).First(&srv).Error; err == nil {
			return &srv, nil
		}
	}
	if c.ServerKey != nil {
		if err := tx.Where("server_key = ?", *c.ServerKey).First(&srv).Error; err == nil {
			return &srv, nil
		}
	}
	srv = server.Server{
		OwnerID:     &in.CompanyID,
		CRMid:       in.ServerCRMID,
		IP:          in.ServerURL,
		UniqueID:    in.ServerUniqueID,
		CabinetLink: strPtr(extractCabinetClientID(ptrValue(in.ServerCabinetLink))),
		DeviceName:  in.ServerName,
		Description: in.ServerDesc,
		ServerKey:   c.ServerKey,
	}
	if err := tx.Create(&srv).Error; err != nil {
		return nil, err
	}
	return &srv, nil
}

// findServer выполняет поиск сервера по нескольким критериям в порядке приоритета.
//
// Порядок поиска:
// 1. По CRM ID (точное совпадение)
// 2. По server_key (UUID на основе URL, точное совпадение)
// 3. По IP/URL (нормализованное сравнение с портом)
// 4. По IP/URL (частичное совпадение хоста)
//
// Параметры:
//   - tx: транзакция БД
//   - crmID: CRM идентификатор сервера
//   - serverKey: UUID на основе URL (SHA1)
//   - normalizedRMS: нормализованный URL/IP сервера (host:port)
//   - source: источник вызова для логирования
//
// Возвращает:
//   - *server.Server: найденный сервер или nil
//   - error: ошибка БД (не включает ErrRecordNotFound)
func (s *agentObservationRepo) findServer(tx *gorm.DB, crmID, serverKey, normalizedRMS, source string) (*server.Server, error) {
	var srv server.Server

	// Проверка кэша отрицательных результатов
	cacheKey := buildNegativeCacheKey(serverKey, normalizedRMS)
	if s.negativeCache.get(cacheKey) {
		s.logger.Debug("запрос уже был, пропускаем",
			"source", source,
			"server_key", serverKey,
			"normalized_rms", normalizedRMS,
		)
		return nil, nil
	}

	// 1. Поиск по CRM ID (наивысший приоритет)
	crmID = strings.TrimSpace(crmID)
	if crmID != "" {
		err := tx.Where("crm_id = ?", crmID).First(&srv).Error
		if err == nil {
			s.logger.Debug("Сервер найден по CRM ID",
				"server_id", srv.ID,
				"crm_id", crmID,
				"owner_id", ptrValue(srv.OwnerID),
			)
			return &srv, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("ошибка поиска по CRM ID: %w", err)
		}
	}

	// 2. Поиск по server_key (UUID на основе URL)
	if strings.TrimSpace(serverKey) != "" {
		err := tx.Where("server_key = ?", serverKey).First(&srv).Error
		if err == nil {
			s.logger.Debug("Сервер найден по server_key",
				"server_id", srv.ID,
				"server_key", serverKey,
				"owner_id", ptrValue(srv.OwnerID),
			)
			return &srv, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("ошибка поиска по server_key: %w", err)
		}
	}

	// 3. Поиск по IP/URL
	normalizedRMS = strings.TrimSpace(strings.ToLower(normalizedRMS))
	if normalizedRMS != "" {
		// Поиск по полному совпадению IP/URL
		err := tx.Where("ip IS NOT NULL AND lower(trim(ip)) = ?", normalizedRMS).First(&srv).Error
		if err == nil {
			s.logger.Debug("Сервер найден по полному IP/URL",
				"server_id", srv.ID,
				"normalized_rms", normalizedRMS,
				"owner_id", ptrValue(srv.OwnerID),
			)
			return &srv, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("ошибка поиска по IP: %w", err)
		}

		// 4. Поиск по частичному совпадению (извлекаем хост без порта)
		host := normalizedRMS
		if strings.Contains(host, ":") {
			host = strings.Split(host, ":")[0]
		}
		if host != "" {
			var candidates []server.Server
			if err := tx.Where("ip IS NOT NULL AND lower(ip) LIKE ?", "%"+strings.ToLower(host)+"%").Limit(200).Find(&candidates).Error; err != nil {
				return nil, fmt.Errorf("ошибка поиска кандидатов по IP: %w", err)
			}
			// Проверка каждого кандидата на точное совпадение после нормализации
			for i := range candidates {
				if normalizeRMS(ptrValue(candidates[i].IP)) == normalizedRMS {
					s.logger.Debug("Сервер найден по частичному IP (нормализованное совпадение)",
						"server_id", candidates[i].ID,
						"server_ip", ptrValue(candidates[i].IP),
						"normalized_rms", normalizedRMS,
						"owner_id", ptrValue(candidates[i].OwnerID),
					)
					return &candidates[i], nil
				}
			}
		}
	}

	// Сервер не найден вЂ” сохраняем в кэше отрицательных результатов
	s.negativeCache.set(cacheKey)
	s.logger.Debug("Сервер не найден",
		"source", source,
		"crm_id", crmID,
		"server_key", serverKey,
		"normalized_rms", normalizedRMS,
	)
	return nil, nil
}

// applyWorkstation создает или обновляет рабочую станцию по данным наблюдения.
//
// Алгоритм:
// 1. Вычисление identity_hash (SHA256 от TeamViewer:LiteManager)
// 2. Поиск существующей Р С по identity_hash или remote IDs
// 3. Проверка на устаревшие данные (observed_at < last_modified_date)
// 4. Создание новой Р С или обновление существующей
// 5. При смене владельца вЂ” запись в owner_change_history
//
// Параметры:
//   - tx: транзакция БД
//   - srv: сервер, к которому привязывается Р С
//   - data: данные от агента
//   - observedAt: время наблюдения
//   - forceOwner: принудительная смена владельца (при подтверждении кандидата)
//   - ownerOverride: переопределение владельца (для network-hub)
//   - ownerChangeSource: источник смены владельца для истории
//
// Возвращает:
//   - *workstation.Workstation: созданная/обновленная Р С
//   - bool: true если данные устарели (stale)
//   - error: ошибка БД
//
// Правила обновления владельца:
//   - Если forceOwner=true: принудительно устанавливаем владельца и режим binding=manual
//   - Если владельца нет: присваиваем владельца от сервера
//   - Если binding!=manual и владелец отличается: обновляем владельца
//   - Если binding=manual: не меняем владельца автоматически
func (s *agentObservationRepo) applyWorkstation(tx *gorm.DB, observationID uint, srv *server.Server, data *api.AgentDataDTO, observedAt time.Time, forceOwner bool, ownerOverride *string, ownerChangeSource string, updater string) (*workstation.Workstation, bool, error) {
	// Вычисление identity_hash для поиска существующей Р С
	identity := identityHash(data.TeamviewerID, data.LitemanagerID)

	s.logger.Debug("Поиск рабочей станции",
		"server_id", srv.ID,
		"hostname", strings.TrimSpace(data.Hostname),
		"identity_hash", identity,
		"teamviewer_id", normRID(data.TeamviewerID),
		"litemanager_id", normRID(data.LitemanagerID),
		"anydesk_id", normRID(data.AnydeskID),
	)

	ws, err := s.findWorkstation(tx, data, identity)
	if err != nil {
		return nil, false, fmt.Errorf("ошибка поиска Р С: %w", err)
	}

	if ws == nil {
		ws = &workstation.Workstation{}
	}

	// Проверка на устаревшие данные
	stale := ws.LastModifiedDate != nil && observedAt.Before(*ws.LastModifiedDate)
	if stale {
		s.logger.Info("Р абочая станция не обновлена: устаревшие данные",
			"workstation_id", ws.ID,
			"observed_at", observedAt.UTC().Format(time.RFC3339),
			"last_modified", ws.LastModifiedDate.UTC().Format(time.RFC3339),
		)
		return ws, true, nil
	}

	// Определение целевого владельца
	targetOwner := ptrValue(srv.OwnerID)
	if ownerOverride != nil && strings.TrimSpace(ptrValue(ownerOverride)) != "" {
		targetOwner = strings.TrimSpace(ptrValue(ownerOverride))
	}

	// Создание новой рабочей станции
	if ws.ID == "" {
		ws.OwnerID = strPtr(targetOwner)
		ws.ServerID = &srv.ID
		ws.DeviceName = strPtr(strings.TrimSpace(data.Hostname))
		ws.Teamviewer = normRIDPtr(data.TeamviewerID)
		ws.Litemanager = normRIDPtr(data.LitemanagerID)
		ws.Anydesk = normRIDPtr(data.AnydeskID)
		ws.IdentityHash = strPtr(identity)
		ws.LastModifiedDate = &observedAt
		ws.LastUpdatedBy = updater
		ws.IsNew = !forceOwner
		if forceOwner {
			ws.OwnerBindingMode = models.OwnerBindingModeManual
		}

		if err := tx.Create(ws).Error; err != nil {
			return nil, false, fmt.Errorf("ошибка создания Р С: %w", err)
		}
		agentUUID := ""
		if isUUID(updater) {
			agentUUID = updater
		}
		if err := s.writeCreationEvent(tx, "Workstation", ws.ID, ptrValue(ws.OwnerID), ownerChangeSource, "Создание рабочей станции", agentUUID, observationID); err != nil {
			return nil, false, fmt.Errorf("ошибка записи истории создания рабочей станции: %w", err)
		}

		s.logger.Info("Создана новая рабочая станция",
			"workstation_id", ws.ID,
			"server_id", srv.ID,
			"owner_id", targetOwner,
			"device_name", ptrValue(ws.DeviceName),
			"teamviewer", ptrValue(ws.Teamviewer),
			"litemanager", ptrValue(ws.Litemanager),
			"anydesk", ptrValue(ws.Anydesk),
			"is_new", ws.IsNew,
		)
		return ws, false, nil
	}

	// Обновление существующей рабочей станции
	prevOwner := ptrValue(ws.OwnerID)
	updates := map[string]interface{}{
		"server_id":          srv.ID,
		"last_modified_date": observedAt,
		"last_updated_by":    updater,
		"identity_hash":      valOrNil(strPtr(identity)),
	}

	// Обновление имени устройства только если Р С новая или имя пустое
	if ws.IsNew || strings.TrimSpace(ptrValue(ws.DeviceName)) == "" {
		updates["device_name"] = valOrNil(strPtr(strings.TrimSpace(data.Hostname)))
	}

	// Логика обновления владельца
	if forceOwner {
		if targetOwner != "" {
			updates["owner_id"] = targetOwner
			updates["owner_binding_mode"] = models.OwnerBindingModeManual
		}
	} else if targetOwner != "" {
		if prevOwner == "" {
			updates["owner_id"] = targetOwner
		} else if strings.TrimSpace(ws.OwnerBindingMode) != models.OwnerBindingModeManual && prevOwner != targetOwner {
			updates["owner_id"] = targetOwner
		}
	}

	// Обновление remote IDs
	if tv := normRIDPtr(data.TeamviewerID); tv != nil {
		updates["teamviewer"] = *tv
	}
	if lm := normRIDPtr(data.LitemanagerID); lm != nil {
		updates["litemanager"] = *lm
	}

	if err := tx.Model(&workstation.Workstation{}).Where("id = ?", ws.ID).Updates(updates).Error; err != nil {
		return nil, false, fmt.Errorf("ошибка обновления Р С: %w", err)
	}

	// Обработка AnyDesk (уникальный ID вЂ” очищаем дубликаты)
	if ad := normRIDPtr(data.AnydeskID); ad != nil {
		res := tx.Model(&workstation.Workstation{}).Where("anydesk = ? AND id <> ?", *ad, ws.ID).Update("anydesk", nil)
		if res.Error != nil {
			return nil, false, fmt.Errorf("ошибка очистки дубликатов AnyDesk: %w", res.Error)
		}
		if res.RowsAffected > 0 {
			s.logger.Debug("Очищены дубликаты AnyDesk",
				"workstation_id", ws.ID,
				"anydesk_id", *ad,
				"cleaned_count", res.RowsAffected,
			)
		}
		if err := tx.Model(&workstation.Workstation{}).Where("id = ?", ws.ID).Update("anydesk", *ad).Error; err != nil {
			return nil, false, fmt.Errorf("ошибка обновления AnyDesk: %w", err)
		}
	}

	// Получение обновленной записи
	if err := tx.Where("id = ?", ws.ID).First(ws).Error; err != nil {
		return nil, false, fmt.Errorf("ошибка получения обновленной Р С: %w", err)
	}

	newOwner := ptrValue(ws.OwnerID)
	agentUUID := ""
	if isUUID(updater) {
		agentUUID = updater
	}

	// Запись истории смены владельца
	if prevOwner != "" && newOwner != "" && prevOwner != newOwner && ownerChangeSource != "" {
		if err := s.writeOwnerChange(tx, "Workstation", ws.ID, prevOwner, newOwner, ownerChangeSource, "Смена владельца рабочей станции", agentUUID, observationID); err != nil {
			return nil, false, fmt.Errorf("ошибка записи истории владельца: %w", err)
		}
		s.logger.Info("Зафиксирована смена владельца Р С",
			"workstation_id", ws.ID,
			"prev_owner_id", prevOwner,
			"new_owner_id", newOwner,
			"source", ownerChangeSource,
		)
	}
	if agentUUID != "" {
		if err := s.writeAgentDataUpdate(tx, "Workstation", ws.ID, newOwner, agentUUID, "Обновление данных рабочей станции от агента", observationID); err != nil {
			return nil, false, fmt.Errorf("ошибка записи истории агентского обновления: %w", err)
		}
	}

	s.logger.Info("Р абочая станция обновлена",
		"workstation_id", ws.ID,
		"server_id", srv.ID,
		"owner_id", ptrValue(ws.OwnerID),
		"device_name", ptrValue(ws.DeviceName),
		"updated_fields", len(updates),
	)

	return ws, false, nil
}

// applyFiscal создает или обновляет фискальный регистратор по данным наблюдения.
//
// Алгоритм:
// 1. Нормализация серийного номера (uppercase, без пробелов)
// 2. Поиск существующего Р¤Р  по normalized serial
// 3. Проверка на устаревшие данные (observed_at < last_modified_date)
// 4. Создание нового Р¤Р  или обновление существующего (Full Trust)
// 5. При смене владельца вЂ” запись в owner_change_history
//
// Параметры:
//   - tx: транзакция БД
//   - srv: сервер, к которому привязывается Р¤Р  (через Р С)
//   - ws: рабочая станция, к которой привязывается Р¤Р
//   - data: данные от агента
//   - observedAt: время наблюдения
//   - forceOwner: принудительная смена владельца (при подтверждении кандидата)
//   - ownerOverride: переопределение владельца (для network-hub)
//   - ownerChangeSource: источник смены владельца для истории
//
// Возвращает:
//   - *fiscal.FiscalRegister: созданный/обновленный Р¤Р  (nil если нет serial_number)
//   - bool: true если данные устарели (stale)
//   - error: ошибка БД
//
// Особенности:
//   - Full Trust: все поля Р¤Р  обновляются из данных агента безусловно
//   - Серийный номер нормализуется для надежного поиска
//   - Р¤Р  привязывается к Р С, а не напрямую к серверу
func (s *agentObservationRepo) applyFiscal(tx *gorm.DB, observationID uint, srv *server.Server, ws *workstation.Workstation, data *api.AgentDataDTO, observedAt time.Time, forceOwner bool, ownerOverride *string, ownerChangeSource string, updater string) (*fiscal.FiscalRegister, bool, error) {
	// Нормализация серийного номера для поиска
	sn := normalizeSerial(data.SerialNumber)
	if sn == "" {
		return nil, false, nil
	}

	s.logger.Debug("Поиск фискального регистратора",
		"workstation_id", ws.ID,
		"serial_number", strings.TrimSpace(data.SerialNumber),
		"serial_normalized", sn,
		"inn", strings.TrimSpace(data.INN),
	)

	var fr fiscal.FiscalRegister
	err := tx.Where("fr_serial_normalized = ?", sn).First(&fr).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, fmt.Errorf("ошибка поиска Р¤Р : %w", err)
	}

	// Определение целевого владельца
	targetOwner := ptrValue(srv.OwnerID)
	if ownerOverride != nil && strings.TrimSpace(ptrValue(ownerOverride)) != "" {
		targetOwner = strings.TrimSpace(ptrValue(ownerOverride))
	}

	// Создание нового фискального регистратора
	if errors.Is(err, gorm.ErrRecordNotFound) {
		fr = fiscal.FiscalRegister{
			OwnerID:            strPtr(targetOwner),
			WorkstationID:      &ws.ID,
			FRSerialNumber:     strPtr(strings.TrimSpace(data.SerialNumber)),
			FRSerialNormalized: &sn,
			ModelKKT:           strPtr(strings.TrimSpace(data.ModelName)),
			RNKKT:              strPtr(strings.TrimSpace(data.RNM)),
			INN:                strPtr(strings.TrimSpace(data.INN)),
			FNNumber:           strPtr(strings.TrimSpace(data.FNSerial)),
			LegalName:          strPtr(strings.TrimSpace(data.OrganizationName)),
			Address:            strPtr(strings.TrimSpace(data.Address)),
			FRDownloader:       strPtr(strings.TrimSpace(data.BootVersion)),
			DriverVersion:      strPtr(strings.TrimSpace(data.InstalledDriver)),
			FRFirmware:         strPtr(strings.TrimSpace(data.BootVersion)),
			LastModifiedDate:   &observedAt,
			Base:               common.Base{LastUpdatedBy: updater},
		}
		if forceOwner {
			fr.OwnerBindingMode = models.OwnerBindingModeManual
		}
		if t := parseDate(data.DateTimeEnd); t != nil {
			fr.FNExpireDate = t
		}
		if t := parseDate(data.DateTimeReg); t != nil {
			fr.KKTRegDate = t
		}

		if err := tx.Create(&fr).Error; err != nil {
			return nil, false, fmt.Errorf("ошибка создания Р¤Р : %w", err)
		}
		agentUUID := ""
		if isUUID(updater) {
			agentUUID = updater
		}
		if err := s.writeCreationEvent(tx, "FiscalRegister", fr.ID, ptrValue(fr.OwnerID), ownerChangeSource, "Создание фискального регистратора", agentUUID, observationID); err != nil {
			return nil, false, fmt.Errorf("ошибка записи истории создания фискального регистратора: %w", err)
		}

		s.logger.Info("Создан новый фискальный регистратор",
			"fr_id", fr.ID,
			"workstation_id", ws.ID,
			"owner_id", targetOwner,
			"serial_number", ptrValue(fr.FRSerialNumber),
			"model_kkt", ptrValue(fr.ModelKKT),
			"inn", ptrValue(fr.INN),
		)
		return &fr, false, nil
	}

	// Проверка на устаревшие данные
	stale := fr.LastModifiedDate != nil && observedAt.Before(*fr.LastModifiedDate)
	if stale {
		s.logger.Info("Фискальный регистратор не обновлен: устаревшие данные",
			"fr_id", fr.ID,
			"serial_number", ptrValue(fr.FRSerialNumber),
			"observed_at", observedAt.UTC().Format(time.RFC3339),
			"last_modified", fr.LastModifiedDate.UTC().Format(time.RFC3339),
		)
		return &fr, true, nil
	}

	// Обновление существующего Р¤Р  (Full Trust вЂ” все поля обновляются)
	updates := map[string]interface{}{
		"workstation_id":       ws.ID,
		"fr_serial_number":     strings.TrimSpace(data.SerialNumber),
		"fr_serial_normalized": sn,
		"model_kkt":            valOrNil(strPtr(strings.TrimSpace(data.ModelName))),
		"rn_kkt":               valOrNil(strPtr(strings.TrimSpace(data.RNM))),
		"inn":                  valOrNil(strPtr(strings.TrimSpace(data.INN))),
		"fn_number":            valOrNil(strPtr(strings.TrimSpace(data.FNSerial))),
		"legal_name":           valOrNil(strPtr(strings.TrimSpace(data.OrganizationName))),
		"address":              valOrNil(strPtr(strings.TrimSpace(data.Address))),
		"fr_downloader":        valOrNil(strPtr(strings.TrimSpace(data.BootVersion))),
		"driver_version":       valOrNil(strPtr(strings.TrimSpace(data.InstalledDriver))),
		"fr_firmware":          valOrNil(strPtr(strings.TrimSpace(data.BootVersion))),
		"last_modified_date":   observedAt,
		"last_updated_by":      updater,
	}
	if t := parseDate(data.DateTimeEnd); t != nil {
		updates["fn_expire_date"] = *t
	}
	if t := parseDate(data.DateTimeReg); t != nil {
		updates["kkt_reg_date"] = *t
	}

	// Логика обновления владельца
	prevOwner := ptrValue(fr.OwnerID)
	if forceOwner {
		if targetOwner != "" {
			updates["owner_id"] = targetOwner
			updates["owner_binding_mode"] = models.OwnerBindingModeManual
		}
	} else if targetOwner != "" {
		if prevOwner == "" {
			updates["owner_id"] = targetOwner
		} else if strings.TrimSpace(fr.OwnerBindingMode) != models.OwnerBindingModeManual && prevOwner != targetOwner {
			updates["owner_id"] = targetOwner
		}
	}

	if err := tx.Model(&fiscal.FiscalRegister{}).Where("id = ?", fr.ID).Updates(updates).Error; err != nil {
		return nil, false, fmt.Errorf("ошибка обновления Р¤Р : %w", err)
	}

	// Получение обновленной записи
	if err := tx.Where("id = ?", fr.ID).First(&fr).Error; err != nil {
		return nil, false, fmt.Errorf("ошибка получения обновленного Р¤Р : %w", err)
	}

	newOwner := ptrValue(fr.OwnerID)
	agentUUID := ""
	if isUUID(updater) {
		agentUUID = updater
	}

	// Запись истории смены владельца
	if prevOwner != "" && newOwner != "" && prevOwner != newOwner && ownerChangeSource != "" {
		if err := s.writeOwnerChange(tx, "FiscalRegister", fr.ID, prevOwner, newOwner, ownerChangeSource, "Смена владельца фискального регистратора", agentUUID, observationID); err != nil {
			return nil, false, fmt.Errorf("ошибка записи истории владельца: %w", err)
		}
		s.logger.Info("Зафиксирована смена владельца Р¤Р ",
			"fr_id", fr.ID,
			"serial_number", ptrValue(fr.FRSerialNumber),
			"prev_owner_id", prevOwner,
			"new_owner_id", newOwner,
			"source", ownerChangeSource,
		)
	}
	if agentUUID != "" {
		if err := s.writeAgentDataUpdate(tx, "FiscalRegister", fr.ID, newOwner, agentUUID, "Обновление данных фискального регистратора от агента", observationID); err != nil {
			return nil, false, fmt.Errorf("ошибка записи истории агентского обновления: %w", err)
		}
	}

	s.logger.Info("Фискальный регистратор обновлен",
		"fr_id", fr.ID,
		"workstation_id", ws.ID,
		"owner_id", ptrValue(fr.OwnerID),
		"serial_number", ptrValue(fr.FRSerialNumber),
		"updated_fields", len(updates),
	)

	return &fr, false, nil
}

// findWorkstation выполняет поиск рабочей станции по нескольким критериям.
//
// Порядок поиска:
// 1. По identity_hash (SHA256 от TeamViewer:LiteManager) вЂ” самый надежный
// 2. По TeamViewer ID
// 3. По LiteManager ID
// 4. По AnyDesk ID
//
// Параметры:
//   - tx: транзакция БД
//   - data: данные от агента с remote IDs
//   - identityHashValue: предварительно вычисленный identity_hash
//
// Возвращает:
//   - *workstation.Workstation: найденная Р С или nil
//   - error: ошибка БД (не включает ErrRecordNotFound)
func (s *agentObservationRepo) findWorkstation(tx *gorm.DB, data *api.AgentDataDTO, identityHashValue string) (*workstation.Workstation, error) {
	var ws workstation.Workstation

	// 1. Поиск по identity_hash (наиболее надежный)
	if identityHashValue != "" {
		if err := tx.Where("identity_hash = ?", identityHashValue).First(&ws).Error; err == nil {
			s.logger.Debug("Р С найдена по identity_hash",
				"workstation_id", ws.ID,
				"identity_hash", identityHashValue,
				"owner_id", ptrValue(ws.OwnerID),
			)
			return &ws, nil
		}
	}

	// 2. Поиск по TeamViewer ID
	if tv := normRID(data.TeamviewerID); tv != "" {
		if err := tx.Where("teamviewer = ?", tv).First(&ws).Error; err == nil {
			s.logger.Debug("Р С найдена по TeamViewer ID",
				"workstation_id", ws.ID,
				"teamviewer_id", tv,
				"owner_id", ptrValue(ws.OwnerID),
			)
			return &ws, nil
		}
	}

	// 3. Поиск по LiteManager ID
	if lm := normRID(data.LitemanagerID); lm != "" {
		if err := tx.Where("litemanager = ?", lm).First(&ws).Error; err == nil {
			s.logger.Debug("Р С найдена по LiteManager ID",
				"workstation_id", ws.ID,
				"litemanager_id", lm,
				"owner_id", ptrValue(ws.OwnerID),
			)
			return &ws, nil
		}
	}

	// 4. Поиск по AnyDesk ID
	if ad := normRID(data.AnydeskID); ad != "" {
		if err := tx.Where("anydesk = ?", ad).First(&ws).Error; err == nil {
			s.logger.Debug("Р С найдена по AnyDesk ID",
				"workstation_id", ws.ID,
				"anydesk_id", ad,
				"owner_id", ptrValue(ws.OwnerID),
			)
			return &ws, nil
		}
	}

	s.logger.Debug("Р С не найдена по remote IDs",
		"identity_hash", identityHashValue,
		"teamviewer_id", normRID(data.TeamviewerID),
		"litemanager_id", normRID(data.LitemanagerID),
		"anydesk_id", normRID(data.AnydeskID),
	)
	return nil, nil
}

// stage создает или обновляет кандидата для ручной обработки оператором.
//
// Вызывается когда:
//   - Сервер не найден (srv == nil) вЂ” новый сервер, требуется создание
//   - Нет remote IDs для идентификации Р С (!hasRemoteID) вЂ” агент не собрал TeamViewer/LiteManager/AnyDesk.
//     Это нормальная ситуация: агент мог не обнаружить установленные программы удаленного доступа.
//     Администратор должен вручную указать remote IDs при подтверждении кандидата на АО.
//
// Алгоритм:
// 1. Поиск существующего кандидата по CRM ID или server_key
// 2. Создание записи CandidateWorkstationStaging с данными Р С
// 3. Создание записи CandidateFiscalStaging с данными Р¤Р  (если есть serial_number)
// 4. Создание/обновление задачи на подключение ТП
//
// Параметры:
//   - tx: транзакция БД
//   - obs: запись наблюдения для связывания
//   - data: данные от агента
//   - observedAt: время наблюдения
//   - normalizedRMS: нормализованный URL/IP сервера
//   - serverKey: UUID на основе URL
//   - srv: найденный сервер (может быть nil при отсутствии сервера в системе)
//
// Возвращает:
//   - *models.Candidate: созданный или найденный кандидат
//   - error: ошибка БД
//
// Создаваемые записи:
//   - Candidate: основная запись кандидата (статус NEW)
//   - CandidateWorkstationStaging: данные Р С для просмотра оператором
//   - CandidateFiscalStaging: данные Р¤Р  для просмотра оператором
//   - ReconciliationTask: задача на подключение ТП
func (s *agentObservationRepo) stage(tx *gorm.DB, obs *models.AgentObservation, data *api.AgentDataDTO, observedAt time.Time, normalizedRMS, serverKey string, srv *server.Server) (*models.Candidate, error) {
	s.logger.Debug("Создание staging-записи для кандидата",
		"observation_id", obs.ID,
		"server_key", serverKey,
		"server_crm_id", strings.TrimSpace(data.CRMID),
		"hostname", strings.TrimSpace(data.Hostname),
		"has_server", srv != nil,
	)

	var existingServerID *string
	if srv != nil && strings.TrimSpace(srv.ID) != "" {
		existingServerID = &srv.ID
	}
	c, err := s.findOrCreateCandidate(tx, data.CRMID, serverKey, normalizedRMS, existingServerID)
	if err != nil {
		return nil, err
	}
	stageWorkstationID, err := s.resolveStageWorkstationID(tx, data)
	if err != nil {
		return nil, err
	}
	if err := tx.Create(&models.CandidateWorkstationStaging{CandidateID: c.ID, ObservationID: obs.ID, ObservedAt: observedAt, Hostname: strPtr(strings.TrimSpace(data.Hostname)), AgentUUID: strPtr(strings.TrimSpace(data.AgentUUID)), WorkstationUUID: stageWorkstationID, TeamviewerID: normRIDPtr(data.TeamviewerID), LitemanagerID: normRIDPtr(data.LitemanagerID), AnydeskID: normRIDPtr(data.AnydeskID), URLRms: strPtr(normalizedRMS)}).Error; err != nil {
		return nil, err
	}
	if sn := strings.TrimSpace(data.SerialNumber); sn != "" {
		if err := tx.Create(&models.CandidateFiscalStaging{CandidateID: c.ID, ObservationID: obs.ID, ObservedAt: observedAt, SerialNumber: strPtr(sn), SerialNormalized: strPtr(normalizeSerial(sn)), RNKKT: strPtr(strings.TrimSpace(data.RNM)), ModelName: strPtr(strings.TrimSpace(data.ModelName)), INN: strPtr(strings.TrimSpace(data.INN)), FNNumber: strPtr(strings.TrimSpace(data.FNSerial)), FNExpireDate: parseDate(data.DateTimeEnd), OrganizationName: strPtr(strings.TrimSpace(data.OrganizationName)), Address: strPtr(strings.TrimSpace(data.Address))}).Error; err != nil {
			return nil, err
		}
	}
	_ = s.createOrRefreshTask(tx, "candidate_connection", fmt.Sprintf("candidate:%d", c.ID), "Кандидат на подключение ТП", map[string]interface{}{"candidate_id": c.ID, "server_key": c.ServerKey, "server_crm_id": c.ServerCRMID})
	s.logger.Info("Создан/обновлен staging по кандидату",
		"candidate_id", c.ID,
		"observation_id", obs.ID,
		"server_key", serverKey,
		"server_crm_id", strings.TrimSpace(data.CRMID),
	)
	return c, nil
}

// findOrCreateCandidate ищет существующего кандидата или создает нового.
//
// Порядок поиска:
// 1. По CRM ID (исключая подтвержденных)
// 2. По server_key (исключая подтвержденных)
//
// Если кандидат найден вЂ” обновляет existing_server_id если он был пустым.
// Если не найден вЂ” создает нового кандидата со статусом NEW.
func (s *agentObservationRepo) findOrCreateCandidate(tx *gorm.DB, crmID, serverKey, rms string, existingServerID *string) (*models.Candidate, error) {
	var c models.Candidate
	crmID = strings.TrimSpace(crmID)
	if crmID != "" {
		if err := tx.Where("server_crm_id = ? AND status <> ?", crmID, models.CandidateStatusApproved).Order("id desc").First(&c).Error; err == nil {
			if c.ExistingServerID == nil && existingServerID != nil {
				_ = tx.Model(&models.Candidate{}).Where("id = ?", c.ID).Update("existing_server_id", *existingServerID).Error
				c.ExistingServerID = existingServerID
			}
			s.logger.Info("Найден существующий кандидат по CRM ID",
				"candidate_id", c.ID,
				"server_crm_id", crmID,
			)
			return &c, nil
		}
	}
	if strings.TrimSpace(serverKey) != "" {
		if err := tx.Where("server_key = ? AND status <> ?", serverKey, models.CandidateStatusApproved).Order("id desc").First(&c).Error; err == nil {
			if c.ExistingServerID == nil && existingServerID != nil {
				_ = tx.Model(&models.Candidate{}).Where("id = ?", c.ID).Update("existing_server_id", *existingServerID).Error
				c.ExistingServerID = existingServerID
			}
			s.logger.Info("Найден существующий кандидат по server_key",
				"candidate_id", c.ID,
				"server_key", serverKey,
			)
			return &c, nil
		}
	}
	meta, _ := json.Marshal(map[string]interface{}{"server_url": rms})
	c = models.Candidate{
		ServerKey:        strPtr(serverKey),
		ServerCRMID:      strPtr(crmID),
		ServerURL:        strPtr(rms),
		Status:           models.CandidateStatusNew,
		Meta:             datatypes.JSON(meta),
		ExistingServerID: existingServerID,
	}
	if err := tx.Create(&c).Error; err != nil {
		return nil, err
	}
	reason := "Создан новый кандидат по результатам агента"
	_ = tx.Create(&models.CandidateStatusHistory{CandidateID: c.ID, ToStatus: models.CandidateStatusNew, Reason: &reason}).Error
	s.logger.Info("Создан новый кандидат",
		"candidate_id", c.ID,
		"server_key", serverKey,
		"server_crm_id", crmID,
		"existing_server_id", ptrValue(c.ExistingServerID),
	)
	return &c, nil
}

// upsertAgent создает или обновляет запись agent_instance.
// Связывает агента с рабочей станцией и обновляет время последнего heartbeat.
// Если агент не существует вЂ” создается новая запись со статусом ACTIVE.
func (s *agentObservationRepo) upsertAgent(tx *gorm.DB, source string, data *api.AgentDataDTO, wsID string, observedAt time.Time) error {
	agentUUID := strings.TrimSpace(data.AgentUUID)
	if agentUUID == "" && isUUID(source) {
		agentUUID = source
	}
	if agentUUID == "" {
		s.logger.Info("Обновление agent_instance пропущено: не задан agent_uuid", "source", source, "workstation_id", wsID)
		return nil
	}
	detachResult := tx.Model(&models.Agent{}).
		Where("workstation_id = ? AND uuid <> ?", wsID, agentUUID).
		Update("workstation_id", nil)
	if detachResult.Error != nil {
		return detachResult.Error
	}
	if detachResult.RowsAffected > 0 {
		s.logger.Info(
			"Сброшены дублирующие привязки агентов к Р С",
			"workstation_id", wsID,
			"kept_agent_uuid", agentUUID,
			"detached_agents_count", detachResult.RowsAffected,
		)
	}
	var agent models.Agent
	err := tx.Where("uuid = ?", agentUUID).First(&agent).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		agent = models.Agent{UUID: agentUUID, Type: defaultStr(strings.TrimSpace(data.AgentType), "workstation"), Status: models.StatusActive, Hostname: strings.TrimSpace(data.Hostname), Version: strings.TrimSpace(data.AgentVersion), WorkstationID: &wsID, LastHeartbeat: time.Now(), LastObservedAt: &observedAt}
		if err := tx.Create(&agent).Error; err != nil {
			return err
		}
		s.logger.Info("Создан agent_instance", "agent_uuid", agentUUID, "workstation_id", wsID)
		return nil
	}
	updates := map[string]interface{}{"workstation_id": wsID, "last_heartbeat": time.Now()}
	if data.AgentVersion != "" {
		updates["version"] = strings.TrimSpace(data.AgentVersion)
	}
	if data.Hostname != "" {
		updates["hostname"] = strings.TrimSpace(data.Hostname)
	}
	if data.AgentType != "" {
		updates["type"] = strings.TrimSpace(data.AgentType)
	}
	if agent.LastObservedAt == nil || observedAt.After(*agent.LastObservedAt) {
		updates["last_observed_at"] = observedAt
	}
	if err := tx.Model(&models.Agent{}).Where("uuid = ?", agentUUID).Updates(updates).Error; err != nil {
		return err
	}
	s.logger.Info("Обновлен agent_instance", "agent_uuid", agentUUID, "workstation_id", wsID)
	return nil
}

// createOrRefreshTask создает или обновляет задачу сверки (ReconciliationTask).
// Если активная задача уже существует вЂ” обновляет details и comment.
// Если нет вЂ” создает новую задачу со статусом "new".
func (s *agentObservationRepo) createOrRefreshTask(tx *gorm.DB, taskType, entityUUID, comment string, details map[string]interface{}) error {
	var existing models.ReconciliationTask
	err := tx.Where("task_type = ? AND entity_uuid = ? AND status IN ?", taskType, entityUUID, []string{"new", "pending_sd_action", "sd_error"}).Order("id desc").First(&existing).Error
	payload, _ := json.Marshal(details)
	if err == nil {
		return tx.Model(&models.ReconciliationTask{}).Where("id = ?", existing.ID).Updates(map[string]interface{}{"details": datatypes.JSON(payload), "comment": comment}).Error
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return tx.Create(&models.ReconciliationTask{TaskType: taskType, EntityType: "AgentObservation", EntityUUID: entityUUID, Details: datatypes.JSON(payload), Status: "new", Comment: comment}).Error
}

// resolveConflicts разрешает конфликты после применения наблюдения.
// Зарезервировано для будущей реализации.
func (s *agentObservationRepo) resolveConflicts(tx *gorm.DB, obs *models.AgentObservation) error {
	_ = tx
	_ = obs
	return nil
}

// writeOwnerChange записывает историю смены владельца сущности.
// Не записывает если fromOwnerID или toOwnerID пустые или равны.
func (s *agentObservationRepo) writeOwnerChange(tx *gorm.DB, entityType, entityID, fromOwnerID, toOwnerID, source, comment, agentUUID string, observationID uint) error {
	if strings.TrimSpace(fromOwnerID) == "" || strings.TrimSpace(toOwnerID) == "" || strings.TrimSpace(fromOwnerID) == strings.TrimSpace(toOwnerID) {
		return nil
	}
	record := models.OwnerChangeHistory{
		EntityType:    entityType,
		EntityID:      entityID,
		FromOwnerID:   strPtr(fromOwnerID),
		ToOwnerID:     strings.TrimSpace(toOwnerID),
		ChangeSource:  strings.TrimSpace(source),
		Comment:       strPtr(comment),
		AgentUUID:     strPtr(agentUUID),
		ObservationID: uintPtrOrNil(observationID),
	}
	return tx.Create(&record).Error
}

func (s *agentObservationRepo) writeAgentDataUpdate(tx *gorm.DB, entityType, entityID, ownerID, agentUUID, comment string, observationID uint) error {
	ownerID = strings.TrimSpace(ownerID)
	agentUUID = strings.TrimSpace(agentUUID)
	if ownerID == "" || agentUUID == "" {
		return nil
	}
	record := models.OwnerChangeHistory{
		EntityType:    entityType,
		EntityID:      entityID,
		FromOwnerID:   strPtr(ownerID),
		ToOwnerID:     ownerID,
		ChangeSource:  models.OwnerChangeSourceAgentDataUpdate,
		Comment:       strPtr(comment),
		AgentUUID:     strPtr(agentUUID),
		ObservationID: uintPtrOrNil(observationID),
	}
	return tx.Create(&record).Error
}

func (s *agentObservationRepo) writeCreationEvent(tx *gorm.DB, entityType, entityID, ownerID, source, comment, agentUUID string, observationID uint) error {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return nil
	}
	source = strings.TrimSpace(source)
	if source == "" {
		source = models.OwnerChangeSourceCreated
	}
	record := models.OwnerChangeHistory{
		EntityType:    entityType,
		EntityID:      entityID,
		ToOwnerID:     ownerID,
		ChangeSource:  source,
		Comment:       strPtr(comment),
		AgentUUID:     strPtr(strings.TrimSpace(agentUUID)),
		ObservationID: uintPtrOrNil(observationID),
	}
	return tx.Create(&record).Error
}

func uintPtrOrNil(value uint) *uint {
	if value == 0 {
		return nil
	}
	out := value
	return &out
}

// isNetworkHubServer проверяет, является ли сервер network-hub.
// Network-hub вЂ” это сервер, владелец которого имеет owner_mode = "network_hub".
// Такие серверы автоматически распределяют наблюдения по дочерним компаниям.
func (s *agentObservationRepo) isNetworkHubServer(tx *gorm.DB, srv *server.Server) (bool, error) {
	if srv == nil || srv.OwnerID == nil || strings.TrimSpace(*srv.OwnerID) == "" {
		return false, nil
	}
	var owner company.Company
	if err := tx.Where("id = ?", strings.TrimSpace(*srv.OwnerID)).First(&owner).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	return strings.TrimSpace(owner.OwnerMode) == models.CompanyOwnerModeNetworkHub, nil
}

// resolveNetworkOwner пытается автоматически определить владельца для network-hub сервера.
//
// Алгоритм:
// 1. Получение списка дочерних компаний hub-компании
// 2. Поиск существующего Р¤Р  по серийному номеру среди дочерних компаний
// 3. Поиск существующей Р С по remote IDs среди дочерних компаний
// 4. Если найден ровно один уникальный владелец вЂ” возвращаем его с confident=true
// 5. Если найдено 0 или >1 владельцев вЂ” возвращаем confident=false
//
// Параметры:
//   - tx: транзакция БД
//   - hubCompanyID: ID hub-компании (владелец сервера)
//   - data: данные от агента для поиска существующих сущностей
//
// Возвращает:
//   - string: ID определенного владельца (пустая строка если не определен)
//   - bool: true если владелец определен уверенно (ровно один кандидат)
//   - error: ошибка БД
//
// Логика скоринга:
//   - Р¤Р  с совпадающим serial дает кандидата на владельца
//   - Р С с совпадающим remote ID дает кандидата на владельца
//   - Если все кандидаты указывают на одну компанию вЂ” автоматическое присвоение
//   - Если кандидаты указывают на разные компании вЂ” требуется ручной выбор
func (s *agentObservationRepo) resolveNetworkOwner(tx *gorm.DB, hubCompanyID string, data *api.AgentDataDTO) (string, bool, error) {
	if strings.TrimSpace(hubCompanyID) == "" {
		return "", false, nil
	}

	// Получение дочерних компаний hub-компании
	var children []company.Company
	if err := tx.Where("parent_id = ?", hubCompanyID).Find(&children).Error; err != nil {
		return "", false, fmt.Errorf("ошибка получения дочерних компаний: %w", err)
	}

	if len(children) == 0 {
		s.logger.Debug("У hub-компании нет дочерних компаний",
			"hub_company_id", hubCompanyID,
		)
		return "", false, nil
	}

	childIDs := make([]string, 0, len(children))
	for _, child := range children {
		childIDs = append(childIDs, child.ID)
	}

	s.logger.Debug("Поиск владельца среди дочерних компаний",
		"hub_company_id", hubCompanyID,
		"children_count", len(children),
		"serial_number", strings.TrimSpace(data.SerialNumber),
		"teamviewer_id", normRID(data.TeamviewerID),
		"litemanager_id", normRID(data.LitemanagerID),
		"anydesk_id", normRID(data.AnydeskID),
	)

	// Сбор кандидатов на владельца
	owners := map[string]struct{}{}

	// Поиск Р¤Р  по серийному номеру среди дочерних компаний
	if sn := normalizeSerial(data.SerialNumber); sn != "" {
		var fr fiscal.FiscalRegister
		if err := tx.Where("fr_serial_normalized = ? AND owner_id IN ?", sn, childIDs).First(&fr).Error; err == nil && fr.OwnerID != nil {
			owners[strings.TrimSpace(*fr.OwnerID)] = struct{}{}
			s.logger.Debug("Найден Р¤Р  среди дочерних компаний",
				"serial_normalized", sn,
				"fr_owner_id", ptrValue(fr.OwnerID),
			)
		} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return "", false, fmt.Errorf("ошибка поиска Р¤Р : %w", err)
		}
	}

	// Поиск Р С по remote IDs среди дочерних компаний
	conditions := []string{}
	values := []interface{}{}
	if tv := normRID(data.TeamviewerID); tv != "" {
		conditions = append(conditions, "teamviewer = ?")
		values = append(values, tv)
	}
	if lm := normRID(data.LitemanagerID); lm != "" {
		conditions = append(conditions, "litemanager = ?")
		values = append(values, lm)
	}
	if ad := normRID(data.AnydeskID); ad != "" {
		conditions = append(conditions, "anydesk = ?")
		values = append(values, ad)
	}

	if len(conditions) > 0 {
		var list []workstation.Workstation
		if err := tx.Where("owner_id IN ?", childIDs).Where(strings.Join(conditions, " OR "), values...).Find(&list).Error; err != nil {
			return "", false, fmt.Errorf("ошибка поиска Р С: %w", err)
		}
		for i := range list {
			if list[i].OwnerID != nil && strings.TrimSpace(*list[i].OwnerID) != "" {
				owners[strings.TrimSpace(*list[i].OwnerID)] = struct{}{}
			}
		}
		if len(list) > 0 {
			s.logger.Debug("Найдены Р С среди дочерних компаний",
				"workstations_count", len(list),
				"unique_owners", len(owners),
			)
		}
	}

	// Анализ кандидатов
	if len(owners) != 1 {
		s.logger.Debug("Владелец не определен: нет или несколько кандидатов",
			"hub_company_id", hubCompanyID,
			"candidates_count", len(owners),
		)
		return "", false, nil
	}

	// РР·РІР»РµС‡РµРЅРёРµ единственного владельца
	for ownerID := range owners {
		s.logger.Info("Владелец автоматически определен",
			"hub_company_id", hubCompanyID,
			"resolved_owner_id", ownerID,
			"method", "network_hub_resolution",
		)
		return ownerID, true, nil
	}

	return "", false, nil
}

// stageNetworkCandidate создает или обновляет network-кандидата для network-hub сервера.
//
// Вызывается когда:
//   - Сервер найден и является network-hub (владелец вЂ” hub-компания)
//   - Автоматическое определение владельца невозможно (0 или >1 кандидатов)
//
// Алгоритм:
// 1. Поиск существующего NetworkCandidate по hub_company_id и server_id
// 2. Создание новой записи NetworkCandidate если не найдена
// 3. Создание NetworkCandidateGroup для связи с наблюдением
// 4. Создание записей NetworkCandidateWSStaging с данными Р С
// 5. Создание записей NetworkCandidateFRStaging с данными Р¤Р
//
// Параметры:
//   - tx: транзакция БД
//   - obs: запись наблюдения для связывания
//   - data: данные от агента
//   - observedAt: время наблюдения
//   - normalizedRMS: нормализованный URL/IP сервера
//   - serverKey: UUID на основе URL
//   - srv: найденный network-hub сервер (обязателен)
//
// Возвращает:
//   - *models.NetworkCandidate: созданный или найденный network-кандидат
//   - error: ошибка БД или если сервер/владелец не найдены
//
// Особенности:
//   - NetworkCandidate группирует наблюдения от одного hub-сервера
//   - Оператор должен вручную выбрать компанию-владельца из дочерних компаний hub-а
//   - После выбора владельца создаются Р С и Р¤Р  с указанным владельцем
func (s *agentObservationRepo) stageNetworkCandidate(tx *gorm.DB, obs *models.AgentObservation, data *api.AgentDataDTO, observedAt time.Time, normalizedRMS, serverKey string, srv *server.Server) (*models.NetworkCandidate, error) {
	if srv == nil || srv.OwnerID == nil || strings.TrimSpace(*srv.OwnerID) == "" {
		s.logger.Error("Ошибка создания network-candidate: сервер или владелец не найдены",
			"observation_id", obs.ID,
			"has_server", srv != nil,
			"has_owner_id", srv != nil && srv.OwnerID != nil,
		)
		return nil, errors.New("для network-кандидата не найден сервер или его владелец")
	}

	s.logger.Debug("Создание network-candidate для hub-сервера",
		"observation_id", obs.ID,
		"server_id", srv.ID,
		"hub_company_id", ptrValue(srv.OwnerID),
		"server_key", serverKey,
	)
	var candidate models.NetworkCandidate
	err := tx.Where("hub_company_id = ? AND server_id = ? AND status IN ?", strings.TrimSpace(*srv.OwnerID), srv.ID, []string{models.NetworkCandidateStatusNew, models.NetworkCandidateStatusInReview}).
		Order("id desc").
		First(&candidate).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		candidate = models.NetworkCandidate{
			Status:       models.NetworkCandidateStatusNew,
			HubCompanyID: strings.TrimSpace(*srv.OwnerID),
			ServerID:     srv.ID,
			ServerKey:    strPtr(serverKey),
			ServerCRMID:  strPtr(strings.TrimSpace(data.CRMID)),
			ServerURL:    strPtr(normalizedRMS),
		}
		if err := tx.Create(&candidate).Error; err != nil {
			return nil, err
		}
	}

	var group models.NetworkCandidateGroup
	if err := tx.Where("candidate_id = ? AND observation_id = ?", candidate.ID, obs.ID).First(&group).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		group = models.NetworkCandidateGroup{
			CandidateID:   candidate.ID,
			ObservationID: obs.ID,
			Status:        models.NetworkCandidateGroupStatusActive,
		}
		if err := tx.Create(&group).Error; err != nil {
			return nil, err
		}
	}

	stageWorkstationID, err := s.resolveStageWorkstationID(tx, data)
	if err != nil {
		return nil, err
	}
	var wsCount int64
	if err := tx.Model(&models.NetworkCandidateWSStaging{}).Where("group_id = ?", group.ID).Count(&wsCount).Error; err != nil {
		return nil, err
	}
	if wsCount == 0 {
		wsStage := models.NetworkCandidateWSStaging{
			GroupID:         group.ID,
			ObservedAt:      observedAt,
			Hostname:        strPtr(strings.TrimSpace(data.Hostname)),
			AgentUUID:       strPtr(strings.TrimSpace(data.AgentUUID)),
			WorkstationUUID: stageWorkstationID,
			TeamviewerID:    normRIDPtr(data.TeamviewerID),
			LitemanagerID:   normRIDPtr(data.LitemanagerID),
			AnydeskID:       normRIDPtr(data.AnydeskID),
			URLRms:          strPtr(normalizedRMS),
		}
		if err := tx.Create(&wsStage).Error; err != nil {
			return nil, err
		}
	}

	if sn := strings.TrimSpace(data.SerialNumber); sn != "" {
		frStage := models.NetworkCandidateFRStaging{
			GroupID:          group.ID,
			ObservedAt:       observedAt,
			SerialNumber:     strPtr(sn),
			SerialNormalized: strPtr(normalizeSerial(sn)),
			RNKKT:            strPtr(strings.TrimSpace(data.RNM)),
			ModelName:        strPtr(strings.TrimSpace(data.ModelName)),
			INN:              strPtr(strings.TrimSpace(data.INN)),
			FNNumber:         strPtr(strings.TrimSpace(data.FNSerial)),
			FNExpireDate:     parseDate(data.DateTimeEnd),
			OrganizationName: strPtr(strings.TrimSpace(data.OrganizationName)),
			Address:          strPtr(strings.TrimSpace(data.Address)),
		}
		if err := tx.Create(&frStage).Error; err != nil {
			return nil, err
		}
		s.logger.Debug("Создана FR-staging запись для network-candidate",
			"candidate_id", candidate.ID,
			"group_id", group.ID,
			"serial_number", sn,
		)
	}

	s.logger.Info("Network-candidate создан/обновлен",
		"candidate_id", candidate.ID,
		"hub_company_id", ptrValue(srv.OwnerID),
		"server_id", srv.ID,
		"observation_id", obs.ID,
		"status", candidate.Status,
	)
	return &candidate, nil
}

// stageNetworkCandidateWithConflict создает network-кандидата с информацией о конфликте владельцев.
//
// Вызывается когда:
//   - Сервер найден и является network-hub
//   - OwnerResolver обнаружил конфликт (WS и Р¤Р  указывают на разные компании)
//
// Алгоритм аналогичен stageNetworkCandidate, но дополнительно:
//   - Сохраняет информацию о конфликте в поле ConflictInfo
//   - Заполняет WSOwnerCandidate и FROwnerCandidate для отображения оператору
//
// Параметры:
//   - tx: транзакция БД
//   - obs: запись наблюдения для связывания
//   - data: данные от агента
//   - observedAt: время наблюдения
//   - normalizedRMS: нормализованный URL/IP сервера
//   - serverKey: UUID на основе URL
//   - srv: найденный network-hub сервер (обязателен)
//   - resolution: результат разрешения владельца с информацией о конфликте
//
// Возвращает:
//   - *models.NetworkCandidate: созданный или найденный network-кандидат
//   - error: ошибка БД или если сервер/владелец не найдены
func (s *agentObservationRepo) stageNetworkCandidateWithConflict(tx *gorm.DB, obs *models.AgentObservation, data *api.AgentDataDTO, observedAt time.Time, normalizedRMS, serverKey string, srv *server.Server, resolution *domainServices.OwnerResolution) (*models.NetworkCandidate, error) {
	if srv == nil || srv.OwnerID == nil || strings.TrimSpace(*srv.OwnerID) == "" {
		s.logger.Error("Ошибка создания network-candidate: сервер или владелец не найдены",
			"observation_id", obs.ID,
			"has_server", srv != nil,
			"has_owner_id", srv != nil && srv.OwnerID != nil,
		)
		return nil, errors.New("для network-кандидата не найден сервер или его владелец")
	}

	s.logger.Debug("Создание network-candidate с конфликтом для hub-сервера",
		"observation_id", obs.ID,
		"server_id", srv.ID,
		"hub_company_id", ptrValue(srv.OwnerID),
		"server_key", serverKey,
		"has_ws_match", resolution.WSMatch != nil,
		"has_fr_match", resolution.FRMatch != nil,
	)

	// Формирование информации о конфликте
	var conflictInfoStr string
	var wsOwnerCandidate, frOwnerCandidate *string
	if resolution.WSMatch != nil {
		wsOwnerCandidate = strPtr(resolution.WSMatch.OwnerID)
	}
	if resolution.FRMatch != nil {
		frOwnerCandidate = strPtr(resolution.FRMatch.OwnerID)
	}
	if resolution.ConflictInfo != "" {
		conflictInfoStr = resolution.ConflictInfo
	} else {
		// Формируем описание конфликта
		parts := []string{}
		if resolution.WSMatch != nil {
			parts = append(parts, fmt.Sprintf("Р С найдена у владельца %s (по %s)", resolution.WSMatch.OwnerID, resolution.WSMatch.MatchBy))
		}
		if resolution.FRMatch != nil {
			parts = append(parts, fmt.Sprintf("Р¤Р  найден у владельца %s (по %s)", resolution.FRMatch.OwnerID, resolution.FRMatch.MatchBy))
		}
		conflictInfoStr = strings.Join(parts, "; ")
	}

	var candidate models.NetworkCandidate
	err := tx.Where("hub_company_id = ? AND server_id = ? AND status IN ?", strings.TrimSpace(*srv.OwnerID), srv.ID, []string{models.NetworkCandidateStatusNew, models.NetworkCandidateStatusInReview}).
		Order("id desc").
		First(&candidate).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		candidate = models.NetworkCandidate{
			Status:           models.NetworkCandidateStatusNew,
			HubCompanyID:     strings.TrimSpace(*srv.OwnerID),
			ServerID:         srv.ID,
			ServerKey:        strPtr(serverKey),
			ServerCRMID:      strPtr(strings.TrimSpace(data.CRMID)),
			ServerURL:        strPtr(normalizedRMS),
			ConflictInfo:     strPtr(conflictInfoStr),
			WSOwnerCandidate: wsOwnerCandidate,
			FROwnerCandidate: frOwnerCandidate,
		}
		if err := tx.Create(&candidate).Error; err != nil {
			return nil, err
		}
	} else {
		// Обновляем информацию о конфликте в существующем кандидате
		updates := map[string]interface{}{
			"conflict_info":      valOrNil(strPtr(conflictInfoStr)),
			"ws_owner_candidate": valOrNil(wsOwnerCandidate),
			"fr_owner_candidate": valOrNil(frOwnerCandidate),
		}
		if err := tx.Model(&models.NetworkCandidate{}).Where("id = ?", candidate.ID).Updates(updates).Error; err != nil {
			return nil, err
		}
	}

	var group models.NetworkCandidateGroup
	if err := tx.Where("candidate_id = ? AND observation_id = ?", candidate.ID, obs.ID).First(&group).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		group = models.NetworkCandidateGroup{
			CandidateID:   candidate.ID,
			ObservationID: obs.ID,
			Status:        models.NetworkCandidateGroupStatusActive,
		}
		if err := tx.Create(&group).Error; err != nil {
			return nil, err
		}
	}

	stageWorkstationID, err := s.resolveStageWorkstationID(tx, data)
	if err != nil {
		return nil, err
	}
	var wsCount int64
	if err := tx.Model(&models.NetworkCandidateWSStaging{}).Where("group_id = ?", group.ID).Count(&wsCount).Error; err != nil {
		return nil, err
	}
	if wsCount == 0 {
		wsStage := models.NetworkCandidateWSStaging{
			GroupID:         group.ID,
			ObservedAt:      observedAt,
			Hostname:        strPtr(strings.TrimSpace(data.Hostname)),
			AgentUUID:       strPtr(strings.TrimSpace(data.AgentUUID)),
			WorkstationUUID: stageWorkstationID,
			TeamviewerID:    normRIDPtr(data.TeamviewerID),
			LitemanagerID:   normRIDPtr(data.LitemanagerID),
			AnydeskID:       normRIDPtr(data.AnydeskID),
			URLRms:          strPtr(normalizedRMS),
		}
		if err := tx.Create(&wsStage).Error; err != nil {
			return nil, err
		}
	}

	if sn := strings.TrimSpace(data.SerialNumber); sn != "" {
		frStage := models.NetworkCandidateFRStaging{
			GroupID:          group.ID,
			ObservedAt:       observedAt,
			SerialNumber:     strPtr(sn),
			SerialNormalized: strPtr(normalizeSerial(sn)),
			RNKKT:            strPtr(strings.TrimSpace(data.RNM)),
			ModelName:        strPtr(strings.TrimSpace(data.ModelName)),
			INN:              strPtr(strings.TrimSpace(data.INN)),
			FNNumber:         strPtr(strings.TrimSpace(data.FNSerial)),
			FNExpireDate:     parseDate(data.DateTimeEnd),
			OrganizationName: strPtr(strings.TrimSpace(data.OrganizationName)),
			Address:          strPtr(strings.TrimSpace(data.Address)),
		}
		if err := tx.Create(&frStage).Error; err != nil {
			return nil, err
		}
		s.logger.Debug("Создана FR-staging запись для network-candidate с конфликтом",
			"candidate_id", candidate.ID,
			"group_id", group.ID,
			"serial_number", sn,
		)
	}

	s.logger.Info("Network-candidate с конфликтом создан/обновлен",
		"candidate_id", candidate.ID,
		"hub_company_id", ptrValue(srv.OwnerID),
		"server_id", srv.ID,
		"observation_id", obs.ID,
		"status", candidate.Status,
		"ws_owner_candidate", ptrValue(wsOwnerCandidate),
		"fr_owner_candidate", ptrValue(frOwnerCandidate),
	)
	return &candidate, nil
}

// isStaleByAgentStream проверяет, являются ли данные устаревшими по сравнению
// с последним наблюдением от того же агента.
// РСЃРїРѕР»СЊР·СѓРµС‚СЃСЏ для защиты от обработки старых данных при восстановлении связи.
// Возвращает (true, lastObservedAt) если observedAt < lastObservedAt.
func (s *agentObservationRepo) isStaleByAgentStream(tx *gorm.DB, source string, data *api.AgentDataDTO, observedAt time.Time) (bool, time.Time, error) {
	agentUUID := strings.TrimSpace(data.AgentUUID)
	if agentUUID == "" && isUUID(source) {
		agentUUID = strings.TrimSpace(source)
	}
	if agentUUID == "" {
		return false, time.Time{}, nil
	}
	var agent models.Agent
	if err := tx.Where("uuid = ?", agentUUID).First(&agent).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, time.Time{}, nil
		}
		return false, time.Time{}, err
	}
	if agent.LastObservedAt == nil {
		return false, time.Time{}, nil
	}
	last := agent.LastObservedAt.UTC()
	return observedAt.Before(last), last, nil
}

// parseObservedAt парсит время наблюдения из строки агента.
// Поддерживает форматы: "2006-01-02 15:04:05", RFC3339, RFC3339Nano, "2006-01-02T15:04:05".
// Если строка пустая или невалидная вЂ” возвращает текущее время UTC.
func parseObservedAt(v string) time.Time {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Now().UTC()
	}
	layouts := []string{"2006-01-02 15:04:05", time.RFC3339, time.RFC3339Nano, "2006-01-02T15:04:05"}
	for _, l := range layouts {
		if t, err := time.Parse(l, v); err == nil {
			return t.UTC()
		}
	}
	return time.Now().UTC()
}

// parseDate парсит дату из строки (дата окончания ФН, дата регистрации ККТ).
// Поддерживает форматы: "2006-01-02", "2006-01-02 15:04:05", RFC3339, "02.01.2006", "02.01.2006 15:04:05".
// Возвращает nil если строка пустая или невалидная.
func parseDate(v string) *time.Time {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	layouts := []string{"2006-01-02", "2006-01-02 15:04:05", time.RFC3339, "02.01.2006", "02.01.2006 15:04:05"}
	for _, l := range layouts {
		if t, err := time.Parse(l, v); err == nil {
			t = t.UTC()
			return &t
		}
	}
	return nil
}

// normalizeRMS нормализует URL/IP сервера RMS.
// РР·РІР»РµРєР°РµС‚ хост и порт, добавляет порт 8080 если не указан.
// Примеры: "SERVER.DOMAIN.RU:443" -> "server.domain.ru:443", "192.168.1.1" -> "192.168.1.1:8080"
func normalizeRMS(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	withSchema := raw
	if !strings.Contains(withSchema, "://") {
		withSchema = "http://" + withSchema
	}
	parsed, err := url.Parse(withSchema)
	if err != nil {
		return strings.ToLower(raw)
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host == "" {
		return strings.ToLower(raw)
	}
	port := strings.TrimSpace(parsed.Port())
	if port == "" {
		port = "8080"
	}
	return host + ":" + port
}

// isLocalRMS проверяет, является ли адрес локальным.
// Локальные адреса: localhost, 127.x.x.x, 10.x.x.x, 172.16-31.x.x, 192.168.x.x, 169.254.x.x
// Такие адреса игнорируются при обработке наблюдений.
func isLocalRMS(rms string) bool {
	host := strings.TrimSpace(strings.ToLower(rms))
	if strings.Contains(host, ":") {
		if u, err := url.Parse("http://" + host); err == nil {
			host = u.Hostname()
		}
	}
	if host == "" || host == "localhost" || host == "127.0.0.1" || strings.HasPrefix(host, "127.") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	cidrs := []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16", "127.0.0.0/8"}
	for _, c := range cidrs {
		_, block, _ := net.ParseCIDR(c)
		if block.Contains(ip) {
			return true
		}
	}
	return false
}

// buildServerKey генерирует UUID v5 (SHA1) на основе нормализованного URL.
// РСЃРїРѕР»СЊР·СѓРµС‚СЃСЏ для идентификации сервера при отсутствии CRM ID.
func buildServerKey(rms string) string {
	rms = strings.TrimSpace(strings.ToLower(rms))
	if rms == "" {
		return ""
	}
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(rms)).String()
}

// normalizeSerial нормализует серийный номер Р¤Р .
// Приводит к верхнему регистру и удаляет пробелы.
// Пример: "123 456 789" -> "123456789"
func normalizeSerial(v string) string {
	v = strings.ToUpper(strings.TrimSpace(v))
	return strings.ReplaceAll(v, " ", "")
}

// normRID нормализует remote ID (TeamViewer, LiteManager, AnyDesk).
// Удаляет пробелы и игнорирует значение "none".
func normRID(v string) string {
	v = strings.TrimSpace(v)
	if strings.EqualFold(v, "none") || v == "" {
		return ""
	}
	return strings.ReplaceAll(v, " ", "")
}

// normRIDPtr нормализует remote ID и возвращает указатель.
// Возвращает nil если значение пустое или "none".
func normRIDPtr(v string) *string {
	n := normRID(v)
	if n == "" {
		return nil
	}
	return &n
}

// hasRemoteID проверяет наличие хотя бы одного remote ID для идентификации рабочей станции.
//
// Проверяемые идентификаторы:
//   - TeamViewer ID (teamviewer_id)
//   - LiteManager ID (litemanager_id)
//   - AnyDesk ID (anydesk_id)
//
// Возвращает true, если хотя бы один ID присутствует.
// Если все ID отсутствуют вЂ” создание Р С невозможно, наблюдение отправляется в staging.
// В этом случае администратор должен вручную указать remote IDs при подтверждении кандидата.
func hasRemoteID(data *api.AgentDataDTO) bool {
	return normRID(data.TeamviewerID) != "" || normRID(data.LitemanagerID) != "" || normRID(data.AnydeskID) != ""
}

// resolveStageWorkstationID ищет уже существующую Р С по remote IDs и
// возвращает ее реальный ID для сохранения в staging.
// Если Р С не найдена, возвращает nil.
func (s *agentObservationRepo) resolveStageWorkstationID(tx *gorm.DB, data *api.AgentDataDTO) (*string, error) {
	identity := identityHash(data.TeamviewerID, data.LitemanagerID)
	ws, err := s.findWorkstation(tx, data, identity)
	if err != nil {
		return nil, fmt.Errorf("ошибка поиска Р С для staging: %w", err)
	}
	if ws == nil {
		return nil, nil
	}
	wsID := strings.TrimSpace(ws.ID)
	if wsID == "" {
		return nil, nil
	}
	return &wsID, nil
}

// identityHash вычисляет SHA256 хеш от пары TeamViewer:LiteManager.
// РСЃРїРѕР»СЊР·СѓРµС‚СЃСЏ для поиска существующей Р С по identity_hash.
func identityHash(tv, lm string) string {
	tv = normRID(tv)
	lm = normRID(lm)
	if tv == "" || lm == "" {
		return ""
	}
	s := sha256.Sum256([]byte(tv + ":" + lm))
	return hex.EncodeToString(s[:])
}

// payloadDigest вычисляет SHA256 хеш от всего payload и возвращает JSON.
// РСЃРїРѕР»СЊР·СѓРµС‚СЃСЏ для идемпотентности вЂ” дубликаты по хешу пропускаются.
func payloadDigest(data *api.AgentDataDTO) (string, datatypes.JSON, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return "", nil, err
	}
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:]), datatypes.JSON(b), nil
}

// isUUID проверяет, является ли строка валидным UUID.
func resolveAgentUpdater(source string, data *api.AgentDataDTO) string {
	if data != nil {
		if agentUUID := strings.TrimSpace(data.AgentUUID); agentUUID != "" {
			return agentUUID
		}
	}
	src := strings.TrimSpace(source)
	if isUUID(src) {
		return src
	}
	return "agent"
}

func isUUID(v string) bool {
	_, err := uuid.Parse(strings.TrimSpace(v))
	return err == nil
}

// defaultStr возвращает значение или fallback если значение пустое.
func defaultStr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

// strPtr создает указатель на строку, возвращая nil для пустой строки.
func strPtr(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return &v
}

// valOrNil возвращает значение указателя или nil для пустой строки.
// РСЃРїРѕР»СЊР·СѓРµС‚СЃСЏ для GORM Updates вЂ” nil означает "не обновлять поле".
func valOrNil(v *string) interface{} {
	if v == nil || strings.TrimSpace(*v) == "" {
		return nil
	}
	return strings.TrimSpace(*v)
}

// ptrValue безопасно возвращает значение строкового указателя.
func ptrValue(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}

func hasServerInput(in CandidateApproveInput) bool {
	if strings.TrimSpace(ptrValue(in.ServerID)) != "" {
		return true
	}
	if strings.TrimSpace(ptrValue(in.ServerCRMID)) != "" {
		return true
	}
	if strings.TrimSpace(ptrValue(in.ServerURL)) != "" {
		return true
	}
	if strings.TrimSpace(ptrValue(in.ServerUniqueID)) != "" {
		return true
	}
	if strings.TrimSpace(ptrValue(in.ServerCabinetLink)) != "" {
		return true
	}
	if strings.TrimSpace(ptrValue(in.ServerName)) != "" {
		return true
	}
	if strings.TrimSpace(ptrValue(in.ServerDesc)) != "" {
		return true
	}
	return false
}

var cabinetIDRegex = regexp.MustCompile(`\d+`)

// extractCabinetClientID извлекает числовой идентификатор кабинета из ссылки.
func extractCabinetClientID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	return cabinetIDRegex.FindString(raw)
}
