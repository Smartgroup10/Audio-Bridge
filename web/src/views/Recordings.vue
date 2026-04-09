<template>
  <div>
    <h1 class="text-2xl font-bold text-gray-800 mb-6">Grabaciones</h1>

    <!-- Table -->
    <div class="card overflow-hidden p-0">
      <div class="overflow-x-auto">
        <table class="w-full text-sm">
          <thead>
            <tr class="bg-gray-50 border-b border-gray-200">
              <th class="text-left px-4 py-3 font-medium text-gray-500">ID</th>
              <th class="text-left px-4 py-3 font-medium text-gray-500">Llamante</th>
              <th class="text-left px-4 py-3 font-medium text-gray-500">Notaria</th>
              <th class="text-left px-4 py-3 font-medium text-gray-500">Duracion</th>
              <th class="text-left px-4 py-3 font-medium text-gray-500">Fecha</th>
              <th class="text-left px-4 py-3 font-medium text-gray-500">Audio Llamante</th>
              <th class="text-left px-4 py-3 font-medium text-gray-500">Audio IA</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="call in recordings"
              :key="call.id"
              class="border-b border-gray-100"
            >
              <td class="px-4 py-3">
                <router-link
                  :to="{ name: 'CallDetail', params: { id: call.id } }"
                  class="font-mono text-xs text-blue-500 hover:text-blue-700 hover:underline"
                >
                  {{ call.id?.substring(0, 8) }}
                </router-link>
              </td>
              <td class="px-4 py-3 font-medium">{{ call.caller_id || '-' }}</td>
              <td class="px-4 py-3">{{ call.notaria_id || '-' }}</td>
              <td class="px-4 py-3">{{ formatDuration(call.duration_seconds) }}</td>
              <td class="px-4 py-3 text-gray-500 text-xs whitespace-nowrap">{{ formatDate(call.start_time) }}</td>
              <td class="px-4 py-3">
                <AudioPlayer
                  v-if="call.recording_caller"
                  :call-id="call.id"
                  channel="caller"
                  label=""
                />
                <span v-else class="text-gray-400 text-xs">-</span>
              </td>
              <td class="px-4 py-3">
                <AudioPlayer
                  v-if="call.recording_ai"
                  :call-id="call.id"
                  channel="ai"
                  label=""
                />
                <span v-else class="text-gray-400 text-xs">-</span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-if="!recordings.length && !loading" class="text-center py-12 text-gray-400">
        No hay grabaciones disponibles
      </div>

      <div v-if="loading" class="text-center py-12 text-gray-400">
        Cargando...
      </div>
    </div>

    <!-- Pagination -->
    <div v-if="totalPages > 1" class="flex items-center justify-between mt-4">
      <p class="text-sm text-gray-500">
        Pagina {{ page }} de {{ totalPages }} ({{ total }} grabaciones)
      </p>
      <div class="flex gap-2">
        <button
          @click="loadPage(page - 1)"
          :disabled="page <= 1"
          class="btn-secondary text-sm disabled:opacity-40"
        >
          Anterior
        </button>
        <button
          @click="loadPage(page + 1)"
          :disabled="page >= totalPages"
          class="btn-secondary text-sm disabled:opacity-40"
        >
          Siguiente
        </button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import api from '../api.js'
import AudioPlayer from '../components/AudioPlayer.vue'

const recordings = ref([])
const loading = ref(false)
const page = ref(1)
const total = ref(0)
const limit = 20

const totalPages = computed(() => Math.ceil(total.value / limit) || 1)

async function loadPage(p = 1) {
  if (p < 1 || p > totalPages.value) return
  loading.value = true
  page.value = p
  try {
    // We use the calls endpoint with a filter for calls that have recordings.
    // The backend ListCalls returns all calls, so we filter client-side or
    // use a dedicated approach. Since the DB has ListRecordings, we'll
    // call the regular calls endpoint and filter.
    // Actually, looking at the backend, there's no separate recordings endpoint
    // for listing. We'll use the call history and filter those with recordings.
    const { data } = await api.get('/api/v1/admin/calls', {
      params: { page: p, limit },
    })
    // Filter calls that have recordings
    recordings.value = (data.calls || []).filter(
      c => c.recording_caller || c.recording_ai
    )
    total.value = data.total || 0
  } catch (err) {
    console.error('Error loading recordings:', err)
  } finally {
    loading.value = false
  }
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

onMounted(() => {
  loadPage(1)
})
</script>
