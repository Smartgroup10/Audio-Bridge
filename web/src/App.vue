<template>
  <div v-if="auth.isAuthenticated" class="flex min-h-screen">
    <Sidebar />
    <main class="flex-1 ml-60 p-6 bg-gray-50 min-h-screen">
      <router-view />
    </main>
  </div>
  <div v-else>
    <router-view />
  </div>
  <ToastContainer />
</template>

<script setup>
import { onMounted } from 'vue'
import { useAuthStore } from './stores/auth.js'
import { useCallsStore } from './stores/calls.js'
import Sidebar from './components/Sidebar.vue'
import ToastContainer from './components/ToastContainer.vue'

const auth = useAuthStore()
const calls = useCallsStore()

onMounted(() => {
  if (auth.isAuthenticated) {
    calls.connectSSE()
  }
})
</script>
