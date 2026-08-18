<template>
  <div class="td-pagination">
    <el-pagination
      :current-page="page"
      :page-size="pageSize"
      :total="total"
      :page-sizes="sizes"
      layout="total, sizes, prev, pager, next"
      @current-change="onPage"
      @size-change="onSize"
    />
  </div>
</template>

<script setup lang="ts">
const page = defineModel<number>('page', { required: true })
const pageSize = defineModel<number>('pageSize', { required: true })

withDefaults(defineProps<{ total: number; sizes?: number[] }>(), {
  sizes: () => [10, 20, 50]
})

const emit = defineEmits<{ (e: 'change'): void }>()

function onPage(p: number) {
  page.value = p
  emit('change')
}

function onSize(s: number) {
  pageSize.value = s
  page.value = 1
  emit('change')
}
</script>
