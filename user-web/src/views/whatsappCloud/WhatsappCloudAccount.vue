<template>
  <div class="whatsapp-cloud">
    <el-card class="card">
      <template #header>
        <div class="card-header">
          <span>WhatsApp Cloud 账号</span>
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
          prop="phone_number_id"
          label="PhoneNumberID"
          min-width="130"
          show-overflow-tooltip
        />
        <el-table-column
          prop="whatsapp_business_id"
          label="WABA ID"
          min-width="120"
          show-overflow-tooltip
        />
        <el-table-column
          label="启用接收"
          width="90"
          align="center"
        >
          <template #default="{ row }">
            <el-tag
              :type="row.webhook_enabled ? 'success' : 'info'"
              size="small"
            >
              {{ row.webhook_enabled ? '已启用' : '未启用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="智能体回复"
          width="100"
          align="center"
        >
          <template #default="{ row }">
            <el-tag
              :type="row.ai_agent_enabled ? 'success' : 'info'"
              size="small"
            >
              {{ row.ai_agent_enabled ? '已开启' : '未开启' }}
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
              @click="openTestSend(row)"
            >
              测试发送
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
      :title="dialogMode === 'edit' ? '编辑 WhatsApp Cloud 账号' : '新增 WhatsApp Cloud 账号'"
      width="640px"
    >
      <el-form
        :model="form"
        label-width="150px"
      >
        <el-form-item
          label="名称"
          prop="name"
        >
          <el-input
            v-model="form.name"
            placeholder="账号名称"
          />
        </el-form-item>
        <el-form-item
          label="备注"
          prop="remark"
        >
          <el-input
            v-model="form.remark"
            placeholder="备注（可选）"
          />
        </el-form-item>
        <el-form-item
          label="PhoneNumberID"
          prop="phone_number_id"
        >
          <el-input
            v-model="form.phone_number_id"
            placeholder="WhatsApp 电话号码 ID"
          />
        </el-form-item>
        <el-form-item
          label="WABA ID"
          prop="whatsapp_business_id"
        >
          <el-input
            v-model="form.whatsapp_business_id"
            placeholder="WhatsApp Business Account ID"
          />
        </el-form-item>
        <el-form-item
          label="Access Token"
          prop="access_token"
        >
          <el-input
            v-model="form.access_token"
            type="password"
            show-password
            placeholder="永久/临时 Access Token"
          />
        </el-form-item>
        <el-form-item
          label="App Secret"
          prop="app_secret"
        >
          <el-input
            v-model="form.app_secret"
            type="password"
            show-password
            placeholder="Meta 应用 App Secret"
          />
        </el-form-item>
        <el-form-item
          label="Verify Token"
          prop="verify_token"
        >
          <el-input
            v-model="form.verify_token"
            placeholder="回调验证 Token（自定义，与管理端一致）"
          />
          <div class="form-hint">
            在 Meta「Webhook 配置」中填写此 Token，用于 GET 验证回调地址。
          </div>
        </el-form-item>

        <el-divider content-position="left">
          回调（接收客户消息 → 智能体自动回复）
        </el-divider>
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
            将此 URL 配置到 Meta「Webhook 配置」的回调 URL 栏。
          </div>
        </el-form-item>
        <el-form-item label="启用接收">
          <el-switch v-model="form.webhook_enabled" />
          <span class="form-hint-inline">开启后接收客户消息回调</span>
        </el-form-item>
        <el-form-item label="智能体回复">
          <el-switch v-model="form.ai_agent_enabled" />
          <span class="form-hint-inline">开启后由已绑定智能体自动回复客户消息</span>
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

    
    <el-dialog
      v-model="testVisible"
      title="测试发送"
      width="520px"
    >
      <el-form
        :model="testForm"
        label-width="90px"
      >
        <el-form-item label="接收号码">
          <el-input
            v-model="testForm.to"
            placeholder="国际格式，如 8613800138000"
          />
        </el-form-item>
        <el-form-item label="消息内容">
          <el-input
            v-model="testForm.content"
            type="textarea"
            :rows="3"
            placeholder="测试消息内容"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="testVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          :loading="testing"
          @click="submitTestSend"
        >
          发送
        </el-button>
      </template>
    </el-dialog>

    
    <agent-binding-dialog
      v-model:visible="bindVisible"
      channel-type="whatsapp"
      :account-id="String(bindRow?.id)"
      :account-name="bindRow?.name"
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
  listWhatsappCloud,
  getWhatsappCloud,
  createWhatsappCloud,
  updateWhatsappCloud,
  deleteWhatsappCloud,
  testSendWhatsappCloud
} from '@/api/whatsappCloud'

const loading = ref(false)
const saving = ref(false)
const testing = ref(false)
const tableData = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(10)

const dialogVisible = ref(false)
const dialogMode = ref('create')
const form = reactive({
  id: null,
  name: '',
  remark: '',
  phone_number_id: '',
  whatsapp_business_id: '',
  access_token: '',
  app_secret: '',
  verify_token: '',
  webhook_enabled: false,
  ai_agent_enabled: false
})

const testVisible = ref(false)
const testForm = reactive({ id: null, to: '', content: '' })

const bindVisible = ref(false)
const bindRow = ref(null)

function callbackUrl(id) {
  if (!id) return ''
  return `${window.location.origin}/api/webhook/whatsapp/${id}`
}

async function loadData() {
  loading.value = true
  try {
    const res = await listWhatsappCloud({ page: page.value, page_size: pageSize.value })
    const arr = Array.isArray(res) ? res : (res.data || [])
    tableData.value = arr
    total.value = res && res.total != null ? res.total : arr.length
  } catch (e) {
    ElMessage.error('加载失败：' + (e.message || e))
  } finally {
    loading.value = false
  }
}

function resetForm() {
  Object.assign(form, {
    id: null,
    name: '',
    remark: '',
    phone_number_id: '',
    whatsapp_business_id: '',
    access_token: '',
    app_secret: '',
    verify_token: '',
    webhook_enabled: false,
    ai_agent_enabled: false
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
    const detail = await getWhatsappCloud(row.id)
    Object.assign(form, {
      id: detail.id,
      name: detail.account_name || detail.name || '',
      remark: '',
      phone_number_id: detail.phone_number_id || '',
      whatsapp_business_id: detail.whatsapp_business_id || '',
      access_token: '',
      app_secret: '',
      verify_token: detail.verify_token || '',
      webhook_enabled: !!detail.webhook_enabled,
      ai_agent_enabled: !!detail.ai_agent_enabled
    })
    dialogVisible.value = true
  } catch (e) {
    ElMessage.error('获取详情失败：' + (e.message || e))
  }
}

async function submit() {
  // 后端 whatsAppCloudCreateReq 的必填字段是 account_name / phone_number_id /
  // whatsapp_business_id / access_token（json tag 就是这几个名字）。原来发的是 `name`
  // 且空值用 `undefined`（JSON 序列化直接丢字段），于是每次保存都拿到 Go validator 原文。
  const payload = {
    account_name: String(form.name || '').trim(),
    phone_number_id: String(form.phone_number_id || '').trim(),
    whatsapp_business_id: String(form.whatsapp_business_id || '').trim(),
    access_token: String(form.access_token || '').trim(),
    phone_number_id: form.phone_number_id || undefined,
    whatsapp_business_id: form.whatsapp_business_id || undefined,
    access_token: form.access_token || undefined,
    app_secret: form.app_secret || undefined,
    verify_token: form.verify_token || undefined,
    webhook_enabled: form.webhook_enabled,
    ai_agent_enabled: form.ai_agent_enabled
  }
  if (!payload.account_name) { ElMessage.warning('请输入账号名称'); return }
  if (!payload.phone_number_id) { ElMessage.warning('请输入 Phone Number ID'); return }
  if (!payload.whatsapp_business_id) { ElMessage.warning('请输入 WhatsApp Business ID'); return }
  if (!payload.access_token) { ElMessage.warning('请输入 Access Token'); return }
  saving.value = true
  try {
    if (dialogMode.value === 'edit') {
      await updateWhatsappCloud(form.id, payload)
    } else {
      await createWhatsappCloud(payload)
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
    await ElMessageBox.confirm(`确认删除账号「${row.account_name || row.name || row.id}」？`, '提示', { type: 'warning' })
  } catch {
    return
  }
  try {
    await deleteWhatsappCloud(row.id)
    ElMessage.success('已删除')
    loadData()
  } catch (e) {
    ElMessage.error('删除失败：' + (e.message || e))
  }
}

function openTestSend(row) {
  testForm.id = row.id
  testForm.to = ''
  testForm.content = ''
  testVisible.value = true
}

async function submitTestSend() {
  if (!testForm.to || !testForm.content) {
    ElMessage.warning('请填写接收号码与消息内容')
    return
  }
  testing.value = true
  try {
    await testSendWhatsappCloud(testForm.id, { to: testForm.to, content: testForm.content })
    ElMessage.success('发送成功')
    testVisible.value = false
  } catch (e) {
    ElMessage.error('发送失败：' + (e.message || e))
  } finally {
    testing.value = false
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
