<template>
  <section class="agent-page agent-chat-page">
    <div class="page-head">
      <div>
        <span class="eyebrow">Agent</span>
        <h1>对话</h1>
      </div>
      <button class="button secondary" type="button" @click="newConversation">新对话</button>
    </div>
    <div class="chat-toolbar">
      <label
        >Agent
        <select
          v-model="selectedAgent"
          aria-label="选择 Agent"
          :disabled="loadingCatalog"
          @change="agentChanged"
        >
          <option value="" disabled>选择 Agent</option>
          <option v-for="item in agentOptions" :key="item.key" :value="item.key">
            {{ item.name }}（{{ item.key }}）
          </option>
        </select>
      </label>
      <label
        >Skill
        <select
          v-model="selectedSkill"
          aria-label="选择 Skill"
          :disabled="streaming || !selectedAgent || loadingSkills"
        >
          <option v-for="item in skills" :key="item.key" :value="item.key">
            {{ item.name }}（{{ item.key }}）
          </option>
        </select>
      </label>
      <span v-if="loadingCatalog || loadingSkills" class="muted">正在加载配置…</span>
    </div>
    <p v-if="skills.find((item) => item.key === selectedSkill)?.description" class="muted">
      {{ skills.find((item) => item.key === selectedSkill)?.description }}
    </p>
    <form class="chat-directory" @submit.prevent="loadAgents(1)">
      <input
        v-model="search"
        aria-label="搜索 Agent"
        maxlength="64"
        placeholder="按名称或标识搜索 Agent"
      />
      <button class="secondary" :disabled="loadingCatalog" type="submit">搜索 Agent</button>
      <button
        class="secondary"
        :disabled="loadingCatalog || catalogPage <= 1"
        type="button"
        @click="loadAgents(catalogPage - 1)"
      >
        上一页
      </button>
      <span>{{ catalogPage }} / {{ Math.max(1, Math.ceil(catalogTotal / 20)) }}</span>
      <button
        class="secondary"
        :disabled="loadingCatalog || catalogPage * 20 >= catalogTotal"
        type="button"
        @click="loadAgents(catalogPage + 1)"
      >
        下一页
      </button>
    </form>
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
      <div v-if="!messages.length" class="chat-empty">
        选择 Agent 后发送消息，回答会以流式方式显示。
      </div>
      <article
        v-for="message in messages"
        :key="message.id"
        class="chat-message"
        :class="message.role"
      >
        <div class="chat-role">{{ message.role === 'user' ? '我' : 'Agent' }}</div>
        <div
          v-if="message.role === 'assistant'"
          class="chat-content markdown"
          v-html="render(message.text)"
        ></div>
        <div v-else class="chat-content">{{ message.text }}</div>
        <span v-if="message.incomplete" class="chat-incomplete">回答未完成</span>
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
    <p class="chat-note">页面内可连续对话。当前模型只处理本次消息，尚不读取历史上下文。</p>
    <p v-if="status" class="chat-status" :class="{ error: statusError }" role="status">
      {{ status }}
    </p>
    <form class="chat-compose" @submit.prevent="send">
      <textarea
        v-model="draft"
        aria-label="消息"
        rows="3"
        :disabled="streaming || !selectedAgent"
        placeholder="输入消息，按 Ctrl/⌘ + Enter 发送"
        @keydown="keyDown"
      ></textarea>
      <div class="chat-actions">
        <button v-if="streaming" class="button secondary" type="button" @click="stop">停止</button>
        <button
          v-else
          class="button"
          type="submit"
          :disabled="!draft.trim() || !selectedAgent || !selectedSkill || loadingSkills"
        >
          发送
        </button>
      </div>
    </form>
  </section>
