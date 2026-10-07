<template>
  <div class="home-page">
    <section class="home-hero">
      <div>
        <p class="eyebrow">Go OJ Agent</p>
        <h1>面向学习者的在线判题与智能编程平台</h1>
        <p class="home-lead">
          Go OJ Agent 将题目管理、代码提交、自动判题、比赛排行榜和 Agent
          服务整合在一个可扩展的分布式平台中。
        </p>
        <div class="detail-actions">
          <RouterLink class="button" to="/problems">浏览题库</RouterLink>
          <RouterLink class="button secondary" to="/contests">查看比赛</RouterLink>
        </div>
      </div>
      <div class="home-mark" aria-hidden="true">
        <span>OJ</span>
        <small>BUILD · RUN · LEARN</small>
      </div>
    </section>

    <section class="home-section">
      <div class="section-heading">
        <p class="eyebrow">平台能力</p>
        <h2>从编写代码到复盘结果</h2>
        <p class="muted">平台的核心链路由 Go 服务负责业务事实，Agent 通过受保护的 Gateway 接入。</p>
      </div>
      <div class="home-cards">
        <article v-for="item in capabilities" :key="item.title" class="home-card">
          <span class="home-card-icon" aria-hidden="true">{{ item.icon }}</span>
          <h3>{{ item.title }}</h3>
          <p>{{ item.description }}</p>
        </article>
      </div>
    </section>

    <section class="home-section home-two-columns">
      <article class="home-panel">
        <p class="eyebrow">当前服务</p>
        <h2>清晰的服务边界</h2>
        <ul class="home-list">
          <li><strong>Go-Kratos 服务</strong><span>用户、题目、提交、判题和比赛业务。</span></li>
          <li>
            <strong>Agent Service</strong
            ><span>Python + FastAPI 的会话、Runtime 和 SSE 服务。</span>
          </li>
          <li>
            <strong>共享基础设施</strong><span>MySQL、Redis、RabbitMQ、MinIO 和 Consul。</span>
          </li>
          <li><strong>Vue 前端</strong><span>提供登录、题库、提交、比赛和个人资料页面。</span></li>
        </ul>
      </article>
      <article class="home-panel">
        <p class="eyebrow">Agent 状态</p>
        <h2>可控的演示运行时</h2>
        <p>
          Agent 已支持可信身份委托、会话持久化、运行状态管理和流式输出。当前使用明确标记的 Fake
          Runtime，真实模型、业务 Tools、RAG 和管理员控制面仍按后续阶段推进。
        </p>
        <p class="muted">生产环境不会自动降级到模拟回答。</p>
      </article>
    </section>

    <section class="home-section home-footer-panel">
      <div>
        <p class="eyebrow">开始开发</p>
        <h2>从本地 Compose 环境开始</h2>
        <p class="muted">完整命令、迁移说明和架构文档请查看项目根目录 README 及 docs/。</p>
      </div>
      <pre><code>make infra-up
make test
make build</code></pre>
    </section>
  </div>
</template>

<script setup lang="ts">
  import { RouterLink } from 'vue-router'

  const capabilities = [
    {
      icon: '01',
      title: '题库与提交',
      description: '浏览题目、提交 Go/Python 等代码，并查看提交状态、判题结果和源码。',
    },
    {
      icon: '02',
      title: '自动判题',
      description: 'Judge Worker 在隔离环境中编译和运行代码，通过 RabbitMQ 驱动异步判题流程。',
    },
    {
      icon: '03',
      title: '比赛与排行榜',
      description: '创建和参加比赛，按 ACM 规则查看解题数、罚时和稳定排序的排行榜。',
    },
    {
      icon: '04',
      title: 'Agent 流式服务',
      description: '经 Gateway 认证后使用会话、可信委托和 SSE 增量接收演示回答。',
    },
  ]
</script>
