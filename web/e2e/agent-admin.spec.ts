import { test, expect, type Page } from '@playwright/test'
import { randomUUID } from 'node:crypto'
import { defaultBudget } from '../src/agent/types'

async function createDependency(page: Page, kind: 'Prompt' | 'Skill') {
  await page.getByRole('button', { name: '新建 ' + kind, exact: true }).click()
  // 导航包含异步权限检查；同名输入框可能暂时仍属于来源表单。
  await expect(page.getByRole('heading', { name: '新建' + kind, exact: true })).toBeVisible()
}

interface Config {
  id: string
  key: string
  kind: string
  content: any
  archived: boolean
  disabled: boolean
  created_at: string
  created_by: number
  archived_at: string | null
  archived_by: number | null
}
async function mockManagement(page: Page, roles = ['admin']) {
  const current = new Map<string, Config>(),
    versions = new Map<string, Config>(),
    writes: string[] = []
  const knowledgeGroup = {
    id: randomUUID(),
    name: '题解',
    created_by: 7,
    created_at: new Date().toISOString(),
  }
  const knowledgeGroups = [knowledgeGroup]
  const model = {
    id: randomUUID(),
    key: 'su8_flash',
    model: 'deepseek-v4-flash',
    temperature: null,
    verbosity: null,
    max_output_tokens: 32000,
    context_window_tokens: 128000,
  }
  let failNext = 0
  let failProfile = false
  let failDetail = false
  const completed = new Map<string, { fingerprint: string; configuration: Config }>()
  const create = (kind: string, key: string, content: any): Config => {
    const previous = current.get(kind + '/' + key)
    if (previous) {
      previous.archived = true
      previous.archived_at = new Date().toISOString()
      previous.archived_by = 7
    }
    const cfg = {
      id: randomUUID(),
      key,
      kind: kind.slice(0, -1),
      content: kind === 'prompts' ? { name: key, ...content } : content,
      archived: false,
      disabled: previous?.disabled || false,
      created_at: new Date().toISOString(),
      created_by: 7,
      archived_at: null,
      archived_by: null,
    }
    current.set(kind + '/' + key, cfg)
    versions.set(cfg.id, cfg)
    return cfg
  }
  await page.addInitScript(() => {
    localStorage.setItem('access_token', 'mock-access')
    localStorage.setItem('refresh_token', 'mock-refresh')
  })
  await page.route('**/api/v1/**', async (route) => {
    const url = new URL(route.request().url()),
      method = route.request().method(),
      parts = url.pathname.split('/').filter(Boolean)
    const respond = (data: unknown, status = 200) => route.fulfill({ status, json: data })
    if (url.pathname === '/api/v1/users/me' && failProfile) return respond({}, 503)
    if (url.pathname === '/api/v1/users/me')
      return respond({
        user: { id: 7, username: '管理员', email: 'admin@example.test', status: 'active', roles },
      })
    if (!url.pathname.startsWith('/api/v1/admin/agent/')) return respond({}, 404)
    if (!roles.some((role) => ['admin', 'system_admin', 'agent_admin'].includes(role)))
      return respond({ reason: 'AGENT_FORBIDDEN' }, 403)
    const kind = parts[4],
      key = parts[5],
      action = parts[6]
    const paged = (values: unknown[]) => {
      const number = Number(url.searchParams.get('page') || 1),
        size = Number(url.searchParams.get('page_size') || 20)
      return respond({
        items: values.slice((number - 1) * size, number * size),
        page: { page: number, page_size: size, total: values.length },
      })
    }
    if (kind === 'model-options')
      return key
        ? respond({ model_option: { ...model, archived: false, disabled: false, available: true } })
        : paged([model])
    if (kind === 'tools') return paged([])
    if (kind === 'rag') {
      if (parts[5] === 'knowledge-bases' && parts[6]) {
        const group = knowledgeGroups.find((item) => item.id === parts[6])
        return group ? respond({ knowledge_base: group }) : respond({}, 404)
      }
      return paged(knowledgeGroups)
    }
    if (method !== 'GET') {
      writes.push(route.request().headers()['x-request-id'])
      if (failNext) {
        const status = failNext
        failNext = 0
        return respond(
          { reason: status === 409 ? 'AGENT_CONFIGURATION_STALE' : 'AGENT_UNAVAILABLE' },
          status,
        )
      }
      const body = route.request().postDataJSON()
      const requestId = route.request().headers()['x-request-id']
      const fingerprint = JSON.stringify({ method, path: url.pathname, body })
      const replay = completed.get(requestId)
      if (replay)
        return replay.fingerprint === fingerprint
          ? respond({ configuration: replay.configuration })
          : respond({ reason: 'AGENT_CONFIGURATION_REQUEST_CONFLICT' }, 409)
      const saved = (configuration: Config) => {
        completed.set(requestId, { fingerprint, configuration })
        return respond({ configuration })
      }
      if (method === 'POST' && !key) {
        const generatedKey = body.key || kind.slice(0, -1) + '_' + requestId.replaceAll('-', '')
        if (current.has(kind + '/' + generatedKey))
          return respond({ reason: 'AGENT_CONFIGURATION_CONFLICT' }, 409)
        return saved(create(kind, generatedKey, body.content))
      }
      const value = current.get(kind + '/' + key)
      if (!value) return respond({}, 404)
      if (body.expected_id !== value.id)
        return respond({ reason: 'AGENT_CONFIGURATION_STALE' }, 409)
      if (method === 'PUT') return saved(create(kind, key, body.content))
      if (action === 'restore')
        return saved(create(kind, key, versions.get(body.source_id)!.content))
      if (action === 'archive') value.archived = true
      if (action === 'disable') value.disabled = true
      if (action === 'enable') value.disabled = false
      return saved(value)
    }
    if (key === 'versions') {
      const value = versions.get(action)
      return value && value.kind + 's' === kind
        ? respond({ configuration: value })
        : respond({}, 404)
    }
    if (!key) {
      const state = url.searchParams.get('state') || 'active',
        search = url.searchParams.get('search') || ''
      return paged(
        [...current.values()].filter(
          (value) =>
            value.kind + 's' === kind &&
            (value.key.includes(search) || value.content.name?.includes(search)) &&
            (state === 'all' ||
              (state === 'archived' && value.archived) ||
              (state === 'disabled' && value.disabled) ||
              (state === 'active' && !value.archived)),
        ),
      )
    }
    if (action === 'versions') {
      if (parts[7]) return respond({ configuration: versions.get(parts[7]) })
      return paged(
        [...versions.values()]
          .filter((value) => value.kind + 's' === kind && value.key === key)
          .reverse(),
      )
    }
    const value = current.get(kind + '/' + key)
    if (failDetail) {
      failDetail = false
      return respond({ reason: 'AGENT_UNAVAILABLE' }, 503)
    }
    return value ? respond({ configuration: value }) : respond({}, 404)
  })
  page.on('dialog', (dialog) => dialog.accept())
  return {
    current,
    named: (name: string) => [...current.values()].find((value) => value.content.name === name)!,
    versions,
    writes,
    model,
    knowledgeGroup,
    knowledgeGroups,
    create,
    fail: (status: number) => {
      failNext = status
    },
    profileFailure: (value: boolean) => {
      failProfile = value
    },
    failReadBack: () => {
      failDetail = true
    },
    roles: (value: string[]) => {
      roles = value
    },
  }
}

