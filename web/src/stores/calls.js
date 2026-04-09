import { defineStore } from 'pinia'

const MOCK = false // Set to false when backend is available

const MOCK_CALLS = [
  { id: 'a1b2c3d4-e5f6-7890-abcd-ef1234567890', caller_id: '+34912345678', ddi: '934001234', notaria_id: 'N001', direction: 'inbound', state: 'completed', call_type: 'inbound', schedule: 'business_hours', start_time: '2026-03-16T10:15:00Z', answer_time: '2026-03-16T10:15:03Z', end_time: '2026-03-16T10:18:45Z', duration_seconds: 222, end_reason: 'resolved', transfer_dest: '', recording_caller: '/opt/audio-bridge/recordings/2026-03-16/a1b2c3d4_caller.wav', recording_ai: '/opt/audio-bridge/recordings/2026-03-16/a1b2c3d4_ai.wav', transcript_user: 'Hola, buenos dias. Queria preguntar por el estado de mi escritura.\nEs la escritura numero 4523 de este ano.\nPerfecto, muchas gracias.', transcript_ai: 'Buenos dias, bienvenido a la Notaria Garcia Lopez. En que puedo ayudarle?\nDejeme consultar... La escritura 4523 esta en fase de revision final. Deberia estar lista para firma en 2-3 dias habiles.\nDe nada, que tenga un buen dia.' },
  { id: 'b2c3d4e5-f6a7-8901-bcde-f12345678901', caller_id: '+34666123456', ddi: '934001235', notaria_id: 'N001', direction: 'inbound', state: 'completed', call_type: 'inbound', schedule: 'business_hours', start_time: '2026-03-16T11:30:00Z', answer_time: '2026-03-16T11:30:02Z', end_time: '2026-03-16T11:32:10Z', duration_seconds: 128, end_reason: 'transfer', transfer_dest: '201', recording_caller: '/opt/audio-bridge/recordings/2026-03-16/b2c3d4e5_caller.wav', recording_ai: '', transcript_user: 'Necesito hablar con el notario directamente, es urgente.', transcript_ai: 'Entiendo, le transfiero directamente con el notario. Un momento por favor.' },
  { id: 'c3d4e5f6-a7b8-9012-cdef-123456789012', caller_id: '+34915556789', ddi: '912005678', notaria_id: 'N002', direction: 'outbound', state: 'completed', call_type: 'callback', schedule: 'business_hours', start_time: '2026-03-16T09:45:00Z', answer_time: '2026-03-16T09:45:15Z', end_time: '2026-03-16T09:52:30Z', duration_seconds: 435, end_reason: 'resolved', transfer_dest: '', recording_caller: '/opt/audio-bridge/recordings/2026-03-16/c3d4e5f6_caller.wav', recording_ai: '/opt/audio-bridge/recordings/2026-03-16/c3d4e5f6_ai.wav', transcript_user: 'Si, digame.\nAh perfecto, el jueves me viene bien a las 10.\nGracias, hasta entonces.', transcript_ai: 'Buenos dias, le llamamos de la Notaria Martinez Ruiz. Queriamos confirmar su cita para la firma de la compraventa.\nPerfecto, le confirmo cita el jueves a las 10:00. Recuerde traer su DNI original.\nMuy bien, hasta el jueves. Buenos dias.' },
  { id: 'd4e5f6a7-b8c9-0123-defa-234567890123', caller_id: '+34933001122', ddi: '934001234', notaria_id: 'N001', direction: 'inbound', state: 'completed', call_type: 'inbound', schedule: 'after_hours', start_time: '2026-03-15T20:10:00Z', answer_time: '2026-03-15T20:10:02Z', end_time: '2026-03-15T20:11:30Z', duration_seconds: 88, end_reason: 'resolved', transfer_dest: '', recording_caller: '', recording_ai: '', transcript_user: 'Cual es el horario de la notaria?', transcript_ai: 'Nuestro horario es de lunes a viernes, de 9:00 a 14:00 y de 16:00 a 19:00. Actualmente estamos fuera de horario. Puede llamar manana a partir de las 9.' },
  { id: 'e5f6a7b8-c9d0-1234-efab-345678901234', caller_id: '+34911223344', ddi: '912005678', notaria_id: 'N002', direction: 'inbound', state: 'completed', call_type: 'inbound', schedule: 'business_hours', start_time: '2026-03-16T12:00:00Z', answer_time: '2026-03-16T12:00:03Z', end_time: '2026-03-16T12:05:15Z', duration_seconds: 312, end_reason: 'resolved', transfer_dest: '', recording_caller: '/opt/audio-bridge/recordings/2026-03-16/e5f6a7b8_caller.wav', recording_ai: '/opt/audio-bridge/recordings/2026-03-16/e5f6a7b8_ai.wav', transcript_user: 'Quiero pedir cita para una escritura de herencia.\nSomos tres hermanos.\nEl martes por la tarde nos vendria bien.', transcript_ai: 'Buenos dias. Le ayudo con la cita. Que tipo de escritura necesita?\nEntendido, escritura de herencia. Necesitaremos la documentacion de todos los herederos. Cuantos son?\nPerfecto, para tres otorgantes. Le puedo ofrecer el martes a las 17:00. Le confirmo?' },
]

