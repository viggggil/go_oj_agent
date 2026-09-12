<template>
  <AuthForm title="登录" submit-text="登录" :loading="auth.loading" :error="error" @submit="submit"
    ><label>账号<input v-model.trim="account" required autocomplete="username" /></label
    ><label
      >密码<input
        v-model="password"
        required
        minlength="8"
        type="password"
        autocomplete="current-password" /></label
    ><template #footer
      >还没有账号？<RouterLink to="/register">立即注册</RouterLink></template
    ></AuthForm
  >
</template>
<script setup lang="ts">
  import { ref } from 'vue'
  import { RouterLink, useRouter } from 'vue-router'
  import AuthForm from '../components/AuthForm.vue'
  import { useAuthStore } from '../stores/auth'
  const auth = useAuthStore()
  const router = useRouter()
  const account = ref('')
  const password = ref('')
  const error = ref('')
  async function submit() {
    error.value = ''
    try {
      await auth.login(account.value, password.value)
      router.push('/profile')
    } catch (e: any) {
      error.value = e.response?.data?.message || '登录失败，请检查账号和密码'
    }
  }
</script>
