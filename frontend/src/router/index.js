import { createRouter, createWebHistory } from 'vue-router'
import { globalStore } from '../store'

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
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

router.beforeEach((to, from, next) => {
  const isAuthenticated = !!globalStore.currentUser

  if (to.meta.requiresAuth && !isAuthenticated) {
    next({ name: 'Auth' })
  } else if (to.name === 'Auth' && isAuthenticated) {
    next({ name: 'Dashboard' })
  } else {
    next()
  }
})

export default router
