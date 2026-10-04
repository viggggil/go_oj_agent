<template>
  <section class="workspace">
    <div class="page-head">
      <div>
        <p class="eyebrow">Contest</p>
        <h1>创建比赛</h1>
        <p>设置比赛时间，并按题目编号配置比赛题目。</p>
      </div>
      <RouterLink class="button secondary" to="/contests">返回比赛</RouterLink>
    </div>
    <form class="editor" @submit.prevent="save">
      <label class="wide"
        >比赛名称<input v-model.trim="form.title" required maxlength="255"
      /></label>
      <label>开始时间<input v-model="form.start_at" type="datetime-local" required /></label>
      <label>结束时间<input v-model="form.end_at" type="datetime-local" required /></label>
      <label class="wide">
        题目配置
        <input v-model="problemText" required placeholder="题目 ID，逗号分隔，例如 1,2,3" />
        <small class="muted">题目按输入顺序排列，每题默认 100 分。</small>
      </label>
      <div class="wide actions">
        <button :disabled="saving">{{ saving ? '创建中' : '创建比赛' }}</button>
      </div>
    </form>
    <p v-if="message" :class="messageIsError ? 'error' : 'notice'">{{ message }}</p>
  </section>
</template>

<script setup lang="ts">
  import { reactive, ref } from 'vue'
  import { RouterLink, useRouter } from 'vue-router'
  import { apiErrorMessage, contestApi } from '../api'

  const router = useRouter()
  const saving = ref(false)
  const message = ref('')
  const messageIsError = ref(false)
  const problemText = ref('')
  const form = reactive({ title: '', start_at: '', end_at: '' })

  async function save() {
    const ids = problemText.value
      .split(',')
      .map((value) => Number(value.trim()))
      .filter((value) => Number.isInteger(value) && value > 0)
    if (!form.title || ids.length === 0 || new Set(ids).size !== ids.length) {
      messageIsError.value = true
      message.value = '请输入比赛名称和不重复的题目编号'
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
        title: form.title,
        start_at: start.toISOString(),
        end_at: end.toISOString(),
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
</script>
