import type { Content, Resource } from './types'

export function agentAdministrator(roles: readonly string[] = []): boolean {
  return roles.some((role) => ['admin', 'system_admin', 'agent_admin'].includes(role))
}
export function safeReturn(value: unknown): string | undefined {
  return typeof value === 'string' &&
    /^\/agent\/(agents|skills)\/(new|[a-z][a-z0-9_]{1,63}\/edit)$/.test(value)
    ? value
    : undefined
}
export function operationIds() {
  let fingerprint = '',
    id = ''
  return {
    for: (body: unknown) => {
      const next = JSON.stringify(body)
      if (fingerprint !== next) {
        fingerprint = next
        id = crypto.randomUUID()
      }
      return id
    },
    clear: () => {
      fingerprint = ''
      id = ''
    },
  }
}
export interface Draft {
  key: string
  content: Content
  expected: string | null
  baseline: string
  returnTo?: string
}
// 仅保存当前账号跳转创建依赖时的未提交输入，不作为配置持久化。
const drafts = new Map<string, Draft>()
export function keepDraft(owner: number, path: string, value: Draft) {
  drafts.set(`${owner}:${path}`, JSON.parse(JSON.stringify(value)) as Draft)
}
export function takeDraft(owner: number, path: string): Draft | undefined {
  const key = `${owner}:${path}`,
    value = drafts.get(key)
  drafts.delete(key)
  return value
}
export function clearAgentDrafts() {
  drafts.clear()
}
export function resourceFrom(value: unknown): Resource {
  if (value === 'agents' || value === 'skills' || value === 'prompts') return value
  throw new Error('Invalid resource route')
}
