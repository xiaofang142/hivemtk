<template>
  <div class="openapi-integration">
    
    <el-alert
      :title="$t('OpenAPI 数据源集成')"
      type="info"
      :closable="false"
      show-icon
    >
      <template #default>
        通过配置外部 OpenAPI 接口,自动将返回的数据导入到知识库。支持 GET / POST / RESTful 风格,支持 Bearer / API Key / HMAC / Basic 认证。
      </template>
    </el-alert>

    
    <el-card class="filter-card">
      <div class="filter-bar">
        <el-select v-model="filter.product_id" placeholder="选择产品(可选)" clearable style="width: 220px" @change="loadSources">
          <el-option v-for="p in productList" :key="p.id" :label="p.name" :value="p.id" />
        </el-select>
        <el-button :icon="Refresh" @click="loadSources">刷新</el-button>
        <el-button type="primary" :icon="Plus" @click="showCreateDialog = true">新建数据源</el-button>
      </div>
    </el-card>

    
    <el-card>
      <el-table :data="sources" v-loading="loading" stripe>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="name" label="名称" min-width="160" show-overflow-tooltip />
        <el-table-column label="类型" width="80">
          <template #default="{ row }">
            <el-tag size="small">{{ row.type?.toUpperCase() }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="方法" width="80">
          <template #default="{ row }">
            <el-tag :type="getMethodTagType(row.method)" size="small">{{ getMethodLabel(row.method) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="endpoint" label="端点" min-width="280" show-overflow-tooltip>
          <template #default="{ row }">
            <el-text size="small" style="font-family: monospace">{{ row.endpoint }}</el-text>
          </template>
        </el-table-column>
        <el-table-column label="认证" width="100">
          <template #default="{ row }">{{ authTypeLabel(row.auth_type) }}</template>
        </el-table-column>
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-switch v-model="row._enabled" @change="(val) => handleToggle(row, val)" />
          </template>
        </el-table-column>
        <el-table-column label="上次同步" width="180">
          <template #default="{ row }">
            <div>{{ row.last_sync_at ? formatDate(row.last_sync_at) : '从未同步' }}</div>
            <el-tag :type="getLastStatusTagType(row.last_status)" size="small">
              {{ getLastStatusLabel(row.last_status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="total_synced" label="累计同步" width="100" />
        <el-table-column label="操作" width="240" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="showEditDialog(row)">编辑</el-button>
            <el-button link type="success" size="small" @click="handleTest(row)">测试</el-button>
            <el-button link type="primary" size="small" @click="handleSync(row)">立即同步</el-button>
            <el-button link type="danger" size="small" @click="handleDelete(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loading && sources.length === 0" description="还没有数据源,点击右上角「新建数据源」开始" />
    </el-card>

    
    <el-dialog v-model="showCreateDialog" :title="editingId ? '编辑数据源' : '新建数据源'" width="720px" :close-on-click-modal="false">
      <el-form :model="form" :rules="rules" ref="formRef" label-width="100px">
        <el-form-item label="名称" prop="name">
          <el-input v-model="form.name" placeholder="如:产品文档同步" />
        </el-form-item>
        <el-form-item label="关联产品" prop="product_id">
          <el-select v-model="form.product_id" placeholder="选择产品" style="width: 100%">
            <el-option v-for="p in productList" :key="p.id" :label="p.name" :value="p.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="端点 URL" prop="endpoint">
          <el-input v-model="form.endpoint" placeholder="https://api.example.com/v1/docs" />
        </el-form-item>
        <el-form-item label="请求方法">
          <el-radio-group v-model="form.method">
            <el-radio-button label="GET">GET</el-radio-button>
            <el-radio-button label="POST">POST</el-radio-button>
            <el-radio-button label="PUT">PUT</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="认证方式">
          <el-select v-model="form.auth_type" style="width: 100%">
            <el-option label="无认证" value="none" />
            <el-option label="Bearer Token" value="bearer" />
            <el-option label="API Key" value="api_key" />
            <el-option label="HMAC 签名" value="hmac" />
            <el-option label="Basic Auth" value="basic" />
          </el-select>
        </el-form-item>
        <el-form-item v-if="form.auth_type === 'bearer'" label="Token">
          <el-input v-model="form.auth_token" placeholder="Bearer Token" show-password />
        </el-form-item>
        <el-form-item v-if="form.auth_type === 'api_key'" label="API Key">
          <el-input v-model="form.auth_token" placeholder="API Key 值" show-password />
        </el-form-item>
        <el-form-item v-if="form.auth_type === 'hmac'" label="HMAC Secret">
          <el-input v-model="form.auth_token" placeholder="HMAC 签名密钥" show-password />
        </el-form-item>
        <template v-if="form.auth_type === 'basic'">
          <el-form-item label="用户名">
            <el-input v-model="form.auth_username" placeholder="Basic Auth 用户名" />
          </el-form-item>
          <el-form-item label="密码">
            <el-input v-model="form.auth_token" type="password" placeholder="Basic Auth 密码" show-password />
          </el-form-item>
        </template>
        <el-form-item label="请求模板" v-if="form.method === 'POST' || form.method === 'PUT'">
          <el-input v-model="form.request_template" type="textarea" :rows="4" placeholder="支持模板变量: {{ now }}, {{ timestamp }}, {{ date }}, {{ last_sync_at }}" />
          <div class="form-tip">JSON 格式,可使用 Go template 语法,<code v-pre>{{.last_sync_at}}</code> 用于增量同步</div>
        </el-form-item>
        <el-form-item label="响应路径">
          <el-input v-model="form.response_path" placeholder="data.items" />
          <div class="form-tip">JSON 响应中数据数组的路径,留空则期望整个响应是数组</div>
        </el-form-item>
        <el-form-item label="字段映射">
          <el-input v-model="form.field_mapping" type="textarea" :rows="3" placeholder='{"title": "name", "content": "body", "ref": "id"}' />
          <div class="form-tip">JSON 格式,指定 title/content/ref 对应的源字段,留空使用默认字段</div>
        </el-form-item>
        <el-form-item label="定时任务">
          <el-input v-model="form.schedule" placeholder="如: 0 */6 * * * (cron 表达式,留空表示仅手动同步)" />
        </el-form-item>
        <el-form-item label="启用">
          <el-switch v-model="form.enabled" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showCreateDialog = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="handleSave">保存</el-button>
      </template>
    </el-dialog>

    
    <el-dialog v-model="showTestResultDialog" title="连接测试结果" width="600px">
      <el-descriptions :column="1" border v-if="testResult">
        <el-descriptions-item label="是否成功">
          <el-tag :type="testResult.success ? 'success' : 'danger'">
            {{ testResult.success ? '成功' : '失败' }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item v-if="testResult.status_code !== undefined" label="HTTP 状态码">
          <el-tag :type="testResult.status_code < 400 ? 'success' : 'danger'">{{ testResult.status_code }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item v-if="testResult.latency_ms !== undefined" label="响应耗时">
          {{ testResult.latency_ms }} ms
        </el-descriptions-item>
        <el-descriptions-item v-if="testResult.body_size !== undefined" label="响应大小">
          {{ formatFileSize(testResult.body_size) }}
        </el-descriptions-item>
        <el-descriptions-item v-if="testResult.error" label="错误信息">
          <el-text type="danger">{{ testResult.error }}</el-text>
        </el-descriptions-item>
        <el-descriptions-item v-if="testResult.body_sample" label="响应预览">
          <pre class="response-preview">{{ testResult.body_sample }}</pre>
        </el-descriptions-item>
      </el-descriptions>
    </el-dialog>

    
    <el-dialog v-model="showSyncResultDialog" title="同步结果" width="600px">
      <el-descriptions :column="2" border v-if="syncResult">
        <el-descriptions-item label="数据源ID">{{ syncResult.source_id }}</el-descriptions-item>
        <el-descriptions-item label="状态">
          <el-tag :type="syncResult.status === 'success' ? 'success' : 'danger'">{{ syncResult.status === 'success' ? '成功' : '失败' }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="获取条目">{{ syncResult.total_items }}</el-descriptions-item>
        <el-descriptions-item label="已导入">{{ syncResult.imported_num }}</el-descriptions-item>
        <el-descriptions-item label="已跳过">{{ syncResult.skipped_num }}</el-descriptions-item>
        <el-descriptions-item label="失败">{{ syncResult.failed_num }}</el-descriptions-item>
        <el-descriptions-item label="耗时" :span="2">{{ syncResult.duration_ms }} ms</el-descriptions-item>
        <el-descriptions-item v-if="syncResult.error_msg" label="错误信息" :span="2">
          <el-text type="danger">{{ syncResult.error_msg }}</el-text>
        </el-descriptions-item>
      </el-descriptions>
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Refresh } from '@element-plus/icons-vue'
import { knowledgeAPI } from '@/api/knowledge'
import { ragProductConfigAPI } from '@/api/ragProductConfig'
import { getAuthTypeLabel } from '@/constants/authType';
import { PASS_FAIL_STATUS, getStatusLabel, getStatusTagType } from '@/constants/status'

const HTTP_METHOD_TAG = { GET: '', POST: 'success', PUT: 'warning', DELETE: 'danger', PATCH: 'warning' };
const getMethodLabel = (m) => (m || '').toUpperCase()
const getMethodTagType = (m) => HTTP_METHOD_TAG[(m || '').toUpperCase()] || 'info'
const getLastStatusLabel = (s) => (s ? getStatusLabel(s, PASS_FAIL_STATUS) : '待同步');
const getLastStatusTagType = (s) => (s ? getStatusTagType(s, PASS_FAIL_STATUS) : 'info')

const loading = ref(false)
const saving = ref(false)
const showCreateDialog = ref(false)
const showTestResultDialog = ref(false)
const showSyncResultDialog = ref(false)
const editingId = ref(null)
const formRef = ref(null)

const productList = ref([])
const sources = ref([])
const testResult = ref(null)
const syncResult = ref(null)

const filter = reactive({ product_id: '' })

const form = reactive({
  name: '',
  product_id: '',
  endpoint: '',
  method: 'GET',
  auth_type: 'none',
  auth_token: '',
  auth_username: '',
  request_template: '',
  response_path: '',
  field_mapping: '',
  schedule: '',
  enabled: true
})

const rules = {
  name: [{ required: true, message: i18n.global.t('请输入名称'), trigger: 'blur' }],
  product_id: [{ required: true, message: i18n.global.t('请选择产品'), trigger: 'change' }],
  endpoint: [{ required: true, message: i18n.global.t('请输入端点 URL'), trigger: 'blur' }]
}

const loadAll = async () => {
  await Promise.all([loadProducts(), loadSources()])
}

const loadProducts = async () => {
  try {
    const res = await ragProductConfigAPI.listProducts()
    if (Array.isArray(res)) {
      productList.value = res
    } else if (Array.isArray(res?.items)) {
      productList.value = res.items
    }
  } catch (e) {
    console.error('加载产品列表失败:', e)
  }
}

const loadSources = async () => {
  loading.value = true
  try {
    const res = await knowledgeAPI.listOpenAPISources({ product_id: filter.product_id })
    const list = res?.items || []
    list.forEach(s => { s._enabled = s.enabled === 1 })
    sources.value = list
  } catch (e) {
    ElMessage.error('加载数据源失败: ' + (e.message || ''))
  } finally {
    loading.value = false
  }
}

const resetForm = () => {
  Object.assign(form, {
    name: '',
    product_id: '',
    endpoint: '',
    method: 'GET',
    auth_type: 'none',
    auth_token: '',
    auth_username: '',
    request_template: '',
    response_path: '',
    field_mapping: '',
    schedule: '',
    enabled: true
  })
  editingId.value = null
  formRef.value?.clearValidate()
}

const showEditDialog = (row) => {
  Object.assign(form, {
    name: row.name,
    product_id: row.product_id,
    endpoint: row.endpoint,
    method: row.method,
    auth_type: row.auth_type,
    auth_token: '',
    auth_username: '',
    request_template: row.request_template,
    response_path: row.response_path,
    field_mapping: row.field_mapping,
    schedule: row.schedule,
    enabled: row.enabled === 1
  })
  try {
    const authCfg = row.auth_config ? JSON.parse(row.auth_config) : {}
    if (authCfg.token) form.auth_token = authCfg.token
    if (authCfg.secret) form.auth_token = authCfg.secret
    if (authCfg.username) form.auth_username = authCfg.username
  } catch (e) { console.warn("[request] 后台调用失败(已忽略):", e) }
  editingId.value = row.id
  showCreateDialog.value = true
}

const handleSave = async () => {
  try {
    await formRef.value.validate()
  } catch {
    return;
  }
  saving.value = true
  try {
    const authConfig = {};
    if (form.auth_type === 'bearer' || form.auth_type === 'api_key' || form.auth_type === 'hmac') {
      authConfig.token = form.auth_token
      authConfig.secret = form.auth_token
    } else if (form.auth_type === 'basic') {
      authConfig.username = form.auth_username
      authConfig.password = form.auth_token
    }
    const payload = {
      name: form.name,
      product_id: form.product_id,
      endpoint: form.endpoint,
      method: form.method,
      type: 'rest',
      auth_type: form.auth_type,
      auth_config: JSON.stringify(authConfig),
      request_template: form.request_template,
      response_path: form.response_path,
      field_mapping: form.field_mapping,
      schedule: form.schedule,
      enabled: form.enabled ? 1 : 0
    }
    if (editingId.value) {
      await knowledgeAPI.updateOpenAPISource(editingId.value, payload)
      ElMessage.success(i18n.global.t('更新成功'))
    } else {
      await knowledgeAPI.createOpenAPISource(payload)
      ElMessage.success(i18n.global.t('创建成功'))
    }
    showCreateDialog.value = false
    resetForm()
    loadSources()
  } catch (e) {
    if (e !== false) {
      ElMessage.error('保存失败: ' + (e.message || ''))
    }
  } finally {
    saving.value = false
  }
}

const handleTest = async (row) => {
  try {
    const payload = {
      name: row.name,
      endpoint: row.endpoint,
      method: row.method,
      auth_type: row.auth_type,
      auth_config: row.auth_config,
      request_template: row.request_template,
      response_path: row.response_path
    }
    const res = await knowledgeAPI.testOpenAPISource(row.id, payload)
    testResult.value = res
    showTestResultDialog.value = true
  } catch (e) {
    ElMessage.error('测试失败: ' + (e.message || ''))
  }
}

const handleSync = async (row) => {
  try {
    await ElMessageBox.confirm(`确认立即同步数据源「${row.name}」吗?`, '立即同步', { type: 'info' })
    const res = await knowledgeAPI.syncOpenAPISource(row.id, { product_id: row.product_id })
    syncResult.value = res
    showSyncResultDialog.value = true
    loadSources()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error('同步失败: ' + (e.message || ''))
  }
}

const handleToggle = async (row, enabled) => {
  try {
    await knowledgeAPI.toggleOpenAPISource(row.id, { enabled }, { product_id: row.product_id })
    ElMessage.success(enabled ? '已启用' : '已禁用')
  } catch (e) {
    row._enabled = !enabled
    ElMessage.error('切换失败: ' + (e.message || ''))
  }
}

const handleDelete = async (row) => {
  try {
    await ElMessageBox.confirm(`确认删除数据源「${row.name}」吗?已同步的数据不会被删除`, '删除数据源', { type: 'warning' })
    await knowledgeAPI.deleteOpenAPISource(row.id, { product_id: row.product_id })
    ElMessage.success(i18n.global.t('删除成功'))
    loadSources()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error('删除失败: ' + (e.message || ''))
  }
}

const authTypeLabel = (t) => getAuthTypeLabel(t)
const formatDate = (d) => d ? new Date(d).toLocaleString('zh-CN') : '-'
const formatFileSize = (b) => {
  if (!b || b <= 0) return '-'
  if (b < 1024) return b + ' B'
  if (b < 1024 * 1024) return (b / 1024).toFixed(1) + ' KB'
  return (b / 1024 / 1024).toFixed(1) + ' MB'
}

onMounted(() => {
  loadAll()
})
</script>

<style scoped lang="scss">
.openapi-integration {
  .el-alert {
    margin-bottom: 16px;
  }
  .filter-card {
    margin-bottom: 16px;
  }
  .filter-bar {
    display: flex;
    gap: 12px;
    align-items: center;
  }
  .form-tip {
    font-size: 12px;
    color: #909399;
    margin-top: 4px;
  }
  .response-preview {
    background: #f5f7fa;
    padding: 8px;
    border-radius: 4px;
    font-size: 12px;
    max-height: 200px;
    overflow: auto;
    white-space: pre-wrap;
    word-break: break-all;
  }
}
</style>
