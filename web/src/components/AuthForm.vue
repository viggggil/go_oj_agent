<template>
  <form class="card" @submit.prevent="$emit('submit')">
    <h1>{{ title }}</h1>
    <slot />
    <p v-if="error" class="error">{{ error }}</p>
    <button :disabled="loading" type="submit">{{ loading ? '提交中…' : submitText }}</button>
    <p class="hint"><slot name="footer" /></p>
  </form>
</template>
<script setup lang="ts">
  defineProps<{ title: string; submitText: string; loading?: boolean; error?: string }>()
  defineEmits<{ submit: [] }>()
</script>
