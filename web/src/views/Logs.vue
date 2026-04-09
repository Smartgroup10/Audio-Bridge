<template>
  <div>
    <h1 class="text-2xl font-bold text-gray-800 mb-6">Logs</h1>

    <!-- Tabs -->
    <div class="flex gap-1 mb-6 bg-gray-100 rounded-lg p-1 w-fit">
      <button
        @click="activeTab = 'interactions'"
        class="px-4 py-2 rounded-md text-sm font-medium transition-colors"
        :class="activeTab === 'interactions'
          ? 'bg-white text-gray-900 shadow-sm'
          : 'text-gray-500 hover:text-gray-700'"
      >
        Llamadas
      </button>
      <button
        @click="activeTab = 'system'"
        class="px-4 py-2 rounded-md text-sm font-medium transition-colors"
        :class="activeTab === 'system'
          ? 'bg-white text-gray-900 shadow-sm'
          : 'text-gray-500 hover:text-gray-700'"
      >
        Sistema
      </button>
    </div>

    <!-- ==================== INTERACTION LOGS TAB ==================== -->
    <template v-if="activeTab === 'interactions'">
      <!-- Filters -->
      <div class="card mb-6">
        <div class="flex flex-wrap gap-4 items-end">
          <div class="min-w-[180px]">
            <label class="block text-xs font-medium text-gray-500 mb-1">Call ID</label>
            <input v-model="iFilters.call_id" type="text" placeholder="UUID..." class="input-field" @keyup.enter="applyInteractionFilters" />
          </div>
          <div class="min-w-[120px]">
            <label class="block text-xs font-medium text-gray-500 mb-1">Direccion</label>
            <select v-model="iFilters.direction" @change="applyInteractionFilters" class="input-field">
              <option value="">Todas</option>
              <option value="user">Usuario</option>
              <option value="ai">IA</option>
            </select>
          </div>
          <div class="min-w-[140px]">
            <label class="block text-xs font-medium text-gray-500 mb-1">Tipo evento</label>
            <select v-model="iFilters.event_type" @change="applyInteractionFilters" class="input-field">
              <option value="">Todos</option>
              <option value="speech">Speech</option>
              <option value="transfer">Transfer</option>
              <option value="hangup">Hangup</option>
              <option value="function_call">Function Call</option>
            </select>
          </div>
          <div class="min-w-[140px]">
            <label class="block text-xs font-medium text-gray-500 mb-1">Desde</label>
            <input v-model="iFilters.from" type="date" class="input-field" @change="applyInteractionFilters" />
          </div>
          <div class="min-w-[140px]">
            <label class="block text-xs font-medium text-gray-500 mb-1">Hasta</label>
            <input v-model="iFilters.to" type="date" class="input-field" @change="applyInteractionFilters" />
          </div>
          <button @click="clearInteractionFilters" class="btn-secondary text-sm">Limpiar</button>
        </div>
      </div>

      <!-- Table -->
      <div class="card overflow-hidden p-0">
        <div class="overflow-x-auto">
          <table class="w-full text-sm">
            <thead>
              <tr class="bg-gray-50 border-b border-gray-200">
                <th class="text-left px-4 py-3 font-medium text-gray-500">Hora</th>
                <th class="text-left px-4 py-3 font-medium text-gray-500">Call ID</th>
                <th class="text-left px-4 py-3 font-medium text-gray-500">Dir</th>
                <th class="text-left px-4 py-3 font-medium text-gray-500">Tipo</th>
                <th class="text-left px-4 py-3 font-medium text-gray-500">Contenido</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="log in logsStore.interactionLogs"
                :key="log.id"
                class="border-b border-gray-100 hover:bg-blue-50/50 transition-colors"
              >
                <td class="px-4 py-3 text-xs text-gray-500 whitespace-nowrap">{{ formatTime(log.timestamp) }}</td>
                <td class="px-4 py-3">
                  <router-link
                    :to="{ name: 'CallDetail', params: { id: log.call_id } }"
                    class="font-mono text-xs text-blue-600 hover:underline"
                  >
                    {{ log.call_id?.substring(0, 8) }}
                  </router-link>
                </td>
                <td class="px-4 py-3">
                  <span
                    class="inline-flex px-2 py-0.5 rounded-full text-xs font-medium"
                    :class="log.direction === 'user' ? 'bg-blue-100 text-blue-700' : 'bg-cyan-100 text-cyan-700'"
                  >
                    {{ log.direction === 'user' ? 'USR' : 'IA' }}
                  </span>
                </td>
                <td class="px-4 py-3">
                  <span class="inline-flex px-2 py-0.5 rounded-full text-xs font-medium bg-gray-100 text-gray-600">
                    {{ log.event_type }}
                  </span>
                </td>
                <td class="px-4 py-3 text-gray-700 max-w-md truncate">{{ log.content }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-if="!logsStore.interactionLogs.length && !logsStore.loading" class="text-center py-12 text-gray-400">
          No se encontraron logs de llamadas
        </div>
        <div v-if="logsStore.loading" class="text-center py-12 text-gray-400">Cargando...</div>
      </div>

      <!-- Pagination -->
      <div v-if="logsStore.interactionTotalPages > 1" class="flex items-center justify-between mt-4">
        <p class="text-sm text-gray-500">
          Pagina {{ logsStore.interactionPagination.page }} de {{ logsStore.interactionTotalPages }}
          ({{ logsStore.interactionPagination.total }} resultados)
        </p>
        <div class="flex gap-2">
          <button
            @click="goInteractionPage(logsStore.interactionPagination.page - 1)"
            :disabled="logsStore.interactionPagination.page <= 1"
            class="btn-secondary text-sm disabled:opacity-40"
          >Anterior</button>
          <button
            @click="goInteractionPage(logsStore.interactionPagination.page + 1)"
            :disabled="logsStore.interactionPagination.page >= logsStore.interactionTotalPages"
            class="btn-secondary text-sm disabled:opacity-40"
          >Siguiente</button>
        </div>
      </div>
    </template>

    <!-- ==================== SYSTEM LOGS TAB ==================== -->
    <template v-if="activeTab === 'system'">
      <!-- Filters -->
      <div class="card mb-6">
        <div class="flex flex-wrap gap-4 items-end">
          <div class="min-w-[120px]">
            <label class="block text-xs font-medium text-gray-500 mb-1">Nivel</label>
            <select v-model="sFilters.level" @change="applySystemFilters" class="input-field">
              <option value="">Todos</option>
              <option value="info">Info</option>
              <option value="warn">Warn</option>
              <option value="error">Error</option>
            </select>
          </div>
          <div class="min-w-[140px]">
            <label class="block text-xs font-medium text-gray-500 mb-1">Desde</label>
            <input v-model="sFilters.from" type="date" class="input-field" @change="applySystemFilters" />
          </div>
          <div class="min-w-[140px]">
            <label class="block text-xs font-medium text-gray-500 mb-1">Hasta</label>
            <input v-model="sFilters.to" type="date" class="input-field" @change="applySystemFilters" />
          </div>
          <button @click="clearSystemFilters" class="btn-secondary text-sm">Limpiar</button>
        </div>
      </div>

      <!-- Table -->
      <div class="card overflow-hidden p-0">
        <div class="overflow-x-auto">
          <table class="w-full text-sm">
            <thead>
              <tr class="bg-gray-50 border-b border-gray-200">
                <th class="text-left px-4 py-3 font-medium text-gray-500">Hora</th>
                <th class="text-left px-4 py-3 font-medium text-gray-500">Nivel</th>
                <th class="text-left px-4 py-3 font-medium text-gray-500">Logger</th>
                <th class="text-left px-4 py-3 font-medium text-gray-500">Mensaje</th>
                <th class="text-left px-4 py-3 font-medium text-gray-500">Caller</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="log in logsStore.systemLogs"
                :key="log.id"
                class="border-b border-gray-100 hover:bg-blue-50/50 transition-colors"
              >
                <td class="px-4 py-3 text-xs text-gray-500 whitespace-nowrap">{{ formatTime(log.timestamp) }}</td>
                <td class="px-4 py-3">
                  <span
                    class="inline-flex px-2 py-0.5 rounded-full text-xs font-medium"
                    :class="levelClass(log.level)"
                  >
                    {{ log.level.toUpperCase() }}
                  </span>
                </td>
                <td class="px-4 py-3 font-mono text-xs text-gray-500">{{ log.logger || '-' }}</td>
                <td class="px-4 py-3 text-gray-700 max-w-lg truncate">{{ log.message }}</td>
                <td class="px-4 py-3 font-mono text-xs text-gray-400">{{ log.caller || '-' }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-if="!logsStore.systemLogs.length && !logsStore.loading" class="text-center py-12 text-gray-400">
          No se encontraron logs del sistema
        </div>
        <div v-if="logsStore.loading" class="text-center py-12 text-gray-400">Cargando...</div>
      </div>

      <!-- Pagination -->
      <div v-if="logsStore.systemTotalPages > 1" class="flex items-center justify-between mt-4">
        <p class="text-sm text-gray-500">
          Pagina {{ logsStore.systemPagination.page }} de {{ logsStore.systemTotalPages }}
          ({{ logsStore.systemPagination.total }} resultados)
        </p>
        <div class="flex gap-2">
          <button
            @click="goSystemPage(logsStore.systemPagination.page - 1)"
            :disabled="logsStore.systemPagination.page <= 1"
            class="btn-secondary text-sm disabled:opacity-40"
          >Anterior</button>
          <button
            @click="goSystemPage(logsStore.systemPagination.page + 1)"
            :disabled="logsStore.systemPagination.page >= logsStore.systemTotalPages"
            class="btn-secondary text-sm disabled:opacity-40"
          >Siguiente</button>
        </div>
      </div>
    </template>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, watch } from 'vue'
import { useLogsStore } from '../stores/logs.js'

const logsStore = useLogsStore()
const activeTab = ref('interactions')

// Interaction filters
const iFilters = reactive({
  call_id: '',
  direction: '',
  event_type: '',
  from: '',
  to: '',
})

// System filters
const sFilters = reactive({
  level: '',
  from: '',
  to: '',
})

function applyInteractionFilters() {
  logsStore.fetchInteractionLogs({
    page: 1,
    call_id: iFilters.call_id,
    direction: iFilters.direction,
    event_type: iFilters.event_type,
    from: iFilters.from,
    to: iFilters.to ? iFilters.to + 'T23:59:59' : '',
  })
}

function clearInteractionFilters() {
  iFilters.call_id = ''
  iFilters.direction = ''
  iFilters.event_type = ''
  iFilters.from = ''
  iFilters.to = ''
  logsStore.fetchInteractionLogs({ page: 1 })
}

function goInteractionPage(page) {
  logsStore.fetchInteractionLogs({
    page,
    call_id: iFilters.call_id,
    direction: iFilters.direction,
    event_type: iFilters.event_type,
    from: iFilters.from,
    to: iFilters.to ? iFilters.to + 'T23:59:59' : '',
  })
}

function applySystemFilters() {
  logsStore.fetchSystemLogs({
    page: 1,
    level: sFilters.level,
    from: sFilters.from,
    to: sFilters.to ? sFilters.to + 'T23:59:59' : '',
  })
}

function clearSystemFilters() {
  sFilters.level = ''
  sFilters.from = ''
  sFilters.to = ''
  logsStore.fetchSystemLogs({ page: 1 })
}

function goSystemPage(page) {
  logsStore.fetchSystemLogs({
    page,
    level: sFilters.level,
    from: sFilters.from,
    to: sFilters.to ? sFilters.to + 'T23:59:59' : '',
  })
}

function formatTime(ts) {
  if (!ts) return '-'
  const d = new Date(ts)
  if (isNaN(d.getTime())) return ts
  return d.toLocaleString('es-ES', {
    day: '2-digit', month: '2-digit', year: 'numeric',
    hour: '2-digit', minute: '2-digit', second: '2-digit',
  })
}

function levelClass(level) {
  if (level === 'error') return 'bg-red-100 text-red-700'
  if (level === 'warn') return 'bg-amber-100 text-amber-700'
  return 'bg-gray-100 text-gray-600'
}

// Load data on tab switch
watch(activeTab, (tab) => {
  if (tab === 'interactions' && !logsStore.interactionLogs.length) {
    logsStore.fetchInteractionLogs({ page: 1 })
  }
  if (tab === 'system' && !logsStore.systemLogs.length) {
    logsStore.fetchSystemLogs({ page: 1 })
  }
})

onMounted(() => {
  logsStore.fetchInteractionLogs({ page: 1 })
})
</script>
