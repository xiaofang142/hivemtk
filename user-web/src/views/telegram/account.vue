<template>
  <div class="tg-bot-management">
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
            <el-button type="primary" @click="handleAdd">{{ $t('添加机器人') }}</el-button>
            <el-button type="info" :loading="loading" @click="fetchAccounts">
              <el-icon><Refresh /></el-icon>
              <span>{{ $t('刷新') }}</span>
            </el-button>
          </div>
        </div>

        <div v-loading="loading" class="account-list">
          <el-table :data="filteredAccounts" style="width: 100%" border>
            <el-table-column prop="account_name" :label="$t('账号名称')" min-width="160" />
            <el-table-column prop="bot_token_masked" label="Bot Token" min-width="200" />
            <el-table-column label="Token 健康度" width="130" align="center">
              <template #default="scope">
                <el-tooltip v-if="scope.row.last_error_msg && (scope.row.last_error_msg.includes('401') || scope.row.last_error_msg.includes('404'))" placement="top">
                  <template #content>Telegram 返回 401/404：Token 无效或已撤销。<br/>到 @BotFather → /mybots → API Token 重新生成，<br/>点「编辑」粘贴新 Token</template>
                  <el-tag type="danger" effect="plain">无效 → 点「编辑」换 Token</el-tag>
                </el-tooltip>
                <el-tag v-else-if="scope.row.last_error_msg" type="warning" effect="plain">{{ scope.row.last_error_msg.includes('429') ? '限流中' : '有错误' }}</el-tag>
                <el-tag v-else type="success" effect="plain">正常</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="webhook_url" label="Webhook URL" min-width="240" show-overflow-tooltip />
            <el-table-column label="Webhook" width="110" align="center">
              <template #default="scope">
                <el-tag :type="scope.row.webhook_enabled ? 'success' : 'info'" effect="plain">
                  {{ scope.row.webhook_enabled ? '已注册' : '未注册' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="智能体" width="110" align="center">
              <template #default="scope">
                <el-tag :type="scope.row.ai_agent_enabled ? 'success' : 'info'" effect="plain">
                  {{ scope.row.ai_agent_enabled ? '已启用' : '未启用' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="状态" width="100" align="center">
              <template #default="scope">
                <el-tag :type="scope.row.status === 1 ? 'success' : 'danger'" effect="plain">
                  {{ scope.row.status === 1 ? '正常' : '停用' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="last_error_msg" label="最近错误" min-width="180" show-overflow-tooltip />
            <el-table-column label="操作" width="400" fixed="right">
              <template #default="scope">
                <el-button size="small" @click="handleEdit(scope.row)">编辑</el-button>
                <el-button size="small" type="success" @click="handleRegisterWebhook(scope.row)">注册Webhook</el-button>
                <el-button size="small" type="warning" @click="handleTestSend(scope.row)">测试发送</el-button>
                <el-button size="small" type="primary" @click="openBindingDialog(scope.row)">绑定AI</el-button>
                <el-button size="small" type="danger" @click="handleDelete(scope.row)">删除</el-button>
              </template>
            </el-table-column>
          </el-table>
        </div>

        <div v-if="!loading && filteredAccounts.length === 0" class="empty-data">
          <el-empty description="暂无 Bot 账号，点击右上角添加" />
        </div>
      </div>
    </div>

    
    <el-dialog
      v-model="dialogVisible"
      :title="dialogType === 'add' ? '添加机器人' : '编辑机器人'"
      width="640px"
      :close-on-click-modal="false"
    >
      <el-form ref="accountFormRef" :model="accountForm" :rules="rules" label-width="120px">
        <el-form-item label="账号名称" prop="account_name">
          <el-input v-model="accountForm.account_name" placeholder="用于识别，例如：售前咨询Bot" />
        </el-form-item>
        <el-form-item label="Bot Token" prop="bot_token">
          <el-input
            v-model="accountForm.bot_token"
            :placeholder="dialogType === 'edit' ? '留空则保持原 Token 不变' : '形如 123456789:AAxxxx...（共 35 位）'"
            type="password"
            show-password
          />
          <div class="form-hint" style="font-size:12px;margin-top:4px">
            获取方式：Telegram 搜索 @BotFather → 发送 /mybots → 选择 Bot → API Token → 复制完整 Token（数字:35位字符）
          </div>
        </el-form-item>
        <el-form-item label="Webhook URL" prop="webhook_url">
          <el-input v-model="accountForm.webhook_url" placeholder="https://your-domain/api/webhook/telegram/1（必须 https://）" />
          <div class="form-hint" style="color:#e6a23c;font-size:12px;margin-top:4px">
            Telegram 强制 https。推荐留空由系统按公网域名自动推导，或填完整 https 地址
          </div>
        </el-form-item>
        <el-form-item label="Webhook Secret" prop="webhook_secret">
          <el-input v-model="accountForm.webhook_secret" placeholder="可选，用于 Telegram X-Telegram-Bot-Api-Secret-Token 校验" />
        </el-form-item>
        <el-form-item label="启用 Webhook" prop="webhook_enabled">
          <el-switch v-model="accountForm.webhook_enabled" />
          <span class="form-hint">开启后接受 Telegram 推送</span>
        </el-form-item>
        <el-form-item label="启用 智能体" prop="ai_agent_enabled">
          <el-switch v-model="accountForm.ai_agent_enabled" />
          <span class="form-hint">开启后，TG 入站消息和入群事件自动触发 智能体流程</span>
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

    
    <el-dialog v-model="testDialogVisible" title="测试发送消息" width="500px">
      <el-form :model="testForm" label-width="100px">
        <el-form-item label="目标 Chat ID">
          <el-input v-model.number="testForm.chat_id" placeholder="请输入目标 Chat ID（群组为负数）" />
        </el-form-item>
        <el-form-item label="消息内容">
          <el-input v-model="testForm.text" type="textarea" :rows="3" placeholder="请输入测试消息内容" />
        </el-form-item>
      </el-form>
      <template #footer>
        <span class="dialog-footer">
          <el-button @click="testDialogVisible = false">取消</el-button>
          <el-button type="primary" :loading="testing" @click="submitTestSend">发送</el-button>
        </span>
      </template>
    </el-dialog>

    
    <AgentBindingDialog
      v-model="bindingDialogVisible"
      channel-type="telegram"
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
  registerWebhook,
  testSend
} from '@/api/telegram'
import AgentBindingDialog from '@/components/AgentBindingDialog.vue'

const loading = ref(false)
const submitting = ref(false)
const testing = ref(false)
const accounts = ref([])
const searchKeyword = ref('')

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
    console.error('获取 Bot 账号失败:', e)
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
  bot_token: '',
  webhook_url: '',
  webhook_secret: '',
  webhook_enabled: true,
  ai_agent_enabled: true,
  status: 1
})

const rules = {
  account_name: [{ required: true, message: i18n.global.t('请输入账号名称'), trigger: 'blur' }],
  bot_token: [
    {
      validator: (rule, value, callback) => {
        if (dialogType.value === 'add' && !value) {
          return callback(new Error('请输入 Bot Token'))
        }
        if (value && !/^\d{6,10}:[A-Za-z0-9_-]{35}$/.test(value.trim())) {
          return callback(new Error('Token 格式不对：应为「数字:35位字母数字」，请到 @BotFather → /mybots → API Token 完整复制'))
        }
        callback()
      },
      trigger: 'blur'
    }
  ],
  webhook_url: [
    {
      validator: (rule, value, callback) => {
        if (value && value.trim().startsWith('http://')) {
          return callback(new Error('Telegram 强制要求 https://，请把 http:// 改成 https://'))
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
    bot_token: '',
    webhook_url: '',
    webhook_secret: '',
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
    bot_token: '',
    webhook_url: row.webhook_url,
    webhook_secret: '',
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
        bot_token: accountForm.bot_token,
        webhook_url: accountForm.webhook_url,
        webhook_secret: accountForm.webhook_secret,
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

const handleRegisterWebhook = (row) => {
  // 前置自检：给出可操作的提示，避免打到 Telegram 才收到晦涩报错
  const problems = []
  if (row.last_error_msg && row.last_error_msg.includes('401')) {
    problems.push('当前 Bot Token 已被 Telegram 判定无效（401）。请先到 @BotFather 重新生成 Token 并在「编辑」里更新，再注册 Webhook')
  }
  if (row.webhook_url && row.webhook_url.startsWith('http://')) {
    problems.push('Webhook URL 是 http:// 开头，Telegram 强制要求 https://，请在「编辑」里修改')
  }
  if (!row.webhook_url) {
    problems.push('Webhook URL 为空：请在「编辑」里填写（https://你的域名/api/webhook/telegram/' + row.id + '），或配置公网域名后留空自动推导')
  }
  if (problems.length) {
    ElMessageBox.alert(problems.join('\n'), '注册前检查未通过', { type: 'warning', confirmButtonText: '知道了' })
    return
  }
  ElMessageBox.confirm(`确定要为机器人 ${row.account_name} 注册 Webhook 吗？`, '确认', {
    confirmButtonText: '确定',
    cancelButtonText: '取消',
    type: 'info'
  })
    .then(async () => {
      try {
        await registerWebhook(row.id, { webhook_url: row.webhook_url })
        ElMessage.success(i18n.global.t('Webhook 注册成功'))
        fetchAccounts()
      } catch (e) {
        ElMessage.error('Webhook 注册失败：' + (e.message || e))
      }
    })
    .catch(() => {})
}

const testDialogVisible = ref(false);
const testForm = reactive({ chat_id: null, text: '' })
let currentTestAccountId = null

const handleTestSend = (row) => {
  currentTestAccountId = row.id
  testForm.chat_id = null
  testForm.text = '你好，这是一条来自营销工具箱的测试消息。'
  testDialogVisible.value = true
}

const submitTestSend = async () => {
  if (!testForm.chat_id) {
    ElMessage.warning(i18n.global.t('请输入目标 Chat ID'))
    return
  }
  if (!testForm.text) {
    ElMessage.warning(i18n.global.t('请输入消息内容'))
    return
  }
  testing.value = true
  try {
    await testSend(currentTestAccountId, { chat_id: testForm.chat_id, text: testForm.text })
    ElMessage.success(i18n.global.t('发送成功'))
    testDialogVisible.value = false
  } catch (e) {
    ElMessage.error('发送失败：' + (e.message || e))
  } finally {
    testing.value = false
  }
}

onMounted(() => {
  fetchAccounts()
})
</script>

<style lang="scss" scoped>
// 变量由 vite additionalData 全局注入(@import)，此处不得再 @use（dart-sass 要求 @use 先于一切规则）

.tg-bot-management {
  .account-tabs {
    background-color: #fff;
    border-radius: 4px;
    box-shadow: $box-shadow-light;
  }

  .account-list-container {
    padding: 20px;

    .account-search {
      display: flex;
      justify-content: space-between;
      align-items: center;
      margin-bottom: 20px;

      .el-input {
        width: 320px;
      }

      .action-buttons {
        display: flex;
        gap: 10px;
      }
    }

    .account-list {
      margin-bottom: 20px;
    }

    .empty-data {
      padding: 40px 0;
    }
  }
}

.form-hint {
  margin-left: 10px;
  color: #909399;
  font-size: 12px;
}
</style>
