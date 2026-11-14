import { expect, it, vi } from 'vitest'
import { evalVerdict, type EvalResultSummary } from './evals'

vi.mock('../api', () => ({ api: {} }))

it('语义评审完成前显示待评审，历史结果和人工结论保持兼容', () => {
  const result: Parameters<typeof evalVerdict>[0] = {
    status: 'COMPLETED',
    checks: { pass: true, hard_fail: false, grading_mode: 'semantic_v2' },
    manual_review: null,
    model_review_status: 'NOT_REQUESTED',
    effective_pass: false,
  }
  for (const status of ['NOT_REQUESTED', 'PENDING', 'RUNNING', 'UNAVAILABLE'] as const) {
    expect(evalVerdict({ ...result, model_review_status: status })).toBe('待评审')
  }
  expect(evalVerdict({ ...result, model_review_status: 'COMPLETED', effective_pass: true })).toBe(
    '通过',
  )
  expect(evalVerdict({ ...result, model_review_status: 'COMPLETED' })).toBe('未通过')
  expect(
    evalVerdict({ ...result, checks: { pass: true, hard_fail: false }, effective_pass: true }),
  ).toBe('通过')
  expect(evalVerdict({ ...result, manual_review: { pass: true }, effective_pass: true })).toBe(
    '通过',
  )
  expect(evalVerdict({ ...result, manual_review: { pass: false } })).toBe('未通过')
  expect(evalVerdict({ ...result, status: 'FAILED' })).toBe('未通过')
  const hardFail: EvalResultSummary['checks'] = { pass: false, hard_fail: true }
  expect(evalVerdict({ ...result, checks: hardFail, manual_review: { pass: true } })).toBe('未通过')
})
