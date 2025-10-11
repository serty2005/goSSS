# ===================================================================
# Полный контекст кода для Vue.js проекта 'etalon-client'
# Сгенерировано: Sat Oct 11 02:12:07 UTC 2025
# ===================================================================

Это полный дамп исходного кода проекта. 
Каждый файл предваряется заголовком с путем к нему.
Структура проекта основана на Vue 3, Vite, Pinia и TailwindCSS.

# ===================================================================
# Файл: vite.config.js
# ===================================================================

```
// vite.config.js
import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'
import path from 'path'

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '');
  
  return {
    plugins: [vue()],
    resolve: {
      alias: {
        '@': path.resolve(__dirname, 'src'), 
      },
    },
    server: {
      host: '0.0.0.0',
      port: 5173,
      proxy: {
        // Прокси для нашего основного API
        '/api': {
          target: env.VITE_API_BASE_URL,
          changeOrigin: true,
        },
        // Новый прокси для RSS-ленты с анекдотами
        '/rss-proxy': {
          target: 'https://www.anekdot.ru',
          changeOrigin: true,
          rewrite: (path) => path.replace(/^\/rss-proxy/, '/rss'), // убираем /rss-proxy и добавляем /rss
        },
      },
    },
  }
})
```

# ===================================================================
# Файл: tailwind.config.js
# ===================================================================

```
// tailwind.config.js
/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./src/**/*.{vue,js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {},
  },
  plugins: [],
}
```

# ===================================================================
# Файл: postcss.config.js
# ===================================================================

```
export default {
  plugins: {
    tailwindcss: {},
    autoprefixer: {},
  },
}
```

# ===================================================================
# Файл: package.json
# ===================================================================

```
{
  "name": "etalon-client",
  "private": true,
  "version": "0.0.0",
  "type": "module",
  "scripts": {
    "dev": "vite --host 0.0.0.0",
    "build": "vite build",
    "preview": "vite preview",
    "deploy": "bash scripts/deploy.sh"
  },
  "dependencies": {
    "axios": "^1.11.0",
    "pinia": "^3.0.3",
    "vue": "^3.5.18",
    "vue-router": "^4.5.1"
  },
  "devDependencies": {
    "@vitejs/plugin-vue": "^6.0.1",
    "autoprefixer": "^10.4.21",
    "postcss": "^8.5.6",
    "tailwindcss": "^3.4.17",
    "vite": "^7.1.2"
  }
}
```

# ===================================================================
# Файл: src/components/layout/GlobalSearch.vue
# ===================================================================

```
<!-- src/components/layout/GlobalSearch.vue -->
<template>
  <div class="flex items-center gap-2 flex-grow" :class="containerClass">
    <!-- Кнопки навигации -->
    <button @click="navigate('back')" :disabled="!searchStore.canGoBack" class="nav-btn" title="Назад">
      ←
    </button>
    <button @click="navigate('forward')" :disabled="!searchStore.canGoForward" class="nav-btn" title="Вперед">
      →
    </button>
    
    <!-- Строка поиска и фильтр -->
    <div class="relative w-full flex items-center gap-2">
      <input
        v-model="uiStore.searchTerm"
        type="text"
        :placeholder="searchPlaceholder"
        class="w-full bg-slate-700/80 text-white placeholder:text-gray-400 rounded-lg py-2 pl-4 pr-10 border border-transparent focus:border-sky-500 focus:outline-none focus:ring-1 focus:ring-sky-500 transition-colors"
        @input="debouncedSearch"
        @keydown.enter="executeSearchNow"
      />
      <!-- Иконка-фильтр "Показывать без контракта" -->
      <button
        v-if="isSearchPage"
        @click="uiStore.toggleShowWithoutContract()"
        class="filter-btn"
        :class="{ 'active': uiStore.showWithoutContract }"
        title="Показывать/скрывать без контракта"
      >
        <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" viewBox="0 0 20 20" fill="currentColor">
          <path fill-rule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clip-rule="evenodd" />
        </svg>
      </button>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue';
import { useRoute } from 'vue-router';
import { useUiStore } from '@/store/ui';
import { useSearchStore } from '@/store/search';
import { useSettingsStore } from '@/store/settings';
import { debounce } from '@/utils/debounce';

const route = useRoute();
const uiStore = useUiStore();
const searchStore = useSearchStore();
const settingsStore = useSettingsStore();

const containerClass = computed(() => {
  return settingsStore.state.searchLayout === 'centered'
    ? 'max-w-xl' // Фиксированная ширина для центрированного вида
    : 'w-1/3 lg:w-2/5'; // Адаптивная ширина для широкого вида
});

const isSearchPage = computed(() => route.name === 'Search');
const searchPlaceholder = computed(() => route.name === 'Tasks' ? 'Поиск по задачам...' : 'Глобальный поиск...');
const executeSearchNow = () => uiStore.executeSearch();
const debouncedSearch = debounce(executeSearchNow, 500);

const navigate = (direction) => {
  const newTerm = searchStore.navigateHistory(direction);
  if (newTerm !== null) {
    uiStore.searchTerm = newTerm;
    searchStore.performGlobalSearch(newTerm, { addToHistory: false });
  }
};
</script>

<style scoped>
.nav-btn { @apply w-9 h-9 bg-slate-700 text-white/80 rounded-md hover:bg-slate-600 transition-colors flex items-center justify-center text-xl font-bold disabled:opacity-40 disabled:cursor-not-allowed flex-shrink-0; }
.filter-btn { @apply w-9 h-9 flex-shrink-0 rounded-md flex items-center justify-center transition-colors duration-200 bg-slate-700 text-white/50 hover:bg-slate-600; }
.filter-btn.active { @apply bg-green-500/20 text-green-400 hover:bg-green-500/30; }
</style>
```

# ===================================================================
# Файл: src/components/layout/TheHeader.vue
# ===================================================================

```
<!-- src/components/layout/TheHeader.vue -->
<template>
  <header class="bg-slate-800/80 backdrop-blur-lg shadow-md h-16 flex items-center justify-between px-6 sticky top-0 z-40 gap-4">
    <!-- Левая часть: Лого -->
    <div class="flex items-center gap-6">
      <div class="text-xl font-bold text-white">Etalon</div>
    </div>

    <!-- Центральная часть: Глобальный поиск -->
    <GlobalSearch />

    <!-- Правая часть: Меню пользователя -->
    <UserMenu />
  </header>
</template>

<script setup>
import UserMenu from './UserMenu.vue';
import GlobalSearch from './GlobalSearch.vue';
</script>
```

# ===================================================================
# Файл: src/components/layout/UserMenu.vue
# ===================================================================

```
<!-- src/components/layout/UserMenu.vue -->
<template>
  <div class="relative">
    <button @click.stop="isOpen = !isOpen" class="flex items-center gap-2 text-white/80 hover:text-white">
      <span>{{ authStore.currentUser?.username || 'Пользователь' }}</span>
      <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7"></path></svg>
    </button>
    <transition name="fade">
      <div v-if="isOpen" v-click-outside="() => isOpen = false" class="absolute right-0 mt-2 w-56 bg-slate-700 rounded-md shadow-lg py-1 z-50">
        <!-- Навигационные ссылки -->
        <router-link
          v-for="item in navItems"
          :key="item.value"
          :to="item.to"
          @click="isOpen = false"
          class="flex items-center px-4 py-2 text-sm text-gray-200 hover:bg-slate-600"
          :class="{ 'bg-sky-600/50 text-white': isLinkActive(item) }"
        >
          <component :is="item.icon" class="w-5 h-5 mr-3 text-gray-300" />
          {{ item.title }}
        </router-link>

        <!-- Разделитель -->
        <div class="my-1 border-t border-slate-600"></div>

        <!-- Ссылка на выход -->
        <a @click.prevent="logout" href="#" class="flex items-center px-4 py-2 text-sm text-gray-200 hover:bg-slate-600">
          <svg xmlns="http://www.w3.org/2000/svg" class="w-5 h-5 mr-3 text-gray-300" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
            <path stroke-linecap="round" stroke-linejoin="round" d="M17 16l4-4m0 0l-4-4m4 4H7m6 4v1a3 3 0 01-3 3H6a3 3 0 01-3-3V7a3 3 0 013-3h4a3 3 0 013 3v1" />
          </svg>
          Выход
        </a>
      </div>
    </transition>
  </div>
</template>

<script setup>
import { ref, h } from 'vue';
import { useRoute } from 'vue-router';
import { useAuthStore } from '@/store/auth';

const route = useRoute();
const authStore = useAuthStore();
const isOpen = ref(false);

const logout = () => {
  isOpen.value = false;
  authStore.logout();
};

const isLinkActive = (item) => {
  if (item.to === '/') {
    return route.path === '/';
  }
  return route.path.startsWith(item.to);
};

// Простая директива для отслеживания клика вне элемента
const vClickOutside = {
  mounted(el, binding) {
    el.__ClickOutsideHandler__ = event => {
      if (!(el === event.target || el.contains(event.target))) {
        binding.value(event);
      }
    };
    document.body.addEventListener('click', el.__ClickOutsideHandler__);
  },
  unmounted(el) {
    document.body.removeEventListener('click', el.__ClickOutsideHandler__);
  },
};

// --- Логика для навигации (перенесена из TheHeader.vue) ---
const createIcon = (d) => ({ render: () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', fill: 'none', viewBox: '0 0 24 24', stroke: 'currentColor', 'stroke-width': 1.5 }, [ h('path', { 'stroke-linecap': 'round', 'stroke-linejoin': 'round', d }) ]) });

const IconSearch = createIcon('M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z');
const IconTasks = createIcon('M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2m-6 9l2 2 4-4');
const IconDuplicates = createIcon('M16 17l-4 4m0 0l-4-4m4 4V3');
const IconSettings = createIcon('M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065zM15 12a3 3 0 11-6 0 3 3 0 016 0z');

const navItems = [
  { title: 'Общий поиск', value: 'search', to: '/', icon: IconSearch },
  { title: 'Задачи на сверку', value: 'tasks', to: '/tasks', icon: IconTasks },
  { title: 'Поиск дубликатов', value: 'duplicates', to: '/duplicates', icon: IconDuplicates },
  { title: 'Админ-панель', value: 'admin', to: '/admin', icon: IconSettings },
];
</script>

<style scoped>
.fade-enter-active, .fade-leave-active {
  transition: opacity 0.2s ease, transform 0.2s ease;
}
.fade-enter-from, .fade-leave-to {
  opacity: 0;
  transform: translateY(-10px);
}
</style>
```

# ===================================================================
# Файл: src/components/common/BaseModal.vue
# ===================================================================

```
<!-- src/components/common/BaseModal.vue -->
<template>
  <teleport to="body">
    <transition name="fade">
      <div v-if="show" @click.self="$emit('close')" class="fixed inset-0 bg-black/70 backdrop-blur-sm flex items-center justify-center z-50 p-4">
        <!-- Устанавливаем максимальную высоту и flex-структуру для всего окна -->
        <div class="bg-slate-800 rounded-lg shadow-xl w-full max-w-2xl flex flex-col max-h-[90vh]">
          <header class="p-4 border-b border-slate-700 flex-shrink-0">
            <!-- Заголовок теперь в слоте -->
            <slot name="header">
              <div class="flex justify-between items-center">
                <h3 class="text-lg font-semibold text-white">Заголовок</h3>
                <button @click="$emit('close')" class="text-gray-400 hover:text-white">&times;</button>
              </div>
            </slot>
          </header>
          <!-- Основной контент с прокруткой -->
          <section class="p-4 overflow-y-auto">
            <slot name="body">Тело модального окна</slot>
          </section>
          <!-- Футер -->
          <footer v-if="$slots.footer" class="p-4 border-t border-slate-700 flex-shrink-0">
            <slot name="footer"></slot>
          </footer>
        </div>
      </div>
    </transition>
  </teleport>
</template>

<script setup>
defineProps({ show: Boolean });
defineEmits(['close']);
</script>

<style scoped>
.fade-enter-active, .fade-leave-active {
  transition: opacity 0.3s ease;
}
.fade-enter-from, .fade-leave-to {
  opacity: 0;
}
</style>
```

# ===================================================================
# Файл: src/components/common/StatusIcon.vue
# ===================================================================

