# Fiscal Mitsu Adapter

## Назначение

`fiscal-mitsu-adapter` — внешний адаптер для `core-agent`, который опрашивает ККТ Mitsu по собственному Mitsu-протоколу через `COM` или `TCP` и возвращает fiscal-данные по JSON-контракту адаптеров.

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
- Mitsu-протокол поверх COM и TCP;
- LRC, Windows-1251 и разбор XML-подобных ответов устройства;
- чтение обязательного payload старого Mitsu-коллектора без host/remote metadata;
- попытку определить `installed_driver` по версии `MitsuCube.exe`.

Пока не входят в scope:

- автодетект Mitsu по COM-портам;
- ARP/network-scan из старого `POSRelayd`;
- перезапись `connect.json`;
- серверная логика выдачи manifest.

## Особенности окружения

В отличие от Штриха, Mitsu-адаптер не завязан на отдельный vendor DLL/COM-bridge.

Практически это означает:

- сам опрос ККТ выполняется по протоколу адаптера;
- `health` ищет `MitsuCube.exe` в стандартных путях и пытается прочитать его версию;
- отсутствие `MitsuCube.exe` не обязательно мешает выполнить `run`, но тогда `installed_driver` вернется как `Error`.

Стандартные пути поиска:

- рядом с бинарником адаптера;
- текущая рабочая директория;
- `C:\Program Files\MITSU.1-F\MitsuCube.exe`;
- `C:\Program Files (x86)\MITSU.1-F\MitsuCube.exe`.

## Команда describe

Пример запуска:

```powershell
@'
{"protocol_version":"1","request_id":"describe-1"}
'@ | .\tmp\fiscal-mitsu-adapter.exe describe
```

## Команда health

Команда проверяет:

- поддерживается ли текущее окружение;
- найден ли `MitsuCube.exe`;
- определяется ли его версия.

Пример запуска:

```powershell
@'
{"protocol_version":"1","request_id":"health-1","timeout_seconds":10}
'@ | .\tmp\fiscal-mitsu-adapter.exe health
```

Возможные статусы:

- `ok` — `MitsuCube.exe` найден и версия определена;
- `degraded` — `MitsuCube.exe` не найден или его версия не определена;
- `error` — проблема самого процесса проверки.

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
        "ip": "10.127.1.124",
        "port": 8200
      },
      {
        "transport": "com",
        "com_port": "COM7",
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
- `port` опционален и по умолчанию будет `8200`.

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
- `licenses` возвращается строкой `"None"`, как в старом Python-коде.

## Команды для сборки и проверки на рабочей станции

### 1. Сборка

Для обычной 64-битной рабочей станции Windows:

```powershell
cd C:\self\repos\goSSS\agent
$env:GOOS='windows'
$env:GOARCH='amd64'
go build -o .\tmp\fiscal-mitsu-adapter.exe .\cmd\fiscal-mitsu-adapter
```

Если рабочая станция 32-битная, вместо этого использовать:

```powershell
$env:GOOS='windows'
$env:GOARCH='386'
go build -o .\tmp\fiscal-mitsu-adapter-386.exe .\cmd\fiscal-mitsu-adapter
```

Далее в примерах ниже используется `fiscal-mitsu-adapter.exe`.

### 2. Проверка describe

```powershell
@'
{"protocol_version":"1","request_id":"describe-ws-1"}
'@ | .\tmp\fiscal-mitsu-adapter.exe describe
```

### 3. Проверка health

```powershell
@'
{"protocol_version":"1","request_id":"health-ws-1","timeout_seconds":10}
'@ | .\tmp\fiscal-mitsu-adapter.exe health
```

### 4. Проверка run по TCP

Подставить реальный IP Mitsu:

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
        "ip": "10.127.1.124",
        "port": 8200
      }
    ]
  }
}
'@ | .\tmp\fiscal-mitsu-adapter.exe run
```

### 5. Проверка run по COM

Подставить реальный COM-порт Mitsu:

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
        "com_port": "COM7",
        "baudrate": "115200"
      }
    ]
  }
}
'@ | .\tmp\fiscal-mitsu-adapter.exe run
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
        "ip": "10.127.1.124",
        "port": 8200
      },
      {
        "transport": "com",
        "com_port": "COM7",
        "baudrate": "115200"
      }
    ]
  }
}
'@ | .\tmp\fiscal-mitsu-adapter.exe run
```

## Ограничения и follow-up

Нужно помнить про следующие доработки:

- автодетект Mitsu по COM и сети еще не перенесен;
- поиск Mitsu через ARP и сетевое сканирование еще не перенесен;
- реальный smoke-тест нужно выполнять на машине с доступным Mitsu-оборудованием и корректным endpoint.
