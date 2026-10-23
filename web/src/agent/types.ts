export type Resource = 'agents' | 'prompts' | 'skills'
export type ConfigState = 'active' | 'archived' | 'disabled' | 'all'
export interface Page {
  page: number
  page_size: number
  total: number
}
export interface PageResult<T> {
  items: T[]
  page: Page
}
export interface Budget {
  max_run_seconds: number
  max_output_chars: number
  max_events: number
  max_tool_calls: number
  max_model_calls: number
  max_input_tokens: number
  max_output_tokens: number
}
export interface PromptContent {
  name?: string
  text: string
  variables: string[]
}
export interface SkillContent {
  name: string
  description?: string
  text?: string | null
  variables?: string[] | null
  prompt_id?: string | null
  execution_mode?: 'direct' | 'react'
  allowed_tools: string[]
  budget?: Budget | null
}
export interface AgentContent {
  name: string
  prompt_id: string
  skill_ids: string[]
  default_skill_id: string
  model_profile_id: string
  allowed_tools: string[]
  budget: Budget
  visibility: 'user' | 'admin'
  is_test: boolean
  test_expires_at: string | null
  knowledge_scope: string[]
}
export type Content = PromptContent | SkillContent | AgentContent
export interface Summary {
  id: string
  kind: 'agent' | 'prompt' | 'skill'
  key: string
  archived: boolean
  disabled: boolean
  created_at: string
  created_by: number
  archived_at: string | null
  archived_by: number | null
  name?: string
  visibility?: 'user' | 'admin'
  is_test?: boolean
  test_expires_at?: string | null
  model_profile_id?: string
}
export interface Configuration extends Summary {
  content: Content
}
export interface ModelOption {
  id: string
  key: string
  model: string
  temperature: number | null
  verbosity: string | null
  max_output_tokens: number
  context_window_tokens: number
  archived?: boolean
  disabled?: boolean
  available?: boolean
}
export interface Tool {
  name: string
  description: string
  enabled: boolean
  side_effect: 'read' | 'write'
  read_scope: string
  sensitivity: string
  allowed_roles: string[]
  input_schema: object
  output_schema: object
  timeout_seconds: number
  requires_confirmation: boolean
}
export interface AgentRun {
  run_id: string
  trace_id: string | null
  status: 'RUNNING' | 'COMPLETED' | 'FAILED' | 'CANCELLED' | 'INTERRUPTED'
  purpose: 'chat'
  started_at: string
  finished_at: string | null
  duration_ms: number | null
  error_code: string | null
  tool_step_count: number
  knowledge_query_count: number
  tempo_url: string | null
  langsmith_url: string | null
  langsmith_export_status: 'PENDING' | 'EXPORTED' | 'FAILED' | null
}
export interface AgentOverview {
  generated_at: string
  window_minutes: number
  availability: 'available' | 'stale' | 'unavailable'
  runs: Record<AgentRun['status'], number>
  total: number
}
export interface Binding {
  id: string
  key: string
  name: string
  archived: boolean
  disabled: boolean
}
export const resourceNames: Record<Resource, string> = {
  agents: 'Agent',
  prompts: 'Prompt',
  skills: 'Skill',
}
export const budgetFields = [
  ['max_run_seconds', '运行时长（秒）', 0.1, 300, 0.1],
  ['max_output_chars', '输出字符数', 1, 32000, 1],
  ['max_events', '事件数', 3, 10000, 1],
  ['max_tool_calls', '工具调用次数', 0, 32, 1],
  ['max_model_calls', '模型调用次数', 1, 16, 1],
  ['max_input_tokens', '输入 Token', 1, 128000, 1],
  ['max_output_tokens', '输出 Token', 1, 32000, 1],
] as const
export function defaultBudget(): Budget {
  return {
    max_run_seconds: 120,
    max_output_chars: 8000,
    max_events: 1000,
    max_tool_calls: 8,
    max_model_calls: 8,
    max_input_tokens: 32000,
    max_output_tokens: 8000,
  }
}
export function emptyContent(resource: Resource): Content {
  if (resource === 'prompts') return { name: '', text: '', variables: [] }
  if (resource === 'skills')
    return {
      name: '',
      description: '',
      text: '',
      allowed_tools: [],
    }
  return {
    name: '',
    prompt_id: '',
    skill_ids: [],
    default_skill_id: '',
    model_profile_id: '',
    allowed_tools: [],
    budget: defaultBudget(),
    visibility: 'user',
    is_test: false,
    test_expires_at: null,
    knowledge_scope: [],
  }
}
