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
    if (path === '/api/v1/admin/observability/metrics') {
      const platformAdmin = roles.some((role) => ['admin', 'system_admin'].includes(role))
      return route.fulfill({
        json: {
          generated_at: new Date().toISOString(),
          window: new URL(route.request().url()).searchParams.get('window') || '1h',
          scope: platformAdmin ? 'system' : 'agent',
          state: 'available',
          ...(platformAdmin
            ? { grafana_url: 'http://127.0.0.1:3000/d/oj-system-overview/oj-system-overview' }
            : {}),
          targets: platformAdmin
            ? [
                {
                  name: 'collector_exporter',
                  state: 'up',
                  last_scrape_at: new Date().toISOString(),
                },
                {
                  name: 'collector_self_metrics',
                  state: 'up',
                  last_scrape_at: new Date().toISOString(),
                },
                { name: 'contest_metrics', state: 'up', last_scrape_at: new Date().toISOString() },
              ]
            : [
                {
                  name: 'collector_exporter',
                  state: 'up',
                  last_scrape_at: new Date().toISOString(),
                },
                {
                  name: 'collector_self_metrics',
                  state: 'up',
                  last_scrape_at: new Date().toISOString(),
                },
              ],
          series: platformAdmin
            ? [
                {
                  key: 'request_rate',
                  label: 'HTTP 请求速率',
                  unit: '请求/秒',
                  service: 'gateway-service',
                  state: 'available',
                  points: [{ timestamp: new Date().toISOString(), value: 1.2 }],
                },
                {
                  key: 'request_error_rate',
                  label: 'HTTP 5xx 速率',
                  unit: '请求/秒',
                  service: 'gateway-service',
                  state: 'available',
                  points: [{ timestamp: new Date().toISOString(), value: 0 }],
                },
                {
                  key: 'api_latency_p95',
                  label: 'API 延迟 P95',
                  unit: '秒',
                  service: 'gateway-service',
                  state: 'available',
                  points: [{ timestamp: new Date().toISOString(), value: 0.08 }],
                },
                {
                  key: 'agent_run_success_ratio',
                  label: 'Agent Run 成功率',
                  unit: '比例',
                  state: 'available',
                  points: [{ timestamp: new Date().toISOString(), value: 0.75 }],
                },
                {
                  key: 'judge_submission_rate',
                  label: '判题提交速率',
                  unit: '次/秒',
                  state: 'no_data',
                  points: [],
                },
                {
                  key: 'judge_queue_p95',
                  label: 'Judge 排队 P95',
                  unit: '秒',
                  state: 'available',
                  points: [
                    { timestamp: new Date(Date.now() - 60_000).toISOString(), value: 0.2 },
                    { timestamp: new Date().toISOString(), value: 0.3 },
                  ],
                },
                {
                  key: 'agent_run_rate',
                  label: 'Agent Run 启动速率',
                  unit: '次/秒',
                  state: 'available',
                  points: [{ timestamp: new Date().toISOString(), value: 0.1 }],
                },
                {
                  key: 'agent_model_failure_rate',
                  label: 'Agent 模型失败速率',
                  unit: '次/秒',
                  state: 'no_data',
                  points: [],
                },
              ]
            : [
                {
                  key: 'agent_run_in_flight',
                  label: 'Agent 在途 Run',
                  unit: '个',
                  state: 'available',
                  points: [{ timestamp: new Date().toISOString(), value: 1 }],
                },
                {
                  key: 'agent_model_failure_rate',
                  label: 'Agent 模型失败速率',
                  unit: '次/秒',
                  state: 'no_data',
                  points: [],
                },
              ],
        },
      })
    }
    if (path === '/api/v1/admin/observability/services') {
      const platformAdmin = roles.some((role) => ['admin', 'system_admin'].includes(role))
      return route.fulfill({
        json: {
          generated_at: new Date().toISOString(),
          scope: platformAdmin ? 'system' : 'agent',
          state: 'available',
          services: (platformAdmin
            ? [
                ['gateway', 'up', 'Gateway is serving the health request'],
                [
                  'user',
                  'up',
                  'Service TCP endpoint is reachable; dependency readiness is not implied',
                ],
                ['problem', 'down', 'Fixed service probe failed'],
              ]
            : [
                ['agent', 'up', 'Service health endpoint is responding'],
                [
                  'rag-worker',
                  'not_configured',
                  'This service does not expose an independent health endpoint',
                ],
              ]
          ).map(([name, state, detail]) => ({ name, state, detail })),
        },
      })
    }
    if (path === '/api/v1/admin/observability/alerts') {
      const platformAdmin = roles.some((role) => ['admin', 'system_admin'].includes(role))
      return route.fulfill({
        json: {
          generated_at: new Date().toISOString(),
          scope: platformAdmin ? 'system' : 'agent',
          state: 'available',
          alerts: platformAdmin
            ? [
                {
                  name: 'OJJudgeQueueLatencyHigh',
                  severity: 'warning',
                  state: 'firing',
                  started_at: new Date().toISOString(),
                  service: 'judge-service',
                  error_code: 'JUDGE_QUEUE_LATENCY_HIGH',
                  trace_available: false,
                },
              ]
            : [],
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

  await expect(page.getByRole('link', { name: '监控', exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { name: '监控总览', exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { name: '全局健康' })).toBeVisible()
  await expect(page.getByText('1.2 请求/秒')).toBeVisible()
  await expect(page.getByRole('heading', { name: '流量与性能趋势' })).toBeVisible()
  await expect(page.getByRole('heading', { name: '服务健康列表' })).toBeVisible()
  await expect(page.getByText('固定探测结果；TCP 可达不代表依赖就绪')).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Prometheus 活跃告警' })).toBeVisible()
  await expect(page.getByText('Judge 队列延迟偏高')).toBeVisible()
  await expect(
    page.locator('.monitoring-alert-preview').getByRole('link', { name: '判题链路 →' }),
  ).toBeVisible()
  await expect(page.getByText('API 延迟 P95')).toBeVisible()
  await expect(page.getByText('0.08 秒')).toBeVisible()
  await expect(page.getByRole('link', { name: 'Agent 运行诊断 →' })).toBeVisible()
  await expect(
    page.locator('.monitoring-module-links').getByRole('link', { name: '判题链路 →' }),
  ).toBeVisible()
  await expect(page.getByRole('link', { name: '外部排障工具' })).toHaveAttribute(
    'href',
    /oj-system-overview/,
  )
  await expect(page.getByRole('link', { name: 'Agent 运行诊断', exact: true })).toBeVisible()
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

test('告警接口失败时展示查询不可用而不是未配置', async ({ page }) => {
  await loginAs(page, ['admin'])
  await page.route('**/api/v1/admin/observability/alerts', (route) => route.abort())
  await page.goto('/monitoring')

  await expect(page.getByText('告警查询暂不可用；监控指标和业务服务不受影响。')).toBeVisible()
  await expect(page.getByText('告警查询不可用')).toBeVisible()
  await expect(page.getByText('此环境没有配置 Prometheus 告警查询。')).toHaveCount(0)
})

test('agent_admin 可查看 Agent 诊断但不能进入系统判题监控', async ({ page }) => {
  await loginAs(page, ['agent_admin'])
  await page.goto('/monitoring')
  await expect(page.getByRole('heading', { name: '监控总览' })).toBeVisible()
  await expect(page.getByText('API 请求速率', { exact: true })).toHaveCount(0)
  await expect(page.getByText('Agent Run 启动速率').first()).toBeVisible()
  await expect(page.getByRole('link', { name: 'Agent 运行诊断', exact: true })).toBeVisible()
  await expect(page.getByRole('link', { name: '判题链路', exact: true })).toHaveCount(0)

  await page.goto('/monitoring/judge')
  await expect(page).toHaveURL(/\/problems$/)
})

test('system_admin 可进入判题链路监控', async ({ page }) => {
  await loginAs(page, ['system_admin'])
  await page.goto('/monitoring/judge')
  await expect(page.getByRole('heading', { name: '判题链路' })).toBeVisible()
})

test('指标页区分无样本与零值，并按 Agent 管理员范围显示指标', async ({ page }) => {
  await loginAs(page, ['system_admin'])
  await page.goto('/monitoring/metrics')
  await expect(page.getByRole('heading', { name: '指标总览' })).toBeVisible()
  await expect(page.getByText('指标源可用')).toBeVisible()
  await expect(page.getByRole('heading', { name: 'HTTP 请求速率' })).toBeVisible()
  await expect(page.getByText('暂无样本；可能没有流量或该指标暂未导出。').first()).toBeVisible()
  await expect(page.getByRole('link', { name: '打开 Grafana' })).toHaveAttribute(
    'href',
    /oj-system-overview/,
  )
  await expect(page.getByText('Contest 独立指标端点')).toBeVisible()

  await loginAs(page, ['agent_admin'])
  await page.goto('/monitoring/metrics')
  await expect(page.getByText('仅展示 Agent Run、模型和后台任务的固定摘要')).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Agent 在途 Run' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'HTTP 请求速率' })).toHaveCount(0)
  await expect(page.getByText('Contest 独立指标端点')).toHaveCount(0)
  await expect(page.getByRole('link', { name: '打开 Grafana' })).toHaveCount(0)
})

test('普通用户不能打开监控指标页', async ({ page }) => {
  await loginAs(page, ['user'])
  await page.goto('/monitoring/metrics')
  await expect(page).toHaveURL(/\/problems$/)
})

test('日志中心带入 Trace 并限制 Agent 服务选项', async ({ page }) => {
  await loginAs(page, ['agent_admin'])
  const trace = '4bf92f3577b34da6a3ce929d0e0e4736'
  await page.route('**/api/v1/admin/observability/logs?**', async (route) => {
    expect(new URL(route.request().url()).searchParams.get('trace_id')).toBe(trace)
    await route.fulfill({
      json: { generated_at: new Date().toISOString(), scope: 'agent', state: 'no_data', logs: [] },
    })
  })
  await page.goto('/monitoring/logs?trace_id=' + trace)
  await expect(page.getByRole('heading', { name: '日志中心' })).toBeVisible()
  await expect(page.getByPlaceholder('32 位 Trace ID')).toHaveValue(trace)
  await expect(page.getByText('当前条件没有日志。')).toBeVisible()
  await expect(page.locator('option[value="gateway-service"]')).toHaveCount(0)
})
