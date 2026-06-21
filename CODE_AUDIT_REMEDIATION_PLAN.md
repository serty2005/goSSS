# План устранения проблем промежуточного аудита goSSS

## Назначение

Документ фиксирует задачи, выявленные при промежуточном аудите backend, frontend, Windows agent и Docker-инфраструктуры.
Он предназначен для последующего переноса задач в Plane без повторного исследования исходного кода.

Аудит выполнен по состоянию репозитория на 19 июня 2026 года.

## Шкала приоритетов

- `P0` — риск компрометации, потери данных или подтвержденная ошибка конкурентного runtime. Исправлять до функциональной разработки и production-релиза.
- `P1` — существенный риск надежности, прав доступа, воспроизводимости сборки или эксплуатации. Исправлять следующим этапом.
- `P2` — накопленный технический долг, мешающий безопасным изменениям и контролю качества.
- `P3` — упрощения и удаление лишнего кода без изменения пользовательского поведения.

## Общий порядок выполнения

1. Закрыть публичные административные маршруты, небезопасные секреты и доступ к infrastructure services.
2. Устранить подтвержденные уязвимости зависимостей и гонку event bus.
3. Зафиксировать права ролей, HTTP-лимиты и управляемые миграции.
4. Восстановить зеленые проверки frontend и agent.
5. После стабилизации CI выполнять архитектурную декомпозицию и упрощения.

## Уже исправлено

### DONE-FE-01. Цикл поиска на странице агентов

**Статус:** выполнено в текущем рабочем дереве.

**Проблема:** верхняя строка поиска и локальный поиск `AgentsPage` независимо синхронизировали debounced-значения через параметр `q`. Устаревшее значение могло перезаписать URL и запустить бесконечное переключение состояния.

**Решение:** `AgentsPage` записывает `q` только для локального пользовательского ввода. Изменение URL извне синхронизирует поле, но не создает обратную запись.

**Проверка:** добавлен Playwright-сценарий, который проверяет стабильный URL, отсутствие browser errors и ограниченное число запросов `/api/agent-diagnostics`.

**Файлы:**

- `front-ui/src/pages/AgentsPage.tsx`
- `front-ui/e2e/desktop-regressions.e2e.ts`

---

# P0 — критические задачи

## SEC-01. Закрыть публичные административные маршруты

**Проблема**

Маршруты `/sync/seed` и `/debug/bus` зарегистрированы вне защищенной группы `/api`.
Seed использует query-параметр `key` с известным fallback-значением, а debug endpoint не требует авторизации.

**Риски**

- несанкционированный запуск наполнения базы;
- изменение или загрязнение production-данных;
- раскрытие внутреннего состояния event bus;
- попадание ключа seed в access logs, browser history и proxy logs.

**Работы**

1. Перенести административные endpoints под JWT-группу `/api`.
2. Ограничить seed и debug ролью `admin`.
3. Убрать передачу секретов через query string.
4. Решить, нужен ли runtime seed endpoint в production вообще. Если нет — исключить маршрут из обычного server runtime.
5. Добавить отрицательные тесты для запроса без JWT, с ролью `intern` и с ролью `support_specialist`.
6. Обновить Swagger и ближайшую эксплуатационную документацию.

**Критерии приемки**

- анонимный запрос к seed/debug получает `401`;
- пользователь без роли `admin` получает `403`;
- секрет seed не передается в URL;
- production-конфигурация может полностью отключить seed endpoint либо endpoint отсутствует в production router;
- тесты проверяют матрицу доступа.

**Проверки**

```bash
go test ./internal/app ./internal/transport/http/handlers ./internal/transport/http/middleware
go test ./...
```

**Файлы для начала:** `internal/app/app.go`, `internal/transport/http/handlers/sync_handlers.go`, `internal/transport/http/handlers/debug_handler.go`.

## SEC-02. Запретить запуск с небезопасными секретами

**Проблема**

Backend имеет известные fallback-значения `JWT_SECRET`, `ADMIN_PASSWORD` и `SEEDER_KEY`. Пустой `AGENT_API_KEY` отключает проверку ключа legacy getad endpoint.

