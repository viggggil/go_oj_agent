<template>
  <section v-if="submission" class="submission-detail">
    <div class="page-head">
      <div>
        <p class="eyebrow">提交 #{{ submission.id }}</p>
        <h1>{{ problemTitle }}</h1>
        <p>
          题目 {{ submission.problem_id }} · <span class="mono">{{ submission.language }}</span>
        </p>
      </div>
      <div class="detail-actions">
        <button v-if="isAdmin" class="danger" :disabled="rejudging" @click="rejudge">
          {{ rejudging ? '重判中…' : '管理员重判' }}
        </button>
        <RouterLink class="button secondary" to="/submissions">返回提交记录</RouterLink>
      </div>
    </div>

    <p v-if="message" :class="messageIsError ? 'error' : 'notice'">{{ message }}</p>
    <div class="submission-summary">
      <div>
        <span>状态</span
        ><strong class="status-chip">{{ statusLabel(result?.status ?? submission.status) }}</strong>
      </div>
      <div>
        <span>判题结果</span
        ><strong :class="verdictClass(result?.verdict ?? submission.verdict)">{{
          verdictLabel(result?.verdict ?? submission.verdict)
        }}</strong>
      </div>
      <div>
        <span>耗时</span><strong>{{ result?.time_ms ?? submission.time_ms }} ms</strong>
      </div>
      <div>
        <span>内存</span><strong>{{ result?.memory_kb ?? submission.memory_kb }} KB</strong>
      </div>
      <div>
        <span>创建时间</span><strong>{{ formatDate(submission.created_at) }}</strong>
      </div>
      <div>
        <span>判题时间</span><strong>{{ formatDate(submission.judged_at) }}</strong>
      </div>
    </div>

    <div class="detail-grid">
      <section class="detail-panel source-panel">
        <div class="section-head">
          <h2>提交代码</h2>
          <button class="secondary" :disabled="!sourceCode" @click="copySource">复制代码</button>
        </div>
        <pre v-if="sourceCode" class="source-code"><code>{{ sourceCode }}</code></pre>
        <p v-else-if="sourceLoading">正在加载源码…</p>
        <p v-else class="muted">{{ sourceError || '源码暂不可用' }}</p>
        <dl v-if="source" class="source-meta">
          <div>
            <dt>大小</dt>
            <dd>{{ formatBytes(source.size_bytes) }}</dd>
          </div>
          <div>
            <dt>SHA256</dt>
            <dd class="mono">{{ source.sha256 }}</dd>
          </div>
        </dl>
      </section>

      <section class="detail-panel">
        <div class="section-head">
          <h2>判题信息</h2>
          <button class="secondary" :disabled="refreshing" @click="refresh">刷新</button>
        </div>
        <dl class="metadata">
          <div>
            <dt>Judge Revision</dt>
            <dd class="mono">{{ submission.judge_revision || '—' }}</dd>
          </div>
          <div>
            <dt>重试次数</dt>
            <dd>{{ submission.retry_count || 0 }}</dd>
          </div>
          <div>
            <dt>更新时间</dt>
            <dd>{{ formatDate(submission.updated_at) }}</dd>
          </div>
        </dl>
        <p v-if="result?.system_error_reason" class="error">
          系统诊断：{{ result.system_error_reason }}
        </p>
      </section>
    </div>

    <section class="detail-panel cases-panel">
      <div class="section-head">
        <h2>测试点结果</h2>
        <span class="muted">{{ result?.case_results?.length || 0 }} 个测试点</span>
      </div>
      <div v-if="!result?.case_results?.length" class="empty-panel compact">
        判题完成后会显示测试点结果。
      </div>
      <div v-else class="case-table">
        <div class="case-row case-head">
          <span>测试点</span><span>结果</span><span>耗时</span><span>内存</span><span>信息</span>
        </div>
        <div v-for="item in result.case_results" :key="item.id || item.case_no" class="case-row">
          <strong>#{{ item.case_no }}</strong
          ><span :class="verdictClass(item.verdict)">{{ verdictLabel(item.verdict) }}</span
          ><span>{{ item.time_ms }} ms</span><span>{{ item.memory_kb }} KB</span
          ><span>{{ item.message || '—' }}</span>
        </div>
      </div>
    </section>
  </section>
  <div v-else-if="loading" class="loading-panel">正在加载提交详情…</div>
  <div v-else class="empty-panel">
    <p>{{ error || '提交不存在或无权访问' }}</p>
    <RouterLink class="button" to="/submissions">返回提交记录</RouterLink>
  </div>
