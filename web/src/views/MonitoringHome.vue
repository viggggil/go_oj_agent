<template>
  <section class="monitoring-page monitoring-dashboard">
    <header class="page-header monitoring-dashboard-header">
      <div>
        <p class="eyebrow">运行总览 / OBSERVABILITY</p>
        <h1>监控总览</h1>
        <p>{{ scopeDescription }}</p>
      </div>
      <div class="metrics-actions">
        <label>
          时间范围
          <select v-model="window" :disabled="loading" @change="load">
            <option value="15m">最近 15 分钟</option>
            <option value="1h">最近 1 小时</option>
            <option value="24h">最近 24 小时</option>
          </select>
        </label>
        <button class="button secondary" type="button" :disabled="loading" @click="load">
          {{ loading ? '刷新中…' : '刷新' }}
        </button>
        <a
          v-if="overview?.grafana_url"
          class="button secondary"
          :href="overview.grafana_url"
          target="_blank"
          rel="noopener noreferrer"
          >外部排障工具</a
        >
      </div>
    </header>

    <p v-if="requestError" class="error" role="alert">{{ requestError }}</p>

    <div v-if="overview" class="monitoring-dashboard-content">
      <div class="monitoring-overview-state" :class="'state-' + overview.state" role="status">
        <span class="monitoring-state-dot" aria-hidden="true"></span>
        <strong>{{ stateLabel(overview.state) }}</strong>
        <span>{{ stateDescription(overview.state) }}</span>
        <small>更新于 {{ formatTime(overview.generated_at) }} · 每 30 秒自动刷新</small>
      </div>

      <section class="monitoring-section" aria-labelledby="health-title">
        <div class="monitoring-section-heading">
          <div>
            <p class="eyebrow">HEALTH SNAPSHOT</p>
            <h2 id="health-title">全局健康</h2>
          </div>
          <span class="monitoring-muted">当前范围：{{ windowLabel }}</span>
        </div>
        <div v-if="isPlatformAdmin" class="monitoring-kpi-grid">
          <article class="monitoring-kpi">
            <span>API 请求速率</span>
            <strong>{{ metricValue('request_rate', '请求/秒') }}</strong>
            <small>按服务聚合的 HTTP 请求速率</small>
          </article>
          <article class="monitoring-kpi">
            <span>API 延迟 P95</span>
            <strong>{{ metricValue('api_latency_p95', '秒') }}</strong>
            <small>按服务聚合的 HTTP 延迟分位数</small>
          </article>
          <article class="monitoring-kpi" :class="errorTone">
            <span>HTTP 5xx 错误率</span>
            <strong>{{ errorRate }}</strong>
            <small>由当前请求速率与 5xx 速率计算</small>
          </article>
          <article class="monitoring-kpi">
            <span>Judge 排队 P95</span>
            <strong>{{ metricValue('judge_queue_p95', '秒') }}</strong>
            <small>队列等待时间，不代表完整判题耗时</small>
          </article>
          <article class="monitoring-kpi">
            <span>Agent 模型失败速率</span>
            <strong>{{ metricValue('agent_model_failure_rate', '次/秒') }}</strong>
            <small>Agent 指标摘要</small>
          </article>
          <article class="monitoring-kpi">
            <span>Agent Run 成功率</span>
            <strong>{{ ratioValue('agent_run_success_ratio') }}</strong>
            <small>已完成 terminal Run / 全部 terminal Run</small>
          </article>
        </div>
        <div v-else class="monitoring-kpi-grid monitoring-agent-kpi-grid">
          <article class="monitoring-kpi">
            <span>Agent Run 启动速率</span
            ><strong>{{ metricValue('agent_run_rate', '次/秒') }}</strong
            ><small>Run 创建速率</small>
          </article>
          <article class="monitoring-kpi">
            <span>Agent 在途 Run</span
            ><strong>{{ metricValue('agent_run_in_flight', '个') }}</strong
            ><small>当前进程观测值</small>
          </article>
          <article class="monitoring-kpi kpi-warning">
            <span>模型失败速率</span
            ><strong>{{ metricValue('agent_model_failure_rate', '次/秒') }}</strong
            ><small>受控模型错误与超时</small>
          </article>
          <article class="monitoring-kpi">
            <span>Tool 失败速率</span
            ><strong>{{ metricValue('agent_tool_failure_rate', '次/秒') }}</strong
            ><small>Agent 工具操作失败与超时</small>
          </article>
        </div>
        <p class="monitoring-capability-note">
          活跃告警和日志事件将在 Loki / Alertmanager 接入后展示；空指标不会被推断为零。
        </p>
      </section>

      <section class="monitoring-section monitoring-dashboard-columns">
        <article class="monitoring-panel monitoring-trend-panel">
          <div class="monitoring-section-heading">
            <div>
              <p class="eyebrow">TRAFFIC & PERFORMANCE</p>
              <h2>流量与性能趋势</h2>
            </div>
            <label class="monitoring-filter">
              指标
              <select v-model="selectedTrend">
                <option v-for="option in trendOptions" :key="option.key" :value="option.key">
                  {{ option.label }}
                </option>
              </select>
            </label>
          </div>
          <div class="monitoring-chart-legend">
            <label
              class="monitoring-filter"
              v-if="selectedTrend === 'request_rate' || selectedTrend === 'request_error_rate'"
            >
              服务
              <select v-model="selectedService">
                <option value="all">所有服务</option>
                <option v-for="service in availableServices" :key="service" :value="service">
                  {{ serviceLabel(service) }}
                </option>
              </select>
            </label>
            <span class="monitoring-muted">{{ trendLabel }}</span>
          </div>
          <div
            v-if="trendSeries.length"
            class="monitoring-chart"
            role="img"
            :aria-label="`${trendLabel}趋势图`"
          >
            <div class="monitoring-chart-grid" aria-hidden="true"><i></i><i></i><i></i><i></i></div>
            <svg viewBox="0 0 720 220" preserveAspectRatio="none" aria-hidden="true">
              <polyline
                v-for="(series, index) in trendSeries"
                :key="`${series.key}:${series.service || 'all'}`"
                :points="
                  sparkline(
                    series.points.map((point) => point.value),
                    720,
                    205,
                  )
                "
                :class="`chart-line chart-line-${index % 5}`"
              />
            </svg>
          </div>
          <div v-else class="monitoring-chart-empty">
            <strong>{{ trendEmptyTitle }}</strong>
            <span>{{ trendEmptyDescription }}</span>
          </div>
          <div v-if="trendSeries.length" class="monitoring-series-legend">
            <span
              v-for="(series, index) in trendSeries"
              :key="`${series.key}:${series.service || 'all'}`"
            >
              <i :class="`legend-dot chart-line-${index % 5}`"></i
              >{{ series.service ? serviceLabel(series.service) : series.label }}
            </span>
          </div>
          <footer class="monitoring-chart-footer">
            <span>{{ chartStart }}</span
            ><span>{{ chartEnd }}</span>
          </footer>
        </article>

        <article class="monitoring-panel monitoring-business-panel">
          <div class="monitoring-section-heading">
            <div>
              <p class="eyebrow">BUSINESS OPERATIONS</p>
              <h2>业务运行状态</h2>
            </div>
          </div>
          <div class="monitoring-business-list">
            <div v-for="item in businessMetrics" :key="item.key" class="monitoring-business-row">
              <span
                ><i class="monitoring-business-icon" :class="item.tone">{{ item.icon }}</i
                >{{ item.label }}</span
              >
              <strong>{{ metricValue(item.key) }}</strong>
            </div>
          </div>
          <p class="monitoring-capability-note">
            Agent TTFT、Token 用量及完整判题阶段耗时暂未纳入现有固定查询。
          </p>
          <div class="monitoring-module-links">
            <RouterLink class="button secondary" to="/monitoring/agent"
              >Agent 运行诊断 →</RouterLink
            >
            <RouterLink v-if="isPlatformAdmin" class="button secondary" to="/monitoring/judge"
              >判题链路 →</RouterLink
            >
          </div>
        </article>
      </section>

      <section class="monitoring-section monitoring-panel" aria-labelledby="services-title">
        <div class="monitoring-section-heading">
          <div>
            <p class="eyebrow">SERVICE SIGNALS</p>
            <h2 id="services-title">服务健康列表</h2>
          </div>
          <span class="monitoring-muted">固定探测结果；TCP 可达不代表依赖就绪</span>
        </div>
        <div class="monitoring-service-grid">
          <article v-for="service in serviceRows" :key="service.key" class="monitoring-service-row">
            <span class="monitoring-service-indicator" :class="'signal-' + service.state"></span>
            <div>
              <strong>{{ service.label }}</strong
              ><small>{{ service.detail }}</small>
            </div>
            <span class="monitoring-service-state" :class="'signal-text-' + service.state">{{
              service.stateLabel
            }}</span>
          </article>
        </div>
      </section>

      <section class="monitoring-section monitoring-panel monitoring-alert-preview">
        <div>
          <p class="eyebrow">RECENT EVENTS</p>
          <h2>告警与异常</h2>
          <p>
            当前版本没有活跃告警与日志事件查询接口。Prometheus
            告警规则在采集端评估，告警摘要将在后续接入。
          </p>
        </div>
        <span class="monitoring-status neutral">告警接口待接入</span>
      </section>

      <section
        class="monitoring-section monitoring-target-section"
        aria-label="Prometheus 抓取目标"
      >
        <div class="monitoring-section-heading">
          <div>
            <p class="eyebrow">PROMETHEUS SCRAPES</p>
            <h2>实际采集目标</h2>
          </div>
          <RouterLink to="/monitoring/metrics" class="monitoring-inline-link"
            >查看完整指标 →</RouterLink
          >
        </div>
        <div class="monitoring-target-grid">
          <article
            v-for="target in overview.targets"
            :key="target.name"
            class="monitoring-target-chip"
            :class="'target-' + target.state"
          >
            <span>{{ targetLabel(target.name) }}</span>
            <strong>{{ targetStateLabel(target.state) }}</strong>
            <small>{{
              target.last_scrape_at ? formatTime(target.last_scrape_at) : '尚无抓取样本'
            }}</small>
          </article>
        </div>
      </section>

      <footer class="monitoring-dashboard-footer">
        <span>数据更新时间 {{ formatTime(overview.generated_at) }} · 页面隐藏时暂停自动刷新</span>
        <RouterLink to="/monitoring/metrics">固定指标详情</RouterLink>
        <a
          v-if="overview.grafana_url"
          :href="overview.grafana_url"
          target="_blank"
          rel="noopener noreferrer"
          >Grafana 外部排障</a
        >
      </footer>
    </div>
    <div v-else-if="loading" class="monitoring-dashboard-loading" role="status">
      正在加载监控摘要…
    </div>
    <div v-else-if="!requestError" class="monitoring-dashboard-loading">等待指标数据</div>
  </section>