**Риски**

- выпуск поддельных JWT;
- предсказуемая учетная запись администратора при первом запуске;
- доступ к seed и legacy agent ingestion;
- случайный production-запуск с development defaults.

**Работы**

1. Удалить секретные fallback-значения из `config.New()`.
2. Добавить явную startup-валидацию обязательных секретов.
3. Ввести явно названный development-режим, если локальный старт без секретов действительно нужен.
4. Не менять legacy `/api/submit_json` на другой auth flow в рамках этой задачи.
5. Зафиксировать отдельно поведение getad при пустом `AGENT_API_KEY`: production должен завершать запуск с ошибкой либо endpoint должен быть явно отключен.
6. Проверять минимальную длину JWT secret и запрещенные шаблонные значения вроде `change-me`.
7. Обновить `.env.example` и `docker/.env.prod.example`, не добавляя реальные секреты.

**Критерии приемки**

- production startup завершается понятной ошибкой при пустом или шаблонном обязательном секрете;
- в исходном коде нет рабочих паролей и ключей по умолчанию;
- локальный development-сценарий описан и требует осознанного включения;
- legacy getad endpoint не становится анонимным из-за пропущенной переменной окружения;
- есть unit-тесты startup-валидации.

**Проверки**

```bash
go test ./internal/infra/config ./internal/app
go test ./...
```

**Файлы для начала:** `internal/infra/config/config.go`, `.env.example`, `docker/.env.prod.example`, `internal/transport/http/handlers/agent_handler.go`.

## SEC-03. Закрыть инфраструктурные порты и записи телефонии

**Проблема**

Production compose публикует PostgreSQL, Redis и MinIO API на всех интерфейсах. Redis запускается без аутентификации. `minio-init` назначает anonymous download не только каталогу адаптеров, но и bucket записей телефонии.

**Риски**

- прямой сетевой доступ к БД и Redis;
- чтение или изменение служебных данных в обход backend;
- публичное раскрытие записей разговоров;
- нарушение требований к персональным данным.

**Работы**

1. Убрать публикацию PostgreSQL и Redis из production compose либо привязать только к `127.0.0.1`, если локальный доступ обязателен.
2. Убрать внешнюю публикацию MinIO API, если к нему обращаются только контейнеры.
3. Включить пароль Redis и согласовать его с `REDIS_PASSWORD` backend.
4. Удалить anonymous policy с bucket записей телефонии.
5. Выдавать доступ к записи через backend authorization и ограниченный по времени presigned URL.
6. Оставить публичным только каталог адаптеров, если это действительно часть действующего agent contract.
7. Добавить MinIO healthcheck и использовать `service_healthy` вместо `service_started` там, где это возможно.
8. Обновить `docker/DEPLOY.md`.

**Критерии приемки**

- БД и Redis недоступны с внешнего интерфейса production host;
- Redis отклоняет запросы без пароля;
- анонимный запрос к записи телефонии получает отказ;
- авторизованный пользователь получает ограниченную ссылку через backend;
- каталог адаптеров продолжает работать для агента;
- compose проходит валидацию.

**Проверки**

```bash
docker compose -f docker/docker-compose.yml config --quiet
docker compose -f docker/docker-compose.yml up -d
```

Дополнительно проверить доступ к портам и объектам MinIO извне docker network.

**Файлы для начала:** `docker/docker-compose.yml`, `docker/DEPLOY.md`, `internal/services/megafon_vats_recording_service.go`, `internal/infra/s3store`.

## REL-01. Устранить гонку и неконтролируемую конкурентность event bus

**Проблема**

`go test -race ./...` подтверждает гонку: `Start()` записывает `logger`, пока `Publish()` читает его. Event handlers запускаются отдельными goroutine без лимита и без ожидания при shutdown.

**Риски**

- data race в production;
- неконтролируемое количество goroutine при всплеске событий;
- незавершенные операции при остановке приложения;
- скрытая потеря событий после пятисекундного ожидания заполненной очереди.

**Работы**

