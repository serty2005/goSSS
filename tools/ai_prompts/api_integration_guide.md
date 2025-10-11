# Руководство по интеграции фронтенда с Etalon Server API

## Анализ текущего состояния проекта

Проект **Etalon-Server** представляет собой центральный сервер-агрегатор для создания эталонной базы данных IT-инфраструктуры клиентов. Бэкенд реализован на Go с использованием Clean Architecture и предоставляет REST API для фронтенд-приложения на Vue.js.

## Актуальные API Endpoints

### 1. Аутентификация

#### POST `/api/auth/login`
Вход в систему для пользователей интерфейса.

**Request DTO:**
```javascript
{
  "username": "string",
  "password": "string"
}
```

**Response DTO:**
```javascript
{
  "access_token": "string",
  "user": {
    "id": "number",
    "username": "string",
    "fullName": "string",
    "roles": ["string"]
  }
}
```

**Использование в существующем коде:** `src/services/api.js`, `src/store/auth.js`

---

### 2. Агенты (Agent API)

#### POST `/api/agents/register`
Регистрация нового агента в системе.

**Request DTO:**
```javascript
{
  "agent_uuid": "string",
  "hostname": "string",
  "agent_version": "string",
  "initial_data": {
    "modelName": "string",
    "serialNumber": "string",
    "RNM": "string",
    "INN": "string",
    "fn_serial": "string",
    "dateTime_end": "string",
    "ffdVersion": "string",
    "fnExecution": "string",
    "organizationName": "string",
    "address": "string",
    "datetime_reg": "string",
    "hostname": "string",
    "url_rms": "string",
    "crmId": "string",
    "teamviewer_id": "string",
    "anydesk_id": "string",
    "litemanager_id": "string",
    "current_time": "string",
    "agent_version": "string",
    "installed_driver": "string",
    "bootVersion": "string",
    "licenses": "object|string"
  }
}
```

#### GET `/api/agents/{uuid}/config`
Получение конфигурации для зарегистрированного агента.

#### POST `/api/agents/{uuid}/data`
Отправка оперативных данных от агента.

---

### 3. Поиск

#### GET `/api/search`
Глобальный поиск по всем сущностям системы.

**Query параметры:**
- `term` - поисковый запрос (обязательный)
- `limit` - максимальное количество результатов (по умолчанию 50)

**Response DTO:**
```javascript
{
  "search_results": [
    {
      "owner": {
        "uuid": "string",
        "external_uuid": "string",
        "name": "string",
        "address": "string",
        "active_contract": "boolean",
        "additional_info": "string",
        "parent_info": {
          "uuid": "string",
          "name": "string"
        }
      },
      "found_entities": [
        {
          "entity_type": "Server|Workstation|FiscalRegister",
          "data": {
            // В зависимости от типа сущности
            "uuid": "string",
            "external_uuid": "string",
            "device_name": "string",
            "ip": "string",
            "operational_status": "string",
            "health_status": "string",
            "status_details": "object",
            "anydesk": "string",
            "teamviewer": "string",
            "litemanager": "string",
            "unique_id": "string",
            "partners_link": "string",
            "server_name": "string",
            "server_version": "string",
            "last_polled_at": "datetime"
          }
        }
      ]
    }
  ]
}
```

**Использование в существующем коде:** `src/store/search.js`, `src/views/Search.vue`

---

### 4. Задачи (Tasks)

#### GET `/api/tasks`
Получение списка задач на сверку с пагинацией и фильтрацией.

**Query параметры:**
- `status` - фильтр по статусу
- `limit` - количество элементов (по умолчанию 50)
- `offset` - смещение для пагинации

**Response DTO:**
```javascript
[
  {
    "id": "number",
    "task_type": "string",
    "entity_type": "string",
    "entity_uuid": "string",
    "details": "object",
    "status": "string",
    "comment": "string",
    "created_at": "datetime",
    "updated_at": "datetime"
  }
]
```

#### POST `/api/tasks/{id}/resolve`
Решение задачи оператором.

