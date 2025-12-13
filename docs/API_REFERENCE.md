# Etalon-Server API Reference


## 1. Общие сведения

*   **Base URL:** `/api`
*   **Protocol:** HTTP/1.1 (REST) & Server-Sent Events (SSE)
*   **Content-Type:** `application/json`
*   **Date Format:** ISO 8601 (`2023-10-27T10:00:00Z`)

### 1.1. Аутентификация
Все запросы к защищенным эндпоинтам требуют заголовок:
`Authorization: Bearer <your_jwt_token>`

### 1.2. Формат ответов (Envelope Pattern)
Все ответы API обернуты в единый конверт.

**Успешный ответ (200 OK, 201 Created, 202 Accepted):**
```json
{
  "status": "success",
  "data": { ... },       // Основные данные (объект или массив)
  "meta": {              // Метаданные (присутствуют при пагинации)
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

Основной инструмент навигации. Ищет Компании, Серверы, Рабочие станции и ФР по введенной строке. Результаты сгруппированы по Компаниям-владельцам.

### 3.1. Поиск сущностей
`GET /search`

**Parameters:**
*   `term` (string, required): Поисковая фраза (IP, Serial, Name, Address, INN).
*   `limit` (int, optional): Лимит записей (default: 50).

**Response:**
```json
{
  "status": "success",
  "data": {
    "search_results": [
      {
        "owner": {
          "uuid": "internal-uuid-company",
          "external_uuid": "sd-uuid-company", // Может быть null
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
              "operational_status": "active", // active, offline, unknown
              "health_status": "ok" // ok, attention_required, locked
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
*   `status` (string, optional): `new`, `resolved`, `rejected`, `pending_sd_action`, `sd_error`.
*   `limit`, `offset` (pagination).

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
        // Структура зависит от task_type.
        // Пример для add_equipment:
        "agent_data": { ... },     // Полные сырые данные от агента
        "etalon_owner_id": "...",  // Предлагаемый владелец
        "equipment_data": {        // Сформированные данные для превью
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

**Request Body (Пример: Сменить владельца):**
```json
{
  "status": "resolved",
  "resolution_payload": {
    "action": "update_owner",
    "new_owner_id": "uuid-of-new-company"
  }
}
```

### 4.3. Создать сущность в ServiceDesk (на основе задачи)
`POST /tasks/{id}/create-entity-in-sd`

Используется для задач типа `add_equipment`, когда оператор подтвердил данные и хочет отправить их в Naumen.

**Request Body:**
```json
{
  "entity_type": "FiscalRegister" // или Server, Workstation
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
*   `company_id` (optional): Фильтр по компании.
*   `asset_id` (optional): Фильтр по оборудованию.
*   `status` (optional): `registered,inprogress,closed`.
*   `search` (optional): Поиск по теме или номеру.
*   `limit`, `offset`.

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
  "priority": "high", // low, medium, high, critical
  "type": "incident",
  "asset_id": "uuid-fiscal", // Опционально
  "asset_type": "FiscalRegister"
}
```

### 5.4. Сменить статус
`PATCH /tickets/{id}/status`

**Request Body:**
```json
{
  "status": "inprogress",
  "comment": "Взял в работу" // Опционально
}
```

### 5.5. Назначить исполнителя
`PATCH /tickets/{id}/assign`

**Request Body:**
```json
{
  "assignee_id": 12 // ID пользователя системы (User.ID), или null для снятия
}
```

---

## 6. Инфраструктура (CMDB)

CRUD операции для основных сущностей.

### 6.1. Компании
*   `GET /companies/{id}`: Детали компании.
*   `GET /companies/{id}/infrastructure`: **Важный эндпоинт**. Возвращает плоский список всего оборудования (Server, Workstation, FiscalRegister), принадлежащего компании. Используется для построения дерева на фронтенде.

### 6.2. Серверы
*   `POST /servers/{id}/poll`: Принудительный опрос статуса (RMS/Iiko).
*   `POST /servers/{id}/license`: Установка лицензии (требуется `unique_id` в теле).
*   `POST /servers/{id}/additional_owners`: Добавить совладельца (тело: `{"company_id": "..."}`).

### 6.3. Общие CRUD
Для `servers`, `workstations`, `fiscals` доступны стандартные методы:
*   `GET /{entity}/{id}`
*   `PUT /{entity}/{id}` (обновление полей)
*   `DELETE /{entity}/{id}` (мягкое удаление)

---

## 7. Real-time События (SSE)

Подписка на обновления в реальном времени.

**Endpoint:** `GET /events`

**Типы событий (event):**
*   `server.polling.succeeded`: Статус сервера обновился (payload: `{ serverUUID, newStatus, ... }`).
*   `server.polling.failed`: Сервер недоступен.
*   `servicedesk.entity.create.requested`: Задача ушла в обработку (статус задачи изменился).
*   `servicedesk.entity.updated`: Пришли новые данные из SD.
*   `duplicates.found`: Найдены новые дубликаты.

**Client Implementation (JS):**
```javascript
const evtSource = new EventSource("/api/events?token=" + jwtToken);
evtSource.addEventListener("server.polling.succeeded", (e) => {
    const data = JSON.parse(e.data);
    console.log("Server updated:", data);
});
```