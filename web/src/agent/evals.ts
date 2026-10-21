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
export type EvalCaseData = {
  id: string
  category: string
  title: string
  turns: { role: string; content: string }[]
  fixture: Record<string, unknown>
  expectations: Record<string, unknown>
}
export type EvalCase = { id: string; category: string; title: string; case: EvalCaseData }
export type EvalRun = {
  id: string
  dataset_id: string
  agent_key: string
  mode: 'programmatic' | 'programmatic_review'
  reviewer_agent_key: string | null
  reviewer_snapshot: Record<string, unknown> | null
  repeat_count: number
  concurrency: number
  status: 'QUEUED' | 'RUNNING' | 'COMPLETED' | 'FAILED' | 'CANCELLED'
  total_cases: number
  completed_cases: number
  hard_failures: number
  error_count: number
  config_snapshot: Record<string, unknown>
  cancel_requested: boolean
  version: number
  created_at: string
  started_at: string | null
  finished_at: string | null
}
export type EvalMetrics = {
  recorded_cases: number
  check_passes: number
  effective_passes: number
  reviewed_cases: number
  model_reviewed_cases: number
  model_review_unavailable: number
  model_passes: number
  average_latency_ms: number | null
}
export const scoreDimensions = [
  ['task_completion', '任务完成'],
  ['technical_correctness', '技术正确性'],
  ['evidence_support', '证据支持'],
  ['instruction_following', '指令遵循'],
  ['clarity', '表达清晰度'],
] as const
export type EvalScores = Record<(typeof scoreDimensions)[number][0], number>
export type EvalReview = {
  pass: boolean
  scores: EvalScores
  tags: string[]
  note: string
  reviewed_by: number
  reviewed_at: string
}
export type EvalModelReview = {
  case_id: string
  pass: boolean
  scores: EvalScores
  hard_fail: boolean
  violations: string[]
  missing_facts: string[]
  reason: string
}
export type EvalModelReviewAttempt = {
  id: string
  result_id: string
  attempt: number
  requested_by: number
  status: 'RUNNING' | 'COMPLETED' | 'UNAVAILABLE'
  review: EvalModelReview | null
  error_code: string | null
  created_at: string
  finished_at: string | null
}
export type EvalResultSummary = {
  id: string
  run_id: string
  case_id: string
  category: string
  title: string
  attempt: number
  status: string
  checks: { pass: boolean; hard_fail: boolean } & Record<string, unknown>
  error_code: string | null
  latency_ms: number | null
  review_version: number
  manual_review: Pick<EvalReview, 'pass'> | null
  model_review_status:
    'NOT_REQUESTED' | 'PENDING' | 'RUNNING' | 'COMPLETED' | 'UNAVAILABLE' | 'SKIPPED'
  model_review_attempt: number
  model_review: Pick<EvalModelReview, 'pass' | 'hard_fail'> | null
  model_review_error_code: string | null
  effective_pass: boolean
  answer_preview: string
}
export type EvalResult = Omit<
  EvalResultSummary,
  'manual_review' | 'answer_preview' | 'model_review'
