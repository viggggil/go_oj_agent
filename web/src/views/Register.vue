<template>
  <AuthForm title="注册" submit-text="创建账号" :error="error" @submit="submit"
    ><label>用户名<input v-model.trim="username" required maxlength="64" /></label
    ><label>邮箱<input v-model.trim="email" required type="email" /></label
    ><label
      >密码<input v-model="password" required minlength="8" maxlength="72" type="password" /></label
    ><template #footer>已有账号？<RouterLink to="/login">返回登录</RouterLink></template></AuthForm
  >
</template>
<script setup lang="ts">
  import { ref } from 'vue'
  import { RouterLink, useRouter } from 'vue-router'
  import AuthForm from '../components/AuthForm.vue'
  import { useAuthStore } from '../stores/auth'
  const auth = useAuthStore()
  const router = useRouter()
  const username = ref('')
  const email = ref('')
  const password = ref('')
  const error = ref('')
  async function submit() {
    error.value = ''
    try {
      await auth.register(username.value, email.value, password.value)
      router.push('/login')
    } catch (e: any) {
      error.value = e.response?.data?.message || '注册失败，请检查输入'
    }
  }
</script>
