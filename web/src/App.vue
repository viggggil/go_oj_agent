<template>
  <main class="shell">
    <header>
      <strong>Go OJ Agent</strong>
      <nav>
        <RouterLink to="/profile">个人资料</RouterLink
        ><RouterLink v-if="!auth.isAuthenticated" to="/login">登录</RouterLink
        ><RouterLink v-if="!auth.isAuthenticated" to="/register">注册</RouterLink
        ><button v-if="auth.isAuthenticated" class="link" @click="signOut">退出</button>
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
