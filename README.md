# XenionDesk / goSSS

`goSSS` - ServiceDesk-система для сотрудников техподдержки. Проект объединяет операторский веб-интерфейс, серверное API, фоновые синхронизации с внешними системами и Windows-агент для сбора инвентаризации и выполнения локальных задач на клиентских рабочих местах.


## Wiki проекта

В репозитории запущен отдельный раздел `wiki/` для документации «по коду».

- стартовая страница: `wiki/README.md`;
- архитектурный снимок: `wiki/01-architecture-snapshot.md`;
- backend runtime: `wiki/02-backend-runtime.md`;
- HTTP и авторизация: `wiki/03-http-auth.md`;
- агентский контур и legacy getad: `wiki/04-agent-and-legacy-getad.md`;
- интеграции и фоновые процессы: `wiki/05-integrations-and-background-jobs.md`;
- процесс поддержки документации: `wiki/06-wiki-maintenance-process.md`.

## 1. Назначение

Система используется как рабочее место техподдержки:

- ведение заявок, комментариев, вложений, назначений и статусов;
- учет компаний, договоров, серверов, рабочих станций и фискальных регистраторов;
- прием и разбор наблюдений от агентов, подтверждение новых сущностей и сетевых кандидатов;
- диагностика агентов, выбор адаптеров, запуск адаптерных задач и контроль heartbeat;
- синхронизация с Naumen ServiceDesk, Bitrix24, Pyrus и телефонией Мегафон ВАТС;
- отчеты, административные справочники, пользователи, переводы UI и настройки профиля.

## 2. Состав репозитория

```text
.
├── cmd/
│   ├── etalon-server/        # основной backend-сервер XenionDesk
│   └── adapter-publisher/    # CLI публикации релизов агентских адаптеров в S3/MinIO
├── internal/                 # backend: домен, инфраструктура, сервисы, HTTP-транспорт
├── pkg/eventbus/             # in-memory event bus backend-приложения
├── front-ui/                 # React/Vite/Ant Design операторский интерфейс
├── agent/                    # Windows core-agent и внешние адаптеры
├── docker/                   # Docker Compose, Dockerfile, production-шаблоны и deploy-доки
├── tools/                    # сидер, мок-данные, служебные инструменты
├── ftp_cache/                # локальный кэш FTP-наблюдений агентов
└── storage/tickets/          # локальное хранилище файлов заявок
```

## 3. Архитектура backend

Backend написан на Go, точка входа - `cmd/etalon-server/main.go`. Приложение собирается в `internal/app.Application`, где поднимаются конфиг, БД, репозитории, сервисы, обработчики, интеграционные модули и фоновые процессы.

Основные слои:

- `internal/domain` - доменные модели, интерфейсы репозиториев и бизнес-типы;
- `internal/infra` - PostgreSQL/GORM, внешние клиенты, S3/MinIO, плагины интеграций, логирование;
- `internal/services` - бизнес-сервисы заявок, агентов, кандидатов, синхронизаций, телефонии, отчетов;
- `internal/core` - event-driven контур: gateways, workers, orchestrator, processing engine;
- `internal/transport/http` - HTTP handlers и middleware;
- `pkg/eventbus` - внутренняя шина событий.

HTTP-запросы идут через `chi`: middleware CORS, request id, real IP, логирование, recoverer, timeout и JWT. Бизнес-поток для обычных API остается слоистым: handler -> service -> repository. Фоновые интеграции и реакции на изменения идут через event bus.

## 4. Основные домены

- `tickets` - заявки, история, комментарии, вложения, файловые связи, отложенные события.
- `company`, `contract` - компании, иерархия, договоры, отчеты по договорам, связи с Bitrix24.
- `server`, `workstation`, `fiscal` - оборудование, владельцы, статусы, история смены владельца.
- `models.Agent*` - агенты, регистрации, сессии, команды, наблюдения, адаптеры и релизы.
- `Candidate*`, `NetworkCandidate*` - операторская приемка найденных рабочих станций и ФР.
- `bitrix`, `pyrus`, `telephony` - проекции и очереди внешних интеграций.
- `Material`, `Article`, `AppLocalization`, `EntityDeletionCandidate` - старые материалы карточек сущностей, статьи базы знаний, переводы UI, безопасное удаление сущностей.

