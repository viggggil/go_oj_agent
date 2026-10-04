<template>
  <section class="workspace">
    <div class="page-head">
      <div>
        <p class="eyebrow">Contest</p>
        <h1>创建比赛</h1>
        <p>设置比赛时间，并从题库中勾选本场比赛的题目。</p>
      </div>
      <RouterLink class="button secondary" to="/contests">返回比赛</RouterLink>
    </div>
    <form class="editor" @submit.prevent="save">
      <label class="wide"
        >比赛名称<input v-model.trim="form.title" required maxlength="255"
      /></label>
      <label>开始时间<input v-model="form.start_at" type="datetime-local" required /></label>
      <label>结束时间<input v-model="form.end_at" type="datetime-local" required /></label>
      <details class="wide problem-picker">
        <summary>选择比赛题目（已勾选 {{ selectedProblemIds.length }} 道）</summary>
        <p v-if="loadingProblems" class="muted">正在加载题库…</p>
        <p v-else-if="!problems.length" class="error">题库中暂无可用题目。</p>
        <template v-else>
          <label v-for="problem in problems" :key="problem.id" class="problem-option">
            <input v-model="selectedProblemIds" type="checkbox" :value="problem.id" />
            <span
              ><strong>#{{ problem.id }} {{ problem.title }}</strong
              ><small>{{ difficulty(problem.difficulty) }}</small></span
            >
          </label>
        </template>
        <small class="muted">已勾选 {{ selectedProblemIds.length }} 道题，每题默认 100 分。</small>
      </details>
      <div class="wide actions">
        <button :disabled="saving">{{ saving ? '创建中' : '创建比赛' }}</button>
      </div>
    </form>
    <p v-if="message" :class="messageIsError ? 'error' : 'notice'">{{ message }}</p>
  </section>
</template>

<script setup lang="ts">
  import { onMounted, reactive, ref } from 'vue'
  import { RouterLink, useRouter } from 'vue-router'
  import { apiErrorMessage, contestApi, problemApi } from '../api'
  import type { ProblemSummary } from '../types'

  const router = useRouter()
  const saving = ref(false)
  const message = ref('')
  const messageIsError = ref(false)
  const problems = ref<ProblemSummary[]>([])
  const selectedProblemIds = ref<number[]>([])
  const loadingProblems = ref(true)
  const form = reactive({ title: '', start_at: '', end_at: '' })

  const difficulty = (value: number) => ({ 1: '简单', 2: '中等', 3: '困难' })[value] || '未知'

  async function save() {
    const title = form.title.trim()
    const ids = selectedProblemIds.value
    if (!title || ids.length === 0) {
      messageIsError.value = true
      message.value = '请输入比赛名称，并至少勾选一道题目'
      return
    }
    const start = new Date(form.start_at)
    const end = new Date(form.end_at)
    if (
      Number.isNaN(start.valueOf()) ||
      Number.isNaN(end.valueOf()) ||
      !start.getTime() ||
      start >= end
    ) {
      messageIsError.value = true
      message.value = '请填写有效的比赛时间，且开始时间必须早于结束时间'
      return
    }
    saving.value = true
    message.value = ''
    try {
      const { data } = await contestApi.create({
        title,
        start_at: { seconds: Math.floor(start.getTime() / 1000), nanos: 0 },
        end_at: { seconds: Math.floor(end.getTime() / 1000), nanos: 0 },
        problems: ids.map((problem_id, index) => ({
          problem_id,
          sort_order: index + 1,
          score: 100,
        })),
      })
      router.replace(`/contests/${data.contest.id}`)
    } catch (error) {
      messageIsError.value = true
      message.value = apiErrorMessage(error, '创建比赛')
    } finally {
      saving.value = false
    }
  }

  onMounted(async () => {
    try {
      const available: ProblemSummary[] = []
      let page = 1
      while (true) {
        const { data } = await problemApi.list(page, 100)
        const items = data.items || []
        available.push(...items.filter((problem) => Number(problem.status) !== 2))
        if (!items.length || page * 100 >= Number(data.page?.total || 0)) break
        page += 1
      }
      problems.value = available
    } catch (error) {
      messageIsError.value = true
      message.value = apiErrorMessage(error, '加载题库')
    } finally {
      loadingProblems.value = false
    }
  })
</script>

<style scoped>
  .problem-picker {
    border: 1px solid #d8dee8;
    border-radius: 8px;
    padding: 12px;
    max-height: 320px;
    overflow-y: auto;
  }
  summary {
    cursor: pointer;
  }
  .problem-option {
    display: flex;
    align-items: center;
    gap: 12px;
    margin: 12px 0;
  }
  .problem-option input {
    width: auto;
  }
  .problem-option small {
    display: block;
  }
</style>
