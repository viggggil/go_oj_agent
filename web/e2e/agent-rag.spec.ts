import { test, expect, type Page } from '@playwright/test'
import { randomUUID } from 'node:crypto'
import type { KnowledgeDocument, KnowledgeGroup } from '../src/agent/rag'

async function mockRag(page: Page, roles = ['agent_admin']) {
  const groups: KnowledgeGroup[] = [
    { id: randomUUID(), name: '题解', created_by: 7, created_at: new Date().toISOString() },
  ]
  const documents: KnowledgeDocument[] = [],
    history: KnowledgeDocument[] = [],
    writes: string[] = []
  const responses = new Map<string, unknown>()
  let loseResponse = false,
    conflict = false,
    calls = 0
  await page.addInitScript(() => localStorage.setItem('access_token', 'mock-access'))
  await page.route('**/api/v1/**', async (route) => {
    const request = route.request(),
      url = new URL(request.url()),
      path = url.pathname,
      method = request.method()
    const send = (json: unknown, status = 200) => route.fulfill({ json, status })
    if (path === '/api/v1/users/me')
      return send({ user: { id: 7, username: '管理员', roles, status: 'active' } })
    if (!path.startsWith('/api/v1/admin/agent/rag'))
      return send({ items: [], page: { page: 1, page_size: 20, total: 0 } })
    calls++
    if (!roles.includes('agent_admin')) return send({}, 403)
    const parts = path.split('/'),
      resource = parts[6],
      id = parts[7],
      action = parts[8]
    const paged = (items: unknown[]) => {
      const number = Number(url.searchParams.get('page') || 1)
      return send({
        items: items.slice((number - 1) * 20, number * 20),
        page: { page: number, page_size: 20, total: items.length },
      })
    }
    if (method === 'GET') {
      if (resource === 'knowledge-bases')
        return id ? send({ knowledge_base: groups.find((item) => item.id === id) }) : paged(groups)
      if (action === 'versions')
        return paged(history.filter((item) => item.document_id === id).toReversed())
      if (id) return send({ document: documents.find((item) => item.document_id === id) })
      return paged(documents.filter((item) => item.base_id === url.searchParams.get('base_id')))
    }
    const body = request.postDataJSON(),
      requestId = request.headers()['x-request-id']
    expect(requestId).toMatch(/^[0-9a-f-]{36}$/)
    if (resource === 'preview')
      return send({
        chunks: [
          { number: 0, text: body.content, section: '正文', start: 0, end: body.content.length },
        ],
        page: { page: 1, page_size: 20, total: 1 },
      })
    if (resource === 'search')
      return send({
        matches: documents
          .filter((item) => item.enabled && body.base_ids.includes(item.base_id))
          .map((item) => ({
            score: 0.8,
            base_id: item.base_id,
            document_id: item.document_id,
            version_id: item.id,
            title: item.title,
            problem_id: item.problem_id,
            number: 0,
            text: item.content,
            section: '正文',
            start: 0,
            end: item.content!.length,
          })),
      })
    writes.push(requestId)
    if (responses.has(requestId)) return send(responses.get(requestId))
    if (resource === 'knowledge-bases') {
      if (id) {
        const group = groups.find((item) => item.id === id)!
        if (group.name !== body.expected_name) return send({ code: 'AGENT_RAG_STALE' }, 409)
        group.name = body.name
        return send({ knowledge_base: group })
      }
      const group = {
        id: randomUUID(),
        name: body.name,
        created_by: 7,
        created_at: new Date().toISOString(),
      }
      groups.unshift(group)
      const response = { knowledge_base: group }
      responses.set(requestId, response)
      return send(response)
    }
    if (action) {
      const doc = documents.find((item) => item.document_id === id)!
      doc.enabled = action === 'enable'
      if (action === 'retry') doc.state = 'READY'
      return send({ result: { document_id: id, version_id: doc.id, action } })
    }
    const old = documents.find((item) => item.document_id === body.document_id)
    if (conflict && old) {
      conflict = false
      old.id = randomUUID()
      old.content = '另一管理员的正文'
      return send({ code: 'AGENT_RAG_STALE' }, 409)
    }
    const doc: KnowledgeDocument = {
      ...body,
      id: randomUUID(),
      document_id: old?.document_id || randomUUID(),
      enabled: old?.enabled || false,
      active_version: null,
      content_hash: 'hash',
      size_bytes: 40,
      model_revision: 'fixed',
      splitter_version: 'v1',
      created_by: 7,
      created_at: new Date().toISOString(),
      state: 'READY',
      chunk_count: 1,
      error_code: null,
    }
    doc.active_version = doc.id
    if (old) documents.splice(documents.indexOf(old), 1)
    documents.push(doc)
    history.push({ ...doc })
    const response = { document: doc }
    responses.set(requestId, response)
    if (loseResponse) {
      loseResponse = false
      return send({}, 503)
    }
    return send(response)
  })
  return {
    groups,
    documents,
    history,
    writes,
    calls: () => calls,
    lose: () => {
      loseResponse = true
    },
    conflict: () => {
      conflict = true
    },
  }
}

