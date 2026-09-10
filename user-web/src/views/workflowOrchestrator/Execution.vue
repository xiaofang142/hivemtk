<template>
  <div class="workflow-execution-page">
    <el-page-header :content="`执行详情 #${executionId}`" @back="$router.back()" />

    <div v-loading="loading" class="execution-body">
      
      <el-row :gutter="16" class="overview-row">
        <el-col :span="6">
          <el-card>
            <div class="overview-item">
              <span class="label">{{ $t('状态') }}</span>
              <el-tag :type="statusType(execution.status)" effect="dark" size="large">
                {{ statusText(execution.status) }}
              </el-tag>
            </div>
          </el-card>
        </el-col>
        <el-col :span="6">
          <el-card>
            <div class="overview-item">
              <span class="label">{{ $t('工作流') }}</span>
              <span class="value">{{ execution.workflow_id || '-' }}</span>
              <el-tag size="small" style="margin-left: 8px">{{ execution.version }}v</el-tag>
            </div>
          </el-card>
        </el-col>
        <el-col :span="6">
          <el-card>
            <div class="overview-item">
              <span class="label">{{ $t('耗时') }}</span>
              <span class="value">{{ durationText }}</span>
            </div>
          </el-card>
        </el-col>
        <el-col :span="6">
          <el-card>
            <div class="overview-item">
              <span class="label">{{ $t('节点进度') }}</span>
              <span class="value">
                {{ completedCount }} / {{ nodeExecutions.length }}
              </span>
            </div>
            <el-progress
              :percentage="progressPercent"
              :status="progressStatus"
              :stroke-width="8"
              style="margin-top: 8px"
            />
          </el-card>
        </el-col>
      </el-row>

      
      <el-card class="actions-card">
        <el-button
          type="primary"
          :disabled="!isRunning"
          @click="handleStop"
        >
          <el-icon><VideoPause /></el-icon>
          {{ $t('停止执行') }}
        </el-button>
        <el-button @click="loadData">
          <el-icon><Refresh /></el-icon>
          {{ $t('刷新') }}
        </el-button>
      </el-card>

      
      <el-card class="detail-card">
        <template #header>
          <span>{{ $t('触发参数') }}</span>
        </template>
        <el-input
          v-model="triggerPayloadText"
          type="textarea"
          :rows="4"
          readonly
          placeholder="无"
        />
      </el-card>

      
      <el-card v-if="execution.error" class="error-card">
        <template #header>
          <span style="color: var(--el-color-danger)">{{ $t('错误信息') }}</span>
        </template>
        <pre class="error-text">{{ execution.error }}</pre>
      </el-card>

      
      <el-card class="timeline-card">
        <template #header>
          <span>{{ $t('节点执行时间线') }}</span>
        </template>

        <el-timeline v-if="nodeExecutions.length > 0">
          <el-timeline-item
            v-for="(node, idx) in nodeExecutions"
            :key="node.id || idx"
            :timestamp="formatTime(node.started_at)"
            :color="timelineColor(node.status)"
            placement="top"
          >
            <div class="node-item">
              <div class="node-header">
                <el-tag :type="statusType(node.status)" size="small">
                  {{ statusText(node.status) }}
                </el-tag>
                <span class="node-name">{{ node.node_id || `节点 ${idx + 1}` }}</span>
                <span class="node-type">{{ node.node_type }}</span>
                <span v-if="node.duration_ms" class="node-duration">
                  {{ node.duration_ms }}ms
                </span>
              </div>
              <div v-if="node.error_message" class="node-error">
                <el-icon><WarningFilled /></el-icon>
                {{ node.error_message }}
              </div>
              <div v-if="node.output_data" class="node-output">
                <strong>{{ $t('输出') }}:</strong>
                <pre>{{ formatOutput(node.output_data) }}</pre>
              </div>
            </div>
          </el-timeline-item>
        </el-timeline>
        <el-empty v-else :description="$t('暂无节点执行记录')" />
      </el-card>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { VideoPause, Refresh, WarningFilled } from '@element-plus/icons-vue'
import { workflowOrchestratorApi } from '@/api/workflowOrchestrator.js'

const route = useRoute()
const executionId = computed(() => Number(route.params.id) || 0)

const loading = ref(false)
const execution = ref({})
const nodeExecutions = ref([])

