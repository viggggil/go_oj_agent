import { defineStore } from 'pinia'
import axios from 'axios'
import { authApi, tokens, tokenState } from '../api'
import type { User } from '../types'
import { agentAdministrator, clearAgentDrafts } from '../agent/state'

export const useAuthStore = defineStore('auth', {
  state: () => ({ user: null as User | null, loading: false, profileError: false }),
  getters: { isAuthenticated: () => Boolean(tokenState.access) },
  actions: {
    async login(account: string, password: string) {
      clearAgentDrafts()
      this.user = null
      this.loading = true
      try {
        const { data } = await authApi.login({ account, password })
        tokens.set(data.access_token)
        tokens.setRefresh(data.refresh_token)
        await this.fetchProfile()
      } finally {
        this.loading = false
      }
    },
    async register(username: string, email: string, password: string) {
      await authApi.register({ username, email, password })
    },
    async fetchProfile() {
      try {
        const { data } = await authApi.profile()
        if (this.user?.id !== data.user.id || !agentAdministrator(data.user.roles))
          clearAgentDrafts()
        this.user = data.user
        this.profileError = false
      } catch (error) {
        this.profileError = true
        if (
          !tokens.access ||
          (axios.isAxiosError(error) && [401, 403].includes(error.response?.status || 0))
        ) {
          this.user = null
          clearAgentDrafts()
        }
        throw error
      }
    },
    async logout() {
      const request = tokens.refresh ? authApi.logout() : Promise.resolve()
      tokens.clear()
      this.user = null
      clearAgentDrafts()
      await request
    },
    clear() {
      tokens.clear()
      this.user = null
      clearAgentDrafts()
    },
  },
})
