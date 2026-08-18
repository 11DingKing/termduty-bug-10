<template>
  <el-tag :type="entry.type" size="small" class="td-state-tag">{{ entry.label }}</el-tag>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{ state: string }>()

type TagType = '' | 'success' | 'warning' | 'info' | 'danger'

const TABLE: Record<string, { type: TagType; label: string }> = {
  open: { type: 'danger', label: '待处理' },
  assigned: { type: 'warning', label: '已接单' },
  handling: { type: '', label: '处置中' },
  resolved: { type: 'success', label: '已解决' },
  closed: { type: 'info', label: '已关闭' },
  revoked: { type: 'info', label: '已撤销' }
}

const entry = computed(() => TABLE[props.state] ?? { type: 'info' as TagType, label: props.state })
</script>
