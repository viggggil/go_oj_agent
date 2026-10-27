<template>
  <section class="monitoring-page metrics-page">
    <header class="page-header">
      <div>
        <p class="eyebrow">Prometheus Metrics</p>
        <h1>指标总览</h1>
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
          {{ loading ? '查询中…' : '刷新' }}
        </button>
        <a
          v-if="overview?.grafana_url"
          class="button secondary"
          :href="overview.grafana_url"
          target="_blank"
          rel="noopener noreferrer"
          >打开 Grafana</a
        >
      </div>
    </header>

    <p v-if="requestError" class="error" role="alert">{{ requestError }}</p>
    <p v-if="overview" class="metrics-state" :class="'state-' + overview.state" role="status">
      <strong>{{ stateLabel(overview.state) }}</strong>
      <span>{{ stateDescription(overview.state, overview.error_code) }}</span>
      <small
        >查询时间 {{ formatTime(overview.generated_at) }} · 每 30 秒刷新（页面隐藏时暂停）</small
      >
    </p>

    <section v-if="overview?.targets.length" class="metrics-targets" aria-label="采集目标">
      <h2>采集目标</h2>
      <ul>
        <li v-for="target in overview.targets" :key="target.name" :class="'target-' + target.state">
          <span>{{ targetLabel(target.name) }}</span>
          <strong>{{ targetStateLabel(target.state) }}</strong>
          <small>{{
            target.last_scrape_at ? formatTime(target.last_scrape_at) : '尚无抓取样本'
          }}</small>
        </li>
      </ul>
    </section>

    <section v-if="overview" class="metrics-cards" aria-label="Prometheus 指标">
      <article
        v-for="series in overview.series"
        :key="`${series.key}:${series.service || 'all'}`"
        class="metrics-card"
      >
        <div class="metrics-card-heading">
          <div>
            <h2>{{ series.label }}</h2>
            <small v-if="series.service">{{ serviceLabel(series.service) }}</small>
          </div>
          <span class="metrics-unit">{{ series.unit }}</span>
        </div>
        <template v-if="series.state === 'available' && series.points.length">
          <strong class="metrics-value">{{ formatValue(latestValue(series)) }}</strong>
          <svg
            class="metrics-sparkline"
            viewBox="0 0 360 72"
            role="img"
            :aria-label="`${series.label} 最近变化`"
          >
            <polyline :points="sparkline(series.points.map((point) => point.value))" />
          </svg>
          <small class="metrics-observed">最近样本 {{ formatTime(latestTimestamp(series)) }}</small>
        </template>
        <p v-else class="metrics-no-data">暂无样本；可能没有流量或该指标暂未导出。</p>
      </article>
    </section>
    <p v-else-if="loading" class="empty">正在读取固定指标查询…</p>
  </section>
</template>

<script setup lang="ts">
  import { computed, onMounted, onUnmounted, ref } from 'vue'
  import { observabilityApi } from '../api'
  import type { MetricsOverview, MetricsWindow } from '../api'
  import { useAuthStore } from '../stores/auth'

  const auth = useAuthStore()
  const isPlatformAdmin = computed(() =>
    auth.user?.roles.some((role) => ['admin', 'system_admin'].includes(role)),
  )
  const scopeDescription = computed(() =>
    isPlatformAdmin.value
      ? '固定查询展示 HTTP、判题、Contest、Agent 与 Collector 摘要；查询不接受 PromQL。'
      : '仅展示 Agent Run、模型和后台任务的固定摘要；不返回判题及平台指标。',
  )
  const window = ref<MetricsWindow>('1h')
  const overview = ref<MetricsOverview>()
  const loading = ref(false)
  const requestError = ref('')
  let timer: ReturnType<typeof setInterval> | undefined
  let controller: AbortController | undefined

  async function load() {
    if (loading.value || document.visibilityState === 'hidden') return
    controller?.abort()
    controller = new AbortController()
    loading.value = true
    requestError.value = ''
    try {
      overview.value = (await observabilityApi.metrics(window.value, controller.signal)).data
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

  function stateLabel(state: MetricsOverview['state']) {
    return {
      available: '指标源可用',
      no_data: '当前没有指标样本',
      stale: '采集目标异常或数据可能过期',
      unavailable: 'Prometheus 暂不可用',
      not_configured: '本环境未启用 Prometheus 查询',
    }[state]
  }

  function stateDescription(state: MetricsOverview['state'], code?: string) {
    if (state === 'no_data') return '采集端可访问，但所选范围没有已导出的指标样本。'
    if (state === 'stale') return `请检查采集目标状态。${code ? `状态码：${code}` : ''}`
    if (state === 'unavailable') return `Prometheus 查询失败。${code ? `状态码：${code}` : ''}`
    if (state === 'not_configured') return '启动可选 observability overlay 后可启用此查询。'
    return '样本存在；单个指标没有样本时仍会单独标记，不会按 0 展示。'
  }

  function targetLabel(name: string) {
    return (
      {
        collector_exporter: 'Collector 指标导出端点',
        collector_self_metrics: 'Collector 自身指标',
        contest_metrics: 'Contest 独立指标端点',
      }[name] || '未知采集目标'
    )
  }

  function targetStateLabel(state: MetricsOverview['targets'][number]['state']) {
    return { up: '正常', down: '不可达', stale: '数据陈旧', unknown: '未知' }[state]
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
        'agent-rag-worker': 'Agent RAG Worker',
        other: '其他服务',
      }[value] || '其他服务'
    )
  }

  function latestValue(series: MetricsOverview['series'][number]) {
    return series.points[series.points.length - 1]?.value ?? 0
  }

  function latestTimestamp(series: MetricsOverview['series'][number]) {
    return series.points[series.points.length - 1]?.timestamp || ''
  }

  function formatValue(value: number) {
    return new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 3 }).format(value)
  }

  function formatTime(value: string) {
    const date = new Date(value)
    return Number.isNaN(date.getTime()) ? '未知' : date.toLocaleString()
  }

  function sparkline(values: number[]) {
    if (values.length < 2) return '0,36 360,36'
    const low = Math.min(...values)
    const high = Math.max(...values)
    const span = high - low || 1
    return values
      .map((value, index) => {
        const x = (index / (values.length - 1)) * 360
        const y = 66 - ((value - low) / span) * 60
        return `${x.toFixed(1)},${y.toFixed(1)}`
      })
      .join(' ')
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