</template>

<script setup lang="ts">
  import { computed, onMounted, onUnmounted, ref } from 'vue'
  import { RouterLink } from 'vue-router'
  import { observabilityApi } from '../api'
  import type { MetricsOverview, MetricsWindow, ObservabilityServices } from '../api'
  import { useAuthStore } from '../stores/auth'

  const auth = useAuthStore()
  const isPlatformAdmin = computed(() =>
    auth.user?.roles.some((role) => ['admin', 'system_admin'].includes(role)),
  )
  const scopeDescription = computed(() =>
    isPlatformAdmin.value
      ? '统一查看系统流量、判题运行与 Agent 指标；业务诊断入口按角色开放。'
      : '仅展示 Agent 运行与采集摘要；平台和判题指标不会返回给当前角色。',
  )
  const window = ref<MetricsWindow>('1h')
  const overview = ref<MetricsOverview>()
  const serviceHealth = ref<ObservabilityServices>()
  const loading = ref(false)
  const requestError = ref('')
  const selectedTrend = ref('request_rate')
  const selectedService = ref('all')
  let timer: ReturnType<typeof setInterval> | undefined
  let controller: AbortController | undefined

  const windowLabel = computed(
    () => ({ '15m': '15 分钟', '1h': '1 小时', '24h': '24 小时' })[window.value],
  )
  const trendOptions = computed(() =>
    isPlatformAdmin.value
      ? [
          { key: 'request_rate', label: 'API 请求速率' },
          { key: 'request_error_rate', label: 'HTTP 5xx 速率' },
          { key: 'judge_queue_p95', label: 'Judge 排队 P95' },
        ]
      : [
          { key: 'agent_run_rate', label: 'Agent Run 启动速率' },
          { key: 'agent_model_failure_rate', label: '模型失败速率' },
          { key: 'agent_tool_failure_rate', label: 'Tool 失败速率' },
        ],
  )
  const trendLabel = computed(
    () => trendOptions.value.find((item) => item.key === selectedTrend.value)?.label || '指标趋势',
  )
  const allSeries = computed(() => overview.value?.series || [])
  const availableServices = computed(() =>
    [
      ...new Set(
        allSeries.value
          .filter((series) => series.key === selectedTrend.value && series.service)
          .map((series) => series.service as string),
      ),
    ].sort(),
  )
  const trendSeries = computed(() =>
    allSeries.value.filter((series) => {
      if (
        series.key !== selectedTrend.value ||
        series.state !== 'available' ||
        !series.points.length
      )
        return false
      if (
        (selectedTrend.value === 'request_rate' || selectedTrend.value === 'request_error_rate') &&
        selectedService.value !== 'all'
      ) {
        return series.service === selectedService.value
      }
      return true
    }),
  )
  const businessMetrics = computed(() =>
    isPlatformAdmin.value
      ? [
          { key: 'judge_submission_rate', label: 'Judge 提交速率', icon: 'J', tone: 'blue' },
          { key: 'judge_queue_p95', label: '排队 P95', icon: 'Q', tone: 'amber' },
          { key: 'judge_worker_in_flight', label: 'Worker 在途', icon: 'W', tone: 'green' },
          {
            key: 'contest_cache_outbox_pending',
            label: 'Contest Outbox 积压',
            icon: 'C',
            tone: 'purple',
          },
          { key: 'agent_run_rate', label: 'Agent Run 启动速率', icon: 'A', tone: 'blue' },
          { key: 'agent_model_failure_rate', label: '模型失败速率', icon: '!', tone: 'red' },
        ]
      : [
          { key: 'agent_run_rate', label: 'Agent Run 启动速率', icon: 'A', tone: 'blue' },
          { key: 'agent_run_in_flight', label: 'Agent 在途 Run', icon: 'R', tone: 'green' },
          { key: 'agent_model_failure_rate', label: '模型失败速率', icon: '!', tone: 'red' },
          { key: 'agent_tool_failure_rate', label: 'Tool 失败速率', icon: 'T', tone: 'amber' },
          { key: 'agent_rag_failure_rate', label: 'RAG 失败速率', icon: 'K', tone: 'purple' },
          { key: 'agent_job_failure_rate', label: '后台任务失败速率', icon: 'B', tone: 'red' },
        ],
  )
  const requestRate = computed(() => sumLatest('request_rate'))
  const requestErrorRateValue = computed(() => sumLatest('request_error_rate'))
  const errorRate = computed(() =>
    requestRate.value === undefined || requestErrorRateValue.value === undefined
      ? '未接入'
      : `${((requestErrorRateValue.value / Math.max(requestRate.value, 0.000001)) * 100).toFixed(2)}%`,
  )
  const errorTone = computed(() =>
    requestErrorRateValue.value === undefined
      ? ''
      : requestErrorRateValue.value > 0
        ? 'kpi-warning'
        : 'kpi-good',
  )
  function ratioValue(key: string) {
    const value = sumLatest(key)
    return value === undefined ? '暂无数据' : `${(value * 100).toFixed(1)}%`
  }
  const chartStart = computed(() => {
    const first = trendSeries.value[0]?.points[0]?.timestamp
    return first ? formatTime(first) : '无样本'
  })
  const chartEnd = computed(() => {
    const last = trendSeries.value[0]?.points.at(-1)?.timestamp
    return last ? formatTime(last) : '无样本'
  })
  const trendEmptyTitle = computed(() =>
    overview.value?.state === 'unavailable' ? '指标服务暂不可用' : '当前范围没有可绘制的样本',
  )
  const trendEmptyDescription = computed(() =>
    overview.value?.state === 'not_configured'
      ? '此环境没有启用可选 Prometheus 观测栈。'
      : '可能尚无流量，或该指标当前未导出。页面不会将空结果显示为零。',
  )

  const serviceNames = [
    ['gateway-service', 'Gateway'],
    ['user-service', 'User'],
    ['problem-service', 'Problem'],
    ['judge-service', 'Judge'],
    ['contest-service', 'Contest'],
    ['agent-service', 'Agent'],
    ['judge-worker', 'Judge Worker'],
    ['agent-rag-worker', 'RAG Worker'],
  ] as const
  const serviceRows = computed(() =>
    serviceNames
      .filter(
        ([key]) => isPlatformAdmin.value || key === 'agent-service' || key === 'agent-rag-worker',
      )
      .map(([key, label]) => {
        const serviceName = key.replace('-service', '').replace('agent-rag-worker', 'rag-worker')
        const health = serviceHealth.value?.services.find((service) => service.name === serviceName)
        if (health)
          return {
            key,
            label,
            state: health.state === 'up' ? 'available' : health.state,
            stateLabel:
              health.state === 'up'
                ? '健康探测正常'
                : health.state === 'not_configured'
                  ? '健康探测未接入'
                  : health.state,
            detail: health.detail,
          }
        const hasMetric = allSeries.value.some(
          (series) =>
            series.service === key && series.state === 'available' && series.points.length,
        )
        const directTarget = overview.value?.targets.find(
          (target) => target.name.replace(/_metrics$/, '-service') === key,
        )
        if (directTarget)
          return {
            key,
            label,
            state: directTarget.state === 'up' ? 'available' : directTarget.state,
            stateLabel: targetStateLabel(directTarget.state),
            detail: 'Prometheus metrics scrape target',
          }
        if (hasMetric)
          return {
            key,
            label,
            state: 'available',
            stateLabel: '有指标样本',
            detail: '存在固定查询指标；不代表健康探测通过',
          }
        return {
          key,
          label,
          state: 'unknown',
          stateLabel: '健康探测未接入',
          detail: '当前没有独立 health probe / scrape target',
        }
      }),
  )

  function sumLatest(key: string): number | undefined {
    const matches = allSeries.value.filter(
      (series) => series.key === key && series.state === 'available' && series.points.length,
    )
    if (!matches.length) return undefined
    return matches.reduce((total, series) => total + (series.points.at(-1)?.value || 0), 0)
  }
  function metricValue(key: string, unit?: string) {
    const value = sumLatest(key)
    if (value === undefined) return '暂无数据'
    return `${formatValue(value)}${unit ? ` ${unit}` : ''}`
  }
  function stateLabel(state: MetricsOverview['state']) {
    return {
      available: '指标源可用',
      no_data: '当前范围暂无样本',
      stale: '部分采集目标异常或数据过期',
      unavailable: 'Prometheus 暂不可用',
      not_configured: '未启用 Prometheus 查询',
    }[state]
  }
  function stateDescription(state: MetricsOverview['state']) {
    return {
      available: '展示服务端固定查询返回的指标；具体服务健康仍以服务探测为准。',
      no_data: '采集端可访问，但所选范围没有指标样本。',
      stale: '采集目标不健康或样本过期，请检查采集状态。',
      unavailable: '查询暂时失败；业务服务不受影响。',
      not_configured: '本环境未启用可选观测栈，业务服务仍可正常运行。',
    }[state]
  }
  function targetLabel(name: string) {
    return (
      {
        collector_exporter: 'Collector Metrics Exporter',
        collector_self_metrics: 'Collector 自身指标',
        contest_metrics: 'Contest Metrics',
      }[name] || '未知采集目标'
    )
  }
  function targetStateLabel(state: MetricsOverview['targets'][number]['state']) {
    return { up: '采集正常', down: '采集失败', stale: '数据陈旧', unknown: '未知' }[state]
  }
  function serviceLabel(value: string) {
    return (
      {
        'gateway-service': 'Gateway',
        'judge-service': 'Judge',
        'judge-worker': 'Judge Worker',
        'problem-service': 'Problem',
        'contest-service': 'Contest',
        'agent-service': 'Agent',
        'agent-rag-worker': 'RAG Worker',
        other: '其他服务',
      }[value] || '其他服务'
    )
  }
  function formatValue(value: number) {
    return new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 3 }).format(value)
  }
  function formatTime(value: string) {
    const date = new Date(value)
    return Number.isNaN(date.getTime()) ? '未知' : date.toLocaleString()
  }
  function sparkline(values: number[], width = 360, height = 72) {
    if (values.length < 2) return `0,${height / 2} ${width},${height / 2}`
    const low = Math.min(...values)
    const high = Math.max(...values)
    const span = high - low || 1
    return values
      .map((value, index) => {
        const x = (index / (values.length - 1)) * width
        const y = height - 8 - ((value - low) / span) * (height - 16)
        return `${x.toFixed(1)},${y.toFixed(1)}`
      })
      .join(' ')
  }
  async function load() {
    if (loading.value || document.visibilityState === 'hidden') return
    controller?.abort()
    controller = new AbortController()
    loading.value = true
    requestError.value = ''
    try {
      const [metricsResult, servicesResult] = await Promise.allSettled([
        observabilityApi.metrics(window.value, controller.signal),
        observabilityApi.services(controller.signal),
      ])
      if (metricsResult.status === 'rejected') throw metricsResult.reason
      overview.value = metricsResult.value.data
      serviceHealth.value =
        servicesResult.status === 'fulfilled' ? servicesResult.value.data : undefined
      if (!trendOptions.value.some((item) => item.key === selectedTrend.value))
        selectedTrend.value = trendOptions.value[0].key
      if (
        selectedService.value !== 'all' &&
        !availableServices.value.includes(selectedService.value)
      )
        selectedService.value = 'all'
    } catch (error) {
      if (controller.signal.aborted) return
      const status = (error as { response?: { status?: number } }).response?.status
      requestError.value =
        status === 403 ? '当前账号无权访问这组指标。' : '指标服务暂不可用，请稍后重试。'
    } finally {
      if (!controller.signal.aborted) loading.value = false
    }
  }
  function onVisibilityChange() {
    if (document.visibilityState === 'hidden') {
      controller?.abort()
      loading.value = false
      return
    }
    void load()
  }
  onMounted(() => {
    void load()
    document.addEventListener('visibilitychange', onVisibilityChange)
    timer = setInterval(() => void load(), 30_000)
  })
  onUnmounted(() => {
    controller?.abort()
    document.removeEventListener('visibilitychange', onVisibilityChange)
    if (timer) clearInterval(timer)
  })
</script>
