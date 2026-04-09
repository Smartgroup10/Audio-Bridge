<template>
  <div class="min-h-screen bg-navy-800 flex items-center justify-center p-4">
    <div class="w-full max-w-sm">
      <!-- Logo -->
      <div class="text-center mb-8">
        <Icon name="headphones" class="w-16 h-16 text-cyan-400 mx-auto mb-3" />
        <h1 class="text-white text-2xl font-bold">Audio Bridge</h1>
        <p class="text-cyan-400 text-sm mt-1">Panel de Administracion</p>
      </div>

      <!-- Login Card -->
      <form @submit.prevent="handleLogin" class="bg-white rounded-xl shadow-lg p-6">
        <h2 class="text-lg font-semibold text-gray-800 mb-4">Acceso Administrador</h2>

        <div class="mb-4">
          <label for="password" class="block text-sm font-medium text-gray-600 mb-1">
            Contrasena
          </label>
          <input
            id="password"
            v-model="password"
            type="password"
            class="input-field"
            placeholder="Introduce la contrasena"
            autofocus
            required
          />
        </div>

        <div v-if="auth.error" class="mb-4 p-3 bg-red-50 border border-red-200 rounded-lg text-sm text-red-600">
          {{ auth.error }}
        </div>

        <button
          type="submit"
          class="btn-primary w-full flex items-center justify-center gap-2"
          :disabled="auth.loading || !password"
        >
          <svg
            v-if="auth.loading"
            class="animate-spin h-4 w-4"
            xmlns="http://www.w3.org/2000/svg"
            fill="none"
            viewBox="0 0 24 24"
          >
            <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
            <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
          </svg>
          {{ auth.loading ? 'Accediendo...' : 'Acceder' }}
        </button>
      </form>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import Icon from '../components/Icon.vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth.js'
import { useCallsStore } from '../stores/calls.js'

const router = useRouter()
const auth = useAuthStore()
const calls = useCallsStore()
const password = ref('')

async function handleLogin() {
  const success = await auth.login(password.value)
  if (success) {
    calls.connectSSE()
    router.push({ name: 'Dashboard' })
  }
}
</script>
