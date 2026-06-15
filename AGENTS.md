# AGENTS.md

Документ задает рабочие правила для AI-агентов и краткую архитектурную карту проекта `goSSS`. Его нужно читать перед любыми изменениями в репозитории.

## 1. Базовые правила

1. Все ответы пользователю, комментарии в коде, логи, тексты ошибок и новые документы пишутся на русском языке.
2. Нельзя придумывать детали реализации внешних систем, бизнес-правил и неоднозначных требований. Если фактов нет в коде, документации или явном ответе пользователя, нужно задать уточняющий вопрос.
3. Комментарии в коде должны описывать только текущую функциональность. Не использовать пометки вроде "добавлено", "обновлено", "временно", если это не часть реального runtime-смысла.
4. При работе в PowerShell принудительно включать UTF-8 для вывода и чтения, например `$env:PYTHONIOENCODING='utf-8'`, чтобы кириллица не превращалась в mojibake.
5. Для чтения и анализа файлов в этом репозитории использовать Python-скрипты с `encoding='utf-8'`. Для массового поиска можно вызывать быстрые инструменты из Python, но итоговое чтение файлов должно сохранять UTF-8.
6. Не откатывать чужие изменения в рабочем дереве. Если найден dirty state, сначала понять, относится ли он к задаче.
7. Не менять реальные секреты, локальные данные, `storage/`, `ftp_cache/`, дампы и production `.env`, если пользователь явно не просит.
8. Для UI-изменений использовать Playwright через Docker-MCP. Тестовый логин: `admin/admin`.
9. При разработке Go-кода использовать скилл `go-modern-guidelines`.
10. Актуальную документацию Bitrix24 брать через MCP-сервер `b24-dev-mcp`. Актуальную документацию библиотек брать через Context7-MCP в Docker MCP или из официальных источников.

## 2. Назначение проекта

`goSSS` - ServiceDesk для сотрудников техподдержки. Проект состоит из:

- backend-сервера XenionDesk;
- React frontend для операторов и администраторов;
- Windows core-agent;
- внешних агентских адаптеров;
- docker-инфраструктуры;
- инструментов импорта, сидирования и публикации адаптеров.

Главные пользовательские сценарии: работа с заявками, учет инфраструктуры клиентов, диагностика агентов, приемка найденного оборудования, синхронизация с внешними системами, телефония и отчеты.

## 3. Технологии и версии

- Backend: Go module `etalon-server`, `go 1.26.2`.
- Agent: Go module `etalon-agent`, `go 1.26.2`.
- Backend HTTP: `chi/v5`.
- Backend DB: PostgreSQL + GORM.
- Шина событий: `pkg/eventbus` с двумя бэкендами — in-memory (`EVENT_BUS_BACKEND=memory`, по умолчанию) и распределённая NATS JetStream (`EVENT_BUS_BACKEND=nats`).
- Очереди интеграций: Redis Streams (входящие вебхуки Bitrix/Pyrus/МегаФон).
- Object storage: S3/MinIO.
- Frontend: React 19, TypeScript, Vite, Ant Design 6, TanStack Query, Zustand, i18next.
- E2E: Playwright.
- Production proxy: Traefik, Nginx во frontend-контейнере.

Важно: исходный Go-код не должен использовать возможности новее версии, указанной в соответствующем `go.mod`. Более свежий Docker builder не повышает language target.

## 4. Карта репозитория

```text
cmd/                         # CLI и backend entrypoints
internal/app/                # сборка приложения, DI, router, фоновые процессы
internal/domain/             # доменные модели, интерфейсы и типы
internal/infra/              # БД, внешние клиенты, плагины, S3, logger, config
internal/services/           # бизнес-сервисы backend
internal/core/               # event bus gateways, workers, processing
internal/transport/http/     # handlers и middleware
pkg/eventbus/                # шина событий: in-memory + NATS JetStream backend
front-ui/                    # frontend
agent/                       # Windows agent и адаптеры
docker/                      # compose, Dockerfile, deploy
tools/                       # сидер и служебные инструменты
DECOMPOSITION_PLAN.md        # целевая архитектура и план декомпозиции монолита
```

## 5. Backend-архитектура

Точка входа: `cmd/etalon-server/main.go`.

Создание приложения: `internal/app/app.go`.

Порядок инициализации:

1. `config.New()` читает `.env`.
2. `db.NewConnection()` подключает PostgreSQL.
3. `db.Migrate()` выполняет `gorm.AutoMigrate`.
4. Создаются репозитории.
5. Создаются внешние клиенты.
6. Создаются бизнес-сервисы.
7. Создаются gateways, workers и integration modules.
8. Создаются HTTP handlers.
9. `Run()` запускает event bus, фоновые процессы и HTTP-сервер.

Обычный HTTP-путь: `handler -> service -> repository -> DB`.

Событийный путь: `gateway/handler -> eventbus -> orchestrator/worker -> service/repository`.

### 5.1 Шина событий и распределённая архитектура

`pkg/eventbus` предоставляет интерфейс `EventBus` (`Publish`, `Subscribe`, `SubscribeChannel`, `Start`) с двумя реализациями:

- `InMemoryEventBus` — in-process шина на буферизированном канале (ёмкость 10000). Бэкенд по умолчанию, подходит для локальной разработки и текущего монолита.
- `NATSEventBus` (`pkg/eventbus/nats_bus.go`) — распределённая шина поверх NATS JetStream. Заменяет in-memory при горизонтальном масштабировании и распиле сервиса на поды.

Бэкенд выбирается фабрикой `eventbus.New(cfg)` по `EVENT_BUS_BACKEND` (`memory` или `nats`) и подключается в `internal/app/app.go`. Подписчики (`Subscribe`, `SubscribeChannel`) и издатели (`Publish`) не различают бэкенды — один и тот же код работает в обоих режимах.

Ключевые детали NATS-бэкенда:

- Стримы разделены по доменам (`sss.agent`, `sss.integration`, `sss.processing`, `sss.domain`), см. `domainStreams` в `nats_bus.go`. Одно событие копируется во все стримы с подходящим subject-фильтром, что позволяет независимо доставлять его воркерам и SSE-broadcast.
- `Subscribe` создаёт durable pull-consumer с детерминированным именем (зависит только от типа события) — все реплики с одинаковым именем делят сообщения конкурентно (одно сообщение получает одна реплика).
- `SubscribeChannel` (SSE) создаёт ephemeral consumer с уникальным durable на процесс — каждый под operator-api получает свою копию событий (broadcast).
- Для восстановления конкретного payload-типа при десериализации используется реестр `eventbus.RegisterPayloadType`. Регистрация всех событий системы выполняется в `internal/core/events/payload_types.go` через `init()`. Это обязательно: без регистрации подписчики не смогут сделать `event.Payload.(events.<ConcretePayload>)`.

Конфигурация NATS (см. `internal/infra/config/config.go`, секция `NATSConfig`): `EVENT_BUS_BACKEND`, `NATS_URLS`, `NATS_CREDS_FILE`, `NATS_STREAM_PREFIX` (default `sss`), `NATS_MAX_AGE_HOURS` (default 168 = 7 дней).

Текущий статус декомпозиции монолита на отдельные сервисы (agent-gateway, integration-hub, processing-service, operator-api) описан в `DECOMPOSITION_PLAN.md`. Реализована Фаза 0 — переключаемая шина событий с NATS JetStream; остальной монолит пока работает как единый процесс.

## 6. Основные backend-модули

- `internal/domain/tickets` - заявки, история, комментарии, вложения, файловые активы.
- `internal/domain/company`, `contract` - компании, договоры, отчеты, Bitrix-маппинги.
- `internal/domain/server`, `workstation`, `fiscal` - оборудование.
- `internal/domain/user` - пользователи, роли, интеграции и профиль.
- `internal/domain/bitrix`, `pyrus`, `telephony` - внешние интеграции.
- `internal/domain/models` - общие модели: агенты, кандидаты, материалы, удаления, локализация.
- `internal/services/agent*` - агентский backend-контур, авторизация и operator flow.
- `internal/core/gateways` - синхронизация с внешними источниками.
- `internal/core/workers` - фоновые реакции и периодические проверки.
- `internal/infra/plugins` - адаптеры внешних систем.

## 7. HTTP и авторизация

Router собирается в `Application.setupRouter()`.

Публичные или специальные группы:

- `/api/auth/login`;
- `/api/submit_json`;
- `/api/agents/register`;
- `/api/agents/auth/refresh`;
- `/api/agents/{uuid}/data`;
- `/api/integrations/bitrix/webhook`;
- `/api/integrations/pyrus/webhook`;
- `/api/integrations/megafon-vats/webhook`;
- `/swagger/*`.

