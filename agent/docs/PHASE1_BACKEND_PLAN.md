# План Phase 1 для бэкенда goSSSagent

## Назначение

Этот документ нужен команде бэкенда перед продолжением миграции `POSRelayd -> goSSSagent`.
Сейчас важно не переносить дальше предметную логику старого агента, пока не будет прозрачно подтверждён и диагностируем процесс регистрации нового `core-agent`.

Документ основан на текущем коде:

- `agent/internal/runtime/agent.go`
- `agent/internal/client/servicedesk_client.go`
- `agent/internal/config/config.go`
- `agent/internal/state/store_windows.go`
- `internal/transport/http/handlers/agent_handler.go`
- `internal/services/agentauth/service.go`
- `internal/services/agent_service.go`
- `internal/domain/models/models.go`

## Текущее состояние относительно плана миграции

### Что уже реализовано

- В репозитории уже есть отдельная `Phase 0` спецификация: `agent/docs/PHASE0_MIGRATION_SPEC.md`.
- Текущий `goSSSagent` уже работает как `core-agent`, а не как прямой перенос всего `POSRelayd` один в один.
- Реализованы:
  - bootstrap-регистрация агента;
  - хранение `agent_uuid` и токенов;
  - refresh токенов;
  - heartbeat;
  - inventory snapshot;
  - приём `adapter_manifests`;
  - локальная синхронизация бинарников адаптеров;
  - self-update `core-agent`.

### Что из исходного плана переноса ещё не начато или не завершено

- Исходный `Phase 1` из `POSRelayd_to_goSSSagent_migration_plan.md` про локальные `SQLite/device_snapshots/fiscal_links/outbox/validation_state` пока не реализован.
- В агенте пока нет `internal/storage/sqlite`.
- В агенте пока нет перенесённых collectors `Atol/Mitsu/Shtrih`.
- В агенте пока нет старой file-based или новой persisted-модели снимков `POSRelayd`.
- Паритет HostInfo с `POSRelayd` пока только частичный:
  - есть `hostname`, интерфейсы, COM-порты, список установленного ПО, `known_components`;
  - нет реального сбора `TeamViewerID/AnyDeskID/RustDeskID/LiteManagerID/url_rms`.

### Вывод

Перед началом исходного `Phase 1` по старому плану нужно закрыть backend-часть вокруг регистрации и наблюдаемости `core-agent`, иначе дальнейшая миграция collectors пойдёт вслепую.

## Проверенный процесс регистрации

### Откуда агент берёт конфиг и локальное состояние

- Локальный конфиг сейчас не хранится в отдельном JSON-файле.
- Источник конфигурации на текущий момент: встроенные значения в `agent/internal/config/config.go`.
- Серверный URL и bootstrap key сейчас зашиты в агент:
  - `BootstrapServerURL`
  - `BootstrapAPIKey`
- Рабочий каталог агента:
  - `C:\ProgramData\<Company>\<AgentName>`
- Каталог адаптеров:
  - `C:\ProgramData\<Company>\<AgentName>\adapters`
- Identity и токены хранятся в реестре:
  - `HKLM\Software\<Company>\<AgentName>`
- Токены в реестре сохраняются в DPAPI machine scope.

### Как выполняется регистрация

1. Агент вычисляет machine fingerprint из `MachineGuid + GOOS + GOARCH`.
2. Агент читает или создаёт локальный `agent_uuid` в `HKLM`.
3. Агент пытается использовать локальные токены.
4. Если валидных токенов нет, агент делает `POST /api/agents/register`.
5. Для регистрации используется заголовок:
   `Authorization: Bearer <bootstrap_api_key>`
6. При успехе сервер возвращает `access_token` и `refresh_token`.
7. Агент сохраняет токены локально в реестр.
8. Дальше heartbeat идёт через `POST /api/agents/{uuid}/data` с `Authorization: Bearer <access_token>`.
9. Если `/{uuid}/data` возвращает `401`, агент сначала пытается сделать refresh токена.
10. Если refresh не удался, агент снова идёт в bootstrap-регистрацию.

## Какие данные агент реально отправляет на регистрацию

Текущее JSON-тело регистрации:

```json
{
  "agent_uuid": "<uuid из HKLM>",
  "hostname": "<os.Hostname()>",
  "agent_version": "<версия бинарника агента>",
  "machine_fingerprint": "<sha256(machine_guid|os|arch)>",
  "initial_data": {
    "hostname": "<os.Hostname()>",
    "current_time": "<RFC3339>",
    "uuid": "<тот же agent_uuid>",
    "agent_type": "sssruner",
    "agent_version": "<версия бинарника агента>"
  },
  "system_info": {
    "os": "<runtime.GOOS>",
    "arch": "<runtime.GOARCH>",
    "hostname": "<os.Hostname()>",
    "agent_process": "<AgentProcessName>",
    "registry_path": "<путь HKLM без префикса HKLM>"
  }
}
```

Важно:

- bootstrap key не входит в JSON-тело, он уходит только в заголовок `Authorization`.
- На регистрации агент пока не отправляет `inventory`.
- `inventory` и `adapter_statuses` отправляются только в heartbeat.

