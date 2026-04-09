import { defineStore } from 'pinia'

export const useLogsStore = defineStore('logs', {
  state: () => ({
    interactionLogs: [],
    systemLogs: [],
    interactionPagination: { page: 1, limit: 50, total: 0 },
    systemPagination: { page: 1, limit: 50, total: 0 },
    loading: false,
  }),

  getters: {
    interactionTotalPages: (state) =>
      Math.ceil(state.interactionPagination.total / state.interactionPagination.limit) || 1,
    systemTotalPages: (state) =>
      Math.ceil(state.systemPagination.total / state.systemPagination.limit) || 1,
  },

  actions: {
    async fetchInteractionLogs(params = {}) {
      this.loading = true
      try {
        const api = (await import('../api.js')).default
        const query = {
          page: params.page || 1,
          limit: params.limit || this.interactionPagination.limit,
        }
        if (params.call_id) query.call_id = params.call_id
        if (params.direction) query.direction = params.direction
        if (params.event_type) query.event_type = params.event_type
        if (params.from) query.from = params.from
        if (params.to) query.to = params.to

        const { data } = await api.get('/api/v1/admin/logs/interactions', { params: query })
        this.interactionLogs = data.logs || []
        this.interactionPagination.total = data.total || 0
        this.interactionPagination.page = data.page || 1
      } catch (err) {
        const { useToastStore } = await import('./toast.js')
        useToastStore().error('Error al cargar logs de llamadas')
      } finally {
        this.loading = false
      }
    },

    async fetchSystemLogs(params = {}) {
      this.loading = true
      try {
        const api = (await import('../api.js')).default
        const query = {
          page: params.page || 1,
          limit: params.limit || this.systemPagination.limit,
        }
        if (params.level) query.level = params.level
        if (params.from) query.from = params.from
        if (params.to) query.to = params.to

        const { data } = await api.get('/api/v1/admin/logs/system', { params: query })
        this.systemLogs = data.logs || []
        this.systemPagination.total = data.total || 0
        this.systemPagination.page = data.page || 1
      } catch (err) {
        const { useToastStore } = await import('./toast.js')
        useToastStore().error('Error al cargar logs del sistema')
      } finally {
        this.loading = false
      }
    },
  },
})
