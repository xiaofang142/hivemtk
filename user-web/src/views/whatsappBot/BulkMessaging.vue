<template>
  <div class="bulk-messaging">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>{{ $t('批量消息发送') }}</span>
        </div>
      </template>

      <el-tabs v-model="activeTab">
        <el-tab-pane label="发送消息" name="send">
          <el-form :model="sendForm" :rules="sendRules" ref="sendFormRef" label-width="120px">
            <el-row :gutter="20">
              <el-col :span="12">
                <el-form-item label="消息模板" prop="templateId">
                  <el-select 
                    v-model="sendForm.templateId" 
                    placeholder="请选择消息模板"
                    style="width: 100%"
                    @change="onTemplateChange"
                  >
                    <el-option
                      v-for="template in templates"
                      :key="template.id"
                      :label="template.name"
                      :value="template.id"
                    />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item label="发送账号" prop="accountId">
                  <el-select 
                    v-model="sendForm.accountId" 
                    placeholder="请选择WhatsApp账号"
                    style="width: 100%"
                  >
                    <el-option
                      v-for="account in accounts"
                      :key="account.id"
                      :label="account.name"
                      :value="account.id"
                    />
                  </el-select>
                </el-form-item>
              </el-col>
            </el-row>

            <el-form-item label="目标线索" prop="leadIds">
              <el-transfer
                v-model="sendForm.leadIds"
                filterable
                filter-placeholder="请输入线索姓名或电话"
                :data="transferData"
                :titles="['所有线索', '已选择']"
                :button-texts="['移除', '添加']"
                style="width: 100%"
              />
            </el-form-item>

            <el-form-item label="消息预览">
              <el-card shadow="never" class="preview-card">
                <div v-html="renderedContent"></div>
              </el-card>
            </el-form-item>

            <el-form-item label="计划发送时间">
              <el-date-picker
                v-model="sendForm.scheduleAt"
                type="datetime"
                placeholder="选择发送时间，为空则立即发送"
                format="YYYY-MM-DD HH:mm:ss"
                value-format="YYYY-MM-DD HH:mm:ss"
              />
            </el-form-item>

            <el-form-item>
              <el-button 
                type="primary" 
                @click="handleSend" 
                :loading="sending"
                :disabled="!canSend"
              >
                {{ sendForm.scheduleAt ? '计划发送' : '立即发送' }}
              </el-button>
              <el-button @click="resetForm">重置</el-button>
            </el-form-item>
          </el-form>
        </el-tab-pane>

        <el-tab-pane label="消息模板" name="templates">
          <div style="margin-bottom: 20px;">
            <el-button type="primary" @click="showTemplateDialog = true">新建模板</el-button>
          </div>

          <el-table :data="templates" style="width: 100%" v-loading="templateLoading">
            <el-table-column prop="name" label="模板名称" width="200" />
            <el-table-column prop="content" label="模板内容" show-overflow-tooltip />
            <el-table-column prop="category" label="分类" width="120" />
            <el-table-column prop="isActive" label="状态" width="100">
              <template #default="{ row }">
                <el-tag :type="row.isActive ? 'success' : 'danger'">
                  {{ row.isActive ? '启用' : '禁用' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="updatedAt" label="更新时间" width="180" />
            <el-table-column label="操作" width="200">
              <template #default="{ row }">
                <el-button size="small" @click="editTemplate(row)">编辑</el-button>
                <el-button size="small" type="danger" @click="deleteTemplate(row)">删除</el-button>
              </template>
            </el-table-column>
          </el-table>

          <el-pagination
            class="pagination"
            @size-change="handleTemplateSizeChange"
            @current-change="handleTemplateCurrentChange"
            :current-page="templatePagination.currentPage"
            :page-sizes="[10, 20, 50, 100]"
            :page-size="templatePagination.pageSize"
            layout="total, sizes, prev, pager, next, jumper"
            :total="templatePagination.total"
            v-if="templatePagination.total > 0"
          />
        </el-tab-pane>

        <el-tab-pane label="发送记录" name="records">
          <el-table :data="sendRecords" style="width: 100%" v-loading="recordLoading">
            <el-table-column prop="templateName" label="模板名称" width="200" />
            <el-table-column prop="totalCount" label="总数量" width="100" />
            <el-table-column prop="sentCount" label="已发送" width="100" />
            <el-table-column prop="failedCount" label="失败" width="100">
              <template #default="{ row }">
                <el-tag :type="row.failedCount > 0 ? 'danger' : 'success'">
                  {{ row.failedCount }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="status" label="状态" width="120">
              <template #default="{ row }">
                <el-tag :type="getStatusType(row.status)">
                  {{ getStatusText(row.status) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="createdAt" label="创建时间" width="180" />
            <el-table-column label="操作" width="150">
              <template #default="{ row }">
                <el-button size="small" @click="viewRecordDetails(row)">详情</el-button>
              </template>
            </el-table-column>
          </el-table>

          <el-pagination
            class="pagination"
            @size-change="handleRecordSizeChange"
            @current-change="handleRecordCurrentChange"
            :current-page="recordPagination.currentPage"
            :page-sizes="[10, 20, 50, 100]"
            :page-size="recordPagination.pageSize"
            layout="total, sizes, prev, pager, next, jumper"
            :total="recordPagination.total"
            v-if="recordPagination.total > 0"
          />
        </el-tab-pane>
      </el-tabs>
    </el-card>

    
    <el-dialog :title="templateDialogTitle" v-model="showTemplateDialog" width="60%">
      <el-form :model="templateForm" :rules="templateRules" ref="templateFormRef" label-width="100px">
        <el-form-item label="模板名称" prop="name">
          <el-input v-model="templateForm.name" placeholder="请输入模板名称" />
        </el-form-item>
        <el-form-item label="消息内容" prop="content">
          <el-input 
            v-model="templateForm.content" 
            type="textarea" 
            :rows="6"
            placeholder="请输入消息内容，可使用变量如：{{name}}、{{company}}等"
            maxlength="4096"
            show-word-limit
          />
          <div class="template-help">
            <p><strong>可用变量：</strong></p>
            <ul>
              <li><code>{{name}}</code> - 姓名</li>
              <li><code>{{phone}}</code> - 手机号</li>
              <li><code>{{email}}</code> - 邮箱</li>
              <li><code>{{company}}</code> - 公司</li>
              <li><code>{{source}}</code> - 来源</li>
            </ul>
          </div>
        </el-form-item>
        <el-form-item label="分类" prop="category">
          <el-select v-model="templateForm.category" placeholder="请选择分类">
            <el-option label="营销推广" value="marketing" />
            <el-option label="客户服务" value="customer_service" />
            <el-option label="活动通知" value="notification" />
            <el-option label="产品介绍" value="product" />
          </el-select>
        </el-form-item>
        <el-form-item label="状态" prop="isActive">
          <el-switch v-model="templateForm.isActive" active-text="启用" inactive-text="禁用" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input 
            v-model="templateForm.description" 
            type="textarea" 
            :rows="3"
            placeholder="请输入模板描述"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <span class="dialog-footer">
          <el-button @click="showTemplateDialog = false">取消</el-button>
          <el-button type="primary" @click="saveTemplate">确定</el-button>
        </span>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import DOMPurify from 'dompurify';
import * as bulkMessagingApi from '@/api/bulkMessaging'
import * as clueApi from '@/api/clue'
import * as whatsappApi from '@/api/whatsapp'
import { toList } from '@/utils/list'

const activeTab = ref('send')
const sending = ref(false)
const templateLoading = ref(false)
const recordLoading = ref(false)
const showTemplateDialog = ref(false)
const templateDialogTitle = ref('')
const sendFormRef = ref()
const templateFormRef = ref()

const sendForm = reactive({
  templateId: '',
  accountId: '',
  leadIds: [],
  scheduleAt: null
});

const sendRules = {
  templateId: [{ required: true, message: i18n.global.t('请选择消息模板'), trigger: 'change' }],
  accountId: [{ required: true, message: i18n.global.t('请选择WhatsApp账号'), trigger: 'change' }],
  leadIds: [{ required: true, message: i18n.global.t('请选择目标线索'), trigger: 'change' }]
}

const templateForm = reactive({
  id: '',
  name: '',
  content: '',
  category: 'marketing',
  isActive: true,
  description: ''
});

const templateRules = {
  name: [{ required: true, message: i18n.global.t('请输入模板名称'), trigger: 'blur' }],
  content: [{ required: true, message: i18n.global.t('请输入消息内容'), trigger: 'blur' }]
}

const templates = ref([]);
const accounts = ref([])
const allLeads = ref([])
const sendRecords = ref([])
const renderedContent = ref('')

const templatePagination = reactive({
  currentPage: 1,
  pageSize: 20,
  total: 0
});

const recordPagination = reactive({
  currentPage: 1,
  pageSize: 20,
  total: 0
})

const transferData = computed(() => {
  return allLeads.value.map(lead => ({
    key: lead.id,
    label: `${lead.name} (${lead.phone})`,
    disabled: false
  }))
});

const canSend = computed(() => {
  return sendForm.templateId && sendForm.accountId && sendForm.leadIds.length > 0
})

onMounted(() => {
  loadTemplates()
  loadAccounts()
  loadLeads()
  loadSendRecords()
})

const loadTemplates = async () => {
  try {
    templateLoading.value = true
    const response = await bulkMessagingApi.getTemplates()
    templates.value = toList(response)
  } catch (error) {
    ElMessage.error(i18n.global.t('加载模板失败'))
  } finally {
    templateLoading.value = false
  }
}

const loadAccounts = async () => {
  try {
    const response = await whatsappApi.getAccounts({ limit: 100 })
    accounts.value = response || [];
  } catch (error) {
    ElMessage.error(i18n.global.t('加载账号失败'))
  }
}

const loadLeads = async () => {
  try {
    const response = await clueApi.clueApi.list(1, 1000);
    const clueData = response?.list || response || [];
    allLeads.value = clueData.map(clue => ({
      id: clue.ID,
      name: clue.Name || clue.name || '未知',
      phone: clue.Account || clue.account || '',
      email: '',
      company: clue.Address || clue.address || clue.City || clue.city || '',
      source: 'clue',
      score: 80,
      status: 'new'
    }))
  } catch (error) {
    ElMessage.error(i18n.global.t('加载线索失败'))
  }
}

const loadSendRecords = async () => {
  try {
    recordLoading.value = true
    const response = await bulkMessagingApi.getSendRecords({
      page: recordPagination.currentPage,
      limit: recordPagination.pageSize
    })
    sendRecords.value = toList(response);
  } catch (error) {
    ElMessage.error(i18n.global.t('加载发送记录失败'))
  } finally {
    recordLoading.value = false
  }
}

const onTemplateChange = (templateId) => {
  const template = templates.value.find(t => t.id === templateId)
  if (template) {
    const rawHtml = template.content
      .replace('{{name}}', '张三')
      .replace('{{phone}}', '13800138000')
      .replace('{{email}}', 'zhangsan@example.com')
      .replace('{{company}}', '示例公司')
      .replace('{{source}}', '网站注册');
    renderedContent.value = DOMPurify.sanitize(rawHtml, { USE_PROFILES: { html: true } })
  }
}

const handleSend = async () => {
  try {
    await sendFormRef.value.validate()
    
    sending.value = true
    
    const response = await bulkMessagingApi.sendBulkMessage(sendForm)
    
    ElMessage.success(response.message || '消息已添加到发送队列')
    
    resetForm();
  } catch (error) {
    ElMessage.error(i18n.global.t('发送失败'))
  } finally {
    sending.value = false
  }
}

const resetForm = () => {
  sendForm.templateId = ''
  sendForm.accountId = ''
  sendForm.leadIds = []
  sendForm.scheduleAt = null
}

const editTemplate = (row) => {
  Object.assign(templateForm, row)
  templateDialogTitle.value = '编辑模板'
  showTemplateDialog.value = true
}

const deleteTemplate = async (row) => {
  try {
    await ElMessageBox.confirm(`确定要删除模板 "${row.name}" 吗？`, '确认删除', {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      type: 'warning'
    })
    
    await bulkMessagingApi.deleteTemplate(row.id)
    ElMessage.success(i18n.global.t('模板删除成功'))
    loadTemplates()
  } catch (error) { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
}

const saveTemplate = async () => {
  try {
    await templateFormRef.value.validate()
    
    if (templateForm.id) {
      await bulkMessagingApi.updateTemplate(templateForm.id, templateForm)
      ElMessage.success(i18n.global.t('模板更新成功'))
    } else {
      await bulkMessagingApi.createTemplate(templateForm)
      ElMessage.success(i18n.global.t('模板创建成功'))
    }
    
    showTemplateDialog.value = false
    resetTemplateForm()
    loadTemplates()
  } catch (error) {
    ElMessage.error(i18n.global.t('操作失败'))
  }
}

const resetTemplateForm = () => {
  templateForm.id = ''
  templateForm.name = ''
  templateForm.content = ''
  templateForm.category = 'marketing'
  templateForm.isActive = true
  templateForm.description = ''
}

const viewRecordDetails = (row) => {
  ElMessageBox.alert(DOMPurify.sanitize(`
    <div style="text-align: left;">
      <p><strong>模板名称:</strong> ${row.templateName}</p>
      <p><strong>总数量:</strong> ${row.totalCount}</p>
      <p><strong>已发送:</strong> ${row.sentCount}</p>
      <p><strong>失败:</strong> ${row.failedCount}</p>
      <p><strong>状态:</strong> ${getStatusText(row.status)}</p>
      <p><strong>创建时间:</strong> ${row.createdAt}</p>
    </div>
  `), '发送记录详情', {
    dangerouslyUseHTMLString: true,
    customClass: 'record-detail-box'
  });
}

const getStatusType = (status) => {
  switch (status) {
    case 'completed': return 'success'
    case 'timeout': return 'warning'
    case 'failed': return 'danger'
    case 'pending': return 'info'
    default: return 'info'
  }
}

const getStatusText = (status) => {
  switch (status) {
    case 'completed': return '已完成'
    case 'timeout': return '已超时'
    case 'failed': return '失败'
    case 'pending': return '待发送'
    default: return status
  }
}

const handleTemplateSizeChange = (val) => {
  templatePagination.pageSize = val
  loadTemplates()
}

const handleTemplateCurrentChange = (val) => {
  templatePagination.currentPage = val
  loadTemplates()
}

const handleRecordSizeChange = (val) => {
  recordPagination.pageSize = val
  loadSendRecords()
}

const handleRecordCurrentChange = (val) => {
  recordPagination.currentPage = val
  loadSendRecords()
}
</script>

<style scoped>
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.pagination {
  margin-top: 20px;
  text-align: right;
}

.preview-card {
  background-color: #f5f5f5;
  padding: 15px;
  border-radius: 4px;
}

.template-help {
  margin-top: 10px;
  padding: 10px;
  background-color: #f0f9ff;
  border-radius: 4px;
}

.template-help ul {
  margin: 5px 0;
  padding-left: 20px;
}

.template-help code {
  background-color: #eee;
  padding: 2px 4px;
  border-radius: 3px;
  font-family: monospace;
}

:deep(.record-detail-box) {
  text-align: left;
}
</style>
