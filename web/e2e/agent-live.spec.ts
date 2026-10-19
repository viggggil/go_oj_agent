import { test, expect } from '@playwright/test'
import { randomUUID, sign } from 'node:crypto'
import { readFileSync } from 'node:fs'

// 测试栈不启动 user-service；仅用户资料由固定身份 fixture 提供，管理请求全部进入真实 Gateway/Agent/MySQL。
test('真实 Gateway 与 MySQL 保存页面配置，历史绑定可查并用于 mock 模型运行', async ({
  page,
  request,
}) => {
  const gateway = process.env.AGENT_TEST_GATEWAY_URL,
    privateKey = process.env.AGENT_TEST_USER_PRIVATE_KEY_FILE
  test.skip(
    !gateway || !privateKey || process.env.AGENT_TEST_MODEL_MODE !== 'true',
    '需隔离 Agent 集成测试栈',
  )
  const now = Math.floor(Date.now() / 1000),
    key = readFileSync(privateKey!)
  const part = (value: unknown) => Buffer.from(JSON.stringify(value)).toString('base64url')
  const tokenFor = (roles: string[]) => {
    const unsigned =
      part({ alg: 'RS256', typ: 'JWT', kid: 'user-test' }) +
      '.' +
      part({
        sub: 7301,
        username: 'web-admin',
        roles,
        iss: 'go-oj-agent',
        aud: 'go-oj-gateway',
        // Host and Docker clocks can briefly differ by a second.
        iat: now - 5,
        exp: now + 600,
        jti: randomUUID(),
      })
    return unsigned + '.' + sign('RSA-SHA256', Buffer.from(unsigned), key).toString('base64url')
  }
  const token = tokenFor(['admin']),
    headers = { Authorization: 'Bearer ' + token },
    prefix = 'web_' + randomUUID().replaceAll('-', '')
  await page.addInitScript((value) => localStorage.setItem('access_token', value), token)
  await page.route('**/api/v1/**', async (route) => {
    const path = new URL(route.request().url()).pathname
    if (path === '/api/v1/users/me')
      return route.fulfill({
        json: {
          user: {
            id: 7301,
            username: 'web-admin',
            email: 'test@example.test',
            status: 'active',
            roles: ['admin'],
          },
        },
      })
    const url = new URL(route.request().url())
    const response = await route.fetch({
      url: gateway! + url.pathname + url.search,
      maxRetries: 0,
      maxRedirects: 0,
    })
    await route.fulfill({ response })
  })
  page.on('dialog', (dialog) => dialog.accept())
  await page.goto('/agent/agents/new')
  await page.getByLabel('名称', { exact: true }).fill('浏览器集成助手')
  await page.getByRole('button', { name: '新建 Prompt', exact: true }).click()
  await expect(page.getByRole('heading', { name: '新建Prompt', exact: true })).toBeVisible()
  await page.getByLabel('名称', { exact: true }).fill(prefix + '_p')
  await page.getByLabel('正文').fill('PR8 浏览器真实配置提示')
  await page.getByRole('button', { name: '创建', exact: true }).click()
  await expect(page.getByLabel('名称', { exact: true })).toHaveValue('浏览器集成助手')
  await page.getByRole('button', { name: '新建 Skill', exact: true }).click()
  await expect(page.getByRole('heading', { name: '新建Skill', exact: true })).toBeVisible()
  await page.getByLabel('名称', { exact: true }).fill(prefix + '_s')
  const base = '/api/v1/admin/agent'
  const promptsResponse = await request.get(gateway! + base + '/prompts?search=' + prefix + '_p', {
    headers,
  })
  expect(promptsResponse.ok()).toBe(true)
  const promptSummary = (await promptsResponse.json()).items[0]
  const prompt = (
    await (await request.get(gateway! + base + '/prompts/' + promptSummary.key, { headers })).json()
  ).configuration
  await page.getByLabel('任务说明').fill('解释算法核心思路和复杂度。')
  await page.getByRole('button', { name: '创建', exact: true }).click()
  await expect(page.getByLabel('名称', { exact: true })).toHaveValue('浏览器集成助手')
  const modelResponse = await request.get(gateway! + base + '/model-options', { headers })
  expect(modelResponse.ok()).toBe(true)
  const model = (await modelResponse.json()).items[0]
  await page.getByLabel('选择模型版本').selectOption(model.id)
  await page.getByLabel('测试 Agent', { exact: true }).check()
  await page.getByLabel('到期时间（本地时区）').fill('2099-10-10T12:00')
  await page.getByRole('button', { name: '创建', exact: true }).click()
  await expect(page).toHaveURL(/\/agent\/agents\/agent_[a-f0-9]+\/edit$/)
  const agentKey = new URL(page.url()).pathname.split('/')[3]
  const skills = (
    await (
      await request.get(gateway! + base + '/skills?search=' + prefix + '_s', { headers })
    ).json()
  ).items
  const skillKey = skills[0].key
  const agentResponse = await request.get(gateway! + base + '/agents/' + agentKey, { headers })
  const agent = (await agentResponse.json()).configuration
  expect(agent.content.prompt_id).toBe(prompt.id)
  expect(agent.created_by).toBe(7301)
  // 修改 Prompt 不改变 Agent 的旧 UUID，浏览器仍可显示对应历史绑定。
  const replacement = await request.put(gateway! + base + '/prompts/' + prompt.key, {
    headers: { ...headers, 'X-Request-ID': randomUUID() },
    data: {
      expected_id: prompt.id,
      content: { name: prefix + '_p', text: '新提示', variables: [] },
    },
  })
  expect(replacement.ok()).toBe(true)
  await page.reload()
  await expect(page.locator('.binding-picker').first()).toContainText(prompt.id)
  await expect(page.locator('.binding-picker').first()).toContainText('历史绑定')
  const chat = await request.post(gateway! + '/api/v1/agent/chat', {
    headers,
    data: { agent_key: agentKey, message: 'PR8 browser real chat' },
  })
  expect(chat.ok()).toBe(true)
  const answer = await chat.text()
  expect(answer).toContain('中文算法回答')
  expect(answer).toContain('event: done')
  const denied = await request.get(gateway! + base + '/prompts/versions/' + prompt.id, {
    headers: { Authorization: 'Bearer ' + tokenFor(['user']) },
  })
  expect(denied.status()).toBe(403)
  await page.getByRole('link', { name: '对话', exact: true }).last().click()
  await expect(page).toHaveURL(new RegExp('agent_key=' + agentKey))
  await expect(page.getByLabel('选择 Agent')).toHaveValue(agentKey)
  await page.getByLabel('消息', { exact: true }).fill('PR9 browser original')
  await page.getByRole('button', { name: '发送', exact: true }).click()
  await expect(page.getByRole('status')).toHaveText('回答完成')
  await expect(page.locator('.assistant')).toContainText('中文算法回答')

  // 同一页面续聊，mock 校验已完成整轮通过真实 Gateway/仓储进入模型输入。
  await page.getByLabel('消息', { exact: true }).fill('PR2 browser followup')
  await page.getByRole('button', { name: '发送', exact: true }).click()
  await expect(page.getByRole('status')).toHaveText('回答完成')
  await expect(page.locator('.assistant')).toHaveCount(2)
  const historyId = await page.getByLabel('选择对话').inputValue()
  await page.reload()
  await page.getByLabel('选择对话').selectOption(historyId)
  await expect(page.locator('.assistant')).toHaveCount(2)
  await expect(page.locator('.user').first()).toContainText('PR9 browser original')
  await page.getByLabel('选择对话').selectOption('new')
  await expect(page.locator('.assistant')).toHaveCount(0)
  await page.getByLabel('消息', { exact: true }).fill('PR2 new conversation')
  await page.getByRole('button', { name: '发送', exact: true }).click()
  await expect(page.getByRole('status')).toHaveText('回答完成')

  // 页面显式重绑 Skill 和 Agent，新版本才用于新会话。
  const newPrompt = (await replacement.json()).configuration
  await page.goto('/agent/skills/' + skillKey + '/edit')
  await page.getByLabel('任务说明').fill('新任务说明，给出具体例子。')
  await page.getByRole('button', { name: '保存新版本', exact: true }).click()
  await expect(page.getByRole('status')).toContainText('已保存版本')
  const newSkill = (
    await (await request.get(gateway! + base + '/skills/' + skillKey, { headers })).json()
  ).configuration
  await page.goto('/agent/agents/' + agentKey + '/edit')
  await page.getByLabel('Prompt 搜索', { exact: true }).fill(prefix + '_p')
  await page.getByRole('button', { name: '搜索 Prompt', exact: true }).click()
  await page.getByLabel('选择 Prompt 版本').selectOption(newPrompt.id)
  await page.getByLabel('Skill 搜索', { exact: true }).fill(prefix)
  await page.getByRole('button', { name: '搜索 Skill', exact: true }).click()
  await page.getByLabel('选择 Skill 版本').selectOption(newSkill.id)
  await page.getByRole('button', { name: '保存新版本', exact: true }).click()
  await expect(page.getByRole('status')).toContainText('已保存版本')
  await page.getByRole('link', { name: '对话', exact: true }).last().click()
  await page.getByLabel('消息', { exact: true }).fill('PR9 browser rebound')
  await page.getByRole('button', { name: '发送', exact: true }).click()
  await expect(page.getByRole('status')).toHaveText('回答完成')
  await expect(page.locator('.assistant')).toContainText('中文算法回答')
  // 统一 ReAct 下只需授权工具，真实 Gateway → Responses mock → 带身份的 gRPC → 模型回答。
  await page.goto('/agent/skills/' + skillKey + '/edit')
  await expect(page.getByRole('combobox', { name: '回答方式', exact: true })).toHaveCount(0)
  await page.getByText('高级设置', { exact: true }).click()
  await page.getByRole('checkbox', { name: /^get_problem/ }).check()
  await page.getByRole('button', { name: '保存新版本', exact: true }).click()
  await expect(page.getByRole('status')).toContainText('已保存版本')
  const toolSkill = (
    await (await request.get(gateway! + base + '/skills/' + skillKey, { headers })).json()
  ).configuration
  await page.goto('/agent/agents/' + agentKey + '/edit')
  await page.getByLabel('Skill 搜索', { exact: true }).fill(prefix)
  await page.getByRole('button', { name: '搜索 Skill', exact: true }).click()
  await page.getByLabel('选择 Skill 版本').selectOption(toolSkill.id)
  await page.getByText('高级设置', { exact: true }).click()
  await page.getByRole('checkbox', { name: /^get_problem/ }).check()
  await page.getByRole('button', { name: '保存新版本', exact: true }).click()
  await expect(page.getByRole('status')).toContainText('已保存版本')
  await page.getByRole('link', { name: '对话', exact: true }).last().click()
  await page.getByLabel('消息', { exact: true }).fill('PR3 read problem')
  await page.getByRole('button', { name: '发送', exact: true }).click()
  await expect(page.getByRole('status')).toHaveText('回答完成')
  await expect(page.locator('.assistant')).toContainText('证据数字 739216')
  await expect(page.locator('.assistant')).not.toContainText('function_call')
  const userHeaders = { Authorization: 'Bearer ' + tokenFor(['user']) }
  const directory = await request.get(gateway! + '/api/v1/agent/agents?search=' + agentKey, {
    headers: userHeaders,
  })
  expect((await directory.json()).items).toEqual([])
  const forbiddenChat = await request.post(gateway! + '/api/v1/agent/chat', {
    headers: userHeaders,
    data: { agent_key: agentKey, message: '探测 test' },
  })
  expect(forbiddenChat.status()).toBe(404)
})

