<template>
  <main class="shell">
    <header>
      <strong>Go OJ Agent</strong>
      <nav>
        <RouterLink v-if="auth.isAuthenticated" to="/problems">题库</RouterLink
        ><RouterLink v-if="auth.isAuthenticated" to="/submissions">提交记录</RouterLink
        ><RouterLink v-if="auth.isAuthenticated" to="/contests">比赛</RouterLink
        ><RouterLink v-if="auth.isAuthenticated" to="/profile">个人账户</RouterLink
        ><RouterLink v-if="!auth.isAuthenticated" to="/login">登录</RouterLink
        ><RouterLink v-if="!auth.isAuthenticated" to="/register">注册</RouterLink>
      </nav>
    </header>
    <RouterView />
  </main>
</template>
<script setup lang="ts">
  import { RouterLink, RouterView, useRouter } from 'vue-router'
  import { useAuthStore } from './stores/auth'
  const auth = useAuthStore()
  const router = useRouter()
  async function signOut() {
    await auth.logout()
    router.push('/login')
  }
</script>
