import { describe, expect, it } from 'vitest'
import { reactive } from 'vue'
import {
  agentAdministrator,
  clearAgentDrafts,
  keepDraft,
  operationIds,
  safeReturn,
  takeDraft,
} from './state'

describe('管理身份与未提交状态', () => {
  it('仅接受三类管理员角色，安全返回路径拒绝外部/聊天/无效资源', () => {
    for (const role of ['admin', 'system_admin', 'agent_admin'])
      expect(agentAdministrator([role])).toBe(true)
    expect(agentAdministrator(['user', 'root'])).toBe(false)
    expect(safeReturn('/agent/agents/new')).toBe('/agent/agents/new')
    expect(safeReturn('/agent/skills/test_skill/edit')).toBe('/agent/skills/test_skill/edit')
    expect(safeReturn('/agent/skills/new/edit')).toBe('/agent/skills/new/edit')
    for (const value of [
      '//evil.test',
      '/agent/chat',
      '/agent/prompts/new',
      '/agent/agents/../new',
      '/agent/agents/test_agent?token=x',
    ])
      expect(safeReturn(value)).toBeUndefined()
  })
  it('同一操作重试保留 UUID，正文/基准变化必须生成新编号', () => {
    const ids = operationIds(),
      body = { expected: 'v1', text: '旧提示' }
    const id = ids.for(body)
    expect(id).toMatch(/^[0-9a-f-]{36}$/)
    expect(ids.for({ ...body })).toBe(id)
    expect(ids.for({ ...body, text: '新提示' })).not.toBe(id)
    expect(ids.for({ ...body, expected: 'v2' })).not.toBe(id)
    ids.clear()
    expect(ids.for(body)).not.toBe(id)
  })
  it('Vue 表单可复制为内存草稿，账号隔离，取出即删除，退出清除', () => {
    const value = reactive({
      key: 'test_skill',
      content: { text: '保持输入', variables: [] },
      expected: null,
      baseline: 'old',
      returnTo: '/agent/agents/new',
    })
    keepDraft(7, '/agent/skills/new', value)
    value.content.text = '后来的更改'
    expect(takeDraft(8, '/agent/skills/new')).toBeUndefined()
    expect(takeDraft(7, '/agent/skills/new')?.content).toEqual({ text: '保持输入', variables: [] })
    expect(takeDraft(7, '/agent/skills/new')).toBeUndefined()
    keepDraft(7, '/agent/skills/new', value)
    clearAgentDrafts()
    expect(takeDraft(7, '/agent/skills/new')).toBeUndefined()
  })
})
