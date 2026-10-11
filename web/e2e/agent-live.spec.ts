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
        iat: now,
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
  await page.getByLabel('稳定标识').fill(prefix)
  await page.getByLabel('名称', { exact: true }).fill('浏览器集成助手')
  await page.getByRole('button', { name: '新建 Prompt', exact: true }).click()
  await expect(page.getByRole('heading', { name: '新建Prompt', exact: true })).toBeVisible()
  await page.getByLabel('稳定标识').fill(prefix + '_p')
  await page.getByLabel('正文').fill('PR8 浏览器真实配置提示')
  await page.getByRole('button', { name: '创建', exact: true }).click()
  await expect(page.getByLabel('名称', { exact: true })).toHaveValue('浏览器集成助手')
  await page.getByRole('button', { name: '新建 Skill', exact: true }).click()
  await expect(page.getByRole('heading', { name: '新建Skill', exact: true })).toBeVisible()
  await page.getByLabel('稳定标识').fill(prefix + '_s')
  await page.getByLabel('名称', { exact: true }).fill('算法解释')
  const base = '/api/v1/admin/agent'
  const promptResponse = await request.get(gateway! + base + '/prompts/' + prefix + '_p', {
    headers,
  })
  expect(promptResponse.ok()).toBe(true)
  const prompt = (await promptResponse.json()).configuration
  await page.getByLabel('Prompt 搜索', { exact: true }).fill(prefix)
  await page.getByRole('button', { name: '搜索 Prompt', exact: true }).click()
  await page.getByLabel('选择 Prompt 版本').selectOption(prompt.id)
  await page.getByRole('button', { name: '创建', exact: true }).click()
  await expect(page.getByLabel('稳定标识')).toHaveValue(prefix)
  const modelResponse = await request.get(gateway! + base + '/model-options', { headers })
  expect(modelResponse.ok()).toBe(true)
  const model = (await modelResponse.json()).items[0]
  await page.getByLabel('选择模型版本').selectOption(model.id)
  await page.getByLabel('测试 Agent', { exact: true }).check()
  await page.getByLabel('到期时间（本地时区）').fill('2099-10-10T12:00')
  await page.getByRole('button', { name: '创建', exact: true }).click()
  await expect(page).toHaveURL(new RegExp('/agent/agents/' + prefix + '/edit$'))
  const agentResponse = await request.get(gateway! + base + '/agents/' + prefix, { headers })
  const agent = (await agentResponse.json()).configuration
  expect(agent.content.prompt_id).toBe(prompt.id)
  expect(agent.created_by).toBe(7301)
  // 修改 Prompt 不改变 Agent 的旧 UUID，浏览器仍可显示对应历史绑定。
  const replacement = await request.put(gateway! + base + '/prompts/' + prefix + '_p', {
    headers: { ...headers, 'X-Request-ID': randomUUID() },
    data: { expected_id: prompt.id, content: { text: '新提示', variables: [] } },
  })
  expect(replacement.ok()).toBe(true)
  await page.reload()
  await expect(page.locator('.binding-picker').first()).toContainText(prompt.id)
  await expect(page.locator('.binding-picker').first()).toContainText('历史绑定')
  const chat = await request.post(gateway! + '/api/v1/agent/chat', {
    headers,
    data: { agent_key: prefix, message: 'PR8 browser real chat' },
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
  await expect(page).toHaveURL(new RegExp('agent_key=' + prefix))
})
