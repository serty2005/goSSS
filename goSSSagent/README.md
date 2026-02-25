# goSSSagent (текущий этап)

Новый агент (`sssruner`) разрабатывается отдельно от старых пассивных агентов (`getad`/репортеров через `/api/submit_json`).

На текущем этапе агент решает только задачи:

1. Регистрация на сервере ServiceDesk.
2. Периодический `heartbeat` для онлайн-статуса рабочей станции.
3. Самообновление по задаче `self_update`, полученной в ответ на `heartbeat`.

## Важно

- Файл `goSSSagent/1prompt` не удаляется.
- В нём содержатся важные инструкции по извлечению `CRMid` из логов `iikoFront`.

## Разделение типов агентов

- `getad` (старый пассивный агент / сборщик данных):
  - использует `/api/submit_json` (и совместимые старые схемы);
  - не использует регистрацию нового агента;
  - не получает задачи на выполнение.

- `sssruner` (новый активный агент, этот проект):
  - регистрируется через `/api/agents/register`;
  - получает `access/refresh` токены;
  - отправляет `heartbeat` через `/api/agents/{uuid}/data`;
  - получает задачи в ответ на `heartbeat`.

## Брендинг и имя агента

Используется общий бренд проекта:

- `BRAND_NAME=MyHoreca_Xenion`

Из второй части бренда формируется имя агента:

- `XenionAgent`
- бинарник/процесс: `XenionAgent.exe`
- ветка реестра: `HKLM\Software\MyHoreca\XenionAgent`

Примечание: фактическое имя процесса зависит от имени собранного `.exe`.

## Конфигурация (на текущем этапе)

На целевой Windows-машине агент **не требует env-конфига**.

Параметры подключения зашиты в код:

- URL тестового сервера: `http://10.25.1.125:8080`
- bootstrap API key (используется только для регистрации)
- тип агента: `sssruner`

## Как работает идентичность агента (UUID + fingerprint)

Агент работает только на Windows и хранит состояние в системном реестре (`HKLM`).

При старте агент:

1. Читает `MachineGuid` Windows.
2. Вычисляет hash fingerprint машины.
3. Сравнивает fingerprint с сохранённым в реестре.
4. Если fingerprint совпадает:
   - использует существующий `AgentUUID`.
5. Если fingerprint отличается (клон диска, замена платы и т.п.):
   - очищает старые значения в ветке агента (UUID и токены),
   - генерирует новый `AgentUUID`,
   - выполняет полную повторную регистрацию как нового агента.

## Где хранится состояние агента

Реестр:

- `HKLM\Software\MyHoreca\XenionAgent`

Ключевые значения:

- `AgentUUID`
- `MachineFingerprintHash`
- `AccessTokenEnc` (DPAPI, machine scope)
- `RefreshTokenEnc` (DPAPI, machine scope)
- `AccessTokenExpiresAt`
- `RefreshTokenExpiresAt`
- `LastTokenRefreshAt`

## Аутентификация нового агента

### 1) Первичная регистрация

- Маршрут: `POST /api/agents/register`
- Авторизация: `Authorization: Bearer <AGENT_API_KEY>` (bootstrap key)
- Сервер возвращает:
  - `access_token` (24 часа)
  - `refresh_token`
  - сроки действия токенов

### 2) Heartbeat и получение задач

- Маршрут: `POST /api/agents/{uuid}/data`
- Авторизация: `Authorization: Bearer <access_token>`

### 3) Обновление токенов

- Маршрут: `POST /api/agents/auth/refresh`
- Агент отправляет `agent_uuid` + `refresh_token`
- Сервер выдает новую пару токенов

Если `refresh_token` недействителен или просрочен, агент повторно проходит bootstrap-регистрацию.

## Что уже реализовано

- Windows-only хранение identity и токенов в `HKLM`.
- Fingerprint-механизм с авто-сбросом identity при несовпадении.
- Шифрование токенов в реестре через DPAPI (machine scope).
- Регистрация нового агента с выдачей токенов.
- Refresh токенов.
- Heartbeat по `access_token`.
- Получение задач только для `sssruner`.
- Каркас workflow + saga runner.
- Workflow `self_update`.

## Что пока не реализовано

- Расширенный набор системных параметров для подтверждения регистрации оператором.
- Отправка результатов выполнения задач обратно на сервер.
- Очередь/ретраи задач с сохранением статуса выполнения.
- Набор рабочих сценариев (очистка диска, управление процессами/службами и т.д.).
- Подпись обновлений (сейчас проверяется только `sha256`, если передан).

## Сборка (Windows)

Из каталога `goSSSagent`:

```powershell
go test ./...
go build -o .\bin\XenionAgent.exe .\cmd\etalon-agent
```

## Формат задачи самообновления (`self_update`)

```json
{
  "id": 123,
  "type": "self_update",
  "payload": {
    "version": "0.1.1",
    "download_url": "https://example.local/agent/XenionAgent.exe",
    "sha256": "hex_sha256_опционально",
    "file_name": "XenionAgent.exe",
    "args": []
  }
}
```

## Архитектурный задел

- `internal/runtime` — цикл агента, heartbeat, auth, маршрутизация задач.
- `internal/state` — Windows-реестр, fingerprint, токены.
- `internal/workflows` — сценарии выполнения задач.
- `internal/saga` — последовательность шагов с компенсациями.
