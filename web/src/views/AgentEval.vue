<template>
  <section class="agent-page agent-eval">
    <header class="agent-page-header">
      <div>
        <p class="eyebrow">质量评测</p>
        <h1>Agent Eval</h1>
        <p class="muted">管理数据集，运行真实 Agent 链路，结合程序检查与人工评审判断质量。</p>
      </div>
      <button v-if="tab === 'datasets'" type="button" @click="showUpload = !showUpload">
        上传数据集
      </button>
    </header>
    <nav class="eval-tabs" aria-label="评测导航">
      <button
        type="button"
        :aria-current="tab === 'datasets' ? 'page' : undefined"
        @click="switchTab('datasets')"
      >
        数据集</button
      ><button
        type="button"
        :aria-current="tab === 'runs' ? 'page' : undefined"
        @click="switchTab('runs')"
      >
        评测运行
      </button>
    </nav>
    <EvalRuns v-if="tab === 'runs'" :dataset-id="initialDataset" />
    <template v-else>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <section v-if="showUpload" class="eval-panel">
        <form class="eval-form" @submit.prevent="upload">
          <h2>上传 JSONL 数据集</h2>
          <label>名称<input v-model="form.name" required maxlength="128" /></label
          ><label>说明<textarea v-model="form.description" maxlength="512" rows="2" /></label
          ><label
            >选择 JSONL 文件<input
              type="file"
              accept=".jsonl,.json,text/plain,application/json"
              @change="readFile" /></label
          ><label
            >JSONL 内容<textarea v-model="form.content" required rows="10" spellcheck="false" />
          </label>
          <p class="muted">
            每行一个 Case；允许 user/assistant 对话，不允许 system 或工具伪造消息。
          </p>
          <p v-if="uploadError" class="error" role="alert">{{ uploadError }}</p>
          <div class="agent-form-actions">
            <button type="button" class="secondary" :disabled="saving" @click="showUpload = false">
              取消上传</button
            ><button :disabled="saving">{{ saving ? '校验中…' : '上传并校验' }}</button>
          </div>
        </form>
      </section>
      <form class="eval-filters" @submit.prevent="searchDatasets">
        <label>搜索数据集<input v-model="search" maxlength="64" /></label
        ><button :disabled="loading">搜索</button>
      </form>
      <div class="agent-table-wrap">
        <table>
          <thead>
            <tr>
              <th>数据集</th>
              <th>Case</th>
              <th>格式 / 状态</th>
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
              <td>{{ item.format_version }} / {{ item.status }}</td>
              <td>{{ item.created_at }}</td>
              <td>
                <div class="eval-actions">
                  <button class="link" type="button" @click="openDataset(item.id)">查看样例</button
                  ><button
                    type="button"
                    :disabled="item.status !== 'READY'"
                    @click="startRun(item.id)"
                  >
                    发起评测
                  </button>
                </div>
              </td>
            </tr>
            <tr v-if="!loading && !datasets.length">
              <td colspan="5" class="muted">还没有符合条件的评测数据集。</td>
            </tr>
          </tbody>
        </table>
      </div>
      <div class="eval-pagination">
        <button type="button" :disabled="page <= 1 || loading" @click="changeDatasetPage(page - 1)">
          上一页数据集</button
        ><span>第 {{ page }} 页 · 共 {{ total }} 个数据集</span
        ><button
          type="button"
          :disabled="page * 20 >= total || loading"
          @click="changeDatasetPage(page + 1)"
        >
          下一页数据集
        </button>
      </div>
      <section v-if="selected" class="eval-panel" aria-label="数据集样例">
        <header class="eval-section-header">
          <h2>{{ selected.dataset.name }}</h2>
          <button type="button" class="secondary" @click="closeDataset">关闭样例</button>
        </header>
        <p class="muted">
          {{ selected.dataset.description || '无说明' }} · {{ selected.dataset.case_count }} 个 Case
          · 内容版本 {{ selected.dataset.content_hash }}
        </p>
        <details v-for="item in selected.cases" :key="item.id">
          <summary>{{ item.id }} · {{ item.category }} · {{ item.title }}</summary>
          <div v-for="(turn, index) in item.case.turns" :key="index">
            <strong>{{ turn.role === 'user' ? '用户输入' : '参考回答' }}</strong>
            <pre>{{ turn.content }}</pre>
          </div>
          <h3>预期与评分维度</h3>
          <pre>{{ JSON.stringify(item.case.expectations, null, 2) }}</pre>
          <h3>上下文</h3>
          <pre>{{ JSON.stringify(item.case.fixture, null, 2) }}</pre>
        </details>
        <div class="eval-pagination">
          <button
            type="button"
            :disabled="casePage <= 1 || caseLoading"
            @click="openDataset(selected.dataset.id, casePage - 1)"
          >
            上一页样例</button
          ><span>第 {{ casePage }} 页 · 共 {{ caseTotal }} 条样例</span
          ><button
            type="button"
            :disabled="casePage * 5 >= caseTotal || caseLoading"
            @click="openDataset(selected.dataset.id, casePage + 1)"
          >
            下一页样例
          </button>
        </div>
        <button type="button" @click="startRun(selected.dataset.id)">使用此数据集评测</button>
      </section>
    </template>
  </section>
