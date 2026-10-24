<template>
  <section class="monitoring-workspace">
    <div class="monitoring-mobile-bar">
      <strong>监控</strong>
      <button class="agent-icon secondary" type="button" @click="open = !open">菜单</button>
    </div>
    <aside class="monitoring-sidebar" :class="{ open }">
      <div class="monitoring-sidebar-title">
        <div>
          <p class="eyebrow">平台监控</p>
          <strong>监控</strong>
        </div>
        <button class="agent-icon secondary monitoring-close" type="button" @click="open = false">
          关闭
        </button>
      </div>
      <nav class="monitoring-nav" aria-label="监控导航">
        <RouterLink to="/monitoring/agent" @click="open = false">Agent 运行诊断</RouterLink>
        <RouterLink v-if="isSystemMonitor" to="/monitoring/judge" @click="open = false">
          判题链路
        </RouterLink>
      </nav>
      <p class="monitoring-sidebar-note">
        监控页面集中展示跨服务运行状态。Agent 配置、知识库和评测仍在 Agent 工作区管理。
      </p>
    </aside>
    <div v-if="open" class="monitoring-backdrop" @click="open = false"></div>
    <main class="monitoring-main">
      <RouterView />
    </main>
  </section>
</template>

<script setup lang="ts">
  import { computed, ref } from 'vue'
  import { RouterLink, RouterView } from 'vue-router'
  import { useAuthStore } from '../stores/auth'

  const auth = useAuthStore()
  const open = ref(false)
  const isSystemMonitor = computed(() =>
    auth.user?.roles.some((role) => ['admin', 'system_admin'].includes(role)),
  )
</script>
