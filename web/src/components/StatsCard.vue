<template>
  <div class="card flex items-center gap-4">
    <div
      class="flex-shrink-0 w-12 h-12 rounded-lg flex items-center justify-center"
      :class="bgClass"
    >
      <Icon :name="icon" class="w-6 h-6" />
    </div>
    <div class="min-w-0">
      <p class="text-sm text-gray-500 truncate">{{ label }}</p>
      <p class="text-2xl font-bold text-gray-800">{{ displayValue }}</p>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import Icon from './Icon.vue'

const props = defineProps({
  label: { type: String, required: true },
  value: { type: [Number, String], default: 0 },
  icon: { type: String, default: 'phone' },
  color: { type: String, default: 'blue' },
  suffix: { type: String, default: '' },
})

const bgClass = computed(() => {
  const map = {
    blue: 'bg-blue-100 text-blue-600',
    green: 'bg-green-100 text-green-600',
    cyan: 'bg-cyan-100 text-cyan-600',
    amber: 'bg-amber-100 text-amber-600',
    red: 'bg-red-100 text-red-600',
    purple: 'bg-purple-100 text-purple-600',
  }
  return map[props.color] || map.blue
})

const displayValue = computed(() => {
  if (props.suffix) return `${props.value}${props.suffix}`
  return props.value
})
</script>
