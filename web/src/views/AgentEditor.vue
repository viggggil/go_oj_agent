<template>
  <section class="agent-page">
    <div class="page-head">
      <div>
        <span class="eyebrow">{{ resourceNames[resource] }} 管理</span>
        <h1>{{ isNew ? '新建' : '编辑' }}{{ resourceNames[resource] }}</h1>
        <p v-if="current">
          <span class="mono">{{ current.id }}</span> ·
          {{ current.archived ? '已归档' : current.disabled ? '已停用' : '当前' }}
        </p>
      </div>
      <div v-if="current" class="agent-actions">
        <RouterLink
          v-if="resource === 'agents'"
          class="button secondary"
          :to="{ path: '/agent/chat', query: { agent_key: key } }"
          >对话</RouterLink
        >
        <button
          class="secondary"
          type="button"
          :disabled="busy || !ready"
          @click="changeState(current.disabled ? 'enable' : 'disable')"
        >
          {{ current.disabled ? '启用' : '停用' }}
        </button>
        <button
          class="danger"
          type="button"
          :disabled="busy || !ready || current.archived"
          @click="changeState('archive')"
        >
          归档
        </button>
      </div>
    </div>
    <p v-if="notice" class="notice" role="status">{{ notice }}</p>
    <p v-if="error" class="error agent-alert" role="alert">
      {{ error }}
      <button v-if="!ready" class="link" type="button" @click="initialize">重试加载</button>
      <button
        v-if="status === 409"
        class="link"
        type="button"
        :disabled="busy"
        @click="readConflict"
      >
        查看当前版本
      </button>
      <RouterLink v-if="status === 401" to="/login">重新登录</RouterLink>
    </p>
    <div v-if="loading" class="loading-panel">加载中…</div>
    <template v-else-if="ready">
      <section v-if="conflict" class="agent-conflict" role="alert">
        <h2>版本冲突</h2>
        <p>
          服务器当前 UUID：<code>{{ conflict.id }}</code
          >。输入仍保留，请比较后选择操作。
        </p>
        <div class="agent-compare">
          <div>
            <h3>当前服务器内容</h3>
            <pre>{{ JSON.stringify(conflict.content, null, 2) }}</pre>
          </div>
          <div>
            <h3>本次输入</h3>
            <pre>{{ JSON.stringify(content, null, 2) }}</pre>
          </div>
        </div>
        <div class="agent-actions">
          <button class="secondary" type="button" @click="useServer">用服务器内容替换表单</button>
          <button v-if="!conflict.archived" type="button" @click="adoptExpected">
            保留输入，基于当前版本重试
          </button>
        </div>
      </section>
      <form class="agent-editor" @submit.prevent="save">
        <fieldset class="agent-form-body" :disabled="busy">
          <label
            >稳定标识<input
              v-model="key"
              :disabled="!isNew"
              maxlength="64"
              required
              pattern="[a-z][a-z0-9_]{1,63}"
          /></label>
          <p v-if="expected" class="agent-version-meta">
            表单基于 UUID <code>{{ expected }}</code>
          </p>
          <ConfigFields
            :resource="resource"
            :content="content"
            :bindings="bindings"
            @create="createDependency"
          />
        </fieldset>
        <div class="agent-form-actions">
          <button
            v-if="returnTo"
            class="secondary"
            type="button"
            :disabled="busy"
            @click="returnWithoutSave"
          >
            返回原表单
          </button>
          <RouterLink class="button secondary" :to="'/agent/' + resource">返回列表</RouterLink>
          <button :disabled="busy || Boolean(current?.archived) || Boolean(conflict)">
            {{ busy ? '保存中…' : isNew ? '创建' : '保存新版本' }}
          </button>
        </div>
      </form>
      <section v-if="current" class="agent-history">
        <div class="section-head">
          <div>
            <h2>历史版本</h2>
            <p class="muted">恢复会创建新 UUID，并保留资源的停用状态。</p>
          </div>
          <button
            class="secondary"
            :disabled="historyLoading || busy"
            @click="loadHistory(historyPage.page)"
          >
            刷新
          </button>
        </div>
        <p v-if="historyError" class="error" role="alert">{{ historyError }}</p>
        <div v-if="historyLoading" class="loading-panel">加载中…</div>
        <div v-else-if="!history.length" class="empty-panel compact">暂无历史版本</div>
        <div v-else class="agent-history-list">
          <div v-for="item in history" :key="item.id" class="agent-history-row">
            <div>
              <strong>{{ item.id === current.id ? '当前' : '历史' }}</strong
              ><small class="mono">{{ item.id }}</small
              ><small
                >{{ formatDate(item.created_at) }} · {{ item.archived ? '已归档' : '未归档' }} ·
                创建者 {{ item.created_by }}</small
              >
            </div>
            <div class="agent-actions">
              <button
                v-if="item.id !== current.id || current.archived"
                class="secondary"
                :disabled="busy"
                @click="restore(item.id)"
              >
                从此版本恢复
              </button>
              <button
                class="agent-icon secondary"
                :disabled="busy"
                :title="'查看 ' + item.id"
                :aria-label="'查看 ' + item.id"
                @click="viewVersion(item.id)"
              >
                <Eye :size="16" />
              </button>
            </div>
          </div>
        </div>
        <div v-if="historyPage.total" class="pager">
          <button
            class="secondary"
            aria-label="历史上一页"
            :disabled="historyLoading || historyPage.page <= 1"
            @click="loadHistory(historyPage.page - 1)"
          >
            <ChevronLeft :size="17" />
          </button>
          <span>第 {{ historyPage.page }} 页，共 {{ historyPage.total }} 个版本</span>
          <button
            class="secondary"
            aria-label="历史下一页"
            :disabled="
              historyLoading || historyPage.page * historyPage.page_size >= historyPage.total
            "
            @click="loadHistory(historyPage.page + 1)"
          >
            <ChevronRight :size="17" />
          </button>
        </div>
      </section>
    </template>
    <dialog ref="versionDialog" class="agent-dialog agent-version-dialog">
      <template v-if="viewed"
        ><div class="section-head">
          <h2>版本详情</h2>
          <button
            class="agent-icon secondary"
            aria-label="关闭版本详情"
            @click="versionDialog?.close()"
          >
            <X :size="18" />
          </button>
        </div>
        <dl class="metadata">
          <dt>UUID</dt>
          <dd class="mono">{{ viewed.id }}</dd>
          <dt>标识</dt>
          <dd>{{ viewed.key }}</dd>
          <dt>创建时间</dt>
          <dd>{{ formatDate(viewed.created_at) }}</dd>
          <dt>创建者</dt>
          <dd>{{ viewed.created_by }}</dd>
          <dt>归档时间</dt>
          <dd>{{ viewed.archived_at ? formatDate(viewed.archived_at) : '—' }}</dd>
          <dt>归档者</dt>
          <dd>{{ viewed.archived_by ?? '—' }}</dd>
        </dl>
        <pre>{{ JSON.stringify(viewed.content, null, 2) }}</pre>
      </template>
    </dialog>
  </section>
