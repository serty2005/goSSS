# План рефакторинга goSSS: декомпозиция монолита на распределённые сервисы

> Документ-план. Сформирован по результатам анализа репозитория на 2026-06-15.
> Обновлён: 2026-06-15 — Фаза 1 реализована.
> Не является описанием текущего состояния — это целевая архитектура и дорожная карта перехода.

## Зафиксированные архитектурные решения

- **Подход**: гибридный — чистый старт для новых сервисов, текущий монолит остаётся working, поэтапно превращается в `operator-api`.
- **Сервисы**: `agent-gateway` (stateless), `integration-hub` (central хост), `processing-service` (событийный), `operator-api` (backend фронта).
- **Данные**: единая logical БД → PostgreSQL master + региональные read-реплики (streaming replication).
- **Брокер**: NATS JetStream заменяет `pkg/eventbus.InMemoryEventBus` и становится транспортной шиной между подами/кластерами.
- **Репозиторий**: остаётся один Go-модуль `etalon-server`; новые бинари в `cmd/*` импортируют общие пакеты из `internal/`. Per-service разнесение пакетов — отдельный второй этап, не блокирующий декомпозицию.

## Целевая топология

```
                       ┌─────────────────────────────────────────────────────┐
                       │  Центральный кластер (master)                       │
   внешние системы ───▶│  integration-hub (вебхуки, sync, шлюзы, singleton)  │
   (Bitrix/Pyrus/      │  processing-service (потребитель NATS, engine)      │
    МегаФон/Naumen/    │  operator-api (REST/SSE для UI)                     │
    IMAP/iiko/S3)      │  PostgreSQL master · NATS JetStream cluster         │
                       │  Redis (стримы вебхуков, suppression-ключи)         │
                       └───────────────┬─────────────────────────────────────┘
                                       │ streaming replication (async)
                       ┌───────────────┴─────────────────────────────────────┐
                       │  Региональные кластеры (read-реплика)               │
   Windows-агенты ────▶│  agent-gateway (stateless, HPA, любая гео-точка)    │
   (getad/sssruner)    │  PostgreSQL read-реплика · NATS edge leaf           │
                       └─────────────────────────────────────────────────────┘
```

Принципы:
- agent-gateway масштабируется горизонтально в любом регионе, не имеет in-memory/session-состояния, не владеет данными.
- Запись идёт в master (accept RTT latency), чтение — из локальной реплики.
- События между сервисами — только через NATS JetStream, in-process шина упраздняется.
- Контракты агрегатов (tickets/companies/equipment/users) остаются в общей БД до стабилизации границ; целевой database-per-service — за рамками плана, т.к. master+replicas уже даёт «единый набор сущностей для всех кластеров».

---

## Фаза 0 — Фундамент (≈1–2 недели)

Цель: ввести NATS JetStream и сделать переключаемую реализацию `EventBus`, не ломая текущий монолит.

### 0.1. NATS JetStream в инфраструктуре
- Добавить `nats`-сервис в `docker/docker-compose.yml` (кластер из 3 узлов для HA, JetStream persistence в volume).
- В `internal/infra/config/config.go` добавить секцию NATS: `NATS_URLS`, `NATS_CREDS_FILE`, `NATS_STREAM_PREFIX` (default `sss`), `NATS_MAX_AGE`, `EVENT_BUS_BACKEND` (`memory|nats`, default `memory` на переходе).

### 0.2. NATS-реализация интерфейса EventBus
- Новый файл `pkg/eventbus/nats_bus.go`: реализация того же интерфейса `eventbus.EventBus` (`Publish`, `Subscribe`, `SubscribeChannel`) поверх JetStream.
- Сериализация payload — через `encoding/json` + сохранившиеся typed-функции из `internal/core/events/events.go`.
- Streams (по доменам, чтобы consumer-groups можно было разносить): `sss.agent`, `sss.integration`, `sss.processing`, `sss.domain` (для SSE broadcast).
- Subjects — напрямую из существующих констант `events.go` (напр. `agent.data.received`, `bitrix.ticket.sync.requested`). Переименование событий не требуется.
- `SubscribeChannel` для SSE — через JetStream consumer + in-process канал; durable-имя по подписчику.
- Внимание: payload-ы событий содержат `interface{}` поля (например, `Data interface{}` в `ServiceDeskEntityPayload`) и прямые ссылки на доменные типы (`tickets.Ticket`, `api.AgentDataDTO`). Для десериализации потребуется тип-реестр (`map[eventType]reflect.Type`) или json.RawMessage + per-type decode на стороне потребителя.