**Request DTO:**
```javascript
{
  "status": "resolved|rejected|pending_sd_action",
  "comment": "string",
  "resolution_payload": {
    "action": "update_in_sd|delete_duplicate|resolve_data_conflict",
    "entity_uuid_to_delete": "string", // для delete_duplicate
    "strategy": "use_local|use_remote" // для resolve_data_conflict
  }
}
```

#### POST `/api/tasks/{id}/create-entity-in-sd`
Создание новой сущности в ServiceDesk на основе данных агента.

**Request DTO:**
```javascript
{
  "entity_type": "Server|Workstation|FiscalRegister"
}
```

**Использование в существующем коде:** `src/store/tasks.js`, `src/views/Tasks.vue`, `src/components/tasks/`

---

### 5. Дубликаты

#### GET `/api/duplicates`
Поиск дубликатов в системе по полям (IP, AnyDesk, TeamViewer, LiteManager).

**Response DTO:**
```javascript
[
  {
    "field": "string",
    "value": "string",
    "entity_type": "string",
    "main_record": "object",
    "duplicates": ["object"]
  }
]
```

---

### 6. CRUD операции для сущностей

#### Servers (Серверы)
- **GET** `/api/servers` - список серверов с пагинацией
- **GET** `/api/servers/{id}` - получение конкретного сервера
- **POST** `/api/servers` - создание нового сервера
- **PUT** `/api/servers/{id}` - обновление сервера
- **DELETE** `/api/servers/{id}` - удаление сервера

**Server DTO:**
```javascript
{
  "unique_id": "string",
  "crm_id": "string",
  "teamviewer": "string",
  "rdp": "string",
  "anydesk": "string",
  "ip": "string",
  "device_name": "string",
  "server_name": "string",
  "server_version": "string",
  "description": "string",
  "owner_id": "string"
}
```

#### Workstations (Рабочие станции)
- **GET** `/api/workstations` - список рабочих станций
- **GET** `/api/workstations/{id}` - получение конкретной рабочей станции
- **POST** `/api/workstations` - создание новой рабочей станции
- **PUT** `/api/workstations/{id}` - обновление рабочей станции
- **DELETE** `/api/workstations/{id}` - удаление рабочей станции

**Workstation DTO:**
```javascript
{
  "teamviewer": "string",
  "anydesk": "string",
  "litemanager": "string",
  "device_name": "string",
  "description": "string",
  "owner_id": "string"
}
```

#### FiscalRegisters (Фискальные регистраторы)
- **GET** `/api/fiscal-registers` - список фискальных регистраторов
- **GET** `/api/fiscal-registers/{id}` - получение конкретного фискального регистратора
- **POST** `/api/fiscal-registers` - создание нового фискального регистратора
- **PUT** `/api/fiscal-registers/{id}` - обновление фискального регистратора
- **DELETE** `/api/fiscal-registers/{id}` - удаление фискального регистратора

**FiscalRegister DTO:**
```javascript
{
  "model_kkt": "string",
  "rn_kkt": "string",
  "inn": "string",
  "fr_serial_number": "string",
  "fn_number": "string",
  "fr_downloader": "string",
  "fr_firmware": "string",
  "driver_version": "string",
  "owner_id": "string"
}
```

#### Companies (Компании)
- **GET** `/api/companies/{id}` - получение конкретной компании
- **POST** `/api/companies` - создание новой компании
- **PUT** `/api/companies/{id}` - обновление компании
- **DELETE** `/api/companies/{id}` - удаление компании

**Company DTO:**
```javascript
{
  "title": "string",
  "address": "string",
  "additional_name": "string",
  "parent_uuid": "string"
}
```

---

### 7. Действия с серверами

#### POST `/api/servers/{id}/install_license`
Установка лицензии на сервер.

**Request DTO:**
```javascript
{
  "uniqueId": "string"
}
```

#### POST `/api/servers/{id}/poll`
Принудительный опрос статуса сервера.

#### POST `/api/servers/{serverID}/additional_owners`
Добавление дополнительного владельца к серверу.

**Request DTO:**
```javascript
{
  "company_id": "string"
}
```

