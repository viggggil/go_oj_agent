<template>
  <section aria-label="评测运行管理">
    <EvalRunForm
      v-if="showCreate"
      :dataset-id="datasetId"
      @close="showCreate = false"
      @created="created"
    />
    <template v-else>
      <header class="eval-section-header">
        <h2>{{ runId ? '运行详情' : '评测运行' }}</h2>
        <div class="eval-actions">
          <button v-if="runId" type="button" class="secondary" @click="openRun('')">
            返回运行列表</button
          ><button type="button" class="secondary" :disabled="loading" @click="restartView">
            刷新运行</button
          ><button type="button" @click="showCreate = true">新建运行</button>
        </div>
      </header>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <p v-if="notice" role="status">{{ notice }}</p>
      <template v-if="!runId">
        <label class="eval-filter"
          >运行状态<select v-model="runFilter" @change="changeRunPage(1)">
            <option value="">全部</option>
            <option v-for="(label, status) in evalRunStatus" :key="status" :value="status">
              {{ label }}
            </option>
          </select></label
        >
        <div class="agent-table-wrap">
          <table>
            <thead>
              <tr>
                <th>Agent / 运行</th>
                <th>状态</th>
                <th>进度</th>
                <th>硬失败 / 错误</th>
                <th>创建时间</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="item in runs" :key="item.id">
                <td>
                  <strong>{{ item.agent_key }}</strong
                  ><br /><small>{{ item.id }}</small>
                </td>
                <td>{{ evalRunStatus[item.status] }}</td>
                <td>{{ item.completed_cases }} / {{ item.total_cases }}</td>
                <td>{{ item.hard_failures }} / {{ item.error_count }}</td>
                <td>{{ item.created_at }}</td>
                <td>
                  <button class="link" type="button" @click="openRun(item.id)">查看运行</button>
                </td>
              </tr>
              <tr v-if="!loading && !runs.length">
                <td colspan="6" class="muted">没有符合条件的评测运行。</td>
              </tr>
            </tbody>
          </table>
        </div>
        <div class="eval-pagination">
          <button
            type="button"
            :disabled="runPage <= 1 || loading"
            @click="changeRunPage(runPage - 1)"
          >
            上一页运行</button
          ><span>第 {{ runPage }} 页 · 共 {{ runTotal }} 次运行</span
          ><button
            type="button"
            :disabled="runPage * 20 >= runTotal || loading"
            @click="changeRunPage(runPage + 1)"
          >
            下一页运行
          </button>
        </div>
      </template>
      <template v-if="runId && selectedRun">
        <section class="eval-panel">
          <h3>{{ selectedRun.agent_key }} · {{ evalRunStatus[selectedRun.status] }}</h3>
          <p class="muted">运行 {{ selectedRun.id }} · 数据集 {{ selectedRun.dataset_id }}</p>
          <progress
            :value="selectedRun.completed_cases"
            :max="selectedRun.total_cases"
            aria-label="评测进度"
          />
          <p>
            已记录 {{ selectedRun.completed_cases }} / {{ selectedRun.total_cases }} 条结果 · 硬失败
            {{ selectedRun.hard_failures }} · 执行错误 {{ selectedRun.error_count }}
          </p>
          <div v-if="metrics" class="eval-metrics">
            <span>程序检查通过 {{ metrics.check_passes }} / {{ metrics.recorded_cases }}</span
            ><span>最终通过 {{ metrics.effective_passes }} / {{ metrics.recorded_cases }}</span
            ><span>已人工评审 {{ metrics.reviewed_cases }}</span
            ><span
              >模型已评审 {{ metrics.model_reviewed_cases }} · 模型通过
              {{ metrics.model_passes }}</span
            ><span>模型评审不可用 {{ metrics.model_review_unavailable }}</span
            ><span>平均耗时 {{ metrics.average_latency_ms ?? '—' }} ms</span>
          </div>
          <p class="muted">
            执行完成表示本批次结束；质量结论需结合程序检查、模型评分和人工评审。最终通过率以已记录结果为分母。
          </p>
          <p v-if="selectedRun.cancel_requested && active">
            正在停止模型调用并记录剩余样例，请等待终态。
          </p>
          <button
            v-if="active"
            type="button"
            class="secondary"
            :disabled="cancelling || selectedRun.cancel_requested"
            @click="cancel"
          >
            {{ cancelling ? '提交取消中…' : '取消运行' }}
          </button>
          <details v-if="selectedRun.reviewer_snapshot">
            <summary>固定的评审 Agent 版本</summary>
            <pre>{{ JSON.stringify(selectedRun.reviewer_snapshot, null, 2) }}</pre>
          </details>
          <details>
            <summary>本次固定的配置快照</summary>
            <pre>{{ JSON.stringify(selectedRun.config_snapshot, null, 2) }}</pre>
          </details>
        </section>
        <div class="eval-workarea">
          <div>
            <form class="eval-filters" @submit.prevent="applyFilters">
              <label
                >结果结论<select v-model="filters.verdict">
                  <option value="">全部</option>
                  <option value="pass">最终通过</option>
                  <option value="fail">最终未通过</option>
                  <option value="hard_fail">硬失败</option>
                  <option value="unreviewed">待人工评审</option>
                </select></label
              ><label
                >执行状态<select v-model="filters.status">
                  <option value="">全部</option>
                  <option value="COMPLETED">执行完成</option>
                  <option value="FAILED">执行失败</option>
                  <option value="CANCELLED">已取消</option>
                </select></label
              ><label>类别<input v-model="filters.category" maxlength="64" /></label
              ><label>错误码<input v-model="filters.error_code" maxlength="64" /></label
              ><label>样例 ID<input v-model="filters.search" maxlength="64" /></label
              ><button :disabled="loading">筛选结果</button>
            </form>
            <div class="agent-table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>样例 / 次数</th>
                    <th>执行状态</th>
                    <th>程序检查</th>
                    <th>人工 / 最终</th>
                    <th>耗时</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="item in results" :key="item.id">
                    <td>
                      <strong>{{ item.case_id }}</strong> · {{ item.attempt }}<br />{{ item.title
                      }}<br /><small>{{ item.category }}</small>
                    </td>
                    <td>
                      {{ item.status }}<br /><small>{{ item.error_code }}</small>
                    </td>
                    <td>
                      {{ item.checks.hard_fail ? '硬失败' : item.checks.pass ? '通过' : '未通过' }}
                    </td>
                    <td>
                      {{
                        item.manual_review
                          ? item.manual_review.pass
                            ? '人工通过'
                            : '人工未通过'
                          : '待评审'
                      }}
                      · 模型：{{ modelReviewStatus[item.model_review_status] }} /
                      {{ item.effective_pass ? '通过' : '未通过' }}
                    </td>
                    <td>{{ item.latency_ms ?? '—' }} ms</td>
                    <td>
                      <button class="link" type="button" @click="resultId = item.id">
                        查看与评分
                      </button>
                    </td>
                  </tr>
                  <tr v-if="!loading && !results.length">
                    <td colspan="6" class="muted">没有符合条件的结果；运行中的结果会自动刷新。</td>
                  </tr>
                </tbody>
              </table>
            </div>
            <div class="eval-pagination">
              <button
                type="button"
                :disabled="resultPage <= 1 || loading"
                @click="changeResultPage(resultPage - 1)"
              >
                上一页结果</button
              ><span>第 {{ resultPage }} 页 · 共 {{ resultTotal }} 条结果</span
              ><button
                type="button"
                :disabled="resultPage * 20 >= resultTotal || loading"
                @click="changeResultPage(resultPage + 1)"
              >
                下一页结果
              </button>
            </div>
          </div>
          <aside v-if="resultId" class="eval-side-panel" aria-label="评测结果与评分">
            <EvalResult
              :key="runId + resultId"
              :run-id="runId"
              :result-id="resultId"
              @close="resultId = ''"
              @updated="restartView"
            />
          </aside>
        </div>
      </template>
    </template>
  </section>
