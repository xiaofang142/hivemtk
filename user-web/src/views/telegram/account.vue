<template>
  <div class="tg-bot-management">

    <!-- ===== 操作说明卡片（首次使用必读）===== -->
    <el-card shadow="never" class="setup-guide-card">
      <template #header>
        <div class="guide-header">
          <span class="guide-title">📖 Telegram 机器人配置指南</span>
          <el-tag effect="plain" size="small">首次使用必读 · 约 2 分钟</el-tag>
        </div>
      </template>

      <el-steps :active="0" direction="horizontal" finish-status="success" simple>
        <el-step title="1. 创建 Bot" description="@BotFather 申请" />
        <el-step title="2. 拉进群+设管理员" description="必要权限配置" />
        <el-step title="3. 关闭隐私模式" description="收不到群消息的根因" />
        <el-step title="4. 粘贴 Token 到此页" description="其余全自动" />
      </el-steps>

      <el-collapse class="guide-collapse" accordion>

        <!-- Step 1: 创建 Bot -->
        <el-collapse-item title="① 创建 Bot（一次性操作）" name="s1">
          <div class="guide-content">
            <ol>
              <li>打开 Telegram → 搜索 <code>@BotFather</code></li>
              <li>发送 <code>/newbot</code> → 给 Bot 起名字（展示用）</li>
              <li>发送 <code>/setdescription</code> <code>/setabouttext</code> <code>/setuserpic</code> 配置 Bot 信息（可选）</li>
              <li><strong>⚠️ 关键一步：发送 <code>/setjoingroups</code> → 选 <code>Enable</code></strong>
                <br>默认是 Disable，不改的话 Bot 加不进任何群！</li>
              <li><strong>⚠️ 关键一步：发送 <code>/setprivacy</code> → 选 <code>Disable</code></strong>
                <br>默认是 Enable，不改的话 Bot 收不到群内 @消息和 reply！</li>
              <li>发送 <code>/token</code> → 复制出来的 API Token 就是 Bot 凭证</li>
              <li>发送 <code>/mybots</code> → 选你的 Bot → 可以随时重新生成 Token</li>
            </ol>
            <el-alert type="warning" :closable="false" show-icon>
              <template #title>
                Token 泄露了？马上到 @BotFather → /mybots → 选 Bot → 「Revoke current token」→ 重新生成即可。旧 Token 立刻失效。
              </template>
            </el-alert>
          </div>
        </el-collapse-item>

        <!-- Step 2: 拉进群+权限 -->
        <el-collapse-item title="② 拉进群 + 设管理员权限" name="s2">
          <div class="guide-content">
            <ol>
              <li>打开你的 Telegram 群 → 群设置 → 成员列表 → 「邀请成员」</li>
              <li>搜索你刚创建的 Bot 用户名（如 @hivemtk_bot）→ 添加</li>
              <li>群设置 → 管理员 → 「添加管理员」→ 选这个 Bot</li>
              <li><strong>勾选以下权限（根据需求）：</strong>
                <ul>
                  <li>✅ <code>can_invite_users</code> 邀请用户 / 批准入群申请（方案 A 审批制需要）</li>
                  <li>✅ <code>can_restrict_members</code> 限制成员 / 禁言（方案 B 进群后禁言解锁需要）</li>
                  <li>✅ <code>can_delete_messages</code> 删除消息（群规维护）</li>
                  <li>✅ <code>can_send_messages</code> 发送消息（基本能力）</li>
                </ul>
              </li>
            </ol>
            <el-alert type="info" :closable="false" show-icon>
              <template #title>
                如果是「方案 B 进群后禁言解锁」的公开群，建议群设置里开启「慢速模式 slow mode = 10 秒」，防止用户进群后 Bot 禁言前先发消息。
              </template>
            </el-alert>
          </div>
        </el-collapse-item>

        <!-- Step 3: Webhook vs Polling -->
        <el-collapse-item title="③ Webhook vs Polling（后端自动选择）" name="s3">
          <div class="guide-content">
            <table class="guide-table">
              <thead>
                <tr><th>模式</th><th>适用场景</th><th>需要公网？</th><th>性能</th></tr>
              </thead>
              <tbody>
                <tr><td>🔗 Webhook</td><td>生产部署，有公网域名 + HTTPS</td><td>✅ 需要</td><td>⭐⭐⭐ 低延迟、高并发</td></tr>
                <tr><td>🔄 Polling</td><td>本地开发 / 内网测试</td><td>❌ 不需要</td><td>⭐ 低延迟、并发有限</td></tr>
              </tbody>
            </table>
            <p><strong>系统自动决策：</strong>创建 Bot 时如果后端配置了 <code>PUBLIC_BASE_URL</code>（如 <code>https://your-domain.com</code>），自动走 Webhook 模式；否则自动降级 Polling。</p>
            <p>想强制切换？到列表页点「注册 Webhook」按钮，或重启服务自动重选。</p>
          </div>
        </el-collapse-item>

        <!-- Step 4: 绑定智能体 -->
        <el-collapse-item title="④ 绑定智能体 + 资产包" name="s4">
          <div class="guide-content">
            <p>创建 Bot 后，到 <strong>智能体页面</strong> 创建一个智能体，选择对应的<strong>资产包</strong>，然后在本页 Bot 行点「绑定智能体」按钮关联即可。</p>
            <p>推荐配置：</p>
            <ul>
              <li><code>hivemtk_bot</code>（产品社群）→ 绑定「hivemtk自用智能体」+「hivemtk社群资产包」</li>
              <li><code>lovesanrenxing_bot</code>（普通社群）→ 绑定「通用社群混合智能体」+「通用社群混合资产包」</li>
              <li>电商场景 → 绑定「通用电商混合智能体」+「通用电商销售混合资产包」</li>
            </ul>
          </div>
        </el-collapse-item>

        <!-- 常见问题 -->
        <el-collapse-item title="⑤ 常见问题 FAQ" name="s5">
          <div class="guide-content">
            <div class="faq-item">
              <strong>❓ Bot 创建了但群内 @不回复？</strong>
              <p>99% 是没关隐私模式 → @BotFather → /setprivacy → Disable。重启服务生效。</p>
            </div>
            <div class="faq-item">
              <strong>❓ Webhook 注册失败 401？</strong>
              <p>服务器需要公网 HTTPS 域名 + 能被 Telegram 访问。本地开发自动降级 Polling，不用管。</p>
            </div>
            <div class="faq-item">
              <strong>❓ Token 健康度显示「无效」？</strong>
              <p>Token 被 @BotFather 吊销了。到本页点「编辑」→ 粘贴新 Token → 保存。</p>
            </div>
            <div class="faq-item">
              <strong>❓ 智能体不自动回群消息？</strong>
              <p>① 检查是否绑定了智能体（本页「绑定智能体」按钮）<br>② 检查资产包是否正确挂载<br>③ 群内需要 @机器人 或配置关键词触发</p>
            </div>
          </div>
        </el-collapse-item>

      </el-collapse>
    </el-card>

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
      <el-form ref="accountFormRef" :model="accountForm" :rules="rules" label-width="140px">
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
        <!-- 测试 Token 按钮（创建模式必显；编辑模式仅当填了新 Token 时显） -->
        <el-form-item v-if="showTestToken" label=" ">
          <el-button type="success" :loading="testingToken" @click="handleTestToken">
            {{ testingToken ? '验证中...' : '🔍 测试 Token 有效性' }}
          </el-button>
          <span v-if="tokenTestResult" :style="{ marginLeft: '12px', fontSize: '13px', color: tokenTestResult.ok ? '#67c23a' : '#f56c6c' }">
            {{ tokenTestResult.ok ? `✅ Token 有效！Bot @${tokenTestResult.username || '(无username)'} 就绪` : '❌ ' + tokenTestResult.msg }}
          </span>
        </el-form-item>

        <!-- ========== 以下为"编辑模式只读展示区"（创建模式不显示） ========== -->
        <template v-if="dialogType === 'edit'">
          <el-divider content-position="left">系统自动配置（只读）</el-divider>
          <el-form-item label="Bot 用户名">
            <el-input :model-value="accountForm.bot_username || '(后端自动填充)'" disabled />
          </el-form-item>
          <el-form-item label="Webhook URL">
            <el-input :model-value="accountForm.webhook_url || '(后端自动推导)'" disabled>
              <template v-if="accountForm.webhook_url" #append>
                <el-button @click="copyText(accountForm.webhook_url)">复制</el-button>
              </template>
            </el-input>
          </el-form-item>
          <el-form-item label="Webhook Secret">
            <el-input :model-value="accountForm.webhook_secret ? '*** (已隐藏)' : '(后端自动生成)'" disabled />
          </el-form-item>
          <el-form-item label="Webhook 状态">
            <el-tag :type="accountForm.webhook_enabled ? 'success' : 'info'" effect="plain">
              {{ accountForm.webhook_enabled ? '已注册' : '未注册' }}
            </el-tag>
          </el-form-item>
          <el-form-item label="智能体开关">
            <el-tag :type="accountForm.ai_agent_enabled ? 'success' : 'info'" effect="plain">
              {{ accountForm.ai_agent_enabled ? '已启用' : '未启用' }}
            </el-tag>
          </el-form-item>
          <el-form-item label="状态">
            <el-tag :type="accountForm.status === 1 ? 'success' : 'danger'" effect="plain">
              {{ accountForm.status === 1 ? '正常' : '停用' }}
            </el-tag>
          </el-form-item>
          <div class="form-hint" style="color:#909399;font-size:12px;margin-left:140px;margin-top:-8px">
            以上字段由后端自动管理，如需修改请使用列表上方的「注册Webhook」等按钮
          </div>
        </template>
      </el-form>
      <template #footer>
        <span class="dialog-footer">
          <el-button @click="dialogVisible = false">取消</el-button>
          <el-button type="primary" :loading="submitting" @click="submitForm">
            {{ dialogType === 'add' ? '创建（自动配置）' : '保存' }}
          </el-button>
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
const testingToken = ref(false)
const tokenTestResult = ref(null) // { ok: bool, username: str, msg: str }
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
  bot_username: '',
  webhook_url: '',
  webhook_secret: '',
  webhook_enabled: false,
  ai_agent_enabled: false,
  status: 1
})

