import { defineStore } from 'pinia'

const MOCK = false // Set to false when backend is available

const MOCK_TENANTS = [
  { notaria_id: 'N001', name: 'Notaria Garcia Lopez - Barcelona', ddis: ['934001234', '934001235'], enabled: true, sip_trunk: 'trunk-notaria-n001', schedule: { timezone: 'Europe/Madrid', business_hours: [{ days: 'mon-fri', start: '09:00', end: '14:00' }, { days: 'mon-fri', start: '16:00', end: '19:00' }] }, transfers: { default: '201', extensions: { recepcion: '200', notario: '201', oficial: '202' }, group_hunt: '300' } },
  { notaria_id: 'N002', name: 'Notaria Martinez Ruiz - Madrid', ddis: ['912005678'], enabled: true, sip_trunk: 'trunk-notaria-n002', schedule: { timezone: 'Europe/Madrid', business_hours: [{ days: 'mon-fri', start: '09:00', end: '18:00' }] }, transfers: { default: '100', extensions: { recepcion: '100', notario: '101' }, group_hunt: '200' } },
  { notaria_id: 'N003', name: 'Notaria Fernandez Vidal - Valencia', ddis: ['963001122'], enabled: false, sip_trunk: 'trunk-notaria-n003', schedule: { timezone: 'Europe/Madrid', business_hours: [{ days: 'mon-fri', start: '09:00', end: '14:00' }] }, transfers: { default: '100', extensions: {}, group_hunt: '' } },
]

export const useTenantsStore = defineStore('tenants', {
  state: () => ({
    tenants: [],
    loading: false,
    error: null,
  }),

  actions: {
    async fetchTenants() {
      this.loading = true
      this.error = null
      if (MOCK) {
        this.tenants = [...MOCK_TENANTS]
        this.loading = false
        return
      }
      try {
        const api = (await import('../api.js')).default
        const { data } = await api.get('/api/v1/admin/tenants')
        this.tenants = data.tenants || []
      } catch (err) {
        this.error = err.response?.data?.error || 'Error cargando notarias'
      } finally { this.loading = false }
    },

    async createTenant(tenant) {
      if (MOCK) {
        MOCK_TENANTS.push({ ...tenant, ddis: tenant.ddis || [], enabled: true })
        this.tenants = [...MOCK_TENANTS]
        return true
      }
      try {
        const api = (await import('../api.js')).default
        await api.post('/api/v1/admin/tenants', tenant)
        await this.fetchTenants()
        return true
      } catch (err) { this.error = err.response?.data?.error || 'Error creando notaria'; return false }
    },

    async updateTenant(notariaId, tenant) {
      if (MOCK) {
        const idx = MOCK_TENANTS.findIndex(t => t.notaria_id === notariaId)
        if (idx >= 0) MOCK_TENANTS[idx] = { ...MOCK_TENANTS[idx], ...tenant }
        this.tenants = [...MOCK_TENANTS]
        return true
      }
      try {
        const api = (await import('../api.js')).default
        await api.put(`/api/v1/admin/tenants/${notariaId}`, tenant)
        await this.fetchTenants()
        return true
      } catch (err) { this.error = err.response?.data?.error || 'Error actualizando notaria'; return false }
    },

    async deleteTenant(notariaId) {
      if (MOCK) {
        const idx = MOCK_TENANTS.findIndex(t => t.notaria_id === notariaId)
        if (idx >= 0) MOCK_TENANTS.splice(idx, 1)
        this.tenants = [...MOCK_TENANTS]
        return true
      }
      try {
        const api = (await import('../api.js')).default
        await api.delete(`/api/v1/admin/tenants/${notariaId}`)
        await this.fetchTenants()
        return true
      } catch (err) { this.error = err.response?.data?.error || 'Error eliminando notaria'; return false }
    },
  },
})