```
<!-- src/components/common/StatusIcon.vue -->
<template>
  <div class="w-5 h-5 flex items-center justify-center">
    <!-- OK: Зеленая галочка -->
    <svg v-if="status === 'ok'" xmlns="http://www.w3.org/2000/svg" class="w-5 h-5 text-green-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
      <path stroke-linecap="round" stroke-linejoin="round" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
    </svg>

    <!-- Attention Required: Желтый предупреждающий треугольник -->
    <svg v-if="status === 'attention_required'" xmlns="http://www.w3.org/2000/svg" class="w-5 h-5 text-yellow-400" viewBox="0 0 20 20" fill="currentColor">
      <path fill-rule="evenodd" d="M8.257 3.099c.765-1.36 2.722-1.36 3.486 0l5.58 9.92c.75 1.334-.21 3.03-1.742 3.03H4.42c-1.532 0-2.492-1.696-1.742-3.03l5.58-9.92zM10 13a1 1 0 11-2 0 1 1 0 012 0zm-1-8a1 1 0 00-1 1v4a1 1 0 102 0V6a1 1 0 00-1-1z" clip-rule="evenodd" />
    </svg>
    
    <!-- Locked: Серый замок (как пример, хотя статус будет применяться через CSS) -->
     <svg v-if="status === 'locked'" xmlns="http://www.w3.org/2000/svg" class="w-5 h-5 text-gray-500" viewBox="0 0 20 20" fill="currentColor">
      <path fill-rule="evenodd" d="M10 1a4.5 4.5 0 00-4.5 4.5V9H5a2 2 0 00-2 2v6a2 2 0 002 2h10a2 2 0 002-2v-6a2 2 0 00-2-2h-.5V5.5A4.5 4.5 0 0010 1zm3 8V5.5a3 3 0 10-6 0V9h6z" clip-rule="evenodd" />
    </svg>
  </div>
</template>

<script setup>
defineProps({
  status: {
    type: String,
    required: true, // 'ok', 'attention_required', 'locked'
  },
});
</script>
```

# ===================================================================
# Файл: src/components/search/FiscalRegisterCard.vue
# ===================================================================

```
<!-- src/components/search/FiscalRegisterCard.vue -->
<template>
  <div
    class="relative bg-black/20 backdrop-blur-sm rounded-lg p-3 text-white/90 h-full flex flex-col cursor-pointer hover:bg-black/30 transition-all duration-200 hover:shadow-lg max-w-sm"
    @click="handleCardClick"
  >
    <card-menu
      :items="menuItems"
      @menuAction="handleMenuAction"
    />
    
    <!-- Заголовок с моделью устройства -->
    <div class="flex justify-between items-center text-sm text-white/60">
      <h3 class="font-bold text-white truncate">{{ entity.data.model_kkt }}</h3>
    </div>
    
    <!-- РНМ (регистрационный номер ККТ) -->
    <div class="mt-2">
      <a :href="serviceDeskLink" target="_blank" class="font-semibold hover:text-sky-400 transition-colors" @click.stop>{{ formattedRnKkt }}</a>
    </div>

    <!-- Серийный номер -->
    <div class="text-sm text-white/70 mt-2">
      <p>Серийный: <span class="font-mono text-white/90">{{ entity.data.fr_serial_number }}</span></p>
    </div>

    <!-- Индикаторы маркировки и акциза -->
    <div class="flex justify-end gap-2 mt-auto pt-3">
      <span
        class="px-2 py-1 rounded text-xs font-medium transition-colors"
        :class="entity.data.is_marking_active ? 'bg-sky-500/30 text-sky-300' : 'bg-red-500/30 text-red-300'"
      >
        Маркировка
      </span>
      <span
        class="px-2 py-1 rounded text-xs font-medium transition-colors"
        :class="entity.data.is_excise_active ? 'bg-purple-500/30 text-purple-300' : 'bg-red-500/30 text-red-300'"
      >
        Акциз
      </span>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue';
import CardMenu from '../common/CardMenu.vue';

const props = defineProps({
  entity: { type: Object, required: true }
});
const emit = defineEmits(['action', 'click']);

const menuItems = [
  { label: 'Редактировать', action: 'edit' },
  { label: 'Удалить', action: 'delete' },
];

const handleMenuAction = (action) => {
  emit('action', action);
};

const handleCardClick = () => {
  emit('click', props.entity);
};

const formattedRnKkt = computed(() => {
  const rn = props.entity.data.rn_kkt || '';
  return rn.replace(/(.{4})/g, '$1 ').trim();
});

const serviceDeskLink = computed(() => `https://myhoreca.itsm365.com/sd/operator/#uuid:${props.entity.data.externalUUID}`);
</script>

<style scoped>
/* No custom styles needed */
</style>
```

# ===================================================================
# Файл: src/components/search/IdDetails.vue
# ===================================================================

```
<!-- src/components/search/IdDetails.vue -->
<template>
  <div class="bg-slate-700/50 rounded-lg p-4">
    <h4 class="text-lg font-semibold text-white mb-3">Идентификаторы</h4>
    <div class="grid grid-cols-1 gap-4 text-sm">
      <InfoField label="Внутренний UUID" :value="data.uuid" :copyable="true" />
      <InfoField label="UUID в ServiceDesk" :value="data.external_uuid" :copyable="true" />
    </div>
  </div>
</template>

