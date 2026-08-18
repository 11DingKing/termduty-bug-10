<template>
  <div>
    <div class="td-header"><h2>采集点管理</h2></div>
    <div class="td-toolbar">
      <el-input v-model="filters.q" placeholder="搜索名称/编码" clearable style="width:200px" @keyup.enter="loadData(1)" @clear="loadData(1)" />
      <el-select v-model="filters.kind" placeholder="类型" clearable style="width:140px" @change="loadData(1)">
        <el-option label="窗口" value="window" />
        <el-option label="自助终端" value="terminal" />
      </el-select>
      <el-select v-model="filters.status" placeholder="状态" clearable style="width:120px" @change="loadData(1)">
        <el-option label="启用" value="active" />
        <el-option label="停用" value="disabled" />
      </el-select>
      <el-button type="primary" :loading="loading" @click="loadData(1)">查询</el-button>
    </div>
    <el-card>
      <el-table :data="collectors" v-loading="loading" border stripe @selection-change="onSelect">
        <el-table-column type="selection" width="44" />
        <el-table-column prop="code" label="编码" width="140" />
        <el-table-column prop="name" label="名称" width="160" />
        <el-table-column prop="kind" label="类型" width="100">
          <template #default="{ row }">{{ row.kind === 'window' ? '窗口' : '自助终端' }}</template>
        </el-table-column>
        <el-table-column prop="location" label="位置" width="140" />
        <el-table-column prop="status" label="状态" width="100">
          <template #default="{ row }"><el-tag :type="row.status === 'active' ? 'success' : 'info'" size="small">{{ row.status === 'active' ? '启用' : '停用' }}</el-tag></template>
        </el-table-column>
        <el-table-column prop="handler_id" label="责任人" width="160" />
        <template #empty><EmptyHint text="暂无采集点" /></template>
      </el-table>
      <div style="margin-top:12px;display:flex;gap:8px">
        <el-button type="danger" :disabled="selected.length === 0" :loading="batching" @click="batchDisable">批量停用({{ selected.length }})</el-button>
        <el-button @click="showCreate = true">新增采集点</el-button>
      </div>
      <Pager v-model:page="page" v-model:pageSize="pageSize" :total="total" :sizes="[10,20,50]" @change="loadData()" />
    </el-card>

    <el-dialog v-model="showCreate" title="新增采集点" width="500px">
      <el-form label-width="80px">
        <el-form-item label="编码"><el-input v-model="form.code" /></el-form-item>
        <el-form-item label="名称"><el-input v-model="form.name" /></el-form-item>
        <el-form-item label="类型">
          <el-select v-model="form.kind"><el-option label="窗口" value="window" /><el-option label="自助终端" value="terminal" /></el-select>
        </el-form-item>
        <el-form-item label="位置"><el-input v-model="form.location" /></el-form-item>
        <el-form-item label="责任人ID"><el-input v-model="form.handler_id" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showCreate = false">取消</el-button>
        <el-button type="primary" :loading="creating" @click="doCreate">创建</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { api, extractError } from '../api/client'
import type { CollectorDTO } from '../types'
import Pager from '../components/Pager.vue'
import EmptyHint from '../components/EmptyHint.vue'

const collectors = ref<CollectorDTO[]>([])
const loading = ref(false)
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const selected = ref<CollectorDTO[]>([])
const batching = ref(false)
const showCreate = ref(false)
const creating = ref(false)
const filters = reactive({ q: '', kind: '', status: '' })
const form = reactive({ code: '', name: '', kind: 'window', location: '', handler_id: '', status: 'active' })

function onSelect(rows: CollectorDTO[]) { selected.value = rows }

async function loadData(targetPage?: number) {
  if (targetPage) page.value = targetPage
  loading.value = true
  try {
    const res = await api.listCollectors({ page: page.value, page_size: pageSize.value, q: filters.q, kind: filters.kind, status: filters.status })
    collectors.value = res.items ?? []
    total.value = res.total ?? 0
  } catch (e) {
    collectors.value = []
    ElMessage.error(extractError(e))
  } finally { loading.value = false }
}

async function batchDisable() {
  batching.value = true
  try {
    const res = await api.batchDisable(selected.value.map(c => c.id))
    ElMessage.success(`批量停用完成 ${res.completed} 个`)
    loadData()
  } catch (e) {
    ElMessage.error(extractError(e))
  } finally { batching.value = false }
}

async function doCreate() {
  creating.value = true
  try {
    await api.createCollector({ ...form })
    ElMessage.success('创建成功')
    showCreate.value = false
    form.code = ''; form.name = ''; form.location = ''; form.handler_id = ''
    loadData(1)
  } catch (e) { ElMessage.error(extractError(e)) }
  finally { creating.value = false }
}

onMounted(() => loadData(1))
</script>
