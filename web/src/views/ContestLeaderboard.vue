<template>
  <section>
    <div class="page-head">
      <div>
        <p class="eyebrow">Leaderboard</p>
        <h1>{{ contest?.title || `比赛 ${contestId}` }} · 排行榜</h1>
        <p>按解题数、罚时和用户 ID 稳定排序。</p>
      </div>
      <RouterLink class="button secondary" :to="`/contests/${contestId}`">返回比赛</RouterLink>
    </div>
    <p v-if="error" class="error">
      {{ error }} <button class="link" @click="load(page)">重试</button>
    </p>
    <div v-if="loading && !items.length" class="loading-panel">正在加载排行榜…</div>
    <div v-else-if="!items.length" class="empty-panel">当前还没有排行榜数据。</div>
    <div v-else class="leaderboard-table">
      <div class="leaderboard-row leaderboard-head">
        <span>排名</span><span>用户</span><span>解题</span><span>罚时</span><span>各题</span>
      </div>
      <div v-for="item in items" :key="`${item.rank}-${item.user_id}`" class="leaderboard-row">
        <strong>#{{ item.rank }}</strong>
        <span>用户 {{ item.user_id }}</span>
        <strong>{{ item.solved_count ?? item.accepted_count }}</strong>
        <span>{{ formatPenalty(item.penalty_seconds ?? item.penalty) }}</span>
        <span class="problem-results">
          <span
            v-for="problem in item.problems || []"
            :key="problem.problem_id"
            :class="['problem-result', { solved: problem.solved }]"
            :title="problemTitle(problem.problem_id)"
          >
            {{
              problem.solved ? 'AC' : problem.wrong_attempts ? `${problem.wrong_attempts} 次` : '—'
            }}
          </span>
        </span>
      </div>
    </div>
    <div class="pager">
      <button :disabled="page <= 1 || loading" @click="load(page - 1)">上一页</button>
      <span>第 {{ page }} 页 · 共 {{ total }} 人</span>
      <button :disabled="page * pageSize >= total || loading" @click="load(page + 1)">
        下一页
      </button>
    </div>
  </section>
</template>

<script setup lang="ts">
  import { onMounted, ref } from 'vue'
  import { RouterLink, useRoute } from 'vue-router'
  import { apiErrorMessage, contestApi, problemApi } from '../api'
  import type { Contest, LeaderboardEntry } from '../types'

  const route = useRoute()
  const contestId = Number(route.params.id)
  const contest = ref<Contest | null>(null)
  const items = ref<LeaderboardEntry[]>([])
  const problemNames = ref<Record<number, string>>({})
  const page = ref(1)
  const total = ref(0)
  const pageSize = 20
  const loading = ref(false)
  const error = ref('')
  function formatPenalty(seconds: number) {
    const value = Number(seconds || 0)
    return `${Math.floor(value / 60)} 分钟 ${value % 60} 秒`
  }
  function problemTitle(id: number) {
    return problemNames.value[id] || `题目 ${id}`
  }
  async function load(next = 1) {
    loading.value = true
    error.value = ''
    try {
      const [leaderboard, details] = await Promise.all([
        contestApi.leaderboard(contestId, next, pageSize),
        contest.value ? Promise.resolve(null) : contestApi.get(contestId),
      ])
      if (details) {
        contest.value = details.data.contest
        const { data } = await problemApi.list(1, 100)
        problemNames.value = Object.fromEntries(
          (data.items || []).map((item) => [item.id, item.title]),
        )
      }
      items.value = leaderboard.data.items || []
      page.value = leaderboard.data.page?.page || next
      total.value = leaderboard.data.page?.total || 0
    } catch (cause) {
      error.value = apiErrorMessage(cause, '加载排行榜')
    } finally {
      loading.value = false
    }
  }
  onMounted(() => load())
</script>
