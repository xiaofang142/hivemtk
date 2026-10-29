<template>
  <div class="app-container">
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>客户同意与偏好</span>
        </div>
      </template>

      <div class="customer-bar">
        <span class="customer-label">客户 ID</span>
        <el-input
          v-model="customerId"
          placeholder="客户统一 ID（one_id）"
          clearable
          style="width: 340px"
          @keyup.enter="loadAll"
        />
        <el-button
          type="primary"
          :loading="loadingTimeline || loadingPrefs"
          @click="loadAll"
        >
          加载
        </el-button>
        <span class="muted">授权事实与偏好都按客户统一身份关联，输入客户 360 中的 one_id。</span>
      </div>
      <el-alert
        v-if="loadError"
        type="error"
        :closable="false"
        show-icon
        :title="loadError"
        class="error-alert"
      />
    </el-card>

    <el-card
      v-loading="loadingTimeline"
      shadow="never"
      class="section-card"
    >
      <template #header>
        <div class="card-header">
          <span>授权时间线</span>
          <el-button
            size="small"
            :disabled="!customerId"
            @click="loadTimeline"
          >
            刷新
          </el-button>
        </div>
      </template>

      <el-alert
        type="info"
        :closable="false"
        show-icon
        class="section-alert"
        title="同意事实账只追加、不覆盖：grant/revoke 都是新事件，最新一条决定当前有效状态，撤回永远压过历史授权。"
      />

      <el-form
        :model="recordForm"
        inline
        class="record-form"
        @submit.prevent
      >
        <el-form-item label="动作">
          <el-radio-group v-model="recordForm.decision">
            <el-radio value="grant">
              授权
            </el-radio>
            <el-radio value="revoke">
              撤回
            </el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="目的">
          <el-select
            v-model="recordForm.purpose"
            style="width: 140px"
          >
            <el-option
              v-for="p in PURPOSE_OPTIONS"
              :key="p.value"
              :label="p.label"
              :value="p.value"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="渠道范围">
          <el-select
            v-model="recordForm.channel_scope"
            clearable
            placeholder="全渠道"
            style="width: 150px"
          >
            <el-option
              v-for="c in CHANNEL_OPTIONS"
              :key="c.value"
              :label="c.label"
              :value="c.value"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="声明版本">
          <el-input
            v-model="recordForm.statement_version"
            placeholder="可选"
            style="width: 130px"
          />
        </el-form-item>
        <el-form-item label="来源">
          <el-select
            v-model="recordForm.source"
            style="width: 130px"
          >
            <el-option
              v-for="s in SOURCE_OPTIONS"
              :key="s.value"
              :label="s.label"
              :value="s.value"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="证据引用">
          <el-input
            v-model="recordForm.evidence_ref"
            placeholder="工单号 / 流水号"
            style="width: 170px"
          />
        </el-form-item>
        <el-form-item>
          <el-button
            type="primary"
            :loading="recording"
            :disabled="!customerId"
            @click="submitRecord"
          >
            追加记录
          </el-button>
        </el-form-item>
      </el-form>

      <el-table
        :data="timeline"
        border
        size="small"
      >
        <el-table-column
          label="事件时间"
          width="170"
        >
          <template #default="{ row }">
            {{ formatTime(row.occurred_at) }}
          </template>
        </el-table-column>
        <el-table-column
          label="决定"
          width="90"
        >
          <template #default="{ row }">
            <el-tag
              :type="row.decision === 'grant' ? 'success' : 'danger'"
              size="small"
            >
              {{ row.decision === 'grant' ? '授权' : '撤回' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="purpose"
          label="目的"
          width="110"
        />
        <el-table-column
          label="渠道范围"
          width="110"
        >
          <template #default="{ row }">
            {{ row.channel_scope ? channelLabel(row.channel_scope) : '全渠道' }}
          </template>
        </el-table-column>
        <el-table-column
          prop="statement_version"
          label="声明版本"
          width="110"
        >
          <template #default="{ row }">
            {{ row.statement_version || '-' }}
          </template>
        </el-table-column>
        <el-table-column
          prop="source"
          label="来源"
          width="120"
        >
          <template #default="{ row }">
            {{ sourceText(row.source) }}
          </template>
        </el-table-column>
        <el-table-column
          prop="evidence_ref"
          label="证据引用"
          min-width="150"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            {{ row.evidence_ref || '-' }}
          </template>
        </el-table-column>
        <el-table-column
          prop="actor"
          label="操作者"
          width="130"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            {{ row.actor || '-' }}
          </template>
        </el-table-column>
        <template #empty>
          <el-empty :description="customerId ? '该客户暂无授权事件' : '请先输入客户 ID 并加载'" />
        </template>
      </el-table>
    </el-card>

    <el-card
      v-loading="loadingPrefs"
      shadow="never"
      class="section-card"
    >
      <template #header>
        <div class="card-header">
          <span>偏好列表</span>
          <div>
            <el-button
              size="small"
              :disabled="!customerId"
              @click="loadPreferences"
            >
              刷新
            </el-button>
            <el-button
              size="small"
              type="primary"
              :disabled="!customerId"
              @click="openPreferenceDialog()"
            >
              新增偏好
            </el-button>
          </div>
        </div>
      </template>

      <el-alert
        type="info"
        :closable="false"
        show-icon
        class="section-alert"
        title="偏好是当前快照：渠道行优先于全渠道行。暂停时 paused_until 留空表示无限期，填写则必须是 RFC3339 时刻。"
      />

      <el-table
        :data="preferences"
        border
        size="small"
      >
        <el-table-column
          prop="purpose"
          label="目的"
          width="120"
        />
        <el-table-column
          label="渠道"
          width="130"
        >
          <template #default="{ row }">
            {{ row.channel ? channelLabel(row.channel) : '全渠道' }}
          </template>
        </el-table-column>
        <el-table-column
          label="暂停"
          width="90"
        >
          <template #default="{ row }">
            <el-tag
              :type="row.paused ? 'warning' : 'success'"
              size="small"
            >
              {{ row.paused ? '已暂停' : '正常' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="暂停截止"
          width="180"
        >
          <template #default="{ row }">
            {{ row.paused && row.paused_until ? formatTime(row.paused_until) : (row.paused ? '无限期' : '-') }}
          </template>
        </el-table-column>
        <el-table-column
          prop="timezone"
          label="时区"
          width="160"
        >
          <template #default="{ row }">
            {{ row.timezone || '-' }}
          </template>
        </el-table-column>
        <el-table-column
          label="更新时间"
          width="170"
        >
          <template #default="{ row }">
            {{ formatTime(row.updated_at) }}
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="90"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button
              link
              type="primary"
              @click="openPreferenceDialog(row)"
            >
              编辑
            </el-button>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty :description="customerId ? '该客户暂无偏好快照' : '请先输入客户 ID 并加载'" />
        </template>
      </el-table>
    </el-card>

    <el-dialog
      v-model="prefDialogVisible"
      :title="prefDialogTitle"
      width="520px"
      :close-on-click-modal="false"
      destroy-on-close
    >
      <el-form
        ref="prefFormRef"
        :model="prefForm"
        :rules="prefRules"
        label-width="110px"
      >
        <el-form-item
          label="目的"
          prop="purpose"
        >
          <el-select
            v-model="prefForm.purpose"
            style="width: 100%"
          >
            <el-option
              v-for="p in PURPOSE_OPTIONS"
              :key="p.value"
              :label="p.label"
              :value="p.value"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="渠道">
          <el-select
            v-model="prefForm.channel"
            clearable
            placeholder="全渠道（默认）"
            style="width: 100%"
          >
            <el-option
              v-for="c in CHANNEL_OPTIONS"
              :key="c.value"
              :label="c.label"
              :value="c.value"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="暂停触达">
          <el-switch
            v-model="prefForm.paused"
            aria-label="暂停该目的/渠道的主动触达"
            @change="onPausedChange"
          />
        </el-form-item>
        <el-form-item
          label="暂停截止"
          prop="paused_until"
        >
          <el-date-picker
            v-model="prefForm.paused_until"
            type="datetime"
            placeholder="留空表示无限期"
            value-format="YYYY-MM-DDTHH:mm:ssZ"
            :disabled="!prefForm.paused"
            style="width: 100%"
          />
          <div class="field-hint">
            必须是 RFC3339 时刻，例如 2026-10-29T18:00:00+08:00；留空表示无限期暂停。
          </div>
        </el-form-item>
        <el-form-item label="时区">
          <el-input
            v-model="prefForm.timezone"
            placeholder="IANA 时区，如 Asia/Shanghai（可空）"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="prefDialogVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          :loading="savingPref"
          @click="submitPreference"
        >
          保存
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { contactConsentApi } from '@/api/contactConsent'
import { CHANNEL_OPTIONS, getChannelLabel } from '@/constants/channel'

const PURPOSE_OPTIONS = [
  { value: 'marketing', label: '主动营销' },
  { value: 'support', label: '客服支持' },
  { value: 'transactional', label: '事务通知' }
]

const SOURCE_OPTIONS = [
  { value: 'api', label: 'API' },
  { value: 'self_service', label: '自助服务' },
  { value: 'import', label: '批量导入' },
  { value: 'legacy_assertion', label: '历史断言' }
]

const RFC3339_RE = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/

const route = useRoute()
const channelLabel = getChannelLabel

const customerId = ref(String(route.query.customer_id || route.query.one_id || ''))
const timeline = ref([])
const preferences = ref([])
const loadingTimeline = ref(false)
const loadingPrefs = ref(false)
const recording = ref(false)
const savingPref = ref(false)
const loadError = ref('')

const recordForm = reactive({
  decision: 'grant',
  purpose: 'marketing',
  channel_scope: '',
  statement_version: '',
  source: 'api',
  evidence_ref: ''
})

const requireCustomerId = () => {
  const id = customerId.value.trim()
  if (!id) {
    ElMessage.warning('请先输入客户 ID')
    return ''
  }
  return id
}

const loadTimeline = async () => {
  const id = requireCustomerId()
  if (!id) return
  loadingTimeline.value = true
  loadError.value = ''
  try {
    const res = await contactConsentApi.getTimeline(id, { limit: 100 })
    timeline.value = res?.list || []
  } catch (e) {
    timeline.value = []
    loadError.value = e.message || '加载授权时间线失败'
  } finally {
    loadingTimeline.value = false
  }
}

const loadPreferences = async () => {
  const id = requireCustomerId()
  if (!id) return
  loadingPrefs.value = true
  try {
    const res = await contactConsentApi.getPreferences(id)
    preferences.value = res?.list || []
  } catch (e) {
    preferences.value = []
    loadError.value = e.message || '加载偏好列表失败'
  } finally {
    loadingPrefs.value = false
  }
}

const loadAll = () => {
  const id = requireCustomerId()
  if (!id) return
  loadError.value = ''
  loadTimeline()
  loadPreferences()
}

const submitRecord = async () => {
  const id = requireCustomerId()
  if (!id) return
  recording.value = true
  try {
    const payload = {
      decision: recordForm.decision,
      purpose: recordForm.purpose,
      channel_scope: recordForm.channel_scope || '',
      statement_version: recordForm.statement_version.trim(),
      source: recordForm.source,
      evidence_ref: recordForm.evidence_ref.trim()
    }
    await contactConsentApi.record(id, payload)
    ElMessage.success(recordForm.decision === 'grant' ? '已追加授权记录' : '已追加撤回记录')
    recordForm.evidence_ref = ''
    loadTimeline()
  } catch (e) {
    ElMessage.error(e.message || '追加记录失败')
  } finally {
    recording.value = false
  }
}

const prefDialogVisible = ref(false)
const prefDialogTitle = ref('')
const prefFormRef = ref()
const prefForm = reactive({
  purpose: 'marketing',
  channel: '',
  paused: false,
  paused_until: '',
  timezone: ''
})

const validatePausedUntil = (rule, value, callback) => {
  if (!prefForm.paused || !value) {
    callback()
    return
  }
  if (!RFC3339_RE.test(value)) {
    callback(new Error('paused_until 必须是 RFC3339 时刻，例如 2026-10-29T18:00:00+08:00'))
    return
  }
  callback()
}

const prefRules = {
  purpose: [{ required: true, message: '请选择目的', trigger: 'change' }],
  paused_until: [{ validator: validatePausedUntil, trigger: 'change' }]
}

// 后端返回的 paused_until 可能带小数秒/时区，el-date-picker 按 value-format
// 严格解析时可能读不出来；统一转成本地时区的 RFC3339 字符串再回填。
const toPickerValue = (value) => {
  if (!value) return ''
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return ''
  const p = (n) => String(n).padStart(2, '0')
  const offset = -d.getTimezoneOffset()
  const sign = offset >= 0 ? '+' : '-'
  const abs = Math.abs(offset)
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}${sign}${p(Math.floor(abs / 60))}:${p(abs % 60)}`
}

const openPreferenceDialog = (row) => {
  prefDialogTitle.value = row ? '编辑偏好' : '新增偏好'
  prefForm.purpose = row?.purpose || 'marketing'
  prefForm.channel = row?.channel || ''
  prefForm.paused = !!row?.paused
  prefForm.paused_until = toPickerValue(row?.paused_until)
  prefForm.timezone = row?.timezone || ''
  prefDialogVisible.value = true
}

const onPausedChange = (paused) => {
  if (!paused) prefForm.paused_until = ''
}

const submitPreference = async () => {
  const id = requireCustomerId()
  if (!id || !prefFormRef.value) return
  await prefFormRef.value.validate(async (valid) => {
    if (!valid) return
    savingPref.value = true
    try {
      await contactConsentApi.setPreference(id, {
        purpose: prefForm.purpose,
        channel: prefForm.channel || '',
        paused: prefForm.paused,
        paused_until: prefForm.paused && prefForm.paused_until ? prefForm.paused_until : '',
        timezone: prefForm.timezone.trim()
      })
      ElMessage.success('偏好已保存')
      prefDialogVisible.value = false
      loadPreferences()
    } catch (e) {
      ElMessage.error(e.message || '保存偏好失败')
    } finally {
      savingPref.value = false
    }
  })
}

const sourceText = (source) => SOURCE_OPTIONS.find((s) => s.value === source)?.label || source || '-'

const formatTime = (value) => {
  if (!value) return '-'
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return '-'
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

onMounted(() => {
  if (customerId.value.trim()) loadAll()
})
</script>

<style scoped>
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.customer-bar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}

.customer-label {
  font-size: 14px;
  color: #606266;
}

.error-alert {
  margin-top: 12px;
}

.section-card {
  margin-top: 16px;
}

.section-alert {
  margin-bottom: 12px;
}

.record-form {
  margin-bottom: 4px;
}

.field-hint {
  margin-top: 4px;
  font-size: 12px;
  color: #909399;
  line-height: 1.5;
}

.muted {
  color: #909399;
  font-size: 13px;
}
</style>
