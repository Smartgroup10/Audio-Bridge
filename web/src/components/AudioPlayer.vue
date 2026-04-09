<template>
  <div class="bg-gray-50 rounded-lg p-4 border border-gray-200">
    <p class="text-sm font-medium text-gray-700 mb-2">{{ label }}</p>
    <div v-if="loading" class="flex items-center gap-2 text-sm text-gray-400 h-10">
      <Icon name="signal" class="w-5 h-5 animate-spin" /> Cargando audio...
    </div>
    <div v-else-if="error" class="text-sm text-red-400 h-10 flex items-center">
      {{ error }}
    </div>
    <audio
      v-else-if="blobUrl"
      controls
      preload="none"
      class="w-full h-10"
      :src="blobUrl"
    >
      Tu navegador no soporta la reproduccion de audio.
    </audio>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import Icon from './Icon.vue'
import api from '../api.js'

const props = defineProps({
  callId: { type: String, required: true },
  channel: { type: String, required: true },
  label: { type: String, default: 'Audio' },
})

const blobUrl = ref(null)
const loading = ref(true)
const error = ref(null)

onMounted(async () => {
  try {
    const response = await api.get(
      `/api/v1/admin/recordings/${props.callId}/${props.channel}`,
      { responseType: 'blob' }
    )
    blobUrl.value = URL.createObjectURL(response.data)
  } catch (err) {
    error.value = err.response?.status === 404
      ? 'Grabacion no disponible'
      : 'Error al cargar audio'
  } finally {
    loading.value = false
  }
})

onUnmounted(() => {
  if (blobUrl.value) {
    URL.revokeObjectURL(blobUrl.value)
  }
})
</script>