<script setup>
import InfoField from './InfoField.vue';
defineProps({ data: Object });
</script>
```

# ===================================================================
# Файл: src/components/search/EntityDetailModal.vue
# ===================================================================

```
<!-- src/components/search/EntityDetailModal.vue -->
<template>
  <base-modal :show="show" @close="$emit('close')">
    <!-- ... (секция #header без изменений) ... -->
    <template #header>
      <div class="flex justify-between items-center">
        <h3 class="text-lg font-semibold text-white">{{ modalTitle }}</h3>
        <button @click="$emit('close')" class="text-gray-400 hover:text-white text-2xl leading-none">&times;</button>
      </div>
    </template>

    <!-- Основной контент -->
    <template #body v-if="entity && entity.data">
      <div class="space-y-6">
        
        <!-- =============================================================== -->
        <!-- == БЛОК: ИНФОРМАЦИЯ О ПРОБЛЕМАХ (STATUS_DETAILS) == -->
        <!-- =============================================================== -->
        <div v-if="entity.data.health_status !== 'ok' && entity.data.status_details" class="p-4 bg-yellow-900/40 border border-yellow-500/30 rounded-lg">
          <!-- ... (внутренняя часть этого блока без изменений) ... -->
          <div class="flex items-center gap-3 mb-3">
            <StatusIcon status="attention_required" />
            <h4 class="text-lg font-semibold text-yellow-300">Требуется внимание</h4>
          </div>
          
          <div v-if="entity.data.status_details.type === 'duplicate_found'">
            <p class="text-sm text-yellow-200">
              Обнаружен конфликт данных. Сущность с полем 
              <span class="font-mono bg-black/20 px-1 rounded">{{ entity.data.status_details.field }}</span>
              и значением 
              <span class="font-mono bg-black/20 px-1 rounded">{{ entity.data.status_details.value }}</span>
              уже существует у других клиентов:
            </p>
            <ul class="mt-3 space-y-2 text-xs">
              <li v-for="dup in entity.data.status_details.duplicates" :key="dup.internal_id" class="p-2 bg-black/20 rounded-md">
                <p class="font-semibold text-white">{{ dup.owner_info.title }}</p>
                <a :href="getServiceDeskLink(dup.external_id)" target="_blank" class="text-sky-400 hover:underline break-all">
                  {{ dup.external_id }}
                </a>
                <p class="text-white/60">Последнее изменение: {{ formatDate(dup.last_modified_date) }}</p>
              </li>
            </ul>
          </div>

          <div v-else>
             <p class="text-sm text-yellow-200 mb-2">Детали проблемы:</p>
             <pre class="text-xs bg-black/30 p-2 rounded-md whitespace-pre-wrap font-mono">{{ JSON.stringify(entity.data.status_details, null, 2) }}</pre>
          </div>
        </div>

        <!-- =============================================================== -->
        <!-- == БЛОК: СЕРВЕР (Server) == -->
        <!-- =============================================================== -->
        <!-- ИСПРАВЛЕНИЕ ЗДЕСЬ: используем props.entity.entity_type -->
        <div v-if="props.entity.entity_type === 'Server'" class="space-y-4">
          <div class="bg-slate-700/50 rounded-lg p-4">
            <h4 class="text-lg font-semibold text-white mb-3">Информация о сервере</h4>
            <div class="grid grid-cols-1 md:grid-cols-2 gap-4 text-sm">
              <InfoField label="Имя устройства" :value="entity.data.device_name" />
              <InfoField label="IP / Домен" :value="entity.data.ip" :copyable="true" />
              <InfoField label="Имя сервера iiko" :value="entity.data.server_name" />
              <InfoField label="Версия" :value="entity.data.server_version" />
              <InfoField label="Редакция" :value="entity.data.server_edition" />
              <InfoField label="Уникальный ID iiko" :value="entity.data.unique_id" />
              <InfoField label="Последний опрос" :value="formatDate(entity.data.last_polled_at)" />
              <InfoField label="Операционный статус" :value="entity.data.operational_status" />
            </div>
          </div>
          <ConnectionDetails :data="entity.data" />
          <IdDetails :data="entity.data" />
        </div>

        <!-- =============================================================== -->
        <!-- == БЛОК: РАБОЧАЯ СТАНЦИЯ (Workstation) == -->
        <!-- =============================================================== -->
        <!-- ИСПРАВЛЕНИЕ ЗДЕСЬ: используем props.entity.entity_type -->
        <div v-else-if="props.entity.entity_type === 'Workstation'" class="space-y-4">
          <div class="bg-slate-700/50 rounded-lg p-4">
            <h4 class="text-lg font-semibold text-white mb-3">Информация о рабочей станции</h4>
             <div class="grid grid-cols-1 md:grid-cols-2 gap-4 text-sm">
                <InfoField label="Имя устройства" :value="entity.data.device_name" />
             </div>
          </div>
          <ConnectionDetails :data="entity.data" />
          <IdDetails :data="entity.data" />
        </div>

        <!-- =============================================================== -->
        <!-- == БЛОК: ФИСКАЛЬНЫЙ РЕГИСТРАТОР (FiscalRegister) == -->
        <!-- =============================================================== -->
        <!-- ИСПРАВЛЕНИЕ ЗДЕСЬ: используем props.entity.entity_type -->
        <div v-else-if="props.entity.entity_type === 'FiscalRegister'" class="space-y-4">
           <div class="bg-slate-700/50 rounded-lg p-4">
            <h4 class="text-lg font-semibold text-white mb-3">Информация о ККТ</h4>
            <div class="grid grid-cols-1 md:grid-cols-2 gap-4 text-sm">
                <InfoField label="Модель ККТ" :value="entity.data.model_kkt" />
                <InfoField label="Рег. номер (РН ККТ)" :value="entity.data.rn_kkt" :copyable="true" />
                <InfoField label="Заводской номер" :value="entity.data.serial_number" />
                <InfoField label="Номер ФН" :value="entity.data.fn_number" />
                <InfoField label="Регистрация ФН" :value="formatDate(entity.data.fn_registration_date)" />
                <InfoField label="Срок действия ФН" :value="formatDate(entity.data.fn_expire_date)" />
                <InfoField label="Прошивка ККТ" :value="entity.data.fr_firmware" />
                <InfoField label="Версия драйвера" :value="entity.data.driver_version" />
                <InfoField label="Организация" :value="entity.data.organization_name" />
                <InfoField label="ИНН" :value="entity.data.inn" />
            </div>
          </div>
           <IdDetails :data="entity.data" />
        </div>
      </div>
    </template>
    
    <!-- ... (секция #footer без изменений) ... -->
    <template #footer>
      <div class="flex justify-between items-center">
        <!-- Ссылки -->
        <div>
          <a v-if="entity && entity.data.external_uuid" :href="getServiceDeskLink(entity.data.external_uuid)" target="_blank" class="action-btn bg-gray-600 hover:bg-gray-700">
            Service Desk
          </a>
           <a v-if="entity && entity.data.partners_link" :href="entity.data.partners_link" target="_blank" class="action-btn bg-blue-600 hover:bg-blue-700 ml-2">
            Портал партнера
          </a>
        </div>
        <!-- Кнопки управления -->
        <div class="flex gap-2">
          <button @click="$emit('edit', entity)" class="action-btn bg-indigo-600 hover:bg-indigo-700">Редактировать</button>
          <button @click="$emit('delete', entity)" class="action-btn bg-red-600 hover:bg-red-700">Удалить</button>
          <button @click="$emit('close')" class="action-btn bg-slate-600 hover:bg-slate-500">Закрыть</button>
        </div>
      </div>
    </template>
  </base-modal>
</template>

<script setup>
import { computed } from 'vue';
import BaseModal from '@/components/common/BaseModal.vue';
import StatusIcon from '@/components/common/StatusIcon.vue';
import InfoField from './InfoField.vue'; // Вспомогательный компонент
import ConnectionDetails from './ConnectionDetails.vue'; // Вспомогательный компонент
import IdDetails from './IdDetails.vue'; // Вспомогательный компонент

const props = defineProps({
  show: { type: Boolean, default: false },
  entity: { type: Object, default: null },
  entityType: { type: String, required: true } // 'server', 'workstation', 'fiscal-register'
});

defineEmits(['close', 'edit', 'delete']);

const modalTitle = computed(() => {
  if (!props.entity || !props.entity.data) return 'Детали объекта';
  const data = props.entity.data;
  switch (props.entity.entity_type) {
    case 'Server': return `Сервер: ${data.device_name || data.ip || 'Без имени'}`;
    case 'Workstation': return `Рабочая станция: ${data.device_name || 'Без имени'}`;
    case 'FiscalRegister': return `ККТ: ${data.model_kkt || data.rn_kkt || 'Без имени'}`;
    default: return 'Детали объекта';
  }
});

const formatDate = (dateString) => {
  if (!dateString) return '—';
  try {
    const date = new Date(dateString);
    // Проверяем, является ли дата валидной
    if (isNaN(date.getTime())) return dateString;
    return date.toLocaleString('ru-RU', {
      year: 'numeric', month: 'numeric', day: 'numeric',
      hour: '2-digit', minute: '2-digit'
    });
  } catch (e) {
    return dateString; // Возвращаем исходную строку, если парсинг не удался
  }
};

const getServiceDeskLink = (uuid) => {
  if (!uuid) return '#';
  // UUID из ServiceDesk часто содержит '$', который нужно закодировать
  const encodedUuid = encodeURIComponent(uuid);
  return `https://myhoreca.itsm365.com/sd/operator/#uuid:${encodedUuid}`;
};

</script>

<style scoped>
.action-btn {
  @apply inline-flex items-center px-4 py-2 text-white text-sm rounded-md transition-colors;
}
</style>
```

# ===================================================================
# Файл: src/components/search/ConnectionDetails.vue
# ===================================================================

```
<!-- src/components/search/ConnectionDetails.vue -->
<template>
  <div v-if="hasConnectionDetails" class="bg-slate-700/50 rounded-lg p-4">
    <h4 class="text-lg font-semibold text-white mb-3">Удаленный доступ</h4>
    <div class="grid grid-cols-1 md:grid-cols-2 gap-4 text-sm">
      <InfoField label="AnyDesk" :value="data.anydesk" :copyable="true" />
      <InfoField label="TeamViewer" :value="data.teamviewer" :copyable="true" />
      <InfoField label="LiteManager" :value="data.litemanager" :copyable="true" />
      <InfoField label="RDP" :value="data.rdp" :copyable="true" />
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue';
import InfoField from './InfoField.vue';

const props = defineProps({ data: Object });

const hasConnectionDetails = computed(() => {
  return props.data.anydesk || props.data.teamviewer || props.data.litemanager || props.data.rdp;
});
</script>
```

# ===================================================================
# Файл: src/components/search/InfoField.vue
# ===================================================================

```
<!-- src/components/search/InfoField.vue -->
<template>
  <!-- Оборачиваем все в единый, безусловный корневой div -->
  <div class="flex flex-col">
    <label class="text-xs text-white/60 mb-1">{{ label }}</label>
    
    <!-- Условная логика теперь находится внутри этого корневого элемента -->
    <template v-if="value">
      <div class="flex items-center gap-2">
        <span class="text-white font-mono break-all">{{ value }}</span>
        <button v-if="copyable" @click="copy(value)" class="text-xs p-1 bg-white/10 text-white/80 rounded hover:bg-white/20 transition-colors" title="Копировать">
          {{ copied ? '✓' : '📋' }}
        </button>
      </div>
    </template>
    <template v-else>
      <span class="text-white/70">—</span>
    </template>
  </div>
</template>

<script setup>
import { ref } from 'vue';
defineProps({
  label: String,
  value: [String, Number],
  copyable: { type: Boolean, default: false }
});

const copied = ref(false);
const copy = async (text) => {
  if (!text) return;
  try {
    await navigator.clipboard.writeText(String(text)); // Приводим к строке на всякий случай
    copied.value = true;
    setTimeout(() => { copied.value = false; }, 2000);
  } catch (err)
 {
    console.error('Failed to copy text: ', err);
  }
};
</script>
```

# ===================================================================
# Файл: src/components/search/CompanyCard.vue
# ===================================================================

```
<!-- src/components/search/CompanyCard.vue -->
<template>
  <div class="bg-slate-800/60 backdrop-blur-xl rounded-2xl p-4 text-white shadow-lg transition-all duration-200 hover:shadow-xl hover:bg-slate-700/60 flex flex-col gap-3" :class="cardWidthClass">
    <!-- Заголовок карточки -->
    <div class="flex justify-between items-start gap-3">
      <div class="min-w-0 flex-1">
        <h2 class="text-lg font-bold truncate" :title="group.owner.name">{{ sanitizedOwnerName }}</h2>
        <p v-if="group.owner.parent_info" class="text-xs text-white/50 mt-1">
          входит в группу:
          <a
            href="#"
            @click.prevent="searchByParent"
            class="text-sky-400 hover:text-sky-300 hover:underline"
            :title="`Искать по '${sanitizedParentName}'`"
          >
            {{ sanitizedParentName }}
          </a>
        </p>
        <p v-else class="text-sm text-white/60 mt-1 truncate" :title="group.owner.address">
          {{ group.owner.address || 'Адрес не указан' }}
        </p>
      </div>
      <div class="flex-shrink-0">
        <div 
          class="w-3 h-3 rounded-full" 
          :class="group.owner.active_contract ? 'bg-green-500' : 'bg-gray-500'" 
          :title="group.owner.active_contract ? 'Активный контракт' : 'Контракт неактивен'">
        </div>
      </div>
    </div>
    
    <!-- Разделитель и счетчик -->
    <div class="pt-3 border-t border-white/10 flex items-center justify-between text-xs text-white/60">
      <span>Найдено объектов: {{ group.found_entities.length }}</span>
    </div>

    <!-- Список найденных сущностей -->
    <div v-if="group.found_entities.length > 0" class="space-y-2 max-h-80 2xl:max-h-96 overflow-y-auto pr-2">
      <FoundEntityItem
        v-for="entity in sortedEntities"
        :key="entity.data.uuid"
        :entity="entity"
        @details="openEntityDetails"
        @request-license-install="(server) => emit('request-license-install', server)"
      />
    </div>
    <div v-else class="text-center text-sm text-white/50 py-4">
      Совпадений по оборудованию не найдено.
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue';
import FoundEntityItem from './FoundEntityItem.vue';

const props = defineProps({
  group: { type: Object, required: true },
  columns: { type: Number, default: 2 },
});
const emit = defineEmits(['openEntity', 'setSearchTerm', 'request-license-install']);

// Вычисляем классы для ширины карточки, чтобы она работала во flex-контейнере
const cardWidthClass = computed(() => {
  // gap-8 = 2rem. Расчет: (100% / кол-во колонок) - (общий_пробел / кол-во_колонок)
  if (props.columns === 3) {
    // 2rem * 2 / 3 = 1.333rem
    return 'w-full lg:basis-[calc(50%-1rem)] 2xl:basis-[calc(33.333%-1.34rem)]';
  }
  // 2rem * 1 / 2 = 1rem
  return 'w-full lg:basis-[calc(50%-1rem)]';
});

const sortedEntities = computed(() => {
  return [...props.group.found_entities].sort((a, b) => {
    const typeOrder = { 'Server': 1, 'Workstation': 2, 'FiscalRegister': 3 };
    const typeA = typeOrder[a.entity_type] || 99;
    const typeB = typeOrder[b.entity_type] || 99;
    if (typeA !== typeB) return typeA - typeB;
    const nameA = a.data.device_name || a.data.model_kkt || '';
    const nameB = b.data.device_name || b.data.model_kkt || '';
    return nameA.localeCompare(nameB);
  });
});

const sanitizeName = (name) => {
  if (!name) return '';
  return name.replace(/[^\p{L}\p{N}\s-]/gu, '').trim();
};

const sanitizedOwnerName = computed(() => sanitizeName(props.group.owner.name));
const sanitizedParentName = computed(() => props.group.owner.parent_info ? sanitizeName(props.group.owner.parent_info.name) : '');

const searchByParent = () => {
  if (sanitizedParentName.value) {
    emit('setSearchTerm', sanitizedParentName.value);
  }
};

const openEntityDetails = (entity) => {
  let entityType;
  switch (entity.entity_type) {
    case 'Server': entityType = 'server'; break;
    case 'Workstation': entityType = 'workstation'; break;
    case 'FiscalRegister': entityType = 'fiscal-register'; break;
    default: entityType = 'unknown';
  }
  emit('openEntity', { entity, entityType });
};
</script>

<style scoped>
.overflow-y-auto::-webkit-scrollbar { width: 6px; }
.overflow-y-auto::-webkit-scrollbar-track { background: transparent; }
.overflow-y-auto::-webkit-scrollbar-thumb { background-color: rgba(148, 163, 184, 0.4); border-radius: 20px; border: 3px solid transparent; }
.overflow-y-auto::-webkit-scrollbar-thumb:hover { background-color: rgba(148, 163, 184, 0.6); }
</style>
```

# ===================================================================
# Файл: src/components/search/FoundEntityItem.vue
# ===================================================================

```
<!-- src/components/search/FoundEntityItem.vue -->
<template>
  <div
    class="relative bg-slate-700/40 rounded-lg p-3 transition-all duration-200 flex items-center gap-3"
    :class="{ 
      'opacity-50 pointer-events-none select-none': isLocked,
      'hover:bg-slate-700/80': !isLocked 
    }"
  >
    <!-- Иконка статуса "здоровья" -->
    <div class="w-5 h-5 flex-shrink-0 flex items-center justify-center">
      <StatusIcon v-if="entity.data.health_status !== 'ok'" :status="entity.data.health_status" :title="tooltipText" />
      <StatusIcon v-else-if="showFiscalOkIcon" status="ok" title="Прошивка и загрузчик ККТ в порядке" />
    </div>

    <!-- Основная информация -->
    <div class="flex-1 min-w-0">
      <p class="font-semibold text-white truncate">{{ entityTitle }}</p>
      
      <!-- Подзаголовок с кнопками -->
      <div class="text-sm text-white/60 flex items-center gap-2 mt-1 flex-wrap">
        <!-- Кнопки для Сервера -->
        <template v-if="entity.entity_type === 'Server'">
          <CopyBadge v-if="entity.data.ip" :textToCopy="entity.data.ip" label="URL" />
          <span v-if="entity.data.server_version" class="text-xs px-2 py-0.5 bg-black/20 rounded-full">
            v{{ entity.data.server_version }}
          </span>
          <!-- Динамическая кнопка-ссылка iikoWeb/Syrve -->
          <a v-if="specialLink" :href="specialLink.href" target="_blank" class="special-link-btn">
            {{ specialLink.label }}
          </a>
        </template>
        <!-- Кнопки для Рабочей станции -->
        <template v-if="entity.entity_type === 'Workstation'">
          <CopyBadge v-if="entity.data.anydesk" :textToCopy="entity.data.anydesk" label="AnyDesk"/>
          <CopyBadge v-if="entity.data.teamviewer" :textToCopy="entity.data.teamviewer" label="TeamViewer"/>
        </template>
        <!-- Инфо для ККТ -->
        <span v-if="entity.entity_type === 'FiscalRegister'" class="font-mono text-xs">{{ entity.data.rn_kkt || 'Нет рег. номера' }}</span>
      </div>
    </div>

    <!-- Индикатор статуса для серверов -->
    <div v-if="entity.entity_type === 'Server'" class="flex-shrink-0 ml-auto">
      <div 
        class="flex items-center gap-1.5 px-2 py-1 rounded-full text-xs cursor-pointer transition-colors" 
        :class="[operationalStatusClass, { 'hover:ring-2 hover:ring-offset-2 hover:ring-offset-slate-800 ring-sky-400': isLicenseActionAvailable }]"
        @click.stop="onRequestLicenseInstall"
        title="Нажмите, чтобы управлять лицензией"
      >
        <div class="w-2 h-2 rounded-full bg-current"></div>
        <span>{{ entity.data.operational_status }}</span>
      </div>
    </div>
    
    <!-- Кнопка "Подробнее" -->
    <button 
      class="text-xs text-sky-400 hover:text-sky-300 ml-2 flex-shrink-0 z-10" 
      :class="{ 'text-gray-400': isLocked }"
      @click.stop="$emit('details', entity)"
    >
      {{ isLocked ? '(забл.)' : 'Подробнее →' }}
    </button>
  </div>
</template>

<script setup>
import { computed } from 'vue';
import StatusIcon from '@/components/common/StatusIcon.vue';
import CopyBadge from './CopyBadge.vue';

const props = defineProps({
  entity: { type: Object, required: true },
});
const emit = defineEmits(['details', 'request-license-install']);

// Логика для динамической ссылки iikoWeb/Syrve
const specialLink = computed(() => {
  if (props.entity.entity_type !== 'Server') return null;

  const data = props.entity.data;
  for (const key in data) {
    const value = data[key];
    if (typeof value === 'string') {
      let label = null;
      if (value.includes('iikoweb.ru')) label = 'iikoWeb';
      if (value.includes('syrve.app')) label = 'SyrveApp';

      if (label) {
        // Добавляем 'https://' если протокола нет
        const fullUrl = value.startsWith('http') ? value : `https://${value}`;
        // Очищаем от порта с помощью объекта URL
        try {
          const urlObject = new URL(fullUrl);
          urlObject.port = ''; // Удаляем порт
          return {
            href: urlObject.href,
            label: label,
          };
        } catch (e) {
          // Если URL невалидный, возвращаем как есть, но без порта
          const cleanedUrl = fullUrl.replace(/:\d+/, '');
          return { href: cleanedUrl, label: label };
        }
      }
    }
  }
  return null;
});

const isLocked = computed(() => props.entity.data.health_status === 'locked');
const isLicenseActionAvailable = computed(() => {
  const status = props.entity.data.operational_status;
  return props.entity.entity_type === 'Server' && (status === 'active' || status === 'license');
});
const showFiscalOkIcon = computed(() => {
  return props.entity.entity_type === 'FiscalRegister' && props.entity.data.fr_downloader && props.entity.data.fr_firmware;
});

const entityTitle = computed(() => {
  const data = props.entity.data;
  switch (props.entity.entity_type) {
    case 'Server': return data.device_name || 'Сервер без имени';
    case 'Workstation': return data.device_name || 'Рабочая станция';
    case 'FiscalRegister': return data.model_kkt || 'ККТ без модели';
    default: return 'Неизвестный объект';
  }
});

const tooltipText = computed(() => {
  const health = props.entity.data.health_status;
  if (health === 'attention_required' && props.entity.data.status_details) {
    const details = props.entity.data.status_details;
    if (details.type === 'duplicate_found') {
      return `Обнаружен дубликат по полю "${details.field}" со значением "${details.value}".`;
    }
    return 'Требуется внимание. Нажмите "Подробнее".';
  }
  if (health === 'locked') {
    return 'Данные заблокированы из-за конфликта.';
  }
  return 'Статус данных: ОК';
});

const operationalStatusClass = computed(() => {
  const status = props.entity.data.operational_status;
  const statusMap = {
    active: 'bg-green-500/20 text-green-300',
    offline: 'bg-gray-500/20 text-gray-300',
    starting: 'bg-blue-500/20 text-blue-300 animate-pulse',
    license: 'bg-red-500/20 text-red-300',
    unknown: 'bg-yellow-500/20 text-yellow-300',
    undefined: 'bg-gray-500/20 text-gray-300',
    archived: 'bg-purple-500/20 text-purple-300',
  };
  return statusMap[status] || 'bg-gray-500/20 text-gray-300';
});

const onRequestLicenseInstall = () => {
  if (isLicenseActionAvailable.value) {
    emit('request-license-install', props.entity);
  }
};
</script>

<style scoped>
.special-link-btn {
  @apply inline-block bg-sky-500/20 text-sky-300 text-xs font-semibold px-2.5 py-1 rounded-full hover:bg-sky-500/40 transition-colors;
}
</style>
```

# ===================================================================
# Файл: src/components/search/CopyBadge.vue
# ===================================================================

```
<!-- src/components/search/CopyBadge.vue -->
<template>
    <div class="flex items-center gap-1 bg-black/20 rounded-full text-xs font-mono pl-2">
        <span class="text-white/70">{{ label }}:</span>
        <span class="text-white">{{ textToCopy }}</span>
        <button 
            @click.stop="copy" 
            class="p-1.5 rounded-full hover:bg-white/20 transition-colors"
            :title="`Копировать ${textToCopy}`"
        >
            <span v-if="copied" class="text-green-400">✓</span>
            <span v-else>📋</span>
        </button>
    </div>
</template>

<script setup>
import { ref } from 'vue';

const props = defineProps({
    textToCopy: { type: String, required: true },
    label: { type: String, default: '' }
});

const copied = ref(false);

const copy = async () => {
  if (!props.textToCopy) return;
  try {
    await navigator.clipboard.writeText(props.textToCopy);
    copied.value = true;
    setTimeout(() => { copied.value = false; }, 2000);
  } catch (err) {
    console.error('Failed to copy text: ', err);
  }
};
</script>
```

# ===================================================================
# Файл: src/components/search/JokeCard.vue
# ===================================================================

```
<!-- src/components/search/JokeCard.vue -->
<template>
  <div class="bg-slate-800/60 backdrop-blur-xl rounded-2xl p-4 text-white shadow-lg flex flex-col gap-3 h-full">
    <div class="flex justify-between items-start gap-3">
      <h2 class="text-lg font-bold text-sky-400">{{ joke.title }}</h2>
      <button 
        @click="refreshJoke" 
        class="p-2 rounded-full text-white/60 hover:bg-slate-700/80 hover:text-white transition-colors"
        title="Другой анекдот"
      >
        <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
          <path stroke-linecap="round" stroke-linejoin="round" d="M4 4v5h5M20 20v-5h-5M20 4h-5v5M4 20h5v-5" />
          <path stroke-linecap="round" stroke-linejoin="round" d="M20 12a8 8 0 11-8-8" />
        </svg>
      </button>
    </div>
    <div class="pt-3 border-t border-white/10 flex-1">
      <p class="text-white/80 whitespace-pre-line">{{ joke.description }}</p>
    </div>
  </div>
</template>

<script setup>
import { useSearchStore } from '@/store/search';

defineProps({
  joke: { type: Object, required: true },
});

const searchStore = useSearchStore();

const refreshJoke = () => {
  searchStore.pickNewRandomJoke();
};
</script>
```

# ===================================================================
# Файл: src/components/servers/LicenseInstallModal.vue
# ===================================================================

```
<!-- src/components/servers/LicenseInstallModal.vue -->
<template>
  <BaseModal :show="show" @close="$emit('close')">
    <template #header>
      <h3 class="text-lg font-semibold text-white">Установка лицензии</h3>
    </template>
    
    <template #body v-if="server && server.data">
      <p class="text-sm text-white/70 mb-4">
        Сервер: <span class="font-medium text-white">{{ server.data.device_name }}</span>
      </p>
      <form @submit.prevent="submit" class="space-y-4">
        <div>
          <label for="uniqueId" class="block text-sm font-medium text-gray-300">Unique ID</label>
          <input v-model="form.unique_id" id="uniqueId" type="text" required class="form-input" />
        </div>
        <div>
          <label for="login" class="block text-sm font-medium text-gray-300">Логин</label>
          <input v-model="form.login" id="login" type="text" required class="form-input" />
        </div>
        <div>
          <label for="password" class="block text-sm font-medium text-gray-300">Пароль</label>
          <input v-model="form.password" id="password" type="password" required class="form-input" />
        </div>
      </form>
      <p v-if="error" class="text-red-400 text-sm mt-3">{{ error }}</p>
    </template>
    
    <template #footer>
      <div class="flex justify-end gap-2">
        <button @click="$emit('close')" type="button" class="btn bg-slate-600 hover:bg-slate-500">Отмена</button>
        <button @click="submit" type="submit" class="btn bg-sky-600 hover:bg-sky-500" :disabled="isLoading">
          {{ isLoading ? 'Установка...' : 'Установить' }}
        </button>
      </div>
    </template>
  </BaseModal>
</template>

<script setup>
import { ref, watch } from 'vue';
import BaseModal from '@/components/common/BaseModal.vue';
import { installLicense } from '@/services/api';

const props = defineProps({
  show: Boolean,
  server: Object,
});
const emit = defineEmits(['close', 'submitted']);

const form = ref({
  unique_id: '',
  login: '',
  password: '',
});
const isLoading = ref(false);
const error = ref(null);

watch(() => props.server, (newServer) => {
  if (newServer && newServer.data) {
    form.value.unique_id = newServer.data.unique_id || '';
    form.value.login = '';
    form.value.password = '';
    error.value = null;
  }
});

async function submit() {
  if (isLoading.value || !props.server) return;
  isLoading.value = true;
  error.value = null;
  try {
    await installLicense(props.server.data.uuid, form.value);
    emit('submitted', props.server.data.uuid);
    emit('close');
  } catch (err) {
    error.value = err.response?.data?.error || 'Произошла ошибка при отправке запроса.';
    console.error(err);
  } finally {
    isLoading.value = false;
  }
}
</script>

<style scoped>
.form-input {
  @apply mt-1 block w-full bg-slate-700 border border-slate-600 rounded-md shadow-sm py-2 px-3 text-white focus:outline-none focus:ring-sky-500 focus:border-sky-500 sm:text-sm;
}
.btn {
  @apply px-4 py-2 text-sm font-medium rounded-md transition-colors;
}
</style>
```

# ===================================================================
# Файл: src/components/tasks/TaskItem.vue
# ===================================================================

```
<!-- src/components/tasks/TaskItem.vue -->
<template>
  <div @click="$emit('select')" class="bg-slate-800/60 p-4 rounded-lg cursor-pointer hover:bg-slate-700/80 transition-colors">
    <div class="flex justify-between items-center">
      <span class="text-xs text-white/50">#{{ task.id }}</span>
      
      <!-- Отображение статуса -->
      <div class="flex items-center gap-1.5" :title="statusTitle">
        <!-- Иконка в ожидании -->
        <svg v-if="task.status === 'pending_sd_action'" class="w-4 h-4 text-amber-400 animate-spin" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
          <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
          <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
        </svg>

        <!-- Иконка ошибки -->
        <svg v-if="task.status === 'sd_error'" class="w-4 h-4 text-red-400" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor">
          <path fill-rule="evenodd" d="M18 10a8 8 0 11-16 0 8 8 0 0116 0zm-7 4a1 1 0 11-2 0 1 1 0 012 0zm-1-9a1 1 0 00-1 1v4a1 1 0 102 0V6a1 1 0 00-1-1z" clip-rule="evenodd" />
        </svg>
        
        <span class="px-2 py-0.5 text-xs rounded-full" :class="statusClass">
          {{ task.status }}
        </span>
      </div>
    </div>
    <div class="mt-2">
      <p class="font-semibold">{{ task.task_type }}</p>
      <p class="text-sm text-white/70">{{ task.entity_repr }}</p>
    </div>
    <div class="mt-3 text-xs text-white/50">
      {{ new Date(task.created_at).toLocaleString() }}
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue';

const props = defineProps({ task: Object });
defineEmits(['select']);

const statusClass = computed(() => ({
  'new': 'bg-blue-500/30 text-blue-300',
  'resolved': 'bg-green-500/30 text-green-300',
  'pending_sd_action': 'bg-amber-500/30 text-amber-300',
  'sd_error': 'bg-red-500/30 text-red-300',
}[props.task.status] || 'bg-gray-500/30 text-gray-300'));

const statusTitle = computed(() => {
    const titleMap = {
        pending_sd_action: 'Операция в ServiceDesk выполняется...',
        sd_error: 'Произошла ошибка в ServiceDesk',
    };
    return titleMap[props.task.status] || props.task.status;
});
</script>
```

# ===================================================================
# Файл: src/components/tasks/NeedUpdateDiff.vue
# ===================================================================

```
<!-- src/components/tasks/NeedUpdateDiff.vue -->
<template>
  <div class="space-y-3 text-sm">
    <div class="grid grid-cols-3 gap-4 font-semibold border-b border-slate-700 pb-2 text-white/60">
      <div>Поле</div>
      <div>Эталонное значение</div>
      <div>Значение в ServiceDesk</div>
    </div>
    <div 
      v-for="(values, key) in details" 
      :key="key"
      class="grid grid-cols-3 gap-4 items-center"
    >
      <div class="text-white/70 font-mono">{{ key }}</div>
      <div class="bg-slate-700/50 p-2 rounded break-words text-white/90">{{ formatValue(key, values.etalon_value) }}</div>
      <div class="bg-amber-900/40 p-2 rounded break-words text-amber-200">{{ formatValue(key, values.service_desk_value) }}</div>
    </div>
  </div>
</template>

<script setup>
const props = defineProps({ details: Object });

const formatValue = (key, value) => {
  if (!value) return 'пусто';
  if (typeof key === 'string' && key.toLowerCase().includes('date')) {
    try {
      return new Date(value).toLocaleDateString('ru-RU');
    } catch (e) {
      return 'Invalid Date';
    }
  }
  return value;
};
</script>
```

# ===================================================================
# Файл: src/components/tasks/DataConflictDetails.vue
# ===================================================================

```
<!-- src/components/tasks/DataConflictDetails.vue -->
<template>
  <div class="flex flex-col gap-4">
    <!-- Сетка с дубликатами -->
    <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
      <div 
        v-for="duplicate in details.duplicates" 
        :key="duplicate.entity.uuid"
        class="relative bg-slate-800/70 p-4 rounded-lg cursor-pointer transition-all duration-300"
        :class="{ 
          'ring-2 ring-red-500 shadow-lg shadow-red-500/20': selectedUuid === duplicate.entity.uuid,
          'hover:bg-slate-700': selectedUuid !== duplicate.entity.uuid
        }"
        @click="selectForDeletion(duplicate.entity.uuid)"
      >
        <!-- Плашка "Будет удалён" -->
        <div 
          v-if="selectedUuid === duplicate.entity.uuid"
          class="absolute -top-2 left-1/2 -translate-x-1/2 bg-red-600 text-white text-xs font-bold px-3 py-1 rounded-full shadow-md"
        >
          Будет удалён
        </div>
        
        <!-- Инфо о владельце -->
        <div class="flex justify-between items-start">
          <div>
            <p class="font-bold text-white">{{ duplicate.owner.name }}</p>
            <p class="text-xs text-white/60 mt-1">{{ duplicate.owner.address }}</p>
          </div>
          <div 
            class="w-3 h-3 rounded-full flex-shrink-0 mt-1" 
            :class="duplicate.owner.active_contract ? 'bg-green-500' : 'bg-gray-500'"
            :title="duplicate.owner.active_contract ? 'Контракт активен' : 'Контракт неактивен'"
          ></div>
        </div>
        
        <hr class="border-white/10 my-3">

        <!-- Инфо о сущности -->
        <div class="text-sm">
          <p class="text-white/70">Сущность:</p>
          <a :href="`https://myhoreca.itsm365.com/sd/operator/#uuid:${duplicate.entity.uuid}`" target="_blank" class="font-semibold text-sky-400 hover:text-sky-300 break-all">
            {{ duplicate.entity.name || duplicate.entity.uuid }}
          </a>
          <p v-if="duplicate.entity.last_modified_date" class="text-xs text-white/60 mt-1">
            Последнее изменение: <span class="font-medium text-white/80">{{ formatDate(duplicate.entity.last_modified_date) }}</span>
          </p>
        </div>
      </div>
    </div>

    <!-- Информация о конфликте (перенесена вниз) -->
    <div class="bg-slate-900/50 p-3 rounded-lg text-center">
      <p class="text-sm text-white/70">
        Конфликт по полю <span class="font-mono text-amber-300">{{ details.conflicting_field }}</span> 
        со значением <span class="font-mono text-amber-300">{{ details.conflicting_value }}</span>
      </p>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue';

