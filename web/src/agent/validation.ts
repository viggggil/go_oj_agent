import {
  budgetFields,
  type AgentContent,
  type Content,
  type PromptContent,
  type Resource,
  type SkillContent,
} from './types'

export function validateConfiguration(kind: Resource, key: string, body: Content): string | null {
  if (key && !/^[a-z][a-z0-9_]{1,63}$/.test(key))
    return '标识须为 2–64 位小写字母、数字或下划线，以字母开头'
  const named = body as { name?: string }
  if (!named.name?.trim() || [...named.name].length > 128) return '名称不能为空，最多 128 个字符'
  if (kind === 'prompts') {
    const prompt = body as PromptContent
    if (!prompt.text.trim() || [...prompt.text].length > 32000)
      return 'Prompt 正文不能为空，最多 32000 个字符'
    // 只接受声明变量与 {{、}} 转义，拒绝格式、转换和属性表达式。
    const variables = new Set<string>()
    for (let index = 0; index < prompt.text.length; index++) {
      const char = prompt.text[index]
      if (char !== '{' && char !== '}') continue
      if (prompt.text[index + 1] === char) {
        index++
        continue
      }
      if (char === '}') return 'Prompt 花括号不匹配'
      const end = prompt.text.indexOf('}', index + 1)
      if (end < 0) return 'Prompt 花括号不匹配'
      const name = prompt.text.slice(index + 1, end)
      if (!['language', 'problem_id', 'submission_id'].includes(name))
        return 'Prompt 包含不支持的变量或表达式'
      variables.add(name)
      index = end
    }
    if (
      variables.size !== prompt.variables.length ||
      prompt.variables.some((name) => !variables.has(name))
    )
      return '正文中的变量必须与勾选的变量一致'
    return null
  }
  const item = body as SkillContent | AgentContent
  if (!item.name.trim() || [...item.name].length > 128) return '名称不能为空，最多 128 个字符'
  if (
    new Set(item.allowed_tools).size !== item.allowed_tools.length ||
    item.allowed_tools.length > 32
  )
    return '工具白名单不能重复，最多 32 个'
  if (kind === 'skills') {
    const skill = body as SkillContent
    if (!skill.text?.trim() || [...skill.text].length > 32000)
      return '任务说明不能为空，最多 32000 个字符'
    if ([...(skill.description || '')].length > 512) return '描述最多 512 个字符'
    if (skill.variables != null) {
      const error = validateConfiguration('prompts', '', {
        name: skill.name,
        text: skill.text,
        variables: skill.variables,
      })
      if (error) return error
    }
    return skill.execution_mode === 'direct' ? null : '当前模型仅支持 direct'
  }
  const agent = body as AgentContent
  if (!agent.prompt_id) return '请选择 Prompt 版本'
  for (const [field, label, min, max, step] of budgetFields) {
    const value = agent.budget[field]
    if (
      !Number.isFinite(value) ||
      value < min ||
      value > max ||
      (step === 1 && !Number.isInteger(value))
    )
      return `${label}超出允许范围`
  }
  if (!agent.model_profile_id) return '请选择模型版本'
  if (new Set(agent.skill_ids).size !== agent.skill_ids.length) return 'Skill 版本不能重复'
  if (!agent.is_test && agent.test_expires_at !== null) return '普通 Agent 不能设置测试到期时间'
  if (
    agent.skill_ids.length < 1 ||
    agent.skill_ids.length > 16 ||
    !agent.skill_ids.includes(agent.default_skill_id)
  )
    return '绑定 1–16 个 Skill，并选择其中一个作为默认 Skill'
  if (
    agent.is_test &&
    (agent.visibility !== 'admin' ||
      !agent.test_expires_at ||
      new Date(agent.test_expires_at).getTime() <= Date.now() ||
      !Number.isFinite(new Date(agent.test_expires_at).getTime()))
  )
    return '测试 Agent 必须管理员可见，并设置未来的到期时间'
  return null
}
