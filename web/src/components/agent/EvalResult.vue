<template>
  <article class="eval-result" aria-label="评测结果详情">
    <header class="eval-section-header">
      <h2>样例结果</h2>
      <button type="button" class="secondary" @click="$emit('close')">关闭结果</button>
    </header>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <p v-if="notice" class="success" role="status">{{ notice }}</p>
    <p v-if="loading">加载结果中…</p>
    <template v-if="result">
      <h3>{{ result.case_id }} · {{ result.title }} · 第 {{ result.attempt }} 次</h3>
      <p>
        执行：{{ result.status }} · 程序检查：{{ result.checks.pass ? '通过' : '未通过' }} ·
        最终结论：{{ result.effective_pass ? '通过' : '未通过' }} · 耗时：{{
          result.latency_ms ?? '—'
        }}
        ms
      </p>
      <p v-if="result.checks.hard_fail" class="error">
        此样例存在硬失败，人工评分不能将其改为通过。
      </p>
      <p v-if="result.error_code" class="error">错误：{{ result.error_code }}</p>
      <details open>
        <summary>输入与参考回答</summary>
        <div v-for="(turn, index) in result.case.turns" :key="index">
          <strong>{{ turn.role === 'user' ? '用户输入' : '参考回答（不注入模型）' }}</strong>
          <pre>{{ turn.content }}</pre>
        </div>
      </details>
      <details>
        <summary>预期与评测上下文</summary>
        <pre>{{ pretty(result.case.expectations) }}</pre>
        <pre>{{ pretty(result.case.fixture) }}</pre>
      </details>
      <h3>实际回答</h3>
      <pre class="eval-answer">{{ result.answer || '没有生成回答' }}</pre>
      <details open>
        <summary>程序检查</summary>
        <pre>{{ pretty(result.checks) }}</pre>
      </details>
      <details>
        <summary>模型、工具与知识来源摘要</summary>
        <pre>{{ pretty(result.model_summary) }}</pre>
      </details>
      <section class="eval-review">
        <h3>评审 Agent 评分</h3>
        <p v-if="result.model_review_status === 'SKIPPED'" class="muted">
          此结果为执行失败或程序化硬失败，已跳过模型评审。
        </p>
        <p
          v-else-if="
            result.model_review_status === 'PENDING' || result.model_review_status === 'RUNNING'
          "
          class="muted"
        >
          评审 Agent 正在处理第 {{ result.model_review_attempt || 1 }} 次尝试…
        </p>
        <template v-else-if="result.model_review">
          <p>
            第 {{ result.model_review_attempt }} 次 ·
            {{ result.model_review.pass ? '模型通过' : '模型未通过' }} ·
            {{ result.model_review.hard_fail ? '硬失败' : '无硬失败' }}
          </p>
          <pre>{{ pretty(result.model_review) }}</pre>
        </template>
        <template v-else-if="result.model_review_status === 'UNAVAILABLE'">
          <p class="error">
            评审 Agent 暂不可用（{{ result.model_review_error_code || '未知错误' }}），可以重跑第
            {{ result.model_review_attempt }} 次评审。
          </p>
          <button type="button" class="secondary" :disabled="reviewing" @click="retryModelReview">
            {{ reviewing ? '重跑中…' : '重跑评审 Agent' }}
          </button>
        </template>
        <p v-else class="muted">此运行未启用评审 Agent。</p>
        <details v-if="modelAudits.length">
          <summary>评审尝试记录</summary>
          <ul>
            <li v-for="audit in modelAudits" :key="audit.id">
              第 {{ audit.attempt }} 次 · {{ audit.status }} · {{ audit.created_at }}
              <span v-if="audit.error_code"> · {{ audit.error_code }}</span>
            </li>
          </ul>
          <div v-if="modelAuditTotal > 10" class="eval-pagination">
            <button
              type="button"
              :disabled="modelAuditPage <= 1"
              @click="loadModelAudits(modelAuditPage - 1)"
            >
              上一页评审
            </button>
            <span>第 {{ modelAuditPage }} 页 / {{ modelAuditTotal }} 条</span>
            <button
              type="button"
              :disabled="modelAuditPage * 10 >= modelAuditTotal"
              @click="loadModelAudits(modelAuditPage + 1)"
            >
              下一页评审
            </button>
          </div>
        </details>
      </section>
      <section class="eval-review">
        <h3>人工评审</h3>
        <p class="muted">五个维度各 0–4 分。人工结论可调整语义判断；程序检查与硬失败始终保留。</p>
        <p v-if="result.manual_review">
          当前评分版本 {{ result.review_version }} · 评审人 #{{
            result.manual_review.reviewed_by
          }}
          · {{ result.manual_review.reviewed_at }}
        </p>
        <details v-if="result.manual_review">
          <summary>查看当前已保存评分</summary>
          <pre>{{ pretty(result.manual_review) }}</pre>
        </details>
        <form class="eval-form" @submit.prevent="save">
          <fieldset :disabled="saving">
            <legend>语义评分</legend>
            <div class="eval-score-grid">
              <label v-for="[key, label] in scoreDimensions" :key="key"
                >{{ label
                }}<input
                  v-model.number="scores[key]"
                  type="number"
                  min="0"
                  max="4"
                  step="1"
                  required
              /></label>
            </div>
          </fieldset>
          <label
            >人工结论<select v-model="passed" aria-label="人工结论" :disabled="saving">
              <option :value="false">未通过</option>
              <option
                :value="true"
                :disabled="result.checks.hard_fail || result.status !== 'COMPLETED'"
              >
                通过
              </option>
            </select></label
          >
          <label
            >标签（逗号分隔，最多 10 个）<input v-model="tags" :disabled="saving" maxlength="329"
          /></label>
          <label
            >评审备注<textarea v-model="note" :disabled="saving" rows="3" maxlength="2000" />
          </label>
          <label
            >本次修改原因<textarea
              v-model="reason"
              :disabled="saving"
              required
              rows="2"
              maxlength="512"
            />
          </label>
          <p v-if="conflict" class="error">草稿已保留。加载最新评分后核对，再提交本次修改。</p>
          <div class="agent-form-actions">
            <button v-if="conflict" type="button" class="secondary" @click="reloadLatest">
              加载最新评分并保留草稿</button
            ><button :disabled="saving || conflict || !reason.trim()">
              {{ saving ? '保存中…' : '保存人工评审' }}
            </button>
          </div>
        </form>
      </section>
      <section class="eval-review">
        <h3>评分修改记录</h3>
        <p v-if="!audits.length" class="muted">尚无人工评审。</p>
        <details v-for="audit in audits" :key="audit.id">
          <summary>
            版本 {{ audit.version }} · 评审人 #{{ audit.reviewed_by }} · {{ audit.created_at }} ·
            {{ audit.reason }}
          </summary>
          <p>修改前</p>
          <pre>{{ pretty(audit.old_value) }}</pre>
          <p>修改后</p>
          <pre>{{ pretty(audit.new_value) }}</pre>
        </details>
        <div v-if="auditTotal > 10" class="eval-pagination">
          <button
            type="button"
            :disabled="auditPage <= 1 || loading"
            @click="loadAudits(auditPage - 1)"
          >
            上一页记录</button
          ><span>第 {{ auditPage }} 页 / {{ auditTotal }} 条</span
          ><button
            type="button"
            :disabled="auditPage * 10 >= auditTotal || loading"
            @click="loadAudits(auditPage + 1)"
          >
            下一页记录
          </button>
        </div>
      </section>
    </template>
  </article>
