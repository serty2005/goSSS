# План переноса функционала POSRelayd в goSSSagent

## Назначение документа

Этот документ фиксирует:

- полный перечень функционала, который сейчас реализован в `POSRelayd`;
- точные исходники, из которых этот функционал нужно забирать;
- целевую карту переноса в `goSSSagent`;
- согласованные ограничения на перенос;
- пошаговый план реализации для следующих сессий.

Документ рассчитан на практическую работу в следующей сессии, уже внутри репозитория `goSSSagent`.

## Зафиксированные решения

На момент составления этого плана приняты следующие решения:

1. Новый агент **не занимается организацией Windows-службы самостоятельно**.
   Управление службой будет выполняться через внешний `nssm`.
   Значит, в `goSSSagent` не нужен собственный код уровня `ServiceFramework`, `golang.org/x/sys/windows/svc` или аналогичный service-wrapper, если он не требуется для другого функционала.

2. Сбор `AnyDeskID` в новом агенте нужно делать **через вызов `anydesk.exe`**, по той же идее, как сейчас собирается `RustDeskID` через вызов `rustdesk.exe`.
   Логика чтения `system.conf` из Python **не переносится**.

3. Механизм самообновления берем **из текущего `goSSSagent`** как базовый.
   Сложный механизм из `POSRelayd/ftp-updater` целиком **не переносится**.
   Из старого updater можно забирать только отдельные требования, если они позже понадобятся.

4. Основная цель миграции:
   перенести **предметную логику сбора, проверки и отправки данных**, а не файловую и процессную архитектуру Python-проекта.

5. Функционал удаленного shell-доступа (`ra/*`) **не включается в обязательный scope первого production-переноса**, если отдельно не будет принято иное решение.

## Целевая цель миграции

Нужно получить production-версию `goSSSagent`, которая:

- сохраняет данные и поведение `POSRelayd`, влияющие на собираемые и передаваемые данные;
- работает как обычный Windows-процесс, запускаемый под `nssm`;
- использует текущий Go-runtime агента как основу;
- не повторяет избыточную архитектуру `POSRelayd`;
- не теряет логику сбора KKT-данных, корреляции ФР/ФН и доставки полезной нагрузки.

## Что уже есть в goSSSagent

Текущий `goSSSagent` уже содержит полезный каркас:

- регистрация агента, heartbeat и получение задач:
  `internal/runtime/agent.go`
- HTTP-клиент к ServiceDesk API:
  `internal/client/servicedesk_client.go`
- хранение identity и токенов в реестре + DPAPI:
  `internal/state/store_windows.go`
- fingerprint машины:
  `internal/state/fingerprint_windows.go`
- простой планировщик задач:
  `internal/services/scheduler.go`
- workflow и saga для `self_update`:
  `internal/workflows/self_update.go`
- механизм скачивания и применения обновления:
  `internal/updater/updater.go`

Это хороший фундамент, но функционального паритета с `POSRelayd` пока нет.

## Что считается обязательным для переноса

### Обязательные блоки

- сбор данных Atol;
- сбор данных Mitsu;
- интеграция с `shtrihscanner.exe` как внешним helper;
- сбор host/remote metadata;
- ведение соответствий `serialNumber -> fn_serial -> v_time`;
- валидация ФР/ФН по логам;
- доставка полезной нагрузки на сервер;
- production-конфигурирование;
- production-логирование;
- использование существующего Go-self-update;
- запуск под `nssm`.

### Необязательные или исключенные из первого релиза блоки

- Python-реализация `remote shell` через WebSocket и `cmd.exe`;
- отдельный Python `ftp-updater` как самостоятельный mini-project;
- файловая схема хранения `date/*.json`, `_resources/fiscals.json`, `_resources/uuid`;
- Python-шифрование через `Fernet`;
- собственный код управления Windows-службой внутри агента.

## Полная карта функционала POSRelayd

### 1. Оркестрация и жизненный цикл

Источник:

- `posrelaydsc.py`
- `about.py`

Что делает сейчас:

- запускает сбор данных Atol и Mitsu;
- при необходимости запускает `shtrihscanner.exe`;
- запускает валидацию ФР/ФН;
- отправляет собранные данные;
- запускает updater;
- при включенном RA запускает WebSocket-клиент для удаленного доступа;
- держит основной жизненный цикл службы.

Что переносим:

- только порядок и смысл этапов runtime;
- без Python-службы;
- без логики self-kill процесса;
- без `os._exit` как штатного механизма управления жизненным циклом.

Куда переносим:

- `internal/runtime`
- `internal/services`
- `cmd/etalon-agent`

### 2. Конфигурация

Источник:

- `service/configs.py`
- `service.json`
- `connect.json`

Что делает сейчас:

- хранит дефолтную структуру конфигов;
- задает режимы подключения к KKT;
- задает параметры sender/validation/updater/notification/RA;
- создает конфиги, если они отсутствуют.

Что переносим:

- всю структуру настроек, относящуюся к runtime и предметной логике;
- семантику полей `type_connect`, `tcp_timeout`, validation settings, sender settings, notification settings;
- конфиг updater только в той части, которая нужна новому Go-updater.

Что не переносим как есть:

- автосоздание JSON-файлов Python-утилитой;
- структуру `remote-access.json` для shell-доступа;
- старую updater-конфигурацию FTP/HTTP в полном объеме.

Куда переносим:

- `internal/config`
- при необходимости `configs/*.json` или `config.example.json` рядом с агентом

### 3. Локальное состояние и общие системные операции

Источник:

- `service/sys_manager.py`

Что делает сейчас:

- хранит пути к `date`, `_resources/fiscals.json`, `_resources/uuid`;
- генерирует/читает UUID;
- выполняет шифрование/дешифрование;
- ведет `fiscals.json`;
- удаляет устаревшие записи;
- управляет subprocess, taskkill, tasklist;
- проверяет сеть;
- сканирует подсеть;
- получает hostname, IP, MAC, версию файлов.

Что переносим:

- модель `fiscal_links`: `serialNumber`, `fn_serial`, `v_time`;
- общие host utilities;
- network check и network scan в той части, где они нужны Mitsu;
- получение версий бинарников и драйверов;
- модель cleanup устаревших записей.

Что не переносим:

- `uuid` в файле;
- `Fernet`;
- файловое хранилище `date/*.json`;
- хаотичное управление процессами через `taskkill`/`tasklist`;
- роль `god object`.

Куда переносим:

- `internal/storage/sqlite`
- `internal/hostinfo`
- `internal/system`
- `internal/domain/fiscal`

### 4. Сбор данных Atol

Источник:

- `getdata/atol/atol.py`
- `getdata/atol/comautodetect.py`
- `getdata/atol/libfptr108.py`
- `getdata/atol/libfptr109.py`

Что делает сейчас:

- определяет установленную версию драйвера;
- выбирает нужную ветку библиотеки `libfptr`;
- подключается к Atol по USB/COM/TCP;
- умеет автодетект virtual COM-портов;
- читает общие данные ККТ;
- читает данные регистрации и ФН;
- читает лицензии;
- формирует итоговый payload;
- пишет `serialNumber/fn_serial/v_time` в `fiscals.json`;
- при отсутствии KKT создает fallback payload хоста.

Что переносим обязательно:

- алгоритм подключения и приоритетов режимов подключения;
- payload и все его поля;
- fallback-сценарий без KKT;
- обновление связи `ФР -> ФН`;
- извлечение driver version;
- извлечение licenses.

Что важно отдельно:

- `libfptr108.py` и `libfptr109.py` считаем vendor boundary, а не шаблоном архитектуры;
- переносим поведение и интеграционный контракт, а не Python-обертки построчно.

Куда переносим:

- `internal/collectors/atol`
- `internal/domain/fiscal`

### 5. Сбор данных Mitsu

Источник:

- `getdata/mitsu.py`

Что делает сейчас:

- поддерживает COM и TCP/IP;
- сам реализует протокол обмена;
- считает LRC;
- умеет автодетект Mitsu на COM-портах;
- умеет искать Mitsu в локальной сети через ARP и сетевой скан;
- обновляет `connect.json` при автодетекте;
- читает модель, серийник, регистрационные данные, ФН и прочие атрибуты;
- формирует итоговый payload;
- обновляет `fiscals.json`.

Что переносим обязательно:

- протокол Mitsu;
- COM/TCP режимы;
- автодетект COM;
- поиск в сети;
- payload и все его поля;
- обновление связи `ФР -> ФН`.

Что переносим с изменением реализации:

- вместо перезаписи `connect.json` можно использовать persisted state/DB-конфиг;
- сами алгоритмы обнаружения сохраняем.

Куда переносим:

- `internal/collectors/mitsu`
- `internal/domain/fiscal`
- `internal/storage/sqlite`

### 6. Интеграция Shtrih

Источник:

- `getdata/shtrih.py`

Что делает сейчас:

- при необходимости запускает внешний `shtrihscanner.exe`;
- ждет завершения;
- ищет в `date/*.json` данные по устройствам, которых еще нет в `fiscals.json`;
- добавляет найденные `serialNumber/fn_serial/v_time` в базу соответствий.

Что переносим:

- orchestration helper-процесса;
- импорт результатов во внутреннее хранилище;
- бизнес-идею: Shtrih живет через внешний scanner.

Что не переносим:

- файловую форму межпроцессного обмена как обязательный контракт.

Куда переносим:

- `internal/collectors/shtrih`
- `internal/system/process`
- `internal/storage/sqlite`

### 7. Сбор host и Remote ID

Источник:

- `getdata/get_remote.py`

Что делает сейчас:

- ищет AppData активного пользователя;
- ищет fallback-пользователя по папкам `C:\Users`;
- читает `serverUrl` из `iiko\cashserver\config.xml` или `syrve\cashserver\config.xml`;
- собирает `TeamViewerID` из реестра;
- собирает `RustDeskID` через вызов `rustdesk.exe --get-id`;
- собирает `AnyDeskID` из `system.conf`;
- собирает `LiteManagerID` из реестра.

Что переносим:

- поиск user AppData;
- чтение `serverUrl`;
- TeamViewer/LiteManager сбор;
- RustDesk через `rustdesk.exe --get-id`;
- **AnyDesk через `anydesk.exe` по аналогии с RustDesk**, а не через `system.conf`.

Что нужно зафиксировать как отдельное правило:

- поведение сборщика `AnyDeskID` меняется относительно Python-версии намеренно;
- остальная логика сборщика переносится без функциональных изменений.

Куда переносим:

- `internal/hostinfo`
- возможно подпакеты:
  `internal/hostinfo/remoteids`
  `internal/hostinfo/iiko`

### 8. Корреляция ФР/ФН и валидация по логам

Источник:

- `service/fn_check.py`
- `service/sys_manager.py`

Что делает сейчас:

- проверяет, насколько давно валидировалась каждая KKT;
- если `v_time` устарел, пытается подтвердить актуальность пары `serialNumber/fn_serial` по логам;
- для Atol и Mitsu использует разные паттерны поиска;
- при успешном совпадении обновляет `v_time`;
- при несовпадении считает ФН замененным;
- может отправлять уведомление;
- может запускать reboot script;
- может отключать проверку и удалять запись, если логов нет слишком долго.

Что переносим обязательно:

- весь алгоритм бизнес-валидации;
- semantics полей `trigger_days`, `interval`, `forced`, `target_time`, `delete_days`;
- обновление `v_time`;
- очистку неактуальных записей;
- сценарий “несовпадение ФР/ФН -> reboot workflow”.

Что переносим с изменением реализации:

- вместо прямого запуска `reboot.bat` делаем управляемый reboot action/workflow;
- вместо `WaitForSingleObject` и file-based state используем runtime + storage.

Куда переносим:

- `internal/validation`
- `internal/storage/sqlite`
- `internal/runtime`

### 9. Отправка данных

Источник:

- `service/connectors.py`

Что делает сейчас:

- собирает список JSON-файлов из каталога `date`;
- расшифровывает URL/API key при необходимости;
- отправляет payload на каждый URL в списке;
- использует `Content-Type: application/json` и `X-API-Key`;
- делает retry с backoff.

Что переносим:

- протокол отправки;
- retry/backoff;
- поддержку списка URL;
- поддержку зашифрованных значений, если это понадобится в новом конфиге;
- semantics sender settings.

Что меняем:

- вместо обхода файлов реализуем `outbox`;
- отправляем не файлы, а записи событий/снимков из БД.

Куда переносим:

- `internal/delivery`
- `internal/storage/sqlite`
- `internal/domain/fiscal`

### 10. Telegram-уведомления

Источник:

- `service/connectors.py`

Что делает сейчас:

- расшифровывает bot token/chat id при необходимости;
- отправляет уведомления в Telegram;
- используется в сценариях validation.

Что переносим:

- только как дополнительный необязательный канал;
- после переноса ядра collectors/validation/delivery.

Куда переносим:

- `internal/notify/telegram`

### 11. Самообновление

Источник Python:

- `ftp-updater/ftpupdater.py`
- `ftp-updater/connectors.py`
- `ftp-updater/configs.py`
- `ftp-updater/updater.json`

Источник Go:

- `internal/updater/updater.go`
- `internal/workflows/self_update.go`

Текущее решение для переноса:

- базовый self-update остается из `goSSSagent`;
- Python `ftp-updater` не переносим как подсистему;
- при необходимости берем только требования:
  backup/replace/restart, минимальная проверка целостности, логирование результата.

Что переносим из Python только как заметку:

- идея аккуратной замены бинарника через helper;
- rollback-подобное мышление;
- запуск startup/completion hooks только если позже это действительно потребуется.

Что не переносим:

- FTP/HTTP manifest updater;
- отдельный `_temp` mini-project;
- отключаемую подпись через `signature_check_disable_key`;
- второй отдельный жизненный цикл updater-приложения.

Куда переносим:

- доработка существующих:
  `internal/updater`
  `internal/workflows/self_update.go`

### 12. Логирование

Источник:

- `service/logger.py`

Что делает сейчас:

- настраивает ротацию логов;
- пишет сервисные и KKT-логи;
- учитывает log level и срок хранения.

Что переносим:

- production logging;
- ротацию;
- разделение минимум на runtime / collector / updater при необходимости.

Куда переносим:

- `internal/logging` или унифицированный logging bootstrap внутри `cmd/etalon-agent`

### 13. Remote access / WebSocket shell

Источник:

- `ra/cmdroute.py`
- `ra/cmdrun.py`
- `ra/setpass.py`

Что делает сейчас:

- открывает WebSocket-соединение;
- поддерживает admin sessions;
- запускает `cmd.exe`;
- иногда запускает его в пользовательской сессии с admin token;
- иначе запускает от `SYSTEM`;
- поддерживает temp password / password update.

Решение по плану:

- в первый production-перенос **не входит**;
- не переносится по умолчанию;
- может быть рассмотрен отдельным проектом или отдельной phase после завершения основного ядра.

Причина:

- высокий риск;
- не относится к критичному сбору и отправке данных;
- не нужен для достижения основной цели миграции.

## Поля полезной нагрузки, которые нельзя потерять

### Payload KKT Atol

Источник:

- `getdata/atol/atol.py`

Поля:

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
- `hostname`
- `url_rms`
- `teamviewer_id`
- `anydesk_id`
- `rustdesk_id`
- `litemanager_id`
- `current_time`
- `v_time`
- `vc`
- `uuid`

### Payload KKT Mitsu

Источник:

- `getdata/mitsu.py`

Поля:

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
- `hostname`
- `url_rms`
- `teamviewer_id`
- `anydesk_id`
- `rustdesk_id`
- `litemanager_id`
- `current_time`
- `v_time`
- `vc`
- `uuid`

Примечание:

- в Python для Mitsu `licenses` заполняется `"None"`;
- это поведение нужно сохранить, если отдельного источника лицензий для Mitsu нет.

### Fallback payload без KKT

Источник:

- `getdata/atol/atol.py`

Поля:

- `hostname`
- `url_rms`
- `teamviewer_id`
- `anydesk_id`
- `rustdesk_id`
- `litemanager_id`
- `current_time`
- `vc`
- `uuid`

### Корреляция KKT -> FN

Источник:

- `service/sys_manager.py`

Поля:

- `serialNumber`
- `fn_serial`
- `v_time`

Это должно стать отдельной persisted сущностью в новом агенте.

## Целевая структура внутри goSSSagent

Рекомендуемая целевая карта пакетов:

- `internal/config`
- `internal/storage/sqlite`
- `internal/domain/fiscal`
- `internal/domain/configmodel`
- `internal/hostinfo`
- `internal/collectors/atol`
- `internal/collectors/mitsu`
- `internal/collectors/shtrih`
- `internal/validation`
- `internal/delivery`
- `internal/notify/telegram`
- `internal/runtime`
- `internal/updater`
- `internal/system`

Возможные дополнительные подпакеты:

- `internal/system/process`
- `internal/system/netutil`
- `internal/system/filever`
- `internal/hostinfo/remoteids`
- `internal/hostinfo/iiko`

## Пошаговый план реализации

## Phase 0. Актуализация контракта и границ

Цель:

- закрепить, что именно переносится;
- убрать двусмысленности перед кодом.

Задачи:

1. Зафиксировать `golden payload` для Atol, Mitsu и fallback-host в виде отдельного документа или DTO-спеки.
2. Зафиксировать все поля конфигурации, которые продолжают существовать в новом агенте.
3. Зафиксировать, что `nssm` отвечает за lifecycle службы вне агента.
4. Зафиксировать, что `AnyDeskID` собирается через `anydesk.exe`.
5. Зафиксировать, что updater берется из текущего Go-агента.
6. Зафиксировать, что `ra/*` не входит в mandatory scope первой production-версии.
7. Проверить и синхронизировать README/внутренние заметки `goSSSagent`, если там есть противоречия по данным из iiko/cash-server/Zabbix.

Результат фазы:

- утвержденный scope;
- утвержденный контракт данных;
- отсутствие спорных трактовок перед реализацией.

## Phase 1. Конфиг, доменная модель и хранилище

Цель:

- заложить целевую архитектуру хранения и конфигурирования.

Задачи:

1. Расширить `internal/config` под конфиг нового агента.
2. Добавить конфиг-структуры для:
   collectors,
   validation,
   delivery,
   telegram,
   shtrih helper,
   updater policy.
3. Ввести SQLite-хранилище.
4. Создать таблицы:
   `device_snapshots`,
   `fiscal_links`,
   `host_snapshots`,
   `outbox`,
   `validation_state`,
   `agent_state`.
5. Вынести registry/DPAPI только для identity/tokens.
6. Подготовить внутренние модели DTO для payload и snapshot.

Результат фазы:

- агент может хранить рабочее состояние без `date/*.json`;
- доменная модель готова к collectors.

## Phase 2. HostInfo и Remote IDs

Цель:

- добиться parity по сбору служебной информации хоста.

Задачи:

1. Реализовать hostname и базовые системные атрибуты.
2. Реализовать поиск AppData активного пользователя.
3. Реализовать fallback-поиск пользователя по `C:\Users`.
4. Реализовать чтение `serverUrl` из iiko/syrve cashserver config.
5. Реализовать `TeamViewerID` из реестра.
6. Реализовать `LiteManagerID` из реестра.
7. Реализовать `RustDeskID` через `rustdesk.exe --get-id`.
8. Реализовать `AnyDeskID` через `anydesk.exe`.
9. Покрыть все это unit/integration-тестами там, где возможно.

Результат фазы:

- новый агент умеет собрать весь host/remote metadata, нужный payload.

## Phase 3. Mitsu collector

Цель:

- перенести Mitsu end-to-end.

Задачи:

1. Реализовать транспорт COM.
2. Реализовать транспорт TCP.
3. Реализовать LRC и пакетный обмен.
4. Реализовать автодетект COM.
5. Реализовать сетевой поиск Mitsu.
6. Реализовать XML/tag parsing.
7. Реализовать snapshot payload Mitsu.
8. Реализовать обновление `fiscal_links`.
9. Реализовать persisted state найденных устройств.

Результат фазы:

- parity с Python Mitsu collector.

## Phase 4. Atol collector

Цель:

- перенести Atol end-to-end.

Задачи:

1. Определить стратегию интеграции с драйвером Atol.
2. Реализовать mode selection USB/COM/TCP.
3. Реализовать автодетект virtual COM.
4. Реализовать чтение registration/FN/status/license данных.
5. Реализовать payload Atol.
6. Реализовать fallback payload без KKT.
7. Реализовать обновление `fiscal_links`.
8. Реализовать чтение driver version.

Критический риск фазы:

- способ вызова `fptr10.dll` и совместимость с Go.

Результат фазы:

- parity с Python Atol collector.

## Phase 5. Shtrih helper integration

Цель:

- сохранить существующую схему с внешним helper, но встроить ее в новый runtime.

Задачи:

1. Реализовать запуск внешнего `shtrihscanner.exe`.
2. Реализовать ожидание завершения.
3. Реализовать импорт результатов helper-а в новое хранилище.
4. Реализовать обновление `fiscal_links`.

Результат фазы:

- Shtrih продолжает поддерживаться без переписывания scanner-а.

## Phase 6. Validation engine

Цель:

- перенести критичную бизнес-логику контроля ФР/ФН.

Задачи:

1. Перенести semantics validation config.
2. Реализовать age-check по `v_time`.
3. Реализовать поиск и фильтрацию логов.
4. Реализовать паттерны Atol и Mitsu.
5. Реализовать обновление `v_time` при успешном совпадении.
6. Реализовать сценарий несовпадения ФР/ФН.
7. Реализовать cleanup записей при длительном отсутствии логов.
8. Реализовать reboot action/workflow.
9. Реализовать notifications hooks.

Результат фазы:

- поведение validation parity с `POSRelayd`.

## Phase 7. Delivery и outbox

Цель:

- сделать надежную доставку данных без file-based очереди.

Задачи:

1. Спроектировать outbox-модель.
2. Реализовать формирование отправляемых событий из snapshot-ов.
3. Реализовать sender на список URL.
4. Реализовать `X-API-Key`.
5. Реализовать retry/backoff.
6. Реализовать delivery status и dedup.
7. Реализовать метрики/логи по доставке.

Результат фазы:

- функционально sender эквивалентен Python, но архитектурно надежнее.

## Phase 8. Production hardening

Цель:

- довести агент до production-уровня.

Задачи:

1. Добавить полноценное логирование и ротацию.
2. Проверить graceful shutdown без `os._exit`.
3. Убедиться, что запуск под `nssm` не требует доп. кода службы.
4. Доработать текущий Go-updater по реальным требованиям.
5. Добавить health logging и диагностику collectors.
6. Добавить smoke-tests и integration-tests.
7. Подготовить инструкции развертывания через `nssm`.
8. Подготовить конфиг-пример для production.

Результат фазы:

- production-ready агент.

## Что переносить нельзя 1:1

- `service.sys_manager.ResourceManagement` как единый класс-комбайн;
- файловое состояние `date/*.json`;
- `_resources/uuid`;
- updater как отдельный Python mini-project;
- raw WebSocket-shell в production-ядро без отдельного решения;
- управление службой внутри агента;
- `taskkill`, `tasklist`, `os._exit` как обычный механизм оркестрации.

## Минимальные acceptance criteria production-версии

Production-версия считается достигнутой, если:

1. Агент запускается как обычный процесс под `nssm`.
2. Агент регистрируется и работает на текущем Go-runtime.
3. Собираются payload Atol и Mitsu с сохранением обязательных полей.
4. Работает host/remote metadata сбор, включая `AnyDeskID` через `anydesk.exe`.
5. Работает integration с `shtrihscanner.exe`.
6. Работает валидация ФР/ФН по логам.
7. Работает надежная доставка payload через outbox.
8. Работает текущий Go self-update.
9. Есть логи, достаточные для production-диагностики.
10. Нет зависимости от Python runtime и Python-конфиговой архитектуры.

## Приоритеты разработки

Приоритет `P0`:

- config
- storage
- hostinfo
- Mitsu
- Atol
- fiscal_links
- validation
- delivery

Приоритет `P1`:

- Shtrih helper integration
- Telegram notifications
- updater hardening

Приоритет `P2`:

- все, что связано с remote shell / RA

## Риски

### Риск 1. Интеграция Atol

Главный технический риск всего переноса.
Нужно отдельно выбрать стратегию вызова драйвера из Go.

### Риск 2. Железо и реальные стенды

Без доступа к реальным KKT production-паритет по collectors и validation подтвердить нельзя.

### Риск 3. Серверный контракт

Нужно заранее подтвердить, какой именно API будет целевым для передачи fiscal payload.

### Риск 4. Документация goSSSagent

Во внутренних заметках проекта уже есть расхождения по источникам данных и назначению агента.
Это нужно поправить до активной реализации, чтобы не тащить неверные предположения в код.

## Рекомендуемый порядок работы в следующей сессии

1. Перенести этот документ в репозиторий `goSSSagent`.
2. Создать рядом технический backlog по phase-ам.
3. Начать с `Phase 0` и `Phase 1`.
4. После этого идти в порядке:
   `hostinfo -> Mitsu -> Atol -> Shtrih -> validation -> delivery -> hardening`.

## Итоговый вердикт

Перенос из `POSRelayd` в `goSSSagent` реалистичен и технически оправдан.
Переписывать нужно не все подряд, а только те части, которые несут бизнес-логику данных.

Новый агент должен унаследовать:

- collectors,
- host metadata,
- fiscal correlation,
- validation,
- delivery semantics.

Новый агент не должен унаследовать:

- монолитную Python-оркестрацию,
- file-based state,
- отдельный Python updater,
- встроенный service management,
- raw remote shell как обязательную часть production-ядра.