</template>
<script setup lang="ts">
  import { computed, nextTick, onBeforeUnmount, reactive, ref, watch } from 'vue'
  import { useRoute, useRouter } from 'vue-router'
  import MarkdownIt from 'markdown-it'
  import DOMPurify from 'dompurify'
  import axios from 'axios'
  import {
    chatCatalog,
    chatHttpError,
    streamChat,
    type ChatAgent,
    type ChatSkill,
  } from '../agent/chat'
  import { ChatFailure } from '../agent/stream'
  import { tokenState } from '../api'
  import { useAuthStore } from '../stores/auth'

  interface Message {
    id: string
    role: 'user' | 'assistant'
    text: string
    incomplete?: boolean
  }
  const route = useRoute(),
    router = useRouter(),
    auth = useAuthStore()
  const agents = ref<ChatAgent[]>([]),
    skills = ref<ChatSkill[]>([]),
    selected = ref<ChatAgent>()
  const selectedAgent = ref(''),
    selectedSkill = ref(''),
    draft = ref('')
  const messages = ref<Message[]>([]),
    streaming = ref(false),
    loadingCatalog = ref(false),
    loadingSkills = ref(false)
  const search = ref(''),
    catalogPage = ref(1),
    catalogTotal = ref(0)
  const catalogError = ref(''),
    status = ref(''),
    statusError = ref(false)
  const scrollBox = ref<HTMLElement>(),
    following = ref(true)
  let conversationId: string | undefined, activeAbort: AbortController | undefined
  let generation = 0,
    directoryAbort: AbortController | undefined,
    skillAbort: AbortController | undefined,
    initAbort: AbortController | undefined
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
      const { data } = await chatCatalog.agents(page, search.value.trim(), controller.signal)
      if (controller.signal.aborted) return
      agents.value = data.items
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
    skills.value = []
    selectedSkill.value = ''
    catalogError.value = ''
    search.value = ''
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
      await loadSkills(candidate.key)
    } catch (error) {
      if (!controller.signal.aborted)
        catalogError.value = error instanceof ChatFailure ? error.message : directoryError(error)
    } finally {
      if (initAbort === controller) loadingCatalog.value = false
    }
  }
  async function loadSkills(key: string) {
    skillAbort?.abort()
    const controller = new AbortController()
    skillAbort = controller
    skills.value = []
    selectedSkill.value = ''
    loadingSkills.value = true
    try {
      const { data } = await chatCatalog.skills(key, controller.signal)
      if (controller.signal.aborted) return
      skills.value = data.items.filter((item) => item.execution_mode === 'direct')
      selectedSkill.value =
        skills.value.find((item) => item.key === data.default_skill_key)?.key || ''
      if (!selectedSkill.value)
        catalogError.value = '此 Agent 没有可用于当前运行时的默认 direct Skill。'
    } catch (error) {
      if (!controller.signal.aborted) catalogError.value = directoryError(error)
    } finally {
      if (skillAbort === controller) loadingSkills.value = false
    }
  }
  async function agentChanged() {
    initAbort?.abort()
    loadingCatalog.value = false
    newConversation()
    catalogError.value = ''
    selected.value = agentOptions.value.find((item) => item.key === selectedAgent.value)
    void loadSkills(selectedAgent.value)
    if (targetKey() !== selectedAgent.value)
      await router.replace({ query: { agent_key: selectedAgent.value } })
  }
  function newConversation() {
    stop()
    conversationId = undefined
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
    if (
      !text.trim() ||
      !selectedAgent.value ||
      !selectedSkill.value ||
      streaming.value ||
      loadingSkills.value
    )
      return
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
    status.value = '正在连接…'
    statusError.value = false
    following.value = true
    void scrollBottom()
    try {
      await streamChat(
        {
          agent_key: selectedAgent.value,
          skill_key: selectedSkill.value,
          message: text,
          ...(conversationId ? { conversation_id: conversationId } : {}),
        },
        abort.signal,
        (identity) => {
          if (current === generation && !abort.signal.aborted)
            conversationId = identity.conversationId
        },
        (event) => {
          if (current !== generation || abort.signal.aborted) return
          if (event.type === 'thinking') status.value = event.data.text || '正在回答…'
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
            status.value = `回答失败（${event.data.code}），部分回答未完成，请重新发送或新建对话`
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
    } finally {
      if (current === generation) {
        if (assistant.incomplete && !draft.value) draft.value = text
        streaming.value = false
        activeAbort = undefined
      }
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
      directoryAbort?.abort()
      skillAbort?.abort()
      initAbort?.abort()
      selectedAgent.value = ''
      selectedSkill.value = ''
      selected.value = undefined
      agents.value = []
      skills.value = []
      if (auth.user && !auth.profileError && tokenState.access) void initialize()
    },
    { flush: 'sync' },
  )
  onBeforeUnmount(() => {
    stop()
    directoryAbort?.abort()
    skillAbort?.abort()
    initAbort?.abort()
  })
  void initialize()
</script>