#### DELETE `/api/servers/{serverID}/additional_owners/{companyID}`
Удаление дополнительного владельца от сервера.

---

### 8. Контракты

#### GET `/api/contracts/{id}` - получение контракта
#### POST `/api/contracts` - создание контракта
#### PUT `/api/contracts/{id}` - обновление контракта
#### DELETE `/api/contracts/{id}` - удаление контракта

**Contract DTO:**
```javascript
{
  "state": "string",
  "state_start_time": "datetime",
  "services": "object",
  "recipients": "object",
  "service_level": "number",
  "company_ids": ["string"]
}
```

---

### 9. Управление пользователями

#### GET `/api/users` - список пользователей
#### POST `/api/users` - создание пользователя
#### PUT `/api/users/{id}` - обновление пользователя
#### DELETE `/api/users/{id}` - удаление пользователя

**User DTO:**
```javascript
{
  "username": "string",
  "password": "string",
  "fullName": "string",
  "roles": ["string"]
}
```

---

## Интеграция с ServiceDesk (Naumen)

✅ **Подтверждено наличие интеграции:** В коде реализована интеграция с ServiceDesk через плагин `internal/infra/plugins/naumen/`. Система поддерживает:

1. **Создание сущностей в ServiceDesk** через задачи типа `add_equipment`
2. **Обновление данных в ServiceDesk** через задачи типа `need_update`
3. **Разрешение конфликтов дубликатов** через задачи типа `data_conflict`
4. **Асинхронное выполнение операций** через воркер `SDEditorWorker`

**Ключевые компоненты интеграции:**
- `internal/services/sd_editor_service.go` - сервис для редактирования данных в ServiceDesk
- `internal/core/workers/sd_editor_worker.go` - асинхронный воркер для выполнения операций
- `internal/infra/plugins/naumen/client.go` - клиент для взаимодействия с Naumen ServiceDesk

## Пошаговый план интеграции новых возможностей

### Этап 1: Расширение существующих сервисов

#### 1.1 Обновление `src/services/api.js`

Добавить недостающие функции для работы с новыми endpoints:

```javascript
// Добавьте в src/services/api.js

// Работа с серверами
export const serversApi = createCrudApiService('servers');
export const workstationsApi = createCrudApiService('workstations');
export const fiscalRegistersApi = createCrudApiService('fiscal-registers');

// Действия с серверами
export const installLicense = (serverUuid, payload) => {
  return apiClient.post(`/servers/${serverUuid}/install_license`, payload);
};

export const pollServer = (uuid) => {
  return apiClient.post(`/servers/${uuid}/poll`);
};

// Работа с контрактами
export const contractsApi = createCrudApiService('contracts');

// Управление дополнительными владельцами серверов
export const addAdditionalOwner = (serverId, companyId) => {
  return apiClient.post(`/servers/${serverId}/additional_owners`, { company_id: companyId });
};

export const removeAdditionalOwner = (serverId, companyId) => {
  return apiClient.delete(`/servers/${serverId}/additional_owners/${companyId}`);
};
```

#### 1.2 Расширение stores

**Обновить `src/store/search.js`:**

```javascript
// Добавить в actions секцию
async fetchEntityDetails({ entityType, entityId }) {
  const apiService = apiServiceMap[entityType];
  if (!apiService) return null;
  
  try {
    const { data } = await apiService.getById(entityId);
    return data;
  } catch (err) {
    this.error = `Ошибка при загрузке деталей ${entityType}.`;
    throw err;
  }
},

async createEntity({ entityType, data }) {
  try {
    await this.createEntity({ entityType, data });
    // Обновить соответствующий список
    await this.fetchEntities({ entityType, params: { limit: 50, offset: 0 } });
  } catch (err) {
    throw err;
  }
},

async updateEntity({ entityType, id, data }) {
  try {
    await this.updateEntity({ entityType, id, data });
    // Обновить локальные данные
    const entityIndex = this.entities[entityType].items.findIndex(item => item.id === id);
    if (entityIndex !== -1) {
      this.entities[entityType].items[entityIndex] = { ...this.entities[entityType].items[entityIndex], ...data };
    }
  } catch (err) {
    throw err;
  }
},

async deleteEntity({ entityType, id }) {
  try {
    await this.deleteEntity({ entityType, id });
    // Удалить из локального списка
    this.entities[entityType].items = this.entities[entityType].items.filter(item => item.id !== id);
    this.entities[entityType].total -= 1;
  } catch (err) {
    throw err;
  }
}
```

