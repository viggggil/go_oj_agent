<template>
  <details class="rag-evidence" :open="!!initialRunId">
    <summary>查看对话使用的知识来源</summary>
    <form class="rag-inline-form" @submit.prevent="load">
      <label
        >运行 ID<input v-model="runId" required placeholder="输入 Run UUID" :disabled="loading"
      /></label>
      <button :disabled="loading || !runId.trim()">查询知识来源</button>
    </form>
    <p class="muted">
      仅管理员可查询。记录实际返回模型的来源版本与位置，不保存检索原文；运行结束后可查。
    </p>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <div v-if="evidence">
      <p>
        Agent：{{ evidence.agent_key }} · {{ evidence.status }} · 固定
        {{ evidence.versions.length }} 个文档版本
      </p>
      <p v-if="!evidence.attempts.length" class="muted">
        本轮没有检索记录；运行中的记录在结束后保存。
      </p>
      <article v-for="attempt in evidence.attempts" :key="attempt.call_id" class="rag-match">
        <p>
          {{
            attempt.ok
              ? `检索成功，返回 ${attempt.sources.length} 个片段`
              : '检索失败：' + attempt.error_code
          }}
        </p>
        <div v-for="source in attempt.sources" :key="source.source_id">
          <strong>{{ source.title }}</strong>
          <p>
            题号：{{ source.problem_id ?? '未关联' }} · {{ source.section }} · 字符位置
            {{ source.start }}–{{ source.end }}
          </p>
          <p>
            文档版本：<code>{{ source.version_id }}</code>
          </p>
        </div>
      </article>
    </div>
  </details>
</template>
<script setup lang="ts">
  import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
  import { useRoute } from 'vue-router'
  import { ragApi, type RunEvidence } from '../../agent/rag'
  import { agentError, agentStatus } from '../../agent/api'
  import { agentAdministrator } from '../../agent/state'
  import { useAuthStore } from '../../stores/auth'
  const auth = useAuthStore(),
    owner = auth.user!.id,
    controller = new AbortController()
  const route = useRoute(),
    initialRunId = typeof route.query.run_id === 'string' ? route.query.run_id : ''
  const runId = ref(initialRunId),
    loading = ref(false),
    error = ref(''),
    evidence = ref<RunEvidence>()
  const alive = () =>
    !controller.signal.aborted &&
    auth.isAuthenticated &&
    auth.user?.id === owner &&
    agentAdministrator(auth.user.roles)
  async function load() {
    if (loading.value || !alive()) return
    error.value = ''
    evidence.value = undefined
    const id = runId.value.trim()
    if (!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(id)) {
      error.value = '请输入完整的运行 UUID'
      return
    }
    loading.value = true
    try {
      const result = await ragApi.evidence(id, controller.signal)
      if (alive()) evidence.value = result.data.evidence
    } catch (cause) {
      if (alive()) error.value = agentError(cause)
      if (agentStatus(cause) === 403) await auth.fetchProfile().catch(() => undefined)
    } finally {
      if (alive()) loading.value = false
    }
  }
  watch(
    () => [auth.isAuthenticated, auth.user?.id, auth.user?.roles.join(',')],
    () => {
      if (!alive()) {
        controller.abort()
        evidence.value = undefined
        error.value = ''
      }
    },
  )
  onBeforeUnmount(() => controller.abort())
  onMounted(() => {
    if (initialRunId) void load()
  })
</script>
