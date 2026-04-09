<template>
  <div>
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-2xl font-bold text-gray-800">Notarias</h1>
      <button @click="openCreate" class="btn-primary flex items-center gap-2">
        + Crear Notaria
      </button>
    </div>

    <!-- Error -->
    <div v-if="error" class="mb-4 p-3 bg-red-50 border border-red-200 rounded-lg text-sm text-red-600">
      {{ error }}
    </div>

    <!-- Table -->
    <div class="card overflow-hidden p-0">
      <div class="overflow-x-auto">
        <table class="w-full text-sm">
          <thead>
            <tr class="bg-gray-50 border-b border-gray-200">
              <th class="text-left px-4 py-3 font-medium text-gray-500">ID Notaria</th>
              <th class="text-left px-4 py-3 font-medium text-gray-500">Company ID</th>
              <th class="text-left px-4 py-3 font-medium text-gray-500">Nombre</th>
              <th class="text-left px-4 py-3 font-medium text-gray-500">DDIs</th>
              <th class="text-left px-4 py-3 font-medium text-gray-500">SIP Trunk</th>
              <th class="text-center px-4 py-3 font-medium text-gray-500">Estado</th>
              <th class="text-right px-4 py-3 font-medium text-gray-500">Acciones</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="tenant in tenants"
              :key="tenant.notaria_id"
              class="border-b border-gray-100"
            >
              <td class="px-4 py-3 font-mono font-medium">{{ tenant.notaria_id }}</td>
              <td class="px-4 py-3 text-xs font-mono text-gray-500">{{ tenant.company_id || '-' }}</td>
              <td class="px-4 py-3">{{ tenant.name || '-' }}</td>
              <td class="px-4 py-3">
                <div class="flex flex-wrap gap-1">
                  <span
                    v-for="ddi in (tenant.ddis || [])"
                    :key="ddi"
                    class="bg-gray-100 text-gray-700 px-2 py-0.5 rounded text-xs font-mono"
                  >
                    {{ ddi }}
                  </span>
                  <span v-if="!tenant.ddis?.length" class="text-gray-400">-</span>
                </div>
              </td>
              <td class="px-4 py-3 text-xs font-mono text-gray-500">{{ tenant.sip_trunk || '-' }}</td>
              <td class="px-4 py-3 text-center">
                <span
                  class="inline-flex px-2 py-0.5 rounded-full text-xs font-medium"
                  :class="tenant.enabled ? 'bg-green-100 text-green-700' : 'bg-red-100 text-red-700'"
                >
                  {{ tenant.enabled ? 'Activa' : 'Inactiva' }}
                </span>
              </td>
              <td class="px-4 py-3 text-right">
                <button @click="openEdit(tenant)" class="text-blue-500 hover:text-blue-700 text-sm mr-3">
                  Editar
                </button>
                <button @click="confirmDelete(tenant)" class="text-red-500 hover:text-red-700 text-sm">
                  Eliminar
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-if="!tenants.length && !loading" class="text-center py-12 text-gray-400">
        No hay notarias configuradas
      </div>

      <div v-if="loading" class="text-center py-12 text-gray-400">
        Cargando...
      </div>
    </div>

    <!-- Create/Edit Modal -->
    <div
      v-if="showModal"
      class="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4"
      @click.self="closeModal"
    >
      <div class="bg-white rounded-xl shadow-xl w-full max-w-lg max-h-[90vh] overflow-y-auto">
        <div class="px-6 py-4 border-b border-gray-200">
          <h2 class="text-lg font-semibold text-gray-800">
            {{ isEditing ? 'Editar Notaria' : 'Crear Notaria' }}
          </h2>
        </div>

        <form @submit.prevent="handleSave" class="p-6 space-y-4">
          <div>
            <label class="block text-sm font-medium text-gray-600 mb-1">ID Notaria</label>
            <input
              v-model="form.notaria_id"
              type="text"
              class="input-field"
              placeholder="ej: notaria-001"
              :disabled="isEditing"
              required
            />
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-600 mb-1">Company ID PekePBX</label>
            <input
              v-model="form.company_id"
              type="text"
              class="input-field"
              placeholder="ej: 20242 (auto-provisiona dialplan)"
            />
            <p class="text-xs text-gray-400 mt-1">Si se rellena, se crea automaticamente el dialplan en PekePBX</p>
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-600 mb-1">Nombre</label>
            <input
              v-model="form.name"
              type="text"
              class="input-field"
              placeholder="Nombre de la notaria"
              required
            />
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-600 mb-1">
              DDIs (separados por coma)
            </label>
            <input
              v-model="form.ddisStr"
              type="text"
              class="input-field"
              placeholder="910123456, 910654321"
            />
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-600 mb-1">SIP Trunk</label>
            <input
              v-model="form.sip_trunk"
              type="text"
              class="input-field"
              placeholder="ej: trunk-notaria-001"
            />
          </div>

          <div class="flex items-center gap-3">
            <label class="relative inline-flex items-center cursor-pointer">
              <input v-model="form.enabled" type="checkbox" class="sr-only peer" />
              <div class="w-11 h-6 bg-gray-200 peer-focus:outline-none peer-focus:ring-2 peer-focus:ring-blue-300 rounded-full peer peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-gray-300 after:border after:rounded-full after:h-5 after:w-5 after:transition-all peer-checked:bg-blue-500"></div>
            </label>
            <span class="text-sm text-gray-700">Habilitada</span>
          </div>

          <!-- Schedule section -->
          <div class="border-t border-gray-200 pt-4">
            <h3 class="text-sm font-semibold text-gray-700 mb-2">Horario</h3>
            <div class="grid grid-cols-2 gap-3">
              <div>
                <label class="block text-xs text-gray-500 mb-1">Zona Horaria</label>
                <input
                  v-model="form.schedule.timezone"
                  type="text"
                  class="input-field text-sm"
                  placeholder="Europe/Madrid"
                />
              </div>
            </div>
            <div v-for="(hr, idx) in form.schedule.business_hours" :key="idx" class="grid grid-cols-3 gap-2 mt-2">
              <input v-model="hr.days" class="input-field text-sm" placeholder="L-V" />
              <input v-model="hr.start" class="input-field text-sm" placeholder="09:00" />
              <input v-model="hr.end" class="input-field text-sm" placeholder="18:00" />
            </div>
            <button type="button" @click="addHourRange" class="text-blue-500 text-xs mt-1 hover:underline">
              + Agregar horario
            </button>
          </div>

          <!-- AI Customization section -->
          <div class="border-t border-gray-200 pt-4">
            <h3 class="text-sm font-semibold text-gray-700 mb-2">Configuracion IA</h3>
            <div class="grid grid-cols-2 gap-3">
              <div>
                <label class="block text-xs text-gray-500 mb-1">Voz</label>
                <select v-model="form.voice" class="input-field text-sm">
                  <option value="">Global (por defecto)</option>
                  <option value="alloy">Alloy</option>
                  <option value="ash">Ash</option>
                  <option value="coral">Coral</option>
                  <option value="echo">Echo</option>
                  <option value="fable">Fable</option>
                  <option value="nova">Nova</option>
                  <option value="onyx">Onyx</option>
                  <option value="sage">Sage</option>
                  <option value="shimmer">Shimmer</option>
                </select>
              </div>
              <div>
                <label class="block text-xs text-gray-500 mb-1">Idioma</label>
                <select v-model="form.language" class="input-field text-sm">
                  <option value="">Global (por defecto)</option>
                  <option value="es">Espanol</option>
                  <option value="en">English</option>
                  <option value="ca">Catala</option>
                  <option value="eu">Euskara</option>
                  <option value="gl">Galego</option>
                  <option value="fr">Francais</option>
                  <option value="pt">Portugues</option>
                </select>
              </div>
            </div>
            <div class="mt-3">
              <label class="block text-xs text-gray-500 mb-1">Instrucciones personalizadas</label>
              <textarea
                v-model="form.instructions"
                class="input-field text-sm"
                rows="4"
                placeholder="Dejar vacio para usar el prompt generico de notaria. Ej: Eres la asistente virtual de la Notaria Garcia Lopez de Barcelona..."
              ></textarea>
            </div>
          </div>

          <!-- Transfers section -->
          <div class="border-t border-gray-200 pt-4">
            <h3 class="text-sm font-semibold text-gray-700 mb-2">Transferencias</h3>
            <div class="grid grid-cols-2 gap-3">
              <div>
                <label class="block text-xs text-gray-500 mb-1">Destino por defecto</label>
                <input
                  v-model="form.transfers.default"
                  type="text"
                  class="input-field text-sm"
                  placeholder="100"
                />
              </div>
              <div>
                <label class="block text-xs text-gray-500 mb-1">Group Hunt</label>
                <input
                  v-model="form.transfers.group_hunt"
                  type="text"
                  class="input-field text-sm"
                  placeholder="200"
                />
              </div>
            </div>
          </div>

          <div class="flex gap-3 pt-4">
            <button type="submit" class="btn-primary flex-1" :disabled="saving">
              {{ saving ? 'Guardando...' : (isEditing ? 'Actualizar' : 'Crear') }}
            </button>
            <button type="button" @click="closeModal" class="btn-secondary flex-1">
              Cancelar
            </button>
          </div>
        </form>
      </div>
    </div>

    <!-- Delete Confirmation -->
    <div
      v-if="showDeleteConfirm"
      class="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4"
      @click.self="showDeleteConfirm = false"
    >
      <div class="bg-white rounded-xl shadow-xl w-full max-w-sm p-6">
        <h2 class="text-lg font-semibold text-gray-800 mb-2">Confirmar Eliminacion</h2>
        <p class="text-sm text-gray-600 mb-6">
          Estas seguro de que quieres eliminar la notaria
          <strong>{{ deletingTenant?.name || deletingTenant?.notaria_id }}</strong>?
          Esta accion no se puede deshacer.
        </p>
        <div class="flex gap-3">
          <button @click="doDelete" class="btn-danger flex-1">
            Eliminar
          </button>
          <button @click="showDeleteConfirm = false" class="btn-secondary flex-1">
            Cancelar
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, reactive } from 'vue'
import { useTenantsStore } from '../stores/tenants.js'

