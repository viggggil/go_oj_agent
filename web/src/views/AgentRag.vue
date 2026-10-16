<template>
  <section class="agent-page rag-page">
    <div class="page-head">
      <div>
        <span class="eyebrow">知识库管理</span>
        <h1>RAG</h1>
        <p>按分组管理模型参考资料，题解、算法讲义和平台文档均可上传。</p>
      </div>
    </div>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <p v-if="notice" class="notice" role="status">{{ notice }}</p>
    <KnowledgeEvidence />
    <div class="rag-layout">
      <aside class="rag-groups">
        <div class="section-head">
          <h2>知识分组</h2>
          <button
            class="secondary"
            :disabled="busy || groupsLoading"
            @click="loadGroups(groupPage.page)"
          >
            刷新分组
          </button>
        </div>
        <form @submit.prevent="saveGroup(false)">
          <label
            >新分组名称<input
              v-model="groupName"
              required
              maxlength="128"
              :disabled="busy" /></label
          ><button :disabled="busy || !groupName.trim()">创建分组</button>
        </form>
        <p v-if="groupsLoading" class="muted">加载分组中…</p>
        <p v-else-if="!groups.length" class="muted">还没有分组，先创建一个，再上传文档。</p>
        <div class="rag-group-list">
          <button
            v-for="group in groups"
            :key="group.id"
            :class="{ secondary: selected?.id !== group.id }"
            :aria-pressed="selected?.id === group.id"
            :disabled="busy || documentsLoading"
            @click="selectGroup(group)"
          >
            {{ group.name }}
          </button>
        </div>
        <div v-if="groupPage.total > groupPage.page_size" class="pager">
          <button
            class="secondary"
            :disabled="groupsLoading || busy || groupPage.page <= 1"
            @click="loadGroups(groupPage.page - 1)"
          >
            上一页分组</button
          ><span>{{ groupPage.page }}</span
          ><button
            class="secondary"
            :disabled="
              groupsLoading || busy || groupPage.page * groupPage.page_size >= groupPage.total
            "
            @click="loadGroups(groupPage.page + 1)"
          >
            下一页分组
          </button>
        </div>
      </aside>
      <div v-if="selected" class="rag-content">
        <div class="section-head">
          <div>
            <h2>{{ selected.name }}</h2>
            <p class="muted">Agent 按此分组绑定访问范围。新文档索引就绪后需要启用。</p>
          </div>
          <button :disabled="busy" @click="newDocument">上传文档</button>
        </div>
        <details>
          <summary>修改分组名称</summary>
          <form class="rag-inline-form" @submit.prevent="saveGroup(true)">
            <label
              >分组名称<input v-model="rename" required maxlength="128" :disabled="busy" /></label
            ><button :disabled="busy || !rename.trim() || rename === selected.name">
              保存分组名称
            </button>
          </form>
        </details>
        <div class="section-head">
          <h3>文档</h3>
          <button
            class="secondary"
            :disabled="busy || documentsLoading"
            @click="loadDocuments(documentPage.page)"
          >
            刷新文档与索引状态
          </button>
        </div>
        <p v-if="documentsLoading" class="muted">加载文档中…</p>
        <p v-else-if="!documents.length" class="empty-panel compact">此分组还没有文档。</p>
        <div v-else class="rag-document-list">
          <article v-for="doc in documents" :key="doc.document_id" class="rag-document">
            <div>
              <h3>{{ doc.title }}</h3>
              <p>
                {{ kinds[doc.kind]
                }}<span v-if="doc.problem_id"> · 题号 {{ doc.problem_id }}</span> ·
                {{ indexState[doc.state] }} · {{ doc.enabled ? '已启用' : '未启用' }}
              </p>
              <p class="muted">
                {{ doc.chunk_count }} 个片段 · {{ doc.size_bytes }} 字节<span
                  v-if="doc.active_version && doc.active_version !== doc.id"
                >
                  · 仍使用上一发布版本</span
                >
              </p>
              <p v-if="doc.error_code" class="error">{{ indexError(doc.error_code) }}</p>
            </div>
            <div class="agent-actions">
              <button class="secondary" :disabled="busy" @click="openDocument(doc.document_id)">
                查看与编辑</button
              ><button
                v-if="doc.state === 'FAILED'"
                class="secondary"
                :disabled="busy"
                @click="changeState(doc, 'retry')"
              >
                重试索引</button
              ><button
                v-if="!doc.enabled"
                :disabled="busy || !doc.active_version"
                @click="changeState(doc, 'enable')"
              >
                启用</button
              ><button v-else class="danger" :disabled="busy" @click="changeState(doc, 'archive')">
                归档
              </button>
            </div>
          </article>
        </div>
        <div v-if="documentPage.total" class="pager">
          <button
            class="secondary"
            :disabled="busy || documentsLoading || documentPage.page <= 1"
            @click="loadDocuments(documentPage.page - 1)"
          >
            上一页文档</button
          ><span>第 {{ documentPage.page }} 页，共 {{ documentPage.total }} 篇</span
          ><button
            class="secondary"
            :disabled="
              busy ||
              documentsLoading ||
              documentPage.page * documentPage.page_size >= documentPage.total
            "
            @click="loadDocuments(documentPage.page + 1)"
          >
            下一页文档
          </button>
        </div>
        <section class="rag-search">
          <h2>检索测试</h2>
          <p class="muted">仅在当前分组中搜索已启用、已发布的文档，结果只对管理员展示。</p>
          <form @submit.prevent="search">
            <label
              >检索问题<input v-model="query" required maxlength="400" :disabled="busy" /></label
            ><label
              >限定题号（选填）<input
                v-model="searchProblem"
                type="number"
                min="1"
                step="1"
                :disabled="busy" /></label
            ><button :disabled="busy || !query.trim()">测试检索</button>
          </form>
          <p v-if="searched && !matches.length" class="muted">
            没有匹配的已发布资料。请检查分组、启用状态和题号。
          </p>
          <article
            v-for="match in matches"
            :key="match.version_id + ':' + match.number"
            class="rag-match"
          >
            <h3>{{ match.title }}</h3>
            <p class="muted">
              相似度 {{ match.score.toFixed(3) }} · {{ match.section }} · 片段
              {{ match.number + 1 }}
            </p>
            <pre>{{ match.text }}</pre>
            <details>
              <summary>来源版本与位置</summary>
              <p class="mono">{{ match.version_id }}</p>
              <p>字符 {{ match.start }}–{{ match.end }}</p>
            </details>
          </article>
        </section>
      </div>
    </div>
    <dialog ref="editor" class="agent-dialog rag-editor" @cancel.prevent="closeEditor">
      <div class="section-head">
        <h2>{{ current ? '查看与编辑文档' : '上传文档' }}</h2>
        <button class="secondary" :disabled="busy" @click="closeEditor">关闭文档</button>
      </div>
      <p v-if="editorError" class="error" role="alert">
        {{ editorError
        }}<button
          v-if="editorStatus === 409 && current"
          type="button"
          class="link"
          :disabled="busy"
          @click="reloadCurrent"
        >
          重新加载当前文档
        </button>
      </p>
      <form @submit.prevent="saveDocument">
        <fieldset class="agent-form-body" :disabled="busy">
          <p>
            所属分组：<strong>{{ selected?.name }}</strong>
          </p>
          <label
            >选择文件（UTF-8 Markdown / TXT，最多 120 KB）<input
              type="file"
              accept=".md,.txt,text/plain,text/markdown"
              @change="readFile"
          /></label>
          <div class="agent-field-grid">
            <label>文档标题<input v-model="form.title" required maxlength="128" /></label
            ><label
              >文档类型<select v-model="form.kind">
                <option value="editorial">题解</option>
                <option value="lecture">算法讲义</option>
                <option value="platform">平台文档</option>
              </select></label
            ><label
              >关联题号（选填）<input v-model="problem" type="number" min="1" step="1" /></label
            ><label>来源地址（选填）<input v-model="form.source_url" maxlength="512" /></label>
          </div>
          <label>标签（逗号分隔，最多 20 个）<input v-model="tags" /></label
          ><label>文件名<input v-model="form.filename" required maxlength="128" /></label
          ><label
            >正文<textarea
              v-model="form.content"
              class="agent-prompt"
              required
              spellcheck="false"
            ></textarea>
          </label>
          <p class="muted">
            {{ contentBytes }} / 120000 字节。保存修改会创建新版本，索引完成前保留上次发布版本。
          </p>
          <div class="agent-actions">
            <button
              type="button"
              class="secondary"
              :disabled="!form.content.trim()"
              @click="preview(1)"
            >
              切分预览</button
            ><button :disabled="!form.content.trim()">
              {{ busy ? '处理中…' : current ? '保存新版本' : '上传并索引' }}
            </button>
          </div>
        </fieldset>
      </form>
      <section v-if="previewPage.total">
        <h3>切分预览</h3>
        <p class="muted">共 {{ previewPage.total }} 个片段，以下是第 {{ previewPage.page }} 页。</p>
        <article v-for="chunk in chunks" :key="chunk.number" class="rag-match">
          <strong>{{ chunk.section }} · 片段 {{ chunk.number + 1 }}</strong>
          <p class="muted">字符 {{ chunk.start }}–{{ chunk.end }}</p>
          <pre>{{ chunk.text }}</pre>
        </article>
        <div class="pager">
          <button
            class="secondary"
            :disabled="busy || previewPage.page <= 1"
            @click="preview(previewPage.page - 1)"
          >
            上一页片段</button
          ><button
            class="secondary"
            :disabled="busy || previewPage.page * previewPage.page_size >= previewPage.total"
            @click="preview(previewPage.page + 1)"
          >
            下一页片段
          </button>
        </div>
      </section>
      <section v-if="current">
        <div class="section-head">
          <h3>文档版本</h3>
          <button class="secondary" :disabled="busy" @click="loadHistory(1)">刷新版本</button>
        </div>
        <p v-if="historyError" class="error" role="alert">{{ historyError }}</p>
        <article v-for="version in history" :key="version.id" class="rag-history">
          <strong
            >{{ version.id === current.id ? '当前编辑版本' : '历史版本'
            }}{{ version.id === version.active_version ? ' · 已发布' : '' }}</strong
          >
          <p>
            {{ version.title }} · {{ indexState[version.state] }} · {{ version.chunk_count }} 个片段
            · {{ new Date(version.created_at).toLocaleString() }}
          </p>
          <code>{{ version.id }}</code>
        </article>
        <div v-if="historyPage.total > historyPage.page_size" class="pager">
          <button
            class="secondary"
            :disabled="busy || historyPage.page <= 1"
            @click="loadHistory(historyPage.page - 1)"
          >
            上一页版本</button
          ><button
            class="secondary"
            :disabled="busy || historyPage.page * historyPage.page_size >= historyPage.total"
            @click="loadHistory(historyPage.page + 1)"
          >
            下一页版本
          </button>
        </div>
      </section>
    </dialog>
  </section>
