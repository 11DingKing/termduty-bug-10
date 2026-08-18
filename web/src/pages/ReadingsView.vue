<template>
  <div>
    <div class="td-header"><h2>运行读数</h2></div>
    <div class="td-toolbar">
      <el-input v-model="filters.collector_id" placeholder="采集点ID筛选" clearable style="width:220px" @keyup.enter="loadData(1)" @clear="loadData(1)" />
      <el-input v-model="filters.fault_code" placeholder="故障码筛选" clearable style="width:160px" @keyup.enter="loadData(1)" @clear="loadData(1)" />
      <el-button type="primary" :loading="loading" @click="loadData(1)">查询</el-button>
      <el-button type="success" :loading="exporting" @click="doExport">导出</el-button>
    </div>
    <el-alert v-if="errorMsg" :title="errorMsg" type="error" :closable="false" style="margin-bottom:12px" />
    <el-card>
      <el-table :data="readings" v-loading="loading" border stripe>
        <el-table-column prop="id" label="读数ID" width="280" show-overflow-tooltip>
          <template #default="{ row }">{{ shortId(row.id) }}</template>
        </el-table-column>
        <el-table-column prop="collector_id" label="采集点" width="200" show-overflow-tooltip />
        <el-table-column prop="timestamp" label="时间" width="180">
          <template #default="{ row }">{{ fmtTime(row.timestamp) }}</template>
        </el-table-column>
        <el-table-column prop="queue_count" label="排队人数" width="100" />
        <el-table-column prop="duration_ms" label="办件耗时(ms)" width="120" />
        <el-table-column prop="fault_code" label="故障码" width="120" />
        <el-table-column prop="shard_id" label="分片" show-overflow-tooltip />
        <template #empty><EmptyHint text="暂无读数数据" /></template>
      </el-table>
      <Pager v-model:page="page" v-model:pageSize="pageSize" :total="total" :sizes="[10, 20, 50, 100]" @change="loadData()" />
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { api, extractError } from '../api/client'
import type { ReadingSummaryDTO } from '../types'
import Pager from '../components/Pager.vue'
import EmptyHint from '../components/EmptyHint.vue'
import { shortId, fmtTime } from '../utils/format'

const route = useRoute()
const readings = ref<ReadingSummaryDTO[]>([])
const loading = ref(false)
const exporting = ref(false)
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const errorMsg = ref('')

const filters = reactive({ collector_id: '', fault_code: '' })

async function loadData(targetPage?: number) {
  if (targetPage) page.value = targetPage
  loading.value = true
  errorMsg.value = ''
  try {
    const res = await api.listReadings({
      page: page.value,
      page_size: pageSize.value,
      collector_id: filters.collector_id,
      fault_code: filters.fault_code
    })
    readings.value = res.items ?? []
    total.value = res.total ?? 0
  } catch (e) {
    readings.value = []
    total.value = 0
    errorMsg.value = extractError(e)
  } finally {
    loading.value = false
  }
}

async function doExport() {
  exporting.value = true
  try {
    const url = api.exportReadingsUrl({
      collector_id: filters.collector_id,
      fault_code: filters.fault_code
    })
    window.open(url, '_blank')
  } finally {
    exporting.value = false
  }
}

onMounted(() => {
  const c = route.query.collector_id as string
  if (c) filters.collector_id = c
  loadData(1)
})
</script>