</template>
<script setup lang="ts">
  import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
  import { useRoute, useRouter } from 'vue-router'
  import { agentError, agentStatus } from '../../agent/api'
  import {
    evalApi,
    evalRunStatus,
    type EvalRun,
    type EvalMetrics,
    type EvalResultSummary,
    type EvalFilters,
  } from '../../agent/evals'
  import { useAuthStore } from '../../stores/auth'
  import EvalRunForm from './EvalRunForm.vue'
  import EvalResult from './EvalResult.vue'
  const modelReviewStatus = {
    NOT_REQUESTED: '未启用',
    PENDING: '等待中',
    RUNNING: '评审中',
    COMPLETED: '已完成',
    UNAVAILABLE: '不可用',
    SKIPPED: '已跳过',
  }
  const props = defineProps<{ datasetId?: string }>()
  const route = useRoute(),
    router = useRouter(),
    auth = useAuthStore()
  const runId = computed(() => (typeof route.query.run === 'string' ? route.query.run : ''))
  const showCreate = ref(Boolean(props.datasetId)),
    selectedRun = ref<EvalRun>(),
    metrics = ref<EvalMetrics>()
  const runs = ref<EvalRun[]>([]),
    results = ref<EvalResultSummary[]>([]),
    resultId = ref('')
  const runPage = ref(1),
    runTotal = ref(0),
    runFilter = ref(''),
    resultPage = ref(1),
    resultTotal = ref(0)
  const filters = reactive<EvalFilters>({
    status: '',
    verdict: '',
    category: '',
    error_code: '',
    search: '',
  })
  const appliedFilters = ref<EvalFilters>({})
  const loading = ref(false),
    cancelling = ref(false),
    error = ref(''),
    notice = ref('')
  const active = computed(
    () => selectedRun.value && ['QUEUED', 'RUNNING'].includes(selectedRun.value.status),
  )
  let controller = new AbortController(),
    timer: ReturnType<typeof setTimeout> | undefined
  let cancelPending: { run: EvalRun; id: string } | undefined
  let disposed = false
  async function openRun(id: string) {
    await router.replace({ query: { tab: 'runs', ...(id ? { run: id } : {}) } })
  }
  async function created(id: string) {
    showCreate.value = false
    await openRun(id)
  }
  async function failed(cause: unknown, signal: AbortSignal) {
    if (signal.aborted || disposed) return
    error.value = agentError(cause)
    if (agentStatus(cause) === 403) {
      runs.value = []
      results.value = []
      selectedRun.value = undefined
      metrics.value = undefined
      resultId.value = ''
      await auth.fetchProfile().catch(() => undefined)
    }
  }
  function changeRunPage(next: number) {
    runPage.value = next
    restartView()
  }
  function changeResultPage(next: number) {
    resultPage.value = next
    restartView()
  }
  function applyFilters() {
    appliedFilters.value = { ...filters }
    changeResultPage(1)
  }
  function restartView() {
    clearTimeout(timer)
    controller.abort()
    controller = new AbortController()
    void refresh(controller)
  }
  async function refresh(current: AbortController) {
    loading.value = true
    error.value = ''
    const id = runId.value
    try {
      if (id) {
        const [detail, page] = await Promise.all([
          evalApi.run(id, current.signal),
          evalApi.results(id, resultPage.value, { ...appliedFilters.value }, current.signal),
        ])
        if (current.signal.aborted) return
        selectedRun.value = detail.data.run
        metrics.value = detail.data.metrics
        results.value = page.data.items
        resultTotal.value = page.data.page.total
      } else {
        const { data } = await evalApi.runs(runPage.value, runFilter.value, current.signal)
        if (current.signal.aborted) return
        runs.value = data.items
        runTotal.value = data.page.total
      }
    } catch (cause) {
      await failed(cause, current.signal)
    } finally {
      if (!current.signal.aborted && !disposed) {
        loading.value = false
        if (
          !error.value &&
          (active.value ||
            (!id && runs.value.some((item) => ['QUEUED', 'RUNNING'].includes(item.status))))
        )
          timer = setTimeout(() => {
            void refresh(current)
          }, 2500)
      }
    }
  }
  async function cancel() {
    if (!selectedRun.value || cancelling.value) return
    cancelling.value = true
    error.value = ''
    notice.value = ''
    if (!cancelPending || cancelPending.run.id !== selectedRun.value.id)
      cancelPending = { run: { ...selectedRun.value }, id: crypto.randomUUID() }
    const current = controller
    try {
      await evalApi.cancel(cancelPending.run, cancelPending.id, current.signal)
      cancelPending = undefined
      notice.value = '已请求取消，等待运行收敛。'
      restartView()
    } catch (cause) {
      if (agentStatus(cause) === 409) {
        cancelPending = undefined
        notice.value = '运行版本已变化，请刷新后确认是否仍需取消。'
      }
      await failed(cause, current.signal)
    } finally {
      cancelling.value = false
    }
  }
  watch(
    runId,
    () => {
      selectedRun.value = undefined
      metrics.value = undefined
      resultId.value = ''
      resultPage.value = 1
      cancelPending = undefined
      notice.value = ''
      restartView()
    },
    { immediate: true },
  )
  onBeforeUnmount(() => {
    disposed = true
    controller.abort()
    clearTimeout(timer)
  })
</script>
