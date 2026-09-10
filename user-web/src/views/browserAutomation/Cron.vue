<template>
  <div class="page">
    <div class="toolbar">
      <h2 style="margin: 0">定时触发器</h2>
      <el-button type="primary" icon="Plus" @click="$router.push('/browser-automation/tasks/create')">新建 cron 任务</el-button>
    </div>

    <el-table :data="list" v-loading="loading">
      <el-table-column prop="id" label="ID" width="70" />
      <el-table-column prop="task_id" label="任务" min-width="200">
        <template #default="{ row }">
          <el-link type="primary" @click="$router.push(`/browser-automation/tasks/${row.task_id}`)">#{{ row.task_id }}</el-link>
        </template>
      </el-table-column>
      <el-table-column prop="cron_expr" label="表达式" width="160" />
      <el-table-column prop="enabled" label="状态" width="100">
        <template #default="{ row }">
          <el-tag :type="row.enabled ? 'success' : 'info'">{{ row.enabled ? '已启用' : '已停用' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="last_run_at" label="上次触发" width="170">
        <template #default="{ row }">{{ row.last_run_at ? new Date(row.last_run_at).toLocaleString('zh-CN') : '—' }}</template>
      </el-table-column>
      <el-table-column label="操作" width="220" fixed="right">
        <template #default="{ row }">
          <el-button size="small" @click="toggle(row)">{{ row.enabled ? '停用' : '启用' }}</el-button>
          <el-button size="small" type="danger" @click="remove(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { listBrowserCron, enableBrowserCron, disableBrowserCron, deleteBrowserCron } from '@/api/browserAutomation'

const list = ref([])
const loading = ref(false)

const unpack = (res) => res?.data ?? res

async function load() {
  loading.value = true
  try {
    const res = await listBrowserCron()
    const data = unpack(res)
    list.value = Array.isArray(data) ? data : data?.list || []
  } finally {
    loading.value = false
  }
}

async function toggle(row) {
  if (row.enabled) await disableBrowserCron(row.id)
  else await enableBrowserCron(row.id)
  ElMessage.success('已更新')
  load()
}

async function remove(row) {
  await ElMessageBox.confirm(`确认删除触发器 #${row.id}？`, '提示', { type: 'warning' })
  await deleteBrowserCron(row.id)
  load()
}

onMounted(load)
</script>

<style scoped>
.page { padding: 16px; }
.toolbar { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; }
</style>
