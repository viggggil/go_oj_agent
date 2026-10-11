// Gateway SSE 的独立有界解码器；只接受完整、通过校验的公开事件。
export interface StreamIdentity {
  runId: string
  conversationId: string
}
export interface ChatEvent {
  type: 'thinking' | 'token' | 'done' | 'error'
  run_id: string
  conversation_id: string
  sequence: number
  data: { text?: string; code?: string }
}
export class ChatFailure extends Error {}
export const canonicalUUID = (value: string) =>
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(value)
const object = (value: unknown): value is Record<string, unknown> =>
  typeof value === 'object' && value !== null && !Array.isArray(value)
const publicErrors = new Set([
  'AGENT_DEADLINE_EXCEEDED',
  'AGENT_RUNTIME_INCOMPLETE',
  'AGENT_EVENT_LIMIT',
  'AGENT_RUNTIME_INVALID_EVENT',
  'AGENT_OUTPUT_LIMIT',
  'AGENT_RUNTIME_FAILED',
  'AGENT_RUN_FAILED',
  'AGENT_MODEL_AUTH_FAILED',
  'AGENT_MODEL_QUOTA',
  'AGENT_MODEL_RATE_LIMIT',
  'AGENT_MODEL_UNAVAILABLE',
  'AGENT_MODEL_TIMEOUT',
  'AGENT_MODEL_INVALID_RESPONSE',
  'AGENT_MODEL_REJECTED',
  'AGENT_MODEL_INPUT_LIMIT',
  'AGENT_MODEL_OUTPUT_LIMIT',
  'AGENT_MODEL_CALL_LIMIT',
  'AGENT_MODEL_BLOCKED',
  'AGENT_MODEL_UNSUPPORTED_OUTPUT',
  'AGENT_MODEL_ENDPOINT_DENIED',
  'AGENT_CREDENTIAL_UNAVAILABLE',
  'AGENT_GATEWAY_TIMEOUT',
  'AGENT_UPSTREAM_TIMEOUT',
  'AGENT_UPSTREAM_INTERRUPTED',
  'AGENT_UPSTREAM_INVALID_EVENT',
])
const bytes = (value: string) => new TextEncoder().encode(value).length

export class AgentStreamParser {
  private decoder = new TextDecoder('utf-8', { fatal: true })
  private pending = ''
  private lines: string[] = []
  private frameSize = 0
  private total = 0
  private sequence = 0
  terminal = false
  constructor(
    private identity: StreamIdentity,
    private onEvent: (event: ChatEvent) => void,
    private limits = { frame: 262144, total: 8 * 1024 * 1024 },
  ) {}
  push(chunk: Uint8Array) {
    this.total += chunk.byteLength
    if (this.total > this.limits.total) throw new ChatFailure('回答超过接收上限，未完成')
    try {
      this.pending += this.decoder.decode(chunk, { stream: true })
    } catch {
      throw new ChatFailure('回答编码无效，未完成')
    }
    let end: number
    while ((end = this.pending.indexOf('\n')) >= 0) {
      const raw = this.pending.slice(0, end)
      this.pending = this.pending.slice(end + 1)
      this.frameSize += bytes(raw) + 1
      if (this.frameSize > this.limits.frame) throw new ChatFailure('回答帧超过接收上限，未完成')
      const line = raw.endsWith('\r') ? raw.slice(0, -1) : raw
      if (line === '') {
        this.frame()
        this.lines = []
        this.frameSize = 0
      } else this.lines.push(line)
    }
    if (this.frameSize + bytes(this.pending) > this.limits.frame)
      throw new ChatFailure('回答帧超过接收上限，未完成')
  }
  finish() {
    try {
      this.decoder.decode()
    } catch {
      throw new ChatFailure('回答编码不完整，未完成')
    }
    if (!this.terminal || this.pending || this.lines.length)
      throw new ChatFailure('连接提前结束，回答未完成，请重新发送或新建对话')
  }
  private frame() {
    let kind = ''
    const data: string[] = []
    for (const line of this.lines) {
      if (line.startsWith(':')) continue
      const colon = line.indexOf(':')
      if (colon < 0) return this.invalid()
      const key = line.slice(0, colon),
        value = line.slice(colon + 1).replace(/^ /, '')
      if (key === 'event' && !kind) kind = value
      else if (key === 'data') data.push(value)
      else return this.invalid()
    }
    if (!kind && !data.length) return
    if (this.terminal) return this.invalid()
    let event: unknown
    try {
      event = JSON.parse(data.join('\n'))
    } catch {
      return this.invalid()
    }
    if (
      !object(event) ||
      Object.keys(event).sort().join(',') !== 'conversation_id,data,run_id,sequence,type' ||
      event.type !== kind ||
      event.run_id !== this.identity.runId ||
      event.conversation_id !== this.identity.conversationId ||
      !Number.isSafeInteger(event.sequence) ||
      (event.sequence as number) <= this.sequence ||
      (event.sequence as number) > 10002 ||
      !object(event.data)
    )
      return this.invalid()
    const payload = event.data
    if (kind === 'token' || kind === 'thinking') {
      if (
        Object.keys(payload).join(',') !== 'text' ||
        typeof payload.text !== 'string' ||
        [...payload.text].length > (kind === 'thinking' ? 256 : 32000)
      )
        return this.invalid()
    } else if (kind === 'done') {
      if (Object.keys(payload).length) return this.invalid()
    } else if (kind === 'error') {
      if (
        Object.keys(payload).join(',') !== 'code' ||
        typeof payload.code !== 'string' ||
        !publicErrors.has(payload.code)
      )
        return this.invalid()
    } else return this.invalid()
    this.sequence = event.sequence as number
    this.terminal = kind === 'done' || kind === 'error'
    this.onEvent(event as unknown as ChatEvent)
  }
  private invalid(): never {
    throw new ChatFailure('回答流格式异常，未完成')
  }
}
