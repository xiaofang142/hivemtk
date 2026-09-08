<template>
  <div class="whatsapp-matrix">
    <el-row :gutter="16" class="stats-row">
      <el-col :span="6">
        <el-card class="stat-card">
          <div class="stat-label">总发送</div>
          <div class="stat-value">{{ stats.sent }}</div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="stat-card success">
          <div class="stat-label">送达</div>
          <div class="stat-value">{{ stats.delivered }}</div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="stat-card primary">
          <div class="stat-label">已读</div>
          <div class="stat-value">{{ stats.read }}</div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="stat-card danger">
          <div class="stat-label">失败</div>
          <div class="stat-value">{{ stats.failed }}</div>
        </el-card>
      </el-col>
    </el-row>

    <el-card>
      <template #header>
        <div class="card-header">
          <span>WhatsApp 模板批量发送（USR-SM-01）</span>
          <el-button type="primary" @click="showPreview = true">预览模板</el-button>
        </div>
      </template>
      <el-form :model="form" label-width="120px">
        <el-form-item label="选择模板">
          <el-select v-model="form.templateId" filterable>
            <el-option v-for="t in templates" :key="t.id" :label="t.name" :value="t.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="目标群体">
          <el-select v-model="form.audienceType">
            <el-option label="全部客户" value="all" />
            <el-option label="按分群" value="segment" />
            <el-option label="按线索" value="clue" />
            <el-option label="上传 CSV" value="csv" />
          </el-select>
        </el-form-item>
        <el-form-item v-if="form.audienceType === 'segment'" label="选择分群">
          <el-select v-model="form.segmentId" filterable>
            <el-option v-for="s in segments" :key="s.id" :label="s.name" :value="s.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="发送速率">
          <el-input-number v-model="form.ratePerMinute" :min="1" :max="100" />
          <span style="margin-left: 8px; color: #94A3B8">条/分钟（防封号）</span>
        </el-form-item>
        <el-form-item label="变量值（JSON）">
          <el-input v-model="form.variablesJson" type="textarea" :rows="4" placeholder='{"customer.name": "张三", "order.id": "ORD-001"}' />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" :loading="sending" @click="send">开始发送</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <el-card class="progress-card">
      <template #header><span>实时进度</span></template>
      <el-progress :percentage="progress" :status="progressStatus" />
      <el-table :data="progressList" max-height="200">
        <el-table-column prop="phone" label="手机" width="160" />
        <el-table-column prop="status" label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="statusType(row.status)">{{ row.status }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="messageId" label="消息 ID" width="200" />
        <el-table-column prop="error" label="错误" show-overflow-tooltip />
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue';
import { ElMessage } from 'element-plus'
import { http } from '@/utils/request'

const stats = ref({ sent: 0, delivered: 0, read: 0, failed: 0 })
const form = ref({
  templateId: null,
  audienceType: 'all',
  segmentId: null,
  ratePerMinute: 20,
  variablesJson: '{}'
})
const templates = ref([])
const segments = ref([])
const showPreview = ref(false)
const sending = ref(false)
const progress = ref(0)
const progressList = ref([])
const progressStatus = ref('')

async function load() {
  templates.value = (await http.get('/api/whatsapp/templates')) || []
  segments.value = (await http.get('/api/user-segments')) || []
}

const progressStats = computed(() => {
  return { progress: progress.value, status: progressStatus.value }
})

function statusType(status) {
  return {
    sent: 'info',
    delivered: 'success',
    read: 'primary',
    failed: 'danger'
  }[status] || ''
}

async function send() {
  if (!form.value.templateId) return ElMessage.warning('请选择模板')
  let variables;
  try { variables = JSON.parse(form.value.variablesJson) } catch (e) {
    return ElMessage.error('变量 JSON 格式错误')
  }
  sending.value = true
  progressStatus.value = ''
  try {
    const job = await http.post('/api/whatsapp/bulk-send', {
      template_id: form.value.templateId,
      audience: { type: form.value.audienceType, segment_id: form.value.segmentId },
      rate_per_minute: form.value.ratePerMinute,
      variables
    })
    ElMessage.success('已启动发送任务，ID: ' + job.id)
    pollProgress(job.id)
  } finally {
    sending.value = false
  }
}

async function pollProgress(jobId) {
  const timer = setInterval(async () => {
    try {
      const p = await http.get(`/api/whatsapp/jobs/${jobId}/progress`)
      progress.value = p.percentage
      progressList.value = p.recent || []
      stats.value = p.stats || stats.value
      if (p.status === 'done' || p.status === 'failed') {
        clearInterval(timer)
        progressStatus.value = p.status === 'done' ? 'success' : 'exception'
        ElMessage[p.status === 'done' ? 'success' : 'error'](`任务${p.status === 'done' ? '完成' : '失败'}`)
      }
    } catch (_) {
      clearInterval(timer)
    }
  }, 2000)
}

onMounted(load)
</script>

<style scoped>
.whatsapp-matrix { padding: 16px; }
.stats-row { margin-bottom: 16px; }
.stat-card { text-align: center; padding: 8px; }
.stat-label { color: #64748B; font-size: 12px; }
.stat-value { font-size: 32px; font-weight: 700; margin: 8px 0; }
.stat-card.success .stat-value { color: #10B981; }
.stat-card.primary .stat-value { color: #4F46E5; }
.stat-card.danger .stat-value { color: #EF4444; }
.card-header { display: flex; justify-content: space-between; align-items: center; }
.progress-card { margin-top: 16px; }
</style>
