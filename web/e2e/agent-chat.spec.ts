import { test, expect, type Page } from '@playwright/test'
import { randomUUID } from 'node:crypto'
import { createServer, type ServerResponse } from 'node:http'

const run = randomUUID(),
  conversation = randomUUID()
const headers = {
  'Content-Type': 'text/event-stream',
  'X-Agent-Run-ID': run,
  'X-Agent-Conversation-ID': conversation,
}
const frame = (type: string, sequence: number, data: object = {}) =>
  `event: ${type}\ndata: ${JSON.stringify({ type, sequence, run_id: run, conversation_id: conversation, data })}\n\n`
async function setup(page: Page) {
  await page.addInitScript(() => {
    localStorage.setItem('access_token', 'chat-access')
    localStorage.setItem('refresh_token', 'chat-refresh')
  })
  await page.route('**/api/v1/problems?*', (route) =>
    route.fulfill({ json: { items: [], page: { page: 1, page_size: 20, total: 0 } } }),
  )
  await page.route('**/api/v1/users/me', (route) =>
    route.fulfill({ json: { user: { id: 7, username: '测试用户', roles: ['user'] } } }),
  )
  await page.route('**/api/v1/agent/conversations?*', (route) =>
    route.fulfill({ json: { items: [], page: { page: 1, page_size: 20, total: 0 } } }),
  )
  await page.route('**/api/v1/agent/agents?*', (route) =>
    route.fulfill({
      json: {
        items: [
          { key: 'public_agent', name: '算法助手', is_default: true },
          { key: 'second_agent', name: '第二助手', is_default: false },
        ],
        page: { page: 1, page_size: 20, total: 2 },
      },
    }),
  )
  await page.route('**/api/v1/agent/agents/*/skills', (route) =>
    route.fulfill({
      json: {
        items: [
          { key: 'direct_skill', name: '直接回答', execution_mode: 'direct', default: true },
          { key: 'other_skill', name: '另一技能', execution_mode: 'direct', default: false },
        ],
        default_skill_key: 'direct_skill',
      },
    }),
  )
}
test('目录选择、中文 IME、连续会话、安全 Markdown 与复制，桌面和移动布局', async ({ page }) => {
  await setup(page)
  const requests: any[] = []
  await page.route('**/api/v1/agent/chat', (route) => {
    requests.push(route.request().postDataJSON())
    return route.fulfill({
      headers,
      body:
        frame('thinking', 1, { text: '整理中' }) +
        frame('token', 2, {
          text: '中文回答\n\n```cpp\nint x = 1;\n```\n\n<script>window.chatXSS=1</script>\n[jump](javascript:alert(1))',
        }) +
        frame('done', 3),
    })
  })
  await page.goto('/agent/chat')
  await expect(page.getByLabel('选择 Agent')).toHaveValue('public_agent')
  await page.getByLabel('消息', { exact: true }).fill('中文问题')
  await page
    .getByLabel('消息', { exact: true })
    .dispatchEvent('keydown', { key: 'Enter', ctrlKey: true, isComposing: true })
  expect(requests).toHaveLength(0)
  await page.getByLabel('消息', { exact: true }).press('Control+Enter')
  await expect(page.getByRole('status')).toHaveText('回答完成')
  await expect(page.locator('.assistant')).toContainText('中文回答')
  await expect(page.locator('.assistant script, .assistant a[href^="javascript:"]')).toHaveCount(0)
  await expect(page.getByRole('button', { name: '复制代码' })).toBeVisible()
  await expect(page.getByRole('button', { name: '复制', exact: true })).toBeVisible()
  await page.getByLabel('选择 Skill').selectOption('other_skill')
  await page.getByLabel('消息', { exact: true }).fill('下一条')
  await page.getByRole('button', { name: '发送', exact: true }).click()
  await expect(page.getByRole('status')).toHaveText('回答完成')
  expect(requests[0].conversation_id).toBeUndefined()
  expect(requests[1]).toMatchObject({ conversation_id: conversation, skill_key: 'other_skill' })
  await page.getByLabel('选择对话').selectOption('new')
  await expect(page.locator('.chat-message')).toHaveCount(0)
  await page.screenshot({ path: 'test-results/agent-chat-desktop.png', fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  )
  await page.screenshot({ path: 'test-results/agent-chat-mobile.png', fullPage: true })
})
test('401 共享刷新但不重放 Chat，HTTP 拒绝和 EOF 保留输入并标未完成', async ({ page }) => {
  await setup(page)
  let posts = 0,
    refresh = 0,
    mode = 401
  await page.route('**/api/v1/auth/refresh', (route) => {
    refresh++
    return route.fulfill({ json: { access_token: 'new-access', refresh_token: 'new-refresh' } })
  })
  await page.route('**/api/v1/agent/chat', (route) => {
    posts++
    return mode === 200
      ? route.fulfill({ headers, body: frame('token', 1, { text: '部分答案' }) })
      : route.fulfill({ status: mode, json: {} })
  })
  await page.goto('/agent/chat')
  await page.getByLabel('消息', { exact: true }).fill('保留原消息')
  await page.getByRole('button', { name: '发送', exact: true }).click()
  await expect(page.getByRole('status')).toContainText('再次点击发送')
  expect(posts).toBe(1)
  expect(refresh).toBe(1)
  await expect(page.getByLabel('消息', { exact: true })).toHaveValue('保留原消息')
  mode = 409
  await page.getByRole('button', { name: '发送', exact: true }).click()
  await expect(page.getByRole('status')).toContainText('配置冲突')
  mode = 503
  await page.getByRole('button', { name: '发送', exact: true }).click()
  await expect(page.getByRole('status')).toContainText('暂不可用')
  mode = 200
  await page.getByRole('button', { name: '发送', exact: true }).click()
  await expect(page.getByRole('status')).toContainText('连接提前结束')
  await expect(page.locator('.assistant').last()).toContainText('部分答案')
  await expect(page.locator('.assistant').last()).toContainText('连接提前结束')
  await expect(page.getByLabel('消息', { exact: true })).toHaveValue('保留原消息')
})
test('不可访问目标保持空选择，不回退到默认 Agent', async ({ page }) => {
  await setup(page)
  await page.goto('/agent/chat?agent_key=expired_test')
  await expect(page.getByRole('alert')).toContainText('指定 Agent 不存在')
  await expect(page.getByLabel('选择 Agent')).toHaveValue('')
  await expect(page.getByRole('button', { name: '发送', exact: true })).toBeDisabled()
  // 较早的精确 key 在子串匹配第 2 页，也必须能通过管理页链接选中。
  await page.route('**/api/v1/agent/agents?*', (route) => {
    const url = new URL(route.request().url()),
      number = Number(url.searchParams.get('page'))
    const items =
      number === 2
        ? [{ key: 'older_test', name: '旧测试助手', is_default: false }]
        : Array.from({ length: 20 }, (_, i) => ({
            key: `older_test_${i}`,
            name: '新版',
            is_default: false,
          }))
    return route.fulfill({ json: { items, page: { page: number, page_size: 20, total: 21 } } })
  })
  await page.goto('/agent/chat?agent_key=older_test')
  await expect(page.getByLabel('选择 Agent')).toHaveValue('older_test')
  await expect(page.getByLabel('选择 Skill')).toHaveValue('direct_skill')
})
test('真实流的停止、新对话、切换与离开会 abort，迟到 token 不进入新会话', async ({ page }) => {
  await setup(page)
  const streams: ServerResponse[] = [],
    requests: any[] = []
  const server = createServer((req, res) => {
    if (req.method === 'OPTIONS') {
      res.writeHead(204, {
        'Access-Control-Allow-Origin': '*',
        'Access-Control-Allow-Headers': '*',
        'Access-Control-Allow-Methods': '*',
      })
      res.end()
      return
    }
    let body = ''
    req.on('data', (chunk) => (body += chunk))
    req.on('end', () => {
      requests.push(JSON.parse(body))
      streams.push(res)
      res.writeHead(200, {
        ...headers,
        'Access-Control-Allow-Origin': '*',
        'Access-Control-Expose-Headers': '*',
      })
      res.write(frame('token', 1, { text: '初始中文' }))
    })
  })
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve))
  const port = (server.address() as { port: number }).port
  await page.route('**/api/v1/agent/chat', (route) =>
    route.continue({ url: `http://127.0.0.1:${port}/chat` }),
  )
  try {
    await page.goto('/agent/chat')
    const send = async () => {
      await page.getByLabel('消息', { exact: true }).fill('等待回答')
      await page.getByRole('button', { name: '发送', exact: true }).click()
      await expect(page.getByRole('button', { name: '停止', exact: true })).toBeVisible()
      await expect(page.locator('.assistant').last()).toContainText('初始中文')
    }
    await send()
    await expect(page.getByLabel('选择 Skill')).toBeDisabled()
    await page.getByRole('button', { name: '停止', exact: true }).click()
    await expect(page.getByRole('status')).toContainText('已停止')
    streams[0]?.write(frame('token', 2, { text: '迟到污染' }))
    await page.getByLabel('选择对话').selectOption('new')
    await send()
    await page.getByLabel('选择 Agent').selectOption('second_agent')
    await expect(page.locator('.chat-message')).toHaveCount(0)
    streams[1]?.end(frame('token', 2, { text: '迟到污染' }) + frame('done', 3))
    await send()
    expect(requests.at(-1)).toMatchObject({ agent_key: 'second_agent' })
    expect(requests.at(-1).conversation_id).toBeUndefined()
    await page.getByLabel('选择对话').selectOption('new')
    await expect(page.locator('.chat-message')).toHaveCount(0)
    await expect(page.locator('.chat-messages')).not.toContainText('迟到污染')
    await send()
    await page.getByRole('link', { name: '题库', exact: true }).click()
    await expect(page).toHaveURL(/\/problems/)
  } finally {
    streams.forEach((res) => res.destroy())
    server.closeAllConnections()
    await new Promise<void>((resolve) => server.close(() => resolve()))
  }
})

