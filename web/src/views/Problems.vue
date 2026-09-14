<template>
  <section class="page-head">
    <div>
      <h1>题目</h1>
      <p>选择一道题开始解答。</p>
    </div>
    <RouterLink v-if="isAdmin" class="button" to="/problems/new">新建题目</RouterLink>
  </section>
  <div class="problem-table">
    <div class="table-row table-head">
      <span>编号</span><span>题目</span><span>难度</span><span v-if="isAdmin">状态</span>
    </div>
    <RouterLink
      v-for="problem in items"
      :key="problem.id"
      class="table-row"
      :class="{ admin: isAdmin }"
      :to="`/problems/${problem.id}`"
    >
      <span>{{ problem.id }}</span
      ><strong>{{ problem.title }}</strong
      ><span :class="`difficulty d-${problem.difficulty}`">{{
        difficulty(problem.difficulty)
      }}</span
      ><span v-if="isAdmin">{{ problem.status === 2 ? '已归档' : '正常' }}</span>
    </RouterLink>
    <p v-if="!loading && !items.length" class="empty">暂无题目</p>
  </div>
  <p v-if="error" class="error">{{ error }}</p>
  <div class="pager">
    <button :disabled="page <= 1 || loading" @click="load(page - 1)">上一页</button
    ><span>第 {{ page }} 页 · 共 {{ total }} 题</span
    ><button :disabled="page * pageSize >= total || loading" @click="load(page + 1)">下一页</button>
  </div>
</template>
<script setup lang="ts">
  import { computed, onMounted, ref } from 'vue'
  import { RouterLink } from 'vue-router'
  import { problemApi } from '../api'
  import { useAuthStore } from '../stores/auth'
  import type { ProblemSummary } from '../types'
  const auth = useAuthStore(),
    items = ref<ProblemSummary[]>([]),
    page = ref(1),
    pageSize = 20,
    total = ref(0),
    loading = ref(false),
    error = ref('')
  const isAdmin = computed(() => auth.user?.roles.includes('admin'))
  const difficulty = (v: number) => ({ 1: '简单', 2: '中等', 3: '困难' })[v] || '未知'
  async function load(next = 1) {
    loading.value = true
    error.value = ''
    try {
      const { data } = await problemApi.list(next, pageSize)
      items.value = data.items || []
      page.value = data.page.page
      total.value = data.page.total
    } catch (e) {
      error.value = '题目列表加载失败'
    } finally {
      loading.value = false
    }
  }
  onMounted(async () => {
    if (!auth.user) await auth.fetchProfile().catch(() => undefined)
    await load()
  })
</script>