Миграции выполняются через `gorm.AutoMigrate` при старте сервера.

## 5. Операторский UI

Frontend находится в `front-ui`, стек: React 19, TypeScript, Vite, Ant Design 6, TanStack Query, Zustand, i18next, TipTap, Playwright.

Основные маршруты UI:

- `/tickets`, `/tickets/:id` - список и карточка заявки;
- `/` - главная с публикациями базы знаний, отмеченными флагом "На главную";
- `/info` - агрегатор статей базы знаний и старых материалов карточек сущностей;
- `/info/articles/:id`, `/info/articles/new`, `/info/articles/:id/edit` - просмотр и редактор статей;
- `/companies`, `/companies/:id` - компании и инфраструктура;
- `/acceptance`, `/network-acceptance` - приемка кандидатов от агентов;
- `/servers`, `/workstations`, `/fiscals` - оборудование;
- `/agents`, `/agent-diagnostics/:uuid`, `/agent-observations` - агентский контур;
- `/admin/users`, `/admin/translations`, `/admin/telephony`, `/admin/synchronizations` - администрирование;
- `/tasks` и `/reports/companies-contracts` - задачи сверки и отчет по договорам.

UI использует JWT, SSE `/api/events` для realtime-обновлений и профильные настройки пользователя для темы, локализации и уведомлений.

## 6. Windows-агент

Агент находится в `agent`, модуль Go - `etalon-agent`. Основная точка входа: `agent/cmd/etalon-agent/main.go`.

Агент:

- читает `agent-config.json` или путь из `--config` / `ETALON_AGENT_CONFIG`;
- работает с правами администратора;
- хранит идентичность и токены в HKLM, токены защищаются DPAPI;
- регистрируется через `POST /api/agents/register` с bootstrap API key;
- обновляет agent access token через `/api/agents/auth/refresh`;
- отправляет heartbeat и inventory через `/api/agents/{uuid}/data`;
- синхронизирует адаптеры по manifest из ответа сервера;
- выполняет задачи `run_adapter`, `adapter_run`, `saga_run`, legacy `self_update`;
- умеет очищать локальные данные через `--cleanup` и `--cleanup-and-run`.

Адаптеры собираются отдельно: `fiscal-atol`, `fiscal-mitsu`, `fiscal-shtrih`, `iiko-syrve-rms`. Контракт описан в `agent/docs/ADAPTER_CONTRACT.md`, жизненный цикл релизов - в `agent/docs/ADAPTER_RELEASE_LIFECYCLE.md`.

## 7. Интеграции и фоновые процессы

Backend поддерживает следующие фоновые контуры:

- `ServiceDeskGateway` и `TicketGateway` - синхронизация сущностей и заявок через Naumen-адаптер;
- `AgentFTPGateway` - импорт JSON-наблюдений из FTP и локального `ftp_cache`;
- `ServerPollingGateway` - опрос RMS/iiko-серверов;
- `ContractGateway` - импорт договорных отчетов из почты и подготовка синхронизации с Bitrix24;
- `DuplicatesGateway` - поиск дублей и создание кандидатов на удаление;
- `FRUpdateFounder` - поиск расхождений по фискальным регистраторам;
- `StatusActualityWorker` - проверка актуальности статусов оборудования;
- `DeferredTicketWorker` - обработка отложенных событий заявок;
- `AgentAdapterCatalogSync` - синхронизация каталога адаптеров из S3/MinIO;
- модули Bitrix24, Pyrus и Мегафон ВАТС - webhook, очереди Redis Streams, справочники и синхронизации.

Включение контуров управляется переменными `ENABLE_*` в `.env`.

## 8. Ключевые API

Публичные или специальные маршруты:

- `POST /api/auth/login` - вход пользователя;
- `POST /api/submit_json` - legacy-прием данных от getad-агентов;
- `POST /api/articles/webhook` - создание публикации внешним сервисом по `X-API-Key`;
- `POST /api/agents/register` - bootstrap-регистрация агента;
- `POST /api/agents/auth/refresh` - обновление токенов агента;
- `POST /api/agents/{uuid}/data` - heartbeat и данные агента;
- `POST /api/integrations/bitrix/webhook` - webhook Bitrix24;
- `POST /api/integrations/pyrus/webhook` - webhook Pyrus;
- `POST /api/integrations/megafon-vats/webhook` - webhook Мегафон ВАТС;
- `GET /swagger/*` - Swagger UI.

