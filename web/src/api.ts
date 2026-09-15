import axios from 'axios'
import { reactive } from 'vue'
import type { Problem, ProblemInput, ProblemSummary, Testcase, TokenPair, User } from './types'

export const api = axios.create({
  baseURL: import.meta.env.VITE_API_BASE_URL || '/',
  headers: { 'Content-Type': 'application/json' },
})
export const tokenState = reactive({
  access: localStorage.getItem('access_token') || '',
  refresh: localStorage.getItem('refresh_token') || '',
})
let refreshPromise: Promise<string | null> | null = null
export const tokens = {
  get access() {
    return tokenState.access
  },
  set(value: string) {
    tokenState.access = value
    value ? localStorage.setItem('access_token', value) : localStorage.removeItem('access_token')
  },
  get refresh() {
    return tokenState.refresh
  },
  setRefresh(value: string) {
    tokenState.refresh = value
    value ? localStorage.setItem('refresh_token', value) : localStorage.removeItem('refresh_token')
  },
  clear() {
    tokenState.access = ''
    tokenState.refresh = ''
    localStorage.removeItem('access_token')
    localStorage.removeItem('refresh_token')
  },
}
api.interceptors.request.use((config) => {
  if (tokenState.access) config.headers.Authorization = `Bearer ${tokenState.access}`
  return config
})
api.interceptors.response.use(
  (response) => response,
  async (error) => {
    const original = error.config
    if (
      error.response?.status !== 401 ||
      original?._retry ||
      !tokenState.refresh ||
      original?.url?.includes('/auth/refresh')
    )
      throw error
    original._retry = true
    refreshPromise ||= api
      .post<TokenPair>('/api/v1/auth/refresh', { refresh_token: tokenState.refresh })
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
  logout: () => api.post('/api/v1/auth/logout', { refresh_token: tokenState.refresh }),
}

export function apiErrorMessage(error: unknown, action: string): string {
  if (!axios.isAxiosError(error)) return `${action}失败，请稍后重试`
  if (!error.response) return `无法连接服务器，请检查网络后重试`
  const status = error.response.status
  const body = error.response.data as { message?: string; reason?: string } | undefined
  const detail = body?.message?.trim()
  if (status === 400) return detail || '提交内容不符合要求'
  if (status === 401) return '登录状态已失效，请重新登录'
  if (status === 403) return '当前账号没有执行此操作的权限'
  if (status === 404) return '题目不存在或已归档'
  if (status === 409) return detail || 'Slug 或测试用例序号已经存在'
  if (status === 412) return detail || '题目当前状态不允许此操作'
  if (status >= 500) return '服务暂时不可用，请稍后重试'
  return detail || `${action}失败，请稍后重试`
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
