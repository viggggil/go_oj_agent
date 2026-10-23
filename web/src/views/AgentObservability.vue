<template>
  <section class="agent-page">
    <header class="page-header">
      <div>
        <h1>Agent 运行诊断</h1>
        <p>查看聊天 Run 的状态、Trace 和受控执行摘要。</p>
      </div>
      <button class="button secondary" type="button" :disabled="loading" @click="load">刷新</button>
    </header>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <p v-if="overviewError" class="error" role="status">运行概览：{{ overviewError }}</p>
    <p v-if="overview" class="freshness" :class="freshnessState">
      近 {{ overview.window_minutes }} 分钟 · 共 {{ overview.total }} 次运行 · 数据时间
      {{ format(overview.generated_at) }} · {{ freshnessLabel }}
    </p>
    <div v-if="overview" class="eval-metrics">
      <span>运行中 {{ overview.runs.RUNNING }}</span
      ><span>成功 {{ overview.runs.COMPLETED }}</span> <span>失败 {{ overview.runs.FAILED }}</span
      ><span>取消 {{ overview.runs.CANCELLED }}</span>
      <span>中断 {{ overview.runs.INTERRUPTED }}</span>
    </div>
    <div class="toolbar">
      <label
        >状态
        <select v-model="status" :disabled="loading" @change="changeFilter">
          <option value="">全部</option>
          <option v-for="value in statuses" :key="value" :value="value">{{ value }}</option>
        </select></label
      >
    </div>
    <p v-if="loading">正在加载运行记录…</p>
    <p v-else-if="!runs.length" class="empty">暂无 Agent Chat 运行记录。</p>
    <div v-else class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>Run</th>
            <th>状态</th>
            <th>开始时间</th>
            <th>耗时</th>
            <th>Trace</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="run in runs" :key="run.run_id">
            <td>
              <code>{{ run.run_id }}</code>
            </td>
            <td>
              {{ run.status }}<small v-if="run.error_code"> · {{ run.error_code }}</small>
            </td>
            <td>{{ format(run.started_at) }}</td>
            <td>{{ run.duration_ms == null ? '运行中' : run.duration_ms + ' ms' }}</td>
            <td>
              <a
                v-if="run.tempo_url"
                :href="run.tempo_url"
                target="_blank"
                rel="noopener noreferrer"
                >在 Tempo 查看</a
              >
              <code v-else-if="run.trace_id">{{ run.trace_id }}</code
              ><span v-else>无本地 Trace</span>
            </td>
            <td><button class="link" type="button" @click="select(run)">详情</button></td>
          </tr>
        </tbody>
      </table>
    </div>
    <aside v-if="selected" class="detail">
      <button class="link close" @click="selected = null">关闭</button>
      <h2>Run 详情</h2>
      <dl>
        <dt>Run ID</dt>
        <dd>
          <code>{{ selected.run_id }}</code>
        </dd>
        <dt>Trace ID</dt>
        <dd>
          <code v-if="selected.trace_id">{{ selected.trace_id }}</code
          ><span v-else>无本地 Trace</span>
        </dd>
        <dt>工具步骤</dt>
        <dd>{{ selected.tool_step_count }}</dd>
        <dt>知识检索</dt>
        <dd>{{ selected.knowledge_query_count }}</dd>
        <dt>结束时间</dt>
        <dd>{{ selected.finished_at ? format(selected.finished_at) : '运行中' }}</dd>
      </dl>
      <p class="observability-links">
        <a
          v-if="selected.tempo_url"
          :href="selected.tempo_url"
          target="_blank"
          rel="noopener noreferrer"
          >在 Tempo 中打开 Trace</a
        >
        <span v-else>Trace 不可用或没有本地 Trace</span>
        <a
          v-if="selected.langsmith_url"
          :href="selected.langsmith_url"
          target="_blank"
          rel="noopener noreferrer"
          >在 LangSmith 中打开</a
        >
        <span v-else>{{ langsmithStatusLabel(selected.langsmith_export_status) }}</span>
      </p>
    </aside>
    <nav v-if="total > pageSize" class="pagination" aria-label="运行记录分页">
      <button class="button secondary" :disabled="page <= 1 || loading" @click="goPage(page - 1)">
        上一页
      </button>
      <span>第 {{ page }} / {{ pageCount }} 页（{{ total }} 条）</span>
      <button
        class="button secondary"
        :disabled="page >= pageCount || loading"
        @click="goPage(page + 1)"
      >
        下一页
      </button>
    </nav>
  </section>
