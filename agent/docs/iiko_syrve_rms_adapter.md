# Iiko/Syrve RMS Adapter

## Назначение

`iiko-syrve-rms-adapter` — внешний адаптер для `core-agent`, который без входных путей определяет активное ПО `iiko` или `syrve`, извлекает `RMS URL` и возвращает результат по общему JSON-контракту адаптеров.

Адаптер реализован как отдельный бинарник и поддерживает команды:

- `describe`
- `health`
- `run`

Базовый общий контракт описан в `agent/docs/adapter_contract.md`.

## Текущий scope

На текущей версии адаптер поддерживает:

- определение активного типа ПО `iiko` или `syrve`;
- поиск только по стабильным внутренним путям под `%AppData%` и fallback-путям пользователей;
- выбор активного пути по детерминированному правилу свежести;
- извлечение `RMS URL` из `config.xml`;
- чтение полного снимка `config.xml` в плоский список настроек;
- чтение `CRMid` из `cash-server.log` для `iiko`;
- локальный inventory установленных плагинов `iikoFront`;
- задачу `task_type = "collect"` с расширенным результатом;
- задачи `soft_shutdown_front`, `inspect_autorun`, `ensure_autorun`, `read_front_config`.

Пока не входят в scope:

- динамическая конфигурация путей через `payload`;
- ручной ввод пути оператором;
- дополнительные источники RMS вне известных `config.xml`.

Ограничения текущей итерации:

- полная функциональность `collect` реализована для `iiko`;
- для `syrve` сейчас гарантируется discovery `config.xml` и `RMS URL`;
- `CRMid` и локальный scan плагинов для `syrve` пока возвращаются как частичный результат.

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
    "detect-rms",
    "read-crm-id",
    "list-plugins",
    "soft-shutdown-front",
    "inspect-autorun",
    "ensure-autorun",
    "read-front-config"
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

Сейчас адаптер поддерживает следующие `task_type`:

- `collect`
- `soft_shutdown_front`
- `inspect_autorun`
- `ensure_autorun`
- `read_front_config`

### Вход

Для `collect`, `soft_shutdown_front`, `inspect_autorun` и `read_front_config` `payload` может быть пустым.

Для `ensure_autorun` ожидается payload вида:

```json
{
  "method": "startup_user",
  "software_type": "iiko",
  "arguments": "",
  "task_name": "",
  "shortcut_name": ""
}
```

Поддержанные методы:

- `startup_user`
- `startup_common`
- `scheduler`

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

Для `collect` адаптер возвращает:

- `software_type`
- `rms_url`
- `crm_id`
- `plugins`

Для `soft_shutdown_front` адаптер возвращает:

- `software_type`
- `process_name`
- `matched_pids`
- `windows_closed`
- `close_sent`

Для `inspect_autorun` адаптер возвращает:

- `software_type`
- `entries`

Для `ensure_autorun` адаптер возвращает:

- `software_type`
- `method`
- `created`
- `updated`
- `path`
- `task_name`

Для `read_front_config` адаптер возвращает:

- `software_type`
- `source_file`
- `settings`

В `details` дополнительно могут возвращаться:

- `active_path`
- `matched_candidates`
- `source_file`
- `detection_reason`
- `cash_server_log`
- `front_executable`
- `plugins_root`
- `front_installation`
- `config_snapshot`

Пример успешного ответа:

```json
{
  "status": "success",
  "message": "Сбор данных завершён успешно",
  "result": {
    "software_type": "iiko",
    "rms_url": "https://demo.iiko.local/resto/",
    "crm_id": "1740537",
    "plugins": [
      {
        "name": "Transport",
        "api_version": "V9Preview7",
        "version": "9.7.20",
        "directory": "Resto.Front.Api.Transport.V9Preview7"
      }
    ]
  },
  "details": {
    "active_path": "C:\\Users\\demo\\AppData\\Roaming\\iiko\\cashserver\\config.xml",
    "source_file": "C:\\Users\\demo\\AppData\\Roaming\\iiko\\cashserver\\config.xml",
    "cash_server_log": "C:\\Users\\demo\\AppData\\Roaming\\iiko\\cashserver\\cash-server.log",
    "plugins_root": "C:\\Program Files\\iiko\\iikoRMS\\Front.Net\\Plugins"
  }
}
```

Пример `soft_shutdown_front`:

```json
{
  "protocol_version": "1",
  "request_id": "run-soft-stop-1",
  "task_type": "soft_shutdown_front",
  "payload": {}
}
```

Пример `inspect_autorun`:

```json
{
  "protocol_version": "1",
  "request_id": "run-autorun-inspect-1",
  "task_type": "inspect_autorun",
  "payload": {}
}
```

Пример `ensure_autorun`:

```json
{
  "protocol_version": "1",
  "request_id": "run-autorun-ensure-1",
  "task_type": "ensure_autorun",
  "payload": {
    "method": "startup_user",
    "software_type": "iiko"
  }
}
```

Пример `read_front_config`:

```json
{
  "protocol_version": "1",
  "request_id": "run-read-config-1",
  "task_type": "read_front_config",
  "payload": {}
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
