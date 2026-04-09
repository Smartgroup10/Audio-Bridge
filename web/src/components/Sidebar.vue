<template>
  <aside class="fixed left-0 top-0 h-full w-60 bg-navy-800 text-gray-300 flex flex-col z-40">
    <!-- Logo -->
    <div class="px-5 py-6 border-b border-white/10">
      <div class="flex items-center gap-3">
        <Icon name="headphones" class="w-8 h-8 text-cyan-400" />
        <div>
          <h1 class="text-white font-bold text-lg leading-tight">Audio Bridge</h1>
          <p class="text-cyan-400 text-xs font-medium">Panel de Administracion</p>
        </div>
      </div>
    </div>

    <!-- Navigation -->
    <nav class="flex-1 py-4 overflow-y-auto">
      <ul class="space-y-1 px-3">
        <li v-for="item in navItems" :key="item.to">
          <router-link
            :to="item.to"
            class="flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium transition-colors duration-200"
            :class="isActive(item.to)
              ? 'bg-blue-500/20 text-white'
              : 'hover:bg-white/5 hover:text-white'"
          >
            <Icon :name="item.icon" class="w-5 h-5 shrink-0" />
            <span>{{ item.label }}</span>
            <span
              v-if="item.badge"
              class="ml-auto bg-cyan-400 text-navy-800 text-xs font-bold px-1.5 py-0.5 rounded-full"
            >
              {{ item.badge }}
            </span>
          </router-link>
        </li>
      </ul>
    </nav>

    <!-- SSE Status -->
    <div class="px-5 py-2 border-t border-white/10">
      <div class="flex items-center gap-2 text-xs">
        <span
          class="w-2 h-2 rounded-full"
          :class="{
            'bg-green-400 animate-pulse': calls.sseStatus === 'connected',
            'bg-amber-400 animate-pulse': calls.sseStatus === 'connecting',
            'bg-red-400': calls.sseStatus === 'disconnected',
          }"
        ></span>
        <span :class="{
          'text-green-400': calls.sseStatus === 'connected',
          'text-amber-400': calls.sseStatus === 'connecting',
          'text-red-400': calls.sseStatus === 'disconnected',
        }">
          {{ calls.sseStatus === 'connected' ? 'En vivo' : calls.sseStatus === 'connecting' ? 'Conectando...' : 'Desconectado' }}
        </span>
      </div>
    </div>

    <!-- Footer / Logout -->
    <div class="px-3 py-4 border-t border-white/10">
      <button
        @click="handleLogout"
        class="flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium w-full hover:bg-red-500/20 hover:text-red-300 transition-colors duration-200"
      >
        <Icon name="arrow-right-on-rectangle" class="w-5 h-5 shrink-0" />
        <span>Cerrar Sesion</span>
      </button>
    </div>
  </aside>
</template>

<script setup>
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth.js'
import { useCallsStore } from '../stores/calls.js'
import Icon from './Icon.vue'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const calls = useCallsStore()

const activeCallCount = computed(() => calls.activeCalls.length || null)

const navItems = computed(() => [
  { to: '/', icon: 'chart-bar', label: 'Dashboard' },
  { to: '/llamadas-activas', icon: 'phone', label: 'Llamadas Activas', badge: activeCallCount.value },
  { to: '/historial', icon: 'clock', label: 'Historial' },
  { to: '/notarias', icon: 'building-office', label: 'Notarias' },
  { to: '/grabaciones', icon: 'microphone', label: 'Grabaciones' },
  { to: '/logs', icon: 'document-text', label: 'Logs' },
  { to: '/configuracion', icon: 'cog-6-tooth', label: 'Configuracion' },
])

function isActive(path) {
  if (path === '/') return route.path === '/'
  return route.path.startsWith(path)
}

function handleLogout() {
  calls.disconnectSSE()
  auth.logout()
  router.push({ name: 'Login' })
}
</script>
