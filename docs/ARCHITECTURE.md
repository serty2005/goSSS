# Архитектура goSSS (фактическое состояние перед деплоем)

Документ фиксирует текущее состояние реализации по коду на момент подготовки релиза.

## 1. Event-Driven модель и роль Orchestrator

### 1.1. Базовая схема
- В системе используется event-driven подход на базе `pkg/eventbus` (`InMemoryEventBus`).
- Входящие потоки (агенты, синхронизация внешних систем, фоновые гейтвеи/воркеры) публикуют события.
- `Orchestrator` подписан на события и выполняет итоговые действия в БД (создание задач, обновления сущностей, связи).

### 1.2. Роль единого исполнителя сценариев
- `ProcessingEngine` формирует план действий (`ProcessingResult.Actions`).
- `Orchestrator` является исполнителем этого плана:
- создает задачи;
- применяет обновления;
- выполняет транзакции;
- добавляет дополнительные связи (например, `AdditionalOwners`).

Подтверждение в коде:
- `internal/core/processing/engine.go`
- `internal/core/processing/orchestrator.go`
- `internal/app/app.go`

## 2. Контуры API агентов и авторизация

### 2.1. Bootstrap-регистрация `/register` с отдельной авторизацией
- Маршрут `POST /api/agents/register` не защищён общим JWT middleware, потому что его вызывает ещё не зарегистрированный агент.
- При этом сама регистрация не является анонимной:
- `AgentHandler` требует заголовок `Authorization: Bearer <AGENT_API_KEY>`;
- при ошибке различаются минимум три причины `401`: отсутствует заголовок, неверный формат, неверный bootstrap key;
- каждая попытка регистрации сохраняется в `agent_registration_attempts`, а в `agents` обновляется последний диагностический срез регистрации.
- Повторная регистрация того же `agent_uuid` сейчас считается допустимой и идемпотентной на уровне бизнес-поведения: сервер перевыпускает access/refresh токены и обновляет last registration snapshot.

Подтверждение в коде:
- `internal/transport/http/handlers/agent_handler.go`
- `internal/services/agentauth/service.go`
- `internal/app/app.go`

### 2.2. Heartbeat `/api/agents/{uuid}/data` с access token агента
- Маршрут `POST /api/agents/{uuid}/data` также не использует общий JWT middleware, но уже проверяет собственный `Authorization: Bearer <access_token>`.
- Проверка access token выполняется через `agentauth.Service`.
- При heartbeat сервер обновляет:
- `last_heartbeat`;
- `last_observed_at`;
- `latest_inventory_snapshot`;
- `latest_adapter_statuses`.

Подтверждение в коде:
- `internal/transport/http/handlers/agent_handler.go`
- `internal/services/agent_service.go`
- `internal/app/app.go`

### 2.3. Защищённый read-контур диагностики агента
- Для операторского UI появился отдельный JWT-защищённый контур:
- `GET /api/agent-diagnostics`
- `GET /api/agent-diagnostics/{uuid}`
- Эти эндпоинты отдают:
- сводку по агенту;
- последний registration payload;
- system_info;
- machine_fingerprint;
- последние `inventory` и `adapter_statuses`;
- историю последних попыток регистрации.

Подтверждение в коде:
- `internal/transport/http/handlers/agent_diagnostics_handler.go`
- `internal/app/app.go`

## 3. Контрактный event-контур (временно отключен)

### 3.1. Текущее состояние
- Подписка `Orchestrator` на событие `contracts.status.recalculated` намеренно отключена.
- Обработчик `handleContractsStatusRecalculated` сохранен в коде, но не подключен в `Start()`.
- Фактический пересчет статусов контрактов и блокировки/разблокировки оборудования выполняется напрямую в `contract.Service`.

### 3.2. Причина и условия возврата
- Причина прямо указана в коде `Orchestrator`: текущий пересчет выполняется напрямую в contract service.
- Условие возврата также указано в коде: после обновления контура контрактной синхронизации подписка может быть возвращена.

Подтверждение в коде:
- `internal/core/processing/orchestrator.go`
- `internal/services/contract/service.go`
- `internal/core/gateways/contract_gateway.go`

## 4. Поведение владения (owner behavior)

### 4.1. Что происходит сейчас
- В контуре `agent_observation_service` реализована автоматическая смена владельца:
- рабочей станции (owner меняется на owner сервера при расхождении);
- фискального регистратора (owner меняется на owner сервера при расхождении).

- В `ProcessingEngine` для части конфликтов сохраняется сценарий создания задач `owner_mismatch` (когда компании не связаны).

### 4.2. Итог по текущей логике
- Текущая реализация действительно допускает автоматическое изменение владения.
- Поведение смешанное: часть кейсов решается автоматически, часть через задачу `owner_mismatch`.