</template>
<script setup lang="ts">
  import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
  import { onBeforeRouteLeave } from 'vue-router'
  import { agentError, agentStatus } from '../agent/api'
  import { operationIds } from '../agent/state'
  import {
    ragApi,
    indexState,
    type Chunk,
    type DocumentInput,
    type KnowledgeDocument,
    type KnowledgeGroup,
    type Match,
  } from '../agent/rag'
  import { useAuthStore } from '../stores/auth'
  import { agentAdministrator } from '../agent/state'
  import KnowledgeEvidence from '../components/agent/KnowledgeEvidence.vue'
  const auth = useAuthStore(),
    owner = auth.user!.id,
    life = new AbortController(),
    ids = operationIds()
  const groups = ref<KnowledgeGroup[]>([]),
    selected = ref<KnowledgeGroup>(),
    documents = ref<KnowledgeDocument[]>([]),
    matches = ref<Match[]>([])
  const groupPage = ref(page()),
    documentPage = ref(page()),
    historyPage = ref(page()),
    previewPage = ref(page())
  const busy = ref(false),
    groupsLoading = ref(false),
    documentsLoading = ref(false),
    error = ref(''),
    notice = ref(''),
    groupName = ref(''),
    rename = ref(''),
    query = ref(''),
    searchProblem = ref(''),
    searched = ref(false)
  const editor = ref<HTMLDialogElement>(),
    current = ref<KnowledgeDocument>(),
    editorError = ref(''),
    editorStatus = ref<number>(),
    historyError = ref(''),
    history = ref<KnowledgeDocument[]>([]),
    chunks = ref<Chunk[]>([]),
    problem = ref<string | number>(''),
    tags = ref(''),
    baseline = ref('')
  const form = ref<DocumentInput>(emptyForm()),
    kinds = { editorial: '题解', lecture: '算法讲义', platform: '平台文档' }
  const contentBytes = computed(() => new TextEncoder().encode(form.value.content).length)
  const serialize = () => JSON.stringify([form.value, problem.value, tags.value])
  const dirty = () => editor.value?.open && serialize() !== baseline.value
  const alive = () =>
    !life.signal.aborted &&
    auth.isAuthenticated &&
    auth.user?.id === owner &&
    agentAdministrator(auth.user.roles)
  function page() {
    return { page: 1, page_size: 20, total: 0 }
  }
  function emptyForm(): DocumentInput {
    return {
      base_id: '',
      title: '',
      kind: 'editorial',
      problem_id: null,
      source_url: '',
      tags: [],
      filename: 'document.md',
      content: '',
    }
  }
  function indexError(code: string) {
    return code === 'AGENT_RAG_RETRIES_EXHAUSTED'
      ? '索引重试次数已耗尽，请检查服务后重试。'
      : '索引暂未成功，可刷新状态或在失败后重试。'
  }
  async function failed(cause: unknown, inEditor = false) {
    if (!alive()) return
    if (inEditor) {
      editorError.value = agentError(cause)
      editorStatus.value = agentStatus(cause)
    } else error.value = agentError(cause)
    if (agentStatus(cause) === 403) await auth.fetchProfile().catch(() => undefined)
  }
  async function loadGroups(number = 1) {
    if (groupsLoading.value) return
    groupsLoading.value = true
    error.value = ''
    try {
      const { data } = await ragApi.groups(number, life.signal)
      if (!alive()) return
      groups.value = data.items
      groupPage.value = data.page
      const refreshed = data.items.find((item) => item.id === selected.value?.id)
      if (refreshed && selected.value) {
        if (rename.value === selected.value.name) rename.value = refreshed.name
        selected.value = refreshed
      }
      if (!selected.value && data.items[0]) await selectGroup(data.items[0])
    } catch (cause) {
      await failed(cause)
    } finally {
      if (alive()) groupsLoading.value = false
    }
  }
  async function selectGroup(group: KnowledgeGroup) {
    if (busy.value || documentsLoading.value) return
    documents.value = []
    selected.value = group
    rename.value = group.name
    matches.value = []
    searched.value = false
    error.value = ''
    notice.value = ''
    await loadDocuments(1)
  }
  async function loadDocuments(number = 1) {
    if (!selected.value || documentsLoading.value) return
    documentsLoading.value = true
    error.value = ''
    try {
      const { data } = await ragApi.documents(selected.value.id, number, life.signal)
      if (!alive()) return
      documents.value = data.items
      documentPage.value = data.page
    } catch (cause) {
      await failed(cause)
    } finally {
      if (alive()) documentsLoading.value = false
    }
  }
  async function saveGroup(isRename: boolean) {
    if (busy.value || groupsLoading.value || documentsLoading.value || !alive()) return
    busy.value = true
    error.value = ''
    notice.value = ''
    try {
      const name = (isRename ? rename.value : groupName.value).trim()
      const request = {
        action: isRename ? 'rename' : 'create',
        id: isRename ? selected.value?.id : null,
        name,
        expected: isRename ? selected.value?.name : null,
      }
      const { data } =
        isRename && selected.value
          ? await ragApi.renameGroup(
              selected.value.id,
              name,
              selected.value.name,
              ids.for(request),
              life.signal,
            )
          : await ragApi.createGroup(name, ids.for(request), life.signal)
      if (!alive()) return
      ids.clear()
      selected.value = data.knowledge_base
      rename.value = data.knowledge_base.name
      groupName.value = ''
      matches.value = []
      searched.value = false
      await loadGroups(1)
      await loadDocuments(1)
      notice.value = isRename
        ? '分组名称已更新，Agent 绑定保持有效。'
        : '分组已创建，可以上传文档。'
    } catch (cause) {
      await failed(cause)
    } finally {
      if (alive()) busy.value = false
    }
  }
  async function newDocument() {
    current.value = undefined
    form.value = emptyForm()
    form.value.base_id = selected.value!.id
    problem.value = ''
    tags.value = ''
    history.value = []
    chunks.value = []
    previewPage.value = page()
    editorError.value = ''
    editorStatus.value = undefined
    historyError.value = ''
    baseline.value = serialize()
    await nextTick()
    editor.value?.showModal()
  }
  async function openDocument(id: string) {
    if (busy.value) return
    busy.value = true
    error.value = ''
    try {
      const { data } = await ragApi.document(id, life.signal)
      if (!alive()) return
      current.value = data.document
      const doc = data.document
      form.value = {
        base_id: doc.base_id,
        document_id: doc.document_id,
        expected_version: doc.id,
        title: doc.title,
        kind: doc.kind,
        problem_id: doc.problem_id,
        source_url: doc.source_url,
        tags: [...doc.tags],
        filename: doc.filename,
        content: doc.content || '',
      }
      problem.value = doc.problem_id || ''
      tags.value = doc.tags.join(', ')
      history.value = []
      chunks.value = []
      previewPage.value = page()
      editorError.value = ''
      editorStatus.value = undefined
      historyError.value = ''
      baseline.value = serialize()
      await nextTick()
      if (!editor.value?.open) editor.value?.showModal()
      await loadHistory(1)
    } catch (cause) {
      await failed(cause, Boolean(editor.value?.open))
    } finally {
      if (alive()) busy.value = false
    }
  }
  async function reloadCurrent() {
    if (dirty() && !window.confirm('重新加载会替换尚未保存的输入，继续吗？')) return
    if (current.value) await openDocument(current.value.document_id)
  }
  function closeEditor() {
    if (busy.value || (dirty() && !window.confirm('文档尚未保存，放弃修改吗？'))) return
    editor.value?.close()
  }
  function problemId(value: string | number): number | null {
    if (value === '') return null
    const number = Number(value)
    if (!Number.isSafeInteger(number) || number <= 0) throw new Error('invalid-problem')
    return number
  }
  async function readFile(event: Event) {
    const input = event.target as HTMLInputElement,
      file = input.files?.[0]
    if (!file) return
    editorError.value = ''
    busy.value = true
    try {
      if (file.size > 120000 || !/\.(md|txt)$/i.test(file.name)) throw new Error('invalid-file')
      const content = new TextDecoder('utf-8', { fatal: true }).decode(await file.arrayBuffer())
      if (!alive()) return
      if (!content.trim() || content.includes('\0')) throw new Error('invalid-file')
      form.value.content = content
      form.value.filename = file.name
      if (!form.value.title) form.value.title = file.name.replace(/\.(md|txt)$/i, '')
    } catch {
      if (alive())
        editorError.value = '请选择有效的 UTF-8 Markdown / TXT 文件，大小不超过 120000 字节。'
    } finally {
      if (alive()) busy.value = false
      input.value = ''
    }
  }
  async function saveDocument() {
    if (busy.value) return
    editorError.value = ''
    editorStatus.value = undefined
    let body: DocumentInput
    try {
      const values = tags.value
        .split(/[,，]/)
        .map((value) => value.trim())
        .filter(Boolean)
      if (
        !form.value.content.trim() ||
        form.value.content.includes('\0') ||
        contentBytes.value > 120000 ||
        !form.value.title.trim() ||
        !/^[^/\\]+\.(md|txt)$/i.test(form.value.filename) ||
        values.length > 20 ||
        new Set(values).size !== values.length ||
        values.some((value) => [...value].length > 64)
      )
        throw new Error('invalid-document')
      body = { ...form.value, problem_id: problemId(problem.value), tags: values }
    } catch {
      editorError.value = '请检查题号、文件名、标签和正文；正文上限为 120000 字节。'
      return
    }
    busy.value = true
    try {
      await ragApi.upload(body, ids.for({ action: 'upload', body }), life.signal)
      if (!alive()) return
      ids.clear()
      baseline.value = serialize()
      editor.value?.close()
      notice.value = '文档已保存，正在等待索引。可刷新状态，索引就绪后启用。'
      await loadDocuments(1)
    } catch (cause) {
      await failed(cause, true)
    } finally {
      if (alive()) busy.value = false
    }
  }
  async function changeState(doc: KnowledgeDocument, action: 'enable' | 'archive' | 'retry') {
    if (busy.value) return
    busy.value = true
    error.value = ''
    notice.value = ''
    try {
      await ragApi.action(
        doc.document_id,
        action,
        doc.id,
        ids.for({ action, id: doc.document_id, expected: doc.id }),
        life.signal,
      )
      if (!alive()) return
      ids.clear()
      matches.value = []
      searched.value = false
      notice.value =
        action === 'retry'
          ? '已重新安排索引。'
          : action === 'enable'
            ? '文档已启用。'
            : '文档已归档，不再参与检索。'
      await loadDocuments(documentPage.value.page)
    } catch (cause) {
      await failed(cause)
    } finally {
      if (alive()) busy.value = false
    }
  }
  let historySequence = 0
  async function loadHistory(number: number) {
    if (!current.value) return
    historyError.value = ''
    const documentId = current.value.document_id,
      sequence = ++historySequence
    try {
      const { data } = await ragApi.history(documentId, number, life.signal)
      if (!alive() || current.value?.document_id !== documentId || sequence !== historySequence)
        return
      history.value = data.items
      historyPage.value = data.page
    } catch (cause) {
      if (alive() && current.value?.document_id === documentId && sequence === historySequence)
        historyError.value = agentError(cause)
    }
  }
  async function preview(number: number) {
    if (busy.value) return
    if (contentBytes.value > 120000) {
      editorError.value = '正文上限为 120000 字节。'
      return
    }
    busy.value = true
    editorError.value = ''
    try {
      const { data } = await ragApi.preview(
        form.value.content,
        number,
        crypto.randomUUID(),
        life.signal,
      )
      if (alive()) {
        chunks.value = data.chunks
        previewPage.value = data.page
      }
    } catch (cause) {
      await failed(cause, true)
    } finally {
      if (alive()) busy.value = false
    }
  }
  async function search() {
    if (busy.value || !selected.value) return
    let id: number | null
    try {
      id = problemId(searchProblem.value)
    } catch {
      error.value = '题号必须为正整数。'
      return
    }
    busy.value = true
    error.value = ''
    matches.value = []
    searched.value = false
    try {
      const { data } = await ragApi.search(
        [selected.value.id],
        query.value,
        id,
        crypto.randomUUID(),
        life.signal,
      )
      if (alive()) {
        matches.value = data.matches
        searched.value = true
      }
    } catch (cause) {
      await failed(cause)
    } finally {
      if (alive()) busy.value = false
    }
  }
  watch(
    () => form.value.content,
    () => {
      chunks.value = []
      previewPage.value = page()
    },
  )
  onBeforeRouteLeave(
    () => !busy.value && (!dirty() || window.confirm('文档尚未保存，放弃修改并离开吗？')),
  )
  onMounted(() => loadGroups())
  onBeforeUnmount(() => {
    life.abort()
    editor.value?.close()
  })
</script>