### 0.3. Фабрика шины в `internal/app/app.go`
- В `New()` (строка 143) заменить `eventbus.NewInMemoryEventBus(10000)` на фабрику `eventbus.New(cfg)` — выбирает backend по `EVENT_BUS_BACKEND`.
- Параметризация: in-memory и NATS реализации за одним интерфейсом, переключение без изменения потребителей.
- Двойная публикация на переходе НЕ делается (избыточно, даёт дубль-обработку); переход — flip-flop флагом с канареечным выкатом.

### 0.4. Документация
- В `AGENTS.md` секция «Распределённая архитектура»: описание backend-ов шины, перечисление сервисов.
- `README.md` — обновить блок topology.

**Критерий готовности Фазы 0**: ✅ реализовано — монолит работает с `EVENT_BUS_BACKEND=nats` без изменений поведения.

---

## Фаза 1 — agent-gateway (stateless) — ✅ РЕАЛИЗОВАНА (2026-06-15)

Цель: выделить агентский контур в отдельный горизонтально-масштабируемый бинарь без in-memory состояния.

### 1.1. Новый entrypoint `cmd/agent-gateway/main.go` — ✅
- Минимальный DI: config, logger, DB, EventBus (NATS или memory), репозитории агентов/компаний, `AgentOperatorFlowService`, `AgentService`, `AgentAuthService` с JWT, `AgentHandler`.
- Маршруты: `/api/agents/register`, `/api/agents/auth/refresh`, `/api/agents/{uuid}/data`, `/api/agents/{uuid}/config`, `/api/agents/report`, `/api/submit_json`.
- Health: `/healthz`, `/readyz` (с проверкой БД).
- Без шлюзов/воркеров/интеграций — только HTTP + publisher в NATS.

