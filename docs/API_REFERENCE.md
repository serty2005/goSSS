# Etalon-Server API Reference


## 1. РћР±С‰РёРµ СЃРІРµРґРµРЅРёСЏ

*   **Base URL:** `/api`
*   **Protocol:** HTTP/1.1 (REST) & Server-Sent Events (SSE)
*   **Content-Type:** `application/json`
*   **Date Format:** ISO 8601 (`2023-10-27T10:00:00Z`)

### 1.1. РђСѓС‚РµРЅС‚РёС„РёРєР°С†РёСЏ
Р’СЃРµ Р·Р°РїСЂРѕСЃС‹ Рє Р·Р°С‰РёС‰РµРЅРЅС‹Рј СЌРЅРґРїРѕРёРЅС‚Р°Рј С‚СЂРµР±СѓСЋС‚ Р·Р°РіРѕР»РѕРІРѕРє:
`Authorization: Bearer <your_jwt_token>`

### 1.2. Р¤РѕСЂРјР°С‚ РѕС‚РІРµС‚РѕРІ (Envelope Pattern)
Р’СЃРµ РѕС‚РІРµС‚С‹ API РѕР±РµСЂРЅСѓС‚С‹ РІ РµРґРёРЅС‹Р№ РєРѕРЅРІРµСЂС‚.

**РЈСЃРїРµС€РЅС‹Р№ РѕС‚РІРµС‚ (200 OK, 201 Created, 202 Accepted):**
```json
{
  "status": "success",
  "data": { ... },       // РћСЃРЅРѕРІРЅС‹Рµ РґР°РЅРЅС‹Рµ (РѕР±СЉРµРєС‚ РёР»Рё РјР°СЃСЃРёРІ)
  "meta": {              // РњРµС‚Р°РґР°РЅРЅС‹Рµ (РїСЂРёСЃСѓС‚СЃС‚РІСѓСЋС‚ РїСЂРё РїР°РіРёРЅР°С†РёРё)
    "total": 100,
    "limit": 50,
    "offset": 0,
    "has_next": true,
    "has_prev": false
  }
}
```

**РћС‚РІРµС‚ СЃ РѕС€РёР±РєРѕР№ (4xx, 5xx):**
```json
{
  "status": "error",
  "error": {
    "error": "РћРїРёСЃР°РЅРёРµ РѕС€РёР±РєРё РґР»СЏ РїРѕР»СЊР·РѕРІР°С‚РµР»СЏ РёР»Рё СЂР°Р·СЂР°Р±РѕС‚С‡РёРєР°"
  }
}
```

---

## 2. РђСѓС‚РµРЅС‚РёС„РёРєР°С†РёСЏ

