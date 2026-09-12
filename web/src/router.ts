import { createRouter, createWebHistory } from 'vue-router'
import { tokens } from './api'
import Login from './views/Login.vue'
import Register from './views/Register.vue'
import Profile from './views/Profile.vue'
export default createRouter({ history: createWebHistory(), routes: [{ path: '/', redirect: '/profile' }, { path: '/login', component: Login }, { path: '/register', component: Register }, { path: '/profile', component: Profile, meta: { auth: true } }] }).beforeEach((to) => { if (to.meta.auth && !tokens.access) return '/login'; if ((to.path === '/login' || to.path === '/register') && tokens.access) return '/profile' })
