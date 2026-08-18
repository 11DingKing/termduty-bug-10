<template>
  <div>
    <div class="td-header"><h2>超时与积压清单</h2><el-button :loading="loading" @click="load">刷新</el-button></div>
    <el-row :gutter="16">
      <el-col :span="4"><el-card><div class="stat-label">待处理</div><div class="stat-num">{{ summary?.open_alerts ?? 0 }}</div></el-card></el-col>
      <el-col :span="4"><el-card><div class="stat-label">已接单</div><div class="stat-num">{{ summary?.assigned_alerts ?? 0 }}</div></el-card></el-col>
      <el-col :span="4"><el-card><div class="stat-label">处置中</div><div class="stat-num">{{ summary?.handling_alerts ?? 0 }}</div></el-card></el-col>
      <el-col :span="4"><el-card><div class="stat-label">待拉取</div><div class="stat-num">{{ summary?.pending_ingest ?? 0 }}</div></el-card></el-col>
      <el-col :span="4"><el-card><div class="stat-label">已租约</div><div class="stat-num">{{ summary?.leased_ingest ?? 0 }}</div></el-card></el-col>
      <el-col :span="4"><el-card><div class="stat-label">死信</div><div class="stat-num">{{ summary?.dead_lettered ?? 0 }}</div></el-card></el-col>
    </el-row>
    <el-card style="margin-top:16px" header="超时告警">
      <el-table :data="summary?.overdue_alerts ?? []" border>
        <el-table-column prop="id" label="工单ID" width="280" show-overflow-tooltip>
          <template #default="{ row }"><router-link :to="`/alerts/${row.id}`">{{ shortId(row.id) }}</router-link></template>
        </el-table-column>
        <el-table-column prop="collector_id" label="采集点" width="200" show-overflow-tooltip />
        <el-table-column prop="severity" label="级别" width="80" />
        <el-table-column prop="message" label="告警内容" show-overflow-tooltip />
        <el-table-column prop="last_seen" label="最近发现" width="180" />
        <template #empty><EmptyHint text="无超时告警" /></template>
      </el-table>
    </el-card>
    <el-card style="margin-top:16px" header="永久失败（死信队列）">
      <el-table :data="summary?.failures ?? []" border>
        <el-table-column prop="id" label="失败ID" width="280" show-overflow-tooltip />
        <el-table-column prop="task_type" label="任务类型" width="120" />
        <el-table-column prop="target_id" label="目标ID" width="200" show-overflow-tooltip />
        <el-table-column prop="last_error" label="错误" show-overflow-tooltip />
        <el-table-column prop="attempts" label="尝试次数" width="100" />
        <template #empty><EmptyHint text="无死信记录" /></template>
      </el-table>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api, extractError } from '../api/client'
import type { BacklogSummary } from '../types'
import EmptyHint from '../components/EmptyHint.vue'
import { shortId } from '../utils/format'

const summary = ref<BacklogSummary | null>(null)
const loading = ref(false)

async function load() {
  loading.value = true
  try { summary.value = await api.backlog() }
  catch { summary.value = null }
  finally { loading.value = false }
}
onMounted(load)
</script>

<style scoped>
.stat-label { color: #909399; font-size: 13px; }
.stat-num { font-size: 28px; font-weight: 600; margin-top: 8px; }
</style>
