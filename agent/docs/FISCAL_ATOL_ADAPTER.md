# Fiscal Atol Adapter

## Назначение

`fiscal-atol-adapter` — внешний адаптер для `core-agent`, который опрашивает ККТ АТОЛ через драйвер `fptr10.dll` и возвращает fiscal-данные по JSON-контракту адаптеров.

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
- выбор ветки драйвера `10.8` или `10.9+` по версии установленного `fptr10.dll`;
- чтение обязательного payload старого Атол-коллектора без host/remote metadata.

Пока не входят в scope:

- автодетект АТОЛ по COM-портам;
- enrich inventory для описаний COM-портов;
- серверная логика выдачи manifest;
- fallback-payload без ККТ внутри самого адаптера.

## Разрядность драйвера и бинарника

Это важный практический момент.

На Windows адаптер должен быть собран в той же разрядности, что и установленный `fptr10.dll`.

На текущей тестовой машине обнаружен только 32-битный драйвер:

- `C:\Program Files (x86)\ATOL\Drivers10\KKT\bin\fptr10.dll`
- версия драйвера: `10.10.8.0`

Поэтому успешный запуск здесь выполнялся 32-битным бинарником:

```powershell
$env:GOARCH='386'
go build -o .\tmp\fiscal-atol-adapter-386.exe .\cmd\fiscal-atol-adapter
```

Если собрать адаптер как `amd64`, а в системе установлен только `x86`-драйвер, загрузка DLL завершится ошибкой вида:

```text
%1 is not a valid Win32 application
```

Для production это означает, что поставка адаптера должна учитывать разрядность драйвера Атол на клиентской машине.

## Команда describe

Пример запуска:

```powershell
'{"protocol_version":"1","request_id":"describe-1"}' | .\tmp\fiscal-atol-adapter-386.exe describe
```

Пример ответа:

```json
{
  "adapter_id": "fiscal-atol",
  "adapter_type": "fiscal-atol",
  "version": "0.1.0-dev",
  "target_os": "windows",
  "target_arch": "386",
  "protocol_version": "1",
  "capabilities": [
    "run-task",
    "collect",
    "multi-device"
  ]
}
```

## Команда health

Команда проверяет:

- поддерживается ли текущее окружение;
- найден ли драйвер Атол;
- определяется ли версия драйвера;
- какая ветка драйвера выбрана.

Пример запуска:

```powershell
'{"protocol_version":"1","request_id":"health-1","timeout_seconds":10}' | .\tmp\fiscal-atol-adapter-386.exe health
```

Пример успешного ответа:

```json
{
  "status": "ok",
  "message": "Адаптер готов к работе",
  "details": {
    "supported": true,
    "driver_present": true,
    "driver_path": "C:\\Program Files (x86)\\ATOL\\Drivers10\\KKT\\bin\\fptr10.dll",
    "driver_version": "10.10.8.0",
    "driver_variant": "10.9+"
  }
}
```

Возможные статусы:

- `ok`
- `degraded`
- `error`

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
        "ip": "10.25.1.22",
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
- `licenses` уже реализованы;
- в ветке драйвера `10.8` поля `attribute_marked` и `fnExecution` могут быть недоступны, тогда адаптер не падает, а пишет предупреждение.

### Пример запуска

```powershell
'{"protocol_version":"1","request_id":"run-1","task_type":"collect","payload":{"devices":[{"transport":"tcp","ip":"10.25.1.22","port":5555}]}}' | .\tmp\fiscal-atol-adapter-386.exe run
```

## Результат smoke-теста

На текущей машине был выполнен реальный опрос ККТ по endpoint:

- `tcp://10.25.1.22:5555`

Результат:

- `health` вернул `ok`;
- `run` вернул `status = success`;
- драйвер определён как `10.10.8.0`;
- выбрана ветка `10.9+`;
- payload вернулся заполненным, включая лицензии.

Это подтверждает, что первый рабочий вертикальный срез адаптера уже реально работает с установленным драйвером Атол и доступным сетевым ФР.

## Ограничения и follow-up

Нужно помнить про следующие доработки:

- автодетект COM-портов АТОЛ ещё не перенесён;
- для надёжного `*ATOL*`-детекта в inventory позже понадобится описание COM-порта, а не только имя порта;
- для production-поставки нужно заранее определить стратегию по `x86/x64` бинарникам адаптера.