Подтверждение в коде:
- `internal/services/agent_observation_service.go`
- `internal/core/processing/engine.go`
- `internal/core/processing/reconciliation.go`

## 5. Границы чистой архитектуры (фактические)

### 5.1. Слои
- `domain`: модели и интерфейсы домена (`internal/domain/*`).
- `use-case/application`: бизнес-логика и сценарии (`internal/services/*`, `internal/core/processing/*`, сборка зависимостей в `internal/app`).
- `infrastructure`: БД, внешние клиенты, адаптеры, репозитории (`internal/infra/*`).
- `transport`: HTTP handlers/middleware/dto (`internal/transport/http/*`).

### 5.2. Правило доступа к БД через репозитории
- Целевое правило: доступ к БД только через репозитории.
- Фактическое состояние: правило соблюдается не полностью.
- Есть прямой доступ к `*gorm.DB` и SQL/GORM-операциям в ряде мест (например, `agent_observation_service`, `candidate_handler`, части gateways/workers/orchestrator/task service).

Подтверждение в коде:
- `internal/services/agent_observation_service.go`
- `internal/transport/http/handlers/candidate_handler.go`
- `internal/core/processing/orchestrator.go`
- `internal/services/task/service.go`

## 6. Готовность к продакшену

### 6.1. Что проверено
- Выполнены тесты: `go test ./...` (успешно).
- Подтверждена работоспособность event-каркаса и подписок оркестратора по текущему набору событий.
- Подтверждены фактические настройки API-авторизации для агентских маршрутов.
- Подтверждено намеренное отключение контрактного event-контура и работа прямого пересчета в contract service.

### 6.2. Что остается риском
- диагностика регистрации теперь хранится и доступна в UI-ready API, но полноценный frontend-контур подтверждения ещё нужно собрать;
- `InMemoryEventBus` не дает персистентной доставки событий (риск потери при аварийной остановке процесса).
- Границы слоя доступа к данным размыты: часть логики работает с БД напрямую, минуя репозитории.
- Логика владения смешанная (автопереназначение + задачи), что требует явного операционного контроля.

### 6.3. Что блокирует / не блокирует релиз
- Не блокирует релиз в рамках текущих принятых решений:
- отсутствие отдельного middleware на `/api/agents/*`, потому что авторизация выполняется внутри handler/service;
- временное отключение контрактной event-подписки при рабочем прямом пересчете в `contract.Service`.

- Потенциальный блокер при изменении требований безопасности:
- необходимость вынести агентскую авторизацию из handler/service в единый middleware-контур.

## 7. Пробелы и вопросы (по коду)

- В коде явно нет единого архитектурного флага/документа, который бы формально разделял временные и постоянные отклонения по слоям доступа к БД.
- Для релизного контроля требуется отдельное решение: считать ли текущую схему внутренней агентской авторизации в handler/service достаточной для production-периметра или дополнительно ограничивать её внешним контуром (reverse proxy/WAF/VPN).

## 8. Изменения по границам доступа к БД (февраль 2026)

Кратко зафиксированы фактические изменения:
- `internal/transport/http/handlers/candidate_handler.go`: прямые запросы к `gorm.DB` убраны, чтение кандидатов и staging вынесено в `CandidateRepo`.
- `internal/services/agent_observation_service.go`: сервис переведен в делегат; SQL/GORM-логика перенесена в `internal/infra/repositories/agent_observation_repo.go`.
- `internal/services/task/service.go`: прямые выборки задач и дубликатов вынесены в `TaskRepo`.
- `internal/services/server_actions_service.go`: операции `AdditionalOwners` вынесены в `server.Repository`.
- `internal/services/task_resolution_service.go` и `internal/services/sd_editor_service.go`: прямые транзакции/`Save` заменены на `Transactor` + методы `TaskRepo`.
- `internal/app/app.go`: DI обновлен на зависимости через репозитории.

Явные технические исключения, оставленные в `internal/services`:
- `internal/services/fr_serial_maintenance_service.go`: инфраструктурный DDL/индексы для обслуживания БД при старте.

Остаток прямого DB-доступа в `internal/core/*` сохранен и требует отдельного этапа рефакторинга.


## 9. Изменения network_hub (февраль 2026)

- Добавлен явный режим компании owner_mode: 
ormal/
etwork_hub.
- Для 
etwork_hub включен отдельный resolver владельца по дочерним компаниям (сильные сигналы: serial ФР, TV/LM/AnyDesk).
- При отсутствии/неоднозначности совпадения создается 
etwork_candidate с группами observation (1 WS + 0..N FR).
- Добавлен отдельный API контур /api/network-candidates/* и UI-страница Принятие в сеть.
- Механизм задач owner_mismatch отключен как рабочий в текущем контуре.
- Добавлены owner_binding_mode (auto/manual) и owner_change_history для фиксации смен владельца.

