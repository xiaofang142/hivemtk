<template>
  <div class="dingtalk-app">
    <el-card class="card">
      <template #header>
        <div class="card-header">
          <span>钉钉应用账号</span>
          <el-button
            type="primary"
            @click="openCreate"
          >
            新增账号
          </el-button>
        </div>
      </template>

      <el-table
        v-loading="loading"
        :data="tableData"
        border
        stripe
      >
        <el-table-column
          prop="id"
          label="ID"
          width="70"
        />
        <el-table-column
          prop="account_name"
          label="名称"
          min-width="120"
        />
        <el-table-column
          prop="app_key"
          label="AppKey"
          min-width="130"
          show-overflow-tooltip
        />
        <el-table-column
          prop="agent_id"
          label="AgentId"
          min-width="100"
          show-overflow-tooltip
        />
        <el-table-column
          label="启用收消息"
          width="100"
          align="center"
        >
          <template #default="{ row }">
            <el-tag
              :type="row.inbound_enabled ? 'success' : 'info'"
              size="small"
            >
              {{ row.inbound_enabled ? '已启用' : '未启用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="绑定AI"
          min-width="120"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            <el-tag
              :type="row.ai_agent_id ? 'success' : 'info'"
              size="small"
            >
              {{ row.ai_agent_id || '未绑定' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="320"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button
              link
              type="primary"
              @click="openTest(row)"
            >
              测试
            </el-button>
            <el-button
              link
              type="primary"
              @click="openBind(row)"
            >
              绑定AI
            </el-button>
            <el-button
              link
              type="primary"
              @click="copyCallbackUrl(row)"
            >
              复制回调URL
            </el-button>
            <el-button
              link
              type="primary"
              @click="openEdit(row)"
            >
              编辑
            </el-button>
            <el-button
              link
              type="danger"
              @click="handleDelete(row)"
            >
              删除
            </el-button>
          </template>
        </el-table-column>
      </el-table>

      <el-pagination
        v-if="total > pageSize"
        class="pager"
        background
        layout="total, prev, pager, next"
        :total="total"
        :page-size="pageSize"
        :current-page="page"
        @current-change="handlePage"
      />
    </el-card>

    
    <el-dialog
      v-model="dialogVisible"
      :title="dialogMode === 'edit' ? '编辑钉钉应用账号' : '新增钉钉应用账号'"
      width="680px"
    >
      <el-form
        :model="form"
        label-width="150px"
      >
        <el-form-item
          label="名称"
          prop="account_name"
        >
          <el-input
            v-model="form.account_name"
            placeholder="账号名称"
          />
        </el-form-item>
        <el-form-item
          label="AppKey"
          prop="app_key"
        >
          <el-input
            v-model="form.app_key"
            placeholder="钉钉应用 AppKey"
          />
        </el-form-item>
        <el-form-item
          label="AppSecret"
          prop="app_secret"
        >
          <el-input
            v-model="form.app_secret"
            type="password"
            show-password
            placeholder="钉钉应用 AppSecret"
          />
        </el-form-item>
        <el-form-item
          label="AgentId"
          prop="agent_id"
        >
          <el-input
            v-model="form.agent_id"
            placeholder="应用 AgentId（可选）"
          />
        </el-form-item>

        <el-divider content-position="left">
          回调（接收客户消息 → 智能体自动回复）
        </el-divider>
        <el-form-item
          label="回调验证 Token"
          prop="token"
        >
          <el-input
            v-model="form.token"
            placeholder="事件订阅的「签名Token」"
          />
          <div class="form-hint">
            在钉钉「事件订阅」中填写此 Token，用于 GET 验证回调地址。
          </div>
        </el-form-item>
        <el-form-item
          label="数据加密 Key"
          prop="aes_key"
        >
          <el-input
            v-model="form.aes_key"
            placeholder="回调数据加密密钥（可选；填写后消息体 AES 解密）"
          />
        </el-form-item>
        <el-form-item
          v-if="dialogMode === 'edit'"
          label="回调 URL"
        >
          <el-input
            :model-value="callbackUrl(form.id)"
            readonly
          >
            <template #append>
              <el-button @click="copyText(callbackUrl(form.id))">
                复制
              </el-button>
            </template>
          </el-input>
          <div class="form-hint">
            将此 URL 配置到钉钉「事件订阅」的「请求地址」栏。
          </div>
        </el-form-item>
        <el-form-item label="启用收消息">
          <el-switch v-model="form.inbound_enabled" />
          <span class="form-hint-inline">开启后接收客户发给应用的消息</span>
        </el-form-item>
        <el-form-item label="绑定 AI 智能体">
          <el-input
            v-model="form.ai_agent_id"
            placeholder="智能体 ID（可选；留空降级默认引擎）"
          />
          <div class="form-hint">
            也可在列表点击「绑定AI」通过对话框选择。
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          :loading="saving"
          @click="submit"
        >
          保存
        </el-button>
      </template>
    </el-dialog>

    
    <agent-binding-dialog
      v-model:visible="bindVisible"
      channel-type="dingtalk"
      :account-id="String(bindRow?.id)"
      :account-name="bindRow?.account_name"
      :current-agent-id="bindRow?.ai_agent_id ? String(bindRow.ai_agent_id) : ''"
      @saved="onBindSaved"
    />
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import AgentBindingDialog from '@/components/AgentBindingDialog.vue'
import { copyToClipboard } from '@/utils/clipboard.js'
import { isUserReportedError } from '@/utils/errorReporting'
import {
  listDingtalkApp,
  getDingtalkApp,
  createDingtalkApp,
  updateDingtalkApp,
  deleteDingtalkApp,
  testDingtalkApp
} from '@/api/dingtalkApp'

const loading = ref(false)
const saving = ref(false)
const tableData = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(10)

const dialogVisible = ref(false)
const dialogMode = ref('create')
const form = reactive({
  id: null,
  account_name: '',
  app_key: '',
  app_secret: '',
  agent_id: '',
  token: '',
  aes_key: '',
  inbound_enabled: false,
  ai_agent_id: ''
})

const bindVisible = ref(false)
const bindRow = ref(null)

function callbackUrl(id) {
  if (!id) return ''
  return `${window.location.origin}/api/webhook/dingtalk/${id}`
}

async function loadData() {
  loading.value = true
  try {
    const res = await listDingtalkApp({ page: page.value, page_size: pageSize.value })
    const arr = Array.isArray(res) ? res : (res.data || [])
    tableData.value = arr
    total.value = res && res.total != null ? res.total : arr.length
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error('加载失败：' + (e.message || e))
  } finally {
    loading.value = false
  }
}

function resetForm() {
  Object.assign(form, {
    id: null,
    account_name: '',
    app_key: '',
    app_secret: '',
    agent_id: '',
    token: '',
    aes_key: '',
    inbound_enabled: false,
    ai_agent_id: ''
  })
}

function openCreate() {
  dialogMode.value = 'create'
  resetForm()
  dialogVisible.value = true
}

async function openEdit(row) {
  dialogMode.value = 'edit'
  resetForm()
  try {
    const detail = await getDingtalkApp(row.id)
    Object.assign(form, {
      id: detail.id,
      account_name: detail.account_name || '',
      app_key: detail.app_key || '',
      app_secret: '',
      agent_id: detail.agent_id || '',
      token: detail.token || '',
      aes_key: '',
      inbound_enabled: !!detail.inbound_enabled,
      ai_agent_id: detail.ai_agent_id || ''
    })
    dialogVisible.value = true
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error('获取详情失败：' + (e.message || e))
  }
}

async function submit() {
  const payload = {
    account_name: form.account_name || undefined,
    app_key: form.app_key || undefined,
    app_secret: form.app_secret || undefined,
    agent_id: form.agent_id || undefined,
    token: form.token || undefined,
    aes_key: form.aes_key || undefined,
    inbound_enabled: form.inbound_enabled,
    ai_agent_id: form.ai_agent_id || undefined
  }
  saving.value = true
  try {
    if (dialogMode.value === 'edit') {
      await updateDingtalkApp(form.id, payload)
    } else {
      await createDingtalkApp(payload)
    }
    ElMessage.success('保存成功')
    dialogVisible.value = false
    loadData()
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error('保存失败：' + (e.message || e))
  } finally {
    saving.value = false
  }
}

async function handleDelete(row) {
  try {
    await ElMessageBox.confirm(`确认删除账号「${row.account_name}」？`, '提示', { type: 'warning' })
  } catch {
    return
  }
  try {
    await deleteDingtalkApp(row.id)
    ElMessage.success('已删除')
    loadData()
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error('删除失败：' + (e.message || e))
  }
}

async function openTest(row) {
  try {
    const res = await testDingtalkApp(row.id)
    ElMessage.success('配置校验通过：' + (res?.ok ? 'AppKey/AppSecret/Token 齐全' : 'OK'))
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error('测试失败：' + (e.message || e))
  }
}

function openBind(row) {
  bindRow.value = row
  bindVisible.value = true
}

function onBindSaved() {
  ElMessage.success('AI 绑定已保存')
  loadData()
}

async function copyText(text) {
  if (!text) return
  const copied = await copyToClipboard(text)
  if (copied) {
    ElMessage.success('已复制到剪贴板')
  } else {
    ElMessage.error('复制失败，请手动复制')
  }
}

function copyCallbackUrl(row) {
  copyText(callbackUrl(row.id))
}

function handlePage(p) {
  page.value = p
  loadData()
}

onMounted(loadData)
</script>

<style scoped>
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.pager {
  margin-top: 16px;
  justify-content: flex-end;
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
</style>
