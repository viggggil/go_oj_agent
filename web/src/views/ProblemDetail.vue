<template>
  <article v-if="problem" class="problem-detail">
    <header class="page-head">
      <div>
        <p class="eyebrow">题目 {{ problem.id }}</p>
        <h1>{{ problem.title }}</h1>
        <div class="meta">
          <span :class="`difficulty d-${problem.difficulty}`">{{ difficulty }}</span
          ><span>{{ problem.time_limit_ms }} ms</span><span>{{ problem.memory_limit_kb }} KB</span>
        </div>
      </div>
      <RouterLink v-if="isAdmin" class="button secondary" :to="`/problems/${problem.id}/edit`"
        >管理</RouterLink
      >
    </header>
    <div class="tags">
      <span v-for="tag in problem.tags" :key="tag.id">{{ tag.name }}</span>
    </div>
    <section class="statement">
      <h2>题目描述</h2>
      <p>{{ problem.description }}</p>
    </section>
  </article>
  <p v-else-if="error" class="error">{{ error }}</p>
  <p v-else>加载中…</p>
</template>
<script setup lang="ts">
  import { computed, onMounted, ref } from 'vue'
  import { RouterLink, useRoute } from 'vue-router'
  import { apiErrorMessage, problemApi } from '../api'
  import { useAuthStore } from '../stores/auth'
  import type { Problem } from '../types'
  const route = useRoute(),
    auth = useAuthStore(),
    problem = ref<Problem | null>(null),
    error = ref('')
  const isAdmin = computed(() => auth.user?.roles.includes('admin'))
  const difficulty = computed(
    () => ({ 1: '简单', 2: '中等', 3: '困难' })[problem.value?.difficulty || 0] || '未知',
  )
  onMounted(async () => {
    if (!auth.user) await auth.fetchProfile().catch(() => undefined)
    try {
      problem.value = (await problemApi.get(Number(route.params.id))).data.problem
    } catch (cause) {
      error.value = apiErrorMessage(cause, '加载题目')
    }
  })
</script>