const tenantsStore = useTenantsStore()

const tenants = computed(() => tenantsStore.tenants)
const loading = computed(() => tenantsStore.loading)
const error = computed(() => tenantsStore.error)

const showModal = ref(false)
const isEditing = ref(false)
const saving = ref(false)

const showDeleteConfirm = ref(false)
const deletingTenant = ref(null)

const defaultForm = () => ({
  notaria_id: '',
  company_id: '',
  name: '',
  ddisStr: '',
  sip_trunk: '',
  enabled: true,
  voice: '',
  language: '',
  instructions: '',
  schedule: {
    timezone: 'Europe/Madrid',
    business_hours: [{ days: 'L-V', start: '09:00', end: '18:00' }],
  },
  transfers: {
    default: '',
    extensions: {},
    group_hunt: '',
  },
})

const form = reactive(defaultForm())

function openCreate() {
  Object.assign(form, defaultForm())
  isEditing.value = false
  showModal.value = true
}

function openEdit(tenant) {
  isEditing.value = true
  form.notaria_id = tenant.notaria_id
  form.company_id = tenant.company_id || ''
  form.name = tenant.name || ''
  form.ddisStr = (tenant.ddis || []).join(', ')
  form.sip_trunk = tenant.sip_trunk || ''
  form.enabled = tenant.enabled !== false
  form.voice = tenant.voice || ''
  form.language = tenant.language || ''
  form.instructions = tenant.instructions || ''
  form.schedule = tenant.schedule || { timezone: 'Europe/Madrid', business_hours: [{ days: 'L-V', start: '09:00', end: '18:00' }] }
  form.transfers = tenant.transfers || { default: '', extensions: {}, group_hunt: '' }
  if (!form.schedule.business_hours?.length) {
    form.schedule.business_hours = [{ days: 'L-V', start: '09:00', end: '18:00' }]
  }
  showModal.value = true
}