async function upload(page: Page) {
  await page.getByRole('button', { name: '上传文档', exact: true }).click()
  await page.getByLabel('选择文件', { exact: false }).setInputFiles({
    name: 'queue.md',
    mimeType: 'text/markdown',
    buffer: Buffer.from('# 单调队列\n\n每个下标最多进出队列一次。'),
  })
  await page.getByLabel('文档标题').fill('滑动窗口最大值题解')
  await page.getByLabel('关联题号').fill('42')
}

test('管理员按组上传、预览、启用检索、编辑版本与归档，移动端保持导航', async ({ page }) => {
  const state = await mockRag(page)
  await page.goto('/agent/rag')
  await expect(page.getByRole('link', { name: 'RAG 知识库' })).toBeVisible()
  await page.getByLabel('新分组名称').fill('算法讲义')
  await page.getByRole('button', { name: '创建分组' }).click()
  await expect(page.getByRole('status')).toContainText('分组已创建')
  await page.getByRole('button', { name: '题解', exact: true }).click()
  await upload(page)
  await page.getByRole('button', { name: '切分预览' }).click()
  await expect(page.getByRole('dialog')).toContainText('每个下标最多进出队列一次')
  await page.getByRole('button', { name: '上传并索引' }).click()
  await expect(page.getByRole('dialog')).not.toBeVisible()
  expect(state.documents[0].base_id).toBe(state.groups.find((item) => item.name === '题解')!.id)
  await page.getByRole('button', { name: '启用', exact: true }).click()
  await expect(page.getByRole('status')).toContainText('已启用')
  await page.getByLabel('检索问题').fill('单调队列复杂度')
  await page.getByRole('button', { name: '测试检索' }).click()
  await expect(page.locator('.rag-search .rag-match')).toContainText('每个下标最多进出队列一次')
  await page.getByRole('button', { name: '查看与编辑' }).click()
  await page.getByLabel('正文', { exact: true }).fill('# 更新题解\n时间 O(n)，空间 O(k)。')
  await page.getByRole('button', { name: '保存新版本', exact: true }).click()
  await expect(page.getByRole('dialog')).not.toBeVisible()
  expect(state.history).toHaveLength(2)
  await page.getByRole('button', { name: '查看与编辑' }).click()
  await expect(page.locator('.rag-history')).toHaveCount(2)
  await page.getByRole('button', { name: '关闭文档' }).click()
  await page.getByRole('button', { name: '归档', exact: true }).click()
  await expect(page.getByRole('status')).toContainText('已归档')
  await page.getByRole('button', { name: '测试检索' }).click()
  await expect(page.getByText('没有匹配的已发布资料。', { exact: false })).toBeVisible()
  await page.setViewportSize({ width: 390, height: 844 })
  await expect
    .poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth))
    .toBe(true)
  await page.getByRole('button', { name: '切换菜单' }).click()
  await expect(page.getByRole('navigation', { name: 'Agent 管理导航' })).toBeVisible()
  await page.screenshot({ path: 'test-results/rag-mobile.png', fullPage: true })
})

test('写入响应丢失重用请求 UUID，版本冲突保留输入，显式刷新后再编辑', async ({ page }) => {
  const state = await mockRag(page)
  page.on('dialog', (dialog) => dialog.accept())
  await page.goto('/agent/rag')
  await upload(page)
  state.lose()
  await page.getByRole('button', { name: '上传并索引' }).click()
  await expect(page.getByRole('dialog').getByRole('alert')).toContainText('暂不可用')
  await expect(page.getByLabel('文档标题')).toHaveValue('滑动窗口最大值题解')
  await page.getByRole('button', { name: '上传并索引' }).click()
  await expect(page.getByRole('dialog')).not.toBeVisible()
  expect(state.documents).toHaveLength(1)
  expect(state.writes[0]).toBe(state.writes[1])
  await page.getByRole('button', { name: '查看与编辑' }).click()
  await page.getByLabel('正文', { exact: true }).fill('本次尚未保存的修改')
  state.conflict()
  await page.getByRole('button', { name: '保存新版本', exact: true }).click()
  await expect(page.getByRole('dialog').getByRole('alert')).toContainText('已变化')
  await expect(page.getByLabel('正文', { exact: true })).toHaveValue('本次尚未保存的修改')
  await page.getByRole('button', { name: '重新加载当前文档' }).click()
  await expect(page.getByLabel('正文', { exact: true })).toHaveValue('另一管理员的正文')
})

test('普通用户直接访问 RAG URL 回到对话，不请求管理文档', async ({ page }) => {
  const state = await mockRag(page, ['user'])
  await page.goto('/agent/rag')
  await expect(page).toHaveURL('/agent/chat')
  await expect(page.getByRole('link', { name: 'RAG 知识库' })).toHaveCount(0)
  expect(state.calls()).toBe(0)
})
