# Ревизия текущего этапа и операторский флоу

## Назначение

Документ фиксирует фактическое состояние `goSSSagent` после закрытия двух обязательных контуров:

- meaningful heartbeat вместо шумного потока observation-событий на каждом heartbeat;
- полный server-side и UI-контур назначения `adapter_manifests` оператором.

Цель документа:

- зафиксировать, что именно уже работает end-to-end;
- описать текущую архитектуру heartbeat/observation;
- закрепить операторский сценарий назначения профиля машины и адаптеров;
- честно обозначить, что еще остается до следующего этапа с отдельным `saga-runner`.

## Фактический статус этапа

### Phase 0. Core-agent и внешние адаптеры

Статус: закрыта по inventory-first контуру.

Что уже есть:

- bootstrap-регистрация, выдача и refresh токенов;
- heartbeat с `inventory` и `adapter_statuses`;
- adapter manager со скачиванием, проверкой `sha256` и локальными descriptor;
- UI для списка агентов, диагностики heartbeat и подтверждения bootstrap-регистрации;
- отдельные бинарники `fiscal-atol`, `fiscal-mitsu`, `fiscal-shtrih` с командами `describe`, `health`, `run`;
- server-side хранение итогового профиля машины и `adapter_manifests`;
- выдача актуальных `adapter_manifests` прямо в heartbeat-ответе;
- операторский UI для подтверждения или корректировки профиля машины.

Что еще не закрыто:

- `core-agent` пока не запускает адаптеры как subprocess;
- нет автоматического удаления/замены локальных адаптеров по жизненному циклу процесса;
- нет отдельного `POS`-адаптера как опубликованного server-side шаблона.

### Phase 1. Конфиг и управление состоянием

Статус: закрыта по серверной механике профиля машины.

Что уже есть:

- конфиг `core-agent`;
- локальное хранение identity и токенов;
- persisted state для connectivity;
- локальный каталог адаптеров с descriptor;
- server-side редактор профиля агента через diagnostics API;
- сохранение `machine_profile` и `adapter_manifests` в `agent.Config`;
- сохранение meaningful heartbeat fingerprint и последнего meaningful state в модели агента.

Что еще не закрыто:

- нет отдельного SQLite-хранилища под snapshots/outbox/fiscal_links;
- нет server-side справочника опубликованных бинарников с полным жизненным циклом релизов.

### Phase 2. HostInfo и кассовое ПО

Статус: хороший рабочий вертикальный срез.

Что уже есть:

- поиск `AppData`;
- чтение `serverUrl` из `iiko/syrve cashserver`;
- `TeamViewerID`, `LiteManagerID`, `RustDeskID`, `AnyDeskID`;
- `inventory.known_components` как источник признаков `iiko/syrve`;
- server-side рекомендации профиля машины по `host_info`, `known_components`, remote IDs и `cash_server_url`.

Что еще не закрыто:

- нет отдельного `POS`-адаптера для `iiko/syrve`;
- нет сбора логов, конфигов и диагностики кассового ПО как отдельного домена.

### Fiscal adapters

Статус: готовы к предметным полевым smoke-тестам как отдельные бинарники.

Что уже есть:

- `fiscal-atol`: `health/run`, работа через `fptr10.dll`, выбор ветки драйвера;
- `fiscal-mitsu`: `health/run`, COM/TCP, Mitsu-протокол;
- `fiscal-shtrih`: `health/run`, COM-драйвер `AddIn.DrvFR`;
- server-side рекомендации `adapter_manifests` по inventory и правилам `signature_key`.

Что еще не закрыто:

- автоматический запуск из `core-agent`;
- унифицированная оркестрация `health/run` на стороне `core-agent`;
- fallback-обогащение host metadata внутри предметных адаптеров.

### Saga/task execution

Статус: не готово как отдельная подсистема.

Что уже есть:

- базовый `saga.Runner`;
- один рабочий workflow `self_update`.

Что еще не закрыто:

- нет отдельного `saga-runner` адаптера;
- нет безопасного task-контракта для локальных действий;
- нет operator flow для согласования и запуска task-адаптеров.

## Meaningful heartbeat и observation flow

### Что было проблемой

