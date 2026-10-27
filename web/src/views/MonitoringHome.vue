<template>
  <section class="monitoring-page">
    <header class="page-header">
      <div>
        <p class="eyebrow">Platform observability</p>
        <h1>监控</h1>
        <p>从 Agent 对话运行到后端判题链路，统一进入排障和运行态视图。</p>
      </div>
    </header>
    <div class="monitoring-grid monitoring-entry-grid">
      <RouterLink class="monitoring-card monitoring-card-link" to="/monitoring/metrics">
        <p class="eyebrow">Metrics</p>
        <h2>指标总览</h2>
        <p>查看当前角色可访问的固定指标摘要与采集状态。</p>
        <span>查看指标 →</span>
      </RouterLink>
      <RouterLink class="monitoring-card monitoring-card-link" to="/monitoring/agent">
        <p class="eyebrow">Agent</p>
        <h2>Agent 运行诊断</h2>
        <p>查看 Run 状态、Trace、工具步骤、知识检索和受控 LangSmith 导出链接。</p>
        <span>进入诊断 →</span>
      </RouterLink>
      <RouterLink
        v-if="isSystemMonitor"
        class="monitoring-card monitoring-card-link"
        to="/monitoring/judge"
      >
        <p class="eyebrow">OJ backend</p>
        <h2>判题链路</h2>
        <p>查看 Gateway、Judge、RabbitMQ、Worker 和 Sandbox 的因果拓扑。</p>
        <span>查看链路 →</span>
      </RouterLink>
    </div>
    <p class="monitoring-footnote">监控页面只展示受控摘要；业务详情和配置仍由各自服务负责。</p>
  </section>
</template>

<script setup lang="ts">
  import { computed } from 'vue'
  import { RouterLink } from 'vue-router'
  import { useAuthStore } from '../stores/auth'

  const auth = useAuthStore()
  const isSystemMonitor = computed(() =>
    auth.user?.roles.some((role) => ['admin', 'system_admin'].includes(role)),
  )
</script>
