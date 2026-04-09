import { defineStore } from 'pinia'

let nextId = 0

export const useToastStore = defineStore('toast', {
  state: () => ({
    toasts: [],
  }),

  actions: {
    add(message, type = 'info', duration = 4000) {
      const id = ++nextId
      this.toasts.push({ id, message, type })
      if (duration > 0) {
        setTimeout(() => this.remove(id), duration)
      }
    },

    remove(id) {
      this.toasts = this.toasts.filter(t => t.id !== id)
    },

    success(message) { this.add(message, 'success') },
    error(message)   { this.add(message, 'error', 6000) },
    warning(message) { this.add(message, 'warning', 5000) },
    info(message)    { this.add(message, 'info') },
  },
})
