<template>
  <section class="agent-page">
    <div class="page-head">
      <div>
        <span class="eyebrow">Agent</span>
        <h1>对话</h1>
      </div>
    </div>
    <div class="agent-chat-placeholder">
      <MessageSquare :size="30" aria-hidden="true" />
      <h2>{{ agentKey ? `已选择 ${agentKey}` : 'Agent 对话' }}</h2>
      <p>对话暂未接入。可以先创建和管理 Agent 配置。</p>
      <RouterLink class="button" v-if="isAdmin" to="/agent/agents">管理 Agent</RouterLink>
    </div>
  </section>
</template>
<script setup lang="ts">
  import { computed } from 'vue'
  import { RouterLink, useRoute } from 'vue-router'
  import { MessageSquare } from '@lucide/vue'
  import { useAuthStore } from '../stores/auth'
  import { agentAdministrator } from '../agent/state'
  const route = useRoute(),
    auth = useAuthStore()
  const agentKey = computed(() =>
    typeof route.query.agent_key === 'string' ? route.query.agent_key : '',
  )
  const isAdmin = computed(() => agentAdministrator(auth.user?.roles || []))
</script>
