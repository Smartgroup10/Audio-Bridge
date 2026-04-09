<template>
  <div class="card border-l-4 cursor-pointer hover:shadow-md transition-shadow" :class="[borderClass, { 'ring-2 ring-blue-400': selected }]" @click="$emit('select', call)">
    <div class="flex items-center justify-between mb-3">
      <div class="flex items-center gap-2">
        <Icon :name="call.direction === 'inbound' ? 'phone-arrow-down-left' : 'phone-arrow-up-right'" class="w-5 h-5" />
        <span class="font-semibold text-gray-800">{{ call.caller_id || 'Desconocido' }}</span>
      </div>
      <span
        class="px-2 py-0.5 rounded-full text-xs font-medium"
        :class="stateClass"
      >
        {{ stateLabel }}
      </span>
    </div>

    <div class="grid grid-cols-2 gap-2 text-sm text-gray-600 mb-3">
      <div>
        <span class="text-gray-400">Notaria:</span>
        {{ call.notaria_id || '-' }}
      </div>
      <div>
        <span class="text-gray-400">Duracion:</span>
        {{ liveDuration }}
      </div>
      <div>
        <span class="text-gray-400">Direccion:</span>
        {{ call.direction === 'inbound' ? 'Entrante' : 'Saliente' }}
      </div>
      <div>
        <span class="text-gray-400">ID:</span>
        {{ shortId }}
      </div>
    </div>

    <!-- Last transcript lines -->
    <div v-if="lastUserLine || lastAILine" class="border-t border-gray-100 pt-2 space-y-1">
      <div v-if="lastUserLine" class="flex gap-2 text-xs">
        <span class="text-blue-500 font-medium flex-shrink-0">Usuario:</span>
        <span class="text-gray-600 truncate">{{ lastUserLine }}</span>
      </div>
      <div v-if="lastAILine" class="flex gap-2 text-xs">
        <span class="text-cyan-500 font-medium flex-shrink-0">IA:</span>
        <span class="text-gray-600 truncate">{{ lastAILine }}</span>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, ref, onMounted, onUnmounted } from 'vue'
import Icon from './Icon.vue'

const props = defineProps({
  call: { type: Object, required: true },
  selected: { type: Boolean, default: false },
})

defineEmits(['select'])

const now = ref(Date.now())
let timer = null

onMounted(() => {
  timer = setInterval(() => { now.value = Date.now() }, 1000)
})
onUnmounted(() => {
  if (timer) clearInterval(timer)
})

const shortId = computed(() => {
  const id = props.call.id || props.call.call_id || ''
  return id.substring(0, 8)
})

const liveDuration = computed(() => {
  const startedAt = props.call.startedAt || props.call.started_at
  if (!startedAt) return '00:00'
  const elapsed = Math.floor((now.value - startedAt) / 1000)
  const mins = Math.floor(elapsed / 60)
  const secs = elapsed % 60
  return `${String(mins).padStart(2, '0')}:${String(secs).padStart(2, '0')}`
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
  return map[props.call.state] || props.call.state || 'Activa'
})

const stateClass = computed(() => {
  const state = props.call.state
  if (state === 'ringing') return 'bg-amber-100 text-amber-700'
  if (state === 'connected' || state === 'streaming') return 'bg-green-100 text-green-700'
  if (state === 'transferring') return 'bg-blue-100 text-blue-700'
  if (state === 'failed') return 'bg-red-100 text-red-700'
  return 'bg-green-100 text-green-700'
})

const borderClass = computed(() => {
  const state = props.call.state
  if (state === 'ringing') return 'border-amber-400'
  if (state === 'connected' || state === 'streaming') return 'border-green-400'
  if (state === 'transferring') return 'border-blue-400'
  if (state === 'failed') return 'border-red-400'
  return 'border-cyan-400'
})

const lastUserLine = computed(() => {
  const t = props.call.transcriptUser || ''
  const lines = t.split('\n').filter(Boolean)
  return lines.length ? lines[lines.length - 1] : ''
})

const lastAILine = computed(() => {
  const t = props.call.transcriptAI || ''
  const lines = t.split('\n').filter(Boolean)
  return lines.length ? lines[lines.length - 1] : ''
})
</script>
