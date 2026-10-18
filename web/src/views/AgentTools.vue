<template>
  <section class="agent-page">
    <div class="page-head">
      <div>
        <span class="eyebrow">Agent 管理</span>
        <h1>工具</h1>
      </div>
    </div>
    <p v-if="error" class="error agent-alert" role="alert">{{ error }}</p>
    <div v-if="loading" class="loading-panel">加载中…</div>
    <div v-else class="tool-admin-layout">
      <div class="tool-list" role="listbox" aria-label="工具目录">
        <button
          v-for="tool in tools"
          :key="tool.name"
          class="tool-list-item"
          :class="{ active: selected?.name === tool.name }"
          type="button"
          role="option"
          :aria-selected="selected?.name === tool.name"
          @click="select(tool)"
        >
          <strong>{{ tool.name }}</strong>
          <span>{{ tool.description }}</span>
        </button>
        <p v-if="!tools.length" class="empty-panel">暂无已注册工具</p>
      </div>
      <article v-if="selected" class="tool-detail">
        <header class="tool-detail-head">
          <div><span class="eyebrow">注册工具</span><h2>{{ selected.name }}</h2></div>
          <span class="status-chip" :class="{ 'agent-chip-muted': !selected.enabled }">
            {{ selected.enabled ? '已启用' : '已停用' }}
          </span>
        </header>
        <form @submit.prevent="save">
          <label class="tool-description-field">
            工具描述
            <textarea v-model="description" maxlength="1000" rows="4" required />
          </label>
          <div class="tool-detail-grid">
            <div><span>读取范围</span><strong>{{ selected.read_scope }}</strong></div>
            <div><span>数据敏感级别</span><strong>{{ selected.sensitivity }}</strong></div>
            <div><span>副作用</span><strong>{{ selected.side_effect }}</strong></div>
            <div><span>超时上限</span><strong>{{ selected.timeout_seconds }} 秒</strong></div>
            <div><span>需要确认</span><strong>{{ selected.requires_confirmation ? '是' : '否' }}</strong></div>
            <div><span>允许角色</span><strong>{{ selected.allowed_roles.join('、') || '不限' }}</strong></div>
          </div>
          <label class="tool-schema-field">输入参数 Schema<pre>{{ format(selected.input_schema) }}</pre></label>
          <label class="tool-schema-field">输出结果 Schema<pre>{{ format(selected.output_schema) }}</pre></label>
          <div class="tool-form-actions">
            <span v-if="saved" role="status">已保存</span>
            <button class="button" type="submit" :disabled="saving || description === selected.description">
              <Save :size="16" />{{ saving ? '保存中…' : '保存描述' }}
            </button>
          </div>
        </form>
      </article>
      <div v-else-if="!loading" class="empty-panel">选择一个工具查看配置</div>
    </div>
  </section>
</template>

<script setup lang="ts">
  import { onMounted, ref } from 'vue'
  import { Save } from '@lucide/vue'
  import { agentApi, agentError } from '../agent/api'
  import type { Tool } from '../agent/types'

  const tools = ref<Tool[]>([])
  const selected = ref<Tool | null>(null)
  const description = ref('')
  const loading = ref(false)
  const saving = ref(false)
  const saved = ref(false)
  const error = ref('')

  function format(value: object) {
    return JSON.stringify(value, null, 2)
  }

  function select(tool: Tool) {
    selected.value = tool
    description.value = tool.description
    saved.value = false
    error.value = ''
  }

  async function load() {
    loading.value = true
    error.value = ''
    try {
      const response = await agentApi.tools()
      tools.value = response.data.items
      if (selected.value) {
        const updated = tools.value.find((tool) => tool.name === selected.value?.name)
        if (updated) select(updated)
      }
      if (!selected.value && tools.value.length) select(tools.value[0])
    } catch (cause) {
      error.value = agentError(cause)
    } finally {
      loading.value = false
    }
  }

  async function save() {
    if (!selected.value) return
    saving.value = true
    saved.value = false
    error.value = ''
    try {
      const response = await agentApi.saveToolDescription(selected.value.name, description.value)
      selected.value = response.data.tool
      tools.value = tools.value.map((tool) =>
        tool.name === selected.value?.name ? response.data.tool : tool,
      )
      description.value = response.data.tool.description
      saved.value = true
    } catch (cause) {
      error.value = agentError(cause)
    } finally {
      saving.value = false
    }
  }

  onMounted(load)
</script>

<style scoped>
  .tool-admin-layout {
    display: grid;
    grid-template-columns: minmax(210px, 0.7fr) minmax(0, 1.8fr);
    gap: 20px;
    align-items: start;
  }
  .tool-list { display: grid; gap: 4px; }
  .tool-list-item {
    display: grid;
    gap: 5px;
    width: 100%;
    padding: 12px;
    border: 1px solid transparent;
    border-radius: 6px;
    background: transparent;
    color: inherit;
    text-align: left;
    cursor: pointer;
  }
  .tool-list-item:hover, .tool-list-item.active { border-color: #d5dbe6; background: #f8f9fc; }
  .tool-list-item span { color: #687188; font-size: 13px; line-height: 1.4; }
  .tool-detail { min-width: 0; padding: 20px; border: 1px solid #e1e5ee; border-radius: 6px; background: #fff; }
  .tool-detail-head, .tool-form-actions { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
  .tool-detail-head { margin-bottom: 18px; }
  .tool-detail h2 { margin: 3px 0 0; font-size: 20px; }
  .tool-description-field, .tool-schema-field { display: grid; gap: 8px; margin-top: 16px; font-weight: 600; }
  .tool-description-field textarea { width: 100%; min-height: 96px; resize: vertical; }
  .tool-detail-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 12px; margin-top: 18px; }
  .tool-detail-grid div { display: grid; gap: 5px; min-width: 0; }
  .tool-detail-grid span { color: #687188; font-size: 12px; }
  .tool-detail-grid strong { overflow-wrap: anywhere; font-size: 14px; }
  .tool-schema-field pre { max-height: 240px; overflow: auto; margin: 0; padding: 12px; border: 1px solid #e7eaf0; border-radius: 4px; background: #f8f9fc; font-size: 12px; font-weight: 400; white-space: pre-wrap; overflow-wrap: anywhere; }
  .tool-form-actions { margin-top: 20px; }
  .tool-form-actions span { color: var(--success-color, #15803d); }
  @media (max-width: 700px) {
    .tool-admin-layout { grid-template-columns: 1fr; }
    .tool-list { grid-template-columns: repeat(2, minmax(0, 1fr)); }
    .tool-detail { padding: 14px; }
    .tool-detail-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  }
</style>
