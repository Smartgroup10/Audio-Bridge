<template>
  <div>
    <!-- Back button -->
    <button @click="$router.back()" class="flex items-center gap-1 text-sm text-gray-500 hover:text-gray-700 mb-4">
      \u2190 Volver al historial
    </button>

    <!-- Loading -->
    <div v-if="loading" class="card text-center py-16 text-gray-400">
      Cargando detalle...
    </div>

    <!-- Not found -->
    <div v-else-if="!call" class="card text-center py-16 text-gray-400">
      Llamada no encontrada
    </div>

    <template v-else>
      <!-- Header Card -->
      <div class="card mb-6">
        <div class="flex items-center justify-between mb-4">
          <h1 class="text-xl font-bold text-gray-800">
            Detalle de Llamada
          </h1>
          <span
            class="px-3 py-1 rounded-full text-sm font-medium"
            :class="stateClass"
          >
            {{ stateLabel }}
          </span>
        </div>

        <div class="grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
          <div>
            <p class="text-gray-400 text-xs mb-0.5">ID</p>
            <p class="font-mono text-gray-700">{{ call.id?.substring(0, 12) }}...</p>
          </div>
          <div>
            <p class="text-gray-400 text-xs mb-0.5">Llamante</p>
            <p class="font-medium text-gray-800">{{ call.caller_id || '-' }}</p>
          </div>
          <div>
            <p class="text-gray-400 text-xs mb-0.5">Notaria</p>
            <p class="text-gray-700">{{ call.notaria_id || '-' }}</p>
          </div>
          <div>
            <p class="text-gray-400 text-xs mb-0.5">DDI</p>
            <p class="text-gray-700">{{ call.ddi || '-' }}</p>
          </div>
          <div>
            <p class="text-gray-400 text-xs mb-0.5">Direccion</p>
            <p>
              <span
                class="inline-flex px-2 py-0.5 rounded-full text-xs font-medium"
                :class="call.direction === 'inbound' ? 'bg-blue-100 text-blue-700' : 'bg-cyan-100 text-cyan-700'"
              >
                {{ call.direction === 'inbound' ? 'Entrante' : 'Saliente' }}
              </span>
            </p>
          </div>
          <div>
            <p class="text-gray-400 text-xs mb-0.5">Duracion</p>
            <p class="text-gray-700">{{ formatDuration(call.duration_seconds) }}</p>
          </div>
          <div>
            <p class="text-gray-400 text-xs mb-0.5">Inicio</p>
            <p class="text-gray-700 text-xs">{{ formatDate(call.start_time) }}</p>
          </div>
          <div>
            <p class="text-gray-400 text-xs mb-0.5">Motivo Fin</p>
            <p class="text-gray-700">{{ call.end_reason || '-' }}</p>
          </div>
        </div>

        <div v-if="call.transfer_dest" class="mt-3 pt-3 border-t border-gray-100 text-sm">
          <span class="text-gray-400">Transferida a:</span>
          <span class="font-medium text-gray-700 ml-1">{{ call.transfer_dest }}</span>
        </div>
      </div>

      <!-- Transcripts -->
      <div class="grid grid-cols-1 lg:grid-cols-2 gap-6 mb-6">
        <!-- User transcript -->
        <div class="card">
          <h2 class="text-lg font-semibold text-gray-800 mb-3 flex items-center gap-2">
            <span class="w-2 h-2 bg-blue-500 rounded-full"></span>
            Transcripcion Usuario
          </h2>
          <div
            v-if="userTranscriptLines.length"
            class="space-y-2 max-h-96 overflow-y-auto"
          >
            <div
              v-for="(line, i) in userTranscriptLines"
              :key="'u' + i"
              class="text-sm text-gray-700 bg-blue-50 rounded-lg px-3 py-2"
            >
              {{ line }}
            </div>
          </div>
          <p v-else class="text-sm text-gray-400">Sin transcripcion disponible</p>
        </div>

        <!-- AI transcript -->
        <div class="card">
          <h2 class="text-lg font-semibold text-gray-800 mb-3 flex items-center gap-2">
            <span class="w-2 h-2 bg-cyan-400 rounded-full"></span>
            Transcripcion IA
          </h2>
          <div
            v-if="aiTranscriptLines.length"
            class="space-y-2 max-h-96 overflow-y-auto"
          >
            <div
              v-for="(line, i) in aiTranscriptLines"
              :key="'a' + i"
              class="text-sm text-gray-700 bg-cyan-50 rounded-lg px-3 py-2"
            >
              {{ line }}
            </div>
          </div>
          <p v-else class="text-sm text-gray-400">Sin transcripcion disponible</p>
        </div>
      </div>

      <!-- Interaction Logs -->
      <div v-if="logs.length" class="card mb-6">
        <h2 class="text-lg font-semibold text-gray-800 mb-3">Registro de Interacciones</h2>
        <div class="space-y-2 max-h-80 overflow-y-auto">
          <div
            v-for="log in logs"
            :key="log.id"
            class="flex gap-3 text-sm border-b border-gray-50 pb-2"
          >
            <span class="text-xs text-gray-400 whitespace-nowrap flex-shrink-0">
              {{ formatTime(log.timestamp) }}
            </span>
            <span
              class="px-1.5 py-0.5 rounded text-xs font-medium flex-shrink-0"
              :class="log.direction === 'user' ? 'bg-blue-100 text-blue-700' : 'bg-cyan-100 text-cyan-700'"
            >
              {{ log.direction === 'user' ? 'USR' : 'IA' }}
            </span>
            <span
              class="px-1.5 py-0.5 rounded text-xs bg-gray-100 text-gray-600 flex-shrink-0"
            >
              {{ log.event_type }}
            </span>
            <span class="text-gray-700">{{ log.content }}</span>
          </div>
        </div>
      </div>

      <!-- Recordings -->
      <div v-if="hasRecordings" class="card">
        <h2 class="text-lg font-semibold text-gray-800 mb-4">Grabaciones</h2>
        <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
          <AudioPlayer
            v-if="call.recording_caller"
            :call-id="call.id"
            channel="caller"
            label="Audio Llamante"
          />
          <AudioPlayer
            v-if="call.recording_ai"
            :call-id="call.id"
            channel="ai"
            label="Audio IA"
          />
        </div>
      </div>
    </template>
  </div>
