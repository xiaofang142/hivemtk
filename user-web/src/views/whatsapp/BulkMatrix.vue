<template>
  <div class="whatsapp-matrix">
    <el-row
      :gutter="16"
      class="stats-row"
    >
      <el-col :span="6">
        <el-card class="stat-card">
          <div class="stat-label">
            总发送
          </div>
          <div class="stat-value">
            {{ stats.sent }}
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="stat-card success">
          <div class="stat-label">
            送达
          </div>
          <div class="stat-value">
            {{ stats.delivered }}
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="stat-card primary">
          <div class="stat-label">
            已读
          </div>
          <div class="stat-value">
            {{ stats.read }}
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="stat-card danger">
          <div class="stat-label">
            失败
          </div>
          <div class="stat-value">
            {{ stats.failed }}
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-card>
      <template #header>
        <div class="card-header">
          <span>WhatsApp 模板批量发送（USR-SM-01）</span>
          <el-button
            type="primary"
            @click="showPreview = true"
          >
            预览模板
          </el-button>
        </div>
      </template>
      <el-form
        :model="form"
        label-width="120px"
      >
        <el-form-item label="选择模板">
          <el-select
            v-model="form.templateId"
            filterable
          >
            <el-option
              v-for="t in templates"
              :key="t.id"
              :label="t.name"
              :value="t.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="发送对象">
          <el-tag type="info">
            全部 WhatsApp 线索（自动获取）
          </el-tag>
        </el-form-item>
        <el-form-item label="发送速率">
          <el-input-number
            v-model="form.ratePerMinute"
            :min="1"
            :max="100"
          />
          <span style="margin-left: 8px; color: #94A3B8">条/分钟（防封号）</span>
        </el-form-item>
        <el-form-item label="变量值（JSON）">
          <el-input
            v-model="form.variablesJson"
            type="textarea"
            :rows="4"
            placeholder="{&quot;customer.name&quot;: &quot;张三&quot;, &quot;order.id&quot;: &quot;ORD-001&quot;}"
          />
        </el-form-item>
        <el-form-item>
          <el-button
            type="primary"
            :loading="sending"
            @click="send"
          >
            开始发送
          </el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <el-card class="progress-card">
      <template #header>
        <span>实时进度</span>
      </template>
      <el-progress
        :percentage="progress"
        :status="progressStatus"
      />
      <el-table
        :data="progressList"
        max-height="200"
      >
        <el-table-column
          prop="phone"
          label="手机"
          width="160"
        />
        <el-table-column
          prop="status"
          label="状态"
          width="100"
        >
          <template #default="{ row }">
            <el-tag :type="statusType(row.status)">
              {{ row.status }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="messageId"
          label="消息 ID"
          width="200"
        />
        <el-table-column
          prop="error"
          label="错误"
          show-overflow-tooltip
        />
      </el-table>
    </el-card>

    <!-- 预览模板弹窗：此前只有按钮和 showPreview ref，模板里没有任何组件消费它，
         点击「预览模板」毫无反应（2026-11-16 UI 全量回归实测死按钮）。 -->
    <el-dialog
      v-model="showPreview"
      title="模板预览"
      width="560px"
    >
      <template v-if="previewTemplate">
        <el-descriptions
          :column="1"
          border
          size="small"
        >
          <el-descriptions-item label="模板名称">
            {{ previewTemplate.name }}
          </el-descriptions-item>
          <el-descriptions-item label="变量值">
            {{ previewVariablesText }}
          </el-descriptions-item>
        </el-descriptions>
        <div class="preview-bubble">
          {{ previewBody }}
        </div>
      </template>
      <el-empty
        v-else
        description="请先在上方选择模板"
      />
      <template #footer>
        <el-button @click="showPreview = false">
          关闭
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue';
import { ElMessage } from 'element-plus'
import { http } from '@/utils/request'

const stats = ref({ sent: 0, delivered: 0, read: 0, failed: 0 })
const form = ref({
  templateId: null,
  ratePerMinute: 20,
  variablesJson: '{}'
})
const templates = ref([])
const showPreview = ref(false)
const sending = ref(false)
const progress = ref(0)
const progressList = ref([])
const progressStatus = ref('')

async function load() {
  // /api/whatsapp/templates 的 data 不是裸数组：返回 { data: [...] }。http 拦截器
  // 已解一层信封（utils/request.js:155 return data.data），这里必须再解一层，
  // 否则 v-for 遍历的是对象的属性值，option 的 label/value 全 undefined，
  // 模板下拉实际渲染空白且选不中（实测 12 条 ElOption type check 告警）。
  // （2026-11-09 审计移除分群下拉：后端 BulkSend 对 segment/clue/csv 一律
  // 发全部 WhatsApp 线索、segment_id 从不参与过滤——下拉是假选项，删掉
  // 比留着骗人诚实；后端真过滤后可再恢复。）
  const tplRes = await http.get('/api/whatsapp/templates')
  templates.value = tplRes?.data || []
}

const _progressStats = computed(() => {
  return { progress: progress.value, status: progressStatus.value }
})

// 预览：把变量 JSON 插值进模板正文，发给谁之前先看一眼实际效果
const previewTemplate = computed(() => templates.value.find(t => t.id === form.value.templateId) || null)
const previewVariables = computed(() => {
  try { return JSON.parse(form.value.variablesJson || '{}') || {} } catch { return null }
})
const previewVariablesText = computed(() => previewVariables.value === null ? '（变量 JSON 格式错误，发送前请修正）' : JSON.stringify(previewVariables.value))
const previewBody = computed(() => {
  const tpl = previewTemplate.value
  const vars = previewVariables.value
  if (!tpl) return ''
  let text = tpl.content || ''
  if (vars) {
    for (const [k, v] of Object.entries(vars)) {
      text = text.split(`{{${k}}}`).join(String(v))
    }
  }
  return text
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
  try { variables = JSON.parse(form.value.variablesJson) } catch  {
    return ElMessage.error('变量 JSON 格式错误')
  }
  sending.value = true
  progressStatus.value = ''
  try {
    const job = await http.post('/api/whatsapp/bulk-send', {
      template_id: form.value.templateId,
      audience: { type: 'all' },
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
    } catch  {
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
.preview-bubble {
  margin-top: 12px;
  padding: 12px 14px;
  border-radius: 8px;
  background: #E7F3EA;
  color: #1F2937;
  white-space: pre-wrap;
  word-break: break-word;
  line-height: 1.6;
}
.card-header { display: flex; justify-content: space-between; align-items: center; }
.progress-card { margin-top: 16px; }
</style>