function closeModal() {
  showModal.value = false
}

function addHourRange() {
  form.schedule.business_hours.push({ days: '', start: '', end: '' })
}

async function handleSave() {
  saving.value = true
  const payload = {
    notaria_id: form.notaria_id,
    company_id: form.company_id,
    name: form.name,
    ddis: form.ddisStr.split(',').map(d => d.trim()).filter(Boolean),
    sip_trunk: form.sip_trunk,
    enabled: form.enabled,
    voice: form.voice,
    language: form.language,
    instructions: form.instructions,
    schedule: {
      ...form.schedule,
      business_hours: form.schedule.business_hours.filter(h => h.days && h.start && h.end),
    },
    transfers: form.transfers,
  }

  let ok
  if (isEditing.value) {
    ok = await tenantsStore.updateTenant(form.notaria_id, payload)
  } else {
    ok = await tenantsStore.createTenant(payload)
  }

  saving.value = false
  if (ok) {
    closeModal()
  }
}

function confirmDelete(tenant) {
  deletingTenant.value = tenant
  showDeleteConfirm.value = true
}

async function doDelete() {
  if (deletingTenant.value) {
    await tenantsStore.deleteTenant(deletingTenant.value.notaria_id)
  }
  showDeleteConfirm.value = false
  deletingTenant.value = null
}

onMounted(() => {
  tenantsStore.fetchTenants()
})
</script>
