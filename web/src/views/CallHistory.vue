<template>
  <div>
    <h1 class="text-2xl font-bold text-gray-800 mb-6">Historial de Llamadas</h1>

    <!-- Filters -->
    <div class="card mb-6">
      <div class="flex flex-wrap gap-4 items-end">
        <div class="flex-1 min-w-[180px]">
          <label class="block text-xs font-medium text-gray-500 mb-1">Notaria</label>
          <select v-model="filters.notaria_id" @change="applyFilters" class="input-field">
            <option value="">Todas</option>
            <option v-for="t in tenants" :key="t.notaria_id" :value="t.notaria_id">
              {{ t.name || t.notaria_id }}
            </option>
          </select>
        </div>
        <div class="min-w-[160px]">
          <label class="block text-xs font-medium text-gray-500 mb-1">Desde</label>
          <input v-model="filters.from" type="date" class="input-field" @change="applyFilters" />
        </div>
        <div class="min-w-[160px]">
          <label class="block text-xs font-medium text-gray-500 mb-1">Hasta</label>
          <input v-model="filters.to" type="date" class="input-field" @change="applyFilters" />
        </div>
        <button @click="clearFilters" class="btn-secondary text-sm">
          Limpiar
        </button>
      </div>
    </div>

    <!-- Table -->
    <div class="card overflow-hidden p-0">
      <div class="overflow-x-auto">
        <table class="w-full text-sm">
          <thead>
            <tr class="bg-gray-50 border-b border-gray-200">
              <th class="text-left px-4 py-3 font-medium text-gray-500">ID</th>
              <th class="text-left px-4 py-3 font-medium text-gray-500">Llamante</th>
              <th class="text-left px-4 py-3 font-medium text-gray-500">Notaria</th>
              <th class="text-left px-4 py-3 font-medium text-gray-500">Direccion</th>
              <th class="text-left px-4 py-3 font-medium text-gray-500">Duracion</th>
              <th class="text-left px-4 py-3 font-medium text-gray-500">Estado</th>
              <th class="text-left px-4 py-3 font-medium text-gray-500">Motivo</th>
              <th class="text-left px-4 py-3 font-medium text-gray-500">Fecha</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="call in callHistory"
              :key="call.id"
              @click="goToDetail(call.id)"
              class="border-b border-gray-100 hover:bg-blue-50/50 cursor-pointer transition-colors"
            >
              <td class="px-4 py-3 font-mono text-xs text-gray-500">{{ call.id?.substring(0, 8) }}</td>
              <td class="px-4 py-3 font-medium">{{ call.caller_id || '-' }}</td>
              <td class="px-4 py-3">{{ call.notaria_id || '-' }}</td>
              <td class="px-4 py-3">
                <span
                  class="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium"
                  :class="call.direction === 'inbound'
                    ? 'bg-blue-100 text-blue-700'
                    : 'bg-cyan-100 text-cyan-700'"
                >
                  {{ call.direction === 'inbound' ? 'Entrante' : 'Saliente' }}
                </span>
              </td>
              <td class="px-4 py-3">{{ formatDuration(call.duration_seconds) }}</td>
              <td class="px-4 py-3">
                <span
                  class="inline-flex px-2 py-0.5 rounded-full text-xs font-medium"
                  :class="stateClass(call.state)"
                >
                  {{ stateLabel(call.state) }}
                </span>
              </td>
              <td class="px-4 py-3 text-gray-500 text-xs">{{ call.end_reason || '-' }}</td>
              <td class="px-4 py-3 text-gray-500 text-xs whitespace-nowrap">{{ formatDate(call.start_time) }}</td>
            </tr>
          </tbody>
        </table>
      </div>

      <!-- Empty state -->
      <div v-if="!callHistory.length && !loading" class="text-center py-12 text-gray-400">
        No se encontraron llamadas
      </div>

      <!-- Loading -->
      <div v-if="loading" class="text-center py-12 text-gray-400">
        Cargando...
      </div>
    </div>

    <!-- Pagination -->
    <div v-if="totalPages > 1" class="flex items-center justify-between mt-4">
      <p class="text-sm text-gray-500">
        Pagina {{ pagination.page }} de {{ totalPages }} ({{ pagination.total }} resultados)
      </p>
      <div class="flex gap-2">
        <button
          @click="goToPage(pagination.page - 1)"
          :disabled="pagination.page <= 1"
          class="btn-secondary text-sm disabled:opacity-40"
        >
          Anterior
        </button>
        <button
          @click="goToPage(pagination.page + 1)"
          :disabled="pagination.page >= totalPages"
          class="btn-secondary text-sm disabled:opacity-40"
        >
          Siguiente
        </button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, reactive } from 'vue'
import { useRouter } from 'vue-router'
import { useCallsStore } from '../stores/calls.js'
import { useTenantsStore } from '../stores/tenants.js'

const router = useRouter()
const callsStore = useCallsStore()
const tenantsStore = useTenantsStore()

const filters = reactive({
  notaria_id: '',
  from: '',
  to: '',
})

const callHistory = computed(() => callsStore.callHistory)
const pagination = computed(() => callsStore.pagination)
const totalPages = computed(() => callsStore.totalPages)
const loading = computed(() => callsStore.loading)
const tenants = computed(() => tenantsStore.tenants)

function applyFilters() {
  callsStore.fetchHistory({
    page: 1,
    notaria_id: filters.notaria_id,
    from: filters.from,
    to: filters.to ? filters.to + 'T23:59:59' : '',
  })
}

function clearFilters() {
  filters.notaria_id = ''
  filters.from = ''
  filters.to = ''
  callsStore.fetchHistory({ page: 1 })
}

function goToPage(page) {
  callsStore.fetchHistory({
    page,
    notaria_id: filters.notaria_id,
    from: filters.from,
    to: filters.to ? filters.to + 'T23:59:59' : '',
  })
}

function goToDetail(id) {
  router.push({ name: 'CallDetail', params: { id } })
}

function formatDuration(seconds) {
  if (!seconds || seconds === 0) return '0s'
  const mins = Math.floor(seconds / 60)
  const secs = Math.round(seconds % 60)
  if (mins === 0) return `${secs}s`
  return `${mins}m ${secs}s`
}

function formatDate(dateStr) {
  if (!dateStr) return '-'
  const d = new Date(dateStr)
  if (isNaN(d.getTime())) return dateStr
  return d.toLocaleString('es-ES', {
    day: '2-digit',
    month: '2-digit',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function stateLabel(state) {
  const map = {
    ringing: 'Sonando',
    connected: 'Conectada',
    streaming: 'En curso',
    transferring: 'Transfiriendo',
    completed: 'Completada',
    failed: 'Fallida',
  }
  return map[state] || state || '-'
}

function stateClass(state) {
  if (state === 'completed') return 'bg-green-100 text-green-700'
  if (state === 'failed') return 'bg-red-100 text-red-700'
  if (state === 'ringing') return 'bg-amber-100 text-amber-700'
  return 'bg-gray-100 text-gray-700'
}

onMounted(() => {
  callsStore.fetchHistory({ page: 1 })
  tenantsStore.fetchTenants()
})
</script>
