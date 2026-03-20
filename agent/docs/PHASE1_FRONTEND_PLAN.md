# План Phase 1 для фронтенда goSSSagent

## Назначение

Этот документ нужен команде фронтенда, чтобы закрыть UI-часть `Phase 1` вокруг нового `core-agent`.
На этой фазе фронт не должен строить экраны collectors и не должен ждать данных по `TeamViewer/AnyDesk/RustDesk`.
Его задача проще и важнее: показать, что агент зарегистрировался, какой payload он прислал, чем закончилась последняя попытка регистрации и какой последний heartbeat snapshot сервер получил.

## Что уже есть в UI

- Есть страница списка агентов:
  - `front-ui/src/pages/AgentsPage.tsx`
- Есть страница ленты наблюдений:
  - `front-ui/src/pages/AgentObservationsPage.tsx`
- Есть модалка просмотра сырого payload наблюдения:
  - `front-ui/src/components/agents/AgentObservationRawModal.tsx`

## Что уже есть на backend к началу Phase 1 frontend

- Защищённый список диагностики агентов:
  - `GET /api/agent-diagnostics`
- Защищённые детали конкретного агента:
  - `GET /api/agent-diagnostics/{uuid}`
- Сервер теперь хранит:
  - `last_registration_at`
  - `last_registration_status`
  - `last_registration_error`
  - `machine_fingerprint`
  - `registration_system_info`
  - `registration_payload`
  - `latest_inventory_snapshot`
  - `latest_adapter_statuses`
- Сервер хранит историю попыток регистрации:
  - `agent_registration_attempts`

## Главная цель фронтенда Phase 1

Сделать в UI отдельный, понятный для техподдержки контур ответа на четыре вопроса:

1. Зарегистрировался ли агент вообще?
2. Когда была последняя попытка регистрации и чем она закончилась?
3. Что именно агент отправил на регистрацию и что потом отправил в heartbeat?
4. Есть ли у агента уже полезный inventory snapshot и статусы адаптеров?

## Что не нужно ждать в этой фазе

- Не нужно ждать реализации collectors `Atol/Mitsu/Shtrih`.
- Не нужно ждать нормализации inventory по отдельным таблицам.
- Не нужно ждать полноценного UI по удалёнкам.
- Данные `TeamViewer/AnyDesk/RustDesk` важны для дальнейшего сопоставления оборудования, но не являются блокером для подтверждения самого факта регистрации агента.

## Обязательные UI-сценарии

### 1. Список агентов с состоянием регистрации

Нужно либо расширить текущую `AgentsPage`, либо сделать рядом отдельный режим списка.

В таблице должны быть видны:

- `uuid`
- `hostname`
- `type`
- `status`
- `last_registration_at`
- `last_registration_status`
- `last_registration_error`
- `last_heartbeat`
- `last_observed_at`
- индикатор наличия `inventory`
- индикатор наличия `adapter_statuses`

Ожидаемое поведение:

- `success` отображается как успешная регистрация;
- `unauthorized` отображается как ошибка bootstrap-авторизации;
- `invalid_request` отображается как ошибка payload/валидации;
- `failed` отображается как серверная ошибка регистрации;
- если heartbeat свежий, но registration stale или failed, это видно отдельно;
- если registration success, но heartbeat ещё не пришёл, это тоже видно отдельно.

### 2. Карточка агента

Нужна отдельная страница деталей агента.

На карточке должны быть блоки:

- Общая сводка
- Последняя регистрация
- Последний heartbeat snapshot
- История попыток регистрации

В сводке показать:

- `uuid`
- `hostname`
- `type`
- `status`
- `owner_id`
- `workstation_id`
- `machine_fingerprint`
- `last_registration_at`
- `last_registration_status`
- `last_registration_error`
- `last_heartbeat`
- `last_observed_at`

### 3. Блок "Последняя регистрация"

В этом блоке показать:

- время последней регистрации;
- статус;
- текст ошибки;
- `machine_fingerprint`;
- `system_info`;
- полный `registration_payload`.

Требования к UX:

- `system_info` и `registration_payload` показывать в readable JSON viewer;
- длинные payload не обрезать навсегда, а сворачивать/разворачивать;
- должна быть кнопка копирования JSON;
- визуально отделить серверные ошибки от ошибок авторизации и ошибок формата.

