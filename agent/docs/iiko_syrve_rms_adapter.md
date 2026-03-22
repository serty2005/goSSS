# Iiko/Syrve RMS Adapter

## Назначение

`iiko-syrve-rms-adapter` — внешний адаптер для `core-agent`, который без входных путей определяет активное ПО `iiko` или `syrve`, извлекает `RMS URL` и возвращает результат по общему JSON-контракту адаптеров.

Адаптер реализован как отдельный бинарник и поддерживает команды:

- `describe`
- `health`
- `run`

Базовый общий контракт описан в `agent/docs/adapter_contract.md`.

## Текущий scope

На первой версии адаптер поддерживает:

- определение активного типа ПО `iiko` или `syrve`;
- поиск только по стабильным внутренним путям под `%AppData%` и fallback-путям пользователей;
- выбор активного пути по детерминированному правилу свежести;
- извлечение `RMS URL` из `config.xml`;
- задачу `task_type = "collect"` без входных путей;
- компактный `result` с полями `rms_url` и `software_type`.

Пока не входят в scope:

- динамическая конфигурация путей через `payload`;
- ручной ввод пути оператором;
- дополнительные типы задач кроме `collect`;
- дополнительные источники RMS вне известных `config.xml`.

## Идентичность адаптера

- `adapter_id`: `iiko-syrve-rms`
- `adapter_type`: `iiko-syrve-rms`
- `protocol_version`: `1`
- `target_os`: `windows`
- `target_arch`: `amd64`

## Стабильные внутренние пути поиска

Адаптер сам строит и проверяет только известные стабильные пути.

### Корни `%AppData%`

Порядок источников:

1. `%APPDATA%`
2. `%USERPROFILE%\\AppData\\Roaming`
3. `C:\\Users\\<user>\\AppData\\Roaming` для несистемных пользователей

Если у нескольких пользователей найдено подходящее дерево, все кандидаты участвуют в отборе, а tie-break выполняется детерминированно.

### Пути для `iiko`

- `%AppData%\\iiko`
- `%AppData%\\iiko\\cashserver`
- `%AppData%\\iiko\\CashServer`
- `%AppData%\\iiko\\cashserver\\config.xml`
- `%AppData%\\iiko\\CashServer\\config.xml`

### Пути для `syrve`

- `%AppData%\\syrve`
- `%AppData%\\syrve\\cashserver`
- `%AppData%\\syrve\\CashServer`
- `%AppData%\\syrve\\cashserver\\config.xml`
- `%AppData%\\syrve\\CashServer\\config.xml`

## Как определяется активный путь

Логика вынесена в отдельные модули `registry`, `detector` и `selector`.

Алгоритм такой:

1. Собираются все доступные `%AppData%`-корни.
2. Для каждого корня и каждого продукта (`iiko`, `syrve`) проверяются известные каталоги и `config.xml`.
3. Для каждого найденного кандидата строится набор activity-signals:
   `product root`, `cashserver/CashServer`, `config.xml`.
4. Внутри кандидата timestamp активности равен самому свежему `modTime` среди activity-signals.
5. Между кандидатами выбирается самый свежий.
6. Если timestamp одинаковый, раньше выбирается путь из более приоритетного `%AppData%`-корня.
7. Если и это не различает кандидатов, применяется лексикографический tie-break по пути.

Именно выбранный самый свежий signal-path возвращается в `details.active_path`.

## Как извлекается RMS URL

Логика вынесена в модуль `parser`.

Адаптер:

- ищет `serverUrl` в виде XML-элемента;
- ищет `serverUrl` в виде XML-атрибута;
- сначала разбирает самый свежий найденный `config.xml`;
- если `config.xml` найден, но `serverUrl` отсутствует или пустой, `health` возвращает `degraded`, а `run` — `partial`.

## Команда describe

Пример запуска:

```powershell
@'
{"protocol_version":"1","request_id":"describe-1"}
'@ | .\tmp\iiko-syrve-rms-adapter.exe describe
```

Пример ответа:

```json
{
  "adapter_id": "iiko-syrve-rms",
  "adapter_type": "iiko-syrve-rms",
  "version": "0.1.0-dev",
  "target_os": "windows",
  "target_arch": "amd64",
  "protocol_version": "1",
  "capabilities": [
    "inventory",
    "run-task",
    "collect",
    "detect-rms"
  ]
}
```

## Команда health

Команда проверяет:

- поддерживается ли текущее окружение;
- доступен ли `%AppData%`;
- какие известные пути `iiko/syrve` существуют;
- найдено ли поддерживаемое ПО;
- удалось ли извлечь `RMS URL`.

Пример запуска:

