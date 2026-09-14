import axios from 'axios'
import type { Problem, ProblemInput, ProblemSummary, Testcase, TokenPair, User } from './types'

export const api = axios.create({
  baseURL: import.meta.env.VITE_API_BASE_URL || '/',
  headers: { 'Content-Type': 'application/json' },
})
let accessToken = localStorage.getItem('access_token') || ''
let refreshToken = localStorage.getItem('refresh_token') || ''
let refreshPromise: Promise<string | null> | null = null
export const tokens = {
  get access() {
    return accessToken
  },
  set(value: string) {
    accessToken = value
    value ? localStorage.setItem('access_token', value) : localStorage.removeItem('access_token')
  },
  get refresh() {
    return refreshToken
  },
  setRefresh(value: string) {
    refreshToken = value
    value ? localStorage.setItem('refresh_token', value) : localStorage.removeItem('refresh_token')
  },
  clear() {
    accessToken = ''
    refreshToken = ''
    localStorage.removeItem('access_token')
    localStorage.removeItem('refresh_token')
  },
}
api.interceptors.request.use((config) => {
  if (accessToken) config.headers.Authorization = `Bearer ${accessToken}`
  return config
})
api.interceptors.response.use(
  (response) => response,
  async (error) => {
    const original = error.config
    if (
      error.response?.status !== 401 ||
      original?._retry ||
      !refreshToken ||
      original?.url?.includes('/auth/refresh')
    )
      throw error
    original._retry = true
    refreshPromise ||= api
      .post<TokenPair>('/api/v1/auth/refresh', { refresh_token: refreshToken })
      .then(({ data }) => {
        tokens.set(data.access_token)
        tokens.setRefresh(data.refresh_token)
        return data.access_token
      })
      .catch(() => {
        tokens.clear()
        return null
      })
      .finally(() => {
        refreshPromise = null
      })
    const next = await refreshPromise
    if (!next) throw error
    original.headers.Authorization = `Bearer ${next}`
    return api(original)
  },
)
export const authApi = {
  register: (payload: { username: string; email: string; password: string }) =>
    api.post('/api/v1/auth/register', payload),
  login: (payload: { account: string; password: string }) =>
    api.post<TokenPair>('/api/v1/auth/login', payload),
  profile: () => api.get<{ user: User }>('/api/v1/users/me'),
  logout: () => api.post('/api/v1/auth/logout', { refresh_token: refreshToken }),
}
export const problemApi = {
  list: (page = 1, pageSize = 20) =>
    api.get<{ items: ProblemSummary[]; page: { page: number; page_size: number; total: number } }>(
      '/api/v1/problems',
      { params: { 'page.page': page, 'page.page_size': pageSize } },
    ),
  get: (id: number) => api.get<{ problem: Problem }>(`/api/v1/problems/${id}`),
  create: (problem: ProblemInput) =>
    api.post<{ problem: Problem }>('/api/v1/problems', { problem }),
  update: (id: number, problem: ProblemInput) =>
    api.put<{ problem: Problem }>(`/api/v1/problems/${id}`, problem),
  archive: (id: number) => api.delete(`/api/v1/problems/${id}`),
  testcases: (id: number, includeArchived = false) =>
    api.get<{ items: Testcase[] }>(`/api/v1/problems/${id}/testcases`, {
      params: { include_archived: includeArchived },
    }),
  uploadTestcase: (id: number, caseNo: number, input: File, output: File) => {
    const form = new FormData()
    form.append('case_no', String(caseNo))
    form.append('input', input)
    form.append('output', output)
    return api.post<{ testcase: Testcase }>(`/api/v1/problems/${id}/testcases/upload`, form, {
      headers: { 'Content-Type': undefined },
    })
  },
  archiveTestcase: (problemId: number, testcaseId: number) =>
    api.delete(`/api/v1/problems/${problemId}/testcases/${testcaseId}`),
}
