<template>
  <fieldset>
    <legend>可访问的知识分组</legend>
    <p class="muted">按组授权，组内启用且索引就绪的文档可供模型参考；不选择时不访问知识库。</p>
    <p v-if="error" class="error" role="alert">
      {{ error }} <button type="button" class="link" @click="load(page.page)">重试</button>
    </p>
    <div class="selected-bindings">
      <div v-for="id in modelValue" :key="id" class="selected-binding">
        <span>{{ names[id] || '分组信息未加载' }}</span>
        <button
          type="button"
          class="secondary"
          :aria-label="'移除分组 ' + (names[id] || id)"
          @click="toggle(id)"
        >
          移除
        </button>
      </div>
    </div>
    <p v-if="loading" class="muted">加载分组中…</p>
    <div v-else class="agent-checks">
      <label v-for="item in items" :key="item.id"
        ><input
          type="checkbox"
          :checked="modelValue.includes(item.id)"
          :disabled="!modelValue.includes(item.id) && modelValue.length >= 20"
          @change="toggle(item.id)"
        />{{ item.name }}</label
      >
      <p v-if="!items.length" class="muted">还没有知识分组。请先到 RAG 页面创建。</p>
    </div>
    <div v-if="page.total > page.page_size" class="pager">
      <button
        type="button"
        class="secondary"
        :disabled="loading || page.page <= 1"
        @click="load(page.page - 1)"
      >
        上一页分组
      </button>
      <span>第 {{ page.page }} 页</span>
      <button
        type="button"
        class="secondary"
        :disabled="loading || page.page * page.page_size >= page.total"
        @click="load(page.page + 1)"
      >
        下一页分组
      </button>
    </div>
  </fieldset>
</template>
<script setup lang="ts">
  import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
  import { ragApi, type KnowledgeGroup } from '../../agent/rag'
  import { agentError } from '../../agent/api'
  const props = defineProps<{ modelValue: string[] }>()
  const emit = defineEmits<{ 'update:modelValue': [value: string[]] }>()
  const controller = new AbortController(),
    items = ref<KnowledgeGroup[]>([]),
    names = ref<Record<string, string>>({}),
    loading = ref(false),
    error = ref('')
  const page = ref({ page: 1, page_size: 20, total: 0 })
  const pendingNames = new Set<string>()
  function toggle(id: string) {
    emit(
      'update:modelValue',
      props.modelValue.includes(id)
        ? props.modelValue.filter((value) => value !== id)
        : [...props.modelValue, id],
    )
  }
  async function loadSelectedNames() {
    const ids = props.modelValue.filter((id) => !names.value[id] && !pendingNames.has(id))
    for (const id of ids) pendingNames.add(id)
    const selected = await Promise.allSettled(ids.map((id) => ragApi.group(id, controller.signal)))
    for (const id of ids) pendingNames.delete(id)
    if (controller.signal.aborted) return
    for (const [index, item] of selected.entries()) {
      if (item.status === 'fulfilled')
        names.value[item.value.data.knowledge_base.id] = item.value.data.knowledge_base.name
      else if (props.modelValue.includes(ids[index]!))
        error.value = '部分绑定分组无法加载，请重试或移除后保存'
    }
  }
  async function load(number = 1) {
    if (loading.value) return
    loading.value = true
    error.value = ''
    try {
      const result = await ragApi.groups(number, controller.signal)
      if (controller.signal.aborted) return
      items.value = result.data.items
      page.value = result.data.page
      for (const item of items.value) names.value[item.id] = item.name
      await loadSelectedNames()
    } catch (cause) {
      if (!controller.signal.aborted) error.value = agentError(cause)
    } finally {
      if (!controller.signal.aborted) loading.value = false
    }
  }
  watch(() => [...props.modelValue], loadSelectedNames, { immediate: true })
  onMounted(() => load())
  onBeforeUnmount(() => controller.abort())
</script>
