<template>
  <div class="fixed top-4 right-4 z-50 flex flex-col gap-2 max-w-sm">
    <TransitionGroup name="toast">
      <div
        v-for="toast in toasts"
        :key="toast.id"
        class="flex items-start gap-3 px-4 py-3 rounded-lg shadow-lg border text-sm cursor-pointer"
        :class="styles[toast.type]"
        @click="toastStore.remove(toast.id)"
      >
        <Icon :name="iconNames[toast.type]" class="w-5 h-5 flex-shrink-0 mt-0.5" />
        <p class="flex-1">{{ toast.message }}</p>
      </div>
    </TransitionGroup>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useToastStore } from '../stores/toast.js'
import Icon from './Icon.vue'

const toastStore = useToastStore()
const toasts = computed(() => toastStore.toasts)

const iconNames = {
  success: 'check-circle',
  error: 'x-circle',
  warning: 'exclamation-triangle',
  info: 'information-circle',
}

const styles = {
  success: 'bg-green-50 border-green-200 text-green-800',
  error: 'bg-red-50 border-red-200 text-red-800',
  warning: 'bg-amber-50 border-amber-200 text-amber-800',
  info: 'bg-blue-50 border-blue-200 text-blue-800',
}
</script>

<style scoped>
.toast-enter-active { transition: all 0.3s ease-out; }
.toast-leave-active { transition: all 0.2s ease-in; }
.toast-enter-from { opacity: 0; transform: translateX(100%); }
.toast-leave-to { opacity: 0; transform: translateX(100%); }
</style>
