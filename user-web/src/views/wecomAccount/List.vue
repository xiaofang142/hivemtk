<template>
  <div class="wecom-account-list">
    <el-row
      v-loading="summaryLoading"
      :gutter="16"
      class="summary-row"
    >
      <el-col :span="6">
        <el-card shadow="hover">
          <div class="summary-card">
            <div class="summary-label">
              {{ $t('账号总数') }}
            </div>
            <div class="summary-value">
              {{ summary?.total_accounts ?? 0 }}
            </div>
            <div class="summary-sub">
              在线 {{ summary?.online_count ?? 0 }} / 离线 {{ summary?.offline_count ?? 0 }}
            </div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="hover">
          <div class="summary-card">
            <div class="summary-label">
              {{ $t('平均健康分') }}
            </div>
            <div
              class="summary-value"
              :class="healthScoreClass"
            >
              {{ summary?.avg_score?.toFixed(1) ?? '-' }}
            </div>
            <div class="summary-sub">
              满分 100
            </div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="hover">
          <div class="summary-card">
            <div class="summary-label">
              {{ $t('配额使用') }}
            </div>
            <div class="summary-value">
              {{ summary?.total_used ?? 0 }} / {{ summary?.total_quota ?? 0 }}
            </div>
            <div class="summary-sub">
              {{ $t('日配额') }}
            </div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="hover">
          <div class="summary-card">
            <div class="summary-label">
              {{ $t('风险账号') }}
            </div>
            <div
              class="summary-value"
              :class="{ 'text-danger': (summary?.risk_accounts?.length ?? 0) > 0 }"
            >
              {{ summary?.risk_accounts?.length ?? 0 }}
            </div>
            <div class="summary-sub">
              {{ $t('需关注') }}
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    
    <el-card
      shadow="never"
      class="table-card"
    >
      <template #header>
        <div class="card-header">
          <span>{{ $t('企微账号管理') }}</span>
          <div>
            <el-button
              type="primary"
              :icon="Plus"
              @click="openCreateDialog"
            >
              {{ $t('新增账号') }}
            </el-button>
            <el-button
              type="primary"
              :icon="Refresh"
              :loading="listLoading"
              @click="loadData"
            >
              {{ $t('刷新') }}
            </el-button>
          </div>
        </div>
      </template>

      <el-table
        v-loading="listLoading"
        :data="accountList"
        border
        style="width: 100%"
      >
        <el-table-column
          prop="account.corp_id"
          label="企业ID"
          min-width="160"
        />
        <el-table-column
          prop="account.agent_id"
          label="应用ID"
          width="100"
        />
        <el-table-column
          label="登录状态"
          width="110"
        >
          <template #default="{ row }">
            <el-tag
              :type="loginStateTag(row.account.login_state)"
              size="small"
            >
              {{ loginStateText(row.account.login_state) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="风险等级"
          width="110"
        >
          <template #default="{ row }">
            <el-tag
              :type="riskLevelTag(row.account.risk_level)"
              size="small"
              effect="dark"
            >
              {{ riskLevelText(row.account.risk_level) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="健康分"
          width="100"
        >
          <template #default="{ row }">
            <span :class="healthScoreTextClass(row.health?.health_score)">
              {{ row.health?.health_score?.toFixed(0) ?? '-' }}
            </span>
          </template>
        </el-table-column>
        <el-table-column
          label="好友/群组"
          width="120"
        >
          <template #default="{ row }">
            {{ row.account.friend_count ?? 0 }} / {{ row.account.group_count ?? 0 }}
          </template>
        </el-table-column>
        <el-table-column
          label="日配额"
          width="130"
        >
          <template #default="{ row }">
            {{ row.account.daily_msg_used ?? 0 }} / {{ row.account.daily_msg_quota ?? 0 }}
          </template>
        </el-table-column>
        <el-table-column
          label="最后活跃"
          min-width="160"
        >
          <template #default="{ row }">
            {{ row.account.last_active_at ? formatTime(row.account.last_active_at) : '从未' }}
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="300"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button
              size="small"
              @click="viewDetail(row)"
            >
              详情
            </el-button>
            <el-button
              size="small"
              type="primary"
              @click="openEditDialog(row.account)"
            >
              编辑
            </el-button>
            <el-button
              size="small"
              type="danger"
              @click="handleDelete(row.account)"
            >
              删除
            </el-button>
            <el-dropdown
              trigger="click"
              :disabled="!!actionLoading"
              @command="(cmd) => handleMoreCommand(cmd, row)"
            >
              <el-button
                size="small"
                type="warning"
              >
                更多<el-icon class="el-icon--right">
                  <ArrowDown />
                </el-icon>
              </el-button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item
                    command="data"
                    :icon="DataLine"
                  >
                    数据看板
                  </el-dropdown-item>
                  <el-dropdown-item
                    command="bind"
                    :icon="Link"
                  >
                    绑定AI
                  </el-dropdown-item>
                  <el-dropdown-item
                    command="syncCustomers"
                    :icon="Refresh"
                  >
                    同步客户
                  </el-dropdown-item>
                  <el-dropdown-item
                    command="syncGroups"
                    :icon="Refresh"
                  >
                    同步群组
                  </el-dropdown-item>
                  <el-dropdown-item
                    command="syncTags"
                    :icon="Refresh"
                  >
                    同步标签
                  </el-dropdown-item>
                  <el-dropdown-item
                    command="send"
                    :icon="Promotion"
                  >
                    测试发送
                  </el-dropdown-item>
                  <el-dropdown-item
                    command="refresh"
                    :icon="RefreshRight"
                    divided
                  >
                    刷新登录状态
                  </el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    
    <AgentBindingDialog
      v-model="bindingDialogVisible"
      channel-type="wecom"
      :account-id="bindingAccountId"
      :account-label="bindingAccountLabel"
      :account-enabled="bindingAccountEnabled"
    />

    
    <WeComSendDialog
      v-model:visible="sendDialogVisible"
      :account-id="sendAccountId"
    />

    
    <el-dialog
      v-model="createDialogVisible"
      :title="dialogTitle"
      width="520px"
      @closed="resetCreateForm"
    >
      <el-form
        ref="createFormRef"
        :model="createForm"
        :rules="createRules"
        label-width="110px"
      >
        <el-form-item
          label="CorpID"
          prop="corp_id"
        >
          <el-input
            v-model="createForm.corp_id"
            placeholder="企业微信 CorpID"
          />
        </el-form-item>
        <el-form-item
          label="CorpSecret"
          prop="corp_secret"
        >
          <el-input
            v-model="createForm.corp_secret"
            type="password"
            show-password
            placeholder="企业微信 CorpSecret"
          />
        </el-form-item>
        <el-form-item
          label="AgentID"
          prop="agent_id"
        >
          <el-input
            v-model="createForm.agent_id"
            placeholder="应用 AgentID（可选）"
          />
        </el-form-item>
        <el-form-item
          label="AgentSecret"
          prop="agent_secret"
        >
          <el-input
            v-model="createForm.agent_secret"
            type="password"
            show-password
            placeholder="应用 Secret（可选）"
          />
        </el-form-item>

        <el-divider content-position="left">
          入站回调（接收客户消息 → 智能体自动回复）
        </el-divider>
        <el-form-item
          v-if="dialogMode === 'edit'"
          label="回调 URL"
        >
          <el-input
            :model-value="wecomCallbackUrl"
            readonly
          >
            <template #append>
              <el-button @click="copyCallbackUrl">
                复制
              </el-button>
            </template>
          </el-input>
          <div class="form-hint">
            将此 URL 配置到企业微信管理后台「客户联系 → 接收事件服务器」
          </div>
        </el-form-item>
        <el-form-item label="Token">
          <el-input
            v-model="createForm.callback_token"
            placeholder="回调 Token（与企微管理端保持一致）"
          />
        </el-form-item>
        <el-form-item label="EncodingAESKey">
          <el-input
            v-model="createForm.encoding_aes_key"
            placeholder="43 位 EncodingAESKey（用于消息体加解密）"
          />
        </el-form-item>
        <el-form-item label="启用接收">
          <el-switch v-model="createForm.webhook_enabled" />
          <span class="form-hint-inline">开启后接收客户消息回调</span>
        </el-form-item>
        <el-form-item label="智能体回复">
          <el-switch v-model="createForm.ai_agent_enabled" />
          <span class="form-hint-inline">开启后由已绑定智能体自动回复客户消息</span>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createDialogVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          :loading="createLoading"
          @click="submitForm"
        >
          确定
        </el-button>
      </template>
    </el-dialog>

    
    <el-dialog
      v-model="detailDialogVisible"
      title="账号详情"
      width="640px"
    >
      <el-descriptions
        v-if="detailAccount"
        :column="2"
        border
        size="small"
      >
        <el-descriptions-item label="账号ID">
          {{ detailAccount.id }}
        </el-descriptions-item>
        <el-descriptions-item label="企业ID">
          {{ detailAccount.corp_id }}
        </el-descriptions-item>
        <el-descriptions-item label="应用ID">
          {{ detailAccount.agent_id || '-' }}
        </el-descriptions-item>
        <el-descriptions-item label="登录状态">
          <el-tag
            :type="loginStateTag(detailAccount.login_state)"
            size="small"
          >
            {{ loginStateText(detailAccount.login_state) }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="风险等级">
          <el-tag
            :type="riskLevelTag(detailAccount.risk_level)"
            size="small"
            effect="dark"
          >
            {{ riskLevelText(detailAccount.risk_level) }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="健康分">
          {{ detailHealth?.health_score?.toFixed(0) ?? '-' }}
        </el-descriptions-item>
        <el-descriptions-item label="好友数">
          {{ detailAccount.friend_count ?? 0 }}
        </el-descriptions-item>
        <el-descriptions-item label="群组数">
          {{ detailAccount.group_count ?? 0 }}
        </el-descriptions-item>
        <el-descriptions-item label="日配额">
          {{ detailAccount.daily_msg_used ?? 0 }} / {{ detailAccount.daily_msg_quota ?? 0 }}
        </el-descriptions-item>
        <el-descriptions-item label="累计发送">
          {{ detailAccount.total_sent ?? 0 }}
        </el-descriptions-item>
        <el-descriptions-item label="累计接收">
          {{ detailAccount.total_received ?? 0 }}
        </el-descriptions-item>
        <el-descriptions-item label="最后活跃">
          {{ detailAccount.last_active_at ? formatTime(detailAccount.last_active_at) : '从未' }}
        </el-descriptions-item>
        <el-descriptions-item label="创建时间">
          {{ detailAccount.created_at ? formatTime(detailAccount.created_at) : '-' }}
        </el-descriptions-item>
        <el-descriptions-item
          v-if="detailHealth"
          label="错误信息"
          :span="2"
        >
          {{ detailHealth.error_message || '无' }}
        </el-descriptions-item>
        <el-descriptions-item
          v-if="detailHealth"
          label="最近上报"
          :span="2"
        >
          {{ detailHealth.reported_at ? formatTime(detailHealth.reported_at) : '暂无' }}
        </el-descriptions-item>
      </el-descriptions>
      <template #footer>
        <el-button @click="detailDialogVisible = false">
          关闭
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import _i18n from '@/i18n'

import { ref, reactive, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh, Plus, ArrowDown, Link, Promotion, RefreshRight, DataLine } from '@element-plus/icons-vue'
import { wecomAccountApi } from '@/api/wecomAccount.js'
import { copyToClipboard } from '@/utils/clipboard'
import AgentBindingDialog from '@/components/AgentBindingDialog.vue'
import WeComSendDialog from '@/components/WeComSendDialog.vue'
import { getAccountStatusLabel as _getAccountStatusLabel, getAccountStatusTagType as _getAccountStatusTagType } from '@/constants/accountType';

const router = useRouter()

const listLoading = ref(false)
const summaryLoading = ref(false)
const accountList = ref([])
const summary = ref(null)

const createDialogVisible = ref(false);
const createLoading = ref(false)
const createFormRef = ref(null)
const dialogMode = ref('create');
const editingId = ref(null)
const createForm = reactive({
  corp_id: '',
  corp_secret: '',
  agent_id: '',
  agent_secret: '',
  callback_token: '',
  encoding_aes_key: '',
  webhook_enabled: false,
  ai_agent_enabled: false
})
const createRules = {
  corp_id: [{ required: true, message: '请输入 CorpID', trigger: 'blur' }],
  corp_secret: [{ required: true, message: '请输入 CorpSecret', trigger: 'blur' }]
}
const dialogTitle = computed(() => (dialogMode.value === 'edit' ? '编辑企微账号' : '新增企微账号'))
const wecomCallbackUrl = computed(() => {
  if (dialogMode.value !== 'edit' || editingId.value == null) return ''
  return `${window.location.origin}/api/webhook/wecom/${editingId.value}`
})
const copyCallbackUrl = async () => {
  if (await copyToClipboard(wecomCallbackUrl.value)) {
    ElMessage.success('回调 URL 已复制')
  } else {
    ElMessage.error('复制失败，请手动复制')
  }
}
const openCreateDialog = () => {
  dialogMode.value = 'create'
  editingId.value = null
  resetCreateForm()
  createDialogVisible.value = true
}
const openEditDialog = (account) => {
  dialogMode.value = 'edit'
  editingId.value = account.id
  createForm.corp_id = account.corp_id || ''
  createForm.corp_secret = account.corp_secret || ''
  createForm.agent_id = account.agent_id ? String(account.agent_id) : ''
  createForm.agent_secret = account.agent_secret || ''
  createForm.callback_token = account.callback_token || ''
  createForm.encoding_aes_key = account.encoding_aes_key || ''
  createForm.webhook_enabled = !!account.webhook_enabled
  createForm.ai_agent_enabled = !!account.ai_agent_enabled
  createDialogVisible.value = true
}
const resetCreateForm = () => {
  createForm.corp_id = ''
  createForm.corp_secret = ''
  createForm.agent_id = ''
  createForm.agent_secret = ''
  createForm.callback_token = ''
  createForm.encoding_aes_key = ''
  createForm.webhook_enabled = false
  createForm.ai_agent_enabled = false
  createFormRef.value?.clearValidate?.()
}
const submitForm = async () => {
  if (!createFormRef.value) return
  try {
    await createFormRef.value.validate()
  } catch {
    return
  }
  createLoading.value = true
  try {
    const payload = {
      corp_id: createForm.corp_id,
      corp_secret: createForm.corp_secret,
      agent_id: createForm.agent_id ? Number(createForm.agent_id) : undefined,
      agent_secret: createForm.agent_secret || undefined,
      callback_token: createForm.callback_token || undefined,
      encoding_aes_key: createForm.encoding_aes_key || undefined,
      webhook_enabled: createForm.webhook_enabled,
      ai_agent_enabled: createForm.ai_agent_enabled
    }
    if (dialogMode.value === 'edit' && editingId.value != null) {
      await wecomAccountApi.updateAccount(editingId.value, payload)
      ElMessage.success('账号更新成功')
    } else {
      await wecomAccountApi.createAccount(payload)
      ElMessage.success('账号创建成功')
    }
    createDialogVisible.value = false
    loadData()
  } catch (err) {
    ElMessage.error('保存失败: ' + (err.message || '未知错误'))
  } finally {
    createLoading.value = false
  }
}

const detailDialogVisible = ref(false);
const detailAccount = ref(null)
const detailHealth = ref(null)
const viewDetail = (row) => {
  detailAccount.value = row.account
  detailHealth.value = row.health
  detailDialogVisible.value = true
}

const handleDelete = async (account) => {
  try {
    await ElMessageBox.confirm(`确认删除企业微信账号「${account.corp_id}」？该操作不可恢复。`, '删除确认', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消'
    })
  } catch {
    return
  }
  try {
    await wecomAccountApi.deleteAccount(account.id)
    ElMessage.success('删除成功')
    loadData()
  } catch (err) {
    ElMessage.error('删除失败: ' + (err.message || '未知错误'))
  }
};

const healthScoreClass = computed(() => {
  const score = summary.value?.avg_score ?? 0
  if (score >= 80) return 'text-success'
  if (score >= 60) return 'text-warning'
  return 'text-danger'
})

const loginStateText = (state) => {
  const map = { online: '在线', offline: '离线', banned: '封禁' }
  return map[state] || state
}

const loginStateTag = (state) => {
  const map = { online: 'success', offline: 'info', banned: 'danger' }
  return map[state] || 'info'
}

const riskLevelText = (level) => {
  const map = { normal: '正常', warning: '警告', critical: '危险', banned: '封禁' }
  return map[level] || level
}

const riskLevelTag = (level) => {
  const map = { normal: 'success', warning: 'warning', critical: 'danger', banned: 'danger' }
  return map[level] || 'info'
}

const healthScoreTextClass = (score) => {
  if (score == null) return ''
  if (score >= 80) return 'text-success'
  if (score >= 60) return 'text-warning'
  return 'text-danger'
}

const formatTime = (iso) => {
  if (!iso) return '-'
  return new Date(iso).toLocaleString('zh-CN')
}

const loadAccountList = async () => {
  listLoading.value = true
  try {
    const data = await wecomAccountApi.listAccounts()
    accountList.value = Array.isArray(data) ? data : []
  } catch (err) {
    ElMessage.error('加载账号列表失败: ' + (err.message || '未知错误'))
    accountList.value = []
  } finally {
    listLoading.value = false
  }
}

const loadSummary = async () => {
  summaryLoading.value = true
  try {
    const data = await wecomAccountApi.getHealthSummary()
    summary.value = data
  } catch (err) {
    ElMessage.error('加载健康度概览失败: ' + (err.message || '未知错误'))
    summary.value = null
  } finally {
    summaryLoading.value = false
  }
}

const loadData = () => {
  loadAccountList()
  loadSummary()
}

onMounted(() => {
  loadData()
})

const bindingDialogVisible = ref(false);
const bindingAccountId = ref('')
const bindingAccountLabel = ref('')
const bindingAccountEnabled = ref(true)

const openBindingDialog = (account) => {
  bindingAccountId.value = String(account.id)
  bindingAccountLabel.value = account.corp_id || ''
  bindingAccountEnabled.value = account.status === 1
  bindingDialogVisible.value = true
}

const actionLoading = ref('');

const sendDialogVisible = ref(false);
const sendAccountId = ref(null)

const openSendDialog = (account) => {
  sendAccountId.value = account.id
  sendDialogVisible.value = true
}

const syncAction = async (account, type) => {
  const apiMap = {
    syncCustomers: wecomAccountApi.syncCustomers,
    syncGroups: wecomAccountApi.syncGroups,
    syncTags: wecomAccountApi.syncTags
  }
  const api = apiMap[type]
  if (!api) return
  actionLoading.value = type
  try {
    const res = await api(account.id)
    const count = res && res.count != null ? res.count : 0
    ElMessage.success('同步完成' + (count ? `，共 ${count} 条` : ''))
  } catch (e) {
    ElMessage.error('同步失败：' + (e.message || e))
  } finally {
    actionLoading.value = ''
  }
};

const refreshAccount = async (account) => {
  actionLoading.value = 'refresh'
  try {
    await wecomAccountApi.refreshAccount(account.id)
    ElMessage.success('登录状态已刷新')
    loadData()
  } catch (e) {
    ElMessage.error('刷新失败：' + (e.message || e))
  } finally {
    actionLoading.value = ''
  }
};

const handleMoreCommand = (cmd, row) => {
  const account = row.account
  switch (cmd) {
    case 'bind':
      openBindingDialog(account)
      break
    case 'syncCustomers':
      syncAction(account, 'syncCustomers')
      break
    case 'syncGroups':
      syncAction(account, 'syncGroups')
      break
    case 'syncTags':
      syncAction(account, 'syncTags')
      break
    case 'send':
      openSendDialog(account)
      break
    case 'data':
      router.push({ name: 'WecomAccountData' })
      break
    case 'refresh':
      refreshAccount(account)
      break
  }
};
</script>

<style scoped>
.wecom-account-list {
  padding: 4px;
}

.summary-row {
  margin-bottom: 16px;
}

.summary-card {
  text-align: center;
  padding: 8px 0;
}

.summary-label {
  font-size: 13px;
  color: #909399;
  margin-bottom: 8px;
}

.summary-value {
  font-size: 28px;
  font-weight: bold;
  color: #303133;
  line-height: 1.2;
}

.summary-sub {
  font-size: 12px;
  color: #c0c4cc;
  margin-top: 6px;
}

.table-card {
  margin-top: 4px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.form-hint {
  font-size: 12px;
  color: #909399;
  line-height: 1.5;
  margin-top: 4px;
}

.form-hint-inline {
  font-size: 12px;
  color: #909399;
  margin-left: 10px;
}

.text-success {
  color: #10B981;
}

.text-warning {
  color: #F59E0B;
}

.text-danger {
  color: #EF4444;
}
</style>
