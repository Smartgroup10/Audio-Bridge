<template>
  <div>
    <h1 class="text-2xl font-bold text-gray-800 mb-6">Configuracion</h1>

    <div v-if="loading" class="card text-center py-12 text-gray-400">
      Cargando configuracion...
    </div>

    <div v-else class="grid grid-cols-1 lg:grid-cols-2 gap-6">
      <!-- Editable Settings -->
      <div class="card">
        <h2 class="text-lg font-semibold text-gray-800 mb-4">Ajustes Editables</h2>

        <form @submit.prevent="saveConfig" class="space-y-5">
          <!-- Recording toggle -->
          <div>
            <label class="block text-sm font-medium text-gray-600 mb-2">Grabacion</label>
            <div class="flex items-center gap-3">
              <label class="relative inline-flex items-center cursor-pointer">
                <input v-model="editableConfig.recording.enabled" type="checkbox" class="sr-only peer" />
                <div class="w-11 h-6 bg-gray-200 peer-focus:outline-none peer-focus:ring-2 peer-focus:ring-blue-300 rounded-full peer peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-gray-300 after:border after:rounded-full after:h-5 after:w-5 after:transition-all peer-checked:bg-blue-500"></div>
              </label>
              <span class="text-sm text-gray-700">
                {{ editableConfig.recording.enabled ? 'Habilitada' : 'Deshabilitada' }}
              </span>
            </div>
          </div>

          <!-- Log level -->
          <div>
            <label class="block text-sm font-medium text-gray-600 mb-1">Nivel de Log</label>
            <select v-model="editableConfig.logging.level" class="input-field">
              <option value="debug">Debug</option>
              <option value="info">Info</option>
              <option value="warn">Warn</option>
              <option value="error">Error</option>
            </select>
          </div>

          <div class="pt-2">
            <button type="submit" class="btn-primary" :disabled="saving">
              {{ saving ? 'Guardando...' : 'Guardar Cambios' }}
            </button>
          </div>

          <div v-if="saveMessage" class="p-3 rounded-lg text-sm" :class="saveError ? 'bg-red-50 text-red-600' : 'bg-green-50 text-green-600'">
            {{ saveMessage }}
          </div>
        </form>
      </div>

      <!-- Read-only Config -->
      <div class="card">
        <h2 class="text-lg font-semibold text-gray-800 mb-4">Configuracion Actual (Solo Lectura)</h2>

        <div class="space-y-4">
          <!-- Server -->
          <div>
            <h3 class="text-sm font-semibold text-gray-500 mb-2">Servidor</h3>
            <div class="bg-gray-50 rounded-lg p-3 space-y-1 text-sm">
              <div class="flex justify-between">
                <span class="text-gray-500">AudioSocket</span>
                <span class="font-mono text-gray-700">{{ config?.server?.audiosocket_addr || '-' }}</span>
              </div>
              <div class="flex justify-between">
                <span class="text-gray-500">Max Concurrentes</span>
                <span class="font-mono text-gray-700">{{ config?.server?.max_concurrent || '-' }}</span>
              </div>
            </div>
          </div>

          <!-- AI -->
          <div>
            <h3 class="text-sm font-semibold text-gray-500 mb-2">IA</h3>
            <div class="bg-gray-50 rounded-lg p-3 space-y-1 text-sm">
              <div class="flex justify-between">
                <span class="text-gray-500">Tipo</span>
                <span class="font-mono text-gray-700">{{ config?.ai?.type || '-' }}</span>
              </div>
              <div class="flex justify-between">
                <span class="text-gray-500">Modelo</span>
                <span class="font-mono text-gray-700">{{ config?.ai?.model || '-' }}</span>
              </div>
              <div class="flex justify-between">
                <span class="text-gray-500">Voz</span>
                <span class="font-mono text-gray-700">{{ config?.ai?.voice || '-' }}</span>
              </div>
              <div class="flex justify-between">
                <span class="text-gray-500">Idioma</span>
                <span class="font-mono text-gray-700">{{ config?.ai?.language || '-' }}</span>
              </div>
              <div class="flex justify-between">
                <span class="text-gray-500">Reintentos Originate</span>
                <span class="font-mono text-gray-700">{{ config?.ai?.originate_retries || '-' }}</span>
              </div>
            </div>
          </div>

          <!-- Audio -->
          <div>
            <h3 class="text-sm font-semibold text-gray-500 mb-2">Audio</h3>
            <div class="bg-gray-50 rounded-lg p-3 space-y-1 text-sm">
              <div class="flex justify-between">
                <span class="text-gray-500">Sample Rate</span>
                <span class="font-mono text-gray-700">{{ config?.audio?.sample_rate || '-' }} Hz</span>
              </div>
              <div class="flex justify-between">
                <span class="text-gray-500">Bit Depth</span>
                <span class="font-mono text-gray-700">{{ config?.audio?.bit_depth || '-' }}</span>
              </div>
              <div class="flex justify-between">
                <span class="text-gray-500">Canales</span>
                <span class="font-mono text-gray-700">{{ config?.audio?.channels || '-' }}</span>
              </div>
              <div class="flex justify-between">
                <span class="text-gray-500">Codec</span>
                <span class="font-mono text-gray-700">{{ config?.audio?.codec || '-' }}</span>
              </div>
              <div class="flex justify-between">
                <span class="text-gray-500">Frame Size</span>
                <span class="font-mono text-gray-700">{{ config?.audio?.frame_size_ms || '-' }} ms</span>
              </div>
            </div>
          </div>

          <!-- Recording -->
          <div>
            <h3 class="text-sm font-semibold text-gray-500 mb-2">Grabacion</h3>
            <div class="bg-gray-50 rounded-lg p-3 space-y-1 text-sm">
              <div class="flex justify-between">
                <span class="text-gray-500">Habilitada</span>
                <span class="font-mono text-gray-700">{{ config?.recording?.enabled ? 'Si' : 'No' }}</span>
              </div>
              <div class="flex justify-between">
                <span class="text-gray-500">Ruta</span>
                <span class="font-mono text-gray-700 text-xs">{{ config?.recording?.path || '-' }}</span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import api from '../api.js'

const config = ref(null)
const loading = ref(true)
const saving = ref(false)
const saveMessage = ref('')
const saveError = ref(false)

const editableConfig = reactive({
  recording: { enabled: false },
  logging: { level: 'info' },
})

async function loadConfig() {
  loading.value = true
  try {
    const { data } = await api.get('/api/v1/admin/config')
    config.value = data
    editableConfig.recording.enabled = data.recording?.enabled || false
    editableConfig.logging.level = data.logging?.level || 'info'
  } catch (err) {
    console.error('Error loading config:', err)
  } finally {
    loading.value = false
  }
}

async function saveConfig() {
  saving.value = true
  saveMessage.value = ''
  saveError.value = false
  try {
    await api.put('/api/v1/admin/config', {
      recording: { enabled: editableConfig.recording.enabled },
      logging: { level: editableConfig.logging.level },
    })
    saveMessage.value = 'Configuracion guardada correctamente'
    // Refresh config to reflect changes
    await loadConfig()
  } catch (err) {
    saveError.value = true
    saveMessage.value = err.response?.data?.error || 'Error guardando configuracion'
  } finally {
    saving.value = false
  }
}

onMounted(() => {
  loadConfig()
})
</script>
