<template>
  <div class="integration-page">
    <el-card class="header-card">
      <div>
        <h2>{{ $t('集成管理') }}</h2>
        <p class="subtitle">
          管理第三方系统集成和 API 对接
        </p>
      </div>
      <el-button
        type="primary"
        @click="showCreateDialog"
      >
        <el-icon><Plus /></el-icon>
        {{ $t('添加集成') }}
      </el-button>
    </el-card>

    <el-row
      :gutter="20"
      class="stats-row"
    >
      <el-col :span="6">
        <el-card>
          <div class="stat-item">
            <div class="stat-label">
              {{ $t('已启用') }}
            </div>
            <div
              class="stat-value"
              style="color: #10B981"
            >
              {{ stats.enabled }}
            </div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card>
          <div class="stat-item">
            <div class="stat-label">
              {{ $t('已禁用') }}
            </div>
            <div
              class="stat-value"
              style="color: #909399"
            >
              {{ stats.disabled }}
            </div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card>
          <div class="stat-item">
            <div class="stat-label">
              {{ $t('对接平台数') }}
            </div>
            <div class="stat-value">
              {{ stats.platforms }}
            </div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card>
          <div class="stat-item">
            <div class="stat-label">
              {{ $t('账号总数') }}
            </div>
            <div class="stat-value">
              {{ stats.total }}
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-card>
      <el-table
        v-loading="loading"
        :data="integrations"
        stripe
      >
        <el-table-column
          :label="$t('集成名称')"
          min-width="170"
        >
          <template #default="{ row }">
            {{ displayName(row) }}
          </template>
        </el-table-column>
        <el-table-column
          prop="platform"
          :label="$t('平台')"
          width="150"
        >
          <template #default="{ row }">
            <el-tag>{{ platformLabel(row.platform) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="status"
          :label="$t('状态')"
          width="110"
        >
          <template #default="{ row }">
            <el-switch
              :model-value="row.status"
              :active-value="1"
              :inactive-value="0"
              :disabled="togglingId === row.id"
              @change="value => toggleIntegration(row, value)"
            />
          </template>
        </el-table-column>
        <el-table-column
          prop="webhook_url"
          :label="$t('回调地址')"
          min-width="220"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            {{ row.webhook_url || '-' }}
          </template>
        </el-table-column>
        <el-table-column
          prop="last_sync_at"
          :label="$t('最后同步')"
          width="180"
        >
          <template #default="{ row }">
            {{ formatTime(row.last_sync_at) }}
          </template>
        </el-table-column>
        <el-table-column
          :label="$t('操作')"
          width="260"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button
              link
              type="primary"
              @click="testConnection(row)"
            >
              {{ $t('测试') }}
            </el-button>
            <el-button
              link
              type="primary"
              @click="viewLogs(row)"
            >
              {{ $t('日志') }}
            </el-button>
            <el-button
              link
              type="primary"
              @click="editIntegration(row)"
            >
              {{ $t('配置') }}
            </el-button>
            <el-button
              link
              type="danger"
              @click="deleteIntegration(row)"
            >
              {{ $t('删除') }}
            </el-button>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty :description="$t('暂无集成账号')" />
        </template>
      </el-table>
    </el-card>

    <el-dialog
      v-model="dialogVisible"
      :title="dialogTitle"
      width="700px"
    >
      <el-form
        ref="formRef"
        :model="form"
        :rules="formRules"
        label-width="120px"
      >
        <el-form-item
          :label="$t('对接平台')"
          prop="platform"
        >
          <el-select
            v-model="form.platform"
            :disabled="isEdit"
            style="width: 100%"
          >
            <el-option
              v-for="option in platformOptions"
              :key="option.value"
              :label="option.label"
              :value="option.value"
            />
          </el-select>
        </el-form-item>
        <el-form-item
          :label="$t('账号名称')"
          prop="account_name"
        >
          <el-input
            v-model="form.account_name"
            :placeholder="$t('用于在列表里识别这个账号')"
          />
        </el-form-item>
        <el-form-item
          :label="$t('API Key')"
          prop="api_key"
        >
          <el-input v-model="form.api_key" />
        </el-form-item>
        <el-form-item
          :label="$t('API Secret')"
          prop="api_secret"
        >
          <el-input
            v-model="form.api_secret"
            type="password"
            show-password
            :placeholder="isEdit ? $t('留空则保留原密钥') : ''"
          />
        </el-form-item>
        <el-form-item
          :label="$t('回调地址')"
          prop="webhook_url"
        >
          <el-input
            v-model="form.webhook_url"
            placeholder="https://..."
          />
        </el-form-item>
        <el-form-item
          :label="$t('扩展配置 (JSON)')"
          prop="config"
        >
          <el-input
            v-model="form.config"
            type="textarea"
            :rows="4"
            placeholder="{&quot;sync_products&quot;: true}"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">
          {{ $t('取消') }}
        </el-button>
        <el-button
          type="primary"
          :loading="submitting"
          @click="submitForm"
        >
          {{ $t('保存') }}
        </el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="logsDialogVisible"
      :title="logsDialogTitle"
      width="900px"
    >
      <el-table
        v-loading="logsLoading"
        :data="logs"
        stripe
        border
        max-height="500px"
      >
        <el-table-column
          prop="sync_type"
          :label="$t('同步类型')"
          width="120"
        />
        <el-table-column
          prop="status"
          :label="$t('状态')"
          width="100"
        >
          <template #default="{ row: logRow }">
            <el-tag :type="syncStatusTag(logRow.status)">
              {{ syncStatusLabel(logRow.status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="record_count"
          :label="$t('记录数')"
          width="90"
        />
        <el-table-column
          prop="start_time"
          :label="$t('开始时间')"
          width="180"
        >
          <template #default="{ row: logRow }">
            {{ formatTime(logRow.start_time) }}
          </template>
        </el-table-column>
        <el-table-column
          prop="end_time"
          :label="$t('结束时间')"
          width="180"
        >
          <template #default="{ row: logRow }">
            {{ formatTime(logRow.end_time) }}
          </template>
        </el-table-column>
        <el-table-column
          prop="error_message"
          :label="$t('错误信息')"
          min-width="200"
          show-overflow-tooltip
        >
          <template #default="{ row: logRow }">
            {{ logRow.error_message || '-' }}
          </template>
        </el-table-column>
        <el-table-column
          :label="$t('操作')"
          width="80"
          fixed="right"
        >
          <template #default="{ row: logRow }">
            <el-button
              link
              type="primary"
              @click="showLogDetail(logRow)"
            >
              {{ $t('详情') }}
            </el-button>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty :description="$t('该平台近期没有同步日志')" />
        </template>
      </el-table>
      <template #footer>
        <el-button @click="logsDialogVisible = false">
          {{ $t('关闭') }}
        </el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="logDetailVisible"
      :title="$t('日志详情')"
      width="600px"
    >
      <el-descriptions
        :column="1"
        border
      >
        <el-descriptions-item :label="$t('平台')">
          {{ currentLogDetail?.platform }}
        </el-descriptions-item>
        <el-descriptions-item :label="$t('同步类型')">
          {{ currentLogDetail?.sync_type }}
        </el-descriptions-item>
        <el-descriptions-item :label="$t('状态')">
          <el-tag :type="syncStatusTag(currentLogDetail?.status)">
            {{ syncStatusLabel(currentLogDetail?.status) }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="$t('记录数')">
          {{ currentLogDetail?.record_count ?? 0 }}
        </el-descriptions-item>
        <el-descriptions-item :label="$t('开始时间')">
          {{ formatTime(currentLogDetail?.start_time) }}
        </el-descriptions-item>
        <el-descriptions-item :label="$t('结束时间')">
          {{ formatTime(currentLogDetail?.end_time) }}
        </el-descriptions-item>
        <el-descriptions-item :label="$t('错误信息')">
          {{ currentLogDetail?.error_message || '-' }}
        </el-descriptions-item>
      </el-descriptions>
      <template #footer>
        <el-button @click="logDetailVisible = false">
          {{ $t('关闭') }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import { toList } from '@/utils/list'
import { isUserReportedError } from '@/utils/errorReporting'
import { formatDate } from '@/utils/format'
import {
  getIntegrations,
  createIntegration,
  updateIntegration,
  deleteIntegration as deleteIntegrationApi,
  toggleIntegrationStatus,
  testIntegration,
  getIntegrationLogs
} from '@/api/integration.js'

const PLATFORM_LABELS = {
  crm_xiaoshouyi: '销售易 CRM',
  crm_fenxiangxiao: '纷享销客 CRM',
  ecommerce_taobao: '淘宝',
  ecommerce_jd: '京东',
  douyin: '抖音'
}

const LOG_PAGE_SIZE = 100

const loading = ref(false)
const submitting = ref(false)
const integrations = ref([])
const stats = ref({ enabled: 0, disabled: 0, platforms: 0, total: 0 })
const dialogVisible = ref(false)
const dialogTitle = ref('')
const isEdit = ref(false)
const formRef = ref()
const togglingId = ref(0)
const logsDialogVisible = ref(false)
const logsLoading = ref(false)
const currentLogAccount = ref(null)
const logs = ref([])
const logDetailVisible = ref(false)
const currentLogDetail = ref(null)

const emptyForm = () => ({
  id: 0,
  platform: 'crm_xiaoshouyi',
  account_name: '',
  api_key: '',
  api_secret: '',
  webhook_url: '',
  config: ''
})

const form = ref(emptyForm())

const validateConfig = (rule, value, callback) => {
  if (!value || !value.trim()) return callback()
  let parsed
  try {
    parsed = JSON.parse(value)
  } catch {
    return callback(new Error(i18n.global.t('扩展配置需是合法 JSON')))
  }
  if (parsed === null || typeof parsed !== 'object' || Array.isArray(parsed)) {
    return callback(new Error(i18n.global.t('扩展配置需是 JSON 对象')))
  }
  callback()
}

const formRules = {
  platform: [{ required: true, message: i18n.global.t('请选择对接平台'), trigger: 'change' }],
  config: [{ validator: validateConfig, trigger: 'blur' }]
}

const platformOptions = computed(() => {
  const known = Object.entries(PLATFORM_LABELS).map(([value, label]) => ({ value, label }))
  const seen = new Set(known.map(o => o.value))
  integrations.value.forEach(row => {
    if (row.platform && !seen.has(row.platform)) {
      seen.add(row.platform)
      known.push({ value: row.platform, label: row.platform })
    }
  })
  return known
})

const displayName = (row) => row.account_name || row.platform || `#${row.id}`

const platformLabel = (platform) => PLATFORM_LABELS[platform] || platform || '-'

const formatTime = (value) => {
  const text = value ? formatDate(value, undefined, { dateStyle: 'short', timeStyle: 'short' }) : ''
  return text || '-'
}

const syncStatusLabel = (status) => {
  if (status === 1) return i18n.global.t('成功')
  if (status === 2) return i18n.global.t('失败')
  return i18n.global.t('进行中')
}

const syncStatusTag = (status) => (status === 1 ? 'success' : status === 2 ? 'danger' : 'warning')

const logsDialogTitle = computed(() => {
  const row = currentLogAccount.value
  return row ? `${displayName(row)} - ${i18n.global.t('同步日志')}` : i18n.global.t('同步日志')
})

const refreshData = async () => {
  loading.value = true
  try {
    const accounts = toList(await getIntegrations())
    integrations.value = accounts
    const enabledCount = accounts.filter(a => a.status === 1).length
    stats.value = {
      enabled: enabledCount,
      disabled: accounts.length - enabledCount,
      platforms: new Set(accounts.map(a => a.platform).filter(Boolean)).size,
      total: accounts.length
    }
  } catch (error) {
    ElMessage.error(i18n.global.t('获取集成列表失败'))
  } finally {
    loading.value = false
  }
}

const showCreateDialog = () => {
  form.value = emptyForm()
  isEdit.value = false
  dialogTitle.value = i18n.global.t('添加集成')
  dialogVisible.value = true
}

const editIntegration = (row) => {
  form.value = {
    id: row.id,
    platform: row.platform,
    account_name: row.account_name || '',
    api_key: row.api_key || '',
    // 列表回传的 api_secret 是掩码值，回填后原样提交会把掩码写进库
    api_secret: '',
    webhook_url: row.webhook_url || '',
    config: prettyConfig(row.config)
  }
  isEdit.value = true
  dialogTitle.value = `${i18n.global.t('编辑集成')} - ${displayName(row)}`
  dialogVisible.value = true
}

const prettyConfig = (raw) => {
  if (!raw) return ''
  try {
    return JSON.stringify(JSON.parse(raw), null, 2)
  } catch {
    return ''
  }
}

const buildPayload = () => {
  const payload = {
    account_name: form.value.account_name,
    api_key: form.value.api_key,
    webhook_url: form.value.webhook_url
  }
  // 平台的归属在创建时定死，更新接口不接受 platform
  if (!isEdit.value) payload.platform = form.value.platform
  if (form.value.api_secret) payload.api_secret = form.value.api_secret
  if (form.value.config.trim()) payload.config = JSON.parse(form.value.config)
  return payload
}

const submitForm = async () => {
  if (!formRef.value) return
  await formRef.value.validate(async (valid) => {
    if (!valid) return
    submitting.value = true
    try {
      if (isEdit.value) {
        await updateIntegration(form.value.id, buildPayload())
      } else {
        await createIntegration(buildPayload())
      }
      ElMessage.success(i18n.global.t('保存成功'))
      dialogVisible.value = false
      refreshData()
    } catch (error) {
      if (!isUserReportedError(error)) ElMessage.error(error?.message || i18n.global.t('保存失败'))
    } finally {
      submitting.value = false
    }
  })
}

const toggleIntegration = async (row, nextStatus) => {
  togglingId.value = row.id
  try {
    await toggleIntegrationStatus(row.id, nextStatus)
    row.status = nextStatus
    ElMessage.success(i18n.global.t('状态已更新'))
  } catch (error) {
    if (!isUserReportedError(error)) ElMessage.error(error?.message || i18n.global.t('状态更新失败'))
  } finally {
    togglingId.value = 0
  }
}

const testConnection = async (row) => {
  const pending = ElMessage({ message: i18n.global.t('正在测试...'), type: 'info', duration: 0 })
  try {
    const res = await testIntegration(row.id)
    pending.close()
    if (res?.status === 'ok') {
      ElMessage.success(i18n.global.t('连接成功'))
    } else {
      ElMessage.error(i18n.global.t('连接失败'))
    }
  } catch (error) {
    pending.close()
    if (!isUserReportedError(error)) ElMessage.error(error?.message || i18n.global.t('测试失败'))
  }
}

const viewLogs = async (row) => {
  currentLogAccount.value = row
  logs.value = []
  logsDialogVisible.value = true
  logsLoading.value = true
  try {
    const data = await getIntegrationLogs({ page: 1, page_size: LOG_PAGE_SIZE })
    logs.value = toList(data).filter(log => log.platform === row.platform)
  } catch (error) {
    if (!isUserReportedError(error)) ElMessage.error(error?.message || i18n.global.t('获取日志失败'))
  } finally {
    logsLoading.value = false
  }
}

const showLogDetail = (row) => {
  currentLogDetail.value = row
  logDetailVisible.value = true
}

const deleteIntegration = async (row) => {
  try {
    await ElMessageBox.confirm(`${i18n.global.t('确定删除集成')} "${displayName(row)}"?`, i18n.global.t('确认'), { type: 'warning' })
  } catch {
    return
  }
  try {
    await deleteIntegrationApi(row.id)
    ElMessage.success(i18n.global.t('删除成功'))
    refreshData()
  } catch (error) {
    if (!isUserReportedError(error)) ElMessage.error(error?.message || i18n.global.t('删除失败'))
  }
}

onMounted(() => refreshData())
</script>

<style scoped lang="scss">
.integration-page { padding: 20px; }
.header-card {
  margin-bottom: 20px;
  :deep(.el-card__body) { display: flex; justify-content: space-between; align-items: center; }
  h2 { margin: 0 0 8px 0; }
  .subtitle { color: #909399; margin: 0; }
}
.stats-row {
  margin-bottom: 20px;
  .stat-item {
    text-align: center;
    .stat-label { color: #909399; font-size: 14px; margin-bottom: 10px; }
    .stat-value { font-size: 28px; font-weight: bold; }
  }
}
</style>