### Этап 2: Создание новых компонентов

#### 2.1 Компонент для управления серверами

Создать `src/views/Servers.vue`:

```vue
<template>
  <div class="p-4 sm:p-8 max-w-7xl mx-auto">
    <div class="flex justify-between items-center mb-6">
      <h1 class="text-2xl font-bold">Управление серверами</h1>
      <button @click="showCreateModal = true" class="btn btn-primary">
        Добавить сервер
      </button>
    </div>

    <!-- Таблица серверов -->
    <div class="bg-slate-800/60 rounded-lg overflow-hidden">
      <table class="w-full">
        <thead class="bg-slate-700/50">
          <tr>
            <th class="px-4 py-3 text-left">Имя устройства</th>
            <th class="px-4 py-3 text-left">IP адрес</th>
            <th class="px-4 py-3 text-left">Статус</th>
            <th class="px-4 py-3 text-left">Действия</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="server in servers" :key="server.id" class="border-b border-slate-700">
            <td class="px-4 py-3">{{ server.device_name || 'Без имени' }}</td>
            <td class="px-4 py-3">
              <span class="font-mono">{{ server.ip || 'Не указан' }}</span>
            </td>
            <td class="px-4 py-3">
              <span :class="getStatusClass(server.operational_status)">
                {{ server.operational_status || 'Неизвестен' }}
              </span>
            </td>
            <td class="px-4 py-3">
              <button @click="editServer(server)" class="btn btn-sm btn-secondary mr-2">
                Редактировать
              </button>
              <button @click="installLicense(server)" class="btn btn-sm btn-primary mr-2">
                Установить лицензию
              </button>
              <button @click="pollServer(server)" class="btn btn-sm btn-info">
                Опросить статус
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Модальные окна -->
    <ServerEditModal 
      :show="showEditModal" 
      :server="selectedServer"
      @close="closeEditModal"
      @saved="handleServerSaved"
    />
    <LicenseInstallModal 
      :show="showLicenseModal" 
      :server="selectedServer"
      @close="closeLicenseModal"
      @submitted="handleLicenseSubmitted"
    />
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue';
import { useSearchStore } from '@/store/search';
import { serversApi, installLicense, pollServer } from '@/services/api';

const searchStore = useSearchStore();
const servers = ref([]);
const showEditModal = ref(false);
const showLicenseModal = ref(false);
const selectedServer = ref(null);

const fetchServers = async () => {
  try {
    await searchStore.fetchEntities({ entityType: 'servers', params: { limit: 100, offset: 0 } });
    servers.value = searchStore.entities.servers.items;
  } catch (err) {
    console.error('Ошибка при загрузке серверов:', err);
  }
};

const editServer = (server) => {
  selectedServer.value = server;
  showEditModal.value = true;
};

const installLicense = (server) => {
  selectedServer.value = server;
  showLicenseModal.value = true;
};

const pollServer = async (server) => {
  try {
    await pollServer(server.id);
    // Показать уведомление об успехе
  } catch (err) {
    console.error('Ошибка при опросе сервера:', err);
  }
};

const closeEditModal = () => {
  showEditModal.value = false;
  selectedServer.value = null;
};

const closeLicenseModal = () => {
  showLicenseModal.value = false;
  selectedServer.value = null;
};

const handleServerSaved = async () => {
  await fetchServers();
  closeEditModal();
};

const handleLicenseSubmitted = async () => {
  await fetchServers();
  closeLicenseModal();
};

onMounted(() => {
  fetchServers();
});
</script>
```

#### 2.2 Компонент для управления контрактами

Создать `src/views/Contracts.vue`:

```vue
<template>
  <div class="p-4 sm:p-8 max-w-7xl mx-auto">
    <div class="flex justify-between items-center mb-6">
      <h1 class="text-2xl font-bold">Управление контрактами</h1>
      <button @click="showCreateModal = true" class="btn btn-primary">
        Создать контракт
      </button>
    </div>

    <!-- Список контрактов -->
    <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
      <div v-for="contract in contracts" :key="contract.id" 
           class="bg-slate-800/60 p-6 rounded-lg">
        <div class="flex justify-between items-start mb-4">
          <h3 class="text-lg font-semibold">{{ contract.state || 'Без названия' }}</h3>
          <div class="flex gap-2">
            <button @click="editContract(contract)" class="btn btn-sm btn-secondary">
              Редактировать
            </button>
            <button @click="deleteContract(contract)" class="btn btn-sm btn-danger">
              Удалить
            </button>
          </div>
        </div>
        
        <div class="space-y-2 text-sm">
          <p><strong>Уровень сервиса:</strong> {{ contract.service_level }}</p>
          <p><strong>Компаний:</strong> {{ contract.company_ids?.length || 0 }}</p>
        </div>
      </div>
    </div>

    <!-- Модальные окна -->
    <ContractEditModal 
      :show="showEditModal" 
      :contract="selectedContract"
      @close="closeEditModal"
      @saved="handleContractSaved"
    />
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue';
import { contractsApi } from '@/services/api';

const contracts = ref([]);
const showEditModal = ref(false);
const selectedContract = ref(null);

const fetchContracts = async () => {
  try {
    const { data } = await contractsApi.getAll({ limit: 100 });
    contracts.value = data;
  } catch (err) {
    console.error('Ошибка при загрузке контрактов:', err);
  }
};

const editContract = (contract) => {
  selectedContract.value = contract;
  showEditModal.value = true;
};

const deleteContract = async (contract) => {
  if (confirm('Вы уверены, что хотите удалить этот контракт?')) {
    try {
      await contractsApi.delete(contract.id);
      await fetchContracts();
    } catch (err) {
      console.error('Ошибка при удалении контракта:', err);
    }
  }
};

const closeEditModal = () => {
  showEditModal.value = false;
  selectedContract.value = null;
};

const handleContractSaved = async () => {
  await fetchContracts();
  closeEditModal();
};

onMounted(() => {
  fetchContracts();
});
</script>
```

### Этап 3: Обновление навигации

Обновить `src/components/layout/UserMenu.vue` для добавления новых ссылок:

```vue
const navItems = [
  { title: 'Общий поиск', value: 'search', to: '/', icon: IconSearch },
  { title: 'Задачи на сверку', value: 'tasks', to: '/tasks', icon: IconTasks },
  { title: 'Поиск дубликатов', value: 'duplicates', to: '/duplicates', icon: IconDuplicates },
  { title: 'Серверы', value: 'servers', to: '/servers', icon: IconServer },
  { title: 'Рабочие станции', value: 'workstations', to: '/workstations', icon: IconWorkstation },
  { title: 'Фискальные регистраторы', value: 'fiscal-registers', to: '/fiscal-registers', icon: IconFiscal },
  { title: 'Компании', value: 'companies', to: '/companies', icon: IconCompany },
  { title: 'Контракты', value: 'contracts', to: '/contracts', icon: IconContract },
  { title: 'Админ-панель', value: 'admin', to: '/admin', icon: IconSettings },
];
```

### Этап 4: Создание форм для редактирования

#### 4.1 Форма редактирования сервера

Создать `src/components/servers/ServerEditModal.vue`:

```vue
<template>
  <BaseModal :show="show" @close="$emit('close')">
    <template #header>
      <h3 class="text-lg font-semibold text-white">
        {{ isEditing ? 'Редактирование сервера' : 'Создание сервера' }}
      </h3>
    </template>
    
    <template #body>
      <form @submit.prevent="submit" class="space-y-4">
        <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
          <div>
            <label class="block text-sm font-medium text-gray-300 mb-2">
              Имя устройства
            </label>
            <input v-model="form.device_name" type="text" class="form-input" />
          </div>
          
          <div>
            <label class="block text-sm font-medium text-gray-300 mb-2">
              IP адрес
            </label>
            <input v-model="form.ip" type="text" class="form-input" />
          </div>
          
          <div>
            <label class="block text-sm font-medium text-gray-300 mb-2">
              AnyDesk ID
            </label>
            <input v-model="form.anydesk" type="text" class="form-input" />
          </div>
          
          <div>
            <label class="block text-sm font-medium text-gray-300 mb-2">
              TeamViewer ID
            </label>
            <input v-model="form.teamviewer" type="text" class="form-input" />
          </div>
          
          <div>
            <label class="block text-sm font-medium text-gray-300 mb-2">
              Unique ID
            </label>
            <input v-model="form.unique_id" type="text" class="form-input" />
          </div>
          
          <div>
            <label class="block text-sm font-medium text-gray-300 mb-2">
              CRM ID
            </label>
            <input v-model="form.crm_id" type="text" class="form-input" />
          </div>
        </div>
        
        <div>
          <label class="block text-sm font-medium text-gray-300 mb-2">
            Описание
          </label>
          <textarea v-model="form.description" rows="3" class="form-input"></textarea>
        </div>
      </form>
      
      <div v-if="error" class="mt-4 p-3 bg-red-500/20 text-red-300 border border-red-500/30 rounded-md">
        {{ error }}
      </div>
    </template>
    
    <template #footer>
      <div class="flex justify-end gap-2">
        <button @click="$emit('close')" type="button" class="btn btn-secondary">
          Отмена
        </button>
        <button @click="submit" type="submit" class="btn btn-primary" :disabled="isLoading">
          {{ isLoading ? 'Сохранение...' : 'Сохранить' }}
        </button>
      </div>
    </template>
  </BaseModal>
</template>

<script setup>
import { ref, computed, watch } from 'vue';
import BaseModal from '@/components/common/BaseModal.vue';
import { serversApi } from '@/services/api';

const props = defineProps({
  show: Boolean,
  server: Object,
});

const emit = defineEmits(['close', 'saved']);

const form = ref({
  device_name: '',
  ip: '',
  anydesk: '',
  teamviewer: '',
  unique_id: '',
  crm_id: '',
  description: ''
});

const isLoading = ref(false);
const error = ref(null);

const isEditing = computed(() => !!props.server?.id);

watch(() => props.server, (newServer) => {
  if (newServer) {
    form.value = {
      device_name: newServer.device_name || '',
      ip: newServer.ip || '',
      anydesk: newServer.anydesk || '',
      teamviewer: newServer.teamviewer || '',
      unique_id: newServer.unique_id || '',
      crm_id: newServer.crm_id || '',
      description: newServer.description || ''
    };
  }
  error.value = null;
}, { immediate: true });

const submit = async () => {
  if (isLoading.value) return;
  isLoading.value = true;
  error.value = null;
  
  try {
    if (isEditing.value) {
      await serversApi.update(props.server.id, form.value);
    } else {
      await serversApi.create(form.value);
    }
    emit('saved');
  } catch (err) {
    error.value = err.response?.data?.error || 'Произошла ошибка при сохранении';
  } finally {
    isLoading.value = false;
  }
};
</script>
```

### Этап 5: Расширение существующих компонентов

#### 5.1 Обновление EntityDetailModal

Добавить кнопки для действий с серверами в `src/components/search/EntityDetailModal.vue`:

```vue
<template #footer>
  <div class="flex justify-between items-center">
    <!-- Существующие ссылки -->
    <div>
      <a v-if="entity && entity.data.external_uuid" 
         :href="getServiceDeskLink(entity.data.external_uuid)" 
         target="_blank" 
         class="action-btn bg-gray-600 hover:bg-gray-700">
        Service Desk
      </a>
      <a v-if="entity && entity.data.partners_link" 
         :href="entity.data.partners_link" 
         target="_blank" 
         class="action-btn bg-blue-600 hover:bg-blue-700 ml-2">
        Портал партнера
      </a>
    </div>
    
    <!-- Новые кнопки действий -->
    <div class="flex gap-2">
      <!-- Кнопки для серверов -->
      <button v-if="props.entity.entity_type === 'Server'" 
              @click="installLicense" 
              class="action-btn bg-green-600 hover:bg-green-700">
        Установить лицензию
      </button>
      
      <button v-if="props.entity.entity_type === 'Server'" 
              @click="pollServer" 
              class="action-btn bg-blue-600 hover:bg-blue-700">
        Опросить статус
      </button>
      
      <!-- Существующие кнопки -->
      <button @click="$emit('edit', entity)" 
              class="action-btn bg-indigo-600 hover:bg-indigo-700">
        Редактировать
      </button>
      <button @click="$emit('delete', entity)" 
              class="action-btn bg-red-600 hover:bg-red-700">
        Удалить
      </button>
      <button @click="$emit('close')" 
              class="action-btn bg-slate-600 hover:bg-slate-500">
        Закрыть
      </button>
    </div>
  </div>
</template>

<script setup>
// Добавить методы
const installLicense = () => {
  // Эмитим событие для родительского компонента
  emit('install-license', props.entity);
};

const pollServer = () => {
  emit('poll-server', props.entity);
};

// Добавить эмит для новых событий
defineEmits(['close', 'edit', 'delete', 'install-license', 'poll-server']);
</script>
```

### Этап 6: Обновление роутера

Добавить новые роуты в `src/router/index.js`:

```javascript
{
  path: 'servers',
  name: 'Servers',
  component: () => import('@/views/Servers.vue'),
},
{
  path: 'workstations',
  name: 'Workstations',
  component: () => import('@/views/Workstations.vue'),
},
{
  path: 'fiscal-registers',
  name: 'FiscalRegisters',
  component: () => import('@/views/FiscalRegisters.vue'),
},
{
  path: 'companies',
  name: 'Companies',
  component: () => import('@/views/Companies.vue'),
},
{
  path: 'contracts',
  name: 'Contracts',
  component: () => import('@/views/Contracts.vue'),
}
```

## Рекомендации по архитектуре

### 1. Сохранение текущего дизайна
- Использовать существующие цветовые схемы и компоненты
- Следовать установленным паттернам для модальных окон и форм
- Поддерживать responsive дизайн

### 2. Обработка ошибок
```javascript
// Пример централизованной обработки ошибок
const handleApiError = (error) => {
  if (error.response?.status === 401) {
    // Перенаправление на страницу логина
    router.push('/login');
  } else if (error.response?.status >= 500) {
    // Показать уведомление о серверной ошибке
    showNotification('Произошла серверная ошибка', 'error');
  } else {
    // Показать ошибку от API
    showNotification(error.response?.data?.error || 'Произошла ошибка', 'error');
  }
};
```

### 3. Оптимистичные обновления UI
```javascript
// Пример оптимистичного обновления
const updateEntityOptimistically = async (entityId, updates) => {
  // Сохраняем оригинальные данные для отката
  const originalData = { ...entities.value.find(e => e.id === entityId) };
  
  // Оптимистично обновляем UI
  const entity = entities.value.find(e => e.id === entityId);
  Object.assign(entity, updates);
  
  try {
    // Выполняем реальный запрос
    await api.updateEntity(entityId, updates);
  } catch (error) {
    // Откатываем изменения при ошибке
    Object.assign(entity, originalData);
    throw error;
  }
};
```

## Заключение

Документация охватывает все актуальные API endpoints Etalon Server и предоставляет конкретные инструкции по интеграции новых возможностей в существующее Vue.js приложение. Все предложенные изменения учитывают текущую архитектуру проекта и сохраняют единообразие пользовательского интерфейса.

Ключевые особенности интеграции:
- ✅ Полная поддержка CRUD операций для всех типов сущностей
- ✅ Интеграция с ServiceDesk для отправки изменений
- ✅ Асинхронные операции с задачами
- ✅ Поиск дубликатов
- ✅ Управление серверами и контрактами
- ✅ Сохранение существующего UX/UI дизайна