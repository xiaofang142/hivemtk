<template>
  <div class="qq-bot-management">
    <div class="account-tabs">
      <div class="account-list-container">
        <div class="qq-webhook-hint">
          <el-alert
            type="info"
            :closable="false"
            show-icon
            title="QQ 机器人接入说明"
            description="在 QQ 开放平台（q.qq.com）创建机器人后，将 AppID / AppSecret / BotSecret 填入下方表单，并在开放平台管理端把回调地址配置为：{你的域名}/api/webhook/qq/{账号ID}。群聊场景用户 @机器人 即可触发智能体回复（被动回复 5 分钟内限 5 条）。"
          />
        </div>
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
            <el-button type="primary" @click="handleAdd">{{ $t('添加机器人') }}</el-button>
            <el-button type="info" :loading="loading" @click="fetchAccounts">
              <el-icon><Refresh /></el-icon>
              <span>{{ $t('刷新') }}</span>
            </el-button>
          </div>
        </div>

        <div v-loading="loading" class="account-list">
          <el-table :data="filteredAccounts" style="width: 100%" border>
            <el-table-column prop="account_name" :label="$t('账号名称')" min-width="150" />
            <el-table-column prop="app_id" label="AppID" min-width="140" />
            <el-table-column prop="app_secret_masked" label="AppSecret" min-width="150" />
            <el-table-column prop="webhook_url" label="Webhook URL" min-width="230" show-overflow-tooltip />
            <el-table-column label="Webhook" width="100" align="center">
              <template #default="scope">
                <el-tag :type="scope.row.webhook_enabled ? 'success' : 'info'" effect="plain">
                  {{ scope.row.webhook_enabled ? '已配置' : '未配置' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="智能体" width="100" align="center">
              <template #default="scope">
                <el-tag :type="scope.row.ai_agent_enabled ? 'success' : 'info'" effect="plain">
                  {{ scope.row.ai_agent_enabled ? '已启用' : '未启用' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="状态" width="90" align="center">
              <template #default="scope">
                <el-tag :type="scope.row.status === 1 ? 'success' : 'danger'" effect="plain">
                  {{ scope.row.status === 1 ? '正常' : '停用' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="last_error_msg" label="最近错误" min-width="160" show-overflow-tooltip />
            <el-table-column label="操作" width="380" fixed="right">
              <template #default="scope">
                <el-button size="small" @click="handleEdit(scope.row)">编辑</el-button>
                <el-button size="small" type="warning" @click="handleTestSend(scope.row)">测试发送</el-button>
                <el-button size="small" type="success" :loading="verifyingId === scope.row.id" @click="handleVerifyCallback(scope.row)">自检验签</el-button>
                <el-button size="small" type="primary" @click="openBindingDialog(scope.row)">绑定AI</el-button>
                <el-button size="small" type="danger" @click="handleDelete(scope.row)">删除</el-button>
              </template>
            </el-table-column>
          </el-table>
        </div>

        <div v-if="!loading && filteredAccounts.length === 0" class="empty-data">
          <el-empty description="暂无 QQ 机器人账号，点击右上角添加" />
        </div>
      </div>
    </div>

    <el-dialog
      v-model="dialogVisible"
      :title="dialogType === 'add' ? '添加 QQ 机器人' : '编辑 QQ 机器人'"
      width="640px"
      :close-on-click-modal="false"
    >
      <el-form ref="accountFormRef" :model="accountForm" :rules="rules" label-width="130px">
        <el-form-item label="账号名称" prop="account_name">
          <el-input v-model="accountForm.account_name" placeholder="用于识别，例如：售前咨询Bot" />
        </el-form-item>
        <el-form-item label="AppID" prop="app_id">
          <el-input v-model="accountForm.app_id" placeholder="QQ 开放平台 AppID" />
        </el-form-item>
        <el-form-item label="AppSecret" prop="app_secret">
          <el-input
            v-model="accountForm.app_secret"
            :placeholder="dialogType === 'edit' ? '留空则保持原 Secret 不变' : 'QQ 开放平台 AppSecret'"
            type="password"
            show-password
          />
        </el-form-item>
        <el-form-item label="BotSecret" prop="webhook_secret">
          <el-input
            v-model="accountForm.webhook_secret"
            :placeholder="dialogType === 'edit' ? '留空则保持原值不变' : '开放平台管理端 BotSecret，用于 Ed25519 验签'"
            type="password"
            show-password
          />
        </el-form-item>
        <el-form-item label="Webhook URL" prop="webhook_url">
          <el-input
            v-model="accountForm.webhook_url"
            :placeholder="accountForm.webhook_url_suggested ? '' : '未配置 PUBLIC_BASE_URL，请手填（https，端口限 80/443/8080/8443）'"
            :disabled="webhookUrlLocked"
          >
            <template v-if="accountForm.webhook_url" #append>
              <el-button @click="copyText(accountForm.webhook_url)">复制</el-button>
            </template>
          </el-input>
          <div class="form-hint-block">
            <el-text type="info" size="small">
              {{ webhookUrlHint }}
            </el-text>
          </div>
        </el-form-item>
        <el-form-item label="启用 Webhook" prop="webhook_enabled">
          <el-switch v-model="accountForm.webhook_enabled" />
          <span class="form-hint">在开放平台管理端配置回调地址后开启</span>
        </el-form-item>
        <el-form-item label="启用 智能体" prop="ai_agent_enabled">
          <el-switch v-model="accountForm.ai_agent_enabled" />
          <span class="form-hint">开启后，QQ 群 @机器人 与单聊消息自动触发智能体回复</span>
        </el-form-item>
        <el-form-item label="状态" prop="status">
          <el-radio-group v-model="accountForm.status">
            <el-radio :label="1">正常</el-radio>
            <el-radio :label="2">停用</el-radio>
          </el-radio-group>
        </el-form-item>
      </el-form>
      <template #footer>
        <span class="dialog-footer">
          <el-button @click="dialogVisible = false">取消</el-button>
          <el-button type="primary" :loading="submitting" @click="submitForm">确认</el-button>
        </span>
      </template>
    </el-dialog>

    <el-dialog v-model="testDialogVisible" title="测试发送消息" width="520px">
      <el-form :model="testForm" label-width="110px">
        <el-form-item label="目标类型">
          <el-radio-group v-model="testForm.target_type">
            <el-radio label="group">群聊</el-radio>
            <el-radio label="c2c">单聊</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="目标 OpenID">
          <el-input v-model="testForm.target_openid" placeholder="群 openid 或用户 openid" />
        </el-form-item>
        <el-form-item label="消息内容">
          <el-input v-model="testForm.text" type="textarea" :rows="3" placeholder="请输入测试消息内容" />
        </el-form-item>
      </el-form>
      <div class="test-hint">
        <el-text type="info" size="small">
          注意：QQ 主动消息需对方开启「允许主动发送」且受平台频控；推荐在群里 @机器人 通过被动回复验证链路。
        </el-text>
      </div>
      <template #footer>
        <span class="dialog-footer">
          <el-button @click="testDialogVisible = false">取消</el-button>
          <el-button type="primary" :loading="testing" @click="submitTestSend">发送</el-button>
        </span>
      </template>
    </el-dialog>

    <AgentBindingDialog
      v-model="bindingDialogVisible"
      channel-type="qq"
      :account-id="bindingAccountId"
      :account-label="bindingAccountLabel"
      :account-enabled="bindingAccountEnabled"
    />
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  listAccounts,
  createAccount,
  updateAccount,
  deleteAccount,
  testSend,
  verifyCallback
} from '@/api/qqBot'
import AgentBindingDialog from '@/components/AgentBindingDialog.vue'

const loading = ref(false)
const submitting = ref(false)
const testing = ref(false)
const accounts = ref([])
const searchKeyword = ref('')
const verifyingId = ref(null)

const bindingDialogVisible = ref(false);
const bindingAccountId = ref('')
const bindingAccountLabel = ref('')
const bindingAccountEnabled = ref(true)

const openBindingDialog = (row) => {
  bindingAccountId.value = String(row.id)
  bindingAccountLabel.value = row.account_name || ''
  bindingAccountEnabled.value = row.status === 1
  bindingDialogVisible.value = true
}

const filteredAccounts = computed(() => {
  if (!searchKeyword.value) return accounts.value
  const kw = searchKeyword.value.toLowerCase()
  return accounts.value.filter(a => (a.account_name || '').toLowerCase().includes(kw))
})

const fetchAccounts = async () => {
  if (loading.value) return
  loading.value = true
  try {
    const res = await listAccounts()
    accounts.value = (res && res.list) || []
  } catch (e) {
    console.error('获取 QQ Bot 账号失败:', e)
    ElMessage.error(i18n.global.t('获取 Bot 账号失败'))
  } finally {
    loading.value = false
  }
}

const handleSearch = () => {}

const dialogVisible = ref(false);
const dialogType = ref('add')
const accountFormRef = ref(null)
const accountForm = reactive({
  id: null,
  account_name: '',
  app_id: '',
  app_secret: '',
  webhook_secret: '',
  webhook_url: '',
  webhook_url_suggested: '',
  webhook_enabled: true,
  ai_agent_enabled: true,
  status: 1
})

// Webhook URL 锁定逻辑：公网 base 可用且推导值合法时只读展示推导值；否则可编辑
const webhookUrlLocked = computed(() => {
  return !!accountForm.webhook_url_suggested && !accountForm.id_dirty_url
})
const webhookUrlHint = computed(() => {
  if (accountForm.webhook_url_suggested) {
    return '已按 PUBLIC_BASE_URL 自动推导（可直接复制到 q.qq.com → 开发者 → 回调配置）。修改公网域名后重新保存账号即可刷新。'
  }
  return '未检测到公网域名配置（PUBLIC_BASE_URL），请手动填写回调地址：https://你的域名/api/webhook/qq/{账号ID}，端口限 80/443/8080/8443。'
})

const rules = {
  account_name: [{ required: true, message: i18n.global.t('请输入账号名称'), trigger: 'blur' }],
  app_id: [{ required: true, message: i18n.global.t('请输入 AppID'), trigger: 'blur' }],
  app_secret: [
    {
      validator: (rule, value, callback) => {
        if (dialogType.value === 'add' && !value) {
          return callback(new Error('请输入 AppSecret'))
        }
        callback()
      },
      trigger: 'blur'
    }
  ]
}

const handleAdd = () => {
  dialogType.value = 'add'
  Object.assign(accountForm, {
    id: null,
    account_name: '',
    app_id: '',
    app_secret: '',
    webhook_secret: '',
    webhook_url: '',
    webhook_url_suggested: '',
    webhook_enabled: true,
    ai_agent_enabled: true,
    status: 1
  })
  dialogVisible.value = true
}

const handleEdit = (row) => {
  dialogType.value = 'edit'
  Object.assign(accountForm, {
    id: row.id,
    account_name: row.account_name,
    app_id: row.app_id,
    app_secret: '',
    webhook_secret: '',
    webhook_url: row.webhook_url,
    webhook_url_suggested: row.webhook_url_suggested || '',
    webhook_enabled: row.webhook_enabled,
    ai_agent_enabled: row.ai_agent_enabled,
    status: row.status
  })
  dialogVisible.value = true
}

const submitForm = () => {
  accountFormRef.value.validate(async (valid) => {
    if (!valid) return
    submitting.value = true
    try {
      const payload = {
        account_name: accountForm.account_name,
        app_id: accountForm.app_id,
        app_secret: accountForm.app_secret,
        webhook_secret: accountForm.webhook_secret,
        // 公网域名自动推导场景下不回传（空值=后端推导/保留原值）
        webhook_url: webhookUrlLocked.value ? '' : accountForm.webhook_url,
        webhook_enabled: accountForm.webhook_enabled,
        ai_agent_enabled: accountForm.ai_agent_enabled,
        status: accountForm.status
      }
      if (accountForm.id) {
        await updateAccount(accountForm.id, payload)
        ElMessage.success(i18n.global.t('更新成功'))
      } else {
        await createAccount(payload)
        ElMessage.success(i18n.global.t('创建成功'))
      }
      dialogVisible.value = false
      fetchAccounts()
    } catch (e) {
      console.error('保存失败:', e)
      ElMessage.error('保存失败：' + (e.message || e))
    } finally {
      submitting.value = false
    }
  })
}

const copyText = async (text) => {
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success('已复制到剪贴板')
  } catch {
    ElMessage.warning('浏览器不支持自动复制，请手动复制')
  }
}

const handleVerifyCallback = async (row) => {
  verifyingId.value = row.id
  try {
    const res = await verifyCallback(row.id)
    const sig = (res && res.signature) || ''
    ElMessageBox.alert(
      `签名前 32 位：${sig.slice(0, 32)}…\n\n${(res && res.hint) || '将回调地址粘贴到 q.qq.com 平台完成真实验证。'}`,
      '自检通过：验签链路连通',
      { confirmButtonText: '知道了', type: 'success' }
    ).catch(() => {})
  } catch (e) {
    ElMessage.error('自检失败：' + (e.message || e))
  } finally {
    verifyingId.value = null
  }
}

const handleDelete = (row) => {
  ElMessageBox.confirm(`确定要删除机器人 ${row.account_name} 吗？`, '警告', {
    confirmButtonText: '确定',
    cancelButtonText: '取消',
    type: 'warning'
  })
    .then(async () => {
      try {
        await deleteAccount(row.id)
        ElMessage.success(i18n.global.t('删除成功'))
        fetchAccounts()
      } catch (e) {
        ElMessage.error('删除失败：' + (e.message || e))
      }
    })
    .catch(() => {})
}

const testDialogVisible = ref(false);
const testForm = reactive({ target_type: 'group', target_openid: '', text: '' })
let currentTestAccountId = null

const handleTestSend = (row) => {
  currentTestAccountId = row.id
  testForm.target_type = 'group'
  testForm.target_openid = ''
  testForm.text = '你好，这是一条来自营销工具箱的测试消息。'
  testDialogVisible.value = true
}

const submitTestSend = async () => {
  if (!testForm.target_openid) {
    ElMessage.warning('请输入目标 OpenID')
    return
  }
  if (!testForm.text.trim()) {
    ElMessage.warning('请输入消息内容')
    return
  }
  testing.value = true
  try {
    await testSend(currentTestAccountId, {
      target_type: testForm.target_type,
      target_openid: testForm.target_openid,
      text: testForm.text
    })
    ElMessage.success('发送成功')
    testDialogVisible.value = false
  } catch (e) {
    ElMessage.error('发送失败：' + (e.message || e))
  } finally {
    testing.value = false
  }
}

onMounted(fetchAccounts)
</script>

<style scoped>
.qq-bot-management {
  padding: 16px;
}
.account-search {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
  gap: 12px;
}
.action-buttons {
  display: flex;
  gap: 8px;
  white-space: nowrap;
}
.qq-webhook-hint {
  margin-bottom: 16px;
}
.form-hint {
  margin-left: 12px;
  color: #909399;
  font-size: 12px;
}
.form-hint-block {
  margin-top: 4px;
  line-height: 1.4;
}
.test-hint {
  padding: 0 8px 8px;
}
.empty-data {
  padding: 32px 0;
}
</style>