1. Передавать logger в конструктор event bus либо защищать его тем же mutex; предпочтителен неизменяемый после создания bus.
2. Определить и реализовать ограничение параллельности callback handlers.
3. Включить handlers в graceful shutdown или явно документировать fire-and-forget контракт.
4. Сделать счетчик dropped events и наблюдаемую метрику заполнения очереди.
5. Не закрывать publish-channel, пока возможны publishers.
6. Добавить тесты конкурентного `Start`, `Publish`, subscribe/unsubscribe и shutdown.

**Критерии приемки**

- `go test -race ./...` проходит;
- число одновременно работающих handlers ограничено;
- shutdown либо дожидается handlers, либо отменяет их через context по документированному контракту;
- dropped events видны в логах и метриках;
- обычный SSE-сценарий не регрессирует.

**Проверки**

```bash
go test ./pkg/eventbus ./internal/core/processing
go test -race ./pkg/eventbus ./internal/core/processing
go test -race ./...
```

**Файлы для начала:** `pkg/eventbus/eventbus.go`, `internal/app/app.go`.

## SEC-04. Обновить уязвимые Go-зависимости

**Проблема**

`govulncheck ./...` находит пять вызываемых уязвимостей `golang.org/x/net/html` версии `v0.53.0`. Вызов идет из parser отчетов договоров. Исправление доступно начиная с `v0.55.0`.

**Работы**

1. Обновить `golang.org/x/net` минимум до исправленной версии.
2. Обновить связанные `golang.org/x/*` модули только в объеме, требуемом совместимостью.
3. Прогнать parser на существующих XLS/HTML отчетах и поврежденных входных данных.
4. Проверить ограничения размера входных отчетов.
5. Зафиксировать `govulncheck` в CI для backend и agent отдельно.

**Критерии приемки**

- `govulncheck ./...` не показывает вызываемых уязвимостей;
- импорт договоров обрабатывает валидные файлы как раньше;
- некорректный HTML возвращает контролируемую ошибку;
- изменения `go.mod` и `go.sum` не содержат необъяснимого массового обновления зависимостей.

**Проверки**

```bash
go test ./internal/services/contract ./...
govulncheck ./...
```

**Файлы для начала:** `go.mod`, `go.sum`, `internal/services/contract/report_parser.go`.

## SEC-05. Обновить уязвимые frontend-зависимости

**Проблема**

`npm audit --omit=dev` сообщает семь production advisory. Среди прямых зависимостей затронуты `axios` и `react-router-dom`; транзитивно затронуты `dompurify`, `markdown-it`, `form-data` и `monaco-editor`.

**Работы**

1. Обновить прямые зависимости до исправленных совместимых версий.
2. Обновить владельцев уязвимых транзитивных зависимостей, не добавляя `overrides` без необходимости.
3. Проверить migration notes React Router и Ant Design через Context7 перед изменением API.
4. Прогнать login, protected routes, redirects, editor, markdown и Monaco-сценарии.
5. Добавить `npm audit --omit=dev` в CI с контролируемой политикой исключений.

**Критерии приемки**

- `npm audit --omit=dev` не содержит high/critical advisory;
- router не допускает open redirect через protocol-relative URL;
- login/logout и protected routes работают;
- rich text, markdown и Monaco продолжают отображаться;
- lock-файл соответствует `package.json`.

**Проверки**

```bash
cd front-ui
npm ci
npm audit --omit=dev
npm run lint
npm run test
npm run build
npm run test:e2e
```

**Файлы для начала:** `front-ui/package.json`, `front-ui/package-lock.json`.

---

# P1 — надежность и контроль доступа

## SEC-06. Зафиксировать матрицу ролей для задач сверки

**Проблема**

`POST /api/tasks/{id}/resolve` и `POST /api/tasks/{id}/create-entity-in-sd` находятся только под JWT middleware. Любая активная роль, включая `intern`, технически может вызвать изменение.

**Работы**

