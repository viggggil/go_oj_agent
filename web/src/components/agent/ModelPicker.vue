<template>
  <div class="binding-picker">
    <div class="agent-control-row">
      <input
        v-model="search"
        aria-label="模型搜索"
        placeholder="搜索模型配置标识"
        maxlength="64"
      /><button
        type="button"
        class="agent-icon secondary"
        title="搜索模型"
        aria-label="搜索模型"
        @click="load(1)"
      >
        <Search :size="17" />
      </button>
    </div>
    <p v-if="error" class="error" role="alert">
      {{ error }} <button type="button" class="link" @click="load(page)">重试</button>
    </p>
    <p v-if="detailError" class="error" role="alert">
      绑定模型 {{ modelValue }}：{{ detailError }}
    </p>
    <div v-if="selected" class="selected-binding">
      <div>
        <strong>{{ selected.model }}</strong
        ><span class="agent-version">{{ selected.key }} · {{ selected.id }}</span
        ><span
          v-if="selected.disabled || selected.archived || selected.available === false"
          class="agent-badge neutral"
          >{{ selected.disabled ? '已停用' : selected.archived ? '历史绑定' : '当前不可用' }}</span
        ><span class="muted agent-caption"
          >上下文 {{ selected.context_window_tokens }} · 输出 {{ selected.max_output_tokens }}</span
        >
      </div>
    </div>
    <select
      :value="modelValue"
      aria-label="选择模型版本"
      :disabled="loading"
      @change="$emit('update:modelValue', ($event.target as HTMLSelectElement).value)"
    >
      <option value="">
        {{ loading ? '加载中...' : items.length ? '选择模型版本' : '没有可用模型' }}
      </option>
      <option
        v-if="selected && !items.some((item) => item.id === selected?.id)"
        :value="selected.id"
      >
        {{ selected.model }} · {{ selected.key }} · 历史绑定
      </option>
      <option v-for="item in items" :key="item.id" :value="item.id">
        {{ item.model }} · {{ item.key }} · {{ item.id }}
      </option>
    </select>
    <div class="agent-small-pager">
      <button
        type="button"
        class="agent-icon secondary"
        title="上一页"
        aria-label="上一页"
        :disabled="page <= 1 || loading"
        @click="load(page - 1)"
      >
        <ChevronLeft :size="16" /></button
      ><span>{{ page }} / {{ Math.max(1, Math.ceil(total / 20)) }}</span
      ><button
        type="button"
        class="agent-icon secondary"
        title="下一页"
        aria-label="下一页"
        :disabled="page * 20 >= total || loading"
        @click="load(page + 1)"
      >
        <ChevronRight :size="16" />
      </button>
    </div>
  </div>
</template>
<script setup lang="ts">
  import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
  import { Search, ChevronLeft, ChevronRight } from '@lucide/vue'
  import { agentApi, agentError } from '../../agent/api'
  import type { ModelOption } from '../../agent/types'
  const props = defineProps<{ modelValue: string }>()
  defineEmits<{ 'update:modelValue': [value: string] }>()
  const items = ref<ModelOption[]>([]),
    selected = ref<ModelOption>(),
    search = ref(''),
    page = ref(1),
    total = ref(0),
    loading = ref(false),
    error = ref(''),
    detailError = ref('')
  let controller: AbortController | undefined, detailController: AbortController | undefined
  async function load(next: number) {
    controller?.abort()
    const active = new AbortController()
    controller = active
    loading.value = true
    error.value = ''
    try {
      const { data } = await agentApi.models(next, search.value, active.signal)
      if (!active.signal.aborted) {
        items.value = data.items
        page.value = data.page.page
        total.value = data.page.total
      }
    } catch (cause) {
      if (!active.signal.aborted) error.value = agentError(cause)
    } finally {
      if (!active.signal.aborted) loading.value = false
    }
  }
  watch(
    () => props.modelValue,
    async (id) => {
      detailController?.abort()
      selected.value = undefined
      detailError.value = ''
      if (!id) return
      const active = new AbortController()
      detailController = active
      try {
        const { data } = await agentApi.model(id, active.signal)
        if (!active.signal.aborted) selected.value = data.model_option
      } catch (cause) {
        if (!active.signal.aborted) detailError.value = agentError(cause)
      }
    },
    { immediate: true },
  )
  onMounted(() => load(1))
  onBeforeUnmount(() => {
    controller?.abort()
    detailController?.abort()
  })
</script>