### 4. Блок "Последний heartbeat snapshot"

Нужно показать:

- `latest_inventory_snapshot`
- `latest_adapter_statuses`

Минимальное отображение inventory:

- `hostname`
- `os`
- `arch`
- `collected_at`
- сетевые интерфейсы
- COM-порты
- установленное ПО
- known components

Минимальное отображение adapter statuses:

- `adapter_id`
- `adapter_type`
- `version`
- `status`
- `target_os`
- `target_arch`
- `last_error`
- `updated_at`

### 5. Блок "История регистраций"

Нужно вывести последние попытки регистрации таблицей.

Столбцы:

- `created_at`
- `status`
- `error_text`
- `remote_addr`
- `machine_fingerprint`
- действие "открыть payload"

Дополнительно:

- сортировка по времени по убыванию;
- цветовое кодирование статуса;
- быстрое сравнение последней и предыдущей попытки регистрации.

## Детальная разбивка задач для frontend-команды

### FE-1. Типы и API-клиент

- Добавить типы для:
  - списка диагностики агентов;
  - деталей агента;
  - истории регистраций.
- Добавить API-клиент:
  - `GET /api/agent-diagnostics`
  - `GET /api/agent-diagnostics/{uuid}`

### FE-2. Обновление страницы списка агентов

- Решить, расширяем текущую `AgentsPage` или делаем отдельный route.
- Добавить колонки по регистрации.
- Добавить фильтры:
  - по `last_registration_status`;
  - по строке поиска;
  - по "есть/нет heartbeat";
  - по "есть/нет inventory".

### FE-3. Новая страница деталей агента

- Сделать route вида:
  - `/agent-diagnostics/:uuid`
- Собрать layout из четырёх секций:
  - summary
  - registration
  - heartbeat snapshot
  - registration history

### FE-4. JSON viewer и копирование

- Переиспользовать существующий паттерн сырого payload из наблюдений, но вынести в общий компонент.
- Поддержать:
  - pretty print;
  - copy button;
  - collapse/expand.

### FE-5. Состояния загрузки и ошибок

- Показать skeleton/loading states для списка и карточки.
- Отдельно отрисовать состояния:
  - агент не найден;
  - агент есть, но registration payload отсутствует;
  - агент зарегистрирован, но heartbeat snapshot ещё не пришёл;
  - последняя регистрация failed/unauthorized.

### FE-6. Навигация

- Добавить переход:
  - из списка агентов в детали диагностики;
  - из карточки оборудования в карточку агентской диагностики, если есть `agent_uuid`;
  - из ленты наблюдений в карточку диагностики агента.

### FE-7. Приёмочное тестирование

- Проверить сценарии:
  - новая успешная регистрация;
  - повторная регистрация того же `agent_uuid`;
  - `401` без `Authorization`;
  - `401` с неверным bootstrap key;
  - успешная регистрация без heartbeat;
  - успешная регистрация с inventory и adapter statuses.

## UX-решение по "подтверждению регистрации"

На Phase 1 фронт не должен вводить отдельную ручную кнопку "Подтвердить регистрацию", если под этим понимается бизнес-подтверждение оператором.

На этой фазе достаточно серверного и UI-подтверждения в двух формах:

- сервер сохранил успешную последнюю регистрацию;
- UI явно показывает `last_registration_status = success` и данные последнего payload.

Если позже понадобится ручной операторский workflow "подтверждено оператором", это уже отдельная фаза и отдельное поле модели, а не часть текущей bootstrap-регистрации.

## Критерий готовности Phase 1 для фронтенда

Phase 1 можно считать закрытым по фронту, если:

- оператор видит список агентов со статусом регистрации;
- оператор может открыть конкретного агента и увидеть последний registration payload;
- оператор видит последний inventory snapshot и adapter statuses;
- оператор видит историю последних попыток регистрации;
- оператор без чтения логов понимает, почему регистрация не прошла.

## Что не входит в эту фазу

- UI collectors и device-level collectors;
- full CMDB-редактор для inventory;
- ручные workflow подтверждения владения оборудования;
- нормализация и визуализация каждого software/network элемента как отдельной сущности.
