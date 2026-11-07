<template>
  <div class="email-list">
    <div class="page-header">
      <h1>{{ $t('邮件列表') }}</h1>
      <div class="toolbar">
        <el-button
          type="primary"
          @click="openSend"
        >
          {{ $t('发送邮件') }}
        </el-button>
        <el-button @click="fetchEmailList">
          {{ $t('刷新') }}
        </el-button>
      </div>
    </div>
    
    <div class="table-container">
      <el-table
        v-loading="loading"
        :data="emailList"
        style="width: 100%"
      >
        <el-table-column
          prop="subject"
          :label="$t('主题')"
          width="200"
        />
        <el-table-column
          prop="to"
          :label="$t('收件人')"
          width="150"
        />
        <el-table-column
          prop="from"
          :label="$t('发件人')"
          width="150"
        />
        <el-table-column
          :label="$t('是否发送')"
          width="100"
        >
          <template #default="scope">
            <el-tag :type="scope.row.is_send > 0 ? 'success' : 'info'">
              {{ scope.row.is_send > 0 ? '是' : '否' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="发送时间"
          width="150"
        >
          <template #default="scope">
            {{ scope.row.is_send > 0 ? scope.row.send_time : '-' }}
          </template>
        </el-table-column>
        <el-table-column
          label="是否阅读"
          width="100"
        >
          <template #default="scope">
            <el-tag :type="scope.row.is_read > 0 ? 'success' : 'info'">
              {{ scope.row.is_read > 0 ? '是' : '否' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="阅读时间"
          width="150"
        >
          <template #default="scope">
            {{ scope.row.is_read > 0 ? scope.row.read_time : '-' }}
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="160"
          fixed="right"
        >
          <template #default="scope">
            <el-button
              link
              type="primary"
              @click="openTrace(scope.row)"
            >
              {{ $t('查看追踪') }}
            </el-button>
            <el-button
              link
              type="danger"
              @click="handleDelete(scope.row)"
            >
              {{ $t('删除') }}
            </el-button>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty description="暂无数据" />
        </template>
      </el-table>
      
      <div class="pagination-container">
        <el-pagination 
          v-model:current-page="emailPage"
          :page-size="emailLimit"
          :total="emailTotal"
          layout="prev, pager, next"
          @current-change="handlePageChange"
        />
      </div>
    </div>

    
    <el-dialog
      v-model="sendVisible"
      :title="$t('发送邮件')"
      width="640px"
    >
      <el-form
        ref="sendFormRef"
        :model="sendForm"
        :rules="sendRules"
        label-width="80px"
      >
        <el-form-item
          label="收件人"
          prop="to"
        >
          <el-input
            v-model="sendForm.to"
            placeholder="收件人邮箱，多个用逗号分隔"
          />
        </el-form-item>
        <el-form-item
          label="发件账号"
          prop="smtpId"
        >
          <el-select
            v-model="sendForm.smtpId"
            placeholder="请选择邮件代理"
            style="width: 100%"
          >
            <el-option
              v-for="item in smtpList"
              :key="item.id"
              :label="item.name ? `${item.name} (${item.username})` : (item.username || item.server || item.id)"
              :value="String(item.id)"
            />
          </el-select>
        </el-form-item>
        <el-form-item
          :label="$t('主题')"
          prop="subject"
        >
          <el-input
            v-model="sendForm.subject"
            :placeholder="$t('主题')"
          />
        </el-form-item>
        <el-form-item
          label="内容"
          prop="content"
        >
          <el-input
            v-model="sendForm.content"
            type="textarea"
            :rows="6"
            placeholder="邮件内容"
          />
        </el-form-item>
        <el-form-item label="附件">
          <el-input
            v-model="attachmentsText"
            type="textarea"
            :rows="2"
            placeholder="附件URL，多个用逗号或换行分隔"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="sendVisible = false">
          {{ $t('取消') }}
        </el-button>
        <el-button
          type="primary"
          :loading="submitting"
          @click="submitSend"
        >
          {{ $t('确定') }}
        </el-button>
      </template>
    </el-dialog>

    
    <el-dialog
      v-model="traceVisible"
      :title="$t('追踪信息')"
      width="640px"
    >
      <div v-loading="traceLoading">
        <div v-if="traceData && traceData.length">
          <el-table
            :data="traceData"
            border
          >
            <el-table-column
              prop="email"
              label="收件人"
            />
            <el-table-column
              prop="eventType"
              label="状态"
            />
            <el-table-column
              prop="timestamp"
              label="时间"
            />
          </el-table>
        </div>
        <el-empty
          v-else
          description="暂无追踪数据"
        />
      </div>
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { emailApi } from '@/api/email'

const emailList = ref([])
const emailTotal = ref(0)
const emailPage = ref(1)
const emailLimit = ref(10)
const loading = ref(false);

const sendVisible = ref(false);
const submitting = ref(false)
const sendFormRef = ref(null)
const sendForm = reactive({ to: '', subject: '', content: '', smtpId: '' })
const smtpList = ref([])
const attachmentsText = ref('')

const validateRecipients = (rule, value, callback) => {
  const list = String(value || '').split(/[,;\s]+/).map(s => s.trim()).filter(Boolean)
  if (!list.length) {
    callback(new Error('请输入收件人'))
    return
  }
  const bad = list.find(addr => !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(addr))
  if (bad) {
    callback(new Error(`收件人格式不正确：${bad}`))
    return
  }
  callback()
}

const sendRules = {
  to: [{ required: true, validator: validateRecipients, trigger: 'blur' }],
  smtpId: [{ required: true, message: '请选择发件账号', trigger: 'change' }],
  subject: [{ required: true, message: '请填写主题', trigger: 'blur' }],
  content: [{ required: true, message: '请填写内容', trigger: 'blur' }]
}

const traceVisible = ref(false);
const traceLoading = ref(false)
const traceData = ref([])

const handlePageChange = (number) => {
  if (emailPage.value === number)
    return;
  emailPage.value = number
  fetchEmailList()
}

const fetchEmailList = async () => {
  if (loading.value)
    return;
  loading.value = true
  try {
    const response = await emailApi.getEmailList(emailPage.value, emailLimit.value)
    emailList.value = response.list || [];
    emailTotal.value = response.total || 0
  } catch (error) {
    console.error('获取邮件列表失败:', error)
    ElMessage.error(i18n.global.t('获取邮件列表失败'))
  } finally {
    loading.value = false
  }
}
onMounted(() => {
  fetchEmailList()
  fetchSmtpList()
});

const fetchSmtpList = async () => {
  try {
    const res = await emailApi.getEmailSmtpList()
    smtpList.value = res.list || []
  } catch (error) {
    console.error('获取邮件代理失败:', error)
    ElMessage.error(i18n.global.t('获取邮件代理失败'))
  }
}

const openSend = () => {
  sendForm.to = ''
  sendForm.subject = ''
  sendForm.content = ''
  sendForm.smtpId = ''
  attachmentsText.value = ''
  sendVisible.value = true
  if (!smtpList.value.length)
    fetchSmtpList()
}

const submitSend = async () => {
  if (sendFormRef.value) {
    try {
      await sendFormRef.value.validate()
    } catch {
      return
    }
  }
  submitting.value = true
  try {
    const attachments = attachmentsText.value
      .split(/[\n,]/)
      .map(s => s.trim())
      .filter(Boolean)
    await emailApi.sendEmail({
      to: sendForm.to.split(/[,;\s]+/).map(s => s.trim()).filter(Boolean).join(','),
      subject: sendForm.subject,
      content: sendForm.content,
      attachments,
      smtpId: sendForm.smtpId,
      immediateSend: true
    })
    ElMessage.success('发送成功')
    sendVisible.value = false
    fetchEmailList()
  } catch (e) {
    console.error(e)
    ElMessage.error('发送失败')
  } finally {
    submitting.value = false
  }
}

const openTrace = async (row) => {
  traceVisible.value = true
  traceLoading.value = true
  traceData.value = []
  try {
    const res = await emailApi.getEmailTrace(row.id)
    traceData.value = Array.isArray(res) ? res : (res.list || [])
  } catch (e) {
    console.error(e)
    ElMessage.error('获取追踪失败')
  } finally {
    traceLoading.value = false
  }
}

const handleDelete = async (row) => {
  try {
    await ElMessageBox.confirm('确认删除该邮件记录？', '提示', { type: 'warning' })
  } catch {
    return
  }
  try {
    await emailApi.deleteEmailList(row.id)
    ElMessage.success('删除成功')
    fetchEmailList()
  } catch (e) {
    console.error(e)
    ElMessage.error('删除失败')
  }
}

</script>

<style lang="scss" scoped>
.email-list {
  padding: 20px;
  
  .page-header {
    margin-bottom: 20px;
    
    h1 {
      font-size: 24px;
      color: #303133;
      margin: 0;
    }
  }
  
  .table-container {
    background: #fff;
    border-radius: 4px;
    padding: 20px;
    box-shadow: 0 1px 4px rgba(0, 0, 0, 0.1);
  }
  
  .pagination-container {
    margin-top: 20px;
    display: flex;
    justify-content: center;
  }
}
</style>