### 2.1. Р’С…РѕРґ РІ СЃРёСЃС‚РµРјСѓ
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
      "fullName": "Р“Р»Р°РІРЅС‹Р№ РђРґРјРёРЅРёСЃС‚СЂР°С‚РѕСЂ",
      "roles": ["admin"]
    }
  }
}
```

---

## 3. Р“Р»РѕР±Р°Р»СЊРЅС‹Р№ РїРѕРёСЃРє (Search)

РћСЃРЅРѕРІРЅРѕР№ РёРЅСЃС‚СЂСѓРјРµРЅС‚ РЅР°РІРёРіР°С†РёРё. РС‰РµС‚ РљРѕРјРїР°РЅРёРё, РЎРµСЂРІРµСЂС‹, Р Р°Р±РѕС‡РёРµ СЃС‚Р°РЅС†РёРё Рё Р¤Р  РїРѕ РІРІРµРґРµРЅРЅРѕР№ СЃС‚СЂРѕРєРµ. Р РµР·СѓР»СЊС‚Р°С‚С‹ СЃРіСЂСѓРїРїРёСЂРѕРІР°РЅС‹ РїРѕ РљРѕРјРїР°РЅРёСЏРј-РІР»Р°РґРµР»СЊС†Р°Рј.

### 3.1. РџРѕРёСЃРє СЃСѓС‰РЅРѕСЃС‚РµР№
`GET /search`

**Parameters:**
*   `term` (string, required): РџРѕРёСЃРєРѕРІР°СЏ С„СЂР°Р·Р° (IP, Serial, Name, Address, INN).
*   `limit` (int, optional): Р›РёРјРёС‚ Р·Р°РїРёСЃРµР№ (default: 50).

**Response:**
```json
{
  "status": "success",
  "data": {
    "search_results": [
      {
        "owner": {
          "uuid": "internal-uuid-company",
          "external_uuid": "sd-uuid-company", // РњРѕР¶РµС‚ Р±С‹С‚СЊ null
          "name": "РћРћРћ Р РѕРјР°С€РєР°",
          "address": "Рі. РњРѕСЃРєРІР°, СѓР». Р›РµРЅРёРЅР° 1",
          "active_contract": true,
          "parent_info": { "uuid": "parent-uuid", "name": "РҐРѕР»РґРёРЅРі Р“СЂСѓРїРї" }
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

## 4. Р—Р°РґР°С‡Рё СЃРІРµСЂРєРё (Tasks)

Р Р°Р±РѕС‡РµРµ РјРµСЃС‚Рѕ РѕРїРµСЂР°С‚РѕСЂР°. Р—РґРµСЃСЊ РѕС‚РѕР±СЂР°Р¶Р°СЋС‚СЃСЏ РєРѕРЅС„Р»РёРєС‚С‹ РґР°РЅРЅС‹С… Рё РЅРѕРІС‹Рµ СѓСЃС‚СЂРѕР№СЃС‚РІР°, С‚СЂРµР±СѓСЋС‰РёРµ СЂРµС€РµРЅРёСЏ.

### 4.1. РџРѕР»СѓС‡РёС‚СЊ СЃРїРёСЃРѕРє Р·Р°РґР°С‡
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
        // РЎС‚СЂСѓРєС‚СѓСЂР° Р·Р°РІРёСЃРёС‚ РѕС‚ task_type.
        // РџСЂРёРјРµСЂ РґР»СЏ add_equipment:
        "agent_data": { ... },     // РџРѕР»РЅС‹Рµ СЃС‹СЂС‹Рµ РґР°РЅРЅС‹Рµ РѕС‚ Р°РіРµРЅС‚Р°
        "etalon_owner_id": "...",  // РџСЂРµРґР»Р°РіР°РµРјС‹Р№ РІР»Р°РґРµР»РµС†
        "equipment_data": {        // РЎС„РѕСЂРјРёСЂРѕРІР°РЅРЅС‹Рµ РґР°РЅРЅС‹Рµ РґР»СЏ РїСЂРµРІСЊСЋ
           "rn_kkt": "...",
           "serial_number": "..."
        }
      }
    }
  ]
}
```

### 4.2. Р РµС€РёС‚СЊ Р·Р°РґР°С‡Сѓ (Resolve)
`POST /tasks/{id}/resolve`

Р”РµР№СЃС‚РІРёРµ Р·Р°РІРёСЃРёС‚ РѕС‚ `resolution_payload.action`.

**Request Body (РџСЂРёРјРµСЂ: РџРѕРґС‚РІРµСЂРґРёС‚СЊ СЃРѕР·РґР°РЅРёРµ):**
```json
{
  "status": "resolved",
  "comment": "РћР±РѕСЂСѓРґРѕРІР°РЅРёРµ РґРѕР±Р°РІР»РµРЅРѕ РєРѕСЂСЂРµРєС‚РЅРѕ",
  "resolution_payload": {
    "action": "create" 
  }
}
```

**Request Body (РџСЂРёРјРµСЂ: РЎРјРµРЅРёС‚СЊ РІР»Р°РґРµР»СЊС†Р°):**
```json
{
  "status": "resolved",
  "resolution_payload": {
    "action": "create"
  }
}
```

### 4.3. РЎРѕР·РґР°С‚СЊ СЃСѓС‰РЅРѕСЃС‚СЊ РІ ServiceDesk (РЅР° РѕСЃРЅРѕРІРµ Р·Р°РґР°С‡Рё)
`POST /tasks/{id}/create-entity-in-sd`

РСЃРїРѕР»СЊР·СѓРµС‚СЃСЏ РґР»СЏ Р·Р°РґР°С‡ С‚РёРїР° `add_equipment`, РєРѕРіРґР° РѕРїРµСЂР°С‚РѕСЂ РїРѕРґС‚РІРµСЂРґРёР» РґР°РЅРЅС‹Рµ Рё С…РѕС‡РµС‚ РѕС‚РїСЂР°РІРёС‚СЊ РёС… РІ Naumen.

**Request Body:**
```json
{
  "entity_type": "FiscalRegister" // РёР»Рё Server, Workstation
}
```
**Response:** `202 Accepted` (РѕРїРµСЂР°С†РёСЏ РІС‹РїРѕР»РЅСЏРµС‚СЃСЏ Р°СЃРёРЅС…СЂРѕРЅРЅРѕ, СЃС‚Р°С‚СѓСЃ Р·Р°РґР°С‡Рё РёР·РјРµРЅРёС‚СЃСЏ РЅР° `pending_sd_action`).

### 4.4. РџРѕР»СѓС‡РёС‚СЊ РіСЂСѓРїРїС‹ РґСѓР±Р»РёРєР°С‚РѕРІ
`GET /duplicates`

Р’РѕР·РІСЂР°С‰Р°РµС‚ СЃРїРёСЃРѕРє РіСЂСѓРїРї СЃСѓС‰РЅРѕСЃС‚РµР№, Сѓ РєРѕС‚РѕСЂС‹С… СЃРѕРІРїР°РґР°СЋС‚ РєР»СЋС‡РµРІС‹Рµ РїРѕР»СЏ (IP, Serial, TeamViewer ID).

---

## 5. РўРёРєРµС‚С‹ (ServiceDesk)

Р Р°Р±РѕС‚Р° СЃ Р·Р°СЏРІРєР°РјРё.

### 5.1. РЎРїРёСЃРѕРє Р·Р°СЏРІРѕРє
`GET /tickets`

**Parameters:**
*   `company_id` (optional): Р¤РёР»СЊС‚СЂ РїРѕ РєРѕРјРїР°РЅРёРё.
*   `asset_id` (optional): Р¤РёР»СЊС‚СЂ РїРѕ РѕР±РѕСЂСѓРґРѕРІР°РЅРёСЋ.
*   `status` (optional): `registered,inprogress,closed`.
*   `search` (optional): РџРѕРёСЃРє РїРѕ С‚РµРјРµ РёР»Рё РЅРѕРјРµСЂСѓ.
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
      "subject": "РќРµ СЂР°Р±РѕС‚Р°РµС‚ РїСЂРёРЅС‚РµСЂ",
      "last_activity": "2023-10-27T12:00:00Z",
      "company_id": "..."
    }
  ],
  "meta": { "total": 50, ... }
}
```

### 5.2. Р”РµС‚Р°Р»Рё Р·Р°СЏРІРєРё
`GET /tickets/{id}`

Р’РѕР·РІСЂР°С‰Р°РµС‚ РїРѕР»РЅСѓСЋ РёРЅС„РѕСЂРјР°С†РёСЋ: РѕРїРёСЃР°РЅРёРµ (HTML), РёСЃС‚РѕСЂРёСЋ РёР·РјРµРЅРµРЅРёР№, РІР»РѕР¶РµРЅРёСЏ Рё РєРѕРјРјРµРЅС‚Р°СЂРёРё.

### 5.3. РЎРѕР·РґР°С‚СЊ Р·Р°СЏРІРєСѓ (Р’РЅСѓС‚СЂРµРЅРЅСЋСЋ)
`POST /tickets`

**Request Body:**
```json
{
  "subject": "РџСЂРѕР±Р»РµРјР° СЃ РєР°СЃСЃРѕР№",
  "description": "РћРїРёСЃР°РЅРёРµ РїСЂРѕР±Р»РµРјС‹...",
  "company_id": "uuid-company",
  "priority": "high", // low, medium, high, critical
  "type": "incident",
  "asset_id": "uuid-fiscal", // РћРїС†РёРѕРЅР°Р»СЊРЅРѕ
  "asset_type": "FiscalRegister"
}
```

### 5.4. РЎРјРµРЅРёС‚СЊ СЃС‚Р°С‚СѓСЃ
`PATCH /tickets/{id}/status`

**Request Body:**
```json
{
  "status": "inprogress",
  "comment": "Р’Р·СЏР» РІ СЂР°Р±РѕС‚Сѓ" // РћРїС†РёРѕРЅР°Р»СЊРЅРѕ
}
```

### 5.5. РќР°Р·РЅР°С‡РёС‚СЊ РёСЃРїРѕР»РЅРёС‚РµР»СЏ
`PATCH /tickets/{id}/assign`

**Request Body:**
```json
{
  "assignee_id": 12 // ID РїРѕР»СЊР·РѕРІР°С‚РµР»СЏ СЃРёСЃС‚РµРјС‹ (User.ID), РёР»Рё null РґР»СЏ СЃРЅСЏС‚РёСЏ
}
```

---

## 6. РРЅС„СЂР°СЃС‚СЂСѓРєС‚СѓСЂР° (CMDB)

CRUD РѕРїРµСЂР°С†РёРё РґР»СЏ РѕСЃРЅРѕРІРЅС‹С… СЃСѓС‰РЅРѕСЃС‚РµР№.

### 6.1. РљРѕРјРїР°РЅРёРё
*   `GET /companies/{id}`: Р”РµС‚Р°Р»Рё РєРѕРјРїР°РЅРёРё.
*   `GET /companies/{id}/infrastructure`: **Р’Р°Р¶РЅС‹Р№ СЌРЅРґРїРѕРёРЅС‚**. Р’РѕР·РІСЂР°С‰Р°РµС‚ РїР»РѕСЃРєРёР№ СЃРїРёСЃРѕРє РІСЃРµРіРѕ РѕР±РѕСЂСѓРґРѕРІР°РЅРёСЏ (Server, Workstation, FiscalRegister), РїСЂРёРЅР°РґР»РµР¶Р°С‰РµРіРѕ РєРѕРјРїР°РЅРёРё. РСЃРїРѕР»СЊР·СѓРµС‚СЃСЏ РґР»СЏ РїРѕСЃС‚СЂРѕРµРЅРёСЏ РґРµСЂРµРІР° РЅР° С„СЂРѕРЅС‚РµРЅРґРµ.

### 6.2. РЎРµСЂРІРµСЂС‹
*   `POST /servers/{id}/poll`: РџСЂРёРЅСѓРґРёС‚РµР»СЊРЅС‹Р№ РѕРїСЂРѕСЃ СЃС‚Р°С‚СѓСЃР° (RMS/Iiko).
*   `POST /servers/{id}/license`: РЈСЃС‚Р°РЅРѕРІРєР° Р»РёС†РµРЅР·РёРё (С‚СЂРµР±СѓРµС‚СЃСЏ `unique_id` РІ С‚РµР»Рµ).
*   `POST /servers/{id}/additional_owners`: Р”РѕР±Р°РІРёС‚СЊ СЃРѕРІР»Р°РґРµР»СЊС†Р° (С‚РµР»Рѕ: `{"company_id": "..."}`).

### 6.3. РћР±С‰РёРµ CRUD
Р”Р»СЏ `servers`, `workstations`, `fiscals` РґРѕСЃС‚СѓРїРЅС‹ СЃС‚Р°РЅРґР°СЂС‚РЅС‹Рµ РјРµС‚РѕРґС‹:
*   `GET /{entity}/{id}`
*   `PUT /{entity}/{id}` (РѕР±РЅРѕРІР»РµРЅРёРµ РїРѕР»РµР№)
*   `DELETE /{entity}/{id}` (РјСЏРіРєРѕРµ СѓРґР°Р»РµРЅРёРµ)

---

## 7. Real-time РЎРѕР±С‹С‚РёСЏ (SSE)

РџРѕРґРїРёСЃРєР° РЅР° РѕР±РЅРѕРІР»РµРЅРёСЏ РІ СЂРµР°Р»СЊРЅРѕРј РІСЂРµРјРµРЅРё.

**Endpoint:** `GET /events`

**РўРёРїС‹ СЃРѕР±С‹С‚РёР№ (event):**
*   `server.polling.succeeded`: РЎС‚Р°С‚СѓСЃ СЃРµСЂРІРµСЂР° РѕР±РЅРѕРІРёР»СЃСЏ (payload: `{ serverUUID, newStatus, ... }`).
*   `server.polling.failed`: РЎРµСЂРІРµСЂ РЅРµРґРѕСЃС‚СѓРїРµРЅ.
*   `servicedesk.entity.create.requested`: Р—Р°РґР°С‡Р° СѓС€Р»Р° РІ РѕР±СЂР°Р±РѕС‚РєСѓ (СЃС‚Р°С‚СѓСЃ Р·Р°РґР°С‡Рё РёР·РјРµРЅРёР»СЃСЏ).
*   `servicedesk.entity.updated`: РџСЂРёС€Р»Рё РЅРѕРІС‹Рµ РґР°РЅРЅС‹Рµ РёР· SD.
*   `duplicates.found`: РќР°Р№РґРµРЅС‹ РЅРѕРІС‹Рµ РґСѓР±Р»РёРєР°С‚С‹.

**Client Implementation (JS):**
```javascript
const evtSource = new EventSource("/api/events?token=" + jwtToken);
evtSource.addEventListener("server.polling.succeeded", (e) => {
    const data = JSON.parse(e.data);
    console.log("Server updated:", data);
});
```

## 8. Network Candidates

### 8.1. Список network-кандидатов
GET /network-candidates

Параметры: status, limit, offset.

### 8.2. Карточка network-кандидата
GET /network-candidates/{id}`r

Ответ содержит candidate и groups (1 WS + 0..N FR).

### 8.3. Подтверждение network-кандидата
POST /network-candidates/{id}/approve`r

### 8.4. Перенос группы в новый кандидат
POST /network-candidates/{id}/groups/{groupID}/remove`r

