<template>
  <section class="eval-panel" aria-label="创建评测运行">
    <h2>新建评测运行</h2>
    <p class="muted">
      每个样例使用独立会话，以部署配置的普通评测账号执行。运行创建时固定配置版本。
    </p>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <form class="eval-form" @submit.prevent="create">
      <fieldset :disabled="saving">
        <legend>运行配置</legend>
        <label
          >评测数据集<select v-model="form.dataset_id" aria-label="评测数据集" required>
            <option value="" disabled>请选择数据集</option>
            <option v-for="item in datasets" :key="item.id" :value="item.id">
              {{ item.name }} · {{ item.case_count }} 个样例
            </option>
          </select></label
        >
        <button
          v-if="datasetPage * 20 < datasetTotal"
          type="button"
          class="link"
          :disabled="optionsLoading"
          @click="loadDatasets(datasetPage + 1)"
        >
          加载更多数据集
        </button>
        <label
          >被测 Agent<select
            v-model="form.agent_key"
            aria-label="被测 Agent"
            required
            @change="loadAgent"
          >
            <option value="" disabled>请选择 Agent</option>
            <option v-for="item in agents" :key="item.id" :value="item.key">
              {{ item.name || item.key }}{{ item.is_test ? '（测试）' : '' }}
            </option>
          </select></label
        >
        <button
          v-if="agentPage * 20 < agentTotal"
          type="button"
          class="link"
          :disabled="optionsLoading"
          @click="loadAgents(agentPage + 1)"
        >
          加载更多 Agent
        </button>
        <p v-if="!agents.length && !optionsLoading" class="muted">
          没有普通用户可用的 Agent，请先在 Agent 管理中配置。
        </p>
        <label
          >评测模式<select v-model="form.mode" aria-label="评测模式">
            <option value="programmatic">程序化评测</option>
            <option value="programmatic_review">程序化评测 + 评审 Agent</option>
          </select></label
        >
        <label v-if="form.mode === 'programmatic_review'"
          >评审 Agent<select v-model="form.reviewer_agent_key" aria-label="评审 Agent" required>
            <option v-if="reviewerReady" value="eval_reviewer">
              Eval 专用评审员（deepseek-v4-pro）
            </option>
          </select></label
        >
        <p v-if="reviewerLoading" class="muted">正在检查评审 Agent 配置…</p>
        <p v-if="reviewerError" class="error" role="alert">{{ reviewerError }}</p>
        <p v-if="form.mode === 'programmatic_review'" class="muted">
          评审配置在本批次启动时固定；每个结果只传入有界回答摘要，不含题目参考回答或凭据。
        </p>
        <div class="eval-score-grid">
          <label
            >并发度<input
              v-model.number="form.concurrency"
              type="number"
              min="1"
              max="8"
              step="1"
              required /></label
          ><label
            >重复次数<input
              v-model.number="form.repeat_count"
              type="number"
              min="1"
              max="3"
              step="1"
              required
          /></label>
        </div>
      </fieldset>
      <p v-if="chosenDataset">
        本次预计执行 {{ chosenDataset.case_count * form.repeat_count }} 条结果。
      </p>
      <details v-if="agentPreview" open>
        <summary>当前配置版本预览</summary>
        <pre>{{ JSON.stringify(agentPreview, null, 2) }}</pre>
      </details>
      <p class="muted">这里展示当前配置；创建完成后的运行详情保存本批次实际使用的版本快照。</p>
      <div class="agent-form-actions">
        <button type="button" class="secondary" :disabled="saving" @click="$emit('close')">
          取消创建</button
        ><button
          :disabled="
            saving ||
            reviewerLoading ||
            !form.dataset_id ||
            !form.agent_key ||
            (form.mode === 'programmatic_review' && (!form.reviewer_agent_key || !reviewerReady))
          "
        >
          {{ saving ? '创建中…' : '启动评测' }}
        </button>
      </div>
    </form>
  </section>
