<template>
  <div class="integration-page">
    <el-card class="header-card">
      <div>
        <h2>{{ $t('集成管理') }}</h2>
        <p class="subtitle">管理第三方系统集成和 API 对接</p>
      </div>
      <el-button type="primary" @click="showCreateDialog">
        <el-icon><Plus /></el-icon>
        {{ $t('添加集成') }}
      </el-button>
    </el-card>

    <el-row :gutter="20" class="stats-row">
      <el-col :span="6">
        <el-card>
          <div class="stat-item">
            <div class="stat-label">{{ $t('已启用') }}</div>
            <div class="stat-value" style="color: #10B981">{{ stats.enabled }}</div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card>
          <div class="stat-item">
            <div class="stat-label">{{ $t('已禁用') }}</div>
            <div class="stat-value" style="color: #909399">{{ stats.disabled }}</div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card>
          <div class="stat-item">
            <div class="stat-label">{{ $t('异常') }}</div>
            <div class="stat-value" style="color: #EF4444">{{ stats.error }}</div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card>
          <div class="stat-item">
            <div class="stat-label">{{ $t('总调用次数') }}</div>
            <div class="stat-value">{{ stats.totalCalls }}</div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-card>
      <el-table :data="integrations" v-loading="loading" stripe>
        <el-table-column prop="account_name" :label="$t('集成名称')" min-width="150" />
        <el-table-column prop="platform" :label="$t('类型')" width="120">
          <template #default="{ row }">
            <el-tag>{{ row.platform }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="status" label="状态" width="100">
          <template #default="{ row }">
            <el-switch v-model="row.status" :active-value="1" :inactive-value="0" @change="toggleIntegration(row)" />
          </template>
        </el-table-column>
        <el-table-column prop="last_sync_at" label="最后调用" width="180" />
        <el-table-column prop="callCount" label="调用次数" width="100" />
        <el-table-column prop="errorCount" label="错误次数" width="100" />
        <el-table-column label="操作" width="300" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="testConnection(row)">测试</el-button>
            <el-button link type="primary" @click="viewLogs(row)">日志</el-button>
            <el-button link type="primary" @click="editIntegration(row)">配置</el-button>
            <el-button link type="danger" @click="deleteIntegration(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog v-model="dialogVisible" :title="dialogTitle" width="700px">
      <el-form :model="form" :rules="formRules" ref="formRef" label-width="120px">
        <el-form-item label="集成名称" prop="name">
          <el-input v-model="form.name" />
        </el-form-item>
        <el-form-item label="集成类型" prop="type">
          <el-select v-model="form.type" style="width: 100%">
            <el-option label="Webhook" value="webhook" />
            <el-option label="REST API" value="api" />
            <el-option label="数据库" value="database" />
            <el-option label="消息队列" value="mq" />
            <el-option label="OAuth" value="oauth" />
          </el-select>
        </el-form-item>
        <el-form-item label="接入地址" v-if="form.type !== 'database'">
          <el-input v-model="form.endpoint" placeholder="https://..." />
        </el-form-item>
        <el-form-item label="认证方式" v-if="form.type !== 'database'">
          <el-select v-model="form.authType" style="width: 100%">
            <el-option label="API Key" value="apikey" />
            <el-option label="Bearer Token" value="bearer" />
            <el-option label="Basic Auth" value="basic" />
            <el-option label="OAuth 2.0" value="oauth2" />
            <el-option label="无" value="none" />
          </el-select>
        </el-form-item>
        <el-form-item label="凭据" v-if="form.authType !== 'none' && form.type !== 'database'">
          <el-input v-model="form.credentials" type="textarea" :rows="3" placeholder='{"api_key": "xxx"}' />
        </el-form-item>
        <el-form-item label="数据库连接" v-if="form.type === 'database'">
          <el-input v-model="form.dbConn" placeholder="user:pass@tcp(host)/dbname" />
        </el-form-item>
        <el-form-item label="同步频率">
          <el-select v-model="form.syncInterval" style="width: 100%">
            <el-option label="实时" value="realtime" />
            <el-option label="每分钟" value="1m" />
            <el-option label="每小时" value="1h" />
            <el-option label="每天" value="1d" />
          </el-select>
        </el-form-item>
        <el-form-item label="事件订阅">
          <el-checkbox-group v-model="form.events">
            <el-checkbox label="user.created">用户创建</el-checkbox>
            <el-checkbox label="clue.updated">线索更新</el-checkbox>
          </el-checkbox-group>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="submitForm">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="logsDialogVisible" :title="`${currentLogName} - 同步日志`" width="900px">
      <el-table :data="logs" v-loading="logsLoading" stripe border max-height="500px">
        <el-table-column prop="timestamp" label="时间" width="180" />
        <el-table-column prop="status" label="状态" width="100">
          <template #default="{ row: logRow }">
            <el-tag :type="logRow.status === 'success' ? 'success' : logRow.status === 'error' ? 'danger' : 'warning'">
              {{ logRow.status === 'success' ? '成功' : logRow.status === 'error' ? '失败' : '处理中' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="message" label="消息" min-width="200" show-overflow-tooltip />
        <el-table-column prop="details" label="详情" min-width="250" show-overflow-tooltip />
        <el-table-column label="操作" width="80" fixed="right">
          <template #default="{ row: logRow }">
            <el-button link type="primary" @click="showLogDetail(logRow)">详情</el-button>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty description="暂无同步日志" />
        </template>
      </el-table>
      <template #footer>
        <el-button @click="logsDialogVisible = false">关闭</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="logDetailVisible" title="日志详情" width="600px">
      <el-descriptions :column="1" border>
        <el-descriptions-item label="时间">{{ currentLogDetail?.timestamp }}</el-descriptions-item>
        <el-descriptions-item label="状态">
          <el-tag :type="currentLogDetail?.status === 'success' ? 'success' : currentLogDetail?.status === 'error' ? 'danger' : 'warning'">
            {{ currentLogDetail?.status === 'success' ? '成功' : currentLogDetail?.status === 'error' ? '失败' : '处理中' }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="消息">{{ currentLogDetail?.message }}</el-descriptions-item>
        <el-descriptions-item label="详情">{{ currentLogDetail?.details }}</el-descriptions-item>
      </el-descriptions>
      <template #footer>
        <el-button @click="logDetailVisible = false">关闭</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import { toList } from '@/utils/list'
import {
  getIntegrations,
  createIntegration,
  updateIntegration,
  deleteIntegration as deleteIntegrationApi,
  toggleIntegrationStatus,
  testIntegration,
  getIntegrationLogs
} from '@/api/integration.js'

const loading = ref(false)
const integrations = ref([])
const stats = ref({ enabled: 0, disabled: 0, error: 0, totalCalls: 0 })
const dialogVisible = ref(false)
const dialogTitle = ref('添加集成')
const formRef = ref()
const logsDialogVisible = ref(false)
const logsLoading = ref(false)
const currentLogName = ref('')
const logs = ref([])
const logDetailVisible = ref(false)
const currentLogDetail = ref(null)
const form = ref({
  id: 0,
  name: '',
  type: 'webhook',
  endpoint: '',
  authType: 'none',
  credentials: '',
  dbConn: '',
  syncInterval: '1h',
  events: []
})
const formRules = {
  name: [{ required: true, message: i18n.global.t('请输入集成名称'), trigger: 'blur' }],
  type: [{ required: true, message: i18n.global.t('请选择类型'), trigger: 'change' }]
}

const refreshData = async () => {
  loading.value = true
  try {
    const iRes = await getIntegrations()
    const accounts = iRes?.accounts || toList(iRes)
    integrations.value = accounts
    const enabledCount = accounts.filter(a => a.status === 1).length
    stats.value = {
      enabled: enabledCount,
      disabled: accounts.length - enabledCount,
      error: 0,
      totalCalls: 0
    }
  } finally {
    loading.value = false
  }
}

const showCreateDialog = () => {
  form.value = { id: 0, name: '', type: 'webhook', endpoint: '', authType: 'none', credentials: '', dbConn: '', syncInterval: '1h', events: [] }
  dialogTitle.value = '添加集成'
  dialogVisible.value = true
}

const editIntegration = (row) => {
  form.value = { ...row }
  dialogTitle.value = '编辑集成'
  dialogVisible.value = true
}

const submitForm = async () => {
  if (!formRef.value) return
  await formRef.value.validate(async (valid) => {
    if (!valid) return
    try {
      if (form.value.id) {
        await updateIntegration(form.value.id, form.value)
      } else {
        await createIntegration(form.value)
      }
      ElMessage.success(i18n.global.t('保存成功'))
      dialogVisible.value = false
      refreshData()
    } catch (error) {
      ElMessage.error(i18n.global.t('保存失败'))
    }
  })
}

const toggleIntegration = async (row) => {
  await toggleIntegrationStatus(row.id, row.status)
  ElMessage.success(i18n.global.t('状态已更新'))
  refreshData()
}

const testConnection = async (row) => {
  const loading = ElMessage({ message: i18n.global.t('正在测试...'), type: 'info', duration: 0 })
  try {
    const res = await testIntegration(row.id)
    loading.close()
    if (res?.status === 'ok') {
      ElMessage.success(i18n.global.t('连接成功'))
    } else {
      ElMessage.error('连接失败: ' + (res?.error || ''))
    }
  } catch (error) {
    loading.close()
    ElMessage.error(i18n.global.t('测试失败'))
  }
}

const viewLogs = async (row) => {
  currentLogName.value = row.name
  logsDialogVisible.value = true
  logsLoading.value = true
  logs.value = []
  try {
    const res = await getIntegrationLogs(row.id)
    const data = res;
    if (Array.isArray(data)) {
      logs.value = data
    } else if (data && data.logs) {
      logs.value = data.logs
    }
  } catch (error) {
    ElMessage.error(i18n.global.t('获取日志失败'))
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
    await ElMessageBox.confirm(`确定删除集成 "${row.name}"？`, '确认', { type: 'warning' })
    await deleteIntegrationApi(row.id)
    ElMessage.success(i18n.global.t('删除成功'))
    refreshData()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(i18n.global.t('删除失败'))
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
.logs-content { padding: 0; }
.log-detail-row { margin-bottom: 12px; }
.log-detail-label { font-weight: bold; color: #606266; margin-bottom: 4px; }
</style>