// 是否显示"测试 Token"按钮：创建时始终显示；编辑时仅当填了新 Token 才显示
const showTestToken = computed(() => {
  if (dialogType.value === 'add') return !!accountForm.bot_token
  return !!accountForm.bot_token // 编辑时只要填了 token 就可测
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
  ]
}

const handleAdd = () => {
  dialogType.value = 'add'
  tokenTestResult.value = null
  Object.assign(accountForm, {
    id: null,
    account_name: '',
    bot_token: '',
    bot_username: '',
    webhook_url: '',
    webhook_secret: '',
    webhook_enabled: false,
    ai_agent_enabled: false,
    status: 1
  })
  dialogVisible.value = true
}

const handleEdit = (row) => {
  dialogType.value = 'edit'
  tokenTestResult.value = null
  Object.assign(accountForm, {
    id: row.id,
    account_name: row.account_name,
    bot_token: '',
    bot_username: row.bot_username || '',
    webhook_url: row.webhook_url || '',
    webhook_secret: row.webhook_secret || '',
    webhook_enabled: !!row.webhook_enabled,
    ai_agent_enabled: !!row.ai_agent_enabled,
    status: row.status
  })
  dialogVisible.value = true
}

/**
 * 测试 Bot Token 有效性
 * 直接前端调用 Telegram getMe API（公开接口，无需后端代理）
 */
