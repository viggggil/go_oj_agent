<template>
  <section v-if="contest" class="contest-detail">
    <header class="page-head">
      <div>
        <p class="eyebrow">Contest #{{ contest.id }}</p>
        <h1>{{ contest.title }}</h1>
        <div class="meta">
          <span :class="['status-chip', `contest-status-${statusKey(contest.status)}`]">{{
            statusLabel(contest.status)
          }}</span>
          <span>{{ formatDate(contest.start_at) }} – {{ formatDate(contest.end_at) }}</span>
        </div>
      </div>
      <div class="detail-actions">
        <button v-if="canJoin" :disabled="joining || joined" @click="join">
          {{ joining ? '报名中…' : joined ? '已报名' : '报名参加' }}
        </button>
        <span v-else-if="joined" class="status-chip">已报名</span>
        <RouterLink class="button secondary" :to="`/contests/${contest.id}/leaderboard`"
          >查看排行榜</RouterLink
        >
      </div>
    </header>

    <p v-if="message" :class="messageIsError ? 'error' : 'notice'">{{ message }}</p>
    <section class="contest-problems">
      <div class="section-head">
        <div>
          <h2>比赛题目</h2>
          <p class="muted">按比赛顺序排列，提交结果会计入本场排行榜。</p>
        </div>
      </div>
      <div class="contest-problem-row contest-problem-head">
        <span>#</span><span>题目</span><span>分值</span><span></span>
      </div>
      <div v-for="item in contest.problems" :key="item.problem_id" class="contest-problem-row">
        <span>{{ item.sort_order }}</span>
        <span>
          <RouterLink class="problem-link" :to="problemLink(item.problem_id)">
            <strong>{{ item.title || `题目 ${item.problem_id}` }}</strong>
          </RouterLink>
          <small>题目 {{ item.problem_id }}</small>
        </span>
        <span>{{ item.score }} 分</span>
        <RouterLink class="button secondary" :to="submissionLink(item.problem_id)"
          >提交代码</RouterLink
        >
      </div>
      <div v-if="!contest.problems?.length" class="empty-panel compact">本场比赛暂未配置题目。</div>
    </section>
  </section>
  <div v-else-if="error" class="error">
    {{ error }} <button class="link" @click="load">重试</button>
  </div>
  <div v-else class="loading-panel">正在加载比赛详情…</div>
</template>

<script setup lang="ts">
  import { computed, onMounted, ref } from 'vue'
  import { RouterLink, useRoute } from 'vue-router'
  import { apiErrorMessage, contestApi } from '../api'
  import { formatDate } from '../submission'
  import type { Contest, ContestStatus } from '../types'

  const route = useRoute()
  const contest = ref<Contest | null>(null)
  const error = ref('')
  const message = ref('')
  const messageIsError = ref(false)
  const joining = ref(false)
  const joined = ref(false)
  const contestId = Number(route.params.id)
  const canJoin = computed(() =>
    Boolean(contest.value && statusKey(contest.value.status) === 'draft'),
  )

  function statusKey(value: ContestStatus) {
    if (typeof value === 'number' || /^\d+$/.test(String(value)))
      return (
        ({ 1: 'draft', 2: 'running', 3: 'ended', 4: 'archived' } as Record<string, string>)[
          String(value)
        ] || 'unspecified'
      )
    return String(value).replace('CONTEST_STATUS_', '').toLowerCase()
  }
  function statusLabel(value: ContestStatus) {
    return (
      (
        { draft: '未开始', running: '进行中', ended: '已结束', archived: '已归档' } as Record<
          string,
          string
        >
      )[statusKey(value)] || '未知状态'
    )
  }
  function submissionLink(problemId: number) {
    return `/submissions/new?contest_id=${contestId}&problem_id=${problemId}`
  }
  function problemLink(problemId: number) {
    return `/problems/${problemId}`
  }
  async function load() {
    error.value = ''
    try {
      const { data } = await contestApi.get(contestId)
      contest.value = data.contest
      joined.value = Boolean(data.contest.joined)
    } catch (cause) {
      error.value = apiErrorMessage(cause, '加载比赛详情')
    }
  }
  async function join() {
    joining.value = true
    message.value = ''
    try {
      await contestApi.join(contestId)
      joined.value = true
      messageIsError.value = false
      message.value = '报名成功，可以开始提交比赛题目。'
    } catch (cause) {
      messageIsError.value = true
      message.value = apiErrorMessage(cause, '报名比赛')
    } finally {
      joining.value = false
    }
  }
  onMounted(load)
</script>
