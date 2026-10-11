import { describe, expect, it } from 'vitest'
import { emptyContent, type AgentContent } from './types'
import { validateConfiguration } from './validation'

describe('配置边界', () => {
  it('模板仅允许声明变量，支持字面括号，拒绝表达式和未声明变量', () => {
    expect(
      validateConfiguration('prompts', 'test_prompt', {
        text: '{{literal}} {language}',
        variables: ['language'],
      }),
    ).toBeNull()
    for (const text of [
      '{language.name}',
      '{language!r}',
      '{language:20}',
      '{unknown}',
      '{language',
      '}',
    ])
      expect(
        validateConfiguration('prompts', 'test_prompt', { text, variables: ['language'] }),
      ).not.toBeNull()
    expect(
      validateConfiguration('prompts', 'test_prompt', { text: '{language}', variables: [] }),
    ).not.toBeNull()
    expect(
      validateConfiguration('prompts', 'test_prompt', {
        text: '中文'.repeat(16001),
        variables: [],
      }),
    ).not.toBeNull()
    expect(
      validateConfiguration('prompts', 'Invalid', { text: '正文', variables: [] }),
    ).not.toBeNull()
  })
  it('测试 Agent 强制管理员可见和未来时间，预算和默认 Skill 均有效', () => {
    const content = emptyContent('agents') as AgentContent
    Object.assign(content, {
      name: 'test',
      prompt_id: 'p',
      model_profile_id: 'm',
      skill_ids: ['s'],
      default_skill_id: 's',
    })
    expect(validateConfiguration('agents', 'test_agent', content)).toBeNull()
    content.is_test = true
    expect(validateConfiguration('agents', 'test_agent', content)).not.toBeNull()
    content.visibility = 'admin'
    content.test_expires_at = new Date(Date.now() + 3600000).toISOString()
    expect(validateConfiguration('agents', 'test_agent', content)).toBeNull()
    content.skill_ids = ['s', 's']
    expect(validateConfiguration('agents', 'test_agent', content)).not.toBeNull()
    content.skill_ids = ['s']
    content.budget.max_events = 2
    expect(validateConfiguration('agents', 'test_agent', content)).not.toBeNull()
    content.budget.max_events = 100
    content.default_skill_id = 'other'
    expect(validateConfiguration('agents', 'test_agent', content)).not.toBeNull()
  })
})