</template>

<script setup lang="ts">
  import { onMounted, onBeforeUnmount, reactive, ref } from 'vue'
  import { agentError, agentStatus } from '../../agent/api'
  import {
    evalApi,
    scoreDimensions,
    type EvalResult,
    type EvalReviewAudit,
    type EvalModelReviewAttempt,
    type EvalScores,
  } from '../../agent/evals'
  import { useAuthStore } from '../../stores/auth'
  const props = defineProps<{ runId: string; resultId: string }>()
  const emit = defineEmits<{ close: []; updated: [] }>()
  const auth = useAuthStore(),
    controller = new AbortController()
  const result = ref<EvalResult>(),
    audits = ref<EvalReviewAudit[]>([]),
    modelAudits = ref<EvalModelReviewAttempt[]>([]),
    modelAuditPage = ref(1),
    modelAuditTotal = ref(0),
    auditPage = ref(1),
    auditTotal = ref(0)
  const loading = ref(false),
    saving = ref(false),
    reviewing = ref(false),
    conflict = ref(false),
    error = ref(''),
    notice = ref('')
  const scores = reactive<EvalScores>({
    task_completion: 2,
    technical_correctness: 2,
    evidence_support: 2,
    instruction_following: 2,
    clarity: 2,
  })
  const passed = ref(false),
    tags = ref(''),
    note = ref(''),
    reason = ref(''),
    baseVersion = ref(0)
  let pending: { fingerprint: string; id: string } | undefined
  let modelTimer: ReturnType<typeof setTimeout> | undefined
  let modelPending: { attempt: number; id: string } | undefined
  function scheduleModelPoll() {
    clearTimeout(modelTimer)
    if (controller.signal.aborted) return
    if (result.value && ['PENDING', 'RUNNING'].includes(result.value.model_review_status)) {
      modelTimer = setTimeout(() => void refreshModel(), 1500)
    }
  }
  async function refreshModel() {
    try {
      result.value = (
        await evalApi.result(props.runId, props.resultId, controller.signal)
      ).data.result
      error.value = ''
      await loadModelAudits(modelAuditPage.value)
      if (!['PENDING', 'RUNNING'].includes(result.value.model_review_status)) emit('updated')
      scheduleModelPoll()
    } catch (cause) {
      await failed(cause)
      if (
        !controller.signal.aborted &&
        result.value &&
        ['PENDING', 'RUNNING'].includes(result.value.model_review_status)
      ) {
        modelTimer = setTimeout(() => void refreshModel(), 3000)
      }
    }
  }
  const pretty = (value: unknown) => JSON.stringify(value, null, 2)
  async function failed(cause: unknown) {
    if (controller.signal.aborted) return
    error.value = agentError(cause)
    if (agentStatus(cause) === 403) {
      result.value = undefined
      audits.value = []
      modelAudits.value = []
      await auth.fetchProfile().catch(() => undefined)
    }
  }
  async function loadAudits(page = 1) {
    try {
      const { data } = await evalApi.reviews(props.resultId, page, controller.signal)
      audits.value = data.items
      auditPage.value = page
      auditTotal.value = data.page.total
    } catch (cause) {
      await failed(cause)
    }
  }
  async function loadModelAudits(page = 1) {
    try {
      const { data } = await evalApi.modelReviews(props.resultId, page, controller.signal)
      modelAudits.value = data.items
      modelAuditTotal.value = data.page.total
      modelAuditPage.value = page
    } catch (cause) {
      await failed(cause)
    }
  }
  async function load(resetDraft = true) {
    loading.value = true
    try {
      const { data } = await evalApi.result(props.runId, props.resultId, controller.signal)
      result.value = data.result
      baseVersion.value = data.result.review_version
      if (resetDraft) {
        const review = data.result.manual_review
        if (review) Object.assign(scores, review.scores)
        passed.value = review?.pass ?? false
        tags.value = review?.tags.join(', ') ?? ''
        note.value = review?.note ?? ''
        reason.value = ''
      }
      await loadAudits()
      await loadModelAudits()
      scheduleModelPoll()
    } catch (cause) {
      await failed(cause)
    } finally {
      loading.value = false
    }
  }
  async function retryModelReview() {
    if (!result.value || reviewing.value || result.value.model_review_status !== 'UNAVAILABLE')
      return
    reviewing.value = true
    error.value = ''
    const attempt = result.value.model_review_attempt
    if (modelPending?.attempt !== attempt) modelPending = { attempt, id: crypto.randomUUID() }
    try {
      await evalApi.retryModelReview(props.resultId, attempt, modelPending.id, controller.signal)
      modelPending = undefined
      notice.value = '已提交重跑请求，等待评审结果。'
      result.value.model_review_status = 'RUNNING'
      await refreshModel()
    } catch (cause) {
      await failed(cause)
      if (agentStatus(cause) === 409) {
        modelPending = undefined
        await refreshModel()
      }
    } finally {
      reviewing.value = false
    }
  }
  async function reloadLatest() {
    error.value = ''
    notice.value = ''
    await load(false)
    if (!error.value) {
      conflict.value = false
      pending = undefined
      notice.value = '已加载最新评分，草稿保留。请核对后再次保存。'
    }
  }
  async function save() {
    if (!result.value || saving.value || conflict.value) return
    saving.value = true
    error.value = ''
    notice.value = ''
    const payload = {
      expected_version: baseVersion.value,
      scores: { ...scores },
      pass: passed.value,
      tags: tags.value
        .split(/[,，]/)
        .map((value) => value.trim())
        .filter(Boolean),
      note: note.value,
      reason: reason.value,
    }
    const fingerprint = JSON.stringify(payload)
    if (pending?.fingerprint !== fingerprint) pending = { fingerprint, id: crypto.randomUUID() }
    try {
      await evalApi.review(props.resultId, payload, pending.id, controller.signal)
      await load()
      if (!error.value) {
        pending = undefined
        notice.value = '人工评审已保存。'
        emit('updated')
      }
    } catch (cause) {
      conflict.value = agentStatus(cause) === 409
      await failed(cause)
    } finally {
      saving.value = false
    }
  }
  onMounted(() => load())
  onBeforeUnmount(() => {
    controller.abort()
    clearTimeout(modelTimer)
  })
</script>
