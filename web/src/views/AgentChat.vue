<template>
  <section class="agent-page agent-chat-page">
    <div class="chat-heading">
      <h1>对话</h1>
      <label class="chat-history-select"
        >会话
        <select
          v-model="historySelection"
          aria-label="选择对话"
          :disabled="loadingHistory"
          @change="historyChanged"
        >
          <option value="" disabled>新对话</option>
          <option value="new">开启新对话</option>
          <option v-for="item in conversationOptions" :key="item.id" :value="item.id">
            {{ item.title }}
          </option>
          <option v-if="historyPage * 20 < historyTotal" value="more">加载更多历史对话…</option>
        </select>
      </label>
      <RouterLink v-if="agentAdministrator(auth.user?.roles || [])" to="/agent/agents" class="link"
        >管理 Agent</RouterLink
      >
    </div>
    <p v-if="catalogError" class="error" role="alert">
      {{ catalogError }} <button class="link" @click="initialize">重新加载</button>
    </p>
    <div
      ref="scrollBox"
      class="chat-messages"
      aria-label="消息列表"
      @scroll="trackScroll"
      @click="copyCode"
    >
      <button
        v-if="nextBefore"
        class="link"
        type="button"
        :disabled="loadingHistory"
        @click="loadOlder"
      >
        加载更早消息
      </button>
      <div v-if="!messages.length" class="chat-empty">
        <strong>有什么可以帮你？</strong>
        <p>选择学习助手，开始讨论题目与算法。</p>
      </div>
      <article
        v-for="message in messages"
        :key="message.id"
        class="chat-message"
        :class="message.role"
      >
        <div class="chat-role">{{ message.role === 'user' ? '我' : 'Agent' }}</div>
        <details v-if="message.activity?.length" class="chat-activity">
          <summary>思路与执行过程（{{ message.activity.length }}）</summary>
          <ol>
            <li v-for="(item, index) in message.activity" :key="index" :class="item.status">
              <span>{{ item.text }}</span>
              <small v-if="item.duration !== undefined"> · {{ item.duration }} ms</small>
            </li>
          </ol>
        </details>
        <div
          v-if="message.role === 'assistant'"
          class="chat-content markdown"
          v-html="render(message.text)"
        ></div>
        <div v-else class="chat-content">{{ message.text }}</div>
        <span v-if="message.failure" class="error chat-failure">{{ message.failure }}</span>
        <span v-else-if="message.incomplete" class="chat-incomplete">{{
          streaming && !message.text ? '正在等待模型回答…' : '回答未完成'
        }}</span>
        <button
          v-if="message.role === 'assistant' && message.text"
          class="link copy-answer"
          type="button"
          @click="copy(message.text)"
        >
          复制
        </button>
      </article>
    </div>
    <p v-if="status" class="chat-status" :class="{ error: statusError }" role="status">
      {{ status }}
    </p>
    <RouterLink
      v-if="lastRunId && !streaming && agentAdministrator(auth.user?.roles || [])"
      :to="{ path: '/agent/observability', query: { run_id: lastRunId } }"
      class="link"
      >诊断本轮运行</RouterLink
    >
    <RouterLink
      v-if="lastRunId && !streaming && agentAdministrator(auth.user?.roles || [])"
      :to="{ path: '/agent/rag', query: { run_id: lastRunId } }"
      class="link"
      >查看本轮知识来源</RouterLink
    >
    <form class="chat-compose" @submit.prevent="send">
      <textarea
        v-model="draft"
        aria-label="消息"
        rows="2"
        :disabled="streaming || !selectedAgent || loadingHistory"
        placeholder="输入消息，按 Ctrl/⌘ + Enter 发送"
        @keydown="keyDown"
      ></textarea>
      <div class="chat-actions">
        <label class="chat-agent-select"
          >Agent
          <select
            v-model="selectedAgent"
            aria-label="选择 Agent"
            :disabled="loadingCatalog || loadingHistory"
            @change="agentChanged"
          >
            <option value="" disabled>选择 Agent</option>
            <option v-for="item in agentOptions" :key="item.key" :value="item.key">
              {{ item.name }}
            </option>
            <option v-if="catalogPage * 20 < catalogTotal" value="more">加载更多 Agent…</option>
          </select>
        </label>
        <span class="chat-action-spacer"></span>
        <button v-if="streaming" class="button secondary" type="button" @click="stop">停止</button>
        <button
          v-else
          class="button"
          type="submit"
          :disabled="!draft.trim() || !selectedAgent || loadingHistory"
        >
          发送
        </button>
      </div>
    </form>
  </section>
