# Etalon-Server API Reference

## 1. Общие сведения

* **Base URL:** `/api`
* **Protocol:** HTTP/1.1 (REST) & Server-Sent Events (SSE)
* **Content-Type:** `application/json`
* **Date Format:** ISO 8601 (`2023-10-27T10:00:00Z`)

### 1.1. Аутентификация

Все запросы к защищённым эндпоинтам требуют заголовок:
`Authorization: Bearer <your_jwt_token>`

### 1.2. Формат ответов (Envelope Pattern)

Все ответы API обёрнуты в единый конверт.

**Успешный ответ (200 OK, 201 Created, 202 Accepted):**
```json
{
  "status": "success",
  "data": { ... },
  "meta": {
    "total": 100,
    "limit": 50,
    "offset": 0,
    "has_next": true,
    "has_prev": false
  }
}
```

**Ответ с ошибкой (4xx, 5xx):**
```json
{
  "status": "error",
  "error": {
    "error": "Описание ошибки для пользователя или разработчика"
  }
}
```

---

## 2. Аутентификация

### 2.1. Вход в систему

`POST /auth/login`

**Request Body:**
```json
{
  "username": "admin",
  "password": "secret_password"
}
```

**Response:**
```json
{
  "status": "success",
  "data": {
    "access_token": "eyJhbGciOiJIUzI1NiIsIn...",
    "user": {
      "id": 1,
      "username": "admin",
      "fullName": "Главный Администратор",
      "roles": ["admin"]
    }
  }
}
```

---

## 3. Глобальный поиск (Search)

Основной инструмент навигации. Ищет Компании, Серверы, Рабочие станции и ФР по введённой строке. Результаты сгруппированы по Компаниям-владельцам.

### 3.1. Поиск сущностей

`GET /search`

**Parameters:**
* `term` (string, required): Поисковая фраза (IP, Serial, Name, Address, INN).
* `limit` (int, optional): Лимит записей (default: 50).

**Response:**
```json
{
  "status": "success",
  "data": {
    "search_results": [
      {
        "owner": {
          "uuid": "internal-uuid-company",
          "external_uuid": "sd-uuid-company",
          "name": "ООО Ромашка",
          "address": "г. Москва, ул. Ленина 1",
          "active_contract": true,
          "parent_info": { "uuid": "parent-uuid", "name": "Холдинг Групп" }
        },
        "found_entities": [
          {
            "entity_type": "Server",
            "data": {
              "uuid": "srv-uuid",
              "device_name": "SRV-01",
              "ip": "192.168.1.10:8080",
              "operational_status": "active",
              "health_status": "ok"
            }
          },
          {
            "entity_type": "FiscalRegister",
            "data": {
              "uuid": "fr-uuid",
              "rn_kkt": "1234567890123456",
              "serial_number": "001067000000",
              "health_status": "attention_required"
            }
          }
        ]
      }
    ]
  }
}
```

---

## 4. Задачи сверки (Tasks)

Рабочее место оператора. Здесь отображаются конфликты данных и новые устройства, требующие решения.

### 4.1. Получить список задач

`GET /tasks`

**Parameters:**
* `status` (string, optional): `new`, `resolved`, `rejected`, `pending_sd_action`, `sd_error`.
* `limit`, `offset` (pagination).

**Response:**
```json
{
  "status": "success",
  "data": [
    {
      "id": 101,
      "task_type": "add_equipment",
      "entity_type": "FiscalRegister",
      "status": "new",
      "created_at": "2023-10-27T10:00:00Z",
      "details": {
        "agent_data": { ... },
        "etalon_owner_id": "...",
        "equipment_data": {
           "rn_kkt": "...",
           "serial_number": "..."
        }
      }
    }
  ]
}
```

### 4.2. Решить задачу (Resolve)

`POST /tasks/{id}/resolve`

Действие зависит от `resolution_payload.action`.

**Request Body (Пример: Подтвердить создание):**
```json
{
  "status": "resolved",
  "comment": "Оборудование добавлено корректно",
  "resolution_payload": {
    "action": "create"
  }
}
```

### 4.3. Создать сущность в ServiceDesk (на основе задачи)

`POST /tasks/{id}/create-entity-in-sd`

Используется для задач типа `add_equipment`, когда оператор подтвердил данные и хочет отправить их в Naumen.

**Request Body:**
```json
{
  "entity_type": "FiscalRegister"
}
```

**Response:** `202 Accepted` (операция выполняется асинхронно, статус задачи изменится на `pending_sd_action`).

### 4.4. Получить группы дубликатов

`GET /duplicates`

Возвращает список групп сущностей, у которых совпадают ключевые поля (IP, Serial, TeamViewer ID).

---

## 5. Тикеты (ServiceDesk)

Работа с заявками.

### 5.1. Список заявок

`GET /tickets`

**Parameters:**
* `company_id` (optional): Фильтр по компании.
* `asset_id` (optional): Фильтр по оборудованию.
* `status` (optional): `registered,inprogress,closed`.
* `search` (optional): Поиск по теме или номеру.
* `limit`, `offset`.

**Response:**
```json
{
  "status": "success",
  "data": [
    {
      "id": "uuid",
      "number": 12345,
      "service_desk_uuid": "serviceCall$...",
      "status": "registered",
      "subject": "Не работает принтер",
      "last_activity": "2023-10-27T12:00:00Z",
      "company_id": "..."
    }
  ],
  "meta": { "total": 50, ... }
}
```

