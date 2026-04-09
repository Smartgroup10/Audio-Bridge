import { defineStore } from 'pinia'

const MOCK = false // Set to false when backend is available

export const useAuthStore = defineStore('auth', {
  state: () => ({
    token: localStorage.getItem('ab_token') || null,
    loading: false,
    error: null,
  }),

  getters: {
    isAuthenticated: (state) => !!state.token,
  },

  actions: {
    async login(password) {
      this.loading = true
      this.error = null
      try {
        if (MOCK) {
          if (password === 'SmartBridge2026!') {
            this.token = 'mock-token-demo'
            localStorage.setItem('ab_token', 'mock-token-demo')
            return true
          }
          this.error = 'Password incorrecto'
          return false
        }
        const api = (await import('../api.js')).default
        const { data } = await api.post('/api/v1/auth/login', { password })
        this.token = data.token
        localStorage.setItem('ab_token', data.token)
        return true
      } catch (err) {
        this.error = err.response?.data?.error || 'Error de conexion'
        return false
      } finally {
        this.loading = false
      }
    },

    logout() {
      this.token = null
      localStorage.removeItem('ab_token')
    },
  },
})
