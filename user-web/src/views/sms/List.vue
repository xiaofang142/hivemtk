<template>
  <div class="sms-list">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>{{ $t('短信列表') }}</span>
          <el-button
            type="primary"
            @click="handleSendSms"
          >
            {{ $t('发送短信') }}
          </el-button>
        </div>
      </template>
      
      <div class="search-form">
        <el-form
          :inline="true"
          :model="searchForm"
        >
          <el-form-item label="手机号">
            <el-input
              v-model="searchForm.phone"
              placeholder="请输入手机号"
              clearable
            />
          </el-form-item>
          <el-form-item label="状态">
            <el-select
              v-model="searchForm.status"
              placeholder="请选择状态"
              clearable
            >
              <el-option
                label="待发送"
                value="pending"
              />
              <el-option
                label="发送中"
                value="sending"
              />
              <el-option
                label="已发送"
                value="sent"
              />
              <el-option
                label="发送失败"
                value="failed"
              />
            </el-select>
          </el-form-item>
          <el-form-item label="发送时间">
            <el-date-picker
              v-model="searchForm.dateRange"
              type="daterange"
              range-separator="至"
              start-placeholder="开始日期"
              end-placeholder="结束日期"
              value-format="YYYY-MM-DD"
            />
          </el-form-item>
          <el-form-item>
            <el-button
              type="primary"
              @click="handleSearch"
            >
              搜索
            </el-button>
            <el-button @click="handleReset">
              重置
            </el-button>
          </el-form-item>
        </el-form>
      </div>
      
      <el-table
        v-loading="loading"
        :data="smsList"
        style="width: 100%"
      >
        <el-table-column
          prop="id"
          label="ID"
          width="80"
        />
        <el-table-column
          prop="phone"
          label="手机号"
        />
        <el-table-column
          prop="content"
          label="短信内容"
          show-overflow-tooltip
        />
        <el-table-column
          prop="status"
          label="状态"
          width="100"
        >
          <template #default="scope">
            <el-tag :type="getStatusType(scope.row.status)">
              {{ getStatusText(scope.row.status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="发送时间"
          width="180"
        >
          <template #default="scope">
            {{ formatTime(scope.row.sendTime) }}
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="150"
        >
          <template #default="scope">
            <el-button
              size="small"
              @click="handleView(scope.row)"
            >
              查看
            </el-button>
            <el-button
              v-if="scope.row.status === 'failed'"
              size="small"
              type="danger"
              @click="handleResend(scope.row)"
            >
              重发
            </el-button>
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

    
    <el-dialog
      v-model="sendDialogVisible"
      title="发送短信"
      width="500px"
    >
      <el-form
        :model="sendForm"
        label-width="80px"
      >
        <el-form-item label="手机号">
          <el-input
            v-model="sendForm.phone"
            placeholder="请输入手机号"
            maxlength="11"
          />
        </el-form-item>
        <el-form-item label="内容">
          <el-input
            v-model="sendForm.content"
            type="textarea"
            :rows="5"
            placeholder="请输入短信内容"
            maxlength="500"
            show-word-limit
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="sendDialogVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          :loading="sending"
          @click="submitSendSms"
        >
          确定发送
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import smsApi from '@/api/sms'
import { isUserReportedError } from '@/utils/errorReporting'
import { getOrderStatusLabel, getOrderStatusTagType } from '@/constants/orderStatus';

// 未发送/失败的短信 sendTime 是 Go 零值（0001-01-01T08:05:43+08:05），直接渲染
// 会显示公元 1 年。零值与非时间一律显示 '-'。
const formatTime = (d) => {
  if (!d) return '-'
  const t = new Date(d)
  if (Number.isNaN(t.getTime()) || t.getUTCFullYear() <= 1) return '-'
  return t.toLocaleString('zh-CN')
}

const searchForm = ref({
  phone: '',
  status: '',
  dateRange: []
});

const smsList = ref([]);
const currentPage = ref(1)
const pageSize = ref(10)
const total = ref(0)
const loading = ref(false)

const getStatusType = (status) => {
  const statusMap = {
    pending: 'info',
    sending: 'warning',
    sent: 'success',
    failed: 'danger'
  }
  return statusMap[status] || 'info'
};

const getStatusText = (status) => {
  const statusMap = {
    pending: '待发送',
    sending: '发送中',
    sent: '已发送',
    failed: '发送失败'
  }
  return statusMap[status] || '未知'
};

const getSmsList = async () => {
  loading.value = true
  try {
    const params = {
      page: currentPage.value,
      limit: pageSize.value,
      phone: searchForm.value.phone,
      status: searchForm.value.status,
      startDate: searchForm.value.dateRange?.[0] || '',
      endDate: searchForm.value.dateRange?.[1] || ''
    }
    const response = await smsApi.getSmsList(params)
    smsList.value = response.list || [];
    total.value = response.total || 0
  } catch (error) {
    if (!isUserReportedError(error)) ElMessage.error(i18n.global.t('获取短信列表失败'))
  } finally {
    loading.value = false
  }
};

const handleSearch = () => {
  currentPage.value = 1
  getSmsList()
};

const handleReset = () => {
  searchForm.value = {
    phone: '',
    status: '',
    dateRange: []
  }
  handleSearch()
};

const handleView = async (row) => {
  try {
    const response = await smsApi.getSmsDetail(row.id)
    ElMessageBox.alert(JSON.stringify(response, null, 2), '短信详情')
  } catch (error) {
    if (!isUserReportedError(error)) ElMessage.error(i18n.global.t('获取短信详情失败'))
  }
};

const handleResend = async (row) => {
  try {
    await smsApi.resendSms(row.id)
    ElMessage.success(i18n.global.t('短信重发成功'))
    getSmsList()
  } catch (error) {
    if (!isUserReportedError(error)) ElMessage.error(i18n.global.t('短信重发失败'))
  }
};

const handleSizeChange = (val) => {
  pageSize.value = val
  getSmsList()
};

const handleCurrentChange = (val) => {
  currentPage.value = val
  getSmsList()
};

onMounted(() => {
  getSmsList()
})

const sendDialogVisible = ref(false);
const sending = ref(false)
const sendForm = reactive({ phone: '', content: '' })

const handleSendSms = () => {
  sendForm.phone = ''
  sendForm.content = ''
  sendDialogVisible.value = true
}

const submitSendSms = async () => {
  if (!/^1[3-9]\d{9}$/.test(sendForm.phone)) {
    ElMessage.warning('请输入正确的手机号')
    return
  }
  if (!sendForm.content.trim()) {
    ElMessage.warning('请输入短信内容')
    return
  }
  sending.value = true
  try {
    await smsApi.sendSms({ phone: sendForm.phone, content: sendForm.content })
    ElMessage.success('短信发送成功')
    sendDialogVisible.value = false
    getSmsList()
  } catch (error) {
    if (!isUserReportedError(error)) ElMessage.error('短信发送失败')
  } finally {
    sending.value = false
  }
}
</script>

<style scoped>
.sms-list {
  padding: 20px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.search-form {
  margin-bottom: 20px;
}

.pagination {
  margin-top: 20px;
  display: flex;
  justify-content: center;
}
</style>