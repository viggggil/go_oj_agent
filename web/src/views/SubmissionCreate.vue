<template>
  <section class="workspace submission-create">
    <div class="page-head">
      <div>
        <p class="eyebrow">Submission</p>
        <h1>提交代码</h1>
        <p>选择题目和语言，提交后可在详情页查看判题进度。</p>
      </div>
      <RouterLink class="button secondary" to="/submissions">提交记录</RouterLink>
    </div>
    <form class="editor submission-form" @submit.prevent="submit">
      <label class="wide"
        >题目<select v-model.number="form.problem_id" required>
          <option :value="0" disabled>请选择题目</option>
          <option v-for="item in problems" :key="item.id" :value="item.id">
            {{ item.id }} · {{ item.title }}
          </option>
        </select></label
      >
      <label
        >语言<select v-model="form.language">
          <option value="go">Go</option>
        </select></label
      >
      <label
        >源码文件<input ref="fileInput" type="file" accept=".go,.txt,text/plain" @change="readFile"
      /></label>
      <label class="wide"
        >代码<textarea
          v-model="form.source_code"
          class="code-input"
          spellcheck="false"
          placeholder="package main&#10;&#10;func main() {&#10;}"
          required
        ></textarea
        ><small>{{ form.source_code.length.toLocaleString() }} / 1,048,576 字节</small></label
      >
      <div class="wide actions">
        <button :disabled="submitting || !valid">{{ submitting ? '提交中…' : '提交代码' }}</button>
      </div>
    </form>
    <p v-if="message" :class="messageIsError ? 'error' : 'notice'">{{ message }}</p>
  </section>
</template>

<script setup lang="ts">
  import { computed, onMounted, reactive, ref } from 'vue'
  import { RouterLink, useRoute, useRouter } from 'vue-router'
  import { apiErrorMessage, problemApi, submissionApi } from '../api'
  import type { ProblemSummary } from '../types'

  const route = useRoute()
  const router = useRouter()
  const problems = ref<ProblemSummary[]>([])
  const fileInput = ref<HTMLInputElement>()
  const submitting = ref(false)
  const message = ref('')
  const messageIsError = ref(false)
  const form = reactive({
    problem_id: Number(route.query.problem_id) || 0,
    language: 'go',
    source_code: '',
  })
  const valid = computed(
    () =>
      form.problem_id > 0 &&
      form.source_code.trim().length > 0 &&
      new TextEncoder().encode(form.source_code).length <= 1048576,
  )

  async function readFile() {
    const file = fileInput.value?.files?.[0]
    if (!file) return
    if (file.size > 1048576) {
      messageIsError.value = true
      message.value = '代码文件不能超过 1 MiB'
      return
    }
    try {
      form.source_code = await file.text()
      message.value = ''
    } catch {
      messageIsError.value = true
      message.value = '无法读取代码文件'
    }
  }
  async function submit() {
    if (!valid.value) {
      messageIsError.value = true
      message.value = '请选择题目并填写代码'
      return
    }
    submitting.value = true
    message.value = ''
    try {
      const { data } = await submissionApi.create({ ...form, idempotency_key: crypto.randomUUID() })
      await router.push(`/submissions/${data.submission_id}`)
    } catch (cause) {
      messageIsError.value = true
      message.value = apiErrorMessage(cause, '提交代码')
    } finally {
      submitting.value = false
    }
  }
  onMounted(async () => {
    try {
      problems.value = (await problemApi.list(1, 100)).data.items || []
    } catch (cause) {
      messageIsError.value = true
      message.value = apiErrorMessage(cause, '加载题目')
    }
  })
</script>