</template>
<script setup lang="ts">
  import { computed, onMounted, onBeforeUnmount, reactive, ref } from 'vue'
  import { useRoute, useRouter } from 'vue-router'
  import { agentError, agentStatus } from '../agent/api'
  import { evalApi, type EvalDataset, type EvalCase } from '../agent/evals'
  import { useAuthStore } from '../stores/auth'
  import EvalRuns from '../components/agent/EvalRuns.vue'
  const auth = useAuthStore(),
    route = useRoute(),
    router = useRouter(),
    controller = new AbortController()
  const tab = computed(() => (route.query.tab === 'runs' || route.query.run ? 'runs' : 'datasets'))
  const initialDataset = computed(() =>
    typeof route.query.dataset === 'string' ? route.query.dataset : undefined,
  )
  const datasets = ref<EvalDataset[]>([]),
    selected = ref<{ dataset: EvalDataset; cases: EvalCase[] }>()
  const loading = ref(false),
    saving = ref(false),
    showUpload = ref(false),
    error = ref(''),
    uploadError = ref('')
  const page = ref(1),
    total = ref(0),
    search = ref(''),
    casePage = ref(1),
    caseTotal = ref(0),
    caseLoading = ref(false)
  const form = reactive({ name: '', description: '', content: '' })
  let caseGeneration = 0,
    listGeneration = 0
  async function switchTab(value: string) {
    closeDataset()
    await router.replace({ query: { tab: value } })
  }
  async function startRun(id: string) {
    closeDataset()
    await router.replace({ query: { tab: 'runs', dataset: id } })
  }
  async function failed(cause: unknown) {
    if (controller.signal.aborted) return
    error.value = agentError(cause)
    if (agentStatus(cause) === 403) {
      datasets.value = []
      selected.value = undefined
      await auth.fetchProfile().catch(() => undefined)
    }
  }
  function searchDatasets() {
    page.value = 1
    void load()
  }
  function changeDatasetPage(next: number) {
    page.value = next
    void load()
  }
  async function load() {
    const generation = ++listGeneration
    loading.value = true
    error.value = ''
    try {
      const { data } = await evalApi.datasets(controller.signal, page.value, search.value)
      if (generation !== listGeneration || controller.signal.aborted) return
      datasets.value = data.items
      total.value = data.page.total
    } catch (cause) {
      if (generation === listGeneration) await failed(cause)
    } finally {
      if (generation === listGeneration) loading.value = false
    }
  }
  async function readFile(event: Event) {
    const file = (event.target as HTMLInputElement).files?.[0]
    if (!file) return
    uploadError.value = ''
    if (file.size > 900_000) {
      uploadError.value = '文件超过 900 KB 上限'
      return
    }
    try {
      form.content = new TextDecoder('utf-8', { fatal: true }).decode(await file.arrayBuffer())
      if (!form.name) form.name = file.name.replace(/\.jsonl?$/i, '').slice(0, 128)
    } catch {
      uploadError.value = '无法读取文件，请确认文件使用 UTF-8 编码'
    }
  }
  async function upload() {
    if (saving.value) return
    saving.value = true
    uploadError.value = ''
    try {
      await evalApi.upload(form, controller.signal)
      form.name = ''
      form.description = ''
      form.content = ''
      showUpload.value = false
      page.value = 1
      await load()
    } catch (cause) {
      if (!controller.signal.aborted) uploadError.value = agentError(cause)
      if (agentStatus(cause) === 403) await failed(cause)
    } finally {
      saving.value = false
    }
  }
  function closeDataset() {
    caseGeneration++
    selected.value = undefined
    caseLoading.value = false
  }
  async function openDataset(id: string, number = 1) {
    const generation = ++caseGeneration
    caseLoading.value = true
    error.value = ''
    try {
      const [dataset, cases] = await Promise.all([
        evalApi.dataset(id, controller.signal),
        evalApi.cases(id, controller.signal, number),
      ])
      if (generation !== caseGeneration || controller.signal.aborted) return
      selected.value = { dataset: dataset.data.dataset, cases: cases.data.items }
      casePage.value = number
      caseTotal.value = cases.data.page.total
    } catch (cause) {
      if (generation === caseGeneration) await failed(cause)
    } finally {
      if (generation === caseGeneration) caseLoading.value = false
    }
  }
  onMounted(load)
  onBeforeUnmount(() => controller.abort())
</script>