// Eval 只 mock 用户资料；数据集、运行、评分、冲突和重放均经真实 Gateway/MySQL。
test('Eval 页面创建运行、保留冲突草稿、重放丢失响应的评分并取消模型运行', async ({
  page,
  request,
}) => {
  test.setTimeout(90_000)
  const gateway = process.env.AGENT_TEST_GATEWAY_URL,
    privateKey = process.env.AGENT_TEST_USER_PRIVATE_KEY_FILE
  test.skip(
    !gateway || !privateKey || process.env.AGENT_TEST_MODEL_MODE !== 'true',
    '需隔离 Agent 集成测试栈',
  )
  const now = Math.floor(Date.now() / 1000),
    key = readFileSync(privateKey!)
  const encode = (value: unknown) => Buffer.from(JSON.stringify(value)).toString('base64url')
  const unsigned =
    encode({ alg: 'RS256', typ: 'JWT', kid: 'user-test' }) +
    '.' +
    encode({
      sub: 7302,
      username: 'eval-admin',
      roles: ['admin'],
      iss: 'go-oj-agent',
      aud: 'go-oj-gateway',
      // Host and Docker clocks can briefly differ by a second.
      iat: now - 5,
      exp: now + 600,
      jti: randomUUID(),
    })
  const token =
    unsigned + '.' + sign('RSA-SHA256', Buffer.from(unsigned), key).toString('base64url')
  const headers = { Authorization: 'Bearer ' + token },
    base = '/api/v1/admin/agent/evals'
  await page.addInitScript((value) => localStorage.setItem('access_token', value), token)
  await page.route('**/api/v1/**', async (route) => {
    const url = new URL(route.request().url())
    if (url.pathname === '/api/v1/users/me')
      return route.fulfill({
        json: { user: { id: 7302, username: 'eval-admin', roles: ['admin'], status: 'active' } },
      })
    const response = await route.fetch({
      url: gateway! + url.pathname + url.search,
      maxRetries: 0,
      maxRedirects: 0,
    })
    await route.fulfill({ response })
  })
  const name = '浏览器评测_' + randomUUID()
  const sample = {
    id: 'browser_soft',
    category: 'algorithm',
    title: '算法回答人工核对',
    turns: [{ role: 'user', content: '介绍算法' }],
    fixture: { principal: 'student_a' },
    expectations: { required_facts: ['字面检查未命中_' + randomUUID()] },
  }
  await page.goto('/agent/eval')
  await page.getByRole('link', { name: '上传数据集', exact: true }).click()
  await expect(page).toHaveURL(/\/agent\/eval\/datasets\/new$/)
  await page.getByLabel('名称', { exact: true }).fill(name)
  await page.getByLabel('JSONL 内容', { exact: true }).fill(JSON.stringify(sample))
  await page.getByRole('button', { name: '上传并校验', exact: true }).click()
  const row = page.getByRole('row').filter({ hasText: name })
  await expect(row).toBeVisible()
  await row.getByRole('button', { name: '发起评测', exact: true }).click()
  await page.getByLabel('被测 Agent', { exact: true }).selectOption('learning_assistant')
  await page.getByRole('button', { name: '启动评测', exact: true }).click()
  await expect(
    page.getByRole('heading', { name: 'learning_assistant · 执行完成', exact: true }),
  ).toBeVisible({ timeout: 20_000 })
  const runId = new URL(page.url()).searchParams.get('run')!
  await page.getByRole('button', { name: '查看与评分', exact: true }).click()
  const result = page.getByRole('article', { name: '评测结果详情' })
  await expect(result).toContainText('中文算法回答')
  await page
    .getByLabel('评审备注', { exact: true })
    .fill('保留我的草稿 <img src=x onerror=alert(1)>')
  await page.getByLabel('本次修改原因', { exact: true }).fill('按算法含义复核')
  await page.getByLabel('人工结论', { exact: true }).selectOption({ label: '通过' })
  for (const label of ['任务完成', '技术正确性', '证据支持', '指令遵循', '表达清晰度'])
    await page.getByLabel(label, { exact: true }).fill('4')
  const resultsResponse = await request.get(gateway! + base + `/runs/${runId}/results`, { headers })
  const resultId = (await resultsResponse.json()).items[0].id
  const reviewPath = base + `/results/${resultId}/review`
  const competing = await request.post(gateway! + reviewPath, {
    headers: { ...headers, 'X-Request-ID': randomUUID() },
    data: {
      expected_version: 0,
      scores: {
        task_completion: 2,
        technical_correctness: 2,
        evidence_support: 2,
        instruction_following: 2,
        clarity: 2,
      },
      pass: false,
      tags: [],
      note: '另一个管理员先保存',
      reason: '并发评审',
    },
  })
  expect(competing.ok()).toBe(true)
  await page.getByRole('button', { name: '保存人工评审', exact: true }).click()
  await expect(result).toContainText('草稿已保留')
  await expect(page.getByLabel('评审备注', { exact: true })).toHaveValue(
    '保留我的草稿 <img src=x onerror=alert(1)>',
  )
  await page.getByRole('button', { name: '加载最新评分并保留草稿', exact: true }).click()
  await expect(result).toContainText('已加载最新评分')
  let lost = false
  const writeIds: string[] = []
  await page.route('**' + reviewPath, async (route) => {
    writeIds.push(route.request().headers()['x-request-id'])
    const response = await route.fetch({
      url: gateway! + reviewPath,
      maxRetries: 0,
      maxRedirects: 0,
    })
    expect(response.ok()).toBe(true)
    if (!lost) {
      lost = true
      await route.abort('failed')
    } else await route.fulfill({ response })
  })
  await page.getByRole('button', { name: '保存人工评审', exact: true }).click()
  await expect(result.getByRole('alert')).toContainText('请求未确认')
  await page.getByRole('button', { name: '保存人工评审', exact: true }).click()
  await expect(result).toContainText('人工评审已保存')
  expect(writeIds).toHaveLength(2)
  expect(writeIds[0]).toBe(writeIds[1])
  const audit = await request.get(gateway! + base + `/results/${resultId}/reviews`, { headers })
  expect((await audit.json()).page.total).toBe(2)
  await page.reload()
  await expect(
    page.getByRole('heading', { name: 'learning_assistant · 执行完成', exact: true }),
  ).toBeVisible()
  await page.getByRole('button', { name: '查看与评分', exact: true }).click()
  await expect(page.getByLabel('评审备注', { exact: true })).toHaveValue(
    '保留我的草稿 <img src=x onerror=alert(1)>',
  )
  await expect(page.getByRole('article', { name: '评测结果详情' }).locator('img')).toHaveCount(0)
  await expect(page.getByRole('article', { name: '评测结果详情' })).toContainText('当前评分版本 2')
  // 从新建运行表单取消一个仍在模型流中的批次。
  const cancelDataset = await request.post(gateway! + base + '/datasets', {
    headers: { ...headers, 'X-Request-ID': randomUUID() },
    data: {
      name: name + '_cancel',
      content: JSON.stringify({
        ...sample,
        id: 'browser_cancel',
        turns: [{ role: 'user', content: 'wait-for-cancel' }],
        expectations: {},
      }),
    },
  })
  expect(cancelDataset.ok()).toBe(true)
  const datasetId = (await cancelDataset.json()).dataset.id
  await page.goto('/agent/eval?tab=runs&dataset=' + datasetId)
  await page.getByLabel('被测 Agent', { exact: true }).selectOption('learning_assistant')
  await page.getByRole('button', { name: '启动评测', exact: true }).click()
  await expect(
    page.getByRole('heading', { name: 'learning_assistant · 运行中', exact: true }),
  ).toBeVisible({ timeout: 20_000 })
  await page.getByRole('button', { name: '取消运行', exact: true }).click()
  await expect(
    page.getByRole('heading', { name: 'learning_assistant · 已取消', exact: true }),
  ).toBeVisible({ timeout: 20_000 })
  await page.getByRole('button', { name: '查看与评分', exact: true }).click()
  await expect(page.getByRole('article', { name: '评测结果详情' })).toContainText(
    '此样例存在硬失败',
  )
  await expect(
    page.getByLabel('人工结论', { exact: true }).getByRole('option', { name: '通过', exact: true }),
  ).toHaveJSProperty('disabled', true)
  const modelDataset = await request.post(gateway! + base + '/datasets', {
    headers: { ...headers, 'X-Request-ID': randomUUID() },
    data: {
      name: name + '_model',
      content: JSON.stringify({
        ...sample,
        id: 'browser_model_' + randomUUID(),
        turns: [{ role: 'user', content: 'review-invalid-once' }],
        expectations: {},
      }),
    },
  })
  expect(modelDataset.ok()).toBe(true)
  await page.goto('/agent/eval?tab=runs&dataset=' + (await modelDataset.json()).dataset.id)
  await page.getByLabel('被测 Agent', { exact: true }).selectOption('learning_assistant')
  await page.getByLabel('评测模式', { exact: true }).selectOption('programmatic_review')
  await expect(page.getByLabel('评审 Agent', { exact: true })).toHaveValue('eval_reviewer')
  await page.getByRole('button', { name: '启动评测', exact: true }).click()
  await expect(
    page.getByRole('heading', { name: 'learning_assistant · 执行完成', exact: true }),
  ).toBeVisible({ timeout: 20_000 })
  await page.getByRole('button', { name: '查看与评分', exact: true }).click()
  await expect(result).toContainText('AGENT_EVAL_REVIEW_INVALID')
  await page.getByLabel('评审备注', { exact: true }).fill('模型重跑时保留我的人工草稿')
  let retrySubmitted = false,
    pollFailed = false
  await page.route('**/evals/**', async (route) => {
    const path = new URL(route.request().url()).pathname
    if (path.endsWith('/review-agent')) retrySubmitted = true
    if (retrySubmitted && !pollFailed && /\/runs\/[^/]+\/results\/[^/]+$/.test(path)) {
      pollFailed = true
      return route.fulfill({ status: 503, json: { reason: 'AGENT_UNAVAILABLE' } })
    }
    return route.fallback()
  })
  await page.getByRole('button', { name: '重跑评审 Agent', exact: true }).click()
  await expect(result).toContainText('模型通过', { timeout: 15_000 })
  expect(pollFailed).toBe(true)
  await expect(page.getByLabel('评审备注', { exact: true })).toHaveValue(
    '模型重跑时保留我的人工草稿',
  )
  await result.getByText('评审尝试记录', { exact: true }).click()
  await expect(result).toContainText('第 2 次 · COMPLETED')
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  )
})