</template>

<script setup lang="ts">
  import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
  import { RouterLink, useRoute } from 'vue-router'
  import { apiErrorMessage, problemApi, submissionApi } from '../api'
  import {
    formatBytes,
    formatDate,
    isTerminalStatus,
    pollJudgeResult,
    statusLabel,
    streamJudgeEvents,
    verdictClass,
    verdictLabel,
  } from '../submission'
  import { useAuthStore } from '../stores/auth'
  import type { JudgeResult, Problem, Submission, SubmissionSource } from '../types'

  const route = useRoute()
  const auth = useAuthStore()
  const id = Number(route.params.id)
  const loading = ref(true)
  const refreshing = ref(false)
  const rejudging = ref(false)
  const error = ref('')
  const message = ref('')
  const messageIsError = ref(false)
  const submission = ref<Submission | null>(null)
  const result = ref<JudgeResult | null>(null)
  const source = ref<SubmissionSource | null>(null)
  const sourceCode = ref('')
  const sourceLoading = ref(true)
  const sourceError = ref('')
  const problem = ref<Problem | null>(null)
  const controller = new AbortController()
  let pollTimer: number | undefined
  const isAdmin = computed(() => auth.user?.roles.includes('admin'))
  const problemTitle = computed(
    () => problem.value?.title || `题目 ${submission.value?.problem_id || ''}`,
  )

  async function load() {
    try {
      const response = await submissionApi.get(id)
      submission.value = response.data.submission
      await Promise.all([
        loadResult(),
        loadSource(),
        problemApi
          .get(submission.value.problem_id)
          .then(({ data }) => (problem.value = data.problem))
          .catch(() => undefined),
      ])
    } catch (cause) {
      error.value = apiErrorMessage(cause, '加载提交详情')
    } finally {
      loading.value = false
    }
  }
  async function loadResult() {
    try {
      result.value = (await submissionApi.result(id)).data.result
    } catch (cause) {
      messageIsError.value = true
      message.value = apiErrorMessage(cause, '加载判题结果')
    }
  }
  async function loadSource() {
    sourceLoading.value = true
    try {
      const response = await submissionApi.source(id)
      source.value = response.data
      sourceCode.value = response.data.source_code
    } catch (cause) {
      sourceError.value = apiErrorMessage(cause, '加载提交源码')
    } finally {
      sourceLoading.value = false
    }
  }
  async function refresh() {
    refreshing.value = true
    await loadResult()
    refreshing.value = false
  }
  function startPolling() {
    if (pollTimer || isTerminalStatus(result.value?.status)) return
    pollTimer = window.setInterval(async () => {
      try {
        const next = await pollJudgeResult(id)
        result.value = next
        if (isTerminalStatus(next.status)) stopPolling()
      } catch {
        // Keep the last known state visible; the next tick retries.
      }
    }, 1000)
  }
  function stopPolling() {
    if (pollTimer) window.clearInterval(pollTimer)
    pollTimer = undefined
  }
  async function watchResult() {
    try {
      await streamJudgeEvents(
        id,
        (next) => {
          result.value = next
          if (isTerminalStatus(next.status)) stopPolling()
        },
        controller.signal,
      )
      if (!isTerminalStatus(result.value?.status)) startPolling()
    } catch {
      if (!controller.signal.aborted) startPolling()
    }
  }
  async function copySource() {
    if (!sourceCode.value) return
    try {
      await navigator.clipboard.writeText(sourceCode.value)
      messageIsError.value = false
      message.value = '代码已复制'
    } catch {
      messageIsError.value = true
      message.value = '复制失败，请手动选择代码'
    }
  }
  async function rejudge() {
    if (!submission.value || !window.confirm('确认重新判定这份提交吗？原提交会标记为已失效。'))
      return
    rejudging.value = true
    try {
      const response = await submissionApi.rejudge(submission.value.id, crypto.randomUUID())
      messageIsError.value = false
      message.value = `已创建新提交 #${response.data.submission.id}`
      window.location.href = `/submissions/${response.data.submission.id}`
    } catch (cause) {
      messageIsError.value = true
      message.value = apiErrorMessage(cause, '重新判定')
    } finally {
      rejudging.value = false
    }
  }
  onMounted(async () => {
    if (!auth.user) await auth.fetchProfile().catch(() => undefined)
    await load()
    if (!isTerminalStatus(result.value?.status)) watchResult()
  })
  onBeforeUnmount(() => {
    controller.abort()
    stopPolling()
  })
</script>