Защищенная группа `/api` использует JWT middleware.

Роли:

- `admin` - полный административный доступ;
- `support_specialist` - операторская работа с заявками, инфраструктурой, кандидатами и агентами;
- `intern` - ограниченный доступ там, где явно разрешено.

При добавлении маршрута обязательно проверить:

- нужный уровень авторизации;
- роль через middleware;
- DTO и ошибки на русском языке;
- согласованность с frontend API-клиентом.

Legacy getad-контур:

- `POST /api/submit_json` зарегистрирован отдельно в `Application.setupRouter()` до JWT-группы;
- handler: `AgentHandler.HandleSubmitJSON`;
- авторизация не JWT и не agent access token, а заголовок `X-API-Key`;
- `X-API-Key` сравнивается с `Config.AgentAPIKey` / `AGENT_API_KEY`;
- если `AGENT_API_KEY` пустой, текущий handler не блокирует запрос по ключу;
- payload читается как `AgentDataDTO`;
- UUID берется из `agent_uuid` или legacy-поля `uuid`;
- `agent_type` всегда принудительно становится `getad`;
- обработка идет через общий `AgentService.ProcessData`;
- неизвестный `getad`-агент может быть создан автоматически, в отличие от `sssruner`, которому нужен bootstrap/token flow;
- ответ идет через `RespondWithJSON`, то есть в JSON-конверте, а не raw heartbeat JSON.

Не переводить `/api/submit_json` на JWT, bootstrap bearer или agent access token без явного решения о миграции getad-агентов.

## 8. Frontend-архитектура

Frontend находится в `front-ui`.

Важные файлы:

- `src/App.tsx` - маршруты, providers, protected/admin routes;
- `src/api/*` - API-клиенты;
- `src/pages/*` - страницы;
- `src/components/*` - переиспользуемые компоненты;
- `src/features/realtime` - SSE;
- `src/store/*` - Zustand stores;
- `src/i18n/*` и `src/locales/*` - локализация;
- `src/theme/*` - тема и профильные настройки;
- `e2e/*` - Playwright-сценарии.

Главные UI-разделы:

- заявки;
- компании и приемка;
- оборудование;
- агенты и наблюдения;
- администрирование;
- отчеты.

UI должен оставаться рабочим инструментом техподдержки: плотная, читаемая, предсказуемая компоновка без лендинговой подачи.

## 9. Agent-архитектура

Agent находится в `agent`.

Точки входа:

- `agent/cmd/etalon-agent` - core-agent;
- `agent/cmd/fiscal-atol-adapter`;
- `agent/cmd/fiscal-mitsu-adapter`;
- `agent/cmd/fiscal-shtrih-adapter`;
- `agent/cmd/iiko-syrve-rms-adapter`.

Core-agent:

- читает `agent-config.json`;
- хранит identity и токены в HKLM;
- защищает токены через DPAPI;
- регистрируется на сервере;
- отправляет heartbeat и inventory;
- получает `adapter_manifests` и задачи;
- синхронизирует адаптеры в `data_dir/adapters`;
- исполняет `run_adapter`, `adapter_run`, `saga_run`, legacy `self_update`;
- ведет локальное состояние связи и backoff.

Документы agent-контура:

- `agent/docs/ADAPTER_CONTRACT.md`;
- `agent/docs/ADAPTER_RELEASE_LIFECYCLE.md`;
- `agent/docs/STAGE_REVIEW_AND_OPERATOR_FLOW.md`;
- `agent/docs/saga_runtime.md`.

## 10. Интеграции

Naumen ServiceDesk:

- базовая внешняя система для компаний, оборудования и заявок;
- подключается через `BASE_URL`, `SDKEY`;
- adapter находится в `internal/infra/plugins/naumen`.

Bitrix24:

- включение: `ENABLE_BITRIX_GATEWAY`;
- webhook: `/api/integrations/bitrix/webhook`;
- входящие события сохраняются в Postgres и обрабатываются через Redis Streams;
- документацию API проверять через `b24-dev-mcp`.

Pyrus:

- включение: `ENABLE_PYRUS_GATEWAY`;
- webhook: `/api/integrations/pyrus/webhook`;
- используется для связей заявок, комментариев, файлов и пользователей.

Мегафон ВАТС:

- включение: `ENABLE_MEGAFON_VATS`;
- webhook: `/api/integrations/megafon-vats/webhook`;
- хранит входящие события, звонки, связи с заявками, pending context, контакты и записи.

S3/MinIO:

- хранит агентские адаптеры и записи телефонии;
- catalog source of truth для адаптеров описан в `agent/docs/ADAPTER_RELEASE_LIFECYCLE.md`.

## 11. Фоновые процессы

Фоновые процессы запускаются из `Application.runBackgroundServices()`:

- event bus;
- ServiceDesk entity sync;
- Ticket sync;
- duplicates search;
- fiscal discrepancy finder;
- agent FTP gateway;
- server polling gateway;
- contract gateway;
- status actuality worker;
- deferred ticket worker;
- agent adapter catalog sync;
- Bitrix/Pyrus/Telephony loops.

Перед изменением фонового процесса нужно проверить флаг `ENABLE_*`, интервал, graceful shutdown через context и влияние на event bus. Шина событий запускается первой (`EventBus.Start`), её бэкенд in-memory или NATS выбирается через `EVENT_BUS_BACKEND` (см. секцию 5.1).

## 12. Конфигурация и данные

Backend config: `internal/infra/config/config.go`.

Основной шаблон: `.env.example`.

Production-шаблоны: `docker/.env.prod.example`, `docker/prod-files/.env`, `docker/prod-files/docker-compose.yml`.

Не коммитить реальные секреты. Не использовать значения из локальных `.env` как документацию к production, если они не совпадают с шаблоном.

Локальные данные:

- `storage/tickets` - файлы заявок;
- `ftp_cache` - кэш FTP-данных агентов;
- `tools/seeder/mock_data` - мок-данные сидера.

## 13. Команды разработки

Backend:

```bash
go test ./...
go run ./cmd/etalon-server
go run ./cmd/etalon-server --seed
go run ./cmd/etalon-server --seed-ftp-cache
go run ./cmd/etalon-server --reverse-seed
```

Agent:

```bash
cd agent
go test ./...
go run ./cmd/etalon-agent --config ./agent-config.json
```

Frontend:

```bash
cd front-ui
npm ci
npm run lint
npm run test
npm run test:e2e
```

Docker:

```bash
docker compose -f docker/docker-compose.build.yml build
docker compose -f docker/docker-compose.yml up -d
```

## 14. Правила изменения backend

1. Сначала найти существующий слой и паттерн.
2. Не класть бизнес-логику в handler.
3. Не обращаться к GORM напрямую из handler, если рядом уже есть service/repository.
4. Новые внешние интеграции оформлять через infra client/plugin и сервисный слой.
5. Для событий использовать существующий event bus и typed payload из `internal/core/events`.
6. Ошибки, пользовательские сообщения и логи писать на русском.
7. Для новых моделей проверить миграцию, индексы, уникальность и обратную совместимость.
8. Для ролей и прав обновить backend middleware и frontend route guards.

## 15. Правила изменения frontend

1. Не делать лендинговые или декоративные экраны вместо рабочего интерфейса.
2. Использовать существующие API-клиенты, stores, i18n и тему.
3. Для новых страниц добавить route, пункт меню при необходимости, права доступа и локализацию.
4. Не хранить серверные права только во frontend: backend должен проверять роль сам.
5. Для realtime использовать существующий SSE-контур.
6. После UI-изменений запускать релевантные unit/e2e проверки или явно фиксировать, что проверка не запускалась.

## 16. Правила изменения agent

1. Учитывать Windows-специфику, права администратора, HKLM и DPAPI.
2. Не ломать совместимость task types `run_adapter`, `adapter_run`, `saga_run`, `self_update`.
3. Не менять adapter contract без обновления `agent/docs/ADAPTER_CONTRACT.md`.
4. Не менять release lifecycle без обновления `agent/docs/ADAPTER_RELEASE_LIFECYCLE.md`.
5. Ошибки preflight должны быть явными и возвращаться серверу как structured task result.
6. Heartbeat должен оставаться устойчивым к частичным ошибкам адаптеров.

## 17. Документация

При изменении поведения обновлять ближайший документ:

- общий обзор - `README.md`;
- правила для агентов - `AGENTS.md`;
- production - `docker/DEPLOY.md`;
- frontend e2e - `front-ui/e2e/README.md`;
- agent contract и runtime - `agent/docs/*`.

Документация должна описывать фактическое состояние проекта, а не желаемую архитектуру.
