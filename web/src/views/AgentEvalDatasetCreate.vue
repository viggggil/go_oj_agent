<template>
  <section class="agent-page agent-eval agent-eval-dataset-create">
    <header class="agent-page-header">
      <div>
        <p class="eyebrow">质量评测</p>
        <h1>上传评测数据集</h1>
        <p class="muted">上传 JSONL 文件并校验样例格式。</p>
      </div>
      <RouterLink class="secondary button" to="/agent/eval">返回数据集</RouterLink>
    </header>
    <section class="eval-panel eval-dataset-create">
      <form class="eval-form" @submit.prevent="upload">
        <label>名称<input v-model="form.name" required maxlength="128" /></label>
        <label>说明<textarea v-model="form.description" maxlength="512" rows="2" /></label>
        <label
          >选择 JSONL 文件<input
            type="file"
            accept=".jsonl,.json,text/plain,application/json"
            @change="readFile"
        /></label>
        <label
          >JSONL 内容<textarea v-model="form.content" required rows="18" spellcheck="false" />
        </label>
        <p class="muted">
          每行一个 Case；允许 user/assistant 对话，不允许 system 或工具伪造消息。最大 900 KB。
        </p>
        <p v-if="error" class="error" role="alert">{{ error }}</p>
        <div class="agent-form-actions">
          <RouterLink class="secondary button" to="/agent/eval">取消</RouterLink>
          <button :disabled="saving || !form.content.trim()">
            {{ saving ? '校验中…' : '上传并校验' }}
          </button>
        </div>
      </form>
    </section>
  </section>
</template>

<script setup lang="ts">
  import { onBeforeUnmount, reactive, ref } from 'vue'
  import { RouterLink, useRouter } from 'vue-router'
  import { agentError } from '../agent/api'
  import { evalApi } from '../agent/evals'

  const router = useRouter()
  const controller = new AbortController()
  const form = reactive({ name: '', description: '', content: '' })
  const saving = ref(false)
  const error = ref('')

  async function readFile(event: Event) {
    const input = event.target as HTMLInputElement
    const file = input.files?.[0]
    if (!file) return
    error.value = ''
    if (file.size > 900_000) {
      error.value = '文件超过 900 KB 上限'
      input.value = ''
      return
    }
    try {
      form.content = new TextDecoder('utf-8', { fatal: true }).decode(await file.arrayBuffer())
      if (!form.name) form.name = file.name.replace(/\.jsonl?$/i, '').slice(0, 128)
    } catch {
      error.value = '无法读取文件，请确认文件使用 UTF-8 编码'
    } finally {
      input.value = ''
    }
  }

  async function upload() {
    if (saving.value) return
    saving.value = true
    error.value = ''
    try {
      await evalApi.upload(form, controller.signal)
      await router.replace('/agent/eval')
    } catch (cause) {
      if (!controller.signal.aborted) error.value = agentError(cause)
    } finally {
      saving.value = false
    }
  }

  onBeforeUnmount(() => controller.abort())
</script>