defineProps({ details: Object });
const emit = defineEmits(['selection-change']);

const selectedUuid = ref(null);

const selectForDeletion = (uuid) => {
  // Позволяем "отжать" выбор, если кликнуть на уже выбранный элемент
  if (selectedUuid.value === uuid) {
    selectedUuid.value = null;
  } else {
    selectedUuid.value = uuid;
  }
  // Сообщаем родителю о выборе
  emit('selection-change', selectedUuid.value);
};

const formatDate = (dateString) => {
  if (!dateString) return 'Н/Д';
  try {
    return new Date(dateString).toLocaleString('ru-RU');
  } catch (e) {
    return dateString;
  }
};
</script>
```

# ===================================================================
# Файл: src/components/tasks/TaskDetailsModal.vue
# ===================================================================

```
<!-- src/components/tasks/TaskDetailsModal.vue -->
<template>
  <BaseModal :show="!!task" @close="$emit('close')">
    <template #header>
      <!-- Улучшенный заголовок -->
      <div class="text-white">
        <div class="flex justify-between items-start">
          <div>
            <p class="text-lg">
              <span class="font-normal text-white/70">Задача #{{ task?.id }}:</span>
              <span class="ml-2 font-semibold">{{ task?.task_type }}</span>
            </p>
            <!-- Комментарий показываем для всех, КРОМЕ конфликта-дубликата -->
            <p v-if="!isDuplicateConflict && task?.comment" class="text-sm text-white/80 mt-2" v-html="formattedComment"></p>
          </div>
          <button @click="$emit('close')" class="text-gray-400 hover:text-white text-2xl leading-none ml-4">&times;</button>
        </div>
      </div>
    </template>
    
    <template #body v-if="task">
      <!-- Основной контент -->
      <div>
        <!-- Конфликт-дубликат -->
        <div v-if="isDuplicateConflict">
          <DataConflictDetails :details="task.details" @selection-change="onDuplicateSelection" />
        </div>
        <!-- Конфликт-сверка -->
        <div v-else-if="isDataConflict">
          <DataConflictDiff :details="task.details" @resolve="handleResolveDataConflict" />
        </div>
        <!-- Обновление данных -->
        <div v-else-if="task.task_type === 'need_update'">
          <NeedUpdateDiff :details="task.details" />
        </div>
        <!-- Новый клиент -->
        <div v-else-if="task.task_type === 'new_client'">
          <NewClientDetails :details="task.details.agent_data" />
        </div>
        <!-- Отображение JSON для остальных -->
        <div v-else class="text-sm text-white/80 whitespace-pre-wrap font-mono bg-slate-900/50 p-3 rounded-lg">
          {{ JSON.stringify(task.details, null, 2) }}
        </div>
      </div>

      <!-- Отображение ошибки SD -->
      <div v-if="task.status === 'sd_error'" class="mt-4 p-3 bg-red-900/40 text-red-300 border border-red-500/30 rounded-md text-sm">
        <p class="font-semibold mb-1">Ошибка операции в ServiceDesk:</p>
        <p class="font-mono text-xs">{{ task.comment }}</p>
      </div>
    </template>
    
    <template #footer v-if="task">
      <div class="w-full flex items-center justify-between">
        <!-- Блок с кнопками слева -->
        <div>
          <button v-if="task.task_type === 'add_equipment'" @click="handleCreateEntity" :disabled="isActionDisabled" class="action-btn bg-sky-600 hover:bg-sky-500">
            {{ isLoading ? 'Отправка...' : 'Создать в ServiceDesk' }}
          </button>
          <button v-if="task.task_type === 'need_update'" @click="handleUpdateEntity" :disabled="isActionDisabled" class="action-btn bg-sky-600 hover:bg-sky-500">
            {{ isLoading ? 'Отправка...' : 'Обновить в ServiceDesk' }}
          </button>
          
          <!-- Кнопка "Отложить" для конфликта-дубликата -->
          <button v-if="isDuplicateConflict" @click="handleSleepTask" :disabled="isActionDisabled" class="action-btn bg-amber-600 hover:bg-amber-500">
            {{ isLoading ? 'Отправка...' : 'Отложить' }}
          </button>
        </div>
        
        <!-- Блок с кнопками справа -->
        <div>
          <!-- Новая кнопка "Применить" для дубликатов -->
          <button v-if="isDuplicateConflict" @click="handleDeleteDuplicate" :disabled="!selectedDuplicateUuid || isActionDisabled" class="action-btn bg-green-600 hover:bg-green-500 mr-2">
             Применить
          </button>

          <button v-if="!isDataConflict" @click="$emit('close')" class="px-4 py-2 bg-slate-700 rounded-md hover:bg-slate-600">Закрыть</button>
        </div>
      </div>

       <div v-if="error" class="w-full text-left mt-2 p-2 bg-red-500/20 text-red-300 border border-red-500/30 rounded-md text-xs">
        {{ error }}
      </div>
    </template>
  </BaseModal>
