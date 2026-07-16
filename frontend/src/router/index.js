import { createRouter, createWebHistory } from 'vue-router'
import { globalStore } from '../store'
import { sessionHelper } from '../services/session'

const routes = [
  {
    path: '/',
    name: 'Dashboard',
    component: () => import('../views/DashboardView.vue'),
    meta: { requiresAuth: true }
  },
  {
    path: '/auth',
    name: 'Auth',
    component: () => import('../views/AuthView.vue')
  },
  {
    // R07c: minimal authenticated per-room queue view. The router guard
    // (beforeEach) already enforces requiresAuth via the same path used
    // for the global Dashboard.
    path: '/rooms/:slug',
    name: 'Room',
    component: () => import('../views/RoomView.vue'),
    meta: { requiresAuth: true }
  },
  {
    // R05b1: room entry surface. Lists active rooms and exposes
    // create / manual-open / invite-redeem forms. Requires auth like
    // the other room routes; the global Dashboard remains the
    // login destination until R14d.
    path: '/rooms',
    name: 'RoomEntry',
    component: () => import('../views/RoomEntryView.vue'),
    meta: { requiresAuth: true }
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

router.beforeEach((to, from, next) => {
  const hasUser = !!globalStore.currentUser
  const hasValidSession = sessionHelper.isValid()
  const isAuthenticated = hasUser && hasValidSession

  // Clear stale localStorage user without valid session
  if (hasUser && !hasValidSession) {
    sessionHelper.clearSession()
    globalStore.clearUser()
  }

  if (to.meta.requiresAuth && !isAuthenticated) {
    next({ name: 'Auth' })
  } else if (to.name === 'Auth' && isAuthenticated) {
    next({ name: 'Dashboard' })
  } else {
    next()
  }
})

export default router
