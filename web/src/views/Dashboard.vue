<template>
  <div>
    <h1 class="text-2xl font-bold text-gray-800 mb-6">Dashboard</h1>

    <!-- Stats Cards -->
    <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4 mb-8">
      <StatsCard
        label="Llamadas Hoy"
        :value="stats?.calls_today || 0"
        icon="phone"
        color="blue"
      />
      <StatsCard
        label="Activas Ahora"
        :value="stats?.calls_active || 0"
        icon="signal"
        color="green"
      />
      <StatsCard
        label="Duracion Media"
        :value="avgDurationFormatted"
        icon="clock"
        color="amber"
      />
      <StatsCard
        label="Total Historico"
        :value="stats?.total_calls || 0"
        icon="chart-bar"
        color="purple"
      />
    </div>

    <!-- Second row: Inbound/Outbound + By Notaria -->
    <div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
      <!-- Today breakdown -->
      <div class="card">
        <h2 class="text-lg font-semibold text-gray-800 mb-4">Hoy por Tipo</h2>
        <div class="flex gap-6">
          <div class="flex-1 bg-blue-50 rounded-lg p-4 text-center">
            <p class="text-3xl font-bold text-blue-600">{{ stats?.inbound_today || 0 }}</p>
            <p class="text-sm text-gray-500 mt-1">Entrantes</p>
          </div>
          <div class="flex-1 bg-cyan-50 rounded-lg p-4 text-center">
            <p class="text-3xl font-bold text-cyan-600">{{ stats?.outbound_today || 0 }}</p>
            <p class="text-sm text-gray-500 mt-1">Salientes</p>
          </div>
        </div>
      </div>

      <!-- By Notaria -->
      <div class="card">
        <h2 class="text-lg font-semibold text-gray-800 mb-4">Llamadas Hoy por Notaria</h2>
        <div v-if="notariaEntries.length" class="space-y-3">
          <div
            v-for="entry in notariaEntries"
            :key="entry.id"
            class="flex items-center justify-between"
          >
            <div class="flex items-center gap-2">
              <Icon name="building-office" class="w-4 h-4 text-gray-500" />
              <span class="text-sm font-medium text-gray-700">{{ entry.id }}</span>
            </div>
            <div class="flex items-center gap-3">
              <div class="w-32 bg-gray-200 rounded-full h-2">
                <div
                  class="bg-blue-500 rounded-full h-2 transition-all duration-500"
                  :style="{ width: entry.pct + '%' }"
                ></div>
              </div>
              <span class="text-sm font-semibold text-gray-800 w-8 text-right">{{ entry.count }}</span>
            </div>
          </div>
        </div>
        <div v-else class="text-sm text-gray-400 text-center py-6">
          Sin llamadas hoy
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted } from 'vue'
import { useCallsStore } from '../stores/calls.js'
import StatsCard from '../components/StatsCard.vue'
import Icon from '../components/Icon.vue'

const callsStore = useCallsStore()

const stats = computed(() => callsStore.stats)

const avgDurationFormatted = computed(() => {
  const secs = stats.value?.avg_duration_seconds || 0
  if (secs === 0) return '0s'
  const mins = Math.floor(secs / 60)
  const remainSecs = Math.round(secs % 60)
  if (mins === 0) return `${remainSecs}s`
  return `${mins}m ${remainSecs}s`
})

const notariaEntries = computed(() => {
  const byNotaria = stats.value?.by_notaria || {}
  const entries = Object.entries(byNotaria).map(([id, count]) => ({ id, count }))
  entries.sort((a, b) => b.count - a.count)
  const max = entries.reduce((m, e) => Math.max(m, e.count), 1)
  return entries.map(e => ({ ...e, pct: Math.round((e.count / max) * 100) }))
})

let statsInterval = null

onMounted(() => {
  callsStore.fetchStats()
  statsInterval = setInterval(() => callsStore.fetchStats(), 30000)
})

onUnmounted(() => {
  if (statsInterval) clearInterval(statsInterval)
})
</script>