</template>
<script setup lang="ts">
  import {
    onBeforeRouteLeave,
    onBeforeRouteUpdate,
    useRoute,
    useRouter,
    RouterLink,
  } from 'vue-router'
  import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
  import { ChevronLeft, ChevronRight, Eye, X } from '@lucide/vue'
  import ConfigFields from '../components/agent/ConfigFields.vue'
  import { agentApi, agentError, agentStatus } from '../agent/api'
  import {
    emptyContent,
    resourceNames,
    type AgentContent,
    type Binding,
    type Configuration,
    type Content,
    type Page,
    type Resource,
    type SkillContent,
    type Summary,
  } from '../agent/types'
  import {
    keepDraft,
    operationIds,
    resourceFrom,
    safeReturn,
    takeDraft,
    type Draft,
  } from '../agent/state'
  import { validateConfiguration } from '../agent/validation'
  import { useAuthStore } from '../stores/auth'
  const route = useRoute(),
    router = useRouter(),
    auth = useAuthStore()
  const resource = resourceFrom(route.params.resource),
    isNew = !route.params.key
  const path = route.path,
    owner = auth.user!.id,
    life = new AbortController()
  const key = ref(isNew ? '' : String(route.params.key)),
    content = ref<Content>(emptyContent(resource)),
    expected = ref<string | null>(null)
  const current = ref<Configuration>(),
    bindings = ref<Binding[]>([]),
    history = ref<Summary[]>([]),
    historyPage = ref<Page>({ page: 1, page_size: 20, total: 0 })
  const loading = ref(true),
    ready = ref(false),
    busy = ref(false),
    error = ref(''),
    notice = ref(''),
    status = ref<number>()
  const historyLoading = ref(false),
    historyError = ref(''),
    conflict = ref<Configuration>(),
    viewed = ref<Configuration>(),
    versionDialog = ref<HTMLDialogElement>()
  const baseline = ref(''),
    returnTo = ref(safeReturn(route.query.return_to)),
    ids = operationIds()
  let skipLeave = false,
    historyController: AbortController | undefined,
    savedDraft: Draft | undefined = takeDraft(owner, path)
  const serialize = () => JSON.stringify({ key: key.value, content: content.value })
  const dirty = computed(() => ready.value && serialize() !== baseline.value)
  const alive = () => !life.signal.aborted && auth.user?.id === owner && auth.isAuthenticated
  const copy = <T,>(value: T): T => JSON.parse(JSON.stringify(value)) as T
  const formatDate = (value: string) => new Date(value).toLocaleString()
  const binding = (cfg: Configuration): Binding => ({
    id: cfg.id,
    key: cfg.key,
    name: (cfg.content as { name?: string }).name || cfg.key,
    archived: cfg.archived,
    disabled: cfg.disabled,
  })
  function checkpoint() {
    baseline.value = serialize()
  }
  async function handleError(cause: unknown) {
    if (!alive()) return
    error.value = agentError(cause)
    status.value = agentStatus(cause)
    if (status.value === 403) {
      ready.value = false
      await auth.fetchProfile().catch(() => undefined)
      if (alive()) await router.replace('/agent/chat')
    }
  }
  async function bindingDetails() {
    const selected = content.value as Partial<AgentContent>,
      pairs: [Resource, string][] = []
    if (selected.prompt_id) pairs.push(['prompts', selected.prompt_id])
    for (const id of selected.skill_ids || []) pairs.push(['skills', id])
    const result = await Promise.allSettled(
      pairs.map(([kind, id]) => agentApi.version(kind, id, life.signal)),
    )
    if (!alive()) return
    bindings.value = result.map((item, index) =>
      item.status === 'fulfilled'
        ? binding(item.value.data.configuration)
        : {
            id: pairs[index][1],
            key: pairs[index][1],
            name: '版本信息加载失败',
            archived: false,
            disabled: false,
          },
    )
  }
  async function initialize() {
    if (busy.value) return
    loading.value = true
    error.value = ''
    ready.value = false
    try {
      if (!isNew) {
        const data = await agentApi.detail(resource, key.value, undefined, life.signal)
        if (!alive()) return
        current.value = data.data.configuration
      }
      if (savedDraft) {
        key.value = savedDraft.key
        content.value = copy(savedDraft.content)
        expected.value = savedDraft.expected
        baseline.value = savedDraft.baseline
        returnTo.value = savedDraft.returnTo || returnTo.value
      } else {
        if (current.value) {
          content.value = copy(current.value.content)
          expected.value = current.value.id
        }
        checkpoint()
      }
      const createdId = route.query.created_id,
        createdKind = route.query.created_kind
      if (
        typeof createdId === 'string' &&
        /^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$/.test(createdId) &&
        ((createdKind === 'prompts' && resource !== 'prompts') ||
          (createdKind === 'skills' && resource === 'agents'))
      ) {
        const data = await agentApi.version(createdKind, createdId, life.signal)
        if (!alive()) return
        const cfg = data.data.configuration
        if (cfg.archived || cfg.disabled) throw new Error('Returned version unavailable')
        if (createdKind === 'prompts') (content.value as SkillContent).prompt_id = cfg.id
        else {
          const value = content.value as AgentContent
          await bindingDetails()
          if (!alive()) return
          const previous = bindings.value.find(
            (item) =>
              value.skill_ids.includes(item.id) && item.key === cfg.key && item.id !== cfg.id,
          )
          if (!previous || window.confirm('将同一 Skill 的历史绑定替换为刚保存的版本？')) {
            if (previous) value.skill_ids = value.skill_ids.filter((id) => id !== previous.id)
            if (!value.skill_ids.includes(cfg.id)) value.skill_ids.push(cfg.id)
            if (!value.default_skill_id || value.default_skill_id === previous?.id)
              value.default_skill_id = cfg.id
          }
        }
        const query = { ...route.query }
        delete query.created_kind
        delete query.created_id
        await router.replace({ path, query })
        if (!alive()) return
      }
      await bindingDetails()
      if (!alive()) return
      ready.value = true
      savedDraft = undefined
      if (current.value) void loadHistory(1)
    } catch (cause) {
      await handleError(cause)
    } finally {
      if (alive()) loading.value = false
    }
  }
  async function loadHistory(page = 1) {
    if (!current.value) return
    historyController?.abort()
    const active = new AbortController()
    historyController = active
    historyLoading.value = true
    historyError.value = ''
    try {
      const { data } = await agentApi.history(resource, key.value, page, active.signal)
      if (!active.signal.aborted && alive()) {
        history.value = data.items
        historyPage.value = data.page
      }
    } catch (cause) {
      if (!active.signal.aborted && alive()) historyError.value = agentError(cause)
    } finally {
      if (!active.signal.aborted && alive()) historyLoading.value = false
    }
  }
  async function refreshAfterWrite(saved: Configuration, replaceBody: boolean) {
    notice.value = '已保存版本 ' + saved.id
    const { data } = await agentApi.detail(resource, key.value, undefined, life.signal)
    if (!alive()) return
    current.value = data.configuration
    expected.value = data.configuration.id
    if (saved.id !== data.configuration.id) {
      notice.value += '；服务器已存在后续版本，表单输入保留。'
      conflict.value = data.configuration
    } else if (replaceBody) {
      content.value = copy(data.configuration.content)
      checkpoint()
      await bindingDetails()
    }
    void loadHistory(1)
  }
  async function save() {
    if (busy.value || !ready.value || conflict.value || (!isNew && !expected.value)) return
    const validation = validateConfiguration(resource, key.value.trim(), content.value)
    if (validation) {
      error.value = validation
      return
    }
    busy.value = true
    error.value = ''
    status.value = undefined
    notice.value = ''
    const body = copy(content.value),
      savedKey = key.value.trim()
    const requestId = ids.for({
      action: isNew ? 'create' : 'replace',
      resource,
      key: savedKey,
      expected: expected.value,
      content: body,
    })
    try {
      const { data } = await agentApi.save(
        resource,
        savedKey,
        body,
        expected.value,
        requestId,
        life.signal,
      )
      if (!alive()) return
      key.value = savedKey
      if (returnTo.value) {
        skipLeave = true
        await router.push({
          path: returnTo.value,
          query: { created_kind: resource, created_id: data.configuration.id },
        })
      } else if (isNew) {
        skipLeave = true
        await router.replace('/agent/' + resource + '/' + savedKey + '/edit')
      } else await refreshAfterWrite(data.configuration, true)
    } catch (cause) {
      await handleError(cause)
    } finally {
      skipLeave = false
      if (alive()) busy.value = false
    }
  }
  async function readConflict() {
    if (busy.value) return
    busy.value = true
    try {
      const { data } = await agentApi.detail(resource, key.value, undefined, life.signal)
      if (alive()) conflict.value = data.configuration
    } catch (cause) {
      await handleError(cause)
    } finally {
      if (alive()) busy.value = false
    }
  }
  async function useServer() {
    if (!conflict.value || !window.confirm('用服务器内容替换本次输入？')) return
    if (isNew) {
      skipLeave = true
      try {
        await router.replace('/agent/' + resource + '/' + conflict.value.key + '/edit')
      } finally {
        skipLeave = false
      }
      return
    }
    current.value = conflict.value
    expected.value = conflict.value.id
    content.value = copy(conflict.value.content)
    conflict.value = undefined
    error.value = ''
    checkpoint()
    await bindingDetails()
  }
  async function adoptExpected() {
    if (
      !conflict.value ||
      !window.confirm('已比较两个版本，保留输入并以服务器当前 UUID 作为下一次保存基准？')
    )
      return
    if (isNew) {
      const editPath = '/agent/' + resource + '/' + conflict.value.key + '/edit'
      keepDraft(owner, editPath, {
        key: conflict.value.key,
        content: content.value,
        expected: conflict.value.id,
        baseline: baseline.value,
        returnTo: returnTo.value,
      })
      skipLeave = true
      try {
        await router.replace(editPath)
      } finally {
        skipLeave = false
      }
      return
    }
    current.value = conflict.value
    expected.value = conflict.value.id
    conflict.value = undefined
    error.value = ''
    ids.clear()
  }
  async function changeState(action: 'archive' | 'disable' | 'enable') {
    if (busy.value || !current.value || !expected.value) return
    const message =
      action === 'archive'
        ? resource === 'agents'
          ? '归档后该 Agent 不能新建运行，历史永久保留。继续？'
          : '归档后不能新绑定；已有 Agent 仍保留历史 UUID。立即阻止运行请使用停用。继续？'
        : action === 'disable'
          ? '停用会阻止使用本资源及依赖的新运行；已接受运行保持快照。继续？'
          : '启用资源不会解除归档或自动更新绑定。继续？'
    if (!window.confirm(message)) return
    busy.value = true
    error.value = ''
    try {
      const { data } = await agentApi.action(
        resource,
        key.value,
        action,
        expected.value,
        ids.for({ action, resource, key: key.value, expected: expected.value }),
        undefined,
        life.signal,
      )
      if (alive()) await refreshAfterWrite(data.configuration, false)
    } catch (cause) {
      await handleError(cause)
    } finally {
      if (alive()) busy.value = false
    }
  }
  async function restore(source: string) {
    if (
      busy.value ||
      !current.value ||
      !expected.value ||
      !window.confirm(
        (dirty.value ? '本次未保存输入将被替换。' : '') +
          '从历史正文创建新 UUID；停用状态保留，依赖重新校验。继续？',
      )
    )
      return
    busy.value = true
    error.value = ''
    try {
      const { data } = await agentApi.action(
        resource,
        key.value,
        'restore',
        expected.value,
        ids.for({ action: 'restore', resource, key: key.value, expected: expected.value, source }),
        source,
        life.signal,
      )
      if (alive()) await refreshAfterWrite(data.configuration, true)
    } catch (cause) {
      await handleError(cause)
    } finally {
      if (alive()) busy.value = false
    }
  }
  async function viewVersion(id: string) {
    try {
      const { data } = await agentApi.detail(resource, key.value, id, life.signal)
      if (alive()) {
        viewed.value = data.configuration
        versionDialog.value?.showModal()
      }
    } catch (cause) {
      await handleError(cause)
    }
  }
  async function createDependency(kind: 'agents' | 'prompts' | 'skills') {
    if (busy.value) return
    keepDraft(owner, path, {
      key: key.value,
      content: content.value,
      expected: expected.value,
      baseline: baseline.value,
      returnTo: returnTo.value,
    })
    skipLeave = true
    await router.push({ path: '/agent/' + kind + '/new', query: { return_to: path } })
    skipLeave = false
  }
  async function returnWithoutSave() {
    if (returnTo.value) await router.push(returnTo.value)
  }
  function beforeUnload(event: BeforeUnloadEvent) {
    if (dirty.value || busy.value) {
      event.preventDefault()
      event.returnValue = ''
    }
  }
  function mayLeave() {
    if (!alive() || skipLeave) return true
    if (busy.value) return false
    return !dirty.value || window.confirm('表单尚未保存，确定离开吗？')
  }
  onBeforeRouteLeave(mayLeave)
  onBeforeRouteUpdate((to) => to.path === path || mayLeave())
  onMounted(() => {
    window.addEventListener('beforeunload', beforeUnload)
    void initialize()
  })
  onBeforeUnmount(() => {
    life.abort()
    historyController?.abort()
    window.removeEventListener('beforeunload', beforeUnload)
  })
</script>
