import { defineStore } from 'pinia'
import { authApi, tokens } from '../api'
import type { User } from '../types'

export const useAuthStore = defineStore('auth', { state: () => ({ user: null as User | null, loading: false }), getters: { isAuthenticated: () => Boolean(tokens.access) }, actions: {
  async login(account: string, password: string) { this.loading = true; try { const { data } = await authApi.login({ account, password }); tokens.set(data.access_token); tokens.setRefresh(data.refresh_token); await this.fetchProfile() } finally { this.loading = false } },
  async register(username: string, email: string, password: string) { await authApi.register({ username, email, password }) },
  async fetchProfile() { const { data } = await authApi.profile(); this.user = data.user },
  async logout() { try { if (tokens.refresh) await authApi.logout() } finally { tokens.clear(); this.user = null } },
  clear() { tokens.clear(); this.user = null },
} })
