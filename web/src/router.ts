import { createRouter, createWebHistory } from 'vue-router'

import { tokens } from './api'
import Login from './views/Login.vue'
import Profile from './views/Profile.vue'
import Register from './views/Register.vue'
import Problems from './views/Problems.vue'
import ProblemDetail from './views/ProblemDetail.vue'
import ProblemEditor from './views/ProblemEditor.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', redirect: '/problems' },
    { path: '/login', component: Login },
    { path: '/register', component: Register },
    { path: '/profile', component: Profile, meta: { auth: true } },
    { path: '/problems', component: Problems, meta: { auth: true } },
    { path: '/problems/new', component: ProblemEditor, meta: { auth: true, admin: true } },
    { path: '/problems/:id', component: ProblemDetail, meta: { auth: true } },
    { path: '/problems/:id/edit', component: ProblemEditor, meta: { auth: true, admin: true } },
  ],
})

router.beforeEach(async (to) => {
  if (to.meta.auth && !tokens.access) {
    return '/login'
  }

  if (to.meta.admin) {
    const { useAuthStore } = await import('./stores/auth')
    const auth = useAuthStore()
    if (!auth.user) await auth.fetchProfile().catch(() => undefined)
    if (!auth.user?.roles.includes('admin')) return '/problems'
  }

  if ((to.path === '/login' || to.path === '/register') && tokens.access) {
    return '/profile'
  }

  return true
})

export default router
