<template>
  <div class="page">
    <div class="header">
      <h2>执行监控 #{{ sessionId }}</h2>
      <el-space>
        <el-tag v-if="session" :type="{ completed: 'success', failed: 'danger', stopped: 'info', active: 'warning' }[session.status] || 'info'">
          {{ session.status }}
        </el-tag>
        <el-button v-if="session && ['created','active'].includes(session.status)" type="danger" @click="onStop">停止</el-button>
      </el-space>
    </div>

    <el-card v-if="session" style="margin-top: 12px">
      <el-descriptions :column="4" border>
        <el-descriptions-item label="耗时">{{ (session.duration_ms / 1000).toFixed(1) }}s</el-descriptions-item>
        <el-descriptions-item label="成功率">{{ session.total_steps ? `${session.success_steps}/${session.total_steps}` : '—' }}</el-descriptions-item>
        <el-descriptions-item label="Hand 延迟">{{ session.hand_latency_ms }}ms</el-descriptions-item>
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

    <el-card v-if="session?.final_screenshot_url" header="最终截图" style="margin-top: 12px">
      <el-image :src="session.final_screenshot_url" fit="contain" style="max-width: 100%" :preview-src-list="[session.final_screenshot_url]" />
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { getBrowserSession, getBrowserSessionSteps, stopBrowserSession } from '@/api/browserAutomation'

const route = useRoute()
const sessionId = computed(() => route.params.id)
const session = ref(null)
const steps = ref([])
const loading = ref(false)
let pollTimer = null

const unpack = (res) => res?.data ?? res

async function load() {
  loading.value = steps.value.length === 0
  try {
    const [sRes, stRes] = await Promise.all([
      getBrowserSession(sessionId.value),
      getBrowserSessionSteps(sessionId.value),
    ])
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
  ElMessage.success('中断信号已发送')
  load()
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
