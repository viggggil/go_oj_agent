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
  AgentRun,
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
          { expected_id: expected, content: writableContent(kind, content) },
          { ...bounded, signal, headers: { 'X-Request-ID': requestId } },
        )
      : api.post<{ configuration: Configuration }>(
          `${base}/${kind}`,
          { ...(key ? { key } : {}), content: writableContent(kind, content) },
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
  saveToolDescription: (name: string, description: string, signal?: AbortSignal) =>
    api.put<{ tool: Tool }>(
      `${base}/tools/${encodeURIComponent(name)}`,
      { description },
      { ...bounded, signal },
    ),
  runs: (page = 1, status?: AgentRun['status'], signal?: AbortSignal) =>
    api.get<PageResult<AgentRun>>(`${base}/observability/runs`, {
      ...bounded,
      params: { page, page_size: 20, ...(status ? { status } : {}) },
      signal,
    }),
  run: (id: string, signal?: AbortSignal) =>
    api.get<AgentRun>(`${base}/observability/runs/${encodeURIComponent(id)}`, {
      ...bounded,
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
    AGENT_RAG_STALE: '文档或分组已变化，请刷新当前版本后再操作',
    AGENT_RAG_REQUEST_CONFLICT: '请求编号已用于其他内容，请重新确认本次操作',
    AGENT_RAG_NOT_READY: '文档尚未完成索引，请等待索引就绪后启用',
    AGENT_RAG_NOT_FOUND: '知识分组或文档不存在，请刷新后重试',
    AGENT_RAG_DEPENDENCY_UNAVAILABLE: '知识检索依赖暂不可用，请稍后重试',
    AGENT_CONFIGURATION_STALE: '配置已被修改，请查看当前版本后再保存',
    AGENT_CONFIGURATION_REQUEST_CONFLICT: '请求编号已用于其他操作，请重新确认本次内容',
    AGENT_CONFIGURATION_REFERENCE_INVALID: '绑定版本不存在、已归档或已停用，请检查依赖',
    AGENT_CONFIGURATION_TOOL_INVALID: '工具不存在或未启用，请检查工具选择',
    AGENT_CONFIGURATION_UNSUPPORTED: '模型或配置字段不受支持，请检查配置',
    AGENT_CONFIGURATION_TEST_EXPIRED: '测试 Agent 的到期时间必须在未来',
    AGENT_MODEL_ENDPOINT_DENIED: '模型地址不在部署允许范围内',
    AGENT_EVAL_DATASET_INVALID: '数据集格式不正确，请检查 JSONL 内容',
    AGENT_EVAL_NOT_FOUND: '评测数据集、运行或结果不存在',
    AGENT_EVAL_VERSION_CONFLICT: '内容已有变化，请加载最新版本后确认操作',
    AGENT_EVAL_REQUEST_CONFLICT: '请求编号已用于其他内容，请重新确认本次操作',
    AGENT_EVAL_HARD_FAILURE: '硬失败或执行未成功的样例不能标记为通过',
    AGENT_EVAL_UNAVAILABLE: '评测服务暂不可用或排队已满，请稍后重试',
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

function writableContent(kind: Resource, content: Content): Content {
  const value = { ...content }
  if (kind === 'skills' && 'execution_mode' in value) delete value.execution_mode
  return value
}
