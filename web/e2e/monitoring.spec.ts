import { test, expect } from '@playwright/test'

async function loginAs(page: import('@playwright/test').Page, roles: string[]) {
  await page.addInitScript(() => {
    localStorage.setItem('access_token', 'monitoring-access')
    localStorage.setItem('refresh_token', 'monitoring-refresh')
  })
  await page.route('**/api/v1/**', async (route) => {
    const path = new URL(route.request().url()).pathname
    if (path === '/api/v1/users/me') {
      return route.fulfill({
        json: {
          user: {
            id: 17,
            username: 'operator',
            email: 'operator@example.test',
            status: 'active',
            roles,
          },
        },
      })
    }
    if (path.endsWith('/observability/overview')) {
      return route.fulfill({
        json: {
          generated_at: new Date().toISOString(),
          window_minutes: 60,
          availability: 'available',
          runs: { RUNNING: 1, COMPLETED: 2, FAILED: 0, CANCELLED: 0, INTERRUPTED: 0 },
          total: 3,
        },
      })
    }
    if (path.endsWith('/observability/runs')) {
      return route.fulfill({
        json: { items: [], page: { page: 1, page_size: 20, total: 0 } },
      })
    }
    if (path.endsWith('/observability/runs/abc')) {
      return route.fulfill({
        json: {
          run_id: 'abc',
          trace_id: null,
          status: 'FAILED',
          purpose: 'chat',
          started_at: new Date().toISOString(),
          finished_at: new Date().toISOString(),
          duration_ms: 1,
          error_code: 'AGENT_RUN_FAILED',
          tool_step_count: 0,
          knowledge_query_count: 0,
          tempo_url: null,
          langsmith_url: null,
          langsmith_export_status: null,
        },
      })
    }
    return route.fulfill({ status: 404, json: {} })
  })
}

test('监控顶层导航包含 Agent 诊断与后端判题拓扑，旧诊断地址保持可达', async ({ page }) => {
  await loginAs(page, ['admin'])
  await page.goto('/monitoring')

  await expect(page.getByRole('link', { name: '监控' })).toBeVisible()
  await expect(page.getByRole('heading', { name: '监控', exact: true })).toBeVisible()
  await expect(page.getByRole('link', { name: 'Agent 运行诊断' })).toBeVisible()
  await page.getByRole('link', { name: '判题链路', exact: true }).click()
  await expect(page).toHaveURL(/\/monitoring\/judge$/)
  await expect(page.getByRole('heading', { name: '判题链路' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'RabbitMQ' })).toBeVisible()
  await expect(page.getByText('指标接入中').first()).toBeVisible()

  await page.goto('/agent/observability?run_id=abc')
  await expect(page).toHaveURL(/\/monitoring\/agent\?run_id=abc$/)
  await expect(page.getByRole('heading', { name: 'Agent 运行诊断' })).toBeVisible()
  await expect(page.getByText('abc', { exact: false }).first()).toBeVisible()
})

test('agent_admin 可查看 Agent 诊断但不能进入系统判题监控', async ({ page }) => {
  await loginAs(page, ['agent_admin'])
  await page.goto('/monitoring')
  await expect(page.getByRole('link', { name: 'Agent 运行诊断' })).toBeVisible()
  await expect(page.getByRole('link', { name: '判题链路', exact: true })).toHaveCount(0)

  await page.goto('/monitoring/judge')
  await expect(page).toHaveURL(/\/problems$/)
})

test('system_admin 可进入判题链路监控', async ({ page }) => {
  await loginAs(page, ['system_admin'])
  await page.goto('/monitoring/judge')
  await expect(page.getByRole('heading', { name: '判题链路' })).toBeVisible()
})
