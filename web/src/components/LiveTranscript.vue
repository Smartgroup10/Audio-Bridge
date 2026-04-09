<template>
  <div class="card flex flex-col h-full">
    <!-- Header -->
    <div class="flex items-center justify-between pb-4 border-b border-gray-200">
      <div>
        <h2 class="text-lg font-bold text-gray-800">Transcripcion en Vivo</h2>
        <div class="flex items-center gap-3 text-sm text-gray-500 mt-1">
          <span class="font-mono">{{ call.caller_id || 'Desconocido' }}</span>
          <span v-if="call.notaria_id" class="text-gray-300">|</span>
          <span v-if="call.notaria_id">{{ call.notaria_id }}</span>
          <span class="text-gray-300">|</span>
          <span class="text-green-600 font-medium">{{ liveDuration }}</span>
        </div>
      </div>
      <button
        @click="$emit('close')"
        class="text-gray-400 hover:text-gray-600 hover:bg-gray-100 rounded-lg p-2 transition"
      >
        <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
        </svg>
      </button>
    </div>

    <!-- Messages -->
    <div ref="messagesContainer" class="flex-1 overflow-y-auto py-4 space-y-3 min-h-0">
      <div v-if="!messages.length" class="text-center text-gray-400 text-sm py-8">
        Esperando transcripcion...
      </div>

      <div
        v-for="(msg, i) in messages"
        :key="i"
        class="flex" :class="msg.role === 'user' ? 'justify-start' : 'justify-end'"
      >
        <div
          class="max-w-[80%] rounded-2xl px-4 py-2 text-sm"
          :class="msg.role === 'user'
            ? 'bg-blue-50 text-blue-900 rounded-bl-md'
            : 'bg-cyan-50 text-cyan-900 rounded-br-md'"
        >
          <div class="flex items-center gap-2 mb-1">
            <span class="text-xs font-semibold" :class="msg.role === 'user' ? 'text-blue-500' : 'text-cyan-500'">
              {{ msg.role === 'user' ? 'Usuario' : 'IA' }}
            </span>
            <span class="text-xs text-gray-400">{{ formatTime(msg.time) }}</span>
          </div>
          <p class="whitespace-pre-wrap">{{ msg.text }}</p>
        </div>
      </div>

      <!-- Scroll anchor -->
      <div ref="scrollAnchor" />
    </div>

    <!-- Live indicator -->
    <div class="pt-3 border-t border-gray-200 flex items-center gap-2 text-xs text-gray-400">
      <span class="relative flex h-2 w-2">
        <span class="animate-ping absolute inline-flex h-full w-full rounded-full bg-green-400 opacity-75"></span>
        <span class="relative inline-flex rounded-full h-2 w-2 bg-green-500"></span>
      </span>
      En vivo &mdash; {{ messages.length }} mensaje{{ messages.length !== 1 ? 's' : '' }}
    </div>
  </div>
</template>

<script setup>
import { computed, ref, watch, nextTick, onMounted, onUnmounted } from 'vue'

const props = defineProps({
  call: { type: Object, required: true },
})

defineEmits(['close'])

const messagesContainer = ref(null)
const scrollAnchor = ref(null)

const now = ref(Date.now())
let timer = null

onMounted(() => {
  timer = setInterval(() => { now.value = Date.now() }, 1000)
  scrollToBottom()
})

onUnmounted(() => {
  if (timer) clearInterval(timer)
})

const messages = computed(() => {
  return props.call.transcriptMessages || []
})

const liveDuration = computed(() => {
  const startedAt = props.call.startedAt || props.call.started_at
  if (!startedAt) return '00:00'
  const elapsed = Math.floor((now.value - startedAt) / 1000)
  const mins = Math.floor(elapsed / 60)
  const secs = elapsed % 60
  return `${String(mins).padStart(2, '0')}:${String(secs).padStart(2, '0')}`
})

function formatTime(timestamp) {
  if (!timestamp) return ''
  const d = new Date(timestamp)
  return d.toLocaleTimeString('es-ES', { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

function scrollToBottom() {
  nextTick(() => {
    scrollAnchor.value?.scrollIntoView({ behavior: 'smooth' })
  })
}

// Auto-scroll when new messages arrive
watch(() => messages.value.length, () => {
  scrollToBottom()
})
</script>
