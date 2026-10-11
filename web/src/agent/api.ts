import axios from 'axios'
import { api } from '../api'
import type {
  Configuration,
  Content,
  ConfigState,
  ModelOption,
  PageResult,
  Resource,
  Summary,
  Tool,
} from './types'

const base = '/api/v1/admin/agent'
const bounded = { timeout: 30_000 }
export const agentApi = {
  list: (
    kind: Resource,
    page = 1,
    search = '',
    state: ConfigState = 'active',
    signal?: AbortSignal,
  ) =>
    api.get<PageResult<Summary>>(`${base}/${kind}`, {
      ...bounded,
      params: { page, page_size: 20, search, state },
      signal,
    }),
  detail: (kind: Resource, key: string, id?: string, signal?: AbortSignal) =>
    api.get<{ configuration: Configuration }>(
      `${base}/${kind}/${key}${id ? `/versions/${id}` : ''}`,
      { ...bounded, signal },
    ),
  version: (kind: Resource, id: string, signal?: AbortSignal) =>
    api.get<{ configuration: Configuration }>(`${base}/${kind}/versions/${id}`, {
      ...bounded,
      signal,
    }),
  history: (kind: Resource, key: string, page = 1, signal?: AbortSignal) =>
    api.get<PageResult<Summary>>(`${base}/${kind}/${key}/versions`, {
      ...bounded,
      params: { page, page_size: 20 },
      signal,
    }),
  save: (
    kind: Resource,
    key: string,
    content: Content,
    expected: string | null,
    requestId: string,
    signal?: AbortSignal,
  ) =>
    expected
      ? api.put<{ configuration: Configuration }>(
          `${base}/${kind}/${key}`,
          { expected_id: expected, content },
          { ...bounded, signal, headers: { 'X-Request-ID': requestId } },
        )
      : api.post<{ configuration: Configuration }>(
          `${base}/${kind}`,
          { key, content },
          { ...bounded, signal, headers: { 'X-Request-ID': requestId } },
        ),
  action: (
    kind: Resource,
    key: string,
    action: 'archive' | 'restore' | 'disable' | 'enable',
    expected: string,
    requestId: string,
    source?: string,
    signal?: AbortSignal,
  ) =>
    api.post<{ configuration: Configuration }>(
      `${base}/${kind}/${key}/${action}`,
      { expected_id: expected, ...(source ? { source_id: source } : {}) },
      { ...bounded, signal, headers: { 'X-Request-ID': requestId } },
    ),
  models: (page = 1, search = '', signal?: AbortSignal) =>
    api.get<PageResult<ModelOption>>(`${base}/model-options`, {
      ...bounded,
      params: { page, page_size: 20, search },
      signal,
    }),
  model: (id: string, signal?: AbortSignal) =>
    api.get<{ model_option: ModelOption }>(`${base}/model-options/${id}`, { ...bounded, signal }),
  tools: (page = 1, signal?: AbortSignal) =>
    api.get<PageResult<Tool>>(`${base}/tools`, {
      ...bounded,
      params: { page, page_size: 100 },
      signal,
    }),
}
export function agentStatus(error: unknown): number | undefined {
  return axios.isAxiosError(error) ? error.response?.status : undefined
}
export function agentError(error: unknown): string {
  if (!axios.isAxiosError(error)) return '操作失败，请重试'
  if (!error.response) return '请求未确认，请检查网络后重试'
  const body = error.response.data as { reason?: string; code?: string }
  const messages: Record<string, string> = {
    AGENT_CONFIGURATION_STALE: '配置已被修改，请查看当前版本后再保存',
    AGENT_CONFIGURATION_REQUEST_CONFLICT: '请求编号已用于其他操作，请重新确认本次内容',
    AGENT_CONFIGURATION_REFERENCE_INVALID: '绑定版本不存在、已归档或已停用，请检查依赖',
    AGENT_CONFIGURATION_TOOL_INVALID: '工具不存在或未启用，请检查工具选择',
    AGENT_CONFIGURATION_UNSUPPORTED: '模型或执行模式不受支持，请重新选择',
    AGENT_CONFIGURATION_TEST_EXPIRED: '测试 Agent 的到期时间必须在未来',
    AGENT_MODEL_ENDPOINT_DENIED: '模型地址不在部署允许范围内',
  }
  const code = body?.reason || body?.code || ''
  if (messages[code]) return messages[code]
  return (
    (
      {
        400: '内容不符合要求，请检查变量、引用和预算',
        401: '登录已失效，请重新登录',
        403: '当前账号没有管理权限',
        404: '配置不存在或无法访问',
        409: '配置冲突，请重新检查当前版本',
        413: '内容超过请求大小上限',
        503: 'Agent 管理服务暂不可用',
        504: '请求超时，可保持内容重试',
      } as Record<number, string>
    )[error.response.status] || '服务暂时不可用，请稍后重试'
  )
}