const completedCount = computed(() =>
  nodeExecutions.value.filter(n => ['completed', 'failed', 'terminated'].includes(n.status)).length
)
const progressPercent = computed(() => {
  if (nodeExecutions.value.length === 0) return 0
  return Math.round((completedCount.value / nodeExecutions.value.length) * 100)
})
const progressStatus = computed(() => {
  if (execution.value.status === 'failed') return 'exception'
  if (execution.value.status === 'completed') return 'success'
  return ''
})
const isRunning = computed(() => execution.value.status === 'running')

const durationText = computed(() => {
  if (!execution.value.started_at) return '-'
  const start = new Date(execution.value.started_at)
  const end = execution.value.finished_at ? new Date(execution.value.finished_at) : new Date()
  const diff = Math.floor((end - start) / 1000)
  if (diff < 60) return `${diff}s`
  const m = Math.floor(diff / 60)
  const s = diff % 60
  return `${m}m ${s}s`
})

const triggerPayloadText = computed(() => {
  const p = execution.value.trigger_payload
  if (!p) return ''
  try {
    return JSON.stringify(p, null, 2)
  } catch {
    return String(p)
  }
})

const loadData = async () => {
  loading.value = true
  try {
    const [execResult, nodesResult] = await Promise.all([
      workflowOrchestratorApi.getExecution(executionId.value),
      workflowOrchestratorApi.getNodeExecutions(executionId.value)
    ])
    execution.value = execResult?.data || execResult || {}
    nodeExecutions.value = nodesResult?.data || nodesResult?.list || []
  } catch (e) {
    ElMessage.error('加载失败: ' + (e?.message || ''))
  } finally {
    loading.value = false
  }
};

const handleStop = async () => {
  try {
    await ElMessageBox.confirm('确认停止此执行？', '停止确认', { type: 'warning' })
    await workflowOrchestratorApi.stopExecution(executionId.value)
    ElMessage.success('已停止')
    loadData()
  } catch (_) { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
};

const statusType = (s) => ({ running: 'primary', completed: 'success', failed: 'danger', terminated: 'warning', pending: 'info' }[s] || 'info');
const statusText = (s) => ({ running: '运行中', completed: '已完成', failed: '失败', terminated: '已终止', pending: '等待中' }[s] || s)
const timelineColor = (s) => ({ running: 'primary', completed: 'success', failed: 'danger', terminated: 'warning', pending: 'info' }[s] || 'gray')
const formatTime = (t) => {
  if (!t) return ''
  return new Date(t).toLocaleString('zh-CN', { hour12: false })
}
const formatOutput = (d) => {
  if (!d) return ''
  try {
    return typeof d === 'string' ? d : JSON.stringify(d, null, 2)
  } catch {
    return String(d)
  }
}

onMounted(loadData)
</script>

<style scoped>
.workflow-execution-page {
  padding: 16px;
}
.execution-body {
  margin-top: 16px;
}
.overview-row {
  margin-bottom: 16px;
}
.overview-item {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.overview-item .label {
  font-size: 13px;
  color: var(--el-text-color-secondary);
}
.overview-item .value {
  font-size: 16px;
  font-weight: 500;
}
.actions-card, .detail-card, .error-card, .timeline-card {
  margin-bottom: 16px;
}
.error-text {
  margin: 0;
  padding: 12px;
  background: var(--el-color-danger-light-9);
  border-radius: 4px;
  color: var(--el-color-danger);
  font-size: 13px;
  white-space: pre-wrap;
  word-break: break-all;
}
.node-item {
  padding: 8px 0;
}
.node-header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
}
.node-name {
  font-weight: 500;
}
.node-type {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  background: var(--el-fill-color-light);
  padding: 2px 6px;
  border-radius: 3px;
}
.node-duration {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-left: auto;
}
.node-error {
  display: flex;
  align-items: center;
  gap: 4px;
  color: var(--el-color-danger);
  font-size: 13px;
  padding: 6px 10px;
  background: var(--el-color-danger-light-9);
  border-radius: 4px;
  margin-top: 6px;
}
.node-output {
  margin-top: 6px;
  font-size: 12px;
  background: var(--el-fill-color-lighter);
  padding: 8px;
  border-radius: 4px;
}
.node-output pre {
  margin: 4px 0 0;
  white-space: pre-wrap;
  word-break: break-all;
  font-family: inherit;
  color: var(--el-text-color-secondary);
}
</style>