</template>

<script setup>
import { ref, computed } from 'vue';
import BaseModal from '@/components/common/BaseModal.vue';
import DataConflictDetails from './DataConflictDetails.vue';
import DataConflictDiff from './DataConflictDiff.vue';
import NeedUpdateDiff from './NeedUpdateDiff.vue';
import NewClientDetails from './NewClientDetails.vue';
import { 
  createEntityInServiceDesk, 
  resolveTaskUpdateInSd,
  resolveTaskAsSleep,
  resolveTaskDeleteDuplicate,
  resolveTaskDataConflict
} from '@/services/api';
import { useTasksStore } from '@/store/tasks';

const props = defineProps({ task: Object });
const emit = defineEmits(['close']);

const tasksStore = useTasksStore();
const isLoading = ref(false);
const error = ref(null);
const selectedDuplicateUuid = ref(null); // Локальное состояние для выбора

// Обработчик события от дочернего компонента
const onDuplicateSelection = (uuid) => {
  selectedDuplicateUuid.value = uuid;
};

// Форматируем комментарий, заменяя UUID на ссылки
const formattedComment = computed(() => {
  if (!props.task?.comment) return '';
  const uuidRegex = /(ou\$[a-zA-Z0-9]+|objectBase\$[a-zA-Z0-9]+)/g;
  return props.task.comment.replace(uuidRegex, (uuid) => {
    const url = `https://myhoreca.itsm365.com/sd/operator/#uuid:${uuid}`;
    return `<a href="${url}" target="_blank" class="text-sky-400 hover:text-sky-300 underline">${uuid}</a>`;
  });
});

const isDataConflict = computed(() => props.task?.task_type === 'data_conflict' && props.task?.details?.local_entity);
const isDuplicateConflict = computed(() => props.task?.task_type === 'data_conflict' && props.task?.details?.duplicates);

const isActionDisabled = computed(() => isLoading.value || props.task?.status === 'pending_sd_action');

const handleCreateEntity = async () => {
  if (!props.task) return;
  isLoading.value = true;
  error.value = null;
  try {
    await createEntityInServiceDesk(props.task.id, props.task.entity_type);
    tasksStore.updateTaskStatusLocally(props.task.id, 'pending_sd_action');
    emit('close');
  } catch (err) {
    error.value = err.response?.data?.error || 'Не удалось отправить команду на создание';
  } finally {
    isLoading.value = false;
  }
};

const handleUpdateEntity = async () => {
  if (!props.task) return;
  isLoading.value = true;
  error.value = null;
  try {
    await resolveTaskUpdateInSd(props.task.id);
    tasksStore.updateTaskStatusLocally(props.task.id, 'pending_sd_action');
    emit('close');
  } catch (err) {
    error.value = err.response?.data?.error || 'Не удалось отправить команду на обновление';
  } finally {
    isLoading.value = false;
  }
};

const handleSleepTask = async () => {
  if (!props.task) return;
  isLoading.value = true;
  error.value = null;
  try {
    await resolveTaskAsSleep(props.task.id);
    tasksStore.updateTaskStatusLocally(props.task.id, 'sleep');
    emit('close');
  } catch (err) {
    error.value = err.response?.data?.error || 'Не удалось отложить задачу';
  } finally {
    isLoading.value = false;
  }
};

const handleDeleteDuplicate = async () => {
  if (!props.task || !selectedDuplicateUuid.value) return;
  const confirmation = confirm(`Вы уверены, что хотите удалить дубликат с UUID: ${selectedDuplicateUuid.value}? Это действие необратимо.`);
  if (!confirmation) return;

  isLoading.value = true;
  error.value = null;
  try {
    await resolveTaskDeleteDuplicate(props.task.id, selectedDuplicateUuid.value);
    tasksStore.updateTaskStatusLocally(props.task.id, 'pending_sd_action');
    emit('close');
  } catch (err) {
    error.value = err.response?.data?.error || 'Не удалось удалить дубликат';
  } finally {
    isLoading.value = false;
  }
};

const handleResolveDataConflict = async (strategy) => {
  if (!props.task) return;
  isLoading.value = true;
  error.value = null;
  try {
    await resolveTaskDataConflict(props.task.id, strategy);
    tasksStore.updateTaskStatusLocally(props.task.id, 'pending_sd_action');
    emit('close');
  } catch (err) {
    error.value = err.response?.data?.error || 'Не удалось решить конфликт';
  } finally {
    isLoading.value = false;
  }
};

</script>

<style scoped>
.action-btn {
  @apply px-4 py-2 rounded-md disabled:bg-slate-600 disabled:opacity-50 disabled:cursor-not-allowed transition-colors;
}
</style>
```

# ===================================================================
# Файл: src/components/tasks/DataConflictDiff.vue
# ===================================================================

```
<!-- src/components/tasks/DataConflictDiff.vue -->
<template>
  <div class="space-y-2">
    <!-- Шапка таблицы -->
    <div class="grid grid-cols-12 gap-2 text-sm font-semibold border-b border-slate-700 pb-2 text-white/60">
      <div class="col-span-3">Поле</div>
      <div 
        class="col-span-4 text-center rounded-t-md p-1"
        :class="isLocalNewer ? 'bg-green-500/20 text-green-300' : 'bg-slate-700/50'"
      >
        Локальные данные
        <p v-if="localDate" class="text-xs font-normal">{{ localDate }}</p>
      </div>
      <div 
        class="col-span-5 text-center rounded-t-md p-1"
        :class="!isLocalNewer ? 'bg-green-500/20 text-green-300' : 'bg-slate-700/50'"
      >
        Входящие данные (Агент/SD)
        <p v-if="remoteDate" class="text-xs font-normal">{{ remoteDate }}</p>
      </div>
    </div>

    <!-- Строки таблицы -->
    <div 
      v-for="key in allKeys" 
      :key="key"
      class="grid grid-cols-12 gap-2 text-xs items-center p-1 rounded"
      :class="{ 'bg-amber-500/20': details.conflicts[key] }"
    >
      <div class="col-span-3 text-white/70 font-mono">{{ key }}</div>
      <div class="col-span-4 break-words bg-slate-800/50 p-1.5 rounded">{{ details.local_entity[key] ?? '—' }}</div>
      <div class="col-span-5 break-words bg-slate-800/50 p-1.5 rounded">{{ details.remote_entity[key] ?? '—' }}</div>
    </div>

    <!-- Кнопки для решения -->
    <div class="flex justify-center gap-4 pt-4 mt-4 border-t border-slate-700">
        <button @click="$emit('resolve', 'use_local')" class="px-4 py-2 text-sm bg-sky-600 hover:bg-sky-500 rounded-md transition-colors">
            Принять локальные данные
        </button>
        <button @click="$emit('resolve', 'use_remote')" class="px-4 py-2 text-sm bg-sky-600 hover:bg-sky-500 rounded-md transition-colors">
            Принять входящие данные
        </button>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue';

