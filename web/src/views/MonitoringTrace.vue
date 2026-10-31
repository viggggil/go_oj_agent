<template>
  <section class="monitoring-page trace-detail-page">
    <header class="page-header">
      <div>
        <p class="eyebrow">TRACE DETAIL</p>
        <h1>链路详情</h1>
        <p>
          <code>{{ traceId }}</code>
        </p>
      </div>
      <button class="button secondary" :disabled="loading" @click="load">刷新</button>
    </header>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <p v-if="detail" class="freshness" role="status">
      {{ stateLabel(detail.state) }} · {{ detail.spans.length }} 个 Span
    </p>
    <p v-if="loading" class="empty">正在读取 Trace…</p>
    <p v-else-if="detail?.state !== 'available'" class="empty">
      当前 Trace 没有可展示的 Span，可能尚未采样、已过留存期或观测后端不可用。
    </p>
    <TraceTimeline
      v-else
      :detail="detail"
      :selected-span-id="selectedSpanId"
      @select="selectedSpanId = $event"
    />
    <section class="trace-logs-section">
      <h2>所选阶段日志</h2>
      <MonitoringLogs
        :trace-id="traceId"
        :span-id="selectedSpanId || undefined"
        :start="logStart"
        :end="logEnd"
        :hide-filters="true"
        embedded
      />
    </section>
  </section>
</template>
<script setup lang="ts">
  import { computed, onMounted, ref } from 'vue'
  import { useRoute } from 'vue-router'
  import { apiErrorMessage, observabilityApi, type TraceDetail } from '../api'
  import TraceTimeline from '../components/observability/TraceTimeline.vue'
  import MonitoringLogs from './MonitoringLogs.vue'
  const route = useRoute()
  const traceId = computed(() => String(route.params.trace_id || ''))
  const detail = ref<TraceDetail>()
  const loading = ref(false)
  const error = ref('')
  const selectedSpanId = ref('')
  const logStart = computed(() => {
    const times =
      detail.value?.spans.map((span) => Date.parse(span.started_at)).filter(Number.isFinite) || []
    return times.length
      ? new Date(Math.max(0, Math.min(...times) - 5 * 60_000)).toISOString()
      : undefined
  })
  const logEnd = computed(() => {
    const spans = detail.value?.spans || []
    if (!spans.length) return undefined
    const end =
      Math.max(
        ...spans
          .map((span) => Date.parse(span.started_at) + span.duration_ms)
          .filter(Number.isFinite),
      ) +
      5 * 60_000
    return new Date(end).toISOString()
  })
  async function load() {
    loading.value = true
    error.value = ''
    try {
      detail.value = (await observabilityApi.trace(traceId.value)).data
      selectedSpanId.value =
        detail.value.spans.find((span) => span.name === 'agent.run')?.span_id ||
        detail.value.spans[0]?.span_id ||
        ''
    } catch (cause) {
      error.value = apiErrorMessage(cause, '查询 Trace')
    } finally {
      loading.value = false
    }
  }
  function spanName(value: string) {
    return (
      (
        {
          'gateway.http': 'Gateway HTTP 请求',
          'agent.run': 'Agent 运行',
          'agent.tool': 'Agent 工具调用',
          'agent.rag.retrieve': 'Agent 知识检索',
          'agent.grpc.client': 'Agent 业务 RPC',
          'judge.submission.create': '创建判题提交',
          'judge.outbox.publish': '发布判题任务',
          'judge.worker.task': 'Worker 执行判题任务',
          'judge.worker.result.publish': '发布判题结果',
          'judge.worker.retry.publish': '发布判题重试',
          'judge.result.consume': '消费判题结果',
          'judge.result.apply': '应用判题结果',
          'contest.result.consume': 'Contest 消费判题结果',
          'contest.projection.apply': '更新 Contest 投影',
          'contest.cache_outbox.apply': '应用排行榜缓存',
        } as Record<string, string>
      )[value] || '观测阶段'
    )
  }
  function serviceName(value: string) {
    return (
      (
        {
          'gateway-service': 'Gateway',
          'judge-service': '判题服务',
          'judge-worker': '判题 Worker',
          'contest-service': '比赛服务',
          'agent-service': 'Agent 服务',
          'agent-rag-worker': 'Agent RAG Worker',
          'problem-service': '题目服务',
        } as Record<string, string>
      )[value] || value
    )
  }
  function format(value: string) {
    return new Date(value).toLocaleString()
  }
  function stateLabel(state: TraceDetail['state']) {
    return {
      available: 'Trace 可用',
      no_data: '无 Trace 数据',
      not_configured: 'Trace 后端未配置',
      unavailable: 'Trace 后端暂不可用',
    }[state]
  }
  onMounted(load)
</script>
