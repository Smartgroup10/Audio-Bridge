<template>
  <div>
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-2xl font-bold text-gray-800">Llamadas Activas</h1>
      <span
        v-if="activeCalls.length"
        class="bg-green-100 text-green-700 text-sm font-semibold px-3 py-1 rounded-full"
      >
        {{ activeCalls.length }} activa{{ activeCalls.length !== 1 ? 's' : '' }}
      </span>
    </div>

    <div v-if="activeCalls.length" class="flex flex-col lg:flex-row gap-4">
      <!-- Active Calls Grid -->
      <div :class="selectedCall ? 'w-full lg:w-1/3 xl:w-2/5' : 'w-full'">
        <div class="grid grid-cols-1 gap-4" :class="!selectedCall && 'md:grid-cols-2 xl:grid-cols-3'">
          <CallCard
            v-for="call in activeCalls"
            :key="call.call_id || call.id"
            :call="call"
            :selected="selectedCall?.call_id === (call.call_id || call.id)"
            @select="selectCall(call)"
          />
        </div>
      </div>

      <!-- Live Transcript Panel -->
      <div v-if="selectedCall" class="w-full lg:w-2/3 xl:w-3/5" style="height: calc(100vh - 140px)">
        <LiveTranscript :call="selectedCall" @close="selectedCall = null" />
      </div>
    </div>

    <!-- Empty State -->
    <div v-else class="card text-center py-16">
      <Icon name="phone" class="w-12 h-12 text-gray-300 mx-auto mb-4" />
      <h2 class="text-lg font-semibold text-gray-600 mb-2">Sin llamadas activas</h2>
      <p class="text-sm text-gray-400">
        Las llamadas apareceran aqui en tiempo real cuando se inicien.
      </p>
    </div>

    <!-- Mobile fullscreen overlay -->
    <div
      v-if="selectedCall"
      class="fixed inset-0 bg-white z-50 p-4 lg:hidden flex flex-col"
    >
      <LiveTranscript :call="selectedCall" @close="selectedCall = null" class="flex-1" />
    </div>
  </div>
</template>

<script setup>
import { computed, ref, watch, onMounted, onUnmounted } from 'vue'
import { useCallsStore } from '../stores/calls.js'
import CallCard from '../components/CallCard.vue'
import LiveTranscript from '../components/LiveTranscript.vue'
import Icon from '../components/Icon.vue'

const callsStore = useCallsStore()
const activeCalls = computed(() => callsStore.activeCalls)
const selectedCall = ref(null)

function selectCall(call) {
  const callId = call.call_id || call.id
  if (selectedCall.value?.call_id === callId) {
    selectedCall.value = null // Toggle off
  } else {
    selectedCall.value = call
  }
}

// Clear selection when the selected call ends
watch(activeCalls, (calls) => {
  if (selectedCall.value) {
    const stillActive = calls.find(c => (c.call_id || c.id) === selectedCall.value.call_id)
    if (!stillActive) {
      selectedCall.value = null
    }
  }
})

onMounted(() => {
  if (!callsStore.sseConnection) {
    callsStore.connectSSE()
  }
})

onUnmounted(() => {
  // SSE stays connected globally
})
</script>