const props = defineProps({ details: Object });
defineEmits(['resolve']);

// Собираем все уникальные ключи из обоих объектов для полного сравнения
const allKeys = computed(() => {
  const keys = new Set([
    ...Object.keys(props.details.local_entity || {}),
    ...Object.keys(props.details.remote_entity || {})
  ]);
  // Исключаем системные ключи, которые не нужно показывать
  return Array.from(keys).filter(k => !['ID', 'UUID', 'MetaClass', 'CreatedAt', 'UpdatedAt', 'DeletedAt', 'owner'].includes(k));
});

// Функция для извлечения и форматирования дат
const getDate = (entity, keyPatterns) => {
    for (const pattern of keyPatterns) {
        if (entity && entity[pattern]) {
            try {
                const date = new Date(entity[pattern]);
                return {
                    dateObj: date,
                    formatted: date.toLocaleString('ru-RU')
                };
            } catch (e) {
                return null;
            }
        }
    }
    return null;
};

// Ищем даты в обоих объектах по возможным ключам
const localDateInfo = computed(() => getDate(props.details.local_entity, ['UpdatedAt', 'last_modified_date']));
const remoteDateInfo = computed(() => getDate(props.details.remote_entity, ['lastModifiedDate']));

const localDate = computed(() => localDateInfo.value?.formatted);
const remoteDate = computed(() => remoteDateInfo.value?.formatted);

// Сравниваем, какая дата новее
const isLocalNewer = computed(() => {
    if (localDateInfo.value?.dateObj && remoteDateInfo.value?.dateObj) {
        return localDateInfo.value.dateObj > remoteDateInfo.value.dateObj;
    }
    return false; // Если одной из дат нет, не подсвечиваем
});

</script>
```

# ===================================================================
# Файл: src/components/tasks/NewClientDetails.vue
# ===================================================================

```
<!-- src/components/tasks/NewClientDetails.vue -->
<template>
  <div class="grid grid-cols-1 md:grid-cols-2 gap-x-6 gap-y-4 text-sm">
    <!-- Итерируемся по ключам, которые мы хотим показать -->
    <div v-for="key in visibleKeys" :key="key" class="border-b border-slate-700 pb-2">
      <p class="text-xs text-white/60 font-medium">{{ keyMappings[key] || key }}</p>
      <p class="font-mono text-white/90 break-words">{{ details[key] || '—' }}</p>
    </div>
  </div>
</template>

<script setup>
const props = defineProps({ details: Object });

// Определяем, какие ключи и в каком порядке показывать
const visibleKeys = [
  'organization_name',
  'inn',
  'url_rms',
  'hostname',
  'serial_number',
  'current_time',
  'teamviewer_id',
  'anydesk_id',
  'litemanager_id',
];

// Русские названия для ключей
const keyMappings = {
  organization_name: 'Организация',
  inn: 'ИНН',
  url_rms: 'RMS URL',
  hostname: 'Имя хоста',
  serial_number: 'ЗН',
  current_time: 'Время снимка',
  teamviewer_id: 'TeamViewer ID',
  anydesk_id: 'AnyDesk ID',
  litemanager_id: 'LiteManager ID',
};
</script>
```

# ===================================================================
# Файл: src/main.js
# ===================================================================

```
// src/main.js
import { createApp } from 'vue'
import App from './App.vue'
import router from './router'
import { createPinia } from 'pinia'
import './assets/main.css' // Импорт Tailwind CSS

const app = createApp(App)
const pinia = createPinia()

app.use(pinia)
app.use(router)
app.mount('#app')
```

# ===================================================================
# Файл: src/layouts/DefaultLayout.vue
# ===================================================================

```
<!-- src/layouts/DefaultLayout.vue -->
<template>
  <div class="min-h-screen bg-slate-900 text-white flex flex-col">
    <TheHeader />
    <main class="flex-1">
      <router-view />
    </main>
  </div>
</template>

<script setup>
import TheHeader from '@/components/layout/TheHeader.vue';
</script>
```

# ===================================================================
# Файл: src/services/api.js
# ===================================================================

```
// src/services/api.js

import axios from 'axios'
import { useAuthStore } from '@/store/auth'

const apiClient = axios.create({
  baseURL: '/api',
  headers: {
    'Content-Type': 'application/json',
  },
})

// Перехватчик запросов (Request Interceptor)
apiClient.interceptors.request.use(
  (config) => {
    const authStore = useAuthStore()
    const token = authStore.token
    if (token) {
      config.headers.Authorization = `Bearer ${token}`
    }
    return config
  },
  (error) => Promise.reject(error)
)

// Перехватчик ответов (Response Interceptor)
apiClient.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response && error.response.status === 401) {
      const authStore = useAuthStore()
      authStore.logout()
    }
    return Promise.reject(error)
  }
)

// --- Функции для работы с API Etalon ---

export const installLicense = (serverUuid, payload) => {
  return apiClient.post(`/servers/${serverUuid}/license`, payload);
};

export const pollServer = (uuid) => {
  return apiClient.post(`/servers/${uuid}/poll`);
};

export const createEntityInServiceDesk = (taskId, entityType) => {
  return apiClient.post(`/tasks/${taskId}/create-entity-in-sd`, {
    entity_type: entityType,
  });
};

export const resolveTaskUpdateInSd = (taskId) => {
  const payload = { status: "pending_sd_action", comment: "...", resolution_payload: { action: "update_in_sd" } };
  return apiClient.post(`/tasks/${taskId}/resolve`, payload);
};

export const resolveTaskAsSleep = (taskId) => {
  const payload = { status: "sleep", comment: "..." };
  return apiClient.post(`/tasks/${taskId}/resolve`, payload);
};

export const resolveTaskDeleteDuplicate = (taskId, entityUuidToDelete) => {
  const payload = { status: "pending_sd_action", comment: `...`, resolution_payload: { action: "delete_duplicate", entity_uuid_to_delete: entityUuidToDelete } };
  return apiClient.post(`/tasks/${taskId}/resolve`, payload);
};

export const resolveTaskDataConflict = (taskId, strategy) => {
  const payload = { status: "pending_sd_action", comment: `...`, resolution_payload: { action: "resolve_data_conflict", strategy: strategy } };
  return apiClient.post(`/tasks/${taskId}/resolve`, payload);
};


// --- Новая функция для получения анекдотов ---
/**
 * Запрашивает RSS-ленту с анекдотами через прокси.
 * @returns {Promise<string>} XML-строка с данными.
 */
export const fetchJokesRss = () => {
  // Используем новый инстанс axios, чтобы не отправлять заголовки авторизации
  // и не использовать базовый URL /api.
  return axios.get('/rss-proxy/export_top.xml', {
    responseType: 'text' // Важно получить ответ как текст для парсера
  });
};


// --- Фабрика для создания CRUD-сервисов ---
const createCrudApiService = (entityName) => ({
  getAll: (params) => apiClient.get(`/${entityName}`, { params }),
  getById: (id) => apiClient.get(`/${entityName}/${id}`),
  create: (data) => apiClient.post(`/${entityName}`, data),
  update: (id, data) => apiClient.put(`/${entityName}/${id}`, data),
  delete: (id) => apiClient.delete(`/${entityName}/${id}`),
});

export const serversApi = createCrudApiService('servers');
export const workstationsApi = createCrudApiService('workstations');
export const fiscalRegistersApi = createCrudApiService('fiscal-registers');

export default apiClient;
```

# ===================================================================
# Файл: src/router/index.js
# ===================================================================

```
// src/router/index.js

import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/store/auth'
import DefaultLayout from '@/layouts/DefaultLayout.vue'

const routes = [
  {
    path: '/',
    component: DefaultLayout,
    meta: { requiresAuth: true }, // Все дочерние роуты требуют аутентификации
    children: [
      {
        path: '', // Главная страница
        name: 'Search',
        component: () => import('@/views/Search.vue'),
      },
      {
        path: 'tasks',
        name: 'Tasks',
        component: () => import('@/views/Tasks.vue'),
      },
      {
        path: 'duplicates',
        name: 'Duplicates',
        component: () => import('@/views/Duplicates.vue'),
      },
      {
        path: 'admin', // Новый маршрут для админ-панели
        name: 'Admin',
        component: () => import('@/views/Admin.vue'),
      },
      // Заглушки для роутов, которые могут понадобиться в будущем
      {
        path: 'companies',
        name: 'Companies',
        component: () => import('@/views/Companies.vue'),
      },
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
    ],
  },
  {
    path: '/login',
    name: 'Login',
    component: () => import('@/views/Login.vue'),
  },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

router.beforeEach((to, from, next) => {
  const authStore = useAuthStore()
  const requiresAuth = to.matched.some(record => record.meta.requiresAuth)

  if (requiresAuth && !authStore.isAuthenticated) {
    next('/login')
  } else {
    next()
  }
})

export default router
```

# ===================================================================
# Файл: src/views/Search.vue
# ===================================================================

```
<!-- src/views/Search.vue -->
<template>
  <div class="min-h-screen bg-[#1e293b] p-4 sm:p-8">
    <div class="max-w-6xl xl:max-w-7xl 2xl:max-w-screen-2xl mx-auto">

      <!-- Область для вывода результатов -->
      <div v-if="searchStore.isLoading" class="text-center text-white/60 py-10">
        <p class="text-lg">Выполняется поиск...</p>
      </div>
      <div v-else-if="searchStore.error" class="mt-4 p-4 bg-red-500/20 text-red-300 border border-red-500/30 rounded-lg">
        {{ searchStore.error }}
      </div>
      
      <!-- Отображение, когда нет результатов -->
      <div v-else-if="!hasSearchResults">
        <!-- Если включены анекдоты, показываем текущий -->
        <div v-if="settingsStore.state.showJokes && searchStore.currentJoke" class="grid grid-cols-1 lg:grid-cols-2 2xl:grid-cols-3 gap-8 pt-6">
           <JokeCard :joke="searchStore.currentJoke" />
        </div>
        <div v-else-if="uiStore.searchTerm.length > 1" class="text-center text-white/60 py-10">
          <p class="text-lg">Ничего не найдено</p>
          <p class="text-sm">Попробуйте изменить поисковый запрос.</p>
        </div>
        <div v-else class="text-center text-white/60 py-10">
          <p class="text-lg">Начните поиск</p>
          <p class="text-sm">Введите запрос в строку поиска вверху.</p>
        </div>
      </div>
      
      <!-- Отображение, когда есть результаты -->
      <div v-else :class="gridClass">
        <CompanyCard
          v-for="group in filteredResults"
          :key="group.owner.uuid"
          :group="group"
          @openEntity="handleOpenEntity"
          @setSearchTerm="handleSetSearchTerm"
          @request-license-install="handleOpenLicenseModal"
        />
        <!-- Если включены анекдоты, добавляем карточку с текущим анекдотом -->
        <JokeCard v-if="settingsStore.state.showJokes && searchStore.currentJoke" :joke="searchStore.currentJoke" />
      </div>
    </div>

    <!-- Модалки -->
    <entity-detail-modal :show="showDetailModal" :entity="selectedEntity" :entity-type="selectedEntityType" @close="closeDetailModal" @edit="handleEdit" @delete="handleDelete" />
    <LicenseInstallModal :show="showLicenseModal" :server="selectedServerForLicense" @close="showLicenseModal = false" @submitted="handleLicenseSubmitted" />
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted, computed } from 'vue';
import { useSearchStore } from '@/store/search';
import { useUiStore } from '@/store/ui';
import { useSettingsStore } from '@/store/settings';
import CompanyCard from '@/components/search/CompanyCard.vue';
import JokeCard from '@/components/search/JokeCard.vue';
import EntityDetailModal from '@/components/search/EntityDetailModal.vue';
import LicenseInstallModal from '@/components/servers/LicenseInstallModal.vue';

const searchStore = useSearchStore();
const uiStore = useUiStore();
const settingsStore = useSettingsStore();

const showDetailModal = ref(false);
const selectedEntity = ref(null);
const selectedEntityType = ref('');
const showLicenseModal = ref(false);
const selectedServerForLicense = ref(null);

const hasSearchResults = computed(() => searchStore.searchResults.length > 0);

const gridClass = computed(() => {
  const base = 'grid grid-cols-1 gap-8 pt-6';
  const columns = settingsStore.state.resultColumns === 3
    ? 'lg:grid-cols-2 2xl:grid-cols-3'
    : 'lg:grid-cols-2';
  return `${base} ${columns}`;
});

const filteredResults = computed(() => {
  if (uiStore.showWithoutContract) {
    return searchStore.searchResults;
  }
  return searchStore.searchResults.filter(group => group.owner.active_contract === true);
});

