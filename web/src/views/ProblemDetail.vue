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
      <div class="detail-actions">
        <RouterLink class="button" :to="`/submissions/new?problem_id=${problem.id}`"
          >提交代码</RouterLink
        >
        <RouterLink v-if="isAdmin" class="button secondary" :to="`/problems/${problem.id}/edit`"
          >管理</RouterLink
        >
      </div>
    </header>
    <div class="tags">
      <span v-for="tag in problem.tags" :key="tag.id">{{ tag.name }}</span>
    </div>
    <section class="statement">
      <h2>题目描述</h2>
      <div class="problem-description" v-html="descriptionHtml"></div>
    </section>
    <section v-if="problem.samples?.length" class="problem-examples">
      <h2>样例</h2>
      <article v-for="(sample, index) in problem.samples" :key="index" class="problem-example">
        <h3>样例 {{ index + 1 }}</h3>
        <div class="example-grid">
          <div>
            <strong>输入</strong>
            <pre>{{ sample.input }}</pre>
          </div>
          <div>
            <strong>输出</strong>
            <pre>{{ sample.output }}</pre>
          </div>
        </div>
      </article>
    </section>
    <section v-if="problem.constraints?.trim()" class="problem-constraints">
      <h2>数据范围</h2>
      <p>{{ problem.constraints }}</p>
    </section>
  </article>
  <p v-else-if="error" class="error">{{ error }}</p>
  <p v-else>加载中…</p>
</template>
<script setup lang="ts">
  import { computed, onMounted, ref } from 'vue'
  import { RouterLink, useRoute } from 'vue-router'
  import DOMPurify from 'dompurify'
  import MarkdownIt from 'markdown-it'
  import { apiErrorMessage, problemApi } from '../api'
  import { useAuthStore } from '../stores/auth'
  import type { Problem } from '../types'
  const route = useRoute(),
    auth = useAuthStore(),
    problem = ref<Problem | null>(null),
    error = ref('')
  const isAdmin = computed(() => auth.user?.roles.includes('admin'))
  const markdown = new MarkdownIt({ html: false, breaks: true, linkify: true })
  const difficulty = computed(() =>
    problem.value ? { 1: '简单', 2: '中等', 3: '困难' }[problem.value.difficulty] : '未知',
  )
  const descriptionHtml = computed(() => {
    if (!problem.value) return ''
    const { description, title, samples, constraints } = problem.value
    const sectionsToReplace = new Set([
      ...(constraints?.trim() ? ['数据范围'] : []),
      ...(samples?.length ? ['样例输入', '样例输出'] : []),
    ])
    const content = description.split('\n').filter((line, index) => {
      if (index === 0 && line.trim() === `# ${title}`) return false
      return true
    })
    let skippingLegacySection = false
    const renderedLines: string[] = []
    for (const line of content) {
      const heading = /^##\s+(.+?)\s*#*\s*$/.exec(line)
      if (heading) skippingLegacySection = sectionsToReplace.has(heading[1])
      if (!skippingLegacySection) renderedLines.push(line)
    }
    const safeHtml = markdown.render(renderedLines.join('\n'))
    return DOMPurify.sanitize(safeHtml, {
      FORBID_TAGS: ['style', 'iframe', 'form'],
      FORBID_ATTR: ['style'],
    })
  })
  onMounted(async () => {
    if (!auth.user) await auth.fetchProfile().catch(() => undefined)
    try {
      problem.value = (await problemApi.get(Number(route.params.id))).data.problem
    } catch (cause) {
      error.value = apiErrorMessage(cause, '加载题目')
    }
  })
</script>
