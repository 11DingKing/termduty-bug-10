<template>
  <el-tag :type="tagType" size="small">{{ severity }}</el-tag>
</template>

<script setup lang="ts">
import { computed } from 'vue'

type TagType = 'success' | 'warning' | 'info' | 'danger'

const props = defineProps<{ severity: string }>()

const tagType = computed<TagType>(() =>
  props.severity === 'critical' ? 'danger' : props.severity === 'warn' ? 'warning' : 'info'
)
</script>