### 5.2. Детали заявки

`GET /tickets/{id}`

Возвращает полную информацию: описание (HTML), историю изменений, вложения и комментарии.

### 5.3. Создать заявку (Внутреннюю)

`POST /tickets`

**Request Body:**
```json
{
  "subject": "Проблема с кассой",
  "description": "Описание проблемы...",
  "company_id": "uuid-company",
  "priority": "high",
  "type": "incident",
  "asset_id": "uuid-fiscal",
  "asset_type": "FiscalRegister"
}
```

### 5.4. Сменить статус

`PATCH /tickets/{id}/status`

**Request Body:**
```json
{
  "status": "inprogress",
  "comment": "Взял в работу"
}
```

### 5.5. Назначить исполнителя

`PATCH /tickets/{id}/assign`

**Request Body:**
```json
{
  "assignee_id": 12
}
```

---

## 6. Инфраструктура (CMDB)

CRUD операции для основных сущностей.

### 6.1. Компании
* `GET /companies/{id}`: Детали компании.
* `GET /companies/{id}/infrastructure`: **Важный эндпоинт**. Возвращает плоский список всего оборудования (Server, Workstation, FiscalRegister), принадлежащего компании.

### 6.2. Серверы
* `POST /servers/{id}/poll`: Принудительный опрос статуса (RMS/Iiko).
* `POST /servers/{id}/license`: Установка лицензии (требуется `unique_id` в теле).
* `POST /servers/{id}/additional_owners`: Добавить совладельца (тело: `{"company_id": "..."}`).

### 6.3. Общие CRUD

Для `servers`, `workstations`, `fiscals` доступны стандартные методы:
* `GET /{entity}/{id}`
* `PUT /{entity}/{id}` (обновление полей)
* `DELETE /{entity}/{id}` (мягкое удаление)

---

## 7. Real-time События (SSE)

Подписка на обновления в реальном времени.

**Endpoint:** `GET /events`

**Типы событий (event):**
* `server.polling.succeeded`: Статус сервера обновился.
* `server.polling.failed`: Сервер недоступен.
* `servicedesk.entity.create.requested`: Задача ушла в обработку.
* `servicedesk.entity.updated`: Пришли новые данные из SD.
* `duplicates.found`: Найдены новые дубликаты.

**Client Implementation (JS):**
```javascript
const evtSource = new EventSource("/api/events?token=" + jwtToken);
evtSource.addEventListener("server.polling.succeeded", (e) => {
    const data = JSON.parse(e.data);
    console.log("Server updated:", data);
});
```

---

## 8. Кандидаты (Candidates)

Работа с кандидатами на подключение к АО.

### 8.1. Список кандидатов

`GET /candidates`

**Parameters:**
* `status` (optional): `NEW`, `IN_REVIEW`, `APPROVED`, `REJECTED`, `ACTIVE` (default).
* `limit`, `offset`.

### 8.2. Карточка кандидата

`GET /candidates/{id}`

Возвращает кандидата с staged-данными по станциям и ФР.

### 8.3. Подтверждение кандидата

`POST /candidates/{id}/approve`

**Request Body:**
```json
{
  "company_id": "uuid-existing-company",
  "company": {
    "title": "Название новой компании",
    "address": "Адрес",
    "additional_name": "Доп. название",
    "parent_id": "uuid-parent-company",
    "contract_mode": "inherit_parent",
    "contract_type": "full"
  },
  "server": {
    "mode": "existing",
    "server_id": "uuid-existing-server",
    "crm_id": "CRM-123",
    "url_rms": "server.domain.ru:8080",
    "unique_id": "unique-identifier",
    "cabinet_link": "https://cabinet.example.com/client/12345",
    "device_name": "SRV-01",
    "description": "Описание сервера"
  },
  "workstations": [
    {
      "staging_id": 1,
      "workstation_uuid": "uuid-ws",
      "name": "КАССА-01"
    }
  ],
  "teamviewer_id": "123456789",
  "litemanager_id": "987654321",
  "anydesk_id": "123456789",
  "comment": "Комментарий оператора"
}
```

**Поля для ручного ввода remote IDs (опционально):**
* `teamviewer_id` (string, optional) — ID TeamViewer для идентификации рабочей станции
* `litemanager_id` (string, optional) — ID LiteManager для идентификации рабочей станции
* `anydesk_id` (string, optional) — ID AnyDesk для идентификации рабочей станции

Эти поля используются когда агент не собрал remote IDs (программы удалённого доступа не установлены или не обнаружены). Приоритет: ручной ввод > значения из staging.

**Response:**
```json
{
  "status": "success",
  "data": {
    "id": 1,
    "status": "APPROVED",
    "approved_company_id": "uuid-company",
    "approved_server_id": "uuid-server"
  }
}
```

---

## 9. Network Candidates

### 9.1. Список network-кандидатов

`GET /network-candidates`

**Parameters:**
* `status` (optional): `NEW`, `IN_REVIEW`, `APPROVED`, `REJECTED`.
* `limit`, `offset`.

### 9.2. Карточка network-кандидата

`GET /network-candidates/{id}`

Ответ содержит candidate и groups (1 WS + 0..N FR).

### 9.3. Подтверждение network-кандидата

`POST /network-candidates/{id}/approve`

### 9.4. Перенос группы в новый кандидат

`POST /network-candidates/{id}/groups/{groupID}/remove`
