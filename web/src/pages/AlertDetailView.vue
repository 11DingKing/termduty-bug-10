<template>
  <div>
    <el-page-header @back="$router.push('/alerts')" content="告警详情" />
    <div v-loading="loading" style="margin-top:16px">
      <template v-if="alert">
        <el-card>
          <el-descriptions :column="2" border>
            <el-descriptions-item label="工单ID">{{ alert.id }}</el-descriptions-item>
            <el-descriptions-item label="状态">
              <StateTag :state="alert.state" />
            </el-descriptions-item>
            <el-descriptions-item label="采集点">{{ alert.collector_id }}</el-descriptions-item>
            <el-descriptions-item label="级别">
              <SeverityTag :severity="alert.severity" />
            </el-descriptions-item>
            <el-descriptions-item label="处置人">{{ alert.assignee_id || '—' }}</el-descriptions-item>
            <el-descriptions-item label="读数ID">
              <router-link v-if="alert.reading_id" :to="`/readings?collector_id=${alert.collector_id}`">{{ alert.reading_id }}</router-link>
            </el-descriptions-item>
            <el-descriptions-item label="首次发现">{{ fmtTime(alert.first_seen) }}</el-descriptions-item>
            <el-descriptions-item label="最近发现">{{ fmtTime(alert.last_seen) }}</el-descriptions-item>
            <el-descriptions-item label="告警内容" :span="2">{{ alert.message }}</el-descriptions-item>
          </el-descriptions>
        </el-card>

        <el-card style="margin-top:16px" header="处置操作">
          <div style="display:flex;gap:12px;flex-wrap:wrap;align-items:center">
            <el-input v-model="handlerId" placeholder="处置人ID（留空则用当前身份）" style="width:240px" />
            <el-input v-model="note" placeholder="处置备注" style="width:300px" />
            <el-button type="primary" :disabled="!canAccept" :loading="acting" @click="doAction('accept')">接单</el-button>
            <el-button type="warning" :disabled="!canStart" :loading="acting" @click="doAction('start')">开始处置</el-button>
            <el-button type="success" :disabled="!canResolve" :loading="acting" @click="doAction('resolve')">完成处置</el-button>
            <el-button :disabled="!canRelease" :loading="acting" @click="doAction('release')">退回</el-button>
            <el-button type="info" :disabled="!canRevoke" :loading="acting" @click="doRevoke">撤销(误报)</el-button>
            <el-button type="info" :disabled="!canClose" :loading="acting" @click="doClose">关闭</el-button>
          </div>
          <el-alert v-if="errorMsg" :title="errorMsg" type="error" :closable="false" style="margin-top:12px" />
          <el-alert v-if="successMsg" :title="successMsg" type="success" :closable="false" style="margin-top:12px" />
        </el-card>
      </template>
      <div v-else-if="!loading" class="td-empty">未找到该告警工单</div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { api, extractError } from '../api/client'
import type { AlertDTO } from '../types'
import StateTag from '../components/StateTag.vue'
import SeverityTag from '../components/SeverityTag.vue'
import { fmtTime } from '../utils/format'

const route = useRoute()
const alert = ref<AlertDTO | null>(null)
const loading = ref(false)
const acting = ref(false)
const handlerId = ref('')
const note = ref('')
const errorMsg = ref('')
const successMsg = ref('')

const canAccept = computed(() => alert.value?.state === 'open')
const canStart = computed(() => alert.value?.state === 'assigned')
const canResolve = computed(() => alert.value?.state === 'handling')
const canRelease = computed(() => alert.value?.state === 'assigned' || alert.value?.state === 'handling')
const canRevoke = computed(() => alert.value && !['closed', 'revoked'].includes(alert.value.state))
const canClose = computed(() => alert.value?.state === 'resolved')

async function load() {
  loading.value = true
  try {
    alert.value = await api.getAlert(route.params.id as string)
  } catch (e) {
    alert.value = null
    errorMsg.value = extractError(e)
  } finally {
    loading.value = false
  }
}

async function doAction(action: 'accept' | 'start' | 'resolve' | 'release') {
  acting.value = true
  errorMsg.value = ''
  successMsg.value = ''
  try {
    if (action === 'accept') {
      const res = await api.acceptAlert(route.params.id as string, handlerId.value, note.value)
      alert.value = res.alert
      successMsg.value = `接单成功，处置人：${res.assignment.handler_id}`
    } else if (action === 'start') {
      alert.value = await api.startAlert(route.params.id as string, handlerId.value)
      successMsg.value = '已开始处置'
    } else if (action === 'resolve') {
      alert.value = await api.resolveAlert(route.params.id as string, handlerId.value, note.value)
      successMsg.value = '处置完成，等待值班台关闭'
    } else if (action === 'release') {
      alert.value = await api.releaseAlert(route.params.id as string, handlerId.value)
      successMsg.value = '已退回待处理池'
    }
  } catch (e) {
    errorMsg.value = extractError(e)
  } finally {
    acting.value = false
  }
}

async function doRevoke() {
  acting.value = true
  errorMsg.value = ''
  successMsg.value = ''
  try {
    alert.value = await api.revokeAlert(route.params.id as string)
    successMsg.value = '已撤销（误报）'
  } catch (e) {
    errorMsg.value = extractError(e)
  } finally {
    acting.value = false
  }
}

async function doClose() {
  acting.value = true
  errorMsg.value = ''
  successMsg.value = ''
  try {
    alert.value = await api.closeAlert(route.params.id as string)
    successMsg.value = '工单已关闭'
  } catch (e) {
    errorMsg.value = extractError(e)
  } finally {
    acting.value = false
  }
}

onMounted(load)
</script>