### 1.2. JWT EdDSA access-токены — ✅
- `internal/services/agentauth/jwt.go`: `EdDSATokenService` — подпись/верификация JWT Ed25519 через `jwt.SigningMethodEdDSA`.
- Access-токены не хранятся в БД — устранён write-per-heartbeat.
- Refresh-токены остаются opaque (lookup в БД).
- `AGENT_JWT_ENABLED=true/false` — переключение JWT/opaque без переразвёртывания.
- Приватный ключ из ENV (`AGENT_JWT_PRIVATE_KEY` PEM PKCS#8) или авто-генерация в памяти.
- `looksLikeJWT` heuristic для выбора пути валидации (JWT vs opaque fallback).
- 16 unit-тестов покрывают выпуск, валидацию, подпись, экспирацию, fallback, header typ.

### 1.3. Устранение in-memory связей — ✅ (без изменений, NATS publisher)
- `AgentService.ProcessData` публикует `AgentObservationRequested` в EventBus (memory или NATS) — без изменений.
- agent-gateway — publisher-only, подписчики остаются в монолите/processing-service.

### 1.4. Идемпотентность команд при multi-instance — ✅
- Поле `AgentCommand.SagaID *string` для идемпотентности scheduled-запусков.
- `generateSagaID(agentUUID, adapterID)` — детерминированный saga_id (формата `uuid/adapter/YYYY-MM-DD`).
- Partial unique index `(agent_uuid, type, saga_id) WHERE saga_id IS NOT NULL` на PostgreSQL.
- `ON CONFLICT DO NOTHING` через `clause.OnConflict{DoNothing: true}` в `enqueueAdapterRunLocked`.
- `db.MigrateAgentGateway(db)` — лёгкая миграция для agent-gateway.
- Unit-тесты для `generateSagaID` (детерминизм, формат, различие агентов/адаптеров).

### 1.5–1.6. FTP и каталог адаптеров — отложены до Фазы 2
- `AgentFTPGateway` и `AgentAdapterCatalogSyncService` остаются в монолите.
- agent-gateway читает проекции каталога из БД, не управляет им.

### 1.7. Deployment — ✅
- `docker/agent-gateway.Dockerfile`: multi-stage, `go 1.26.2-alpine`.
- `docker/agent-gateway.k8s.yaml`: Deployment (replicas 2), HPA (2–10 по CPU), Service ClusterIP, PodAntiAffinity, liveness/readiness, ConfigMap, Secret.
- `.env.example` обновлён: `AGENT_GATEWAY_PORT`, `AGENT_JWT_*`.
- Агентские маршруты убраны из монолита (`internal/app/app.go`).

**Критерий готовности Фазы 1**: ✅ два пода agent-gateway могут одновременно обслуживать heartbeat-нагрузку, scheduled-команды не дублируются (saga_id + ON CONFLICT), JWT access-токены проверяются без БД.

---

## Фаза 2 — integration-hub (central хост) — ПРИОРИТЕТ 2 (≈3–4 недели)

Цель: вынести весь интеграционный контур на хост с доступом ко всем внешним системам.

### 2.1. Новый entrypoint `cmd/integration-hub/main.go`
- DI: config, logger, DB (master), NATS, Redis, S3, все внешние клиенты (`SDClient`, `BitrixClient`, `PyrusClient`, `MegafonVATSClient`, `IikoClient`, `ContractMailbox`, `FTPClient`).
- HTTP-сервер — только вебхуки: `/api/integrations/bitrix/webhook`, `/api/integrations/pyrus/webhook`, `/api/integrations/megafon-vats/webhook`. Регистрируются через существующие модули `bitrix_module`/`pyrus_module`/`telephony_module` (методы `registerPublicRoutes`).
- Фоновые процессы — из `runBackgroundServices` (`app.go:886-955`), только интеграционные.

### 2.2. Вебхуки и incoming-консьюмеры
- Bitrix/Pyrus/МегаФон incoming-сервисы переезжают без изменений (Redis Streams как буфер + Postgres-таблица для recovery уже готовы).
- Redis — общий (доступен integration-hub и другим сервисам если нужно).

### 2.3. Sync-сервисы (исходящая запись во внешние системы)
- `BitrixSyncService` (7 доменных репозиториев в конструкторе — самый тяжёлый), `PyrusSyncService`, `MegafonVATSSyncService`, `TelephonyService`.
- На переходе — имеют доступ к общей БД напрямую (гибридный подход). По мере распила доменов — заменяются на Domain API / события.
- Event-bridges (`bitrix_event_bridge.go`, `pyrus_event_bridge.go`) остаются, подписываются через NATS.

### 2.4. Шлюзы
Переезжают целиком:
- `sdesk_gateway.go` (Naumen sync справочника).
- `ticket_gateway.go` + `ticket_file_sync_service.go` (Naumen sync заявок).
- `contract_gateway.go` (IMAP 1С, опционально Bitrix-синк точек).
- `server_polling_gateway.go` (iiko RMS).
- `agent_ftp_gateway.go` (FTP getad — singleton, shared S3-кэш вместо локального `FTPCachePath`).
- `fr_update_founder.go` (сверка ФР с Naumen).
- `sd_editor_worker.go` (обратная запись в Naumen).
- `agent_adapter_catalog_sync.go` (S3-каталог адаптеров, singleton).

### 2.5. Локальные файлы → S3
- `TICKET_STORAGE_PATH` (локальный диск для ассетов от Bitrix/Pyrus, `bitrix_incoming_service.go:persistBitrixCommentAttachment`, `pyrus_incoming_service.go:persistPyrusAttachment`) → S3-бакет. Паттерн уже реализован для telephony-recordings (`internal/infra/adapterstore/s3.go`).
- Миграция существующих файлов: одноразовый job `tools/migrate-ticket-files-to-s3`.
- `FileAsset.Storage` — поле для указания `s3|local` (обратная совместимость на переходе).

### 2.6. Leader-выборка для singleton-воркеров
- Шлюзы и catalog-sync не должны работать в 2+ экземплярах одновременно.
- Решение: NATS-based leader election (NATS KeyValue bucket `sss:leaders` с TTL) либо k8s Deployment `replicas: 1` для воркер-группы. На переходе — последнее (проще).

### 2.7. Deployment
- `docker/integration-hub.Dockerfile`, `docker/integration-hub.k8s.yaml`: Deployment (replicas по типу: вебхук-сервер — HPA, воркеры — singleton), Service с internal-only Ingress (внешние IP открывает только хост-провайдер, где доступны интеграции).
- Конфиг: все `ENABLE_*`-флаги и ключи внешних систем.

**Критерий готовности Фазы 2**: integration-hub автономно обслуживает все вебхуки и шлюзы; монолит больше не запускает интеграционные процессы (флаги `ENABLE_BITRIX_GATEWAY` и т.д. в монолите = `false`).

---

## Фаза 3 — processing-service (≈2 недели)

Цель: выделить событийный обработчик (оркестратор + processing engine) в отдельный потребитель NATS.

### 3.1. Новый entrypoint `cmd/processing-service/main.go`
- DI: config, logger, DB (master), NATS, репозитории всех доменов (processing engine работает с company/server/workstation/fiscal/tickets).
- Без HTTP-сервера — только consumer.

### 3.2. Потребители NATS
Переносятся подписки из `orchestrator.Start` (`orchestrator.go:69-100`):
- `agent.data.received` → обработка agent observation (processing engine `ProcessAgentData`).
- `duplicates.found` → `ProcessDuplicates`.
- `servicedesk.entity.updated/deleted` → `ProcessServiceDeskUpdate`.
- `server.polling.succeeded/failed`.
- `discrepancy.fiscal_register.found`.

### 3.3. Обработчики
- `Orchestrator` + `orchestrator_handlers.go` + `engine.go` (`ProcessingEngine`, `processingEntityExecutor`, `processingPlanExecutor`) переезжают целиком.
- `SDEditorWorker` остаётся в integration-hub (он пишет во внешнюю систему Naumen), но потребляет событие `servicedesk.entity.create.requested` из NATS — не из локальной шины.
- `EntityMatcherService`, `ReconciliationEngine` — переезжают (внутренние движки processing).

### 3.4. Публикация доменных событий
- После plan/execute публикует `ticket.updated`, `agent.observation.updated`, `contracts.status.recalculated` в NATS — для SSE operator-api.

### 3.5. Параллелизм
- Несколько подов processing-service с consumer-group NATS (одно сообщение — один потребитель). Идемпотентность обработки — на уровне processing engine (уже есть идемпотентность через fingerprint/duplicates).

### 3.6. Deployment
- `docker/processing-service.Dockerfile`, k8s Deployment с HPA по длине NATS consumer-pending.

**Критерий готовности Фазы 3**: обработка наблюдений и reconciliation идёт в отдельном сервисе; монолит больше не содержит orchestrator.

---

## Фаза 4 — operator-api cleanup (≈2 недели)

Цель: монолит становится чистым backend для UI.

### 4.1. Rename и зачистка
- `cmd/etalon-server` → остаётся как `cmd/operator-api` (или alias), флаги `ENABLE_*` для agent/integration/processing в конфиге монолита убираются.
- Из `runBackgroundServices` убираются все вынесенные процессы (к этому моменту уже не запускаются по флагам).

### 4.2. SSE через NATS
- `sse_handler.go` (`bus.SubscribeChannel`) — переписывается на JetStream consumer.
- Multi-instance operator-api: каждый под имеет свой durable-consumer, события приходят всем (broadcast) через отдельный stream без consumer-dedup.
- `TicketUpdated`, `TelephonyLineUpdated`, `AgentObservationUpdated` — из NATS.

### 4.3. Чтение из реплики
- Read-пути (списки, поиск, детали) — через read-реплику (`?host=localhost&target_session_attrs=read-only`).
- Write-пути (создание/редактирование оператором) — через master.
- Connection routing в `internal/infra/db`: два пула, выбор по типу операции (репликация логики уже заложена в GORM-репозиториях через `getDB`).

### 4.4. Deployment
- `docker/operator-api.Dockerfile` (по сути текущий образ, без агентских/интеграционных пакетов в финальном бинаре), HPA.

**Критерий готовности Фазы 4**: operator-api содержит только UI-backend, всё остальное — в выделенных сервисах.

---

## Фаза 5 — Гео-репликация БД (≈1–2 недели)

Цель: вынести agent-gateway в региональные кластеры с локальной read-репликой.

### 5.1. PostgreSQL streaming replication
- Master в центральном кластере, физическая read-реплика в каждом региональном.
- `docker/` — обновить compose/manifests для региональных кластеров; `pg_hba.conf`, replication user, slot management.
- Мониторинг replication lag (Prometheus exporter).

### 5.2. Read-after-write consistency
- Проблема: agent-gateway пишет heartbeat в master, затем читает adapter_manifests из реплики — может не увидеть свою запись при lag.
- Митигация: критичные чтения сразу после записи (adapter_manifests, pending commands) — через master-соединение (маркируются как `requires-fresh-read`). Некритичные — из реплики.

### 5.3. NATS edge leaf
- В каждом регионе — NATS leaf node с подключением к центральному кластеру (leafnode). agent-gateway публикует события локально → leaf пробрасывает в центральный JetStream.
- Снижает latency публикации и устойчив к временным разрывам связи с центром.

### 5.4. Маршрутизация агентов
- DNS / GeoDNS: `agent.etalon.local` резолвится в ближайший региональный agent-gateway.
- Agent-config: `server_url` остаётся один, маршрут определяется DNS.

**Критерий готовности Фазы 5**: агент в регионе X ходит на локальный agent-gateway, heartbeat обрабатывается с локальной read-репликой, события доходят до центрального processing-service через NATS leaf.

---

## Кросс-каттинги и риски

### `external_system_links` (полиморфный shared-state)
Таблица `models.ExternalSystemLink` (`models.go:26-46`) — центральный маппинг внутренних UUID ↔ внешних ID, используется integration-hub (запись/чтение) и processing-service (чтение). При общей БД — работает без изменений. При будущем database-per-service — станет первым кандидатом на выделение в отдельный сервис-владелец (за рамками плана).

### `TicketService` — супер-хаб (14 зависимостей)
`TicketService` (`app.go:433-449`) тянет репозитории 6 доменов. При распиле:
- До Фазы 4 остаётся в operator-api как есть.
- Долгосрочно — разбивается на агрегаты: Ticket (операторский CRUD), TicketIntegrationProjection (для incoming-сервисов). За рамками текущего плана.

### Сквозные транзакции (`Transactor`)
`Transactor.WithinTransaction` + `ExtractDB` работают в рамках одного процесса и одного `*gorm.DB`. После распила:
- Внутри каждого сервиса — остаются (каждый сервис имеет свой пул к общей БД).
- Cross-service операции — невозможны как одна транзакция. В гибридном подходе это не проблема: integration-hub и operator-api имеют доступ к общей БД, cross-service write нет. При будущем database-per-service — saga/outbox.

### NATS как новая точка отказа
- Кластер из 3 узлов с JetStream persistence (raft), quorum 2/3.
- События persistent — при рестарте consumer-а доставляются повторно (at-least-once → потребители должны быть идемпотентны).
- Деградация: при недоступности NATS agent-gateway может обрабатывать heartbeat (write в БД) и возвращать adapter_manifests, но события наблюдений буферизуются в JetStream — отставание processing допустимо.

### Replication lag при гео
- Допустимая величина — определяется SLA (для ServiceDesk приемлемо eventual consistency в секундах для большинства read-путей).
- Критичные read-after-write — через master (5.2).

### Идемпотентность потребителей
- Все потребители NATS — потенциально получают дубль (at-least-once). Текущая идемпотентность:
  - `ProcessAgentData` — через fingerprint/meaningfulChange.
  - Incoming events — через SHA256-хэш в `*_incoming_events`.
  - Commands — через `status IN ('new','sent')` в `CompleteCommands`.
- Аудит: на Фазах 1–3 проверить каждый consumer на идемпотентность, добавить unique-констрейнты где нужно.

---

## Порядок реализации и зависимости

```
Фаза 0 (фундамент)
   │
   ├──▶ Фаза 1 (agent-gateway) ───▶ Фаза 5 (гео)
   │        │
   │        └──▶ Фаза 3 (processing-service) — потребитель событий агентов
   │
   ├──▶ Фаза 2 (integration-hub)
   │
   └──▶ Фаза 4 (operator-api cleanup) — после 1/2/3
```

Фазы 1, 2 можно вести параллельно после Фазы 0. Фаза 3 — после 1 (нужен потребитель `agent.data.received`). Фаза 4 — после всех, т.к. требует чтобы монолит ничего не дублировал. Фаза 5 — после 1 (agent-gateway готов к regional deploy).

Общая длительность: ≈10–14 недель при одном инженере; меньше при параллельных Фазах 1/2.

---

## Что НЕ входит в этот план (намеренно)

- Database-per-service и реальная изоляция данных — отложено; master+replicas уже удовлетворяет требованию единого набора сущностей.
- Перепись `TicketService` на агрегаты — отдельная задача после стабилизации границ.
- Миграция `external_system_links` в отдельный сервис-владелец — только при переходе на database-per-service.
- Переписывание frontend — не требуется, API operator-api сохраняет контракты.
- Изменения core-agent (Windows) — не требуются; `server_url` остаётся, маршрутизация через DNS.

---

## Проверка плана на исходные требования

| Требование | Покрытие |
|---|---|
| Декомпозиция монолита | Фазы 1–4: 4 сервиса из одного монолита |
| Разнос по kuber-кластерам | Фаза 5: региональные кластеры с read-репликами |
| Решение недоступности единого хоста | Гео-DNS + regional agent-gateway (Фаза 5) |
| Сохранение агентского флоу | Фаза 1: agent-gateway держит все `/api/agents/*` и `/api/submit_json` |
| Единый набор сущностей | Master+replicas: одна logical БД на все кластеры |
| Event-driven между подами | NATS JetStream как замена in-memory EventBus (Фаза 0) |
| agent-gateway stateless | Фаза 1: JWT вместо opaque, без in-memory session, горизонтальное HPA |
| Собственные БД per-service | Отложено — master+replicas даёт единые сущности; per-service выделение — второй этап после стабилизации |

---

## Ключевые ссылки на код (опорные точки для реализации)

- In-memory шина (замена): `pkg/eventbus/eventbus.go`
- Типы событий и payload: `internal/core/events/events.go`
- DI и сборка приложения: `internal/app/app.go` (точка создания шины — строка 143)
- Конфигурация: `internal/infra/config/config.go`
- Агентская авторизация (opaque → JWT): `internal/services/agentauth/service.go`
- AgentService.ProcessData (публикация AgentObservationRequested): `internal/services/agent_service.go:228`
- Агентские маршруты и middleware: `internal/transport/http/handlers/agent_handler.go`
- Идемпотентность команд (гонка multi-instance): `internal/services/agent_adapter_runtime_profiles.go:530`
- Orchestrator (переезд в processing-service): `internal/core/processing/orchestrator.go:69-100`
- Фоновые процессы (разнос по сервисам): `internal/app/app.go:886-955`
- Вебхуки интеграций (переезд в integration-hub): `internal/app/bitrix_module.go`, `pyrus_module.go`, `telephony_module.go`
- external_system_links (shared-state): `internal/domain/models/models.go:26-46`
- S3-паттерн для миграции TICKET_STORAGE_PATH: `internal/infra/adapterstore/s3.go`
- Локальная инфраструктура: `docker/docker-compose.yml`
- Шаблон окружения: `.env.example`