test('管理员通过嵌套创建保留表单，保存 test Agent，并查看历史/归档/恢复', async ({ page }) => {
  const state = await mockManagement(page)
  await page.goto('/agent/agents/new')
  await page.getByLabel('名称', { exact: true }).fill('算法测试助手')
  await createDependency(page, 'Prompt')
  await page.getByLabel('名称', { exact: true }).fill('基础规则')
  await page.getByLabel('正文').fill('用中文解释算法')
  await page.getByRole('button', { name: '创建', exact: true }).click()
  await expect(page.getByLabel('名称', { exact: true })).toHaveValue('算法测试助手')
  await createDependency(page, 'Skill')
  await page.getByLabel('名称', { exact: true }).fill('算法讲解')
  await page.getByLabel('任务说明').fill('提供时间复杂度分析')
  await expect(page.getByRole('button', { name: '新建 Prompt', exact: true })).toHaveCount(0)
  await expect(page.getByLabel('运行时长（秒）')).toHaveCount(0)
  await page.getByRole('button', { name: '创建', exact: true }).click()
  await expect(page.getByLabel('名称', { exact: true })).toHaveValue('算法测试助手')
  await page.getByLabel('选择模型版本').selectOption(state.model.id)
  await page.getByRole('checkbox', { name: '题解', exact: true }).check()
  await page.getByLabel('测试 Agent', { exact: true }).check()
  await page.getByLabel('到期时间（本地时区）').fill('2099-10-10T12:00')
  await page.getByRole('button', { name: '创建', exact: true }).click()
  await expect(page).toHaveURL(/\/agent\/agents\/agent_[a-f0-9]+\/edit$/)
  const first = state.named('算法测试助手')
  expect(first.content.prompt_id).toBe(state.named('基础规则').id)
  expect(first.content.skill_ids).toEqual([state.named('算法讲解').id])
  expect(first.content.visibility).toBe('admin')
  expect(first.content.knowledge_scope).toEqual([state.knowledgeGroup.id])
  await page.getByLabel('名称', { exact: true }).fill('新测试助手')
  await page.getByRole('button', { name: '保存新版本' }).click()
  await expect(page.getByRole('status')).toContainText('已保存版本')
  expect(state.current.get('agents/' + first.key)!.id).not.toBe(first.id)
  await page.getByRole('button', { name: '查看 ' + first.id }).click()
  await expect(page.getByRole('dialog')).toContainText('算法测试助手')
  await page.getByRole('button', { name: '关闭版本详情' }).click()
  await page.getByRole('button', { name: '归档', exact: true }).click()
  await expect(page.getByRole('button', { name: '保存新版本' })).toBeDisabled()
  await page.getByRole('button', { name: '从此版本恢复' }).last().click()
  await expect(page.getByLabel('名称', { exact: true })).toHaveValue('算法测试助手')
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.screenshot({ path: 'test-results/agent-desktop.png', fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  await expect
    .poll(() => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth))
    .toBe(true)
  await page.getByRole('button', { name: '切换菜单' }).click()
  await expect(page.getByRole('navigation', { name: 'Agent 管理导航' })).toBeVisible()
  await page.getByRole('button', { name: '关闭菜单' }).click()
  await page.screenshot({ path: 'test-results/agent-mobile.png', fullPage: true })
})

test('恢复 Agent 历史配置后补取跨页分组名称，并支持分页选择', async ({ page }) => {
  const state = await mockManagement(page)
  for (let number = 1; number <= 20; number++)
    state.knowledgeGroups.push({
      ...state.knowledgeGroup,
      id: randomUUID(),
      name: '讲义 ' + number,
    })
  const lastGroup = state.knowledgeGroups.at(-1)!
  const prompt = state.create('prompts', 'group_prompt', { text: '算法助手', variables: [] })
  const skill = state.create('skills', 'group_skill', {
    name: '算法讲解',
    text: '解释算法思路和复杂度',
    execution_mode: 'direct',
    allowed_tools: [],
    budget: defaultBudget(),
  })
  const content = {
    name: '分组恢复助手',
    prompt_id: prompt.id,
    skill_ids: [skill.id],
    default_skill_id: skill.id,
    model_profile_id: state.model.id,
    allowed_tools: [],
    budget: defaultBudget(),
    visibility: 'user',
    is_test: false,
    test_expires_at: null,
    knowledge_scope: [lastGroup.id],
  }
  state.create('agents', 'group_agent', content)
  state.create('agents', 'group_agent', { ...content, knowledge_scope: [] })
  await page.goto('/agent/agents/group_agent/edit')
  await expect(page.getByRole('checkbox', { name: '题解', exact: true })).toBeVisible()
  await expect(page.getByRole('checkbox', { name: lastGroup.name, exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: '从此版本恢复' }).last().click()
  await expect(page.getByRole('status')).toContainText('已保存版本')
  await expect(page.locator('.selected-bindings')).toContainText(lastGroup.name)
  await expect(page.locator('.selected-bindings')).not.toContainText('分组信息未加载')
  await page.getByRole('button', { name: '下一页分组' }).click()
  const lastCheckbox = page.getByRole('checkbox', { name: lastGroup.name, exact: true })
  await expect(lastCheckbox).toBeChecked()
  await lastCheckbox.uncheck()
  await page.getByRole('button', { name: '上一页分组' }).click()
  await page.getByRole('checkbox', { name: '题解', exact: true }).check()
  await page.getByRole('button', { name: '保存新版本' }).click()
  await expect(page.getByRole('status')).toContainText('已保存版本')
  await expect
    .poll(() => state.current.get('agents/group_agent')!.content.knowledge_scope)
    .toEqual([state.knowledgeGroup.id])
})

test('已停用工具仍可从历史绑定中取消后保存', async ({ page }) => {
  const state = await mockManagement(page)
  const prompt = state.create('prompts', 'tool_prompt', { text: '提示', variables: [] })
  const disabledTool = {
    name: 'old_tool',
    description: '已下线工具',
    enabled: false,
    side_effect: 'read',
    read_scope: 'public',
    sensitivity: 'low',
    allowed_roles: [],
    input_schema: {},
    output_schema: {},
  }
  await page.route('**/api/v1/admin/agent/tools*', async (route) =>
    route.fulfill({ json: { items: [disabledTool], page: { page: 1, page_size: 100, total: 1 } } }),
  )
  state.create('skills', 'tool_skill', {
    name: '旧 Skill',
    prompt_id: prompt.id,
    execution_mode: 'direct',
    allowed_tools: ['old_tool'],
    budget: defaultBudget(),
  })
  await page.goto('/agent/skills/tool_skill/edit')
  await page.getByText('高级设置', { exact: true }).click()
  const checkbox = page.getByRole('checkbox', { name: /old_tool/ })
  await expect(checkbox).toBeChecked()
  await expect(checkbox).toBeEnabled()
  await checkbox.uncheck()
  await expect(checkbox).toBeDisabled()
  await page.getByRole('button', { name: '保存新版本' }).click()
  await expect(page.getByRole('status')).toContainText('已保存版本')
  expect(state.current.get('skills/tool_skill')!.content.allowed_tools).toEqual([])
  const skill = state.current.get('skills/tool_skill')!
  state.create('agents', 'tool_agent', {
    name: '旧 Agent',
    prompt_id: prompt.id,
    skill_ids: [skill.id],
    default_skill_id: skill.id,
    model_profile_id: state.model.id,
    allowed_tools: ['old_tool'],
    budget: defaultBudget(),
    visibility: 'user',
    is_test: false,
    test_expires_at: null,
    knowledge_scope: [],
  })
  await page.goto('/agent/agents/tool_agent/edit')
  await page.getByText('高级设置', { exact: true }).click()
  await expect(checkbox).toBeChecked()
  await expect(checkbox).toBeEnabled()
  await checkbox.uncheck()
  await expect(checkbox).toBeDisabled()
  await page.getByRole('button', { name: '保存新版本' }).click()
  await expect(page.getByRole('status')).toContainText('已保存版本')
  expect(state.current.get('agents/tool_agent')!.content.allowed_tools).toEqual([])
})

test('网络重试保留 UUID，冲突保留输入，明确比较后才能覆盖', async ({ page }) => {
  const state = await mockManagement(page)
  const original = state.create('prompts', 'test_prompt', { text: '旧提示', variables: [] })
  await page.goto('/agent/prompts/test_prompt')
  await page.getByLabel('正文').fill('管理员的新输入')
  state.fail(503)
  await page.getByRole('button', { name: '保存新版本' }).click()
  await expect(page.getByRole('alert')).toContainText('暂不可用')
  await page.getByRole('button', { name: '保存新版本' }).click()
  await expect(page.getByRole('status')).toContainText('已保存版本')
  expect(state.writes[0]).toBe(state.writes[1])
  await page.getByLabel('正文').fill('保留输入')
  const server = state.create('prompts', 'test_prompt', {
    text: '另一个管理员的版本',
    variables: [],
  })
  await page.getByRole('button', { name: '保存新版本' }).click()
  await expect(page.getByRole('alert')).toContainText('配置已被修改')
  await expect(page.getByLabel('正文')).toHaveValue('保留输入')
  await page.getByRole('button', { name: '查看当前版本' }).click()
  await expect(page.getByRole('button', { name: '保存新版本' })).toBeDisabled()
  await expect(page.locator('.agent-conflict')).toContainText(server.id)
  await page.getByRole('button', { name: '保留输入，基于当前版本重试' }).click()
  await page.getByRole('button', { name: '保存新版本' }).click()
  await expect(page.getByRole('status')).toContainText('已保存版本')
  expect(state.current.get('prompts/test_prompt')!.content.text).toBe('保留输入')
  expect(state.versions.get(original.id)!.archived).toBe(true)
  expect(state.writes[state.writes.length - 1]).not.toBe(state.writes[state.writes.length - 2])
})

test('普通用户管理 URL 转到对话，不读取管理正文；失败详情不允许写入', async ({ page }) => {
  const requests: string[] = []
  page.on('request', (req) => requests.push(req.url()))
  await mockManagement(page, ['user'])
  await page.goto('/agent/prompts/test_prompt')
  await expect(page).toHaveURL(/\/agent\/chat$/)
  expect(requests.some((url) => url.includes('/api/v1/admin/agent/'))).toBe(false)
  await expect(page.getByRole('link', { name: 'Prompt', exact: true })).toHaveCount(0)
})

test('管理员详情 404 可重试，正文作为纯文本显示', async ({ page }) => {
  const state = await mockManagement(page, ['agent_admin'])
  await page.goto('/agent/prompts/missing_prompt')
  await expect(page.getByRole('alert')).toContainText('配置不存在')
  await expect(page.getByRole('button', { name: '保存新版本' })).toHaveCount(0)
  state.create('prompts', 'missing_prompt', {
    text: '<img src=x onerror="window.XSS=true">',
    variables: [],
  })
  await page.getByRole('button', { name: '重试加载' }).click()
  await expect(page.getByLabel('正文')).toHaveValue('<img src=x onerror="window.XSS=true">')
  expect(await page.evaluate(() => (window as any).XSS)).toBeUndefined()
})

test('编辑路由复用仍确认离开，取消后保留输入；创建依赖可返回原表单', async ({ page }) => {
  await mockManagement(page)
  await page.goto('/agent/agents/new')
  await page.getByLabel('名称', { exact: true }).fill('保留 Agent 名称')
  await createDependency(page, 'Prompt')
  await page.getByLabel('正文').fill('未保存 Prompt')
  page.removeAllListeners('dialog')
  await Promise.all([
    page.waitForEvent('dialog').then((dialog) => dialog.dismiss()),
    page.getByRole('button', { name: '返回原表单' }).click(),
  ])
  await expect(page).toHaveURL(/\/agent\/prompts\/new\?return_to=/)
  await expect(page.getByLabel('正文')).toHaveValue('未保存 Prompt')
  await Promise.all([
    page.waitForEvent('dialog').then((dialog) => dialog.accept()),
    page.getByRole('button', { name: '返回原表单' }).click(),
  ])
  await expect(page.getByLabel('名称', { exact: true })).toHaveValue('保留 Agent 名称')
})

test('资料临时失败隐藏管理页，重试恢复输入与嵌套草稿；角色丢失清除管理内容', async ({ page }) => {
  const state = await mockManagement(page)
  await page.goto('/agent/agents/new')
  await page.getByLabel('名称', { exact: true }).fill('网络失败保留 Agent')
  await createDependency(page, 'Prompt')
  await page.getByLabel('名称', { exact: true }).fill('保留 Prompt 输入')
  state.profileFailure(true)
  await page.getByRole('button', { name: '返回原表单', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('无法确认账号权限')
  await expect(page.getByLabel('名称', { exact: true })).toBeHidden()
  state.profileFailure(false)
  await page.getByRole('button', { name: '重试', exact: true }).click()
  await expect(page.getByLabel('名称', { exact: true })).toHaveValue('保留 Prompt 输入')
  await page.getByRole('button', { name: '返回原表单' }).click()
  await expect(page.getByLabel('名称', { exact: true })).toHaveValue('网络失败保留 Agent')
  state.roles(['user'])
  await page.getByRole('link', { name: 'Prompt', exact: true }).click()
  await expect(page).toHaveURL(/\/agent\/chat$/)
  await expect(page.getByLabel('名称', { exact: true })).toHaveCount(0)
  await expect(page.getByRole('link', { name: 'Prompt', exact: true })).toHaveCount(0)
})

test('合法标识 new 可创建、打开编辑和再次保存', async ({ page }) => {
  const state = await mockManagement(page)
  await page.goto('/agent/prompts/new')
  state.create('prompts', 'new', { name: '旧 new 标识', text: '合法 new 标识', variables: [] })
  await page.goto('/agent/prompts/new/edit')
  await page.getByLabel('正文').fill('合法 new 标识')
  await page.getByRole('button', { name: '保存新版本', exact: true }).click()
  await expect(page).toHaveURL(/\/agent\/prompts\/new\/edit$/)
  await page.getByRole('link', { name: '返回列表' }).click()
  await page.getByRole('link', { name: '编辑', exact: true }).click()
  await expect(page.getByLabel('正文')).toHaveValue('合法 new 标识')
  await page.getByLabel('正文').fill('新版本')
  await page.getByRole('button', { name: '保存新版本' }).click()
  await expect(page.getByRole('status')).toContainText('已保存版本')
  expect(state.current.get('prompts/new')!.content.text).toBe('新版本')
})

test('已有 Agent 创建 Skill 后消费返回参数，保存刷新不重复绑定', async ({ page }) => {
  const state = await mockManagement(page)
  const prompt = state.create('prompts', 'base_prompt', { text: '基础提示', variables: [] })
  const skill = state.create('skills', 'base_skill', {
    name: '原 Skill',
    prompt_id: prompt.id,
    execution_mode: 'direct',
    allowed_tools: [],
    budget: defaultBudget(),
  })
  state.create('agents', 'existing_agent', {
    name: '已有 Agent',
    prompt_id: prompt.id,
    skill_ids: [skill.id],
    default_skill_id: skill.id,
    model_profile_id: state.model.id,
    allowed_tools: [],
    budget: defaultBudget(),
    visibility: 'user',
    is_test: false,
    test_expires_at: null,
    knowledge_scope: [],
  })
  await page.goto('/agent/agents/existing_agent/edit')
  await createDependency(page, 'Skill')
  await page.getByLabel('名称', { exact: true }).fill('新增 Skill')
  await page.getByLabel('任务说明').fill('直接说明任务')
  await page.getByRole('button', { name: '创建', exact: true }).click()
  await expect(page).toHaveURL(/\/agent\/agents\/existing_agent\/edit$/)
  await page.getByRole('button', { name: '保存新版本' }).click()
  await expect(page.getByRole('status')).toContainText('已保存版本')
  await page.reload()
  await expect(
    page.getByText('绑定 Skill', { exact: true }).locator('..').locator('.selected-binding'),
  ).toHaveCount(2)
  await page.getByLabel('名称', { exact: true }).fill('刷新后继续修改')
  await page.getByRole('button', { name: '保存新版本' }).click()
  await expect(page.getByRole('status')).toContainText('已保存版本')
  expect(state.current.get('agents/existing_agent')!.content.skill_ids).toEqual([
    skill.id,
    state.named('新增 Skill').id,
  ])
})

test('保存后跳转遇到资料故障，恢复后仍有未保存离开保护', async ({ page }) => {
  await mockManagement(page).then(async (state) => {
    await page.goto('/agent/prompts/new')
    await page.getByLabel('名称', { exact: true }).fill('cancelled_navigation')
    await page.getByLabel('正文').fill('已写入正文')
    state.profileFailure(true)
    await page.getByRole('button', { name: '创建', exact: true }).click()
    await expect(page.getByRole('alert')).toContainText('无法确认账号权限')
    state.profileFailure(false)
    await page.getByRole('button', { name: '重试', exact: true }).click()
    await page.getByLabel('正文').fill('仍需保护的新输入')
    page.removeAllListeners('dialog')
    await Promise.all([
      page.waitForEvent('dialog').then((dialog) => dialog.dismiss()),
      page.getByRole('link', { name: '返回列表' }).click(),
    ])
    await expect(page).toHaveURL(/\/agent\/prompts\/new$/)
    await expect(page.getByLabel('正文')).toHaveValue('仍需保护的新输入')
  })
})

test('同名资源可创建，后端标识独立，创建失败重试保留请求 UUID', async ({ page }) => {
  const state = await mockManagement(page)
  const original = state.create('prompts', 'existing_name', {
    name: '同名提示',
    text: '旧正文',
    variables: [],
  })
  await page.goto('/agent/prompts/new')
  await expect(page.getByLabel('稳定标识')).toHaveCount(0)
  await page.getByLabel('名称', { exact: true }).fill('同名提示')
  await page.getByLabel('正文').fill('独立正文')
  state.fail(503)
  await page.getByRole('button', { name: '创建', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('暂不可用')
  await page.getByRole('button', { name: '创建', exact: true }).click()
  await expect(page).toHaveURL(/\/agent\/prompts\/prompt_[a-f0-9]+\/edit$/)
  expect(state.writes[0]).toBe(state.writes[1])
  expect(state.current.size).toBe(2)
  expect(state.current.get('prompts/' + original.key)!.id).toBe(original.id)
})

test('写入成功但读回失败保留输入和离开保护，同编号重试不新增版本', async ({ page }) => {
  const state = await mockManagement(page)
  state.create('prompts', 'readback_prompt', { text: '原始正文', variables: [] })
  await page.goto('/agent/prompts/readback_prompt/edit')
  await page.getByLabel('正文').fill('写入后保留输入')
  state.failReadBack()
  await page.getByRole('button', { name: '保存新版本' }).click()
  await expect(page.getByRole('alert')).toContainText('暂不可用')
  const savedId = state.current.get('prompts/readback_prompt')!.id
  page.removeAllListeners('dialog')
  await Promise.all([
    page.waitForEvent('dialog').then((dialog) => dialog.dismiss()),
    page.getByRole('link', { name: '返回列表' }).click(),
  ])
  await expect(page).toHaveURL(/\/readback_prompt\/edit$/)
  await expect(page.getByLabel('正文')).toHaveValue('写入后保留输入')
  await page.getByRole('button', { name: '保存新版本' }).click()
  await expect(page.getByRole('status')).toContainText(savedId)
  expect(state.writes[1]).toBe(state.writes[0])
  expect(state.current.get('prompts/readback_prompt')!.id).toBe(savedId)
  expect(state.versions.size).toBe(2)
})

for (const replace of [true, false]) {
  test(`嵌套返回同名 Skill ${replace ? '确认后替换绑定与默认版本' : '取消后保留原绑定'}`, async ({
    page,
  }) => {
    const state = await mockManagement(page)
    const prompt = state.create('prompts', 'nested_prompt', { text: '提示', variables: [] })
    const skill = state.create('skills', 'same_skill', {
      name: '原 Skill',
      prompt_id: prompt.id,
      execution_mode: 'direct',
      allowed_tools: [],
      budget: defaultBudget(),
    })
    state.create('agents', 'nested_agent', {
      name: '嵌套 Agent',
      prompt_id: prompt.id,
      skill_ids: [skill.id],
      default_skill_id: skill.id,
      model_profile_id: state.model.id,
      allowed_tools: [],
      budget: defaultBudget(),
      visibility: 'user',
      is_test: false,
      test_expires_at: null,
      knowledge_scope: [],
    })
    await page.goto('/agent/agents/nested_agent/edit')
    await createDependency(page, 'Skill')
    await page.goto(
      '/agent/skills/same_skill/edit?return_to=%2Fagent%2Fagents%2Fnested_agent%2Fedit',
    )
    await page.getByLabel('名称', { exact: true }).fill('新 Skill')
    await page.getByLabel('任务说明').fill('直接说明任务')
    page.removeAllListeners('dialog')
    page.on('dialog', (dialog) => (replace ? dialog.accept() : dialog.dismiss()))
    await page.getByRole('button', { name: '保存新版本' }).click()
    await expect(page).toHaveURL(/\/agent\/agents\/nested_agent\/edit$/)
    const selected = replace ? state.current.get('skills/same_skill')!.id : skill.id
    await expect(page.getByLabel('默认 Skill')).toHaveValue(selected)
    await expect(
      page.getByText('绑定 Skill', { exact: true }).locator('..').locator('.selected-binding'),
    ).toHaveCount(1)
    await page.getByRole('button', { name: '保存新版本' }).click()
    await expect(page.getByRole('status')).toContainText('已保存版本')
    expect(state.current.get('agents/nested_agent')!.content.skill_ids).toEqual([selected])
    expect(state.current.get('agents/nested_agent')!.content.default_skill_id).toBe(selected)
  })
}