const handleTestToken = async () => {
  const token = (accountForm.bot_token || '').trim()
  if (!token) {
    ElMessage.warning('请先填写 Bot Token')
    return
  }
  testingToken.value = true
  tokenTestResult.value = null
  try {
    // Telegram getMe 是公开 REST API，直接 fetch（CORS 友好）
    const resp = await fetch(`https://api.telegram.org/bot${token}/getMe`)
    const data = await resp.json()
    if (data && data.ok && data.result) {
      const u = data.result
      tokenTestResult.value = {
        ok: true,
        username: u.username,
        firstName: u.first_name,
        msg: ''
      }
      // 自动填充 bot_username 到表单
      if (u.username) {
        accountForm.bot_username = '@' + u.username
      }
      ElMessage.success(`Token 验证通过：@${u.username || '(无)'}`)
    } else {
      const desc = data?.description || 'Token 无效'
      tokenTestResult.value = { ok: false, msg: desc.replace(/^[^:]+:\s*/, '') }
      ElMessage.error(`Token 无效: ${desc}`)
    }
  } catch (err) {
    tokenTestResult.value = { ok: false, msg: '网络错误，无法连接 Telegram API' }
    ElMessage.error('网络错误: ' + err.message)
  } finally {
    testingToken.value = false
  }
}

