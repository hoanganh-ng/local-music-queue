import { createRouter, createWebHistory } from 'vue-router'
import { globalStore } from '../store'
import { sessionHelper } from '../services/session'
import { roomCutoverAuthoritative, authenticatedLandingRouteName } from '../config/cutover'

// R14d: the root route is cutover-aware. In the false (pre-cutover /
// rollback) artifact `/` remains the named Dashboard route with its
// existing behavior. In the true artifact no route named Dashboard
// exists — `/` redirects to RoomEntry so DashboardView cannot mount
// through normal routing. DashboardView.vue itself is intentionally
// NOT deleted: the false build is the pre-cutover and rollback artifact.
const rootRoute = roomCutoverAuthoritative
  ? {
      path: '/',
      redirect: { name: 'RoomEntry' }
    }
  : {
      path: '/',
      name: 'Dashboard',
      component: () => import('../views/DashboardView.vue'),
      meta: { requiresAuth: true }
    }

const routes = [
  rootRoute,
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
    // R14d: single shared cutover-aware landing decision (false mode →
    // Dashboard; true mode → RoomEntry). AuthView's post-login push
    // uses the same helper.
    next({ name: authenticatedLandingRouteName() })
  } else {
    next()
  }
})

export default router
