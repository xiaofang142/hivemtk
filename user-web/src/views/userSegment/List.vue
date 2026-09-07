<template>
  <div class="user-segment-page">
    <el-card class="header-card">
      <div class="header-content">
        <h2>用户分群 (RFM)</h2>
        <p class="subtitle">基于 RFM 模型对用户进行分群，识别高价值客户</p>
      </div>
      <div class="header-actions">
        <el-button type="primary" @click="showCreateDialog">
          <el-icon><Plus /></el-icon>
          {{ $t('创建分群') }}
        </el-button>
        <el-button @click="$router.push('/userSegment/builder')">
          <el-icon><SetUp /></el-icon>
          {{ $t('规则构建器') }}
        </el-button>
        <el-button @click="refreshData">
          <el-icon><Refresh /></el-icon>
          {{ $t('刷新') }}
        </el-button>
      </div>
    </el-card>

    
    <el-row :gutter="20" class="overview-row">
      <el-col :xs="12" :sm="6">
        <el-card class="stat-card">
          <div class="stat-label">{{ $t('总用户数') }}</div>
          <div class="stat-value">{{ stats.totalUsers ?? stats.total ?? 0 }}</div>
        </el-card>
      </el-col>
      <el-col :xs="12" :sm="6">
        <el-card class="stat-card">
          <div class="stat-label">{{ $t('高价值客户') }}</div>
          <div class="stat-value" style="color: #10B981">{{ stats.highValue ?? stats.high_value ?? 0 }}</div>
        </el-card>
      </el-col>
      <el-col :xs="12" :sm="6">
        <el-card class="stat-card">
          <div class="stat-label">{{ $t('活跃客户') }}</div>
          <div class="stat-value" style="color: #4F46E5">{{ stats.active ?? stats.active_count ?? 0 }}</div>
        </el-card>
      </el-col>
      <el-col :xs="12" :sm="6">
        <el-card class="stat-card">
          <div class="stat-label">{{ $t('流失风险') }}</div>
          <div class="stat-value" style="color: #EF4444">{{ stats.churnRisk ?? stats.churn_risk ?? 0 }}</div>
        </el-card>
      </el-col>
    </el-row>

    
    <el-card class="matrix-card">
      <template #header>
        <span>RFM 客户分群矩阵</span>
      </template>
      <div class="rfm-matrix">
        <table>
          <thead>
            <tr>
              <th></th>
              <th>高 F (频繁)</th>
              <th>中 F</th>
              <th>低 F (久远)</th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <th>高 M (高额)</th>
              <td class="cell high-value" @click="onCellClick('high_value')">
                <div class="cell-name">重要价值客户</div>
                <div class="cell-count">{{ matrixCounts.high_value || 0 }} 人</div>
              </td>
              <td class="cell good" @click="onCellClick('important_develop')">
                <div class="cell-name">重要发展客户</div>
                <div class="cell-count">{{ matrixCounts.important_develop || 0 }} 人</div>
              </td>
              <td class="cell warn" @click="onCellClick('important_keep')">
                <div class="cell-name">重要保持客户</div>
                <div class="cell-count">{{ matrixCounts.important_keep || 0 }} 人</div>
              </td>
            </tr>
            <tr>
              <th>中 M</th>
              <td class="cell good" @click="onCellClick('general_value')">
                <div class="cell-name">一般价值客户</div>
                <div class="cell-count">{{ matrixCounts.general_value || 0 }} 人</div>
              </td>
              <td class="cell normal" @click="onCellClick('general_develop')">
                <div class="cell-name">一般发展客户</div>
                <div class="cell-count">{{ matrixCounts.general_develop || 0 }} 人</div>
              </td>
              <td class="cell warn" @click="onCellClick('general_keep')">
                <div class="cell-name">一般保持客户</div>
                <div class="cell-count">{{ matrixCounts.general_keep || 0 }} 人</div>
              </td>
            </tr>
            <tr>
              <th>低 M (低额)</th>
              <td class="cell normal" @click="onCellClick('potential_value')">
                <div class="cell-name">潜在价值客户</div>
                <div class="cell-count">{{ matrixCounts.potential_value || 0 }} 人</div>
              </td>
              <td class="cell warn" @click="onCellClick('potential_develop')">
                <div class="cell-name">潜在发展客户</div>
                <div class="cell-count">{{ matrixCounts.potential_develop || 0 }} 人</div>
              </td>
              <td class="cell low-value" @click="onCellClick('churn')">
                <div class="cell-name">流失客户</div>
                <div class="cell-count">{{ matrixCounts.churn || 0 }} 人</div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </el-card>

    
    <el-card class="segments-card">
      <template #header>
        <div class="card-header">
          <span>分群列表</span>
          <el-input
            v-model="searchKeyword"
            placeholder="搜索分群名称"
            style="width: 200px"
            clearable
          />
        </div>
      </template>
      <el-table :data="filteredSegments" v-loading="loading" stripe>
        <el-table-column prop="name" label="分群名称" min-width="150" />
        <el-table-column prop="type" label="分群类型" width="120">
          <template #default="{ row }">
            <el-tag :type="getTypeTagType(row.type)">{{ getTypeLabel(row.type) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="userCount" label="用户数" width="100" />
        <el-table-column prop="criteria" label="分群规则" min-width="250" />
        <el-table-column prop="createdAt" label="创建时间" width="180" />
        <el-table-column prop="status" label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="getStatusTagType(row.status)">{{ getStatusLabel(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="250" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="viewUsers(row)">查看用户</el-button>
            <el-button link type="primary" @click="editSegment(row)">编辑</el-button>
            <el-button link type="danger" @click="deleteSegment(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    
    <el-dialog v-model="dialogVisible" :title="dialogTitle" width="700px">
      <el-form :model="form" :rules="formRules" ref="formRef" label-width="100px">
        <el-form-item label="分群名称" prop="name">
          <el-input v-model="form.name" placeholder="请输入分群名称" />
        </el-form-item>
        <el-form-item label="分群类型" prop="type">
          <el-select v-model="form.type" placeholder="请选择分群类型" style="width: 100%">
            <el-option label="RFM 自动" value="RFM" />
            <el-option label="自定义" value="CUSTOM" />
            <el-option label="行为标签" value="BEHAVIOR" />
          </el-select>
        </el-form-item>
        <el-form-item label="R 范围(天数)" v-if="form.type === 'RFM'">
          <el-input-number v-model="form.rDays" :min="1" :max="365" />
        </el-form-item>
        <el-form-item label="F 次数" v-if="form.type === 'RFM'">
          <el-input-number v-model="form.fCount" :min="1" :max="100" />
        </el-form-item>
        <el-form-item label="M 金额" v-if="form.type === 'RFM'">
          <el-input-number v-model="form.mAmount" :min="0" />
        </el-form-item>
        <el-form-item label="分群规则" prop="criteria">
          <el-input
            v-model="form.criteria"
            type="textarea"
            :rows="4"
            placeholder="例如: 最近30天有消费且消费金额>=1000"
          />
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="form.remark" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="submitForm">确定</el-button>
      </template>
    </el-dialog>

    
    <el-dialog
      v-model="viewUsersDialogVisible"
      :title="'分群 ' + (currentSegment?.name) + ' 的用户列表'"
      width="900px"
    >
      <el-table :data="segmentUsers" v-loading="viewUsersLoading" stripe max-height="500">
        <el-table-column prop="id" label="用户ID" width="80" />
        <el-table-column prop="name" label="用户名" min-width="120" />
        <el-table-column prop="email" label="邮箱" min-width="180" />
        <el-table-column prop="phone" label="手机号" width="140" />
        <el-table-column prop="rScore" label="R 评分" width="80" align="center" />
        <el-table-column prop="fScore" label="F 评分" width="80" align="center" />
        <el-table-column prop="mScore" label="M 评分" width="80" align="center" />
        <el-table-column prop="segmentLabel" label="分群标签" width="120">
          <template #default="{ row: user }">
            <el-tag type="primary">{{ user.segmentLabel}}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="lastOrderTime" label="最近消费时间" width="170" />
        <el-table-column label="操作" width="100" fixed="right">
          <template #default="{ row: user }">
            <el-button link type="primary" @click="viewUserDetail(user)">详情</el-button>
          </template>
        </el-table-column>
      </el-table>
      <template #footer>
        <el-button @click="viewUsersDialogVisible = false">关闭</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Refresh } from '@element-plus/icons-vue'
import {
  getUserSegments,
  createUserSegment,
  updateUserSegment,
  deleteUserSegment,
  getSegmentStats,
  getSegmentUsers
} from '@/api/userSegment.js'
import { toList } from '@/utils/list.js'
import {
  SEGMENT_TYPE,
  GROUP_STATUS,
  getStatusLabel as getStatusLabelFromArr,
  getStatusTagType as getStatusTagTypeFromArr,
} from '@/constants/status';
import { getEnabledLabel, getEnabledTagType } from '@/constants/enabled'

const router = useRouter()

const loading = ref(false)
const searchKeyword = ref('')
const segments = ref([])
const stats = ref({ totalUsers: 0, highValue: 0, active: 0, churnRisk: 0 })
const matrixCounts = ref({})
const dialogVisible = ref(false)
const dialogTitle = ref('创建分群')
const viewUsersDialogVisible = ref(false)
const viewUsersLoading = ref(false)
const currentSegment = ref(null)
const segmentUsers = ref([])
const formRef = ref()
const form = ref({
  id: 0,
  name: '',
  type: 'RFM',
  rDays: 30,
  fCount: 3,
  mAmount: 1000,
  criteria: '',
  remark: '',
  status: 'active'
})
const formRules = {
  name: [{ required: true, message: i18n.global.t('请输入分群名称'), trigger: 'blur' }],
  type: [{ required: true, message: i18n.global.t('请选择分群类型'), trigger: 'change' }],
  criteria: [{ required: true, message: i18n.global.t('请输入分群规则'), trigger: 'blur' }]
}

const filteredSegments = computed(() => {
  if (!searchKeyword.value) return segments.value
  return segments.value.filter(s => s.name.includes(searchKeyword.value))
})

const LEGACY_SEGMENT_TYPE = { RFM: 'RFM', CUSTOM: '自定义', BEHAVIOR: '行为' };
const getTypeLabel = (type) => LEGACY_SEGMENT_TYPE[type] || getStatusLabelFromArr(type, SEGMENT_TYPE)
const getTypeTagType = (type) => {
  const map = { RFM: 'success', CUSTOM: 'primary', BEHAVIOR: 'warning' }
  return map[type] || getStatusTagTypeFromArr(type, SEGMENT_TYPE) || 'info'
}

const getStatusLabel = (s) => getEnabledLabel(s);
const getStatusTagType = (s) => getEnabledTagType(s)

const refreshData = async () => {
  loading.value = true
  try {
    const [segRes, statsRes] = await Promise.all([
      getUserSegments(),
      getSegmentStats()
    ])
    segments.value = toList(segRes)
    const s = statsRes || {}
    stats.value = {
      totalUsers: s.totalUsers ?? s.total_users ?? s.total ?? 0,
      highValue: s.highValue ?? s.high_value ?? 0,
      active: s.active ?? s.active_count ?? 0,
      churnRisk: s.churnRisk ?? s.churn_risk ?? 0
    }
    matrixCounts.value = s.matrix || s.matrix_counts || s.rfmMatrix || {}
  } catch (error) {
    console.error('加载数据失败', error)
  } finally {
    loading.value = false
  }
}

const onCellClick = (key) => {
  const map = {
    high_value: '重要价值客户',
    important_develop: '重要发展客户',
    important_keep: '重要保持客户',
    general_value: '一般价值客户',
    general_develop: '一般发展客户',
    general_keep: '一般保持客户',
    potential_value: '潜在价值客户',
    potential_develop: '潜在发展客户',
    churn: '流失客户'
  }
  ElMessage.info(`查看分群：${map[key] || key}`)
  const fakeSegment = { id: key, name: map[key] };
  viewUsers(fakeSegment)
}

const showCreateDialog = () => {
  form.value = { id: 0, name: '', type: 'RFM', rDays: 30, fCount: 3, mAmount: 1000, criteria: '', remark: '', status: 'active' }
  dialogTitle.value = '创建分群'
  dialogVisible.value = true
}

const editSegment = (row) => {
  form.value = { ...row }
  dialogTitle.value = '编辑分群'
  dialogVisible.value = true
}

const submitForm = async () => {
  if (!formRef.value) return
  await formRef.value.validate(async (valid) => {
    if (!valid) return
    try {
      if (form.value.id) {
        await updateUserSegment(form.value.id, form.value)
        ElMessage.success(i18n.global.t('更新成功'))
      } else {
        await createUserSegment(form.value)
        ElMessage.success(i18n.global.t('创建成功'))
      }
      dialogVisible.value = false
      refreshData()
    } catch (error) {
      ElMessage.error(i18n.global.t('操作失败'))
    }
  })
}

const deleteSegment = async (row) => {
  try {
    await ElMessageBox.confirm(`确定要删除分群 "${row.name}" 吗？`, '确认', { type: 'warning' })
    await deleteUserSegment(row.id)
    ElMessage.success(i18n.global.t('删除成功'))
    refreshData()
  } catch (error) {
    if (error !== 'cancel') ElMessage.error(i18n.global.t('删除失败'))
  }
}

const viewUsers = async (row) => {
  currentSegment.value = { id: row.id, name: row.name }
  viewUsersDialogVisible.value = true
  viewUsersLoading.value = true
  try {
    const res = await getSegmentUsers(row.id)
    segmentUsers.value = toList(res)
  } catch {
    ElMessage.error(i18n.global.t('加载用户列表失败'))
    segmentUsers.value = []
  } finally {
    viewUsersLoading.value = false
  }
}

const viewUserDetail = (user) => {
  router.push({ path: '/customer360', query: { id: user.id, name: user.name } });
}

onMounted(() => {
  refreshData()
})
</script>

<style scoped lang="scss">
.user-segment-page {
  padding: 20px;
}
.header-card {
  margin-bottom: 20px;
  :deep(.el-card__body) {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  .header-content h2 {
    margin: 0 0 8px 0;
  }
  .subtitle {
    color: #909399;
    margin: 0;
  }
  .header-actions {
    display: flex;
    gap: 10px;
  }
}
.overview-row {
  margin-bottom: 20px;
  .stat-card {
    text-align: center;
    .stat-label {
      color: #909399;
      font-size: 14px;
      margin-bottom: 10px;
    }
    .stat-value {
      font-size: 32px;
      font-weight: bold;
    }
  }
}
.matrix-card {
  margin-bottom: 20px;
  .rfm-matrix {
    table {
      width: 100%;
      border-collapse: collapse;
      th, td {
        border: 1px solid #ebeef5;
        padding: 12px;
        text-align: center;
      }
      th {
        background: #f5f7fa;
      }
      .cell {
        &.high-value { background: #10B981; color: white; }
        &.good { background: #4F46E5; color: white; }
        &.warn { background: #F59E0B; color: white; }
        &.normal { background: #909399; color: white; }
        &.low-value { background: #EF4444; color: white; }
      }
    }
  }
}
.segments-card {
  .card-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
}
</style>