const handleSetSearchTerm = (newTerm) => { uiStore.searchTerm = newTerm; uiStore.executeSearch(); };
const handleOpenEntity = ({ entity, entityType }) => { selectedEntity.value = entity; selectedEntityType.value = entityType; showDetailModal.value = true; };
const closeDetailModal = () => { showDetailModal.value = false; selectedEntity.value = null; selectedEntityType.value = ''; };
const handleEdit = (entity) => console.log('Редактирование:', entity);
const handleDelete = (entity) => console.log('Удаление:', entity);
const handleKeydown = (event) => { if (event.key === 'Escape' && showDetailModal.value) closeDetailModal(); };
const handleOpenLicenseModal = (serverEntity) => { selectedServerForLicense.value = serverEntity; showLicenseModal.value = true; };
const handleLicenseSubmitted = (serverUuid) => {
  console.log(`Лицензия для ${serverUuid} отправлена. Отслеживание...`);
  setTimeout(() => uiStore.executeSearch(), 5000);
};

onMounted(() => {
  document.addEventListener('keydown', handleKeydown);
  if (settingsStore.state.showJokes) {
    searchStore.fetchAndParseJokes();
  }
});

onUnmounted(() => {
  document.removeEventListener('keydown', handleKeydown);
});
</script>
```

# ===================================================================
# Файл: src/views/FiscalRegisters.vue
# ===================================================================

```
<template>
  <div>
    <h1>Фискальные регистраторы</h1>
    <p>Эта страница находится в разработке.</p>
  </div>
</template>
```

# ===================================================================
# Файл: src/views/Duplicates.vue
# ===================================================================

```
<template>
  <div>
    <h1>Поиск дубликатов</h1>
    <p>Эта страница находится в разработке.</p>
  </div>
</template>
```

# ===================================================================
# Файл: src/views/Admin.vue
# ===================================================================

```
<!-- src/views/Admin.vue -->
<template>
  <div class="p-4 sm:p-8 max-w-4xl mx-auto">
    <h1 class="text-2xl font-bold mb-6">Админ-панель</h1>
    
    <div class="bg-slate-800/60 p-6 rounded-lg space-y-6">
      <div>
        <h2 class="text-xl font-semibold text-white mb-3">Настройки поиска</h2>
        
        <!-- Настройка ширины строки поиска -->
        <div class="setting-item">
          <label class="text-white/80">Ширина строки поиска</label>
          <div class="flex gap-2">
            <!-- ИЗМЕНЕНИЕ ЗДЕСЬ: убран .value -->
            <button @click="settingsStore.setSearchLayout('wide')" :class="['btn', { 'btn-active': settingsStore.state.searchLayout === 'wide' }]">Широкая</button>
            <button @click="settingsStore.setSearchLayout('centered')" :class="['btn', { 'btn-active': settingsStore.state.searchLayout === 'centered' }]">По центру</button>
          </div>
        </div>

        <!-- Настройка колонок результатов -->
        <div class="setting-item">
          <label class="text-white/80">Количество колонок в результатах</label>
           <div class="flex gap-2">
            <!-- ИЗМЕНЕНИЕ ЗДЕСЬ: убран .value -->
            <button @click="settingsStore.setResultColumns(2)" :class="['btn', { 'btn-active': settingsStore.state.resultColumns === 2 }]">2 колонки</button>
            <button @click="settingsStore.setResultColumns(3)" :class="['btn', { 'btn-active': settingsStore.state.resultColumns === 3 }]">3 колонки</button>
          </div>
        </div>
      </div>

      <div>
        <h2 class="text-xl font-semibold text-white mb-3">Прочее</h2>
        <!-- Настройка показа анекдотов -->
        <div class="setting-item">
          <label class="text-white/80">Режим "Анекдоты"</label>
          <!-- ИЗМЕНЕНИЕ ЗДЕСЬ: убран .value -->
          <button @click="handleToggleJokes" :class="['btn', { 'btn-active': settingsStore.state.showJokes }]">
            {{ settingsStore.state.showJokes ? 'Включен' : 'Выключен' }}
          </button>
        </div>
      </div>

    </div>
  </div>
</template>

<script setup>
import { useSettingsStore } from '@/store/settings';
import { useSearchStore } from '@/store/search';

const settingsStore = useSettingsStore();
const searchStore = useSearchStore();

const handleToggleJokes = () => {
  settingsStore.toggleJokes();
  // ИЗМЕНЕНИЕ ЗДЕСЬ: убран .value
  if (settingsStore.state.showJokes) {
    searchStore.fetchAndParseJokes();
  }
};
</script>

<style scoped>
.setting-item { @apply flex justify-between items-center p-3 border-b border-slate-700; }
.btn { @apply px-4 py-2 bg-slate-700 text-white/80 rounded-md hover:bg-slate-600 transition-colors text-sm; }
.btn-active { @apply bg-sky-600 text-white font-semibold; }
</style>
```

# ===================================================================
# Файл: src/views/Companies.vue
# ===================================================================

```
<template>
  <div>
    <h1>Компании</h1>
    <p>Эта страница находится в разработке.</p>
  </div>
</template>
```

# ===================================================================
# Файл: src/views/Workstations.vue
# ===================================================================

```
<template>
  <div>
    <h1>Рабочие станции</h1>
    <p>Эта страница находится в разработке.</p>
  </div>
</template>
```

# ===================================================================
# Файл: src/views/Login.vue
# ===================================================================

```
<template>
  <div class="min-h-screen bg-slate-900 flex items-center justify-center p-4">
    <div class="w-full max-w-md">
      <div class="bg-slate-800 rounded-lg shadow-lg p-8">
        <h1 class="text-2xl font-bold text-white text-center mb-6">Вход в систему Etalon</h1>
        <form @submit.prevent="handleLogin">
          <div class="mb-4">
            <label for="username" class="block text-sm font-medium text-gray-300 mb-2">Логин</label>
            <input
              id="username"
              v-model="username"
              type="text"
              required
              class="w-full bg-slate-700 text-white placeholder:text-gray-400 rounded-md py-2 px-3 border border-slate-600 focus:border-sky-500 focus:outline-none focus:ring-1 focus:ring-sky-500"
            />
          </div>
          <div class="mb-6">
            <label for="password" class="block text-sm font-medium text-gray-300 mb-2">Пароль</label>
            <input
              id="password"
              v-model="password"
              type="password"
              required
              class="w-full bg-slate-700 text-white placeholder:text-gray-400 rounded-md py-2 px-3 border border-slate-600 focus:border-sky-500 focus:outline-none focus:ring-1 focus:ring-sky-500"
            />
          </div>

          <!-- Сообщение об ошибке -->
          <div v-if="error" class="mb-4 p-3 bg-red-500/20 text-red-300 border border-red-500/30 rounded-md text-sm">
            {{ error }}
          </div>

          <button
            type="submit"
            class="w-full bg-sky-600 text-white font-bold py-2 px-4 rounded-md hover:bg-sky-700 transition-colors duration-200"
          >
            Войти
          </button>
        </form>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { useAuthStore } from '@/store/auth'

const username = ref('')
const password = ref('')
const error = ref(null)
const authStore = useAuthStore()

const handleLogin = async () => {
  error.value = null
  try {
    await authStore.login({
      username: username.value,
      password: password.value,
    })
  } catch (err) {
    error.value = err.message
  }
}
</script>
```

# ===================================================================
# Файл: src/views/Servers.vue
# ===================================================================

```
<!-- src/views/Servers.vue -->
<template>
  <div>
    <h1>Серверы</h1>
    <p>Эта страница находится в разработке.</p>
  </div>
</template>
```

# ===================================================================
# Файл: src/views/Tasks.vue
# ===================================================================

```
<!-- src/views/Tasks.vue -->
<template>
  <div class="p-4 sm:p-8 max-w-7xl mx-auto">
    <h1 class="text-2xl font-bold mb-6">Задачи на сверку</h1>

    <!-- Фильтры и кнопка обновления -->
    <div class="flex justify-between items-center mb-6">
      <div class="flex gap-2">
        <button 
          @click="tasksStore.toggleResolvedVisibility()" 
          :class="['btn', { 'btn-active': tasksStore.showResolved }]">
          Показывать решенные
        </button>
      </div>
      <button @click="tasksStore.fetchTasks()" class="btn" title="Обновить список задач">
        <svg class="w-5 h-5" :class="{'animate-spin': tasksStore.isLoading}" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" d="M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.993 0l3.181 3.183a8.25 8.25 0 0011.664 0l3.181-3.183m-3.181-4.991v4.99" />
        </svg>
      </button>
    </div>

    <!-- Блок отображения -->
    <div v-if="tasksStore.isLoading && !tasksStore.tasks.length" class="text-center text-white/60 py-10">
      Загрузка задач...
    </div>
    <div v-else-if="tasksStore.error" class="mt-4 p-4 bg-red-500/20 text-red-300 border border-red-500/30 rounded-lg">
      {{ tasksStore.error }}
    </div>
    <div v-else>
      <div v-if="searchedTasks.length" class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
        <TaskItem 
          v-for="task in searchedTasks" 
          :key="task.id" 
          :task="task"
          @select="selectedTask = task"
        />
      </div>
      <div v-else class="text-center text-white/60 py-10">
        Нет задач для отображения.
      </div>
    </div>
    
    <TaskDetailsModal 
      :task="selectedTask" 
      @close="selectedTask = null"
    />
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted, computed } from 'vue';
import { useTasksStore } from '@/store/tasks';
import { useUiStore } from '@/store/ui';
import TaskItem from '@/components/tasks/TaskItem.vue';
import TaskDetailsModal from '@/components/tasks/TaskDetailsModal.vue';

const tasksStore = useTasksStore();
const uiStore = useUiStore();
const selectedTask = ref(null);
let pollInterval = null;

// Фильтрация задач на клиенте по строке поиска из uiStore
const searchedTasks = computed(() => {
  const baseTasks = tasksStore.filteredTasks;
  const term = uiStore.searchTerm.toLowerCase().trim();

  if (!term) {
    return baseTasks;
  }

  return baseTasks.filter(task => {
    return (
      String(task.id).includes(term) ||
      task.task_type.toLowerCase().includes(term) ||
      task.entity_repr.toLowerCase().includes(term) ||
      task.status.toLowerCase().includes(term)
    );
  });
});

onMounted(() => {
  tasksStore.fetchTasks();
  pollInterval = setInterval(() => {
    if (!tasksStore.isLoading && !selectedTask.value) {
      tasksStore.fetchTasks();
    }
  }, 10000);
});

onUnmounted(() => {
  clearInterval(pollInterval);
  // Очищаем поиск при уходе со страницы
  uiStore.clearSearchTerm();
});

</script>

<style scoped>
.btn {
  @apply px-4 py-2 bg-slate-700 text-white/80 rounded-md hover:bg-slate-600 transition-colors disabled:opacity-50 disabled:cursor-not-allowed;
}
.btn-active {
  @apply bg-sky-600 text-white font-semibold;
}
</style>
```

# ===================================================================
# Файл: src/store/settings.js
# ===================================================================

```
// src/store/settings.js
import { defineStore } from 'pinia';
import { reactive, watch } from 'vue'; // ИСПРАВЛЕНИЕ ЗДЕСЬ: добавлен импорт ref

// Функция для безопасной загрузки настроек из localStorage
const loadSettingsFromStorage = () => {
  try {
    const storedSettings = localStorage.getItem('etalon-settings');
    if (storedSettings) {
      return JSON.parse(storedSettings);
    }
  } catch (e) {
    console.error("Failed to parse settings from localStorage", e);
  }
  return null;
};

export const useSettingsStore = defineStore('settings', () => {
  // Начальное состояние. Загружаем из localStorage или используем дефолтное.
  const state = reactive({
    searchLayout: 'centered', // 'wide' или 'centered'
    resultColumns: 3,     // 2 или 3
    showJokes: false,
    ...loadSettingsFromStorage(),
  });

// Действия для изменения состояния
  const setSearchLayout = (layout) => {
    state.searchLayout = layout;
  };

  const setResultColumns = (columns) => {
    state.resultColumns = columns;
  };

  const toggleJokes = () => {
    state.showJokes = !state.showJokes;
  };

  // Наблюдаем за изменениями в состоянии и сохраняем их в localStorage
  watch(state, (newState) => {
    localStorage.setItem('etalon-settings', JSON.stringify(newState));
  }, { deep: true });

  // state это сам объект, а не ref
  return {
    state,
    setSearchLayout,
    setResultColumns,
    toggleJokes,
  };
});
```

# ===================================================================
# Файл: src/store/tasks.js
# ===================================================================

```
// src/store/tasks.js
import { defineStore } from 'pinia';
import apiClient from '@/services/api';

