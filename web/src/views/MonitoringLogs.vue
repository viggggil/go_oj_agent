<template>
  <section class="monitoring-page logs-page" :class="{ embedded }">
    <header class="page-header">
      <div>
        <p class="eyebrow">Loki observability</p>
        <h1>日志中心</h1>
        <p>按服务、级别、错误码和 Trace ID 检索受控结构化日志。</p>
      </div>
      <button class="button secondary" :disabled="loading" @click="load">刷新</button>
    </header>
    <form class="logs-filters" @submit.prevent="load">
      <label
        >服务<select v-model="filters.service">
          <option value="">全部</option>
          <option v-for="item in services" :key="item" :value="item">{{ item }}</option>
        </select></label
      >
      <label
        >级别<select v-model="filters.level">
          <option value="">全部</option>
          <option v-for="item in levels" :key="item" :value="item">{{ item }}</option>
        </select></label
      >
      <label
        >错误码<input
          v-model.trim="filters.error_code"
          maxlength="64"
          placeholder="例如 AGENT_RUN_FAILED"
      /></label>
      <label
        >Trace ID<input v-model.trim="filters.trace_id" maxlength="32" placeholder="32 位 Trace ID"
      /></label>
      <label
        >时间范围<select v-model="range">
          <option value="15m">最近 15 分钟</option>
          <option value="1h">最近 1 小时</option>
          <option value="24h">最近 24 小时</option>
        </select></label
      >
      <button class="button" :disabled="loading">{{ loading ? '查询中…' : '查询日志' }}</button>
    </form>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <p v-if="result" class="freshness" :class="'logs-state-' + result.state" role="status">
      {{ stateLabel(result.state) }} · {{ result.generated_at ? format(result.generated_at) : '' }}
    </p>
    <p v-if="loading" class="empty">正在查询 Loki…</p>
    <p v-else-if="result?.state === 'not_configured'" class="empty">
      日志后端尚未配置，请启动 observability overlay。
    </p>
    <p v-else-if="result?.state === 'unavailable'" class="empty">
      日志后端暂不可用，业务服务不受影响，请稍后重试。
    </p>
    <p v-else-if="result?.state === 'no_data' || !result?.logs.length" class="empty">
      当前条件没有日志。
    </p>
    <div v-else class="table-wrap logs-table-wrap">
      <table>
        <thead>
          <tr>
            <th>时间</th>
            <th>级别</th>
            <th>服务</th>
            <th>消息</th>
            <th>错误码</th>
            <th>Trace / 业务关联</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="item in result.logs"
            :key="item.timestamp + item.message + (item.span_id || '')"
          >
            <td class="mono">{{ format(item.timestamp) }}</td>
            <td>
              <span class="log-level" :class="'log-' + item.level.toLowerCase()">{{
                item.level
              }}</span>
            </td>
            <td>{{ item.service_name }}</td>
            <td class="log-message">{{ item.message }}</td>
            <td class="mono">{{ item.error_code || '—' }}</td>
            <td>
              <code v-if="item.trace_id">{{ item.trace_id }}</code
              ><span v-else>—</span
              ><small v-if="item.run_id || item.submission_id">{{
                item.run_id ? `Run ${item.run_id}` : `Submission ${item.submission_id}`
              }}</small>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>

<script setup lang="ts">
  const props = defineProps<{
    traceId?: string
    embedded?: boolean
    start?: string
    end?: string
  }>()
  const embedded = props.embedded || false
  import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
  import { useRoute } from 'vue-router'
  import { useAuthStore } from '../stores/auth'
  import { observabilityApi, apiErrorMessage, type ObservabilityLogs } from '../api'

  const route = useRoute()
  const auth = useAuthStore()
  const services = computed(() =>
    [
      'gateway-service',
      'judge-service',
      'judge-worker',
      'contest-service',
      'agent-service',
      'agent-rag-worker',
    ].filter(
      (service) =>
        auth.user?.roles.some((role) => ['admin', 'system_admin'].includes(role)) ||
        service.startsWith('agent-'),
    ),
  )
  const levels = ['DEBUG', 'INFO', 'WARN', 'ERROR']
  const filters = reactive({
    service: '',
    level: '',
    error_code: '',
    trace_id:
      props.traceId || (typeof route.query.trace_id === 'string' ? route.query.trace_id : ''),
  })
  const range = ref('15m')
  const result = ref<ObservabilityLogs>()
  const loading = ref(false)
  const error = ref('')
  let controller: AbortController | null = null
  function rangeStart() {
    const end = new Date()
    const minutes = range.value === '24h' ? 1440 : range.value === '1h' ? 60 : 15
    return {
      start: props.start || new Date(end.getTime() - minutes * 60_000).toISOString(),
      end: props.end || end.toISOString(),
    }
  }
  function format(value: string) {
    return new Date(value).toLocaleString()
  }
  function stateLabel(state: ObservabilityLogs['state']) {
    return {
      available: '已接入',
      no_data: '无数据',
      not_configured: '未配置',
      unavailable: '暂不可用',
    }[state]
  }
  async function load() {
    controller?.abort()
    const current = new AbortController()
    controller = current
    loading.value = true
    error.value = ''
    try {
      const window = rangeStart()
      result.value = (
        await observabilityApi.logs({ ...filters, ...window, limit: 200 }, current.signal)
      ).data
    } catch (reason) {
      if ((reason as { code?: string })?.code !== 'ERR_CANCELED')
        error.value = apiErrorMessage(reason, '查询日志')
    } finally {
      if (controller === current) loading.value = false
    }
  }
  onMounted(load)
  onUnmounted(() => controller?.abort())
</script>