1. Подтвердить допустимые роли для чтения задач, разрешения задач и создания сущностей во внешнем ServiceDesk.
2. Зафиксировать матрицу прав тестами router/middleware.
3. Добавить `RequireAnyRole` на мутирующие маршруты согласно подтвержденной матрице.
4. Проверить frontend route guards и доступность действий в UI.
5. Проверить остальные handler `RegisterRoutes`, подключенные только под общей JWT-группой.

**Критерии приемки**

- для каждого task endpoint определены допустимые роли;
- backend запрещает операции независимо от frontend;
- UI скрывает или блокирует недоступные действия;
- тесты покрывают `admin`, `support_specialist`, `intern` и отсутствие роли.

**Файлы для начала:** `internal/transport/http/handlers/task_handler.go`, `internal/app/app.go`, `front-ui/src/utils/permissions.ts`.

## SEC-07. Ужесточить JWT transport и проверку алгоритма

**Проблема**

JWT middleware принимает token из query string и разрешает любой HMAC signing method, хотя issuer использует HS256.

**Работы**

1. Для обычных HTTP endpoints принимать JWT только из `Authorization: Bearer`.
2. Если query token нужен для конкретного legacy/SSE-сценария, изолировать исключение на одном маршруте и документировать срок миграции.
3. Ограничить parser алгоритмом HS256.
4. Проверять обязательные `exp`, `iat`, `sub` и тип roles.
5. Рассмотреть `issuer` и `audience`, если они могут быть заданы без нарушения существующих клиентов.
6. Добавить тесты подмены алгоритма, истекшего JWT и token в URL.

**Критерии приемки**

- query token не авторизует обычный API-запрос;
- принимается только ожидаемый signing algorithm;
- невалидные claims дают `401` без внутренних деталей;
- SSE продолжает работать согласованным способом.

**Файлы для начала:** `internal/transport/http/middleware/middleware.go`, `internal/services/auth_service.go`, `front-ui/src/features/realtime`.

## SEC-08. Ограничить HTTP body и время соединений

**Проблема**

HTTP server не задает `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout` и `IdleTimeout`. JSON handlers и webhooks читают body без общего лимита; часть кода использует `io.ReadAll`.

**Работы**

1. Задать server timeouts с учетом SSE и долгих license endpoints.
2. Ограничить request body через общий middleware или `http.MaxBytesReader` на trust boundary.
3. Для больших договорных и файловых payload задать отдельные документированные лимиты.
4. Использовать `json.Decoder.DisallowUnknownFields()` только для стабильных внутренних DTO; legacy agent payload требует отдельного решения совместимости.
5. Возвращать `413` при превышении лимита.
6. Добавить тесты oversized body и slow request там, где это практически воспроизводимо.

**Критерии приемки**

- каждый публичный webhook и JSON endpoint имеет размерный предел;
- oversized payload не аллоцируется целиком и получает `413`;
- slow headers не удерживают соединение бесконечно;
- SSE и загрузка допустимых файлов работают.

**Файлы для начала:** `internal/app/app.go`, `internal/transport/http/handlers/*_webhook_handler.go`, `internal/transport/http/handlers/agent_handler.go`.

## SEC-09. Снизить последствия XSS для пользовательской сессии

**Проблема**

JWT и профиль пользователя сохраняются Zustand persist middleware в `localStorage`. Любой успешный XSS получает долговременный access token.

**Работы**

1. Выбрать целевой способ сессии: HttpOnly Secure SameSite cookie либо короткоживущий token только в памяти с безопасным refresh flow.
2. Не внедрять cookie-модель без backend CSRF-защиты и решения для SSE.
3. Добавить logout/revocation поведение и очистку старого `etalon-auth-storage` при миграции.
4. Проверить multi-tab logout и истечение сессии.
5. Добавить Content Security Policy на proxy/frontend уровне как отдельный защитный слой.

**Критерии приемки**

- JavaScript не имеет доступа к долговременному credential;
- state-changing cookie requests защищены от CSRF;
- login, refresh, logout и SSE покрыты тестами;
- старое localStorage-состояние безопасно мигрируется или удаляется.

**Зависимости:** выполнять после `SEC-07` и согласования auth contract.

**Файлы для начала:** `front-ui/src/store/authStore.ts`, `front-ui/src/api/axios.ts`, `internal/services/auth_service.go`, frontend Nginx config.