const convertKeysToSnakeCase = (obj) => {
  if (typeof obj !== 'object' || obj === null) { return obj; }
  if (Array.isArray(obj)) { return obj.map(v => convertKeysToSnakeCase(v)); }
  const newObj = {};
  for (const key in obj) {
    if (key === 'ID') { newObj['id'] = obj[key]; continue; }
    if (key === 'UUID') { newObj['uuid'] = obj[key]; continue; }
    const newKey = key
      .replace(/([A-Z]+)([A-Z][a-z])/g, '$1_$2')
      .replace(/([a-z\d])([A-Z])/g, '$1_$2')
      .toLowerCase();
    newObj[newKey] = convertKeysToSnakeCase(obj[key]);
  }
  return newObj;
};

export const useTasksStore = defineStore('tasks', {
  state: () => ({
    tasks: [], // Здесь будет полный список всех задач
    isLoading: false,
    error: null,
    showResolved: false, // Новый флаг для фильтрации на клиенте
  }),

  getters: {
    // Геттер для отображения отфильтрованного списка в компоненте
    filteredTasks: (state) => {
      if (state.showResolved) {
        return state.tasks; // Показываем все
      }
      // Показываем все, кроме "resolved"
      return state.tasks.filter(task => task.status !== 'resolved');
    }
  },

  actions: {
    async fetchTasks() {
      this.isLoading = true;
      this.error = null;
      try {
        // Запрашиваем ВСЕ релевантные статусы
        const statuses = ['new', 'pending_sd_action', 'sd_error', 'resolved'].join(',');
        const response = await apiClient.get('/tasks', { params: { limit: 500 } });
        
        const rawTasks = response.data || [];
        this.tasks = rawTasks.map(task => {
          const convertedTask = convertKeysToSnakeCase(task);
          if (convertedTask.entity_type && convertedTask.entity_uuid) {
            convertedTask.entity_repr = `${convertedTask.entity_type}: ${convertedTask.entity_uuid}`;
          } else {
            convertedTask.entity_repr = convertedTask.comment.substring(0, 50) + '...';
          }
          return convertedTask;
        });
      } catch (err) {
        this.error = 'Не удалось загрузить список задач.';
        this.tasks = [];
        console.error(err);
      } finally {
        this.isLoading = false;
      }
    },
    
    updateTaskStatusLocally(taskId, newStatus) {
        const task = this.tasks.find(t => t.id === taskId);
        if (task) {
            task.status = newStatus;
        }
    },

    // Новый экшен для переключения фильтра
    toggleResolvedVisibility() {
      this.showResolved = !this.showResolved;
    }
  }
});
```

# ===================================================================
# Файл: src/store/auth.js
# ===================================================================

```
import { defineStore } from 'pinia'
import apiClient from '@/services/api'
import router from '@/router'

export const useAuthStore = defineStore('auth', {
  state: () => ({
    token: localStorage.getItem('token') || null,
    user: JSON.parse(localStorage.getItem('user')) || null,
  }),

  getters: {
    isAuthenticated: (state) => !!state.token,
    currentUser: (state) => state.user,
  },

  actions: {
    // Действие для входа в систему
    async login(credentials) {
      try {
        // --- ЭТО РЕАЛЬНЫЙ ЗАПРОС К БЭКЕНДУ ---
        const response = await apiClient.post('/auth/login', credentials);
        
        const { access_token, user } = response.data

        // 1. Сохраняем токен и данные пользователя в state
        this.token = access_token
        this.user = user

        // 2. Сохраняем в localStorage для персистентности между перезагрузками
        localStorage.setItem('token', access_token)
        localStorage.setItem('user', JSON.stringify(user))

        // 3. Перенаправляем на главную страницу после успешного входа
        // Используем replace, чтобы пользователь не мог вернуться на страницу логина кнопкой "назад"
        await router.replace('/')
      } catch (error) {
        console.error('Ошибка аутентификации:', error)
        // Пробрасываем ошибку дальше, чтобы компонент Login.vue мог ее отобразить
        throw new Error(error.response?.data?.error || 'Произошла сетевая ошибка')
      }
    },

    // Действие для выхода из системы
    logout() {
      this.token = null
      this.user = null
      localStorage.removeItem('token')
      localStorage.removeItem('user')
      router.push('/login')
    },
  },
})
```

# ===================================================================
# Файл: src/store/ui.js
# ===================================================================

```
// src/store/ui.js
import { defineStore } from 'pinia';
import { useSearchStore } from './search';
import router from '@/router';

export const useUiStore = defineStore('ui', {
  state: () => ({
    searchTerm: '',
    showWithoutContract: false, // Состояние для фильтра "показывать без контракта"
  }),
  actions: {
    /**
     * Выполняет поиск в зависимости от текущего маршрута.
     */
    executeSearch() {
      const currentRoute = router.currentRoute.value;
      
      // Если мы на странице задач, поиск не будет вызывать API,
      // а будет использоваться для фильтрации на клиенте.
      if (currentRoute.name === 'Tasks') {
        return;
      }

      // Для всех остальных страниц выполняем глобальный поиск.
      const searchStore = useSearchStore();
      
      // Если мы не на главной, переходим на нее для отображения результатов.
      if (currentRoute.name !== 'Search') {
        router.push('/');
      }
      
      searchStore.performGlobalSearch(this.searchTerm, { addToHistory: true });
    },
    
    /**
     * Очищает строку поиска. Полезно при переходе между страницами.
     */
    clearSearchTerm() {
        this.searchTerm = '';
    },

    /**
     * Переключает видимость клиентов без активного контракта.
     */
    toggleShowWithoutContract() {
      this.showWithoutContract = !this.showWithoutContract;
    }
  },
});
```

# ===================================================================
# Файл: src/store/search.js
# ===================================================================

```
import { defineStore } from 'pinia';
import apiClient, { serversApi, workstationsApi, fiscalRegistersApi, fetchJokesRss } from '@/services/api';

const apiServiceMap = {
  servers: serversApi,
  workstations: workstationsApi,
  'fiscal-registers': fiscalRegistersApi,
};

export const useSearchStore = defineStore('search', {
  state: () => ({
    isLoading: false,
    error: null,
    searchResults: [],
    entities: {
      servers: { items: [], total: 0, limit: 50, offset: 0, has_next: false, has_prev: false },
      workstations: { items: [], total: 0, limit: 50, offset: 0, has_next: false, has_prev: false },
      'fiscal-registers': { items: [], total: 0, limit: 50, offset: 0, has_next: false, has_prev: false },
    },
    expandedCards: {},
    searchHistory: [],
    historyIndex: -1,
    jokes: [],
    currentJoke: null, // Храним текущий анекдот здесь
  }),

  getters: {
    canGoBack: (state) => state.historyIndex > 0,
    canGoForward: (state) => state.historyIndex < state.searchHistory.length - 1,
  },

  actions: {
    // Действие для выбора нового случайного анекдота
    pickNewRandomJoke() {
      if (this.jokes.length === 0) {
        this.currentJoke = null;
        return;
      }
      // Простая защита от повторения того же анекдота подряд
      if (this.jokes.length > 1) {
        let newJoke;
        do {
          const randomIndex = Math.floor(Math.random() * this.jokes.length);
          newJoke = this.jokes[randomIndex];
        } while (this.currentJoke && newJoke.title === this.currentJoke.title);
        this.currentJoke = newJoke;
      } else {
        this.currentJoke = this.jokes[0];
      }
    },
    
    async performGlobalSearch(term, options = { addToHistory: true }) {
      if (!term || term.length < 2) {
        this.searchResults = [];
        this.error = null;
        this.pickNewRandomJoke(); // Обновляем анекдот даже при очистке
        return;
      }
      if (options.addToHistory) {
        this.addSearchToHistory(term);
      }
      this.isLoading = true;
      this.error = null;
      try {
        const response = await apiClient.get(`/search?term=${term}`);
        this.searchResults = response.data.search_results || [];
      } catch (err) {
        this.error = 'Произошла ошибка при выполнении поиска.';
        this.searchResults = [];
      } finally {
        this.isLoading = false;
        this.pickNewRandomJoke(); // Обновляем анекдот после каждого поиска
      }
    },
    
    addSearchToHistory(term) {
      if (this.searchHistory[this.historyIndex] === term) return;
      const newHistory = this.searchHistory.slice(0, this.historyIndex + 1);
      newHistory.push(term);
      this.searchHistory = newHistory;
      this.historyIndex = this.searchHistory.length - 1;
    },

    navigateHistory(direction) {
      if (direction === 'back' && this.canGoBack) this.historyIndex--;
      else if (direction === 'forward' && this.canGoForward) this.historyIndex++;
      return this.searchHistory[this.historyIndex] || null;
    },

    async fetchAndParseJokes() {
      if (this.jokes.length > 0) return;
      this.isLoading = true;
      try {
        const response = await fetchJokesRss();
        const parser = new DOMParser();
        const xmlDoc = parser.parseFromString(response.data, "application/xml");
        const items = xmlDoc.querySelectorAll("item");
        const parsedJokes = [];
        items.forEach(item => {
          const description = item.querySelector("description")?.textContent || '';
          parsedJokes.push({
            title: item.querySelector("title")?.textContent || 'Анекдот',
            description: description.replace(/<br>/g, '\n').trim(),
          });
        });
        this.jokes = parsedJokes;
        this.pickNewRandomJoke(); // Выбираем первый анекдот сразу после загрузки
      } catch (err) {
        this.error = "Не удалось загрузить анекдоты.";
      } finally {
        this.isLoading = false;
      }
    },
    // --- Новые CRUD Actions ---

    /**
     * Загружает список сущностей с пагинацией и фильтрами.
     * @param {object} payload - { entityType: 'servers', params: { limit, offset, filter, sort... } }
     */
    async fetchEntities({ entityType, params }) {
      const apiService = apiServiceMap[entityType];
      if (!apiService) return;

      this.isLoading = true;
      this.error = null;
      try {
        const { data } = await apiService.getAll(params);
        this.entities[entityType] = {
          items: data.data,
          ...data.pagination, // total, limit, offset, has_next, has_prev
        };
      } catch (err) {
        this.error = `Ошибка при загрузке ${entityType}.`;
        console.error(err);
      } finally {
        this.isLoading = false;
      }
    },

    /**
     * Создание новой сущности.
     * @param {object} payload - { entityType: 'servers', data: { ... } }
     */
    async createEntity({ entityType, data }) {
      const apiService = apiServiceMap[entityType];
      if (!apiService) return;
      
      // Можно добавить optimistic UI update или просто перезапросить список
      try {
        await apiService.create(data);
        // Перезапрашиваем первую страницу, чтобы увидеть новый элемент
        await this.fetchEntities({ entityType, params: { limit: this.entities[entityType].limit, offset: 0 } });
      } catch (err) {
        this.error = `Ошибка при создании ${entityType}.`;
        console.error(err);
        throw err; // Пробрасываем ошибку для обработки в UI
      }
    },

    /**
     * Обновление существующей сущности.
     * @param {object} payload - { entityType: 'servers', id: '...', data: { ... } }
     */
    async updateEntity({ entityType, id, data }) {
      const apiService = apiServiceMap[entityType];
      if (!apiService) return;

      try {
        const { data: updatedEntity } = await apiService.update(id, data);
        
        // Обновляем элемент в локальном списке
        const Elist = this.entities[entityType];
        const index = Elist.items.findIndex(item => item.id === id);
        if (index !== -1) {
          Elist.items[index] = updatedEntity.data;
        }
      } catch (err) {
        this.error = `Ошибка при обновлении ${entityType}.`;
        console.error(err);
        throw err;
      }
    },

    /**
     * Удаление сущности.
     * @param {object} payload - { entityType: 'servers', id: '...' }
     */
    async deleteEntity({ entityType, id }) {
      const apiService = apiServiceMap[entityType];
      if (!apiService) return;
      
      try {
        await apiService.delete(id);
        // Удаляем элемент из локального списка
        const Elist = this.entities[entityType];
        Elist.items = Elist.items.filter(item => item.id !== id);
        Elist.total -= 1;
      } catch (err) {
        this.error = `Ошибка при удалении ${entityType}.`;
        console.error(err);
        throw err;
      }
    },
  },
});
```

# ===================================================================
# Файл: src/App.vue
# ===================================================================

```
<!-- src/App.vue -->
<template>
  <router-view />
</template>
```

# ===================================================================
# Файл: src/utils/debounce.js
# ===================================================================

```
// src/utils/debounce.js

/**
 * Создает debounce-функцию, которая откладывает вызов func до тех пор,
 * пока не пройдет wait миллисекунд после последнего вызова.
 * @param {Function} func Исполняемая функция.
 * @param {number} wait Задержка в миллисекундах.
 * @returns {Function} Новая debounce-функция.
 */
export function debounce(func, wait) {
  let timeout;

  return function executedFunction(...args) {
    const later = () => {
      clearTimeout(timeout);
      func(...args);
    };

    clearTimeout(timeout);
    timeout = setTimeout(later, wait);
  };
}
```

# ===================================================================
# Файл: src/assets/main.css
# ===================================================================

```
/* src/assets/main.css */
@tailwind base;
@tailwind components;
@tailwind utilities;
```
