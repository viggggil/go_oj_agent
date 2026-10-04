<template>
  <section>
    <div class="page-head">
      <div>
        <p class="eyebrow">Contest</p>
        <h1>比赛</h1>
        <p>浏览比赛、报名并查看比赛结果。</p>
      </div>
      <RouterLink v-if="isAdmin" class="button" to="/contests/new">创建比赛</RouterLink>
    </div>

    <div class="filter-bar contest-filter">
      <label
        >状态<select v-model="status" @change="load(1)">
          <option value="">全部比赛</option>
          <option value="CONTEST_STATUS_DRAFT">未开始</option>
          <option value="CONTEST_STATUS_RUNNING">进行中</option>
          <option value="CONTEST_STATUS_ENDED">已结束</option>
        </select></label
      >
    </div>

    <p v-if="error" class="error">
      {{ error }} <button class="link" @click="load(page)">重试</button>
    </p>
    <div v-if="loading && !items.length" class="loading-panel">正在加载比赛…</div>
    <div v-else-if="!items.length" class="empty-panel">暂无符合条件的比赛。</div>
    <div v-else class="contest-table">
      <div class="contest-row contest-head">
        <span>比赛</span><span>状态</span><span>开始时间</span><span>结束时间</span>
      </div>
      <RouterLink
        v-for="item in items"
        :key="item.id"
        class="contest-row"
        :to="`/contests/${item.id}`"
      >
        <span
          ><strong>{{ item.title }}</strong
          ><small>比赛 #{{ item.id }}</small></span
        >
        <span :class="['status-chip', `contest-status-${statusKey(item.status)}`]">{{
          statusLabel(item.status)
        }}</span>
        <time :datetime="item.start_at">{{ formatDate(item.start_at) }}</time>
        <time :datetime="item.end_at">{{ formatDate(item.end_at) }}</time>
      </RouterLink>
    </div>
    <div class="pager">
      <button :disabled="page <= 1 || loading" @click="load(page - 1)">上一页</button>
      <span>第 {{ page }} 页 · 共 {{ total }} 场</span>
      <button :disabled="page * pageSize >= total || loading" @click="load(page + 1)">
        下一页
      </button>
    </div>
  </section>
</template>

<script setup lang="ts">
  import { computed, onMounted, ref } from 'vue'
  import { RouterLink } from 'vue-router'
  import { apiErrorMessage, contestApi } from '../api'
  import { formatDate } from '../submission'
  import type { ContestStatus, ContestSummary } from '../types'
  import { useAuthStore } from '../stores/auth'

  const pageSize = 20
  const page = ref(1)
  const total = ref(0)
  const loading = ref(false)
  const error = ref('')
  const status = ref('')
  const items = ref<ContestSummary[]>([])
  const auth = useAuthStore()
  const isAdmin = computed(() => auth.user?.roles.includes('admin'))

  function statusKey(value: ContestStatus) {
    const text = String(value)
    if (/^\d+$/.test(text))
      return (
        ({ 1: 'draft', 2: 'running', 3: 'ended', 4: 'archived' } as Record<string, string>)[text] ||
        'unspecified'
      )
    return text.replace('CONTEST_STATUS_', '').toLowerCase() || 'unspecified'
  }
  function statusLabel(value: ContestStatus) {
    const key = statusKey(value)
    return (
      (
        {
          unspecified: '未知',
          draft: '未开始',
          running: '进行中',
          ended: '已结束',
          archived: '已归档',
        } as Record<string, string>
      )[key] || key
    )
  }
  async function load(next = 1) {
    loading.value = true
    error.value = ''
    try {
      const { data } = await contestApi.list(next, pageSize, status.value || undefined)
      items.value = data.items || []
      page.value = data.page?.page || next
      total.value = data.page?.total || 0
    } catch (cause) {
      error.value = apiErrorMessage(cause, '加载比赛列表')
    } finally {
      loading.value = false
    }
  }
  onMounted(() => load())
</script>