Основные защищенные группы `/api`:

- `/tickets`, `/companies`, `/servers`, `/workstations`, `/fiscals`, `/contracts`, `/materials`, `/articles`;
- `/candidates`, `/network-candidates`, `/deletion-candidates`;
- `/agent-diagnostics`, `/agent-observations`, `/agents-list`;
- `/tasks`, `/duplicates`, `/search`, `/events`, `/reports`;
- `/profile`, `/translations`, `/users`;
- `/bitrix/*`, `/pyrus/*`, `/telephony/*`, `/integrations/*` при включенных модулях.

Пользовательские маршруты требуют `Authorization: Bearer <JWT>`. Агентские маршруты используют bootstrap key или agent access token.

### 8.1. Legacy getad endpoint `/api/submit_json`

`POST /api/submit_json` - отдельная совместимая ручка для старых пассивных getad-агентов. Она зарегистрирована вне JWT-группы и не использует новый bootstrap/access-token flow активного `sssruner`-агента.

Фактическое поведение:

- авторизация выполняется внутри handler по заголовку `X-API-Key`;
- значение `X-API-Key` сравнивается с backend-переменной `AGENT_API_KEY`;
- если `AGENT_API_KEY` пустой, handler не отклоняет запрос по ключу;
- тело запроса декодируется как `AgentDataDTO`;
- UUID агента берется из `agent_uuid`, а для legacy payload допускается поле `uuid`;
- если UUID не найден, возвращается `400 Field 'uuid' is required`;
- `agent_type` принудительно устанавливается в `getad`;
- данные передаются в общий `AgentService.ProcessData`;
- для неизвестного `getad`-агента сервис может создать запись автоматически со статусом active;
- ответ возвращается через стандартный JSON-конверт `{"status":"success","data":...}`.

Эта ручка нужна для обратной совместимости. Новый активный агент должен использовать `/api/agents/register`, `/api/agents/auth/refresh` и `/api/agents/{uuid}/data`.

### 8.2. Webhook создания публикаций `/api/articles/webhook`

`POST /api/articles/webhook` - публичная ручка для создания статей базы знаний внешними сервисами без пользовательского JWT.

Авторизация:

- ключ передается в заголовке `X-API-Key`;
- значение сравнивается с backend-переменной `ARTICLE_WEBHOOK_KEY`;
- если `ARTICLE_WEBHOOK_KEY` пустой или заголовок не совпадает, публикация не создается и возвращается `401`;
- ключ не связан с `AGENT_API_KEY` и не дает доступ к агентским маршрутам.

Тело запроса:

```json
{
  "title": "Заголовок публикации",
  "summary": "Короткое описание",
  "content": "Markdown-содержимое",
  "content_format": "markdown",
  "type": "wiki",
  "status": "published",
  "tags": ["wiki", "release"],
  "is_pinned": false,
  "show_on_home": true,
  "author_name": "Название внешнего сервиса"
}
```

Поддерживаемые значения:

- `type`: `wiki`, `release_note`, `company_news`, `incident_note`, `internal_doc`;
- `status`: `draft`, `published`, `archived`;
- `content_format`: `markdown`, `tiptap_json`.

Дополнительные поля:

- `slug` можно передать явно; если не передан, backend сгенерирует уникальный slug из `title`;
- для `release_note` обязательны `project_key` и `version`;
- `links` принимает массив объектов `{ "entity_type": "...", "entity_id": "..." }` с теми же типами сущностей, что и обычные статьи: `Company`, `Server`, `Workstation`, `FiscalRegister`, `Ticket`;
- `author_name` необязателен, по умолчанию используется `Внешний сервис`;
- `show_on_home=true` публикует статью в блоке базы знаний на главной странице, если `status=published`.

Успешный ответ возвращается в стандартном JSON-конверте со статусом `201` и DTO созданной статьи.

### 8.3. Контакты тикета

Тикет может иметь несколько контактов в `ticket_contacts`: телефонные контакты связаны с сущностью телефонии, Telegram хранится как значение контакта тикета. Один контакт помечается главным. Если главный контакт выбран вручную, автоподхват из связанных звонков или текста комментариев не перебивает этот выбор; если ручного выбора нет, главным становится последний добавленный контакт.