Раньше сервер на каждом heartbeat публиковал `agent.observation.requested`.
Из-за этого:

- лента наблюдений засорялась однотипными heartbeat-событиями;
- журналы и feed не показывали реальную предметную динамику;
- `payload_hash`, рассчитанный от полного JSON, реагировал на летучие поля вроде `current_time` и `v_time`.

### Что сделано

Сервер теперь разделяет:

- heartbeat как канал обновления liveness и последних snapshot-данных;
- observation как событие только для meaningful change.

На каждом heartbeat сервер всегда обновляет:

- `last_heartbeat`;
- `last_observed_at`;
- `latest_inventory_snapshot`;
- `latest_adapter_statuses`.

Дополнительно для агента сохраняются:

- `last_meaningful_heartbeat_at`;
- `last_meaningful_observed_at`;
- `last_meaningful_heartbeat_fingerprint`;
- `last_meaningful_heartbeat_state`.

### Как определяется meaningful change

Сервер строит нормализованный fingerprint по состоянию, которое действительно влияет на доменную обработку и рекомендации адаптеров.

В fingerprint входят:

- host info;
- `known_components`;
- remote IDs;
- `url_rms`, `crm_id`, другие существенные server-side идентификаторы;
- фискальные данные;
- `signature_key` и `classification` для COM-устройств;
- существенные `adapter_statuses`.

Из fingerprint исключаются летучие поля heartbeat, например:

- `current_time`;
- `v_time`;
- `collected_at`;
- `updated_at`;
- прочий heartbeat-only шум, который не меняет доменный смысл состояния.

### Поведение после изменений

- первый heartbeat после регистрации всегда считается meaningful;
- heartbeat без meaningful change не создает observation-сущность;
- heartbeat без meaningful change не публикует `agent.observation.requested`;
- heartbeat без meaningful change не засоряет feed и логирование как доменное событие;
- heartbeat с meaningful change публикует observation и попадает в обычный доменный контур;
- в логах явно различаются `heartbeat_result=noop` и `heartbeat_result=meaningful_change`.

Это означает, что UI продолжает видеть свежие `latest_inventory_snapshot` и `latest_adapter_statuses`, но observation-лента теперь показывает только реально значимые изменения.

## Профиль машины и рекомендации адаптеров

### Что теперь есть на сервере

На сервере реализована модель operator flow поверх inventory:

- рекомендованный профиль машины;
- причины рекомендации;
- рекомендованные `adapter_manifests`;
- подтвержденный профиль и подтвержденный набор manifests;
- эффективный набор manifests, который реально попадет в heartbeat-ответ;
- список кандидатов для правил `COM signature_key`.

Источники рекомендаций:

- `inventory.host_info`;
- `inventory.known_components`;
- `cash_server_url`;
- признаки `iiko/syrve`;
- `COM signature_key/classification`;
- server-side правила по сигнатурам;
- уже известные server-side признаки типов устройств.

### Профили машины

Сервер поддерживает как минимум следующие профили:

- `unknown`;
- `service-workstation`;
- `pos-workstation`;
- `fiscal-workstation`;
- `hybrid-pos-fiscal`.

### Adapter manifests

Серверный каталог рекомендаций уже умеет предлагать минимум:

- `fiscal-atol`;
- `fiscal-mitsu`;
- `fiscal-shtrih`.

Если машина выглядит как `POS`, но отдельный `POS`-адаптер еще не опубликован в серверном каталоге, UI показывает предупреждение. Это осознанное состояние: механика профиля и сохранения конфигурации уже есть, но отдельный бинарник POS-адаптера будет закрываться следующим этапом.

## COM-порты и правила `signature_key`

### Что уже было

`inventory.com_ports` уже содержит:

- `friendly_name`, `description`, `manufacturer`, `class`, `service`, `location`;
- `hardware_ids[]`, `compatible_ids[]`;
- `vendor_id`, `product_id`;
- нормализованную сигнатуру `signature_key`;
- `classification`, если сигнатура уже известна.

### Что добавлено на сервере

Теперь сервер хранит правила по `signature_key`.
Оператор может прямо из diagnostics UI:

- выбрать observed `signature_key`;
- указать `device_type`;
- указать `profile_hint`;
- указать `suggested_adapter`;
- сохранить правило для следующих машин.