const MOCK_ACTIVE = [
  { call_id: 'f6a7b8c9-d0e1-2345-fabc-456789012345', caller_id: '+34666999888', notaria_id: 'N001', direction: 'inbound', start_time: new Date().toISOString(), transcriptUser: 'Hola, llamo por una consulta sobre poderes notariales.', transcriptAI: 'Buenos dias, bienvenido a la Notaria Garcia Lopez. Claro, le ayudo con su consulta sobre poderes. Que tipo de poder necesita?', transcriptMessages: [{ role: 'user', text: 'Hola, llamo por una consulta sobre poderes notariales.', time: Date.now() - 30000 }, { role: 'ai', text: 'Buenos dias, bienvenido a la Notaria Garcia Lopez. Claro, le ayudo con su consulta sobre poderes. Que tipo de poder necesita?', time: Date.now() - 25000 }], startedAt: Date.now() - 45000 },
]

const MOCK_STATS = {
  calls_today: 12,
  calls_active: 1,
  avg_duration_seconds: 195.5,
  total_calls: 847,
  inbound_today: 9,
  outbound_today: 3,
  by_notaria: { 'N001': 7, 'N002': 5 },
}

export const useCallsStore = defineStore('calls', {
  state: () => ({
    activeCalls: MOCK ? [...MOCK_ACTIVE] : [],
    callHistory: [],
    currentCall: null,
    currentCallLogs: [],
    stats: null,
    pagination: { page: 1, limit: 20, total: 0 },
    loading: false,
    sseConnection: null,
    sseStatus: 'disconnected', // 'connected', 'connecting', 'disconnected'
  }),

  getters: {
    totalPages: (state) => Math.ceil(state.pagination.total / state.pagination.limit) || 1,
  },

  actions: {
    async fetchStats() {
      if (MOCK) { this.stats = MOCK_STATS; return }
      try {
        const api = (await import('../api.js')).default
        const { data } = await api.get('/api/v1/admin/stats')
        this.stats = data
      } catch (err) {
        const { useToastStore } = await import('./toast.js')
        useToastStore().error('Error al cargar estadisticas')
      }
    },

    async fetchHistory(params = {}) {
      this.loading = true
      if (MOCK) {
        this.callHistory = MOCK_CALLS
        this.pagination.total = MOCK_CALLS.length
        this.loading = false
        return
      }
      try {
        const api = (await import('../api.js')).default
        const query = { page: params.page || this.pagination.page, limit: params.limit || this.pagination.limit }
        if (params.notaria_id) query.notaria_id = params.notaria_id
        if (params.from) query.from = params.from
        if (params.to) query.to = params.to
        const { data } = await api.get('/api/v1/admin/calls', { params: query })
        this.callHistory = data.calls || []
        this.pagination.total = data.total || 0
        this.pagination.page = data.page || 1
      } catch (err) {
        const { useToastStore } = await import('./toast.js')
        useToastStore().error('Error al cargar historial de llamadas')
      }
      finally { this.loading = false }
    },

    async fetchCallDetail(callId) {
      this.loading = true
      if (MOCK) {
        this.currentCall = MOCK_CALLS.find(c => c.id === callId) || null
        this.currentCallLogs = []
        this.loading = false
        return
      }
      try {
        const api = (await import('../api.js')).default
        const { data } = await api.get(`/api/v1/admin/calls/${callId}`)
        this.currentCall = data.call
        this.currentCallLogs = data.logs || []
      } catch (err) { this.currentCall = null; this.currentCallLogs = [] }
      finally { this.loading = false }
    },

    connectSSE() {
      if (MOCK) return // No SSE in mock mode
      if (this.sseConnection) return // Already connected or connecting
      const token = localStorage.getItem('ab_token')
      if (!token) return
      this.sseConnection = { close: () => {} } // Mark as connecting immediately
      this.sseStatus = 'connecting'
      const url = '/api/v1/admin/sessions/live'
      const connect = () => {
        const freshToken = localStorage.getItem('ab_token')
        if (!freshToken) { this.sseStatus = 'disconnected'; this.sseConnection = null; return }
        this.sseStatus = 'connecting'
        fetch(url, { headers: { 'Authorization': `Bearer ${freshToken}` } }).then(response => {
          if (!response.ok) { this.sseStatus = 'disconnected'; setTimeout(connect, 5000); return }
          this.sseStatus = 'connected'
          const reader = response.body.getReader()
          const decoder = new TextDecoder()
          let buffer = ''
          const read = () => {
            reader.read().then(({ done, value }) => {
              if (done) { setTimeout(connect, 3000); return }
              buffer += decoder.decode(value, { stream: true })
              const lines = buffer.split('\n')
              buffer = lines.pop() || ''
              let currentEvent = ''
              for (const line of lines) {
                if (line.startsWith('event:')) currentEvent = line.substring(6).trim()
                else if (line.startsWith('data:')) {
                  const dataStr = line.substring(5).trim()
                  if (dataStr && currentEvent) {
                    try { this.handleSSEEvent(currentEvent, JSON.parse(dataStr)) } catch(e) {}
                    currentEvent = ''
                  }
                }
              }
              read()
            }).catch(() => { this.sseStatus = 'disconnected'; setTimeout(connect, 5000) })
          }
          read()
          this.sseConnection = { close: () => reader.cancel() }
        }).catch(() => { this.sseStatus = 'disconnected'; setTimeout(connect, 5000) })
      }
      connect()
    },

    handleSSEEvent(type, data) {
      const eventData = data.data || data
      switch (type) {
        case 'call_started': {
          const callId = eventData.call_id || eventData.id
          if (!this.activeCalls.find(c => c.call_id === callId)) {
            this.activeCalls.push({ ...eventData, call_id: callId, transcriptUser: '', transcriptAI: '', transcriptMessages: [], startedAt: Date.now() })
          }
          break
        }
        case 'call_ended':
          this.activeCalls = this.activeCalls.filter(c => c.call_id !== (eventData.call_id || eventData.id))
          break
        case 'transcript_user': {
          const call = this.activeCalls.find(c => c.call_id === eventData.call_id)
          if (call) {
            call.transcriptUser = (call.transcriptUser || '') + (call.transcriptUser ? '\n' : '') + (eventData.text || '')
            call.transcriptMessages.push({ role: 'user', text: eventData.text || '', time: Date.now() })
          }
          break
        }
        case 'transcript_ai': {
          const call = this.activeCalls.find(c => c.call_id === eventData.call_id)
          if (call) {
            call.transcriptAI = (call.transcriptAI || '') + (call.transcriptAI ? '\n' : '') + (eventData.text || '')
            call.transcriptMessages.push({ role: 'ai', text: eventData.text || '', time: Date.now() })
          }
          break
        }
      }
    },

    disconnectSSE() {
      if (this.sseConnection) { this.sseConnection.close(); this.sseConnection = null }
      this.sseStatus = 'disconnected'
    },
  },
})