test('恢复历史后续聊，失败在回答位置显示且不自动重发', async ({ page }) => {
  await setup(page)
  await page.route('**/api/v1/agent/conversations?*', (route) =>
    route.fulfill({
      json: {
        items: [
          {
            id: conversation,
            agent_key: 'public_agent',
            title: '题目讲解',
            updated_at: '2026-10-14T06:00:00',
          },
        ],
        page: { page: 1, page_size: 20, total: 1 },
      },
    }),
  )
  await page.route('**/api/v1/agent/conversations/*/messages*', (route) =>
    route.fulfill({
      json: {
        conversation: { id: conversation, agent_key: 'public_agent', title: '题目讲解' },
        items: [
          { id: 1, role: 'user', content: '第一题', status: 'COMPLETED', error_code: null },
          { id: 2, role: 'assistant', content: '题目内容', status: 'COMPLETED', error_code: null },
        ],
        next_before: null,
      },
    }),
  )
  let posts = 0
  await page.route('**/api/v1/agent/chat', (route) => {
    posts++
    expect(route.request().postDataJSON().conversation_id).toBe(conversation)
    return route.fulfill({ headers, body: frame('error', 1, { code: 'AGENT_MODEL_UNAVAILABLE' }) })
  })
  await page.goto('/agent/chat')
  await page.getByLabel('选择对话').selectOption(conversation)
  await expect(page.locator('.assistant')).toContainText('题目内容')
  await expect(page.getByLabel('搜索 Agent')).toHaveCount(0)
  await page.getByLabel('消息', { exact: true }).fill('解释思路')
  await page.getByRole('button', { name: '发送', exact: true }).click()
  await expect(page.locator('.assistant').last()).toContainText('模型连接中断')
  await expect(page.getByLabel('消息', { exact: true })).toHaveValue('解释思路')
  expect(posts).toBe(1)
  await page.getByLabel('选择对话').selectOption('new')
  await expect(page.locator('.chat-message')).toHaveCount(0)
})
