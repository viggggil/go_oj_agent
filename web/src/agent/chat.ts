import { api, tokens, refreshAccessToken } from '../api'
import type { PageResult } from './types'
import {
  AgentStreamParser,
  canonicalUUID,
  ChatFailure,
  type ChatEvent,
  type StreamIdentity,
} from './stream'
export interface ChatAgent {
  key: string
  name: string
  is_default: boolean
}
export interface ChatSkill {
  description?: string
  key: string
  name: string
  execution_mode: string
  default: boolean
}
export const chatCatalog = {
  agents: (page: number, search: string, signal: AbortSignal) =>
    api.get<PageResult<ChatAgent>>('/api/v1/agent/agents', {
      params: { page, page_size: 20, search },
      signal,
      timeout: 30_000,
    }),
  skills: (key: string, signal: AbortSignal) =>
    api.get<{ items: ChatSkill[]; default_skill_key: string }>(
      `/api/v1/agent/agents/${encodeURIComponent(key)}/skills`,
      { signal, timeout: 30_000 },
    ),
}
export interface ChatRequest {
  message: string
  agent_key: string
  skill_key: string
  conversation_id?: string
}
export interface ChatConversation {
  id: string
  agent_key: string | null
  title: string
  updated_at: string
}
export const chatHistory = {
  list: (page: number, signal: AbortSignal) =>
    api.get<PageResult<ChatConversation>>('/api/v1/agent/conversations', {
      params: { page },
      signal,
    }),
  messages: (id: string, signal: AbortSignal, before?: number) =>
    api.get<{
      conversation: { id: string; agent_key: string | null; title: string }
      items: {
        id: number
        role: 'user' | 'assistant'
        content: string
        status: string | null
        error_code: string | null
      }[]
      next_before: number | null
    }>(`/api/v1/agent/conversations/${encodeURIComponent(id)}/messages`, {
      params: { before },
      signal,
    }),
}
export function modelErrorMessage(code: string): string {
  return (
    (
      {
        AGENT_MODEL_UNAVAILABLE: '模型连接中断或暂不可用，回答未完成，请重新发送。',
        AGENT_MODEL_TIMEOUT: '等待模型回答超时，请稍后重新发送。',
        AGENT_MODEL_AUTH_FAILED: '模型认证失败，请联系管理员检查 Provider 凭据。',
        AGENT_MODEL_QUOTA: '模型额度不足，请联系管理员。',
        AGENT_MODEL_RATE_LIMIT: '模型请求受限，请稍后重新发送。',
        AGENT_MODEL_INVALID_RESPONSE: '模型返回的流式回答不完整，请重新发送。',
      } as Record<string, string>
    )[code] || '回答失败或未完成，请重新发送或开启新对话。'
  )
}
export function chatHttpError(status: number): string {
  return (
    (
      {
        400: '消息或配置所需上下文不符合要求，请检查后重试',
        401: '登录已失效，请重新登录',
        403: '当前账号无法使用此 Agent',
        404: 'Agent 或会话不存在、已到期或不可用，请检查选择',
        409: '会话正在运行或配置冲突，请稍后重试或新建对话',
        413: '消息超过请求大小上限',
        429: '请求过于频繁，请稍后重试',
        503: 'Agent 服务暂不可用，请稍后重试',
        504: '回答请求超时，请重试或新建对话',
      } as Record<number, string>
    )[status] || '无法完成回答，请稍后重试'
  )
}
// 一次点击仅一个 POST。401 可共享刷新令牌，但必须由用户重新发送。
export async function streamChat(
  request: ChatRequest,
  signal: AbortSignal,
  onIdentity: (identity: StreamIdentity) => void,
  onEvent: (event: ChatEvent) => void,
) {
  const controller = new AbortController()
  const abort = () => controller.abort()
  signal.addEventListener('abort', abort, { once: true })
  if (signal.aborted) abort()
  let timedOut = false
  const timeout = () => {
    timedOut = true
    controller.abort()
  }
  const totalTimer = setTimeout(timeout, 330_000)
  let idleTimer = setTimeout(timeout, 45_000)
  let reader: ReadableStreamDefaultReader<Uint8Array> | undefined
  try {
    const access = tokens.access
    const base = new URL((api.defaults.baseURL || '/').replace(/\/?$/, '/'), window.location.origin)
    const response = await fetch(new URL('api/v1/agent/chat', base), {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Accept: 'text/event-stream',
        Authorization: `Bearer ${access}`,
      },
      body: JSON.stringify(request),
      signal: controller.signal,
      redirect: 'error',
    })
    if (!response.ok) {
      await response.body?.cancel()
      if (response.status === 401 && !signal.aborted && tokens.access === access) {
        const refreshed = await refreshAccessToken()
        throw new ChatFailure(
          refreshed ? '登录已刷新，请再次点击发送（本次消息未重发）' : chatHttpError(401),
        )
      }
      throw new ChatFailure(chatHttpError(response.status))
    }
    const identity = {
      runId: response.headers.get('X-Agent-Run-ID') || '',
      conversationId: response.headers.get('X-Agent-Conversation-ID') || '',
    }
    if (
      !/^text\/event-stream(?:\s*;|$)/i.test(response.headers.get('Content-Type') || '') ||
      !canonicalUUID(identity.runId) ||
      !canonicalUUID(identity.conversationId) ||
      (request.conversation_id && request.conversation_id !== identity.conversationId) ||
      !response.body
    ) {
      await response.body?.cancel()
      throw new ChatFailure('回答响应无效，请重试或新建对话')
    }
    onIdentity(identity)
    const parser = new AgentStreamParser(identity, onEvent)
    reader = response.body.getReader()
    while (!parser.terminal) {
      const { done, value } = await reader.read()
      if (controller.signal.aborted) throw new DOMException('Aborted', 'AbortError')
      clearTimeout(idleTimer)
      idleTimer = setTimeout(timeout, 45_000)
      if (done) {
        parser.finish()
        break
      }
      parser.push(value)
      if (parser.terminal) parser.finish()
    }
  } catch (error) {
    if (timedOut && !signal.aborted)
      throw new ChatFailure('回答请求超时，部分回答未完成，请重试或新建对话')
    throw error
  } finally {
    clearTimeout(totalTimer)
    clearTimeout(idleTimer)
    signal.removeEventListener('abort', abort)
    controller.abort()
    if (reader) {
      await reader.cancel().catch(() => undefined)
      reader.releaseLock()
    }
  }
}
