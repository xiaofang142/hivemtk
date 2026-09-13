<template>
  <div class="page">
    <div class="header">
      <h2>执行监控 #{{ sessionId }}</h2>
      <el-space>
        <el-tag v-if="session" :type="{ completed: 'success', failed: 'danger', stopped: 'info', active: 'warning' }[session.status] || 'info'">
          {{ session.status }}
        </el-tag>
        <el-button v-if="session && ['created','active'].includes(session.status)" type="danger" @click="onStop">停止</el-button>
        <el-button v-if="session" @click="onExport">导出审计包</el-button>
      </el-space>
    </div>

    <el-card v-if="session" style="margin-top: 12px">
      <el-descriptions :column="4" border>
        <el-descriptions-item label="耗时">{{ (session.duration_ms / 1000).toFixed(1) }}s</el-descriptions-item>
        <el-descriptions-item label="成功率">{{ session.total_steps ? `${session.success_steps}/${session.total_steps}` : '—' }}</el-descriptions-item>
        <el-descriptions-item label="开始时间">{{ session.started_at ? new Date(session.started_at).toLocaleString('zh-CN') : '—' }}</el-descriptions-item>
        <el-descriptions-item v-if="session.error_msg" label="错误" :span="4">
          <span style="color: #ef4444">{{ session.error_msg }}</span>
        </el-descriptions-item>
      </el-descriptions>
    </el-card>

    <el-card v-if="session?.llm_summary" header="LLM 总结" style="margin-top: 12px">
      {{ session.llm_summary }}
    </el-card>

    <el-card header="步骤执行" style="margin-top: 12px">
      <el-table :data="steps" v-loading="loading">
        <el-table-column type="index" label="#" width="60" />
        <el-table-column prop="action" label="动作" width="130" />
        <el-table-column prop="target" label="目标" min-width="160" show-overflow-tooltip />
        <el-table-column prop="status" label="状态" width="110">
          <template #default="{ row }">
            <el-tag :type="{ success: 'success', failed: 'danger', running: 'warning', skipped: 'info', pending: 'info' }[row.status] || 'info'">
              {{ row.status }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="duration_ms" label="耗时" width="100">
          <template #default="{ row }">{{ row.duration_ms ? `${row.duration_ms}ms` : '—' }}</template>
        </el-table-column>
        <el-table-column prop="error_msg" label="错误" min-width="200" show-overflow-tooltip />
      </el-table>
    </el-card>

    <el-card style="margin-top: 12px">
      <template #header>
        <div style="display:flex;justify-content:space-between;align-items:center">
          <span>命令流（append-only 审计：command 下发 / event 回包 / judge 验收）</span>
          <el-select v-model="logDirection" size="small" style="width: 110px" @change="loadLogs">
            <el-option label="全部" value="" />
            <el-option label="command" value="command" />
            <el-option label="event" value="event" />
            <el-option label="judge" value="judge" />
          </el-select>
        </div>
      </template>
      <el-table :data="logs" size="small" max-height="420">
        <el-table-column prop="seq" label="seq" width="60" />
        <el-table-column prop="direction" label="方向" width="90">
          <template #default="{ row }">
            <el-tag size="small" :type="{ command: '', event: 'success', judge: 'warning' }[row.direction] || 'info'">{{ row.direction }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="action" label="动作" width="130" />
        <el-table-column prop="ok" label="结果" width="70">
          <template #default="{ row }">{{ row.ok ? '✓' : '✗' }}</template>
        </el-table-column>
        <el-table-column prop="duration_ms" label="耗时" width="80">
          <template #default="{ row }">{{ row.duration_ms ? `${row.duration_ms}ms` : '—' }}</template>
        </el-table-column>
        <el-table-column label="payload" min-width="260" show-overflow-tooltip>
          <template #default="{ row }"><span style="font-family: monospace; font-size: 12px">{{ payloadPreview(row.payload) }}</span></template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-card v-if="session?.final_screenshot_url" header="最终截图" style="margin-top: 12px">
      <el-image :src="session.final_screenshot_url" fit="contain" style="max-width: 100%" :preview-src-list="[session.final_screenshot_url]" />
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { getBrowserSession, getBrowserSessionSteps, stopBrowserSession, getBrowserSessionLogs, exportBrowserSessionAudit } from '@/api/browserAutomation'

const route = useRoute()
const sessionId = computed(() => route.params.id)
const session = ref(null)
const steps = ref([])
const logs = ref([])
const logDirection = ref('')
const loading = ref(false)

const payloadPreview = (p) => {
  if (p == null) return '—'
  const str = typeof p === 'string' ? p : JSON.stringify(p)
  return str.length > 160 ? str.slice(0, 160) + '…' : str
}

async function loadLogs() {
  try {
    const res = await getBrowserSessionLogs(sessionId.value, logDirection.value)
    const list = unpack(res)
    logs.value = Array.isArray(list) ? list : list?.list || []
  } catch {
    logs.value = []
  }
}
let pollTimer = null

const unpack = (res) => res?.data ?? res

async function load() {
  loading.value = steps.value.length === 0
  try {
    const [sRes, stRes] = await Promise.all([
      getBrowserSession(sessionId.value),
      getBrowserSessionSteps(sessionId.value),
    ])
    loadLogs()
    session.value = unpack(sRes)
    const list = unpack(stRes)
    steps.value = Array.isArray(list) ? list : list?.list || []
    // 终态停止轮询
    if (session.value && ['completed', 'failed', 'stopped'].includes(session.value.status)) {
      if (pollTimer) { clearInterval(pollTimer); pollTimer = null }
    }
  } finally {
    loading.value = false
  }
}

async function onStop() {
  await stopBrowserSession(sessionId.value, '用户手动中断')
  ElMessage.success('停止请求已发送——将在当前步骤执行完成后生效（步边界收敛，最长 ≈ 当前步超时）')
  load()
}

// I5：审计包导出（会话+步+命令流+LLM 成本账）落盘 JSON
async function onExport() {
  try {
    const res = await exportBrowserSessionAudit(sessionId.value)
    const payload = unpack(res)
    const blob = new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json' })
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    a.download = `browser_session_${sessionId.value}_audit.json`
    a.click()
    URL.revokeObjectURL(a.href)
    ElMessage.success('审计包已导出')
  } catch {
    ElMessage.error('导出失败')
  }
}

onMounted(() => {
  load()
  pollTimer = setInterval(load, 2000)
})
onBeforeUnmount(() => { if (pollTimer) clearInterval(pollTimer) })
</script>

<style scoped>
.page { padding: 16px; }
.header { display: flex; justify-content: space-between; align-items: center; }
</style>