</template>
<script setup lang="ts">
  import { computed, nextTick, onBeforeUnmount, reactive, ref, watch } from 'vue'
  import { RouterLink, useRoute, useRouter } from 'vue-router'
  import MarkdownIt from 'markdown-it'
  import DOMPurify from 'dompurify'
  import axios from 'axios'
  import {
    chatCatalog,
    chatHistory,
    modelErrorMessage,
    type ChatConversation,
    chatHttpError,
    streamChat,
    type ChatAgent,
  } from '../agent/chat'
  import { agentAdministrator } from '../agent/state'
  import { ChatFailure } from '../agent/stream'
  import { tokenState } from '../api'
  import { useAuthStore } from '../stores/auth'

  interface Message {
    id: string
    role: 'user' | 'assistant'
    text: string
    incomplete?: boolean
    failure?: string
    activity?: { text: string; status?: string; duration?: number; callId?: string }[]
  }
  const route = useRoute(),
    router = useRouter(),
    auth = useAuthStore()
  const agents = ref<ChatAgent[]>([]),
    selected = ref<ChatAgent>()
  const selectedAgent = ref(''),
    draft = ref('')
  const messages = ref<Message[]>([]),
    streaming = ref(false),
    loadingCatalog = ref(false)
  const catalogPage = ref(1),
    catalogTotal = ref(0)
  const conversations = ref<ChatConversation[]>([]),
    historySelection = ref(''),
    historyPage = ref(1),
    historyTotal = ref(0),
    loadingHistory = ref(false),
    nextBefore = ref<number | null>(null)
  let historyAbort: AbortController | undefined, historyListAbort: AbortController | undefined
  const catalogError = ref(''),
    lastRunId = ref(''),
    status = ref(''),
    statusError = ref(false)
  const scrollBox = ref<HTMLElement>(),
    following = ref(true)
  let conversationId: string | undefined, activeAbort: AbortController | undefined
  let generation = 0,
    directoryAbort: AbortController | undefined,
    initAbort: AbortController | undefined
  const conversationOptions = computed(() => {
    const id = historySelection.value
    return id &&
      id !== 'more' &&
      id !== 'new' &&
      !conversations.value.some((item) => item.id === id)
      ? [{ id, title: '当前对话' }, ...conversations.value]
      : conversations.value
  })
  const agentOptions = computed(() =>
    selected.value && !agents.value.some((item) => item.key === selected.value?.key)
      ? [selected.value, ...agents.value]
      : agents.value,
  )
  const md = new MarkdownIt({ html: false, linkify: true, breaks: true })
  // 图片以文字显示；外部链接隔离 opener。代码复制只读取清理后的 DOM 文本。
  md.renderer.rules.image = (tokens, index) => md.utils.escapeHtml(tokens[index]?.content || '')
  const fence = md.renderer.rules.fence!
  md.renderer.rules.fence = (...args) =>
    '<div class="chat-code"><button type="button" class="copy-code">复制代码</button>' +
    fence(...args) +
    '</div>'
  const linkOpen = md.renderer.rules.link_open
  md.renderer.rules.link_open = (tokens, index, options, env, self) => {
    tokens[index]?.attrSet('target', '_blank')
    tokens[index]?.attrSet('rel', 'noopener noreferrer')
    return linkOpen
      ? linkOpen(tokens, index, options, env, self)
      : self.renderToken(tokens, index, options)
  }
  function render(text: string) {
    return DOMPurify.sanitize(md.render(text), {
      FORBID_TAGS: ['style', 'script', 'iframe', 'object', 'form'],
      FORBID_ATTR: ['style'],
      ADD_ATTR: ['target'],
    })
  }
  const targetKey = () => (typeof route.query.agent_key === 'string' ? route.query.agent_key : '')
  function directoryError(error: unknown) {
    return axios.isAxiosError(error) && error.response
      ? chatHttpError(error.response.status)
      : '无法加载聊天配置，请检查网络后重试'
  }
  async function loadAgents(page = 1) {
    directoryAbort?.abort()
    const controller = new AbortController()
    directoryAbort = controller
    loadingCatalog.value = true
    try {
      const { data } = await chatCatalog.agents(page, '', controller.signal)
      if (controller.signal.aborted) return
      agents.value = page === 1 ? data.items : [...agents.value, ...data.items]
      catalogPage.value = data.page.page
      catalogTotal.value = data.page.total
      return true
    } catch (error) {
      if (!controller.signal.aborted) catalogError.value = directoryError(error)
    } finally {
      if (directoryAbort === controller) loadingCatalog.value = false
    }
  }
  async function initialize() {
    initAbort?.abort()
    const controller = new AbortController()
    initAbort = controller
    newConversation()
    selectedAgent.value = ''
    selected.value = undefined
    catalogError.value = ''
    void loadHistory()
    const loaded = await loadAgents()
    if (!loaded || controller.signal.aborted) return
    loadingCatalog.value = true
    try {
      const target = targetKey()
      let candidate = target
        ? agents.value.find((item) => item.key === target)
        : agents.value.find((item) => item.is_default)
      if (target && !candidate) {
        if (!/^[a-z][a-z0-9_]{1,63}$/.test(target)) throw new ChatFailure('对话目标 Agent 标识无效')
        // search 是子串过滤；精确 key 可能排在匹配结果的后续页。
        for (let page = 1; page <= 100; page++) {
          const { data } = await chatCatalog.agents(page, target, controller.signal)
          candidate = data.items.find((item) => item.key === target)
          if (candidate || page * 20 >= data.page.total) break
          if (page === 100) throw new ChatFailure('匹配目录过大，无法确认目标，请缩小目录后重试')
        }
      } else if (!target && !candidate) {
        // 部署默认 Agent 可能不在第一页。扫描有界分页，保留第一页供用户浏览。
        for (let page = 2; (page - 1) * 20 < catalogTotal.value && page <= 100; page++) {
          const { data } = await chatCatalog.agents(page, '', controller.signal)
          candidate = data.items.find((item) => item.is_default)
          if (candidate) break
        }
      }
      if (controller.signal.aborted) return
      if (!candidate) {
        catalogError.value = target
          ? '指定 Agent 不存在、已到期或当前账号无权使用，请选择可用 Agent。'
          : '部署默认 Agent 不可用，请选择目录中的 Agent。'
        return
      }
      selectedAgent.value = candidate.key
      selected.value = candidate
    } catch (error) {
      if (!controller.signal.aborted)
        catalogError.value = error instanceof ChatFailure ? error.message : directoryError(error)
    } finally {
      if (initAbort === controller) loadingCatalog.value = false
    }
  }
  async function agentChanged() {
    if (selectedAgent.value === 'more') {
      selectedAgent.value = selected.value?.key || ''
      await loadAgents(catalogPage.value + 1)
      return
    }
    initAbort?.abort()
    loadingCatalog.value = false
    newConversation()
    catalogError.value = ''
    selected.value = agentOptions.value.find((item) => item.key === selectedAgent.value)
    if (targetKey() !== selectedAgent.value)
      await router.replace({ query: { agent_key: selectedAgent.value } })
  }
  function newConversation() {
    historyAbort?.abort()
    loadingHistory.value = false
    historySelection.value = ''
    nextBefore.value = null
    stop()
    conversationId = undefined
    lastRunId.value = ''
    messages.value = []
    draft.value = ''
    status.value = ''
    statusError.value = false
    following.value = true
  }
  function stop() {
    ++generation
    activeAbort?.abort()
    activeAbort = undefined
    if (streaming.value) {
      streaming.value = false
      status.value = '已停止，当前回答未完成。'
      statusError.value = false
      const last = messages.value.at(-1)
      if (last?.role === 'assistant') last.incomplete = true
      if (!draft.value) draft.value = messages.value.at(-2)?.text || ''
    }
  }
  async function send() {
    const text = draft.value
    if (!text.trim() || !selectedAgent.value || streaming.value || loadingHistory.value) return
    if ([...text].length > 32000) {
      status.value = '消息最多 32,000 个字符'
      statusError.value = true
      return
    }
    const current = ++generation,
      abort = new AbortController()
    activeAbort = abort
    const assistant = reactive<Message>({
      id: crypto.randomUUID(),
      role: 'assistant',
      text: '',
      incomplete: true,
    })
    messages.value.push({ id: crypto.randomUUID(), role: 'user', text }, assistant)
    draft.value = ''
    streaming.value = true
    lastRunId.value = ''
    status.value = '正在连接…'
    statusError.value = false
    following.value = true
    void scrollBottom()
    try {
      await streamChat(
        {
          agent_key: selectedAgent.value,
          message: text,
          ...(conversationId ? { conversation_id: conversationId } : {}),
        },
        abort.signal,
        (identity) => {
          if (current === generation && !abort.signal.aborted) {
            conversationId = identity.conversationId
            lastRunId.value = identity.runId
            historySelection.value = conversationId
            void loadHistory()
          }
        },
        (event) => {
          if (current !== generation || abort.signal.aborted) return
          if (event.type === 'thinking') status.value = event.data.text || '正在回答…'
          if (event.type === 'progress' || event.type === 'reasoning_summary') {
            const text = event.data.message || event.data.text || ''
            status.value = text
            ;(assistant.activity ||= []).push({ text })
          }
          if (event.type === 'tool_call') {
            ;(assistant.activity ||= []).push({
              text: `${event.data.label}：执行中`,
              status: 'started',
              callId: event.data.call_id,
            })
          }
          if (event.type === 'tool_result') {
            const item = assistant.activity?.find((value) => value.callId === event.data.call_id)
            if (item) {
              const state =
                event.data.status === 'succeeded'
                  ? '成功'
                  : event.data.status === 'rejected'
                    ? '已拒绝'
                    : '失败'
              item.text = `${event.data.tool}：${state} · ${event.data.summary}`
              item.status = event.data.status
              item.duration = event.data.duration_ms
            }
          }
          if (event.type === 'token') {
            const delta = event.data.text || ''
            if ([...assistant.text].length + [...delta].length > 32000)
              throw new ChatFailure('回答超过字符上限，未完成')
            assistant.text += delta
            void scrollBottom()
          }
          if (event.type === 'done') {
            assistant.incomplete = false
            status.value = '回答完成'
            statusError.value = false
          }
          if (event.type === 'error') {
            status.value = modelErrorMessage(event.data.code || '')
            assistant.failure = status.value
            statusError.value = true
          }
        },
      )
    } catch (error) {
      if (current !== generation || abort.signal.aborted) return
      assistant.incomplete = true
      status.value =
        error instanceof ChatFailure ? error.message : '连接失败，回答未完成，请检查网络后重试'
      statusError.value = true
      assistant.failure = status.value
    } finally {
      if (current === generation) {
        if (assistant.incomplete && !draft.value) draft.value = text
        streaming.value = false
        activeAbort = undefined
        void loadHistory()
      }
    }
  }
  async function loadHistory(page = 1) {
    historyListAbort?.abort()
    const controller = new AbortController()
    historyListAbort = controller
    try {
      const { data } = await chatHistory.list(page, controller.signal)
      if (controller.signal.aborted) return
      const items = page === 1 ? data.items : [...conversations.value, ...data.items]
      conversations.value = [...new Map(items.map((item) => [item.id, item])).values()]
      historyPage.value = data.page.page
      historyTotal.value = data.page.total
    } catch (error) {
      if (!controller.signal.aborted)
        catalogError.value = '无法加载历史对话。' + directoryError(error)
    }
  }
  function historyMessages(
    items: Awaited<ReturnType<typeof chatHistory.messages>>['data']['items'],
  ): Message[] {
    return items.flatMap((item) => {
      const result: Message[] = [
        {
          id: String(item.id),
          role: item.role,
          text: item.content,
          activity:
            item.role === 'assistant'
              ? item.tool_steps?.map((step) => ({
                  text: `${step.tool_name}：${step.status === 'succeeded' ? '成功' : step.status === 'rejected' ? '已拒绝' : '失败'} · ${step.summary}`,
                  status: step.status,
                  duration: step.duration_ms,
                  callId: step.call_id,
                }))
              : undefined,
        },
      ]
      if (item.role === 'user' && item.status && item.status !== 'COMPLETED') {
        result.push({
          id: 'failure-' + item.id,
          activity: item.tool_steps?.map((step) => ({
            text: `${step.tool_name}：${step.status === 'succeeded' ? '成功' : step.status === 'rejected' ? '已拒绝' : '失败'} · ${step.summary}`,
            status: step.status,
            duration: step.duration_ms,
            callId: step.call_id,
          })),
          role: 'assistant',
          text: '',
          incomplete: true,
          failure:
            item.status === 'RUNNING'
              ? '此前的回答仍在运行，请稍后重试。'
              : item.status === 'CANCELLED' || item.status === 'INTERRUPTED'
                ? '此前的回答已停止，未保存完整回答。'
                : modelErrorMessage(item.error_code || ''),
        })
      }
      return result
    })
  }
  async function historyChanged() {
    const id = historySelection.value
    if (id === 'more') {
      historySelection.value = conversationId || ''
      await loadHistory(historyPage.value + 1)
      return
    }
    newConversation()
    if (!id || id === 'new') return
    initAbort?.abort()
    const controller = new AbortController()
    historyAbort = controller
    historySelection.value = id
    loadingHistory.value = true
    try {
      const { data } = await chatHistory.messages(id, controller.signal)
      if (controller.signal.aborted) return
      const key = data.conversation.agent_key || 'demo'
      selectedAgent.value = key
      selected.value = agentOptions.value.find((item) => item.key === key) || {
        key,
        name: '历史 Agent',
        is_default: false,
      }
      conversationId = id
      messages.value = historyMessages(data.items)
      nextBefore.value = data.next_before
      if (!controller.signal.aborted) {
        await router.replace({ query: { agent_key: key } })
        void scrollBottom()
      }
    } catch (error) {
      if (!controller.signal.aborted) {
        historySelection.value = ''
        status.value = directoryError(error)
        statusError.value = true
      }
    } finally {
      if (historyAbort === controller) loadingHistory.value = false
    }
  }
  async function loadOlder() {
    if (!conversationId || !nextBefore.value || loadingHistory.value) return
    const controller = new AbortController()
    historyAbort?.abort()
    historyAbort = controller
    loadingHistory.value = true
    const box = scrollBox.value,
      height = box?.scrollHeight || 0,
      top = box?.scrollTop || 0
    try {
      const { data } = await chatHistory.messages(
        conversationId,
        controller.signal,
        nextBefore.value,
      )
      if (controller.signal.aborted) return
      const existing = new Set(messages.value.map((item) => item.id))
      messages.value.unshift(
        ...historyMessages(data.items).filter((item) => !existing.has(item.id)),
      )
      nextBefore.value = data.next_before
      await nextTick()
      if (box) box.scrollTop = top + box.scrollHeight - height
    } catch (error) {
      if (!controller.signal.aborted) {
        status.value = directoryError(error)
        statusError.value = true
      }
    } finally {
      if (historyAbort === controller) loadingHistory.value = false
    }
  }
  function keyDown(event: KeyboardEvent) {
    if (
      !event.isComposing &&
      event.keyCode !== 229 &&
      (event.ctrlKey || event.metaKey) &&
      event.key === 'Enter'
    ) {
      event.preventDefault()
      void send()
    }
  }
  function trackScroll() {
    const box = scrollBox.value
    if (box) following.value = box.scrollHeight - box.clientHeight - box.scrollTop < 70
  }
  async function scrollBottom() {
    if (!following.value) return
    await nextTick()
    if (following.value && scrollBox.value) scrollBox.value.scrollTop = scrollBox.value.scrollHeight
  }
  async function copy(text: string) {
    try {
      await navigator.clipboard.writeText(text)
      status.value = '已复制'
      statusError.value = false
    } catch {
      status.value = '复制失败，请手动选择文本复制'
      statusError.value = true
    }
  }
  function copyCode(event: MouseEvent) {
    const target = event.target as HTMLElement
    if (target.closest('.copy-code'))
      void copy(target.closest('.chat-code')?.querySelector('pre code')?.textContent || '')
  }
  watch(
    () => route.query.agent_key,
    () => {
      if (targetKey() !== selectedAgent.value) void initialize()
    },
  )
  watch(
    () =>
      `${auth.user?.id || ''}:${auth.user?.roles.join(',') || ''}:${auth.profileError}:${Boolean(tokenState.access)}`,
    () => {
      stop()
      conversationId = undefined
      messages.value = []
      draft.value = ''
      historyAbort?.abort()
      historyListAbort?.abort()
      conversations.value = []
      historySelection.value = ''
      nextBefore.value = null
      loadingHistory.value = false
      directoryAbort?.abort()
      initAbort?.abort()
      selectedAgent.value = ''
      selected.value = undefined
      agents.value = []
      if (auth.user && !auth.profileError && tokenState.access) void initialize()
    },
    { flush: 'sync' },
  )
  onBeforeUnmount(() => {
    stop()
    historyAbort?.abort()
    historyListAbort?.abort()
    directoryAbort?.abort()
    initAbort?.abort()
  })
  void initialize()
</script>

<style scoped>
  .chat-activity {
    margin: 0.5rem 0;
    padding: 0.75rem;
    border: 1px solid var(--border, #dbe1e8);
    border-radius: 0.75rem;
    font-size: 0.875rem;
  }
  .chat-activity summary {
    cursor: pointer;
  }
  .chat-activity ol {
    padding-left: 1.5rem;
    line-height: 1.8;
  }
  .chat-activity .failed,
  .chat-activity .rejected {
    color: #b42318;
  }
  .chat-activity .succeeded {
    color: #187043;
  }
</style>