> & {
  answer: string | null
  model_summary: Record<string, unknown> | null
  manual_review: EvalReview | null
  model_review: EvalModelReview | null
  case: EvalCaseData
}
export type EvalReviewAudit = {
  id: string
  result_id: string
  version: number
  reviewed_by: number
  old_value: EvalReview | null
  new_value: EvalReview
  reason: string
  created_at: string
}
export type EvalReviewWrite = {
  expected_version: number
  scores: EvalScores
  pass: boolean
  tags: string[]
  note: string
  reason: string
}
export type EvalRunWrite = {
  dataset_id: string
  agent_key: string
  concurrency: number
  repeat_count: number
}
export type EvalFilters = {
  status?: string
  verdict?: string
  category?: string
  error_code?: string
  search?: string
}
const base = '/api/v1/admin/agent/evals'
const options = (signal?: AbortSignal) => ({ signal, timeout: 30_000 })
const writeOptions = (requestId: string, signal?: AbortSignal) => ({
  ...options(signal),
  headers: { 'X-Request-ID': requestId },
})
export const evalApi = {
  datasets(signal?: AbortSignal, page = 1, search = '') {
    return api.get<PageResult<EvalDataset>>(`${base}/datasets`, {
      ...options(signal),
      params: { page, page_size: 20, search },
    })
  },
  dataset(id: string, signal?: AbortSignal) {
    return api.get<{ dataset: EvalDataset }>(`${base}/datasets/${id}`, options(signal))
  },
  cases(id: string, signal?: AbortSignal, page = 1) {
    return api.get<PageResult<EvalCase>>(`${base}/datasets/${id}/cases`, {
      ...options(signal),
      params: { page, page_size: 5 },
    })
  },
  upload(payload: { name: string; description: string; content: string }, signal?: AbortSignal) {
    return api.post<{ dataset: EvalDataset; duplicate: boolean }>(
      `${base}/datasets`,
      payload,
      writeOptions(crypto.randomUUID(), signal),
    )
  },
  runs(page = 1, status = '', signal?: AbortSignal) {
    return api.get<PageResult<EvalRun>>(`${base}/runs`, {
      ...options(signal),
      params: { page, page_size: 20, status },
    })
  },
  create(payload: EvalRunWrite, requestId: string, signal?: AbortSignal) {
    return api.post<{ run: EvalRun; duplicate: boolean }>(
      `${base}/runs`,
      payload,
      writeOptions(requestId, signal),
    )
  },
  run(id: string, signal?: AbortSignal) {
    return api.get<{ run: EvalRun; metrics: EvalMetrics }>(`${base}/runs/${id}`, options(signal))
  },
  cancel(run: EvalRun, requestId: string, signal?: AbortSignal) {
    return api.post<{ run: EvalRun }>(
      `${base}/runs/${run.id}/cancel`,
      {},
      {
        ...writeOptions(requestId, signal),
        params: { version: run.version },
      },
    )
  },
  results(id: string, page = 1, filters: EvalFilters = {}, signal?: AbortSignal) {
    return api.get<PageResult<EvalResultSummary>>(`${base}/runs/${id}/results`, {
      ...options(signal),
      params: { page, page_size: 20, ...filters },
    })
  },
  result(runId: string, resultId: string, signal?: AbortSignal) {
    return api.get<{ result: EvalResult }>(
      `${base}/runs/${runId}/results/${resultId}`,
      options(signal),
    )
  },
  review(id: string, payload: EvalReviewWrite, requestId: string, signal?: AbortSignal) {
    return api.post<{ review: EvalReview; review_version: number; duplicate: boolean }>(
      `${base}/results/${id}/review`,
      payload,
      writeOptions(requestId, signal),
    )
  },
  reviews(id: string, page = 1, signal?: AbortSignal) {
    return api.get<PageResult<EvalReviewAudit>>(`${base}/results/${id}/reviews`, {
      ...options(signal),
      params: { page, page_size: 10 },
    })
  },
  retryModelReview(id: string, expected_attempt: number, requestId: string, signal?: AbortSignal) {
    return api.post<{ attempt: EvalModelReviewAttempt; duplicate: boolean }>(
      `${base}/results/${id}/review-agent`,
      { expected_attempt },
      writeOptions(requestId, signal),
    )
  },
  modelReviews(id: string, page = 1, signal?: AbortSignal) {
    return api.get<PageResult<EvalModelReviewAttempt>>(`${base}/results/${id}/model-reviews`, {
      ...options(signal),
      params: { page, page_size: 10 },
    })
  },
}
export const evalRunStatus: Record<EvalRun['status'], string> = {
  QUEUED: '排队中',
  RUNNING: '运行中',
  COMPLETED: '执行完成',
  FAILED: '执行失败',
  CANCELLED: '已取消',
}
