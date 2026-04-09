import { createRouter, createWebHistory } from 'vue-router'

import Login from './views/Login.vue'
import Dashboard from './views/Dashboard.vue'
import ActiveCalls from './views/ActiveCalls.vue'
import CallHistory from './views/CallHistory.vue'
import CallDetail from './views/CallDetail.vue'
import Tenants from './views/Tenants.vue'
import Recordings from './views/Recordings.vue'
import Settings from './views/Settings.vue'
import Logs from './views/Logs.vue'

const routes = [
  {
    path: '/login',
    name: 'Login',
    component: Login,
    meta: { public: true },
  },
  {
    path: '/',
    name: 'Dashboard',
    component: Dashboard,
  },
  {
    path: '/llamadas-activas',
    name: 'ActiveCalls',
    component: ActiveCalls,
  },
  {
    path: '/historial',
    name: 'CallHistory',
    component: CallHistory,
  },
  {
    path: '/llamadas/:id',
    name: 'CallDetail',
    component: CallDetail,
  },
  {
    path: '/notarias',
    name: 'Tenants',
    component: Tenants,
  },
  {
    path: '/grabaciones',
    name: 'Recordings',
    component: Recordings,
  },
  {
    path: '/logs',
    name: 'Logs',
    component: Logs,
  },
  {
    path: '/configuracion',
    name: 'Settings',
    component: Settings,
  },
]

const router = createRouter({
  history: createWebHistory('/admin/'),
  routes,
})

// Navigation guard — require auth for non-public routes
router.beforeEach((to, from, next) => {
  const token = localStorage.getItem('ab_token')
  if (!to.meta.public && !token) {
    next({ name: 'Login' })
  } else if (to.name === 'Login' && token) {
    next({ name: 'Dashboard' })
  } else {
    next()
  }
})

export default router
