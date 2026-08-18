<template>
  <div v-if="error" class="td-empty td-error">{{ error }}</div>
  <div v-else class="td-empty">{{ text }}</div>
</template>

<script setup lang="ts">
withDefaults(defineProps<{ text?: string; error?: string }>(), {
  text: '暂无数据'
})
</script>