</template>

<script setup>
import { computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useCallsStore } from '../stores/calls.js'
import AudioPlayer from '../components/AudioPlayer.vue'

const route = useRoute()
const callsStore = useCallsStore()

const call = computed(() => callsStore.currentCall)
const logs = computed(() => callsStore.currentCallLogs)
const loading = computed(() => callsStore.loading)

const hasRecordings = computed(() => {
  if (!call.value) return false
  return !!(call.value.recording_caller || call.value.recording_ai)
})

const userTranscriptLines = computed(() => {
  if (!call.value?.transcript_user) return []
  return call.value.transcript_user.split('\n').filter(Boolean)
})

const aiTranscriptLines = computed(() => {
  if (!call.value?.transcript_ai) return []
  return call.value.transcript_ai.split('\n').filter(Boolean)
})

const stateLabel = computed(() => {
  const map = {
    ringing: 'Sonando',
    connected: 'Conectada',
    streaming: 'En curso',
    transferring: 'Transfiriendo',
    completed: 'Completada',
    failed: 'Fallida',
  }
  return map[call.value?.state] || call.value?.state || '-'
})

const stateClass = computed(() => {
  const s = call.value?.state
  if (s === 'completed') return 'bg-green-100 text-green-700'
  if (s === 'failed') return 'bg-red-100 text-red-700'
  return 'bg-gray-100 text-gray-700'
})

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
    second: '2-digit',
  })
}

function formatTime(dateStr) {
  if (!dateStr) return ''
  const d = new Date(dateStr)
  if (isNaN(d.getTime())) return dateStr
  return d.toLocaleTimeString('es-ES', { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

onMounted(() => {
  const callId = route.params.id
  if (callId) {
    callsStore.fetchCallDetail(callId)
  }
})
</script>
