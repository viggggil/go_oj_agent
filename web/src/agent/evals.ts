import { api } from '../api'
import type { PageResult } from './types'

export type EvalDataset = {
  id: string
  name: string
  description: string
  format_version: string
  content_hash: string
  case_count: number
  created_by: number
  created_at: string
  status: string
}

export type EvalCase = {
  id: string
  category: string
  title: string
  case: Record<string, unknown>
}

const base = '/api/v1/admin/agent/evals'
const options = (signal?: AbortSignal) => ({ signal })

export const evalApi = {
  datasets(signal?: AbortSignal) {
    return api.get<PageResult<EvalDataset>>(`${base}/datasets`, options(signal))
  },
  dataset(id: string, signal?: AbortSignal) {
    return api.get<{ dataset: EvalDataset }>(`${base}/datasets/${id}`, options(signal))
  },
  cases(id: string, signal?: AbortSignal) {
    return api.get<PageResult<EvalCase>>(`${base}/datasets/${id}/cases`, options(signal))
  },
  upload(payload: { name: string; description: string; content: string }, signal?: AbortSignal) {
    return api.post<{ dataset: EvalDataset; duplicate: boolean }>(`${base}/datasets`, payload, {
      ...options(signal),
      headers: { 'X-Request-ID': crypto.randomUUID() },
    })
  },
}
