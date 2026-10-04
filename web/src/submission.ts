import { api, tokenState } from './api'
import type { JudgeResult, SubmissionStatus } from './types'

export const statusLabels: Record<string, string> = {
  SUBMISSION_STATUS_QUEUED: '排队中',
  SUBMISSION_STATUS_COMPILING: '编译中',
  SUBMISSION_STATUS_RUNNING: '运行中',
  SUBMISSION_STATUS_DONE: '已完成',
  SUBMISSION_STATUS_RETRY_WAIT: '等待重试',
  SUBMISSION_STATUS_CANCELLED: '已取消',
  SUBMISSION_STATUS_INVALIDATED: '已失效',
  SUBMISSION_STATUS_UNSPECIFIED: '未知状态',
}

export const verdictLabels: Record<string, string> = {
  JUDGE_VERDICT_AC: 'AC · 通过',
  JUDGE_VERDICT_WA: 'WA · 答案错误',
  JUDGE_VERDICT_TLE: 'TLE · 超时',
  JUDGE_VERDICT_MLE: 'MLE · 超内存',
  JUDGE_VERDICT_RE: 'RE · 运行错误',
  JUDGE_VERDICT_CE: 'CE · 编译错误',
  JUDGE_VERDICT_SYSTEM_ERROR: '系统错误',
  JUDGE_VERDICT_UNSPECIFIED: '等待判题',
}

const statusNumbers: Record<number, string> = {
  0: 'SUBMISSION_STATUS_UNSPECIFIED',
  1: 'SUBMISSION_STATUS_QUEUED',
  2: 'SUBMISSION_STATUS_COMPILING',
  3: 'SUBMISSION_STATUS_RUNNING',
  4: 'SUBMISSION_STATUS_DONE',
  5: 'SUBMISSION_STATUS_RETRY_WAIT',
  6: 'SUBMISSION_STATUS_CANCELLED',
  7: 'SUBMISSION_STATUS_INVALIDATED',
}
const verdictNumbers: Record<number, string> = {
  0: 'JUDGE_VERDICT_UNSPECIFIED',
  1: 'JUDGE_VERDICT_AC',
  2: 'JUDGE_VERDICT_WA',
  3: 'JUDGE_VERDICT_TLE',
  4: 'JUDGE_VERDICT_MLE',
  5: 'JUDGE_VERDICT_RE',
  6: 'JUDGE_VERDICT_CE',
  7: 'JUDGE_VERDICT_SYSTEM_ERROR',
}

export function enumLabel(value: string | number | undefined, labels: Record<string, string>) {
  if (typeof value === 'number') {
    const key = labels === statusLabels ? statusNumbers[value] : verdictNumbers[value]
    return key ? labels[key] : '未知'
  }
  return labels[value || ''] || value || '未知'
}

export function statusLabel(value: SubmissionStatus | number | undefined) {
  return enumLabel(value, statusLabels)
}

export function verdictLabel(value: string | number | undefined) {
  return enumLabel(value, verdictLabels)
}

export function isTerminalStatus(value: string | number | undefined) {
  return (
    value === 'SUBMISSION_STATUS_DONE' ||
    value === 'SUBMISSION_STATUS_CANCELLED' ||
    value === 'SUBMISSION_STATUS_INVALIDATED' ||
    value === 4 ||
    value === 6 ||
    value === 7
  )
}

export function verdictClass(value: string | number | undefined) {
  const key =
    typeof value === 'string' ? value.replace('JUDGE_VERDICT_', '').toLowerCase() : 'pending'
  return `verdict verdict-${key}`
}

export function formatDate(value?: string | { seconds?: number | string; nanos?: number }) {
  if (!value) return '—'
  const date =
    typeof value === 'object'
      ? new Date(Number(value.seconds || 0) * 1000 + Number(value.nanos || 0) / 1e6)
      : new Date(value)
  return Number.isNaN(date.valueOf()) ? '—' : date.toLocaleString('zh-CN', { hour12: false })
}

export function formatBytes(value: number) {
  if (!value) return '0 B'
  if (value < 1024) return `${value} B`
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`
  return `${(value / 1024 / 1024).toFixed(2)} MB`
}

export async function pollJudgeResult(id: number) {
  return (await api.get<{ result: JudgeResult }>(`/api/v1/submissions/${id}/result`)).data.result
}

export async function streamJudgeEvents(
  id: number,
  onResult: (result: JudgeResult) => void,
  signal: AbortSignal,
) {
  const base = String(api.defaults.baseURL || window.location.origin)
  const url = new URL(`/api/v1/submissions/${id}/events`, new URL(base, window.location.origin))
  const response = await fetch(url, {
    headers: tokenState.access ? { Authorization: `Bearer ${tokenState.access}` } : {},
    signal,
  })
  if (!response.ok || !response.body) throw new Error(`SSE request failed: ${response.status}`)
  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  while (true) {
    const chunk = await reader.read()
    if (chunk.done) break
    buffer += decoder.decode(chunk.value, { stream: true })
    const frames = buffer.split('\n\n')
    buffer = frames.pop() || ''
    for (const frame of frames) {
      const event = frame
        .split('\n')
        .find((line) => line.startsWith('event:'))
        ?.slice(6)
        .trim()
      const data = frame
        .split('\n')
        .filter((line) => line.startsWith('data:'))
        .map((line) => line.slice(5).trim())
        .join('\n')
      if (!data) continue
      if (event === 'submission.error') throw new Error(JSON.parse(data) as string)
      const parsed = JSON.parse(data) as JudgeResult
      onResult(parsed)
      if (isTerminalStatus(parsed.status)) return
    }
  }
}