</template>
<script setup lang="ts">
  import { computed, onMounted, onUnmounted, ref } from 'vue'
  import { useRoute, useRouter } from 'vue-router'
  import { agentApi, agentError } from '../agent/api'
  import type { AgentOverview, AgentRun } from '../agent/types'
  const statuses = ['RUNNING', 'COMPLETED', 'FAILED', 'CANCELLED', 'INTERRUPTED'] as const
  const route = useRoute(),
    router = useRouter()
  const runs = ref<AgentRun[]>([]),
    selected = ref<AgentRun | null>(null),
    overview = ref<AgentOverview>(),
    loading = ref(false),
    error = ref(''),
    overviewError = ref(''),
    page = ref(1),
    total = ref(0),
    pageSize = 20,
    status = ref<AgentRun['status'] | ''>('')
  const pageCount = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))
  const now = ref(Date.now())
  const freshnessState = computed(() =>
    overview.value && now.value - Date.parse(overview.value.generated_at) > 45_000
      ? 'stale'
      : 'fresh',
  )
  const freshnessLabel = computed(() =>
    freshnessState.value === 'stale' ? '数据可能已过期' : '数据新鲜',
  )
  async function load() {
    if (loading.value) return
    loading.value = true
    error.value = ''
    overviewError.value = ''
    try {
      const selectedId = selected.value?.run_id
      const selectedRequest = selectedId ? agentApi.run(selectedId) : Promise.resolve(null)
      const [runResult, overviewResult, selectedResult] = await Promise.allSettled([
        agentApi.runs(page.value, status.value || undefined),
        agentApi.overview(),
        selectedRequest,
      ])
      if (runResult.status === 'fulfilled') {
        runs.value = runResult.value.data.items
        total.value = runResult.value.data.page.total
      } else {
        error.value = agentError(runResult.reason)
      }
      if (overviewResult.status === 'fulfilled') {
        overview.value = overviewResult.value.data
      } else {
        overviewError.value = '未知；Agent 概览暂不可用'
      }
      if (selectedId && selectedResult.status === 'fulfilled' && selectedResult.value) {
        selected.value = selectedResult.value.data
      }
    } finally {
      loading.value = false
    }
  }
  async function select(run: AgentRun) {
    selected.value = run
    try {
      selected.value = (await agentApi.run(run.run_id)).data
      await router.replace({ query: { ...route.query, run_id: run.run_id } })
    } catch (err) {
      error.value = agentError(err)
    }
  }
  function changeFilter() {
    page.value = 1
    void load()
  }
  function goPage(value: number) {
    page.value = value
    void load()
  }
  function format(value: string) {
    return new Date(value).toLocaleString()
  }
  function langsmithStatusLabel(value: AgentRun['langsmith_export_status']) {
    if (value === 'PENDING') return 'LangSmith 导出等待确认'
    if (value === 'EXPORTED') return '已确认导出；链接未配置'
    if (value === 'FAILED') return 'LangSmith 导出失败'
    return 'LangSmith 导出未启用或未配置'
  }
  let timer: ReturnType<typeof setInterval> | undefined
  onMounted(() => {
    void load()
    const runId = typeof route.query.run_id === 'string' ? route.query.run_id : ''
    if (runId)
      void agentApi
        .run(runId)
        .then((response) => {
          selected.value = response.data
        })
        .catch((err) => {
          error.value = agentError(err)
        })
    timer = setInterval(() => {
      now.value = Date.now()
      if (document.visibilityState === 'visible') void load()
    }, 15000)
  })
  onUnmounted(() => {
    if (timer) clearInterval(timer)
  })
</script>
