import { createRouter, createWebHistory } from 'vue-router'

import { tokens } from './api'
import Login from './views/Login.vue'
import Profile from './views/Profile.vue'
import Register from './views/Register.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', redirect: '/profile' },
    { path: '/login', component: Login },
    { path: '/register', component: Register },
    { path: '/profile', component: Profile, meta: { auth: true } },
  ],
})

router.beforeEach((to) => {
  if (to.meta.auth && !tokens.access) {
    return '/login'
  }

  if ((to.path === '/login' || to.path === '/register') && tokens.access) {
    return '/profile'
  }

  return true
})

export default router
