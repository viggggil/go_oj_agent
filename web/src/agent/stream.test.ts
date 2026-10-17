import { expect, it } from 'vitest'
import { AgentStreamParser, type ChatEvent } from './stream'
const identity = {
  runId: '11111111-1111-4111-8111-111111111111',
  conversationId: '22222222-2222-4222-8222-222222222222',
}
const encode = new TextEncoder()
const event = (type: string, sequence: number, data: object = {}) => ({
  type,
  sequence,
  run_id: identity.runId,
  conversation_id: identity.conversationId,
  data,
})
const frame = (value: ReturnType<typeof event>) =>
  `event: ${value.type}\ndata: ${JSON.stringify(value)}\n\n`
it('逐字节 UTF-8 / CRLF 拆帧、注释和多行 data 保持中文 token 与终态', () => {
  const seen: ChatEvent[] = [],
    parser = new AgentStreamParser(identity, (value) => seen.push(value))
  const body =
    ': heartbeat\r\n\r\n' +
    frame(event('thinking', 1, { text: '准备' })) +
    frame(event('token', 2, { text: '中文😀' })).replace(',"sequence"', ',\ndata: "sequence"') +
    frame(event('done', 3))
  for (const byte of encode.encode(body.replace(/(?<!\r)\n/g, '\r\n')))
    parser.push(new Uint8Array([byte]))
  parser.finish()
  expect(seen.map((value) => value.type)).toEqual(['thinking', 'token', 'done'])
  expect(seen[1]?.data.text).toBe('中文😀')
})
it('身份、event/type、递增序号与公开 payload 异常都拒绝', () => {
  const invalid = [
    { ...event('token', 2, { text: '私有' }), run_id: identity.conversationId },
    { ...event('token', 2, { text: '私有' }), conversation_id: identity.runId },
    event('token', 1, { text: '重复' }),
    event('done', 2, { hidden: 'secret' }),
    event('token', 2, { text: 'x', secret: 'value' }),
    event('unexpected', 2),
    { ...event('token', 2, { text: 'x' }), type: 'thinking' },
  ]
  for (const value of invalid) {
    const parser = new AgentStreamParser(identity, () => {})
    parser.push(encode.encode(frame(event('thinking', 1, { text: '' }))))
    const body =
      value === invalid.at(-1)
        ? frame(value).replace('event: thinking', 'event: token')
        : frame(value)
    expect(() => parser.push(encode.encode(body))).toThrow()
  }
})
it('error 是失败终态；EOF、非法 UTF-8、帧和总量超限不能伪装 done', () => {
  const seen: ChatEvent[] = [],
    parser = new AgentStreamParser(identity, (value) => seen.push(value))
  parser.push(encode.encode(frame(event('error', 1, { code: 'AGENT_RUN_FAILED' }))))
  parser.finish()
  expect(seen[0]?.type).toBe('error')
  for (const code of [
    'AGENT_GATEWAY_TIMEOUT',
    'AGENT_UPSTREAM_TIMEOUT',
    'AGENT_UPSTREAM_INTERRUPTED',
    'AGENT_UPSTREAM_INVALID_EVENT',
  ]) {
    const gateway = new AgentStreamParser(identity, (value) => expect(value.data.code).toBe(code))
    gateway.push(encode.encode(frame(event('error', 1, { code }))))
    gateway.finish()
  }
  expect(() => new AgentStreamParser(identity, () => {}).finish()).toThrow(/未完成/)
  expect(() => new AgentStreamParser(identity, () => {}).push(new Uint8Array([255]))).toThrow(
    /编码/,
  )
  expect(() =>
    new AgentStreamParser(identity, () => {}, { frame: 10, total: 100 }).push(
      encode.encode('data: 123456789'),
    ),
  ).toThrow(/帧/)
  expect(() =>
    new AgentStreamParser(identity, () => {}, { frame: 100, total: 5 }).push(
      encode.encode(':heartbeat\n\n'),
    ),
  ).toThrow(/上限/)
})
it('done 后不接受额外业务帧或不完整尾帧', () => {
  const parser = new AgentStreamParser(identity, () => {})
  expect(() =>
    parser.push(
      encode.encode(frame(event('done', 1)) + frame(event('token', 2, { text: 'late' }))),
    ),
  ).toThrow()
  const other = new AgentStreamParser(identity, () => {})
  other.push(encode.encode(frame(event('done', 1)) + 'data: tail'))
  expect(() => other.finish()).toThrow()
})

it('公开过程与工具时间线关联校验，拒绝原始结果、错配、重复和未完成终态', () => {
  const call = {
    step: 1,
    call_id: 'call_1',
    tool: 'get_problem',
    status: 'started',
    label: '查询题目',
  }
  const result = {
    step: 1,
    call_id: 'call_1',
    tool: 'get_problem',
    status: 'succeeded',
    duration_ms: 12,
    summary: '已获取授权信息',
  }
  const seen: ChatEvent[] = []
  const parser = new AgentStreamParser(identity, (value) => seen.push(value))
  const events = [
    event('progress', 1, { phase: 'preparing', message: '准备上下文' }),
    event('reasoning_summary', 2, { step: 1, text: '需要确认授权信息' }),
    event('tool_call', 3, call),
    event('tool_result', 4, result),
    event('token', 5, { text: '最终答案' }),
    event('done', 6),
  ]
  for (const value of events) parser.push(encode.encode(frame(value)))
  parser.finish()
  expect(
    seen
      .filter((value) => value.type === 'token')
      .map((value) => value.data.text)
      .join(''),
  ).toBe('最终答案')
  for (const invalid of [
    event('tool_result', 2, result),
    event('tool_result', 2, { ...result, output: 'private' }),
    event('reasoning_summary', 2, { step: 1, text: 'x'.repeat(401) }),
    event('progress', 2, { phase: 'invented', message: 'x' }),
  ]) {
    const other = new AgentStreamParser(identity, () => {})
    expect(() => other.push(encode.encode(frame(invalid)))).toThrow()
  }
  for (const invalid of [
    event('tool_call', 2, call),
    event('tool_result', 2, { ...result, call_id: 'other' }),
    event('tool_result', 2, { ...result, tool: 'other_tool' }),
    event('done', 2),
  ]) {
    const other = new AgentStreamParser(identity, () => {})
    other.push(encode.encode(frame(event('tool_call', 1, call))))
    expect(() => other.push(encode.encode(frame(invalid)))).toThrow()
  }
})
