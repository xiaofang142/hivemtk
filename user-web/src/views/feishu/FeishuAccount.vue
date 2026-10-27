<template>
  <div class="feishu-account-management">
    <div class="account-tabs">
      <div class="account-list-container">
        <div class="account-search">
          <el-input
            v-model="searchKeyword"
            :placeholder="$t('输入账号名称搜索')"
            prefix-icon="Search"
            clearable
            @clear="handleSearch"
            @input="handleSearch"
          />
          <div class="action-buttons">
            <el-button
              type="primary"
              @click="handleAdd"
            >
              {{ $t('添加飞书账号') }}
            </el-button>
            <el-button
              type="info"
              :loading="loading"
              @click="fetchAccounts"
            >
              <el-icon><Refresh /></el-icon>
              <span>{{ $t('刷新') }}</span>
            </el-button>
          </div>
        </div>

        <div
          v-loading="loading"
          class="account-list"
        >
          <el-table
            :data="filteredAccounts"
            style="width: 100%"
            border
          >
            <el-table-column
              prop="account_name"
              :label="$t('账号名称')"
              min-width="160"
            />
            <el-table-column
              prop="app_id"
              label="App ID"
              min-width="200"
            />
            <el-table-column
              :label="$t('状态')"
              width="100"
              align="center"
            >
              <template #default="scope">
                <el-tag
                  :type="scope.row.status === 1 ? 'success' : 'danger'"
                  effect="plain"
                >
                  {{ scope.row.status === 1 ? '正常' : '停用' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              label="Webhook"
              width="110"
              align="center"
            >
              <template #default="scope">
                <el-tag
                  :type="scope.row.webhook_enabled ? 'success' : 'info'"
                  effect="plain"
                >
                  {{ scope.row.webhook_enabled ? '已启用' : '未启用' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              label="智能体"
              width="110"
              align="center"
            >
              <template #default="scope">
                <el-tag
                  :type="scope.row.ai_agent_enabled ? 'success' : 'info'"
                  effect="plain"
                >
                  {{ scope.row.ai_agent_enabled ? '已启用' : '未启用' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              label="Token"
              width="110"
              align="center"
            >
              <template #default="scope">
                <el-tooltip
                  :content="scope.row.token_cached ? '已缓存 tenant_access_token' : '尚未缓存'"
                  placement="top"
                >
                  <el-tag
                    :type="scope.row.token_cached ? 'success' : 'warning'"
                    effect="plain"
                  >
                    {{ scope.row.token_cached ? '已缓存' : '未缓存' }}
                  </el-tag>
                </el-tooltip>
              </template>
            </el-table-column>
            <el-table-column
              prop="last_error_msg"
              label="最近错误"
              min-width="180"
              show-overflow-tooltip
            />
            <el-table-column
              label="操作"
              width="420"
              fixed="right"
            >
              <template #default="scope">
                <el-button
                  size="small"
                  @click="handleEdit(scope.row)"
                >
                  编辑
                </el-button>
                <el-button
                  size="small"
                  type="success"
                  @click="handleTestSend(scope.row)"
                >
                  测试发送
                </el-button>
                <el-button
                  size="small"
                  type="warning"
                  @click="handleRefreshToken(scope.row)"
                >
                  刷新Token
                </el-button>
                <el-button
                  size="small"
                  type="primary"
                  @click="openBindingDialog(scope.row)"
                >
                  绑定AI
                </el-button>
                <el-button
                  size="small"
                  type="danger"
                  @click="handleDelete(scope.row)"
                >
                  删除
                </el-button>
              </template>
            </el-table-column>
          </el-table>
        </div>

        <div
          v-if="!loading && filteredAccounts.length === 0"
          class="empty-data"
        >
          <el-empty description="暂无飞书账号，点击右上角添加" />
        </div>
      </div>
    </div>

    
    <el-dialog
      v-model="dialogVisible"
      :title="dialogType === 'add' ? '添加飞书账号' : '编辑飞书账号'"
      width="640px"
      :close-on-click-modal="false"
    >
      <el-form
        ref="accountFormRef"
        :model="accountForm"
        :rules="rules"
        label-width="140px"
      >
        <el-form-item
          label="账号名称"
          prop="account_name"
        >
          <el-input
            v-model="accountForm.account_name"
            placeholder="用于识别，例如：飞书客服Bot"
          />
        </el-form-item>
        <el-form-item
          label="App ID"
          prop="app_id"
        >
          <el-input
            v-model="accountForm.app_id"
            placeholder="飞书开放平台 App ID"
          />
        </el-form-item>
        <el-form-item
          label="App Secret"
          prop="app_secret"
        >
          <el-input
            v-model="accountForm.app_secret"
            :placeholder="dialogType === 'edit' ? '留空则保持原 Secret 不变' : '请输入飞书 App Secret'"
            type="password"
            show-password
          />
        </el-form-item>
        <el-form-item
          label="Verification Token"
          prop="verification_token"
        >
          <el-input
            v-model="accountForm.verification_token"
            placeholder="事件订阅的 Verification Token（用于验证回调）"
          />
        </el-form-item>
        <el-form-item
          label="Encrypt Key"
          prop="encrypt_key"
        >
          <el-input
            v-model="accountForm.encrypt_key"
            placeholder="事件订阅的 Encrypt Key（用于加密事件）"
            type="password"
            show-password
          />
        </el-form-item>
        <el-form-item
          label="启用 Webhook"
          prop="webhook_enabled"
        >
          <el-switch v-model="accountForm.webhook_enabled" />
          <span class="form-hint">开启后接受飞书事件推送（URL 验证 + 消息事件）</span>
        </el-form-item>
        <el-form-item
          label="启用 智能体"
          prop="ai_agent_enabled"
        >
          <el-switch v-model="accountForm.ai_agent_enabled" />
          <span class="form-hint">开启后，飞书入站消息自动触发 智能体流程</span>
        </el-form-item>
        <el-form-item
          label="状态"
          prop="status"
        >
          <el-radio-group v-model="accountForm.status">
            <el-radio :label="1">
              正常
            </el-radio>
            <el-radio :label="0">
              停用
            </el-radio>
          </el-radio-group>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          :loading="submitting"
          @click="handleSubmit"
        >
          确定
        </el-button>
      </template>
    </el-dialog>

    
    <el-dialog
      v-model="testSendVisible"
      title="测试发送消息"
      width="500px"
    >
      <el-form
        :model="testSendForm"
        label-width="100px"
      >
        <el-form-item label="接收者">
          <el-input
            v-model="testSendForm.open_id"
            placeholder="请输入 open_id（ou_xxx）或 chat_id（oc_xxx）"
          />
        </el-form-item>
        <el-form-item label="消息内容">
          <el-input
            v-model="testSendForm.content"
            type="textarea"
            :rows="4"
            placeholder="请输入测试消息"
          />
        </el-form-item>
        <el-form-item label="消息类型">
          <el-select
            v-model="testSendForm.msg_type"
            placeholder="默认 text"
          >
            <el-option
              label="text"
              value="text"
            />
            <el-option
              label="post"
              value="post"
            />
            <el-option
              label="interactive"
              value="interactive"
            />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="testSendVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          :loading="testSending"
          @click="submitTestSend"
        >
          发送
        </el-button>
      </template>
    </el-dialog>

    
    <el-dialog
      v-model="bindingVisible"
      title="绑定 智能体"
      width="500px"
    >
      <el-form
        :model="bindingForm"
        label-width="100px"
      >
        <el-form-item label="账号">
          <el-input
            :value="bindingForm.account_name"
            disabled
          />
        </el-form-item>
        <el-form-item label="智能体">
          <el-switch v-model="bindingForm.ai_agent_enabled" />
        </el-form-item>
        <el-form-item label="Webhook">
          <el-switch v-model="bindingForm.webhook_enabled" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="bindingVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          :loading="bindingSubmitting"
          @click="submitBinding"
        >
          保存
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import { toList } from '@/utils/list'
import {
  listAccounts,
  createAccount,
  updateAccount,
  deleteAccount,
  testSend,
  refreshToken
} from '@/api/feishu'

const loading = ref(false);
const submitting = ref(false)
const testSending = ref(false)
const bindingSubmitting = ref(false)
const accounts = ref([])
const searchKeyword = ref('')

const dialogVisible = ref(false);
const dialogType = ref('add');
const accountFormRef = ref(null)
const accountForm = ref({
  account_name: '',
  app_id: '',
  app_secret: '',
  verification_token: '',
  encrypt_key: '',
  webhook_enabled: false,
  ai_agent_enabled: false,
  status: 1
})

const testSendVisible = ref(false);
const testSendForm = ref({
  account_id: null,
  open_id: '',
  content: '',
  msg_type: 'text'
})

const bindingVisible = ref(false);
const bindingForm = ref({
  account_id: null,
  account_name: '',
  ai_agent_enabled: false,
  webhook_enabled: false
})

const rules = {
  account_name: [{ required: true, message: i18n.global.t('请输入账号名称'), trigger: 'blur' }],
  app_id: [{ required: true, message: i18n.global.t('请输入 App ID'), trigger: 'blur' }],
  app_secret: [{ required: true, message: i18n.global.t('请输入 App Secret'), trigger: 'blur' }]
};

const filteredAccounts = computed(() => {
  if (!searchKeyword.value) return accounts.value
  return accounts.value.filter(a =>
    a.account_name?.toLowerCase().includes(searchKeyword.value.toLowerCase())
  )
});

const fetchAccounts = async () => {
  loading.value = true
  try {
    const res = await listAccounts()
    accounts.value = toList(res)
  } catch (e) {
    ElMessage.error('网络错误：' + e.message)
  } finally {
    loading.value = false
  }
};

const handleSearch = () => {}

const resetForm = () => {
  accountForm.value = {
    account_name: '',
    app_id: '',
    app_secret: '',
    verification_token: '',
    encrypt_key: '',
    webhook_enabled: false,
    ai_agent_enabled: false,
    status: 1
  }
}

const handleAdd = () => {
  dialogType.value = 'add'
  resetForm()
  dialogVisible.value = true
}

const handleEdit = (row) => {
  dialogType.value = 'edit'
  accountForm.value = {
    id: row.id,
    account_name: row.account_name,
    app_id: row.app_id,
    app_secret: '',
    verification_token: row.verification_token || '',
    encrypt_key: '',
    webhook_enabled: row.webhook_enabled || false,
    ai_agent_enabled: row.ai_agent_enabled || false,
    status: row.status
  }
  dialogVisible.value = true
}

const handleSubmit = async () => {
  if (!accountFormRef.value) return
  await accountFormRef.value.validate(async (valid) => {
    if (!valid) return
    submitting.value = true
    try {
      const isEdit = dialogType.value === 'edit'
      await (isEdit
        ? updateAccount(accountForm.value.id, accountForm.value)
        : createAccount(accountForm.value))
      ElMessage.success(isEdit ? '更新成功' : '创建成功')
      dialogVisible.value = false
      await fetchAccounts()
    } catch (e) {
      ElMessage.error('网络错误：' + e.message)
    } finally {
      submitting.value = false
    }
  })
}

const handleDelete = async (row) => {
  try {
    await ElMessageBox.confirm(
      `确定要删除账号 "${row.account_name}" 吗？`,
      '确认删除',
      { type: 'warning' }
    )
    await deleteAccount(row.id)
    ElMessage.success(i18n.global.t('删除成功'))
    await fetchAccounts()
  } catch (e) {
    if (e !== 'cancel') {
      ElMessage.error('操作失败：' + e.message)
    }
  }
}

const handleTestSend = (row) => {
  testSendForm.value = {
    account_id: row.id,
    open_id: '',
    content: i18n.global.t('这是一条来自 hivemtk 的测试消息，飞书账号：') + row.account_name,
    msg_type: 'text'
  }
  testSendVisible.value = true
}

const submitTestSend = async () => {
  if (!testSendForm.value.open_id) {
    ElMessage.warning(i18n.global.t('请输入接收者 open_id 或 chat_id'))
    return
  }
  if (!testSendForm.value.content) {
    ElMessage.warning(i18n.global.t('请输入消息内容'))
    return
  }
  testSending.value = true
  try {
    await testSend(testSendForm.value.account_id, {
      open_id: testSendForm.value.open_id,
      content: testSendForm.value.content,
      msg_type: testSendForm.value.msg_type
    })
    ElMessage.success(i18n.global.t('测试消息已发送'))
    testSendVisible.value = false
  } catch (e) {
    ElMessage.error('网络错误：' + e.message)
  } finally {
    testSending.value = false
  }
}

const handleRefreshToken = async (row) => {
  try {
    await refreshToken(row.id)
    ElMessage.success(i18n.global.t('Token 刷新成功'))
    await fetchAccounts()
  } catch (e) {
    // 失败提示由 request 拦截器统一弹出（502/400 展示服务端原因，网络错误展示超时）。
    // 这里再弹一条「网络错误：…」只会和拦截器那条叠成两条同义报错。
  }
}

const openBindingDialog = (row) => {
  bindingForm.value = {
    account_id: row.id,
    account_name: row.account_name,
    ai_agent_enabled: row.ai_agent_enabled || false,
    webhook_enabled: row.webhook_enabled || false
  }
  bindingVisible.value = true
}

const submitBinding = async () => {
  bindingSubmitting.value = true
  try {
    await updateAccount(bindingForm.value.account_id, {
      ai_agent_enabled: bindingForm.value.ai_agent_enabled,
      webhook_enabled: bindingForm.value.webhook_enabled
    })
    ElMessage.success(i18n.global.t('保存成功'))
    bindingVisible.value = false
    await fetchAccounts()
  } catch (e) {
    ElMessage.error('网络错误：' + e.message)
  } finally {
    bindingSubmitting.value = false
  }
}

onMounted(() => {
  fetchAccounts()
});
</script>

<style scoped>
.feishu-account-management {
  padding: 16px;
}
.account-search {
  display: flex;
  gap: 12px;
  margin-bottom: 16px;
  align-items: center;
}
.action-buttons {
  display: flex;
  gap: 8px;
}
.empty-data {
  padding: 40px 0;
  text-align: center;
}
.form-hint {
  margin-left: 12px;
  color: #909399;
  font-size: 12px;
}
</style>