## SEC-10. Заменить самописную HTML-санитизацию

**Проблема**

`sanitizeRichHtml` вручную фильтрует DOM и блокирует только буквальный префикс `javascript:`. Нет явного allowlist протоколов и централизованной политики для ссылок и изображений.

**Работы**

1. После обновления зависимостей использовать поддерживаемый sanitizer как прямую dependency.
2. Задать allowlist тегов, атрибутов и URL-схем.
3. Отдельно определить правила для `/api/static`, `https`, `mailto`, изображений и внутренних user links.
4. Удалить дублирующую ручную DOM-фильтрацию.
5. Добавить regression corpus: encoded javascript, data URL, SVG, malformed HTML, duplicate attributes, target blank.
6. Добавить CSP, не рассматривая sanitizer как единственную защиту.

**Критерии приемки**

- опасные payload не создают исполняемый DOM;
- допустимое форматирование статей и тикетов сохраняется;
- ссылки `_blank` получают `noopener noreferrer`;
- тесты используют реальные browser DOM semantics.

**Зависимости:** `SEC-05`.

**Файлы для начала:** `front-ui/src/utils/sanitizeRichHtml.ts`, `front-ui/src/utils/safeHtml.tsx`, компоненты rich text.

## DB-01. Перейти от startup AutoMigrate к версионированным миграциям

**Проблема**

При каждом старте backend выполняет общий `AutoMigrate`, очистку orphan records и дедупликацию ФР. Изменение схемы и данных связано с запуском приложения, не имеет версии и контролируемого rollback.

**Работы**

1. Выбрать существующий минимальный migration tool для PostgreSQL; не создавать собственный framework.
2. Зафиксировать baseline текущей production-схемы.
3. Перенести cleanup/dedup операции в идемпотентные версионированные миграции.
4. Разделить команду применения миграций и обычный runtime startup.
5. Добавить проверку schema version перед запуском приложения.
6. Описать backup, apply и rollback procedure в `docker/DEPLOY.md`.

**Критерии приемки**

- повторный старт backend не изменяет схему и данные;
- чистая БД поднимается последовательностью миграций;
- существующая БД обновляется без потери данных;
- неуспешная миграция останавливает deploy до старта нового приложения;
- процесс проверен на копии production schema.

**Проверки:** отдельный integration test PostgreSQL для empty DB и upgrade from baseline.

**Файлы для начала:** `internal/infra/db/db.go`, `internal/app/app.go`, `docker/DEPLOY.md`.

## AG-01. Восстановить воспроизводимые проверки agent

**Проблема**

Документированная команда `cd agent && go test ./...` не работает на Linux. `internal/state` состоит только из Windows-файлов; iiko stubs не экспортируют используемые ошибки и функции; четыре JSON fixture отсутствуют.

**Текущее подтверждение:** `GOOS=windows GOARCH=amd64 go build ./...` и `GOOS=windows GOARCH=386 go build ./...` проходят.

**Работы**

1. Определить официальную CI-матрицу agent: Linux compile checks плюс Windows tests либо полностью Windows runner.
2. Добавить минимальные non-Windows stubs, необходимые для компиляции runtime tests, без имитации DPAPI/HKLM поведения.
3. Выровнять API Windows и stub файлов `iikosyrverms/shutdown` и `autorun`.
4. Вернуть отсутствующие fixture JSON в `testdata/fixtures`.
5. Отделить Windows-only tests build tags.
6. Добавить `govulncheck` в поддерживаемой target-среде.

**Критерии приемки**

- документированная команда agent tests проходит в выбранной CI-среде;
- Windows amd64 и 386 build продолжают проходить;
- Windows-specific поведение HKLM/DPAPI проверяется Windows tests;
- test fixtures находятся в Git и доступны из чистого clone;
- README/AGENTS содержат реально работающие команды.

**Файлы для начала:** `agent/internal/state`, `agent/internal/iikosyrverms`, `agent/internal/iikosyrverms/testsupport/fixtures.go`, `AGENTS.md`.

