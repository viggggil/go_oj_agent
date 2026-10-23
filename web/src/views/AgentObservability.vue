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
    <div class="toolbar">
      <label
        >状态
        <select v-model="status" @change="load">
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
              <code v-if="run.trace_id">{{ run.trace_id }}</code
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
    </aside>
  </section>
</template>
<script setup lang="ts">
  import { onMounted, onUnmounted, ref } from 'vue'
  import { agentApi, agentError } from '../agent/api'
  import type { AgentRun } from '../agent/types'
  const statuses = ['RUNNING', 'COMPLETED', 'FAILED', 'CANCELLED', 'INTERRUPTED'] as const
  const runs = ref<AgentRun[]>([]),
    selected = ref<AgentRun | null>(null),
    loading = ref(false),
    error = ref(''),
    status = ref<AgentRun['status'] | ''>('')
  async function load() {
    loading.value = true
    error.value = ''
    try {
      const response = await agentApi.runs(1, status.value || undefined)
      runs.value = response.data.items
    } catch (err) {
      error.value = agentError(err)
    } finally {
      loading.value = false
    }
  }
  async function select(run: AgentRun) {
    selected.value = run
    try {
      selected.value = (await agentApi.run(run.run_id)).data
    } catch (err) {
      error.value = agentError(err)
    }
  }
  function format(value: string) {
    return new Date(value).toLocaleString()
  }
  let timer: ReturnType<typeof setInterval> | undefined
  onMounted(() => {
    void load()
    timer = setInterval(() => {
      if (document.visibilityState === 'visible') void load()
    }, 15000)
  })
  onUnmounted(() => {
    if (timer) clearInterval(timer)
  })
</script>
