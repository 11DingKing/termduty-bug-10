<template>
  <div>
    <div class="td-header">
      <h2>告警工单</h2>
      <el-select v-model="api.actor.role" style="width:160px" @change="onRoleChange">
        <el-option label="值班台(duty)" value="duty" />
        <el-option label="处置人(handler)" value="handler" />
        <el-option label="运维(ops)" value="ops" />
      </el-select>
    </div>
    <div class="td-toolbar">
      <el-input v-model="filters.collector_id" placeholder="采集点ID筛选" clearable style="width:220px" @keyup.enter="loadData(1)" @clear="loadData(1)" />
      <el-select v-model="filters.state" placeholder="状态" clearable style="width:140px" @change="loadData(1)">
        <el-option label="待处理" value="open" />
        <el-option label="已接单" value="assigned" />
        <el-option label="处置中" value="handling" />
        <el-option label="已解决" value="resolved" />
        <el-option label="已关闭" value="closed" />
        <el-option label="已撤销" value="revoked" />
      </el-select>
      <el-select v-model="filters.severity" placeholder="级别" clearable style="width:120px" @change="loadData(1)">
        <el-option label="提示" value="info" />
        <el-option label="警告" value="warn" />
        <el-option label="严重" value="critical" />
      </el-select>
      <el-button type="primary" :loading="loading" @click="loadData(1)">查询</el-button>
    </div>
    <el-alert v-if="errorMsg" :title="errorMsg" type="error" :closable="false" style="margin-bottom:12px" />
    <el-card>
      <el-table :data="alerts" v-loading="loading" border stripe>
        <el-table-column prop="id" label="工单ID" width="280" show-overflow-tooltip>
          <template #default="{ row }"><router-link :to="`/alerts/${row.id}`">{{ shortId(row.id) }}</router-link></template>
        </el-table-column>
        <el-table-column prop="collector_id" label="采集点" width="200" show-overflow-tooltip />
        <el-table-column prop="severity" label="级别" width="80">
          <template #default="{ row }"><SeverityTag :severity="row.severity" /></template>
        </el-table-column>
        <el-table-column prop="state" label="状态" width="100">
          <template #default="{ row }"><StateTag :state="row.state" /></template>
        </el-table-column>
        <el-table-column prop="message" label="告警内容" show-overflow-tooltip />
        <el-table-column prop="assignee_id" label="处置人" width="140" />
        <el-table-column prop="last_seen" label="最近发现" width="180">
          <template #default="{ row }">{{ fmtTime(row.last_seen) }}</template>
        </el-table-column>
        <template #empty><EmptyHint text="暂无告警工单" /></template>
      </el-table>
      <Pager v-model:page="page" v-model:pageSize="pageSize" :total="total" :sizes="[10, 20, 50]" @change="loadData()" />
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { api, extractError } from '../api/client'
import type { AlertDTO, Role } from '../types'
import StateTag from '../components/StateTag.vue'
import SeverityTag from '../components/SeverityTag.vue'
import Pager from '../components/Pager.vue'
import EmptyHint from '../components/EmptyHint.vue'
import { shortId, fmtTime } from '../utils/format'

const alerts = ref<AlertDTO[]>([])
const loading = ref(false)
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const errorMsg = ref('')

const filters = reactive({ collector_id: '', state: '', severity: '' })

async function loadData(targetPage?: number) {
  if (targetPage) page.value = targetPage
  loading.value = true
  errorMsg.value = ''
  try {
    const res = await api.listAlerts({
      page: page.value,
      page_size: pageSize.value,
      collector_id: filters.collector_id,
      state: filters.state,
      severity: filters.severity
    })
    alerts.value = res.items ?? []
    total.value = res.total ?? 0
  } catch (e) {
    alerts.value = []
    total.value = 0
    errorMsg.value = extractError(e)
  } finally {
    loading.value = false
  }
}

function onRoleChange(role: Role) {
  api.setActor({ id: api.actor.id, role })
}

onMounted(() => loadData(1))
</script>