## QA-01. Сделать mock API строгим

**Проблема**

Общий Playwright mock отвечает `200 OK` и `data: null` для любого неизвестного endpoint. Опечатка URL или отсутствующий mock может не сломать тест и скрыть регрессию API-контракта.

**Работы**

1. Заменить fallback на явный failure: `404/501` и запись неизвестного request в диагностическое сообщение.
2. Добавить необходимые mocks для существующих E2E-сценариев, включая `/agent-diagnostics`.
3. Разрешать passthrough только явно перечисленным static/dev запросам.
4. Добавить self-test fixture, подтверждающий отказ для неизвестного endpoint.

**Критерии приемки**

- неизвестный API request немедленно валит E2E с понятным URL и методом;
- все существующие E2E используют явно определенные ответы;
- mock contract соответствует JSON envelope backend.

**Файлы для начала:** `front-ui/e2e/fixtures/mockApi.ts`.

---

# P2 — качество и сопровождаемость

## FE-02. Вернуть зеленый frontend lint

**Проблема**

`npm run lint` завершается с 8 ошибками и 7 предупреждениями. Основная ошибка — чтение и изменение refs во время render в `AgentOperatorFlowCard`. Есть missing dependencies и нестабильные expressions в hooks.

**Работы**

1. Удалить ref-based mutation во время render из hydration flow.
2. Сделать hydration snapshot обычным memoized derived value либо переносить синхронизацию в effect/event boundary.
3. Исправить missing dependencies без отключения eslint rules.
4. Стабилизировать массивы и callbacks только там, где это требуется hooks-контрактом.
5. Добавить smoke/regression tests для повторной гидратации adapter profiles.

**Критерии приемки**

- `npm run lint` проходит без errors и warnings;
- hydration не затирает несохраненный пользовательский ввод;
- существующие 24 unit tests и agent UI tests проходят;
- не добавлены `eslint-disable` без локально документированной причины.

**Файлы для начала:** `front-ui/src/components/agents/AgentOperatorFlowCard.tsx`, `AgentAdapterRunsCard.tsx`, `NewTicketModal.tsx`, `AdminTranslationsPage.tsx`, `CompanyPage.tsx`.

## FE-03. Завершить миграцию deprecated API Ant Design 6

**Проблема**

В коде остаются как минимум 208 `Space.direction`, 3 `destroyOnClose`, 6 `Statistic.valueStyle`, deprecated `Alert.message`, Drawer width и индекс в `Table.rowKey`.

**Работы**

1. Сверить актуальные replacements через Context7 для установленной Ant Design 6.
2. Выполнить механическую замену поддерживаемых props.
3. Отдельно проверить случаи, где новая семантика отличается: Modal/Drawer lifecycle и Table row keys.
4. Устранить warnings из unit tests.
5. Добавить запрет deprecated API в review checklist либо lint, если доступно без нового тяжелого инструмента.

**Критерии приемки**

- frontend tests не печатают Ant Design deprecation warnings;
- modal/drawer сохраняют ожидаемое состояние открытия и очистки;
- все таблицы используют стабильный domain key;
- desktop/mobile E2E проходят.

## ARCH-01. Вернуть доступ к БД за service/repository boundary

**Проблема**

Несколько handlers напрямую используют `*gorm.DB`: articles, materials, translations, reports, agent diagnostics и observation feed. Это нарушает основной путь `handler -> service -> repository` и усложняет transaction/error tests.

**Работы**

1. Разбирать по одному bounded context, начиная с самого изменяемого.
2. Перенести query и transaction logic из handler в существующий service/repository.
3. Оставить handler ответственным за HTTP parsing, authorization, DTO mapping и response.
4. Не создавать интерфейс на каждый однострочный helper; интерфейс нужен на реальной границе тестирования или инфраструктуры.
5. Сохранить HTTP contract и status codes.

**Критерии приемки**

- production handlers не импортируют GORM;
- бизнес-транзакции тестируются на service level;
- HTTP tests используют service stubs;
- изменения выполняются отдельными небольшими PR по контекстам.

