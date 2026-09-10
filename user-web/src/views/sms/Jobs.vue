<template>
  <div class="sms-jobs">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>{{ $t('短信任务') }}</span>
          <el-button type="primary" @click="handleCreateJob" :loading="loading">{{ $t('创建任务') }}</el-button>
        </div>
      </template>
      
      <el-table :data="jobList" style="width: 100%" v-loading="loading">
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="name" label="任务名称" />
        <el-table-column prop="total" label="总数" width="100" />
        <el-table-column prop="sent" label="已发送" width="100" />
        <el-table-column prop="failed" label="失败数" width="100" />
        <el-table-column prop="status" label="状态" width="100">
          <template #default="scope">
            <el-tag :type="getStatusType(scope.row.status)">
              {{ getStatusText(scope.row.status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="scheduleTime" label="计划发送时间" width="180">
          <template #default="scope">
            {{ scope.row.scheduleTime || '-' }}
          </template>
        </el-table-column>
        <el-table-column prop="createdAt" label="创建时间" width="180" />
        <el-table-column label="操作" width="250">
          <template #default="scope">
            <el-button size="small" @click="handleView(scope.row)">查看</el-button>
            <el-button size="small" type="warning" @click="handlePause(scope.row)" v-if="scope.row.status === 'running'">暂停</el-button>
            <el-button size="small" type="success" @click="handleResume(scope.row)" v-if="scope.row.status === 'paused'">继续</el-button>
            <el-button size="small" type="danger" @click="handleStop(scope.row)" v-if="scope.row.status === 'running' || scope.row.status === 'paused'">停止</el-button>
            <el-button size="small" type="danger" @click="handleDelete(scope.row)" v-if="scope.row.status === 'completed' || scope.row.status === 'failed'">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
      
      <div class="pagination">
        <el-pagination
          v-model:current-page="currentPage"
          v-model:page-size="pageSize"
          :page-sizes="[10, 20, 50, 100]"
          layout="total, sizes, prev, pager, next, jumper"
          :total="total"
          @size-change="handleSizeChange"
          @current-change="handleCurrentChange"
        />
      </div>
    </el-card>

    
    <el-dialog v-model="dialogVisible" title="创建任务" width="600px" :before-close="handleDialogClose">
      <el-form :model="formData" label-width="100px" :rules="rules" ref="formRef">
        <el-form-item label="任务名称" prop="name">
          <el-input v-model="formData.name" placeholder="请输入任务名称" maxlength="100" show-word-limit />
        </el-form-item>
        
        <el-form-item label="选择草稿" prop="draftId">
          <el-select v-model="formData.draftId" placeholder="请选择草稿">
            <el-option
              v-for="draft in draftOptions"
              :key="draft.id"
              :label="draft.title"
              :value="draft.id"
            />
          </el-select>
        </el-form-item>
        
        <el-form-item label="内容预览" v-if="selectedDraft">
          <div class="content-preview">{{ selectedDraft.content }}</div>
        </el-form-item>
        
        <el-form-item label="接收人群" prop="phoneList">
          <div class="phone-input-section">
            <el-input
              v-model="tempPhone"
              placeholder="请输入手机号"
              maxlength="11"
              @keyup.enter="addPhone"
            >
              <template #append>
                <el-button @click="addPhone">添加</el-button>
              </template>
            </el-input>
          </div>
          
          <div class="phone-list">
            <el-tag
              v-for="(phone, index) in formData.phoneList"
              :key="index"
              closable
              @close="removePhone(index)"
            >
              {{ phone }}
            </el-tag>
          </div>
          
          <div class="phone-tips">
            <input
              ref="fileInputRef"
              type="file"
              accept=".csv,.txt"
              style="display: none"
              @change="handleFileChange"
            />
            <el-button size="small" type="link" @click="handleBatchUpload">批量上传</el-button>
            <span class="tips-text">或从线索库选择</span>
          </div>
        </el-form-item>
        
        <el-form-item label="发送方式">
          <el-radio v-model="formData.sendType" label="immediate">立即发送</el-radio>
          <el-radio v-model="formData.sendType" label="schedule">定时发送</el-radio>
        </el-form-item>
        
        <el-form-item label="定时时间" v-if="formData.sendType === 'schedule'" prop="scheduleTime">
          <el-date-picker
            v-model="formData.scheduleTime"
            type="datetime"
            placeholder="选择发送时间"
            value-format="YYYY-MM-DD HH:mm:ss"
            :disabled-date="disabledBeforeToday"
          />
        </el-form-item>
      </el-form>
      
      <template #footer>
        <span class="dialog-footer">
          <el-button @click="handleDialogClose">取消</el-button>
          <el-button type="primary" @click="handleSubmit">确定</el-button>
        </span>
      </template>
    </el-dialog>

    
    <el-dialog v-model="detailVisible" title="任务详情" width="700px">
      <el-descriptions :column="1" border>
        <el-descriptions-item label="任务名称">{{ currentJob?.name }}</el-descriptions-item>
        <el-descriptions-item label="总数">{{ currentJob?.total }}</el-descriptions-item>
        <el-descriptions-item label="已发送">{{ currentJob?.sent }}</el-descriptions-item>
        <el-descriptions-item label="失败数">{{ currentJob?.failed }}</el-descriptions-item>
        <el-descriptions-item label="状态">
          <el-tag :type="getStatusType(currentJob?.status)">
            {{ getStatusText(currentJob?.status) }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="计划发送时间">{{ currentJob?.scheduleTime || '-' }}</el-descriptions-item>
        <el-descriptions-item label="创建时间">{{ currentJob?.createdAt }}</el-descriptions-item>
      </el-descriptions>
      
      <div class="job-detail-list" v-if="jobDetails.length > 0">
        <h3>发送记录</h3>
        <el-table :data="jobDetails" style="width: 100%">
          <el-table-column prop="phone" label="手机号" width="150" />
          <el-table-column prop="status" label="状态" width="100">
            <template #default="scope">
              <el-tag :type="getStatusType(scope.row.status)">
                {{ getStatusText(scope.row.status) }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="sendTime" label="发送时间" width="180" />
          <el-table-column prop="errorMsg" label="错误信息" show-overflow-tooltip />
        </el-table>
      </div>
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import smsApi from '@/api/sms'
import { getOrderStatusLabel, getOrderStatusTagType } from '@/constants/orderStatus';
import DOMPurify from 'dompurify'
import { toList } from '@/utils/list'

const jobList = ref([]);
const currentPage = ref(1)
const pageSize = ref(10)
const total = ref(0)
const loading = ref(false)

const dialogVisible = ref(false);
const detailVisible = ref(false)
const formRef = ref(null)
const currentJob = ref(null)
const jobDetails = ref([])

const draftOptions = ref([]);
const selectedDraft = computed(() => {
  return draftOptions.value.find(draft => draft.id === formData.draftId)
})

const formData = reactive({
  name: '',
  draftId: '',
  phoneList: [],
  sendType: 'immediate',
  scheduleTime: null
});

const tempPhone = ref('');

const rules = {
  name: [
    { required: true, message: i18n.global.t('请输入任务名称'), trigger: 'blur' },
    { min: 1, max: 100, message: i18n.global.t('任务名称长度在 1 到 100 个字符'), trigger: 'blur' }
  ],
  draftId: [
    { required: true, message: i18n.global.t('请选择草稿'), trigger: 'blur' }
  ],
  phoneList: [
    { required: true, message: i18n.global.t('请至少添加一个手机号'), trigger: 'blur' },
    { min: 1, message: i18n.global.t('请至少添加一个手机号'), trigger: 'blur' }
  ],
  scheduleTime: [
    { required: true, message: i18n.global.t('请选择定时发送时间'), trigger: 'change' }
  ]
};

const getStatusType = (status) => {
  const statusMap = {
    pending: 'info',
    running: 'success',
    paused: 'warning',
    completed: 'success',
    failed: 'danger'
  }
  return statusMap[status] || 'info'
};

const getStatusText = (status) => {
  const statusMap = {
    pending: '待执行',
    running: '执行中',
    paused: '已暂停',
    completed: '已完成',
    failed: '执行失败'
  }
  return statusMap[status] || '未知'
};

const disabledBeforeToday = (time) => {
  return time.getTime() < Date.now() - 8.64e7;
};

const getDraftOptions = async () => {
  try {
    const response = await smsApi.getDraftList({ page: 1, limit: 100 })
    draftOptions.value = response.list || [];
  } catch (error) {
    ElMessage.error(i18n.global.t('获取草稿列表失败'))
  }
};

const getJobList = async () => {
  loading.value = true
  try {
    const params = {
      page: currentPage.value,
      limit: pageSize.value
    }
    const response = await smsApi.getJobList(params)
    jobList.value = response.list || [];
    total.value = response.total || 0
  } catch (error) {
    ElMessage.error(i18n.global.t('获取任务列表失败'))
  } finally {
    loading.value = false
  }
};

const resetForm = () => {
  if (formRef.value) {
    formRef.value.resetFields()
  }
  formData.name = ''
  formData.draftId = ''
  formData.phoneList = []
  formData.sendType = 'immediate'
  formData.scheduleTime = null
  tempPhone.value = ''
};

const addPhone = () => {
  const phone = tempPhone.value.trim()
  if (!phone) {
    ElMessage.warning(i18n.global.t('请输入手机号'))
    return
  }
  
  if (!/^1[3-9]\d{9}$/.test(phone)) {
    ElMessage.warning(i18n.global.t('请输入正确的手机号'))
    return
  }
  
  if (formData.phoneList.includes(phone)) {
    ElMessage.warning(i18n.global.t('手机号已存在'))
    return
  }
  
  formData.phoneList.push(phone)
  tempPhone.value = ''
};

const removePhone = (index) => {
  formData.phoneList.splice(index, 1)
};

const fileInputRef = ref(null);

const handleBatchUpload = () => {
  fileInputRef.value?.click()
};

const handleFileChange = (event) => {
  const file = event.target.files?.[0]
  if (!file) return

  const reader = new FileReader()
  reader.onload = (e) => {
    const content = e.target?.result
    if (typeof content !== 'string') return

    const rawNumbers = content.split(/[\r\n,;]+/).map(s => s.trim()).filter(Boolean);

    const validPhones = []
    const invalidPhones = []
    const duplicatePhones = []
    const existingSet = new Set(formData.phoneList)

    for (const raw of rawNumbers) {
      if (!/^1[3-9]\d{9}$/.test(raw)) {
        invalidPhones.push(raw)
        continue
      }
      if (existingSet.has(raw) || validPhones.includes(raw)) {
        duplicatePhones.push(raw)
        continue
      }
      validPhones.push(raw)
      existingSet.add(raw)
    }

    if (validPhones.length > 0) {
      formData.phoneList.push(...validPhones)
    }

    const messages = []
    if (validPhones.length > 0) {
      messages.push(`成功导入 ${validPhones.length} 个手机号`)
    }
    if (duplicatePhones.length > 0) {
      messages.push(`已去除 ${duplicatePhones.length} 个重复号码`)
    }
    if (invalidPhones.length > 0) {
      const preview = invalidPhones.slice(0, 5).join(', ')
      const suffix = invalidPhones.length > 5 ? `... 等共 ${invalidPhones.length} 个` : ''
      messages.push(`发现 ${invalidPhones.length} 个无效号码: ${preview}${suffix}`)
    }

    if (messages.length > 0) {
      ElMessageBox.alert(DOMPurify.sanitize(messages.join('<br>')), '批量导入结果', {
        dangerouslyUseHTMLString: true
      })
    } else {
      ElMessage.warning(i18n.global.t('文件中未找到有效手机号'))
    }
  }
  reader.readAsText(file)

  event.target.value = '';
};

const handleCreateJob = () => {
  resetForm()
  getDraftOptions()
  dialogVisible.value = true
};

const handleDialogClose = () => {
  resetForm()
  dialogVisible.value = false
};

const handleSubmit = async () => {
  try {
    if (!formRef.value) return
    await formRef.value.validate()
    
    const jobData = {
      name: formData.name,
      phoneList: formData.phoneList,
      content: selectedDraft.value.content
    }
    
    if (formData.sendType === 'schedule' && formData.scheduleTime) {
      jobData.scheduleTime = formData.scheduleTime
    }
    
    await smsApi.createJob(jobData)
    ElMessage.success(i18n.global.t('任务创建成功'))
    
    dialogVisible.value = false
    getJobList()
  } catch (error) {
    if (error !== false) {
      ElMessage.error('任务创建失败：' + (error.message || '未知错误'))
    }
  }
};

const handleView = async (row) => {
  try {
    const response = await smsApi.getJobDetail(row.id)
    currentJob.value = response
    try {
      const recordsRes = await smsApi.getJobRecords(row.id, { page: 1, page_size: 50 })
      const recordsData = toList(recordsRes)
      jobDetails.value = Array.isArray(recordsData) ? recordsData : []
    } catch (e) {
      jobDetails.value = []
    }
    detailVisible.value = true
  } catch (error) {
    ElMessage.error(i18n.global.t('获取任务详情失败'))
  }
};

const handlePause = async (row) => {
  try {
    await smsApi.pauseJob(row.id)
    ElMessage.success(i18n.global.t('任务暂停成功'))
    getJobList()
  } catch (error) {
    ElMessage.error(i18n.global.t('任务暂停失败'))
  }
};

const handleResume = async (row) => {
  try {
    await smsApi.resumeJob(row.id)
    ElMessage.success(i18n.global.t('任务继续成功'))
    getJobList()
  } catch (error) {
    ElMessage.error(i18n.global.t('任务继续失败'))
  }
};

const handleStop = async (row) => {
  try {
    await ElMessageBox.confirm('确定要停止该任务吗？', '确认停止', {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      type: 'warning'
    })
    await smsApi.stopJob(row.id)
    ElMessage.success(i18n.global.t('任务停止成功'))
    getJobList()
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error(i18n.global.t('任务停止失败'))
    }
  }
};

const handleDelete = async (row) => {
  try {
    await ElMessageBox.confirm('确定要删除该任务吗？', '确认删除', {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      type: 'warning'
    })
    await smsApi.deleteJob(row.id)
    ElMessage.success(i18n.global.t('任务删除成功'))
    getJobList()
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error(i18n.global.t('任务删除失败'))
    }
  }
};

const handleSizeChange = (val) => {
  pageSize.value = val
  getJobList()
};

const handleCurrentChange = (val) => {
  currentPage.value = val
  getJobList()
};

onMounted(() => {
  getJobList()
})
</script>

<style scoped>
.sms-jobs {
  padding: 20px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.pagination {
  margin-top: 20px;
  display: flex;
  justify-content: center;
}

.content-preview {
  background-color: #f5f7fa;
  padding: 10px;
  border-radius: 4px;
  min-height: 60px;
  white-space: pre-wrap;
  word-break: break-word;
  color: #606266;
}

.phone-input-section {
  margin-bottom: 10px;
}

.phone-list {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-bottom: 10px;
  max-height: 150px;
  overflow-y: auto;
  padding: 10px;
  background-color: #f5f7fa;
  border-radius: 4px;
}

.phone-tips {
  display: flex;
  align-items: center;
  gap: 10px;
  color: #909399;
  font-size: 14px;
}

.tips-text {
  color: #909399;
}

.job-detail-list {
  margin-top: 20px;
}

.job-detail-list h3 {
  margin-bottom: 10px;
  font-size: 16px;
  color: #303133;
}
</style>