<template>
  <section class="agent-workspace">
    <div class="agent-mobile-bar">
      <strong>Agent 工作区</strong>
      <button
        class="agent-icon secondary"
        type="button"
        title="切换菜单"
        aria-label="切换菜单"
        @click="open = !open"
      >
        <Menu :size="18" />
      </button>
    </div>
    <aside class="agent-sidebar" :class="{ open }">
      <div class="agent-sidebar-title">
        <strong>Agent</strong
        ><button
          class="agent-icon secondary"
          type="button"
          title="关闭菜单"
          aria-label="关闭菜单"
          @click="open = false"
        >
          <X :size="18" />
        </button>
      </div>
      <nav class="agent-side-nav" aria-label="Agent 管理导航">
        <RouterLink to="/agent/chat" @click="open = false"
          ><MessageSquare :size="17" />对话</RouterLink
        >
        <template v-if="isAdmin">
          <RouterLink to="/agent/rag" @click="open = false"
            ><Database :size="17" />RAG 知识库</RouterLink
          >
          <RouterLink to="/agent/eval" @click="open = false"
            ><FileCheck :size="17" />Agent Eval</RouterLink
          >
          <RouterLink to="/agent/tools" @click="open = false"><Wrench :size="17" />工具</RouterLink>
          <RouterLink to="/agent/observability" @click="open = false"
            ><Activity :size="17" />运行诊断</RouterLink
          >
          <RouterLink to="/agent/agents" @click="open = false"><Bot :size="17" />Agent</RouterLink>
          <RouterLink to="/agent/prompts" @click="open = false"
            ><FileText :size="17" />Prompt</RouterLink
          >
          <RouterLink to="/agent/skills" @click="open = false"
            ><Puzzle :size="17" />Skill</RouterLink
          >
        </template>
      </nav>
      <p v-if="!auth.user && !auth.loading" class="agent-side-note">正在确认账号权限…</p>
      <p v-if="auth.user && !isAdmin" class="agent-side-note">当前账号只能使用对话</p>
    </aside>
    <div v-if="open" class="agent-sidebar-backdrop" @click="open = false"></div>
    <main class="agent-main">
      <p v-if="auth.profileError" class="error" role="alert">
        无法确认账号权限。<button class="link" @click="retry">重试</button>
      </p>
      <div v-show="!auth.profileError">
        <RouterView
          v-if="auth.isAuthenticated && auth.user && (!route.meta.agentAdmin || isAdmin)"
          :key="auth.user.id + ':' + route.path"
        />
      </div>
    </main>
  </section>
</template>
<script setup lang="ts">
  import { computed, ref, watch } from 'vue'
  import { RouterLink, RouterView, useRoute, useRouter } from 'vue-router'
  import {
    Bot,
    Activity,
    Database,
    FileCheck,
    FileText,
    Menu,
    MessageSquare,
    Puzzle,
    Wrench,
    X,
  } from '@lucide/vue'
  import { useAuthStore } from '../stores/auth'
  import { agentAdministrator, clearAgentDrafts } from '../agent/state'
  const auth = useAuthStore(),
    open = ref(false)
  const isAdmin = computed(() => agentAdministrator(auth.user?.roles || []))
  const route = useRoute(),
    router = useRouter()
  async function retry() {
    await auth.fetchProfile().catch(() => undefined)
    if (route.meta.agentAdmin && !auth.profileError && !isAdmin.value)
      await router.replace('/agent/chat')
  }
  watch(
    () => auth.isAuthenticated,
    (loggedIn) => {
      if (!loggedIn) {
        clearAgentDrafts()
        auth.user = null
        void router.replace('/login')
      }
    },
  )
</script>
