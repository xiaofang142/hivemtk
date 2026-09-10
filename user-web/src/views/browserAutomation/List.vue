<template>
  <div class="page">
    <div class="toolbar">
      <el-button type="primary" icon="Plus" @click="$router.push('/browser-automation/tasks/create')">新建任务</el-button>
      <el-select v-model="query.status" placeholder="状态" clearable style="width: 140px" @change="load">
        <el-option v-for="s in statusOptions" :key="s.value" :label="s.label" :value="s.value" />
      </el-select>
    </div>

    <el-table :data="list" v-loading="loading">
      <el-table-column prop="id" label="ID" width="70" />
      <el-table-column prop="name" label="名称" min-width="160" />
      <el-table-column prop="task_type" label="类型" width="100" />
      <el-table-column prop="status" label="状态" width="100">
        <template #default="{ row }">
          <el-tag :type="statusTagType(row.status)">{{ row.status }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="brain_mode" label="模式" width="90">
        <template #default="{ row }">{{ row.brain_mode ? 'Brain' : '步骤' }}</template>
      </el-table-column>
      <el-table-column prop="last_run_at" label="上次执行" width="170">
        <template #default="{ row }">{{ row.last_run_at ? formatTime(row.last_run_at) : '—' }}</template>
      </el-table-column>
      <el-table-column label="操作" width="330" fixed="right">
        <template #default="{ row }">
          <el-button size="small" type="success" :disabled="row.status === 'running'" @click="onRun(row)">执行</el-button>
          <el-button size="small" v-if="row.status === 'draft'" @click="onPublish(row)">发布</el-button>
          <el-button size="small" v-if="row.status === 'running'" @click="onPause(row)">暂停</el-button>
          <el-button size="small" @click="$router.push(`/browser-automation/tasks/${row.id}`)">详情</el-button>
          <el-button size="small" @click="$router.push(`/browser-automation/tasks/${row.id}/edit`)">编辑</el-button>
          <el-button size="small" type="danger" @click="onDelete(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-pagination
      v-model:current-page="query.page"
      :page-size="query.limit"
      :total="total"
      layout="total, prev, pager, next"
      style="margin-top: 12px"
      @current-change="load"
    />

    <!-- 无 Host 引导弹窗（409） -->
    <el-dialog v-model="hostDialog.visible" title="本机 Chrome 未连接" width="480px">
      <el-alert type="warning" :closable="false" show-icon>
        <p>执行浏览器任务需要本机 Chrome 运行 HiveMTK 扩展和 NM Host：</p>
        <ol style="margin: 8px 0 0 16px; line-height: 1.9">
          <li>chrome://extensions 加载 <code>user-web/browser_automation/dist</code></li>
          <li>运行 <code>user-server/cmd/nm-host/install.sh</code></li>
          <li>在 <code>~/.hivemtk/nm_host.conf</code> 填入 admin 生成的 token</li>
        </ol>
      </el-alert>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted, onBeforeUnmount, reactive } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  listBrowserTasks, publishBrowserTask, runBrowserTask, pauseBrowserTask, deleteBrowserTask,
} from '@/api/browserAutomation'

const statusOptions = [
  { value: 'draft', label: '草稿' }, { value: 'ready', label: '就绪' },
  { value: 'running', label: '执行中' }, { value: 'paused', label: '已暂停' },
  { value: 'done', label: '完成' }, { value: 'failed', label: '失败' },
  { value: 'archived', label: '已归档' },
]

const list = ref([])
const total = ref(0)
const loading = ref(false)
const query = reactive({ status: '', page: 1, limit: 20 })
const hostDialog = reactive({ visible: false })
let pollTimer = null

const unpack = (res) => res?.data ?? res

async function load() {
  loading.value = true
  try {
    const res = await listBrowserTasks({ ...query })
    const data = unpack(res)
    list.value = data?.list || (Array.isArray(data) ? data : [])
    total.value = data?.total || 0
    // 有 running 任务时轻量轮询刷新状态
    if (list.value.some((t) => t.status === 'running')) {
      if (!pollTimer) pollTimer = setInterval(load, 5000)
    } else if (pollTimer) {
      clearInterval(pollTimer); pollTimer = null
    }
  } finally {
    loading.value = false
  }
}

const statusTagType = (s) => ({
  draft: 'info', ready: '', running: 'warning', paused: 'info',
  done: 'success', failed: 'danger', archived: 'info',
}[s] || 'info')

const formatTime = (t) => new Date(t).toLocaleString('zh-CN')

function handleRunError(err) {
  // 409 = Host 未连接
  if (String(err?.message || err).includes('未连接') || err?.code === 409) {
    hostDialog.visible = true
  } else {
    ElMessage.error(String(err?.message || err))
  }
}

async function onRun(row) {
  try {
    await runBrowserTask(row.id)
    ElMessage.success('已开始执行')
    load()
  } catch (e) { handleRunError(e) }
}

async function onPublish(row) {
  try {
    await publishBrowserTask(row.id)
    ElMessage.success('已发布')
    load()
  } catch (e) { ElMessage.error(String(e?.message || e)) }
}

async function onPause(row) {
  try {
    await pauseBrowserTask(row.id)
    ElMessage.success('已暂停')
    load()
  } catch (e) { ElMessage.error(String(e?.message || e)) }
}

async function onDelete(row) {
  await ElMessageBox.confirm(`确认删除任务「${row.name}」？`, '提示', { type: 'warning' })
  await deleteBrowserTask(row.id)
  ElMessage.success('已删除')
  load()
}

onMounted(load)
onBeforeUnmount(() => { if (pollTimer) clearInterval(pollTimer) })
</script>

<style scoped>
.page { padding: 16px; }
.toolbar { display: flex; gap: 12px; margin-bottom: 12px; }
</style>
