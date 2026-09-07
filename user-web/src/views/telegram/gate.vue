<template>
  <div class="tg-gate-management">
    <el-alert type="info" :closable="false" style="margin-bottom: 12px">
      <template #title>
        入群管控解决 Telegram 官方限制（Bot 无法主动私聊从未交互的用户）：
        <b>方案 A（join_request）</b>私密群开启「申请加入」，用户申请后引导其向 Bot 发送 /start 验证，通过后自动批准进群；
        <b>方案 B（mute_unlock）</b>公开群进群即禁言，用户点击群内验证链接在 Bot 私聊完成 /start 后自动解禁。超时未验证自动清理。
      </template>
    </el-alert>

    <div class="toolbar">
      <el-select v-model="filterAccount" placeholder="按 Bot 账号过滤" clearable style="width: 220px" @change="fetchGates">
        <el-option v-for="acc in accounts" :key="acc.id" :label="acc.account_name" :value="acc.id" />
      </el-select>
      <el-button type="primary" @click="handleAdd">添加群组管控</el-button>
      <el-button :loading="loading" @click="fetchGates">
        <el-icon><Refresh /></el-icon>
        <span>刷新</span>
      </el-button>
    </div>

    <el-table v-loading="loading" :data="gates" border>
      <el-table-column prop="id" label="ID" width="70" />
      <el-table-column label="群组" min-width="180">
        <template #default="scope">
          {{ scope.row.chat_title || scope.row.chat_id }}
        </template>
      </el-table-column>
      <el-table-column prop="chat_id" label="Chat ID" width="160" />
      <el-table-column label="管控模式" width="180">
        <template #default="scope">
          <el-tag :type="scope.row.mode === 'join_request' ? 'warning' : 'primary'" effect="plain">
            {{ scope.row.mode === 'join_request' ? '方案 A 申请审批' : '方案 B 禁言解锁' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="状态" width="100" align="center">
        <template #default="scope">
          <el-tag :type="scope.row.enabled ? 'success' : 'info'" effect="plain">
            {{ scope.row.enabled ? '已启用' : '未启用' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="verify_ttl_min" label="验证时限(分钟)" width="130" align="center" />
      <el-table-column label="操作" width="260" fixed="right">
        <template #default="scope">
          <el-button size="small" type="primary" @click="openMembers(scope.row)">成员台账</el-button>
          <el-button size="small" @click="handleEdit(scope.row)">编辑</el-button>
          <el-button size="small" type="danger" @click="handleDelete(scope.row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="dialogVisible" :title="dialogType === 'add' ? '添加群组管控' : '编辑群组管控'" width="620px" :close-on-click-modal="false">
      <el-form ref="gateFormRef" :model="gateForm" :rules="rules" label-width="130px">
        <el-form-item label="Bot 账号" prop="account_id">
          <el-select v-model="gateForm.account_id" placeholder="选择 Bot 账号" :disabled="dialogType === 'edit'" style="width: 100%">
            <el-option v-for="acc in accounts" :key="acc.id" :label="acc.account_name" :value="acc.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="群组 Chat ID" prop="chat_id">
          <el-input v-model="gateForm.chat_id" placeholder="群组 chat_id（负数，如 -1001234567890）" :disabled="dialogType === 'edit'" />
        </el-form-item>
        <el-form-item label="群组名称">
          <el-input v-model="gateForm.chat_title" placeholder="便于识别，可留空" />
        </el-form-item>
        <el-form-item label="管控模式" prop="mode">
          <el-radio-group v-model="gateForm.mode">
            <el-radio value="join_request">方案 A 申请审批（私密群）</el-radio>
            <el-radio value="mute_unlock">方案 B 禁言解锁（公开群）</el-radio>
          </el-radio-group>
          <div class="form-hint">
            方案 A 需在群管理开启「申请加入」；方案 B 需要 Bot 拥有「限制成员」管理员权限
          </div>
        </el-form-item>
        <el-form-item label="启用管控">
          <el-switch v-model="gateForm.enabled" />
          <span class="form-hint">关闭后本群恢复 Telegram 默认行为</span>
        </el-form-item>
        <el-form-item label="验证时限(分钟)">
          <el-input-number v-model="gateForm.verify_ttl_min" :min="1" :max="1440" />
          <span class="form-hint">超时未验证：方案 A 拒绝申请 / 方案 B 踢出群组</span>
        </el-form-item>
        <el-form-item label="提示语模板">
          <el-input v-model="gateForm.welcome_msg" type="textarea" :rows="3" placeholder="留空使用默认模板。可用占位符：%s = 用户名 / Bot 用户名 / 验证 token（按顺序）" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="handleSave">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="membersVisible" :title="`成员验证台账 - ${currentGate ? (currentGate.chat_title || currentGate.chat_id) : ''}`" width="860px">
      <el-tabs v-model="memberStatusTab" @tab-change="fetchMembers">
        <el-tab-pane label="全部" name="" />
        <el-tab-pane label="待验证" name="pending" />
        <el-tab-pane label="禁言中" name="restricted" />
        <el-tab-pane label="已放行" name="approved" />
        <el-tab-pane label="已清理" name="kicked" />
      </el-tabs>
      <el-table v-loading="membersLoading" :data="members" border size="small">
        <el-table-column prop="user_id" label="用户 ID" width="150" />
        <el-table-column prop="full_name" label="昵称" width="140" />
        <el-table-column prop="username" label="用户名" width="140">
          <template #default="scope">{{ scope.row.username ? '@' + scope.row.username : '-' }}</template>
        </el-table-column>
        <el-table-column label="状态" width="110" align="center">
          <template #default="scope">
            <el-tag :type="statusTagType(scope.row.join_status)" effect="plain">{{ statusLabel(scope.row.join_status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="已激活私聊" width="110" align="center">
          <template #default="scope">
            <el-tag :type="scope.row.authorized ? 'success' : 'info'" effect="plain">{{ scope.row.authorized ? '是' : '否' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="created_at" label="加入时间" min-width="160" />
        <el-table-column label="操作" width="120" fixed="right">
          <template #default="scope">
            <el-button v-if="!scope.row.authorized" size="small" type="success" @click="handleAuthorize(scope.row)">人工放行</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { listAccounts, listGates, createGate, updateGate, deleteGate, listGateMembers, authorizeGateMember } from '@/api/telegram'

const loading = ref(false)
const saving = ref(false)
const gates = ref([])
const accounts = ref([])
const filterAccount = ref(null)
const dialogVisible = ref(false)
const dialogType = ref('add')

const membersVisible = ref(false)
const membersLoading = ref(false)
const members = ref([])
const currentGate = ref(null)
const memberStatusTab = ref('')

const gateForm = reactive({
  id: null,
  account_id: null,
  chat_id: '',
  chat_title: '',
  mode: 'mute_unlock',
  enabled: true,
  verify_ttl_min: 10,
  welcome_msg: ''
})

const rules = {
  account_id: [{ required: true, message: '请选择 Bot 账号', trigger: 'change' }],
  chat_id: [{ required: true, message: '请输入群组 Chat ID', trigger: 'blur' }],
  mode: [{ required: true, message: '请选择管控模式', trigger: 'change' }]
}

const statusLabel = (s) => ({ pending: '待验证', restricted: '禁言中', approved: '已放行', kicked: '已清理' }[s] || s)
const statusTagType = (s) => ({ pending: 'warning', restricted: 'danger', approved: 'success', kicked: 'info' }[s] || 'info')

async function fetchAccounts() {
  const res = await listAccounts()
  accounts.value = res.data?.list || res.data || []
}

async function fetchGates() {
  loading.value = true
  try {
    const params = filterAccount.value ? { account_id: filterAccount.value } : {}
    const res = await listGates(params)
    gates.value = res.data?.list || res.data || []
  } finally {
    loading.value = false
  }
}

function handleAdd() {
  dialogType.value = 'add'
  Object.assign(gateForm, { id: null, account_id: filterAccount.value || null, chat_id: '', chat_title: '', mode: 'mute_unlock', enabled: true, verify_ttl_min: 10, welcome_msg: '' })
  dialogVisible.value = true
}

function handleEdit(row) {
  dialogType.value = 'edit'
  Object.assign(gateForm, { id: row.id, account_id: row.account_id, chat_id: row.chat_id, chat_title: row.chat_title, mode: row.mode, enabled: row.enabled, verify_ttl_min: row.verify_ttl_min, welcome_msg: row.welcome_msg })
  dialogVisible.value = true
}

async function handleSave() {
  saving.value = true
  try {
    const payload = { ...gateForm }
    if (dialogType.value === 'add') {
      await createGate(payload)
      ElMessage.success('创建成功')
    } else {
      await updateGate(gateForm.id, payload)
      ElMessage.success('更新成功')
    }
    dialogVisible.value = false
    fetchGates()
  } finally {
    saving.value = false
  }
}

async function handleDelete(row) {
  await ElMessageBox.confirm(`确定删除群组「${row.chat_title || row.chat_id}」的管控配置？`, '提示', { type: 'warning' })
  await deleteGate(row.id)
  ElMessage.success('已删除')
  fetchGates()
}

function openMembers(row) {
  currentGate.value = row
  memberStatusTab.value = ''
  membersVisible.value = true
  fetchMembers()
}

async function fetchMembers() {
  if (!currentGate.value) return
  membersLoading.value = true
  try {
    const params = { limit: 100 }
    if (memberStatusTab.value) params.status = memberStatusTab.value
    const res = await listGateMembers(currentGate.value.id, params)
    members.value = res.data?.list || res.data || []
  } finally {
    membersLoading.value = false
  }
}

async function handleAuthorize(row) {
  await authorizeGateMember(currentGate.value.id, row.id)
  ElMessage.success('已放行')
  fetchMembers()
}

onMounted(() => {
  fetchAccounts()
  fetchGates()
})
</script>

<style scoped>
.tg-gate-management { padding: 16px; }
.toolbar { display: flex; gap: 12px; margin-bottom: 12px; align-items: center; }
.form-hint { font-size: 12px; color: #909399; margin-left: 8px; }
</style>
