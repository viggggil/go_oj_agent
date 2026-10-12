<template>
  <section class="agent-page">
    <div class="page-head">
      <div>
        <span class="eyebrow">Agent 管理</span>
        <h1>{{ resourceNames[resource] }}</h1>
        <p>配置版本以 UUID 保留；替换会创建新版本，不会自动更新其他绑定。</p>
      </div>
      <RouterLink class="button" :to="`/agent/${resource}/new`"
        ><Plus :size="17" />新建{{ resourceNames[resource] }}</RouterLink
      >
    </div>
    <form class="agent-filter" @submit.prevent="reload">
      <label>搜索<input v-model="search" placeholder="按名称或标识搜索" /></label
      ><label
        >状态<select v-model="state">
          <option value="active">未归档</option>
          <option value="disabled">已停用</option>
          <option value="archived">已归档</option>
          <option value="all">全部</option>
        </select></label
      ><button class="secondary" type="submit" :disabled="loading">
        <Search :size="16" />查询
      </button>
    </form>
    <p v-if="error" class="error agent-alert" role="alert">
      {{ error }} <button class="link" type="button" @click="reload">重试</button>
    </p>
    <div v-if="loading" class="loading-panel">加载中…</div>
    <div v-else-if="!items.length" class="empty-panel">
      暂无{{ resourceNames[resource] }}配置。<RouterLink :to="`/agent/${resource}/new`"
        >创建第一条</RouterLink
      >
    </div>
    <div v-else class="agent-table" role="table">
      <div class="agent-row agent-row-head" role="row">
        <span>标识</span><span>版本 / 状态</span><span>创建者</span><span>操作</span>
      </div>
      <div v-for="item in items" :key="item.id" class="agent-row" role="row">
        <div>
          <strong>{{ item.name || item.key }}</strong
          ><small class="mono">{{ item.key }}</small
          ><small v-if="item.name && item.name !== item.key" class="mono">{{ item.id }}</small>
        </div>
        <div>
          <span
            class="status-chip"
            :class="{ 'agent-chip-muted': item.archived || item.disabled }"
            >{{ item.archived ? '已归档' : item.disabled ? '已停用' : '当前' }}</span
          ><small class="mono">{{ item.id }}</small>
        </div>
        <span>{{ item.created_by }} · {{ formatDate(item.created_at) }}</span>
        <div class="agent-actions">
          <RouterLink class="button secondary" :to="`/agent/${resource}/${item.key}/edit`"
            >编辑</RouterLink
          ><button
            class="agent-icon secondary"
            type="button"
            title="查看对话"
            aria-label="查看对话"
            v-if="resource === 'agents'"
            @click="goChat(item.key)"
          >
            <MessageSquare :size="16" />
          </button>
        </div>
      </div>
    </div>
    <div v-if="page.total" class="pager">
      <button
        class="secondary"
        type="button"
        :disabled="page.page <= 1 || loading"
        @click="load(page.page - 1)"
      >
        <ChevronLeft :size="17" /></button
      ><span>第 {{ page.page }} 页，共 {{ page.total }} 条</span
      ><button
        class="secondary"
        type="button"
        :disabled="page.page * page.page_size >= page.total || loading"
        @click="load(page.page + 1)"
      >
        <ChevronRight :size="17" />
      </button>
    </div>
  </section>
</template>
<script setup lang="ts">
  import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
  import { RouterLink, useRoute, useRouter } from 'vue-router'
  import { ChevronLeft, ChevronRight, MessageSquare, Plus, Search } from '@lucide/vue'
  import { agentApi, agentError } from '../agent/api'
  import {
    resourceNames,
    type ConfigState,
    type Page,
    type Resource,
    type Summary,
  } from '../agent/types'
  import { resourceFrom } from '../agent/state'
  const route = useRoute(),
    router = useRouter(),
    resource = ref<Resource>(resourceFrom(route.params.resource)),
    search = ref(''),
    state = ref<ConfigState>('active'),
    loading = ref(false),
    error = ref(''),
    items = ref<Summary[]>([]),
    page = ref<Page>({ page: 1, page_size: 20, total: 0 })
  let controller: AbortController | undefined
  function formatDate(value: string) {
    return new Date(value).toLocaleString()
  }
  async function load(number = 1) {
    controller?.abort()
    const active = new AbortController()
    controller = active
    loading.value = true
    error.value = ''
    try {
      const { data } = await agentApi.list(
        resource.value,
        number,
        search.value.trim(),
        state.value,
        active.signal,
      )
      if (!active.signal.aborted) {
        items.value = data.items
        page.value = data.page
      }
    } catch (cause) {
      if (!active.signal.aborted) error.value = agentError(cause)
    } finally {
      if (!active.signal.aborted) loading.value = false
    }
  }
  function reload() {
    void load(1)
  }
  function goChat(key: string) {
    void router.push({ path: '/agent/chat', query: { agent_key: key } })
  }
  watch(
    () => route.params.resource,
    (value) => {
      resource.value = resourceFrom(value)
      reload()
    },
  )
  onMounted(() => load())
  onBeforeUnmount(() => controller?.abort())
</script>