**Файлы для начала:** `internal/transport/http/handlers/article_handler.go`, `material_handler.go`, `translations_handler.go`, `report_handler.go`, `agent_diagnostics_handler.go`, `agent_observation_feed_handler.go`.

## ARCH-02. Разделить наиболее крупные модули по ответственности

**Проблема**

Крупнейшие файлы объединяют несколько независимых процессов:

- `internal/infra/repositories/agent_observation_repo.go` — более 3400 строк;
- `front-ui/src/pages/TicketDetailsPage.tsx` — более 2800 строк;
- `internal/services/ticket_service.go` — более 2500 строк;
- `front-ui/src/components/common/HeaderSearch.tsx` — более 1500 строк.

**Работы**

1. Сначала зафиксировать characterization tests для изменяемого поведения.
2. Разделять по уже существующим обязанностям, а не по произвольному размеру файла.
3. Для agent observations выделить query groups и reconciliation operations.
4. Для ticket service выделить comments/files/history/integration orchestration только при подтвержденной независимости.
5. Для UI вынести самостоятельные панели и hooks с узким контрактом.
6. Не вводить новые global stores или framework abstractions только ради декомпозиции.

**Критерии приемки**

- каждый новый модуль имеет одну понятную ответственность;
- публичные API и пользовательское поведение не изменены;
- зависимости между модулями не образуют циклов;
- diff состоит преимущественно из перемещения кода и локальных тестов;
- декомпозиция идет отдельными задачами, а не одним большим PR.

## QA-02. Поднять покрытие критического runtime

**Проблема**

Нулевое покрытие имеют event bus, DB migrations, core workers, Naumen client, S3 store и несколько domain services. HTTP handlers покрыты примерно на 9,7%.

**Работы**

1. Не ставить общий процент как самоцель.
2. Добавить минимальные tests для рисковых контрактов:
   - event delivery/shutdown/backpressure;
   - migration baseline/upgrade;
   - worker cancellation и feature flags;
   - external client timeout/retry/error mapping;
   - S3 access policy и presigned URL;
   - role-protected routes.
3. Публиковать coverage artifact в CI.
4. Ввести запрет на снижение покрытия изменяемого пакета, если CI позволяет сделать это без нестабильности.

**Критерии приемки**

- каждый P0/P1 fix имеет regression test;
- background workers проверяются на остановку по context;
- интеграционные клиенты тестируются через `httptest.Server` или локальный контейнер;
- CI сохраняет coverage report.

## CFG-01. Сделать конфигурацию строгой и наблюдаемой

**Проблема**

Ошибочные integer, bool и duration значения молча заменяются defaults. Это скрывает опечатки production `.env` и может менять интервалы или отключать защитные настройки без явной ошибки.

**Работы**

1. Разделить чтение конфигурации и `Validate()`.
2. Возвращать ошибку для заданного, но некорректного значения.
3. Проверять обязательные URL, диапазоны parallelism/rate limits, положительные интервалы и зависимости feature flags.
4. Выводить при старте безопасную сводку активных модулей без секретов.
5. Не логировать URL, содержащие credentials/webhook tokens.

**Критерии приемки**

- опечатка в production env останавливает startup с именем переменной;
- defaults применяются только к отсутствующим необязательным значениям;
- секреты не попадают в error/log output;
- config tests покрывают invalid, missing и boundary values.

## QA-03. Добавить единый обязательный CI quality gate

**Проблема**

Отдельные команды существуют, но текущее состояние допускает одновременно падающий lint, agent tests, race test и dependency audit.

**Работы**

1. Сформировать независимые jobs: backend, frontend, agent Windows build/tests, security audit, E2E smoke.
2. Использовать `npm ci`, а не изменяющую lock-файл установку.
3. Добавить `go test -race` минимум для конкурентных пакетов, затем расширить на backend.
4. Добавить `govulncheck` и `npm audit --omit=dev`.
5. Кэшировать зависимости, но не build artifacts, способные скрыть missing files.
6. Сделать jobs обязательными для merge после закрытия текущих известных падений.

**Критерии приемки**

