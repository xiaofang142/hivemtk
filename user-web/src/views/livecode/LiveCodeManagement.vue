<template>
  <div class="livecode-management">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>{{ $t('活码管理') }}</span>
          <el-button
            type="primary"
            @click="handleAdd"
          >
            {{ $t('新增活码') }}
          </el-button>
        </div>
      </template>
      
      
      <div class="search-bar">
        <el-form
          :inline="true"
          :model="searchForm"
          class="search-form"
        >
          <el-form-item label="活码名称">
            <el-input
              v-model="searchForm.name"
              placeholder="请输入活码名称"
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
                label="启用"
                :value="1"
              />
              <el-option
                label="禁用"
                :value="0"
              />
            </el-select>
          </el-form-item>
          <el-form-item>
            <el-button
              type="primary"
              @click="handleSearch"
            >
              查询
            </el-button>
            <el-button @click="resetSearch">
              重置
            </el-button>
          </el-form-item>
        </el-form>
      </div>
      
      
      <el-table
        v-loading="loading"
        :data="liveCodeList"
        style="width: 100%"
      >
        <el-table-column
          prop="id"
          label="ID"
          width="80"
        />
        <el-table-column
          prop="name"
          label="活码名称"
        />
        <el-table-column
          prop="short_link"
          label="短链"
        />
        <el-table-column
          prop="full_short_link"
          label="完整短链"
          min-width="200"
        >
          <template #default="scope">
            <el-link
              :href="scope.row.full_short_link"
              target="_blank"
              type="primary"
            >
              {{ scope.row.full_short_link }}
            </el-link>
          </template>
        </el-table-column>
        <el-table-column
          prop="total_views"
          label="总访问量"
          width="100"
        />
        <el-table-column
          prop="today_views"
          label="今日访问"
          width="100"
        />
        <el-table-column
          prop="status"
          label="状态"
          width="100"
        >
          <template #default="scope">
            <el-tag :type="scope.row.status === 1 ? 'success' : 'danger'">
              {{ scope.row.status === 1 ? '启用' : '禁用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="created_at"
          label="创建时间"
          width="180">
          <template #default="{ row }">
            {{ fmtTime(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="300"
          fixed="right"
        >
          <template #default="scope">
            <el-button
              type="primary"
              size="small"
              @click="handleEdit(scope.row)"
            >
              编辑
            </el-button>
            <el-button
              type="success"
              size="small"
              @click="handleViewQR(scope.row)"
            >
              二维码
            </el-button>
            <el-button
              type="info"
              size="small"
              @click="handleStats(scope.row)"
            >
              统计
            </el-button>
            <el-button
              type="warning"
              size="small"
              @click="handleShare(scope.row)"
            >
              分享
            </el-button>
            <el-button
              type="danger"
              size="small"
              @click="handleDelete(scope.row)"
            >
              删除
            </el-button>
          </template>
        </el-table-column>
      </el-table>
      
      
      <div class="pagination">
        <el-pagination
          v-model:current-page="pagination.page"
          v-model:page-size="pagination.pageSize"
          :page-sizes="[10, 20, 50, 100]"
          layout="total, sizes, prev, pager, next, jumper"
          :total="pagination.total"
          @size-change="handleSizeChange"
          @current-change="handleCurrentChange"
        />
      </div>
    </el-card>
    
    
    <el-dialog
      v-model="dialogVisible"
      :title="dialogTitle"
      width="600px"
      @close="handleDialogClose"
    >
      <el-form
        ref="liveCodeFormRef"
        :model="liveCodeForm"
        :rules="liveCodeRules"
        label-width="120px"
      >
        <el-form-item
          label="活码名称"
          prop="name"
        >
          <el-input v-model="liveCodeForm.name" />
        </el-form-item>
        <el-form-item
          label="短链"
          prop="short_link"
        >
          <el-input v-model="liveCodeForm.short_link" />
        </el-form-item>
        <el-form-item
          label="短链域名"
          prop="short_domain_id"
        >
          <!-- 原来这里是裸的 el-input-number（默认 1），用户既不知道 1 是谁、也不知道该填什么，
               提交后只会拿到「活码域名不可用: 短链域名 1」。改成从域名池选可用域名。 -->
          <el-select
            v-model="liveCodeForm.short_domain_id"
            filterable
            placeholder="选择短链域名"
            style="width: 100%"
          >
            <el-option
              v-for="d in domainOptions"
              :key="d.id"
              :label="domainLabel(d)"
              :value="d.id"
              :disabled="d.status !== 1"
            />
          </el-select>
        </el-form-item>
        <el-form-item
          label="入口域名"
          prop="entry_domain_id"
        >
          <!-- 原来这里是裸的 el-input-number（默认 1），用户既不知道 1 是谁、也不知道该填什么，
               提交后只会拿到「活码域名不可用: 短链域名 1」。改成从域名池选可用域名。 -->
          <el-select
            v-model="liveCodeForm.entry_domain_id"
            filterable
            placeholder="选择入口域名"
            style="width: 100%"
          >
            <el-option
              v-for="d in domainOptions"
              :key="d.id"
              :label="domainLabel(d)"
              :value="d.id"
              :disabled="d.status !== 1"
            />
          </el-select>
        </el-form-item>
        <el-form-item
          label="落地域名"
          prop="landing_domain_id"
        >
          <!-- 原来这里是裸的 el-input-number（默认 1），用户既不知道 1 是谁、也不知道该填什么，
               提交后只会拿到「活码域名不可用: 短链域名 1」。改成从域名池选可用域名。 -->
          <el-select
            v-model="liveCodeForm.landing_domain_id"
            filterable
            placeholder="选择落地域名"
            style="width: 100%"
          >
            <el-option
              v-for="d in domainOptions"
              :key="d.id"
              :label="domainLabel(d)"
              :value="d.id"
              :disabled="d.status !== 1"
            />
          </el-select>
        </el-form-item>
        <el-form-item
          label="图片URL"
          prop="image_url"
        >
          <el-input v-model="liveCodeForm.image_url" />
        </el-form-item>
        <el-form-item
          label="入口URL"
          prop="entry_url"
        >
          <el-input v-model="liveCodeForm.entry_url" />
        </el-form-item>
        <el-form-item
          label="落地URL"
          prop="landing_url"
        >
          <el-input v-model="liveCodeForm.landing_url" />
        </el-form-item>
        <el-form-item
          label="状态"
          prop="status"
        >
          <el-radio-group v-model="liveCodeForm.status">
            <el-radio :label="1">
              启用
            </el-radio>
            <el-radio :label="0">
              禁用
            </el-radio>
          </el-radio-group>
        </el-form-item>
      </el-form>
      
      <template #footer>
        <span class="dialog-footer">
          <el-button @click="dialogVisible = false">取消</el-button>
          <el-button
            type="primary"
            :loading="submitLoading"
            @click="handleSubmit"
          >
            确定
          </el-button>
        </span>
      </template>
    </el-dialog>
    
    
    <el-dialog
      v-model="qrDialogVisible"
      title="二维码管理"
      width="800px"
    >
      <div class="qr-header">
        <el-button
          type="primary"
          @click="handleAddQR"
        >
          新增二维码
        </el-button>
      </div>
      
      <el-table
        v-loading="qrLoading"
        :data="qrList"
        style="width: 100%"
      >
        <el-table-column
          prop="id"
          label="ID"
          width="80"
        />
        <el-table-column
          prop="daily_limit"
          label="每日限制"
          width="100"
        >
          <template #default="scope">
            {{ scope.row.daily_limit }}次
          </template>
        </el-table-column>
        <el-table-column
          prop="status"
          label="状态"
          width="100"
        >
          <template #default="scope">
            <el-tag :type="scope.row.status === 1 ? 'success' : 'danger'">
              {{ scope.row.status === 1 ? '启用' : '禁用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="created_at"
          label="创建时间"
          width="180">
          <template #default="{ row }">
            {{ fmtTime(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="200"
          fixed="right"
        >
          <template #default="scope">
            <el-button
              type="primary"
              size="small"
              @click="handleEditQR(scope.row)"
            >
              编辑
            </el-button>
            <el-button
              type="danger"
              size="small"
              @click="handleDeleteQR(scope.row)"
            >
              删除
            </el-button>
          </template>
        </el-table-column>
      </el-table>
      
      <template #footer>
        <span class="dialog-footer">
          <el-button @click="qrDialogVisible = false">关闭</el-button>
        </span>
      </template>
    </el-dialog>
    
    
    <el-dialog
      v-model="qrFormVisible"
      :title="qrFormTitle"
      width="500px"
      @close="handleQRFormClose"
    >
      <el-form
        ref="qrFormRef"
        :model="qrForm"
        :rules="qrRules"
        label-width="120px"
      >
        <el-form-item
          label="过期天数"
          prop="expire_days"
        >
          <!-- 后端 GenerateQRCodeRequest.ExpireDays 是 required；
               之前表单没有这个字段 ⇒ 每次生成二维码都弹 Go validator 原文。 -->
          <el-input-number
            v-model="qrForm.expire_days"
            :min="1"
            :max="3650"
            controls-position="right"
          />
          <span style="margin-left: 10px;">天</span>
        </el-form-item>
        <el-form-item
          label="每日限制"
          prop="daily_limit"
        >
          <el-input-number 
            v-model="qrForm.daily_limit" 
            :min="50" 
            :max="500" 
            :step="50"
            controls-position="right"
          />
          <span style="margin-left: 10px;">次</span>
        </el-form-item>
        <el-form-item
          label="状态"
          prop="status"
        >
          <el-radio-group v-model="qrForm.status">
            <el-radio :label="1">
              启用
            </el-radio>
            <el-radio :label="0">
              禁用
            </el-radio>
          </el-radio-group>
        </el-form-item>
      </el-form>
      
      <template #footer>
        <span class="dialog-footer">
          <el-button @click="qrFormVisible = false">取消</el-button>
          <el-button
            type="primary"
            :loading="qrSubmitLoading"
            @click="handleQRSubmit"
          >
            确定
          </el-button>
        </span>
      </template>
    </el-dialog>
    
    
    <el-dialog
      v-model="statsDialogVisible"
      title="活码统计"
      width="800px"
    >
      <div class="stats-container">
        <el-row :gutter="20">
          <el-col :span="6">
            <el-card class="stats-card">
              <div class="stats-item">
                <div class="stats-value">
                  {{ statsData.total_views || 0 }}
                </div>
                <div class="stats-label">
                  总浏览量
                </div>
              </div>
            </el-card>
          </el-col>
          <el-col :span="6">
            <el-card class="stats-card">
              <div class="stats-item">
                <div class="stats-value">
                  {{ statsData.today_views || 0 }}
                </div>
                <div class="stats-label">
                  今日浏览
                </div>
              </div>
            </el-card>
          </el-col>
          <el-col :span="6">
            <el-card class="stats-card">
              <div class="stats-item">
                <div class="stats-value">
                  {{ statsData.active_qr_count || 0 }}
                </div>
                <div class="stats-label">
                  活跃二维码
                </div>
              </div>
            </el-card>
          </el-col>
          <el-col :span="6">
            <el-card class="stats-card">
              <div class="stats-item">
                <div class="stats-value">
                  {{ statsData.total_qr_count || 0 }}
                </div>
                <div class="stats-label">
                  总二维码数
                </div>
              </div>
            </el-card>
          </el-col>
        </el-row>
        
        <!-- 接口 LiveCodeStatsResponse 不含日趋势字段，原来这里放了个永远空白的
             #statsChart（从未 init echarts）。改为展示点击/二维码相关计数 + 转化率说明。 -->
        <el-row
          :gutter="16"
          style="margin-top: 16px;"
        >
          <el-col
            v-for="item in statsSecondary"
            :key="item.label"
            :span="6"
          >
            <el-card class="stats-card">
              <div class="stats-item">
                <div class="stats-value">
                  {{ item.value }}
                </div>
                <div class="stats-label">
                  {{ item.label }}
                </div>
              </div>
            </el-card>
          </el-col>
        </el-row>
        <div class="stats-note">
          转化率 {{ Number(statsData.conversion_rate || 0).toFixed(1) }}%（二维码点击 / 二维码展示）；接口暂不提供日趋势，趋势图暂未接入。
        </div>
      </div>
      
      <template #footer>
        <span class="dialog-footer">
          <el-button @click="statsDialogVisible = false">关闭</el-button>
        </span>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'
import { isUserReportedError } from '@/utils/errorReporting'

import { ref, reactive, onMounted, nextTick, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { 
  getLiveCodes, 
  createLiveCode, 
  updateLiveCode, 
  deleteLiveCode, 
  getLiveCodeStats,
  getLiveCodeQRs,
  generateLiveCodeQR,
  deleteLiveCodeQR,
  updateLiveCodeQR,
  shareLiveCode
} from '@/api/livecode'
import { copyToClipboard } from '@/utils/clipboard'
import { fmtTime } from '@/utils/time'
import { domainPoolApi } from '@/api/domainPool'

const loading = ref(false);
const submitLoading = ref(false)
const qrLoading = ref(false)
const qrSubmitLoading = ref(false)
const liveCodeList = ref([])
const qrList = ref([])
const dialogVisible = ref(false)
const qrDialogVisible = ref(false)
const qrFormVisible = ref(false)
const statsDialogVisible = ref(false)
const dialogTitle = ref('')
const qrFormTitle = ref('')
const liveCodeFormRef = ref()
const qrFormRef = ref()
const currentLiveCodeId = ref(null)
// 域名池候选：活码写入时要的是 domain_pool.id，之前让人手填数字 ⇒ 必然踩「域名不可用」。
const domainOptions = ref([])
const domainLabel = (d) => `${d.domain}（#${d.id}）${d.status === 1 ? '' : ' · 已停用'}`
const defaultDomainId = () => {
  const first = domainOptions.value.find((d) => d.status === 1)
  return first ? first.id : null
}
const loadDomainOptions = async () => {
  try {
    const res = await domainPoolApi.getList({ page: 1, page_size: 200 })
    domainOptions.value = (res?.list || []).map((d) => ({ id: d.id, domain: d.domain, status: d.status }))
  } catch (error) {
    domainOptions.value = []
    if (!isUserReportedError(error)) ElMessage.error('获取可用域名失败')
  }
}

const statsData = ref({})

// 弹窗下半部分的次级指标（总点击 / 今日点击 / 二维码展示 / 二维码点击）
const statsSecondary = computed(() => [
  { label: '总点击', value: statsData.value.total_clicks || 0 },
  { label: '今日点击', value: statsData.value.today_clicks || 0 },
  { label: '二维码展示', value: statsData.value.total_qr_shown || 0 },
  { label: '二维码点击', value: statsData.value.total_qr_clicks || 0 },
])

const searchForm = reactive({
  name: '',
  status: ''
});

const pagination = reactive({
  page: 1,
  pageSize: 10,
  total: 0
});

const liveCodeForm = reactive({
  id: null,
  name: '',
  short_link: '',
  short_domain_id: null,
  entry_domain_id: null,
  landing_domain_id: null,
  status: 1,
  image_url: '',
  entry_url: '',
  landing_url: ''
});

const qrForm = reactive({
  id: null,
  expire_days: 30,
  daily_limit: 200,
  status: 1
});

const liveCodeRules = {
  name: [
    { required: true, message: i18n.global.t('请输入活码名称'), trigger: 'blur' },
    { min: 2, max: 50, message: i18n.global.t('活码名称长度在 2 到 50 个字符'), trigger: 'blur' }
  ],
  short_link: [
    { required: true, message: i18n.global.t('请输入短链'), trigger: 'blur' }
  ],
  short_domain_id: [
    { required: true, message: i18n.global.t('请输入短链域名ID'), trigger: 'blur', type: 'number' }
  ],
  entry_domain_id: [
    { required: true, message: i18n.global.t('请输入入口域名ID'), trigger: 'blur', type: 'number' }
  ],
  landing_domain_id: [
    { required: true, message: i18n.global.t('请输入落地域名ID'), trigger: 'blur', type: 'number' }
  ]
};

const qrRules = {
  expire_days: [
    { required: true, message: '请输入过期天数', trigger: 'blur', type: 'number' }
  ],
  daily_limit: [
    { required: true, message: i18n.global.t('请输入每日限制'), trigger: 'blur', type: 'number' }
  ]
};



const fetchLiveCodeList = async () => {
  loading.value = true
  try {
    const params = {
      page: pagination.page,
      pageSize: pagination.pageSize
    }
    
    if (searchForm.name) {
      params.name = searchForm.name
    }
    if (searchForm.status !== '') {
      params.status = searchForm.status
    }
    
    const response = await getLiveCodes(params)
    liveCodeList.value = response.list
    pagination.total = response.total
  } catch (error) {
    if (!isUserReportedError(error)) ElMessage.error(i18n.global.t('获取活码列表失败'))
  } finally {
    loading.value = false
  }
};

const fetchQRList = async (liveCodeId) => {
  qrLoading.value = true
  try {
    const response = await getLiveCodeQRs(liveCodeId)
    qrList.value = response
  } catch (error) {
    if (!isUserReportedError(error)) ElMessage.error(i18n.global.t('获取二维码列表失败'))
  } finally {
    qrLoading.value = false
  }
};

const handleSearch = () => {
  pagination.page = 1
  fetchLiveCodeList()
};

const resetSearch = () => {
  searchForm.name = ''
  searchForm.status = ''
  pagination.page = 1
  fetchLiveCodeList()
};

const handleSizeChange = (val) => {
  pagination.pageSize = val
  fetchLiveCodeList()
};

const handleCurrentChange = (val) => {
  pagination.page = val
  fetchLiveCodeList()
};

const handleAdd = () => {
  dialogTitle.value = '新增活码'
  dialogVisible.value = true
  
  Object.assign(liveCodeForm, {
    id: null,
    name: '',
    short_link: '',
    short_domain_id: defaultDomainId(),
    entry_domain_id: defaultDomainId(),
    landing_domain_id: defaultDomainId(),
    status: 1,
    image_url: '',
    entry_url: '',
    landing_url: ''
  });
};

const handleEdit = (row) => {
  dialogTitle.value = '编辑活码'
  dialogVisible.value = true
  
  Object.assign(liveCodeForm, {
    id: row.id,
    name: row.name,
    short_link: row.short_link,
    short_domain_id: row.short_domain_id,
    entry_domain_id: row.entry_domain_id,
    landing_domain_id: row.landing_domain_id,
    status: row.status,
    image_url: row.image_url,
    entry_url: row.entry_url,
    landing_url: row.landing_url
  });
};

const handleDelete = (row) => {
  ElMessageBox.confirm(
    `确定要删除活码 "${row.name}" 吗？`,
    '提示',
    {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      type: 'warning'
    }
  )
    .then(async () => {
      try {
        await deleteLiveCode(row.id)
        ElMessage.success(i18n.global.t('删除成功'))
        fetchLiveCodeList()
      } catch (error) {
        if (!isUserReportedError(error)) ElMessage.error(i18n.global.t('删除失败'))
      }
    })
    .catch(() => {})
};

const handleViewQR = (row) => {
  currentLiveCodeId.value = row.id
  qrDialogVisible.value = true
  fetchQRList(row.id)
};

const handleAddQR = () => {
  qrFormTitle.value = '新增二维码'
  qrFormVisible.value = true
  
  Object.assign(qrForm, {
    id: null,
    daily_limit: 200,
    status: 1
  });
};

const handleEditQR = (row) => {
  qrFormTitle.value = '编辑二维码'
  qrFormVisible.value = true
  
  Object.assign(qrForm, {
    id: row.id,
    daily_limit: row.daily_limit,
    status: row.status
  });
};

const handleDeleteQR = (row) => {
  ElMessageBox.confirm(
    `确定要删除二维码 #${row.id} 吗？`,
    '提示',
    {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      type: 'warning'
    }
  )
    .then(async () => {
      try {
        await deleteLiveCodeQR(row.id)
        ElMessage.success(i18n.global.t('删除成功'))
        fetchQRList(currentLiveCodeId.value)
      } catch (error) {
        if (!isUserReportedError(error)) ElMessage.error(i18n.global.t('删除失败'))
      }
    })
    .catch(() => {})
};

const handleStats = async (row) => {
  try {
    const response = await getLiveCodeStats(row.id)
    statsData.value = response
    statsDialogVisible.value = true;
  } catch (error) {
    if (!isUserReportedError(error)) ElMessage.error(i18n.global.t('获取统计数据失败'))
  }
};

const handleShare = async (row) => {
  try {
    const response = await shareLiveCode(row.id, {})
    const shareUrl = response.share_url;

    if (await copyToClipboard(shareUrl)) {
      ElMessage.success(i18n.global.t('分享链接已复制到剪贴板'))
    } else {
      ElMessage.warning(i18n.global.t('复制失败，请手动复制'))
    }
  } catch (error) {
    if (!isUserReportedError(error)) ElMessage.error(i18n.global.t('分享失败'))
  }
};

const handleSubmit = async () => {
  if (!liveCodeFormRef.value) return
  
  try {
    await liveCodeFormRef.value.validate()
    submitLoading.value = true
    
    if (liveCodeForm.id) {
      await updateLiveCode(liveCodeForm.id, liveCodeForm);
      ElMessage.success(i18n.global.t('更新成功'))
    } else {
      await createLiveCode(liveCodeForm);
      ElMessage.success(i18n.global.t('创建成功'))
    }
    
    dialogVisible.value = false
    fetchLiveCodeList()
  } catch (error) {
    if (error.message) {
      if (!isUserReportedError(error)) ElMessage.error(error.message)
    }
  } finally {
    submitLoading.value = false
  }
};

const handleQRSubmit = async () => {
  if (!qrFormRef.value) return
  
  try {
    await qrFormRef.value.validate()
    qrSubmitLoading.value = true
    
    if (qrForm.id) {
      await updateLiveCodeQR(qrForm.id, qrForm);
      ElMessage.success(i18n.global.t('更新成功'))
    } else {
      await generateLiveCodeQR(currentLiveCodeId.value, qrForm);
      ElMessage.success(i18n.global.t('创建成功'))
    }
    
    qrFormVisible.value = false
    fetchQRList(currentLiveCodeId.value)
  } catch (error) {
    if (error.message) {
      if (!isUserReportedError(error)) ElMessage.error(error.message)
    }
  } finally {
    qrSubmitLoading.value = false
  }
};

const handleDialogClose = () => {
  if (liveCodeFormRef.value) {
    liveCodeFormRef.value.resetFields()
  }
};

const handleQRFormClose = () => {
  if (qrFormRef.value) {
    qrFormRef.value.resetFields()
  }
};

onMounted(() => {
  loadDomainOptions()
  fetchLiveCodeList()
});
</script>

<style scoped>
.livecode-management {
  padding: 20px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.search-bar {
  margin-bottom: 20px;
}

.search-form {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.pagination {
  margin-top: 20px;
  display: flex;
  justify-content: flex-end;
}

.qr-header {
  margin-bottom: 20px;
}

.stats-container {
  padding: 10px 0;
}

.stats-card {
  text-align: center;
}

.stats-item {
  padding: 10px 0;
}

.stats-value {
  font-size: 24px;
  font-weight: bold;
  color: #4F46E5;
  margin-bottom: 5px;
}

.stats-label {
  font-size: 14px;
  color: #909399;
}
.stats-note {
  margin-top: 12px;
  font-size: 12px;
  line-height: 1.5;
  color: var(--el-text-color-secondary);
}
</style>