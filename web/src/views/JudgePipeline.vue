<template>
  <section class="monitoring-page">
    <header class="page-header">
      <div>
        <p class="eyebrow">Backend observability</p>
        <h1>判题链路</h1>
        <p>从提交进入队列，到 Worker 执行并回写结果的后端因果路径。</p>
      </div>
      <span class="monitoring-status neutral">拓扑视图</span>
    </header>

    <div class="pipeline-note">
      <strong>如何使用</strong>
      <span>先从提交详情查看业务状态，再沿 Trace 链接进入 Tempo 检查跨服务 Span。</span>
    </div>

    <form class="pipeline-note trace-search" @submit.prevent="searchTrace">
      <label for="submission-trace-id">管理员按提交编号排查</label>
      <input id="submission-trace-id" v-model="submissionId" inputmode="numeric" min="1" type="number" required />
      <button class="button secondary" :disabled="searching">{{ searching ? '查询中…' : '查找 Trace' }}</button>
      <a v-if="trace?.link_available" class="button" :href="trace.tempo_url" target="_blank" rel="noopener noreferrer">在 Grafana 中打开</a>
      <span v-if="trace && !trace.link_available" class="muted">Grafana 未配置，或 Trace 尚不可用 / 未采样。</span>
      <span v-else-if="trace?.link_available" class="muted">搜索无结果可能表示未采样或已超过留存时间。</span>
      <span v-if="searchError" class="error">{{ searchError }}</span>
    </form>

    <section class="pipeline" aria-label="判题服务链路">
      <article v-for="(node, index) in nodes" :key="node.title" class="pipeline-node">
        <div class="pipeline-node-icon" aria-hidden="true">{{ index + 1 }}</div>
        <div>
          <p class="eyebrow">{{ node.service }}</p>
          <h2>{{ node.title }}</h2>
          <p>{{ node.description }}</p>
        </div>
        <span class="monitoring-status" :class="node.stateClass">{{ node.state }}</span>
      </article>
    </section>

    <section class="monitoring-grid">
      <article class="monitoring-card">
        <p class="eyebrow">业务事实</p>
        <h2>Submission / Judge Service</h2>
        <p>提交状态、判题结果、重判和幂等语义由 Judge Service 负责，监控页面不复制业务状态。</p>
        <RouterLink class="button secondary" to="/submissions">查看提交记录</RouterLink>
      </article>
      <article class="monitoring-card">
        <p class="eyebrow">异步链路</p>
        <h2>Outbox / RabbitMQ</h2>
        <p>
          任务发布、重试、ACK 和结果事件保持独立语义；出现延迟时从 Trace 和服务日志定位具体阶段。
        </p>
        <span class="monitoring-status neutral">指标接入中</span>
      </article>
      <article class="monitoring-card">
        <p class="eyebrow">执行环境</p>
        <h2>Judge Worker / go-judge</h2>
        <p>Worker 读取固定版本测试点，将编译和执行委托给隔离 Sandbox，并保留测试点级结果。</p>
        <span class="monitoring-status neutral">指标接入中</span>
      </article>
    </section>
  </section>
</template>

<script setup lang="ts">
  import { ref } from 'vue'
  import { api, apiErrorMessage } from '../api'
  import { RouterLink } from 'vue-router'

  const submissionId = ref('')
  const searching = ref(false)
  const searchError = ref('')
  const trace = ref<{ tempo_url: string; link_available: boolean } | null>(null)

  async function searchTrace() {
    searchError.value = ''
    trace.value = null
    searching.value = true
    try {
      const { data } = await api.get<{ tempo_url: string; link_available: boolean }>(
        `/api/v1/admin/observability/judge/submissions/${encodeURIComponent(submissionId.value)}`,
      )
      trace.value = data
    } catch (error) {
      searchError.value = apiErrorMessage(error, '查询判题 Trace')
    } finally {
      searching.value = false
    }
  }

  const nodes = [
    {
      service: 'Gateway',
      title: '提交请求',
      description: '认证用户、校验题目和语言，并把请求转交给 Judge Service。',
      state: '入口',
      stateClass: 'blue',
    },
    {
      service: 'Judge Service',
      title: '持久化与发布',
      description: '创建 Submission，写入 Outbox，并发布带因果上下文的判题任务。',
      state: '事实源',
      stateClass: 'blue',
    },
    {
      service: 'RabbitMQ',
      title: '任务与重试',
      description: '按语言分发任务；超时、重试和死信沿事件 ID 保持幂等。',
      state: '异步',
      stateClass: 'purple',
    },
    {
      service: 'Judge Worker',
      title: '编译与执行',
      description: '读取 Judge Revision 和测试点，调用 go-judge 完成隔离执行。',
      state: '执行',
      stateClass: 'purple',
    },
    {
      service: 'Judge / Contest',
      title: '结果回写',
      description: '提交最终状态并发布 judged/invalidation 事件，更新比赛投影。',
      state: '终态',
      stateClass: 'green',
    },
  ]
</script>