## Что сервер реально использует и сохраняет сейчас

### На маршруте `/api/agents/register`

- Сервер требует bootstrap-авторизацию через `Authorization: Bearer`.
- Проверяется точное совпадение значения с `AGENT_API_KEY` сервера.
- Если ключ не совпал, сервер возвращает `401`.

### После приёма запроса

Сейчас сервер сохраняет в модели `Agent` только:

- `uuid`
- `hostname`
- `version`
- `type`
- `last_heartbeat`
- `status`

### Что сервер сейчас не сохраняет напрямую

- `machine_fingerprint`
- `system_info`
- полное тело registration request
- историю попыток регистрации
- причину последнего отказа в регистрации

### Что это означает для UI

- UI может показать только то, что реально лежит в `agents`.
- Поэтому со стороны сервера сейчас нормально видеть в основном только `hostname/version/type`.
- Это не значит, что агент отправляет только `hostname`.
- Это значит, что серверный слой сейчас не сохраняет и не экспонирует остальные поля регистрации.

## Выявленные проблемы

1. Наблюдаемость регистрации недостаточна.
2. На сервере нет сохранённого следа полного registration payload.
3. При `401` нет удобной связки:
   - какой именно payload отправлялся;
   - какой был `agent_uuid`;
   - это была первичная регистрация или повторная после неудачного refresh.
4. Документация репозитория расходится с кодом:
   - `docs/ARCHITECTURE.md` пишет, что `/register` без авторизации;
   - текущий код реально требует bootstrap key.
5. DTO heartbeat уже умеет принимать `inventory` и `adapter_statuses`, но серверная модель пока не хранит это как отдельный наблюдаемый срез.

## План Phase 1 для команды бэкенда

### Цель

Сделать регистрацию `goSSSagent` прозрачной, диагностируемой и пригодной для UI/поддержки до начала дальнейшего переноса collectors.

### Задачи

1. Синхронизировать документацию с реальным контрактом регистрации.
   Нужно обновить минимум:
   - `docs/ARCHITECTURE.md`
   - `README.md`

2. Зафиксировать backend-контракт bootstrap-регистрации.
   Нужно явно записать:
   - endpoint;
   - обязательный `Authorization: Bearer <AGENT_API_KEY>`;
   - idempotent-поведение при повторной регистрации одного `agent_uuid`;
   - формат ответов `200/401/409`.

3. Добавить серверное хранение метаданных регистрации.
   Минимум нужен актуальный срез:
   - `last_registration_at`
   - `last_registration_status`
   - `last_registration_error`
   - `machine_fingerprint`
   - `system_info jsonb`
   - `registration_payload jsonb`

4. Рассмотреть отдельную таблицу истории регистраций.
   Предпочтительный вариант:
   - `agent_registrations`
   - одна запись на одну попытку регистрации
   - с полями `agent_uuid`, `created_at`, `status`, `error`, `payload`, `remote_addr`

5. Сделать API/UI для просмотра последней регистрации агента.
   В UI должны быть видны:
   - `agent_uuid`
   - время последней регистрации
   - результат последней регистрации
   - `machine_fingerprint`
   - `system_info`
   - последнее registration body

6. Добавить понятную диагностику `401`.
   Нужно различать минимум три причины:
   - отсутствует заголовок `Authorization`
   - неверный формат заголовка
   - неверный bootstrap key

7. Подготовить сохранение heartbeat snapshot.
   До переноса collectors нужен хотя бы актуальный срез для:
   - `inventory`
   - `adapter_statuses`
   Это можно хранить как `jsonb`, если нормализация пока не нужна.

8. Подтвердить серверную семантику повторной регистрации.
   По текущему коду сервер повторно выдаёт токены при регистрации того же `agent_uuid`.
   Это нужно либо официально подтвердить как допустимое поведение, либо изменить осознанно.

## Минимальный результат Phase 1

`Phase 1` можно считать закрытым для backend-поддержки, если:

- мы видим в UI не только `hostname`, но и реальный registration payload;
- видно, была ли последняя попытка регистрации успешной или отклонённой;
- `401` диагностируется без чтения исходников;
- документация совпадает с кодом;
- последняя heartbeat-полезная нагрузка доступна хотя бы как `latest snapshot`.

## Что не нужно делать в этой фазе

- Не нужно уже сейчас проектировать полный backend под `Atol/Mitsu/Shtrih`.
- Не нужно переносить в сервер file-based semantics старого `POSRelayd`.
- Не нужно нормализовывать весь inventory по отдельным таблицам, если на первом шаге достаточно `jsonb`.
- Не нужно начинать UI-слой collectors, пока не видна и не подтверждена регистрация `core-agent`.

## Короткий итог

Текущий `goSSSagent` уже дошёл до состояния `Phase 0 core-agent`.
Следующий практический блок для backend-команды не про collectors, а про прозрачную регистрацию:

- принять,
- сохранить,
- показать,
- диагностировать.

Без этого дальнейший перенос `POSRelayd` в новый агент будет трудно отлаживать и подтверждать на стенде.