const copyText = async (text) => {
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success('已复制到剪贴板')
  } catch {
    ElMessage.warning('浏览器不支持自动复制，请手动复制')
  }
}

const submitForm = () => {
  accountFormRef.value.validate(async (valid) => {
    if (!valid) return
    submitting.value = true
    try {
      let payload
      if (accountForm.id) {
        // Update：只传需要改的字段；webhook_secret 留空=保留原值
        payload = {
          account_name: accountForm.account_name
        }
        if (accountForm.bot_token) {
          payload.bot_token = accountForm.bot_token
        }
        // webhook_enabled / ai_agent_enabled / status 由后端自动管理，
        // 编辑对话框不允许修改这些字段，所以 Update 时不传它们
      } else {
        // Create：只传 2 个必填字段，其余后端自动处理
        payload = {
          account_name: accountForm.account_name,
          bot_token: accountForm.bot_token
        }
      }
      if (accountForm.id) {
        await updateAccount(accountForm.id, payload)
        ElMessage.success(i18n.global.t('更新成功'))
      } else {
        const res = await createAccount(payload)
        ElMessage.success(i18n.global.t('创建成功！正在后台自动配置 Bot...'))
        // 创建成功后立刻刷新列表，展示后端自动填充的字段
        fetchAccounts()
      }
      dialogVisible.value = false
      if (!accountForm.id) {
        fetchAccounts()
      }
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
    problems.push('Webhook URL 为空：请先在「编辑」对话框填入（或等待后端自动推导完成后再点注册）')
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

  // ===== 操作说明卡片样式 =====
  .setup-guide-card {
    margin-bottom: 20px;
    border-left: 4px solid #409eff;

    .guide-header {
      display: flex;
      justify-content: space-between;
      align-items: center;
      gap: 12px;

      .guide-title {
        font-size: 15px;
        font-weight: 600;
        color: #303133;
      }
    }

    .guide-collapse {
      margin-top: 10px;
      border: none;

      .el-collapse-item__header {
        font-weight: 500;
        font-size: 14px;
        border-bottom: none;
        padding: 8px 0;
      }

      .el-collapse-item__wrap {
        border-bottom: none;
      }
    }

    .guide-content {
      font-size: 13px;
      line-height: 1.8;
      color: #606266;
      padding: 4px 0 12px;

      ol {
        padding-left: 20px;

        li {
          margin-bottom: 6px;
          code {
            background: #f4f4f5;
            border-radius: 3px;
            padding: 1px 5px;
            font-size: 12px;
            color: #e6a23c;
          }
        }

        ul {
          margin: 6px 0 6px 10px;
          padding-left: 18px;

          li {
            margin-bottom: 3px;
            code {
              color: #409eff;
              font-size: 12px;
            }
          }
        }
      }

      .faq-item {
        margin-bottom: 12px;
        padding: 8px 12px;
        background: #fafafa;
        border-radius: 6px;
        border-left: 3px solid #e6a23c;

        strong {
          font-size: 13px;
          color: #303133;
        }

        p {
          margin: 4px 0 0;
          font-size: 12px;
          color: #606266;
        }
      }

      .guide-table {
        width: 100%;
        border-collapse: collapse;
        margin-bottom: 10px;
        font-size: 12px;

        th, td {
          border: 1px solid #ebeef5;
          padding: 6px 10px;
          text-align: left;
        }

        th {
          background: #f5f7fa;
          font-weight: 600;
        }
      }

      p {
        margin: 6px 0;

        code {
          background: #ecf5ff;
          color: #409eff;
          border-radius: 3px;
          padding: 1px 5px;
          font-size: 12px;
        }
      }
    }
  }

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