```powershell
@'
{"protocol_version":"1","request_id":"health-1","timeout_seconds":10}
'@ | .\tmp\iiko-syrve-rms-adapter.exe health
```

Возможные статусы:

- `ok` — найдено поддерживаемое ПО и извлечён `RMS URL`;
- `degraded` — ПО найдено, но `RMS URL` не извлечён, или поддерживаемое ПО не найдено;
- `error` — неподдерживаемая среда или отсутствуют доступные `%AppData%`-пути.

Пример успешного ответа:

```json
{
  "status": "ok",
  "message": "Адаптер готов к работе",
  "details": {
    "supported_environment": true,
    "software_type": "iiko",
    "rms_url": "https://demo.iiko.local/resto/",
    "active_path": "C:\\Users\\demo\\AppData\\Roaming\\iiko\\cashserver\\config.xml",
    "source_file": "C:\\Users\\demo\\AppData\\Roaming\\iiko\\cashserver\\config.xml"
  }
}
```

## Команда run

Сейчас поддерживается только `task_type = "collect"`.

### Вход

`payload` может быть пустым или содержать служебные поля, но пути адаптер игнорирует полностью.

Пример минимального запроса:

```json
{
  "protocol_version": "1",
  "request_id": "run-1",
  "task_type": "collect",
  "payload": {}
}
```

### Выход

Адаптер всегда возвращает компактный `result`:

- `rms_url`
- `software_type`

Дополнительно в `details` может возвращать:

- `active_path`
- `matched_candidates`
- `source_file`
- `detection_reason`

Пример успешного ответа:

```json
{
  "status": "success",
  "message": "RMS URL успешно определён",
  "result": {
    "rms_url": "https://demo.iiko.local/resto/",
    "software_type": "iiko"
  },
  "details": {
    "active_path": "C:\\Users\\demo\\AppData\\Roaming\\iiko\\cashserver\\config.xml",
    "source_file": "C:\\Users\\demo\\AppData\\Roaming\\iiko\\cashserver\\config.xml"
  }
}
```

Пример запроса с лишними путями, которые будут проигнорированы:

```json
{
  "protocol_version": "1",
  "request_id": "run-2",
  "task_type": "collect",
  "payload": {
    "paths": [
      "C:\\temp\\manual",
      "D:\\custom\\config.xml"
    ],
    "appdata_path": "C:\\temp\\fake-appdata"
  }
}
```

## Команды для сборки и локальной проверки

### 1. Сборка

```powershell
cd C:\self\repos\goSSS\agent
$env:GOOS='windows'
$env:GOARCH='amd64'
go build -o .\tmp\iiko-syrve-rms-adapter.exe .\cmd\iiko-syrve-rms-adapter
```

### 2. Проверка describe

```powershell
@'
{"protocol_version":"1","request_id":"describe-ws-1"}
'@ | .\tmp\iiko-syrve-rms-adapter.exe describe
```

### 3. Проверка health

```powershell
@'
{"protocol_version":"1","request_id":"health-ws-1","timeout_seconds":10}
'@ | .\tmp\iiko-syrve-rms-adapter.exe health
```

### 4. Проверка run с пустым payload

```powershell
@'
{
  "protocol_version": "1",
  "request_id": "run-ws-1",
  "task_type": "collect",
  "payload": {}
}
'@ | .\tmp\iiko-syrve-rms-adapter.exe run
```

### 5. Проверка run с минимальным future-proof payload

```powershell
@'
{
  "protocol_version": "1",
  "request_id": "run-ws-2",
  "task_type": "collect",
  "payload": {
    "requested_by": "local-smoke-test"
  }
}
'@ | .\tmp\iiko-syrve-rms-adapter.exe run
```

## Фикстуры для тестов

Для unit-тестов добавлены фикстуры искусственного дерева `%AppData%`:

- `agent/internal/iikosyrverms/testdata/fixtures/iiko_active.json`
- `agent/internal/iikosyrverms/testdata/fixtures/syrve_active.json`
- `agent/internal/iikosyrverms/testdata/fixtures/multi_candidate.json`
- `agent/internal/iikosyrverms/testdata/fixtures/missing_url.json`

Они материализуются во временный каталог и позволяют отдельно тестировать `detector`, `selector`, `parser` и `service`.

## Архитектура пакетов

- `contract` — JSON-контракт команд `describe`, `health`, `run`
- `domain` — доменные модели кандидатов, путей и scan-результата
- `registry` — централизованный список стабильных путей и поиск `%AppData%`
- `detector` — сбор кандидатов и известных путей
- `selector` — выбор самого свежего активного пути
- `parser` — извлечение `RMS URL` из `config.xml`
- `service` — orchestration всего поиска
- `command` — отдельные command handlers
- `cli` — разбор stdin/stdout JSON и маршрутизация команды