- чистый clone воспроизводит CI локальными командами;
- merge блокируется при lint/test/build/security failure;
- Windows agent targets собираются;
- неизвестный E2E API request валит job;
- список временных исключений пуст либо имеет владельца и срок удаления.

---

# P3 — упрощение и удаление лишнего

## CLEAN-01. Удалить `cmd/test_logger`

**Проблема:** отдельный runtime command вручную демонстрирует logger, не используется сборкой и документацией и не является автоматической проверкой.

**Работы:** удалить command; необходимые сценарии logger оставить в `internal/infra/logger` tests.

**Критерии приемки:** logger tests проходят, production build не меняется, одна лишняя точка входа удалена.

## CLEAN-02. Удалить passthrough `NetworkCandidateService`

**Проблема:** service полностью делегирует четыре метода repository без validation, orchestration или transaction logic.

**Работы:** передать handler существующий repository interface либо разместить контракт на стороне потребителя; удалить service implementation и wiring.

**Критерии приемки:** HTTP contract и tests не меняются; лишний service и constructor удалены; новый interface не создается без необходимости.

## CLEAN-03. Использовать `net.IP.IsPrivate()`

**Проблема:** `IsPrivateIP` каждый вызов вручную парсит три CIDR, хотя стандартная библиотека Go предоставляет `IP.IsPrivate()`.

**Работы:** сохранить текущую validation error для невалидного IP и заменить проверки CIDR одним вызовом stdlib.

**Критерии приемки:** тесты покрывают IPv4 private/public, IPv6 private/public и invalid input.

## CLEAN-04. Механически применить возможности Go 1.26

**Проблема:** в коде остаются устаревшие формы `interface{}`, `wg.Add + go + Done`, `errors.As` и helpers для получения указателя на значение.

**Работы**

1. Заменять только механически эквивалентные конструкции.
2. Использовать `any`, `WaitGroup.Go`, `errors.AsType` и `new(value)`.
3. Не смешивать эту задачу с изменением бизнес-логики.
4. Делить изменения по пакетам, чтобы review оставался проверяемым.

**Критерии приемки:** `gofmt`, `go test`, `go vet` и `go test -race` проходят; поведение не изменено; diff не содержит новых abstractions.

---

# Рекомендуемая группировка в Plane

## Epic 1. Production Security Baseline

- `SEC-01` Публичные административные маршруты
- `SEC-02` Обязательные секреты
- `SEC-03` Infrastructure exposure и записи телефонии
- `SEC-04` Go vulnerabilities
- `SEC-05` Frontend vulnerabilities
- `SEC-07` JWT hardening
- `SEC-08` HTTP limits
- `SEC-09` Session storage
- `SEC-10` HTML sanitization

## Epic 2. Runtime Reliability

- `REL-01` Event bus race и backpressure
- `DB-01` Версионированные миграции
- `AG-01` Agent build/test reproducibility
- `CFG-01` Строгая конфигурация

## Epic 3. Authorization and Quality Gates

- `SEC-06` Матрица прав задач
- `QA-01` Строгий mock API
- `QA-02` Покрытие критического runtime
- `QA-03` CI quality gate
- `FE-02` Зеленый frontend lint
- `FE-03` Ant Design 6 migration

## Epic 4. Architecture Simplification

- `ARCH-01` Удаление GORM из handlers
- `ARCH-02` Декомпозиция крупных модулей
- `CLEAN-01` Удаление test logger command
- `CLEAN-02` Удаление passthrough service
- `CLEAN-03` Использование stdlib для private IP
- `CLEAN-04` Механическая модернизация Go

# Definition of Done для задач аудита

Задача считается завершенной, если одновременно выполнено следующее:

- исправлено исходное поведение или удален подтвержденный долг;
- добавлена минимальная regression-проверка;
- обновлена ближайшая документация, если изменился runtime contract;
- не добавлены реальные секреты или production data;
- `go test ./...`, релевантный `go test -race`, frontend lint/test/build и целевые E2E проходят;
- для dependency-задач проходит соответствующий security scanner;
- в описании PR указаны способ проверки и известные ограничения.