</template>
<script setup lang="ts">
  import { computed, onMounted, onBeforeUnmount, reactive, ref, watch } from 'vue'
  import { agentApi, agentError, agentStatus } from '../../agent/api'
  import { evalApi, type EvalDataset, type EvalRunWrite } from '../../agent/evals'
  import type { AgentContent, Summary } from '../../agent/types'
  import { useAuthStore } from '../../stores/auth'
  const props = defineProps<{ datasetId?: string }>()
  const emit = defineEmits<{ close: []; created: [id: string] }>()
  const auth = useAuthStore(),
    controller = new AbortController()
  const form = reactive<EvalRunWrite>({
    dataset_id: props.datasetId || '',
    agent_key: '',
    mode: 'programmatic',
    reviewer_agent_key: undefined,
    concurrency: 1,
    repeat_count: 1,
  })
  const datasets = ref<EvalDataset[]>([]),
    agents = ref<Summary[]>([])
  const datasetPage = ref(1),
    datasetTotal = ref(0),
    agentPage = ref(1),
    agentTotal = ref(0)
  const saving = ref(false),
    optionsLoading = ref(false),
    error = ref(''),
    agentPreview = ref<Record<string, unknown>>()
  const chosenDataset = computed(() => datasets.value.find((item) => item.id === form.dataset_id))
  const reviewerReady = ref(false),
    reviewerLoading = ref(false),
    reviewerError = ref('')
  let pending: { fingerprint: string; id: string } | undefined
  let previewGeneration = 0
  async function failed(cause: unknown) {
    if (controller.signal.aborted) return
    error.value = agentError(cause)
    if (agentStatus(cause) === 403) {
      datasets.value = []
      agents.value = []
      agentPreview.value = undefined
      await auth.fetchProfile().catch(() => undefined)
    }
  }
  async function loadDatasets(page = 1) {
    optionsLoading.value = true
    try {
      const { data } = await evalApi.datasets(controller.signal, page)
      datasets.value = [
        ...new Map([...datasets.value, ...data.items].map((item) => [item.id, item])).values(),
      ]
      datasetPage.value = page
      datasetTotal.value = data.page.total
    } catch (cause) {
      await failed(cause)
    } finally {
      optionsLoading.value = false
    }
  }
  async function loadAgents(page = 1) {
    optionsLoading.value = true
    try {
      const { data } = await agentApi.list('agents', page, '', 'active', controller.signal)
      agents.value = [
        ...agents.value,
        ...data.items.filter((item) => !item.disabled && item.visibility !== 'admin'),
      ]
      agentPage.value = page
      agentTotal.value = data.page.total
    } catch (cause) {
      await failed(cause)
    } finally {
      optionsLoading.value = false
    }
  }
  async function loadAgent() {
    const generation = ++previewGeneration
    agentPreview.value = undefined
    try {
      const { data } = await agentApi.detail('agents', form.agent_key, undefined, controller.signal)
      if (generation !== previewGeneration || controller.signal.aborted) return
      const content = data.configuration.content as AgentContent
      agentPreview.value = {
        agent_version: data.configuration.id,
        prompt_id: content.prompt_id,
        skill_ids: content.skill_ids,
        model_profile_id: content.model_profile_id,
        knowledge_scope: content.knowledge_scope,
        budget: content.budget,
      }
    } catch (cause) {
      if (generation === previewGeneration) await failed(cause)
    }
  }
  async function loadReviewer() {
    reviewerReady.value = false
    form.reviewer_agent_key = undefined
    reviewerError.value = ''
    reviewerLoading.value = true
    try {
      const { data } = await agentApi.detail(
        'agents',
        'eval_reviewer',
        undefined,
        controller.signal,
      )
      if (controller.signal.aborted || form.mode !== 'programmatic_review') return
      const config = data.configuration.content as AgentContent
      if (
        data.configuration.disabled ||
        config.visibility !== 'admin' ||
        config.is_test ||
        config.allowed_tools.length > 0 ||
        config.knowledge_scope.length > 0
      ) {
        reviewerError.value = 'Eval 专用评审 Agent 配置不满足无工具、无知识库的限制。'
        return
      }
      reviewerReady.value = true
      form.reviewer_agent_key = 'eval_reviewer'
    } catch (cause) {
      if (!controller.signal.aborted) {
        reviewerError.value = agentError(cause)
        if (agentStatus(cause) === 403) await failed(cause)
      }
    } finally {
      reviewerLoading.value = false
    }
  }
  async function create() {
    if (saving.value) return
    saving.value = true
    error.value = ''
    const fingerprint = JSON.stringify(form)
    if (pending?.fingerprint !== fingerprint) pending = { fingerprint, id: crypto.randomUUID() }
    try {
      const { data } = await evalApi.create({ ...form }, pending.id, controller.signal)
      if (!controller.signal.aborted) emit('created', data.run.id)
    } catch (cause) {
      await failed(cause)
    } finally {
      saving.value = false
    }
  }
  onMounted(async () => {
    await Promise.all([loadDatasets(), loadAgents()])
    if (props.datasetId && !chosenDataset.value) {
      try {
        datasets.value.push(
          (await evalApi.dataset(props.datasetId, controller.signal)).data.dataset,
        )
      } catch (cause) {
        await failed(cause)
      }
    }
  })
  watch(
    () => form.mode,
    (mode) => {
      if (mode === 'programmatic_review') void loadReviewer()
      else {
        reviewerReady.value = false
        reviewerError.value = ''
        form.reviewer_agent_key = undefined
      }
    },
  )
  onBeforeUnmount(() => controller.abort())
</script>
