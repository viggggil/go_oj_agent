<template>
  <section>
    <div class="page-head">
      <div>
        <p class="eyebrow">Submission</p>
        <h1>我的提交</h1>
        <p>查看你最近提交的代码和判题状态。</p>
      </div>
      <RouterLink class="button" to="/submissions/new">提交代码</RouterLink>
    </div>

    <form class="filter-bar" @submit.prevent="load(1)">
      <label
        >题目 ID<input
          v-model.number="filters.problemId"
          type="number"
          min="1"
          placeholder="全部题目"
      /></label>
      <label
        >语言<select v-model="filters.language">
          <option value="">全部语言</option>
          <option value="go">Go</option>
        </select></label
      >
      <label
        >状态<select v-model="filters.status">
          <option value="">全部状态</option>
          <option v-for="item in statuses" :key="item.value" :value="item.value">
            {{ item.label }}
          </option>
        </select></label
      >
      <button :disabled="loading">{{ loading ? '加载中' : '筛选' }}</button>
    </form>

    <p v-if="error" class="error">
      {{ error }} <button class="link" @click="load(page)">重试</button>
    </p>
    <div v-if="loading && !items.length" class="loading-panel">正在加载提交记录…</div>
    <div v-else-if="!items.length" class="empty-panel">还没有提交记录，去提交一道题吧。</div>
    <div v-else class="submission-table">
      <div class="submission-row submission-head">
        <span>ID</span><span>题目</span><span>语言</span><span>状态</span><span>结果</span
        ><span>提交时间</span>
      </div>
      <RouterLink
        v-for="item in items"
        :key="item.id"
        class="submission-row"
        :to="`/submissions/${item.id}`"
      >
        <strong>#{{ item.id }}</strong>
        <span
          ><strong>{{ problemName(item.problem_id) }}</strong
          ><small>题目 {{ item.problem_id }}</small></span
        >
        <span class="mono">{{ item.language }}</span>
        <span
          ><span class="status-chip">{{ statusLabel(item.status) }}</span></span
        >
        <span :class="verdictClass(item.verdict)">{{ verdictLabel(item.verdict) }}</span>
        <time :datetime="item.created_at">{{ formatDate(item.created_at) }}</time>
      </RouterLink>
    </div>
    <div class="pager">
      <button :disabled="page <= 1 || loading" @click="load(page - 1)">上一页</button>
      <span>第 {{ page }} 页 · 共 {{ total }} 条</span>
      <button :disabled="page * pageSize >= total || loading" @click="load(page + 1)">
        下一页
      </button>
    </div>
  </section>
</template>

<script setup lang="ts">
  import { onMounted, reactive, ref } from 'vue'
  import { RouterLink } from 'vue-router'
  import { apiErrorMessage, problemApi, submissionApi } from '../api'
  import { formatDate, statusLabel, verdictClass, verdictLabel } from '../submission'
  import type { ProblemSummary, Submission } from '../types'

  const pageSize = 20
  const page = ref(1)
  const total = ref(0)
  const loading = ref(false)
  const error = ref('')
  const items = ref<Submission[]>([])
  const problems = ref<ProblemSummary[]>([])
  const filters = reactive({ problemId: undefined as number | undefined, status: '', language: '' })
  const statuses = [
    { value: 'SUBMISSION_STATUS_QUEUED', label: '排队中' },
    { value: 'SUBMISSION_STATUS_COMPILING', label: '编译中' },
    { value: 'SUBMISSION_STATUS_RUNNING', label: '运行中' },
    { value: 'SUBMISSION_STATUS_DONE', label: '已完成' },
    { value: 'SUBMISSION_STATUS_RETRY_WAIT', label: '等待重试' },
    { value: 'SUBMISSION_STATUS_CANCELLED', label: '已取消' },
    { value: 'SUBMISSION_STATUS_INVALIDATED', label: '已失效' },
  ]
  const problemName = (id: number) =>
    problems.value.find((item) => item.id === id)?.title || `题目 ${id}`
  async function load(next = 1) {
    loading.value = true
    error.value = ''
    try {
      const { data } = await submissionApi.list({ page: next, pageSize, ...filters })
      items.value = data.items || []
      page.value = data.page?.page || next
      total.value = data.page?.total || 0
    } catch (cause) {
      error.value = apiErrorMessage(cause, '加载提交记录')
    } finally {
      loading.value = false
    }
  }
  onMounted(async () => {
    await Promise.all([
      load(),
      problemApi
        .list(1, 100)
        .then(({ data }) => (problems.value = data.items || []))
        .catch(() => undefined),
    ])
  })
</script>
