<template>
  <section class="agent-page">
    <header class="agent-page-header">
      <div>
        <p class="eyebrow">质量评测</p>
        <h1>Agent Eval</h1>
        <p class="muted">上传 JSONL 数据集，先校验样例结构；批量运行和人工评审将在后续阶段接入。</p>
      </div>
      <button type="button" @click="showUpload = true">上传数据集</button>
    </header>

    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <div class="agent-table-wrap">
      <table>
        <thead>
          <tr>
            <th>数据集</th>
            <th>Case</th>
            <th>格式</th>
            <th>状态</th>
            <th>创建时间</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in datasets" :key="item.id">
            <td>
              <strong>{{ item.name }}</strong
              ><br /><span class="muted">{{ item.description || '无说明' }}</span>
            </td>
            <td>{{ item.case_count }}</td>
            <td>{{ item.format_version }}</td>
            <td>{{ item.status }}</td>
            <td>{{ item.created_at }}</td>
            <td>
              <button class="link" type="button" @click="openDataset(item.id)">查看样例</button>
            </td>
          </tr>
          <tr v-if="!loading && !datasets.length">
            <td colspan="6" class="muted">还没有评测数据集。</td>
          </tr>
        </tbody>
      </table>
    </div>

    <dialog :open="showUpload" @close="showUpload = false">
      <form class="agent-form" @submit.prevent="upload">
        <h2>上传 JSONL 数据集</h2>
        <label>名称<input v-model="form.name" required maxlength="128" /></label>
        <label>说明<textarea v-model="form.description" maxlength="512" rows="2" /></label>
        <label
          >JSONL 内容<textarea v-model="form.content" required rows="14" spellcheck="false" />
        </label>
        <p class="muted">每行一个 Case；允许 user/assistant 对话，不允许 system 或工具伪造消息。</p>
        <p v-if="uploadError" class="error" role="alert">{{ uploadError }}</p>
        <div class="agent-form-actions">
          <button type="button" class="secondary" @click="showUpload = false">取消</button
          ><button :disabled="saving">{{ saving ? '校验中…' : '上传并校验' }}</button>
        </div>
      </form>
    </dialog>

    <dialog :open="Boolean(selected)" @close="selected = undefined">
      <article v-if="selected" class="agent-detail">
        <h2>{{ selected.dataset.name }}</h2>
        <p class="muted">
          {{ selected.dataset.description || '无说明' }} · {{ selected.dataset.case_count }} 个 Case
        </p>
        <ol>
          <li v-for="item in selected.cases" :key="item.id">
            <strong>{{ item.id }}</strong> · {{ item.category }} · {{ item.title }}
          </li>
        </ol>
        <button type="button" @click="selected = undefined">关闭</button>
      </article>
    </dialog>
  </section>
</template>

<script setup lang="ts">
  import { onMounted, onBeforeUnmount, reactive, ref } from 'vue'
  import { agentError, agentStatus } from '../agent/api'
  import { evalApi, type EvalDataset, type EvalCase } from '../agent/evals'
  import { useAuthStore } from '../stores/auth'

  const auth = useAuthStore(),
    controller = new AbortController()
  const datasets = ref<EvalDataset[]>([]),
    selected = ref<{ dataset: EvalDataset; cases: EvalCase[] }>(),
    loading = ref(false),
    saving = ref(false),
    showUpload = ref(false),
    error = ref(''),
    uploadError = ref('')
  const form = reactive({ name: '', description: '', content: '' })

  async function load() {
    loading.value = true
    try {
      datasets.value = (await evalApi.datasets(controller.signal)).data.items
    } catch (cause) {
      error.value = agentError(cause)
      if (agentStatus(cause) === 403) await auth.fetchProfile().catch(() => undefined)
    } finally {
      loading.value = false
    }
  }
  async function upload() {
    saving.value = true
    uploadError.value = ''
    try {
      await evalApi.upload(form, controller.signal)
      form.name = ''
      form.description = ''
      form.content = ''
      showUpload.value = false
      await load()
    } catch (cause) {
      uploadError.value = agentError(cause)
    } finally {
      saving.value = false
    }
  }
  async function openDataset(id: string) {
    try {
      const [dataset, page] = await Promise.all([
        evalApi.dataset(id, controller.signal),
        evalApi.cases(id, controller.signal),
      ])
      selected.value = { dataset: dataset.data.dataset, cases: page.data.items }
    } catch (cause) {
      error.value = agentError(cause)
    }
  }
  onMounted(load)
  onBeforeUnmount(() => controller.abort())
</script>
