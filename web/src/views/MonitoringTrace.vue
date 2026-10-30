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
    <section v-else class="trace-layout">
      <article
        v-for="span in detail.spans"
        :key="span.span_id"
        class="trace-span-card"
        :style="{ marginLeft: `${depth(span)}rem` }"
      >
        <div class="trace-span-heading">
          <strong>{{ span.name }}</strong
          ><span>{{ span.service_name }}</span
          ><small>{{ span.duration_ms.toFixed(2) }} ms</small>
        </div>
        <div class="trace-span-meta">
          <span>{{ format(span.started_at) }}</span
          ><span>{{ span.kind }} · {{ span.status }}</span
          ><code>{{ span.span_id }}</code>
        </div>
        <dl v-if="Object.keys(span.attributes).length" class="trace-attributes">
          <template v-for="(value, key) in span.attributes" :key="key"
            ><dt>{{ key }}</dt>
            <dd>{{ value }}</dd></template
          >
        </dl>
      </article>
    </section>
    <section class="trace-logs-section">
      <h2>关联日志</h2>
      <MonitoringLogs :trace-id="traceId" :start="logStart" :end="logEnd" embedded />
    </section>
  </section>
</template>
<script setup lang="ts">
  import { computed, onMounted, ref } from 'vue'
  import { useRoute } from 'vue-router'
  import { apiErrorMessage, observabilityApi, type TraceDetail } from '../api'
  import MonitoringLogs from './MonitoringLogs.vue'
  const route = useRoute()
  const traceId = computed(() => String(route.params.trace_id || ''))
  const detail = ref<TraceDetail>()
  const loading = ref(false)
  const error = ref('')
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
    } catch (cause) {
      error.value = apiErrorMessage(cause, '查询 Trace')
    } finally {
      loading.value = false
    }
  }
  function depth(span: TraceDetail['spans'][number]) {
    let depth = 0
    let parent = span.parent_span_id
    const spans = detail.value?.spans || []
    while (parent && depth < 8) {
      const found = spans.find((item) => item.span_id === parent)
      if (!found) break
      depth++
      parent = found.parent_span_id
    }
    return depth
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
