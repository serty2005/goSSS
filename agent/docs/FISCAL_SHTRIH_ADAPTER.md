# Fiscal Shtrih Adapter

## Назначение

`fiscal-shtrih-adapter` — внешний адаптер для `core-agent`, который опрашивает ККТ Штрих через COM-драйвер `AddIn.DrvFR` и возвращает fiscal-данные по JSON-контракту адаптеров.

Адаптер реализован как отдельный бинарник и поддерживает команды:

- `describe`
- `health`
- `run`

Базовый общий контракт описан в `ADAPTER_CONTRACT.md`.

## Текущий scope

На текущем этапе адаптер поддерживает:

- явный запуск задачи `collect`;
- несколько endpoint-ов за один запуск;
- транспорт `tcp`;
- транспорт `com` с передачей `com_port` и `baudrate`;
- чтение обязательного payload старого Штрих-коллектора;
- получение `installed_driver` и `driver_version` из COM-драйвера;
- чтение лицензии в формате исходного `shtrih-kkt`.

Пока не входят в scope:

- автодетект устройств Штрих;
- серверная логика выдачи manifest;
- fallback-payload без ККТ внутри самого адаптера.

## Разрядность драйвера и бинарника

Для Штриха это критично.

Текущая реализация рабочего рантайма поддерживает только:

- `windows`
- `386`

Причина в том, что используется 32-битный COM-драйвер `AddIn.DrvFR`.

Поэтому рабочий production-бинарник для Штриха нужно собирать как `x86`:

```powershell
$env:GOOS='windows'
$env:GOARCH='386'
go build -o .\tmp\fiscal-shtrih-adapter-386.exe .\cmd\fiscal-shtrih-adapter
```

Если собрать `amd64`, `health` вернет `error`, а `run` не сможет подключиться к драйверу.

## Команда describe

Пример запуска:

```powershell
@'
{"protocol_version":"1","request_id":"describe-1"}
'@ | .\tmp\fiscal-shtrih-adapter-386.exe describe
```

## Команда health

Команда проверяет:

- поддерживается ли текущее окружение;
- зарегистрирован ли COM-драйвер `AddIn.DrvFR`;
- определяется ли версия драйвера.

Пример запуска:

```powershell
@'
{"protocol_version":"1","request_id":"health-1","timeout_seconds":10}
'@ | .\tmp\fiscal-shtrih-adapter-386.exe health
```

Ожидаемый успешный статус:

- `ok` — драйвер доступен и версия определена;
- `degraded` — драйвер найден, но версия не определена;
- `error` — неподдерживаемая сборка или драйвер отсутствует.

## Команда run

Сейчас поддерживается `task_type = "collect"`.

### Вход

Пример запроса с несколькими endpoint-ами:

```json
{
  "protocol_version": "1",
  "request_id": "run-1",
  "task_type": "collect",
  "payload": {
    "devices": [
      {
        "transport": "tcp",
        "ip": "192.168.0.90",
        "port": 5555
      },
      {
        "transport": "com",
        "com_port": "COM4",
        "baudrate": "115200"
      }
    ]
  }
}
```

Для `transport=com`:

- обязателен `com_port`;
- `baudrate` опционален и по умолчанию будет `115200`.

Для `transport=tcp`:

- обязателен `ip`;
- обязателен `port`.

### Выход

Адаптер возвращает:

- общий `status`: `success`, `partial` или `failed`;
- `result.devices[]` по каждому endpoint;
- исходный endpoint;
- статус конкретного устройства;
- диагностическое сообщение;
- `warnings` и `error`, если есть;
- fiscal payload устройства;
- служебные поля `connection_label`, `transport`, `driver_version`.

### Поля payload

Адаптер возвращает следующие поля предметных данных:

- `modelName`
- `serialNumber`
- `RNM`
- `organizationName`
- `fn_serial`
- `datetime_reg`
- `dateTime_end`
- `ofdName`
- `bootVersion`
- `ffdVersion`
- `INN`
- `address`
- `attribute_excise`
- `attribute_marked`
- `fnExecution`
- `installed_driver`
- `licenses`

Важно:

- host/remote metadata в ответ адаптера не включаются;
- `licenses` возвращается строкой, как в исходном `shtrih-kkt`.

## Команды для сборки и проверки на рабочей станции

### 1. Сборка

```powershell
cd C:\self\repos\goSSS\agent
$env:GOOS='windows'
$env:GOARCH='386'
go build -o .\tmp\fiscal-shtrih-adapter-386.exe .\cmd\fiscal-shtrih-adapter
```

### 2. Проверка describe

```powershell
@'
{"protocol_version":"1","request_id":"describe-ws-1"}
'@ | .\tmp\fiscal-shtrih-adapter-386.exe describe
```

### 3. Проверка health

```powershell
@'
{"protocol_version":"1","request_id":"health-ws-1","timeout_seconds":10}
'@ | .\tmp\fiscal-shtrih-adapter-386.exe health
```

### 4. Проверка run по TCP

Подставить реальный IP и порт ФР Штрих:

```powershell
@'
{
  "protocol_version": "1",
  "request_id": "run-ws-tcp-1",
  "task_type": "collect",
  "payload": {
    "devices": [
      {
        "transport": "tcp",
        "ip": "192.168.0.90",
        "port": 5555
      }
    ]
  }
}
'@ | .\tmp\fiscal-shtrih-adapter-386.exe run
```

### 5. Проверка run по COM

Подставить реальный COM-порт:

```powershell
@'
{
  "protocol_version": "1",
  "request_id": "run-ws-com-1",
  "task_type": "collect",
  "payload": {
    "devices": [
      {
        "transport": "com",
        "com_port": "COM4",
        "baudrate": "115200"
      }
    ]
  }
}
'@ | .\tmp\fiscal-shtrih-adapter-386.exe run
```

### 6. Проверка нескольких устройств за один запуск

```powershell
@'
{
  "protocol_version": "1",
  "request_id": "run-ws-multi-1",
  "task_type": "collect",
  "payload": {
    "devices": [
      {
        "transport": "tcp",
        "ip": "192.168.0.90",
        "port": 5555
      },
      {
        "transport": "com",
        "com_port": "COM4",
        "baudrate": "115200"
      }
    ]
  }
}
'@ | .\tmp\fiscal-shtrih-adapter-386.exe run
```

## Ограничения и follow-up

Нужно помнить про следующие доработки:

- автодетект Штриха еще не перенесен;
- production-поставка должна учитывать обязательную `x86`-сборку;
- реальный smoke-тест нужно выполнять именно на машине с установленным COM-драйвером и доступным оборудованием.