Для обратной совместимости поле `tickets.contact_id` хранит только главный телефонный контакт. Bitrix24 синхронизирует только этот главный телефон; Telegram-контакт используется в UI передачи менеджеру и не отправляется в Bitrix24 как контакт сделки. При передаче тикета менеджеру Telegram дополнительно сохраняется публичным комментарием, чтобы способ связи попал во внешний тикет.

## 9. Конфигурация

Backend читает первый найденный `.env`, поднимаясь от текущей директории к корню. Основной шаблон - `.env.example`.

Минимальные группы настроек:

- приложение: `PORT`, `DATABASE_URL`, `LOG_LEVEL`, `ALLOWED_ORIGINS`;
- безопасность: `JWT_SECRET`, `JWT_EXPIRATION_MIN`, `ADMIN_USERNAME`, `ADMIN_PASSWORD`, `AGENT_API_KEY`, `ARTICLE_WEBHOOK_KEY`;
- Naumen ServiceDesk: `BASE_URL`, `SDKEY`, `SD_DRY_RUN`, rate limit и retry;
- хранилища: `TICKET_STORAGE_PATH`, `FTP_CACHE_PATH`, `S3_*`, `AGENT_ADAPTER_CATALOG_*`;
- фоновые контуры: `ENABLE_SDESK_GATEWAY`, `ENABLE_AGENT_FTP_GATEWAY`, `ENABLE_POLLING_GATEWAY`, `ENABLE_CONTRACT_GATEWAY`;
- внешние интеграции: `ENABLE_BITRIX_GATEWAY`, `ENABLE_PYRUS_GATEWAY`, `ENABLE_MEGAFON_VATS`;
- очереди: `REDIS_ADDR`, `REDIS_PASSWORD`, `REDIS_DB`.

Frontend использует `front-ui/.env.example`; основной параметр для dev-прокси - `VITE_API_TARGET`.

## 10. Локальный запуск

Требования:

- Go для backend-модуля `etalon-server`;
- Go для `agent/`, если нужно собирать агента;
- Node.js 22+ и npm для frontend;
- PostgreSQL 16+;
- Redis 7+ для интеграционных очередей;
- MinIO/S3 для каталога агентских адаптеров и записей телефонии, если включены соответствующие контуры.

Backend:

```bash
cp .env.example .env
go mod download
go run ./cmd/etalon-server
```

Frontend:

```bash
cd front-ui
npm ci
npm run dev
```

Агент:

```bash
cd agent
go mod download
go run ./cmd/etalon-agent --config ./agent-config.json
```

Дополнительные режимы backend:

```bash
go run ./cmd/etalon-server --seed
go run ./cmd/etalon-server --seed-ftp-cache
go run ./cmd/etalon-server --reverse-seed
```

## 11. Docker

Локальная инфраструктура в `docker/docker-compose.yml` поднимает PostgreSQL, Redis, MinIO, `minio-init` и опциональный `agents-proxy`.

Сборка образов:

```bash
docker compose -f docker/docker-compose.build.yml build
```

Production-шаблон находится в `docker/prod-files/docker-compose.yml`. Подробный порядок деплоя, backup/restore и публикации адаптеров описан в `docker/DEPLOY.md`.

## 12. Тесты и проверки

Backend:

```bash
go test ./...
```

Agent:

```bash
cd agent
go test ./...
```

Frontend:

```bash
cd front-ui
npm run lint
npm run test
npm run test:e2e
```

Playwright E2E использует логин `admin/admin` и по умолчанию запускает Vite на `127.0.0.1:5178`. Детали - `front-ui/e2e/README.md`.

## 13. Важные документы

- `AGENTS.md` - рабочие правила для AI-агентов и архитектурная карта проекта;
- `docker/DEPLOY.md` - Docker, production, backup/restore, MinIO и релизы адаптеров;
- `agent/docs/ADAPTER_CONTRACT.md` - контракт core-agent и внешних адаптеров;
- `agent/docs/ADAPTER_RELEASE_LIFECYCLE.md` - публикация, promote и rollback адаптеров;
- `agent/docs/STAGE_REVIEW_AND_OPERATOR_FLOW.md` - операторский flow выбора адаптеров;
- `agent/docs/saga_runtime.md` - runtime многошаговых задач агента.
