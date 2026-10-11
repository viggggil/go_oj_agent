<template>
  <div class="binding-picker">
    <div class="agent-control-row">
      <input v-model="search" :aria-label="`${label} 搜索`" placeholder="搜索标识" maxlength="64" />
      <button
        type="button"
        class="agent-icon secondary"
        :title="`搜索 ${label}`"
        :aria-label="`搜索 ${label}`"
        @click="load(1)"
      >
        <Search :size="17" />
      </button>
      <button
        type="button"
        class="agent-icon secondary"
        :title="`新建 ${label}`"
        :aria-label="`新建 ${label}`"
        @click="$emit('create')"
      >
        <Plus :size="17" />
      </button>
    </div>
    <p v-if="error" class="error" role="alert">
      {{ error }} <button type="button" class="link" @click="load(page)">重试</button>
    </p>
    <p v-if="loading" class="muted" role="status">加载中...</p>
    <div v-for="item in selected" :key="item.id" class="selected-binding">
      <div>
        <strong>{{ item.name || item.key }}</strong
        ><span class="agent-version">{{ item.key }} · {{ item.id }}</span
        ><span
          v-if="item.disabled || item.archived"
          class="agent-badge"
          :class="item.disabled ? 'bad' : 'neutral'"
          >{{ item.disabled ? '已停用' : '历史绑定' }}</span
        >
      </div>
      <button
        type="button"
        class="agent-icon secondary"
        :title="`移除 ${item.key}`"
        :aria-label="`移除 ${item.key}`"
        @click="remove(item.id)"
      >
        <X :size="16" />
      </button>
    </div>
    <select
      :value="''"
      :aria-label="`选择 ${label} 版本`"
      :disabled="loading"
      @change="choose(($event.target as HTMLSelectElement).value)"
    >
      <option value="">{{ items.length ? `选择 ${label} 版本` : '没有可选版本' }}</option>
      <option
        v-for="item in items"
        :key="item.id"
        :value="item.id"
        :disabled="item.disabled || item.archived || selected.some((value) => value.id === item.id)"
      >
        {{ item.name || item.key }} · {{ item.key }} · {{ item.id
        }}{{ item.disabled ? ' · 已停用' : '' }}
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
  import { Search, Plus, X, ChevronLeft, ChevronRight } from '@lucide/vue'
  import { agentApi, agentError } from '../../agent/api'
  import type { Binding, Resource, Summary } from '../../agent/types'
  const props = defineProps<{
    label: string
    resource: Resource
    modelValue: string[]
    initial?: Binding[]
    multiple?: boolean
  }>()
  const emit = defineEmits<{
    'update:modelValue': [value: string[]]
    bindings: [value: Binding[]]
    create: []
  }>()
  const items = ref<Summary[]>([]),
    selected = ref<Binding[]>([]),
    search = ref(''),
    page = ref(1),
    total = ref(0),
    loading = ref(false),
    error = ref('')
  let controller: AbortController | undefined
  function sync() {
    const pool = [
      ...(props.initial || []),
      ...selected.value,
      ...items.value.map((item) => ({ ...item, name: item.name || item.key })),
    ]
    selected.value = props.modelValue.map(
      (id) =>
        pool.find((item) => item.id === id) || {
          id,
          key: id,
          name: '版本信息加载失败',
          archived: false,
          disabled: false,
        },
    )
  }
  async function load(next: number) {
    controller?.abort()
    const active = new AbortController()
    controller = active
    loading.value = true
    error.value = ''
    try {
      const { data } = await agentApi.list(
        props.resource,
        next,
        search.value,
        'active',
        active.signal,
      )
      if (active.signal.aborted) return
      items.value = data.items
      page.value = data.page.page
      total.value = data.page.total
      sync()
    } catch (cause) {
      if (!active.signal.aborted) error.value = agentError(cause)
    } finally {
      if (!active.signal.aborted) loading.value = false
    }
  }
  function choose(id: string) {
    const item = items.value.find((value) => value.id === id)
    if (!item || item.disabled || item.archived || props.modelValue.includes(id)) return
    const previous = selected.value.find((value) => value.key === item.key)
    if (props.multiple && previous && !window.confirm('将同一 Skill 的历史绑定替换为所选版本？'))
      return
    const values = props.multiple
      ? [...props.modelValue.filter((value) => value !== previous?.id), id]
      : [id]
    const selectedItem = { ...item, name: item.name || item.key }
    emit('bindings', [...selected.value.filter((value) => value.id !== previous?.id), selectedItem])
    emit('update:modelValue', values)
  }
  function remove(id: string) {
    emit(
      'update:modelValue',
      props.modelValue.filter((value) => value !== id),
    )
  }
  watch(() => [props.modelValue, props.initial], sync, { deep: true, immediate: true })
  onMounted(() => load(1))
  onBeforeUnmount(() => controller?.abort())
</script>