После сохранения правила сервер начинает использовать его при расчете рекомендаций профиля и manifests.

### Практический результат

Цепочка теперь выглядит так:

1. Агент присылает `signature_key` и device metadata.
2. UI показывает observed-кандидаты и текущее server-side правило, если оно уже есть.
3. Оператор подтверждает правило.
4. Сервер сохраняет `signature_key -> device_type/profile_hint/adapter`.
5. Следующие машины с той же сигнатурой получают более точную рекомендацию автоматически.

## Единый операторский флоу

### Фактический рабочий сценарий

1. Оператор открывает раздел `Агенты` и видит новый `sssruner`.
2. Если статус регистрации `pending_approval`, оператор подтверждает bootstrap в карточке диагностики.
3. После первого heartbeat UI показывает `inventory`, `adapter_statuses` и блок operator flow.
4. Сервер рассчитывает meaningful heartbeat state и рекомендацию профиля машины.
5. UI показывает:
   - рекомендованный профиль;
   - причины рекомендации;
   - рекомендуемые `adapter_manifests`;
   - предупреждения по отсутствующим server-side шаблонам;
   - observed `signature_key` для COM-устройств.
6. Оператор:
   - подтверждает профиль как есть;
   - либо корректирует профиль;
   - при необходимости вручную правит итоговый набор manifests;
   - при необходимости сохраняет правило по `signature_key`.
7. Сервер сохраняет итоговую конфигурацию в `agent.Config`.
8. Следующий heartbeat отдает агенту актуальные `adapter_manifests`.
9. Следующий heartbeat после применения manifests показывает `adapter_statuses` уже по назначенным адаптерам.
10. В observation/feed попадают только meaningful change, а не каждый heartbeat.

### Что видит оператор в diagnostics UI

В карточке diagnostics оператор теперь видит:

- состояние последнего meaningful heartbeat;
- рекомендованный профиль машины;
- причины рекомендации;
- рекомендуемые manifests;
- сохраненный профиль и сохраненные manifests;
- effective manifests, которые реально уйдут агенту;
- кандидаты `COM signature_key`;
- форму сохранения server-side правила по сигнатуре.

## Готовность к первому боевому тесту

### Что уже можно тестировать вживую

- bootstrap-регистрацию и approval;
- первый meaningful heartbeat после регистрации;
- noop heartbeat без создания observation;
- inventory и `host_info`;
- remote IDs;
- enriched COM inventory;
- сохранение server-side профиля машины;
- выдачу `adapter_manifests` в heartbeat-ответе;
- появление `adapter_statuses` после назначения адаптеров;
- сохранение правил по `signature_key`.

### Что еще нельзя считать production-ready

- subprocess lifecycle адаптеров внутри `core-agent`;
- отдельный POS-адаптер для `iiko/syrve`;
- автоматическое снятие лишних адаптеров по фактическому жизненному циклу локального процесса;
- task execution через отдельный `saga-runner`.

### Итоговый вердикт

К первому боевому тесту готов именно законченный inventory-first/operator-driven контур:

- регистрация;
- meaningful heartbeat;
- server-side рекомендация профиля;
- ручное подтверждение оператором;
- сохранение manifests;
- возврат manifests агенту;
- контроль последующих `adapter_statuses`.

Не готово к следующему этапу автоматизации:

- полный runtime lifecycle адаптеров внутри `core-agent`;
- отдельный `POS`-адаптер;
- отдельный `saga-runner`.

## Минимальный backlog перед `saga-runner`

Приоритет `P0`:

- subprocess lifecycle для адаптеров в `core-agent`;
- published-каталог серверных бинарников и шаблонов доставки;
- отдельный `POS`-адаптер для `iiko/syrve`;
- автоматическое снятие неактуальных адаптеров при смене профиля машины.

Приоритет `P1`:

- расширение правил `signature_key` на банковские терминалы, сканеры, весы;
- health/run orchestration для предметных адаптеров;
- richer-профили машин с типизированными server-side шаблонами.

Приоритет `P2`:

- отдельный `saga-runner` адаптер;
- task-контракт и операторское согласование локальных действий.
