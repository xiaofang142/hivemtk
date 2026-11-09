<template>
  <div class="backup-page">
    <el-card class="header-card">
      <div>
        <h2>{{ $t('数据备份') }}</h2>
        <p class="subtitle">
          {{ $t('系统全量备份、自动调度、文件归档管理') }}
        </p>
      </div>
      <div>
        <el-button @click="loadData">
          <el-icon><Refresh /></el-icon>
          {{ $t('刷新') }}
        </el-button>
        <el-button
          type="primary"
          :loading="creating"
          @click="openCreateDialog"
        >
          <el-icon><Plus /></el-icon>
          {{ $t('立即备份') }}
        </el-button>
      </div>
    </el-card>

    <el-row
      :gutter="20"
      class="stat-row"
    >
      <el-col :span="6">
        <el-card>
          <div class="stat-item">
            <div class="stat-label">
              {{ $t('备份总数') }}
            </div>
            <div class="stat-value">
              {{ pagination.total }}
            </div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card>
          <div class="stat-item">
            <div class="stat-label">
              {{ $t('已完成') }}
            </div>
            <div class="stat-value success">
              {{ stats.completed }}
            </div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card>
          <div class="stat-item">
            <div class="stat-label">
              {{ $t('进行中') }}
            </div>
            <div class="stat-value warning">
              {{ stats.running }}
            </div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card>
          <div class="stat-item">
            <div class="stat-label">
              {{ $t('失败') }}
            </div>
            <div class="stat-value danger">
              {{ stats.failed }}
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-card
      v-if="userStore.isAdmin"
      class="strategy-card"
    >
      <template #header>
        <div class="strategy-header">
          <span>{{ $t('备份策略') }}</span>
          <el-button
            type="primary"
            :loading="savingStrategy"
            @click="saveStrategy"
          >
            {{ $t('保存策略') }}
          </el-button>
        </div>
      </template>
      <el-form
        :model="strategy"
        label-width="120px"
        class="strategy-form"
      >
        <el-form-item :label="$t('自动备份')">
          <el-switch v-model="strategy.enabled" />
        </el-form-item>
        <el-form-item :label="$t('每日备份')">
          <el-time-picker
            v-model="dailyTime"
            format="HH:mm"
            :placeholder="$t('02:00')"
          />
        </el-form-item>
        <el-form-item :label="$t('每周备份')">
          <el-select
            v-model="strategy.weeklyDay"
            style="width: 120px"
          >
            <el-option
              :label="$t('周日')"
              :value="0"
            />
            <el-option
              :label="$t('周一')"
              :value="1"
            />
          </el-select>
          <el-time-picker
            v-model="weeklyTime"
            format="HH:mm"
            :placeholder="$t('03:00')"
            style="margin-left: 8px"
          />
        </el-form-item>
        <el-form-item :label="$t('保留天数')">
          <el-input-number
            v-model="strategy.retentionDays"
            :min="7"
            :max="365"
          />
        </el-form-item>
        <el-form-item :label="$t('校验和')">
          <el-switch v-model="strategy.checksum" />
        </el-form-item>
      </el-form>
    </el-card>

    <el-tabs
      v-model="activeTab"
      @tab-change="onTabChange"
    >
      <el-tab-pane
        :label="$t('备份记录')"
        name="backup"
      >
        <el-card>
          <div class="filter-bar">
            <el-input
              v-model="searchKeyword"
              :placeholder="$t('搜索备份名')"
              clearable
              style="width: 220px"
            />
            <el-select
              v-model="filterType"
              :placeholder="$t('备份类型')"
              clearable
              style="width: 150px"
            >
              <el-option
                :label="$t('全量备份')"
                value="full"
              />
              <el-option
                :label="$t('增量备份')"
                value="incremental"
              />
              <el-option
                :label="$t('差异备份')"
                value="differential"
              />
            </el-select>
            <el-select
              v-model="filterStatus"
              :placeholder="$t('状态')"
              clearable
              style="width: 150px"
            >
              <el-option
                :label="$t('待执行')"
                value="pending"
              />
              <el-option
                :label="$t('进行中')"
                value="running"
              />
              <el-option
                :label="$t('已完成')"
                value="completed"
              />
              <el-option
                :label="$t('失败')"
                value="failed"
              />
            </el-select>
          </div>

          <el-table
            v-loading="loading"
            :data="filteredBackups"
            stripe
          >
            <el-table-column
              prop="id"
              label="ID"
              width="80"
            />
            <el-table-column
              prop="backup_name"
              :label="$t('备份名称')"
              min-width="200"
              show-overflow-tooltip
            />
            <el-table-column
              prop="backup_type"
              :label="$t('类型')"
              width="120"
              align="center"
            >
              <template #default="{ row }">
                <el-tag
                  :type="getTypeTag(row.backup_type)"
                  size="small"
                >
                  {{ getTypeText(row.backup_type) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              prop="status"
              label="状态"
              width="120"
              align="center"
            >
              <template #default="{ row }">
                <el-tag
                  :type="getStatusType(row.status)"
                  size="small"
                >
                  {{ getStatusText(row.status) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              prop="file_size"
              label="文件大小"
              width="120"
              align="right"
            >
              <template #default="{ row }">
                {{ formatSize(row.file_size) }}
              </template>
            </el-table-column>
            <el-table-column
              prop="started_at"
              label="开始时间"
              min-width="170"
            >
              <template #default="{ row }">
                {{ formatTime(row.started_at) }}
              </template>
            </el-table-column>
            <el-table-column
              prop="completed_at"
              label="完成时间"
              min-width="170"
            >
              <template #default="{ row }">
                {{ formatTime(row.completed_at) }}
              </template>
            </el-table-column>
            <el-table-column
              prop="error_message"
              label="错误信息"
              min-width="180"
              show-overflow-tooltip
            />
            <el-table-column
              label="操作"
              width="220"
              fixed="right"
            >
              <template #default="{ row }">
                <el-button
                  link
                  type="primary"
                  @click="viewDetail(row)"
                >
                  详情
                </el-button>
                <el-button
                  link
                  type="primary"
                  :disabled="row.status !== 'completed'"
                  @click="openPreview(row)"
                >
                  预览影响
                </el-button>
                <el-button
                  link
                  type="success"
                  :disabled="row.status !== 'completed'"
                  @click="openRestoreDialog(row)"
                >
                  恢复
                </el-button>
                <el-button
                  link
                  type="danger"
                  @click="doDeleteBackup(row)"
                >
                  删除
                </el-button>
              </template>
            </el-table-column>
            <template #empty>
              <el-empty description="暂无备份记录" />
            </template>
          </el-table>

          <el-pagination
            v-model:current-page="pagination.page"
            v-model:page-size="pagination.size"
            :total="pagination.total"
            :page-sizes="[10, 20, 50, 100]"
            layout="total, sizes, prev, pager, next, jumper"
            style="margin-top: 15px; text-align: right"
            @current-change="loadData"
            @size-change="loadData"
          />
        </el-card>
      </el-tab-pane>

      
      <el-tab-pane
        label="恢复记录"
        name="restore"
      >
        <el-card>
          <div class="filter-bar">
            <el-input
              v-model="restoreKeyword"
              placeholder="搜索备份名"
              clearable
              style="width: 220px"
            />
            <el-select
              v-model="restoreStatus"
              placeholder="状态"
              clearable
              style="width: 150px"
            >
              <el-option
                label="待执行"
                value="pending"
              />
              <el-option
                label="进行中"
                value="running"
              />
              <el-option
                label="已完成"
                value="completed"
              />
              <el-option
                label="失败"
                value="failed"
              />
            </el-select>
          </div>

          <el-table
            v-loading="restoreLoading"
            :data="filteredRestores"
            stripe
          >
            <el-table-column
              prop="id"
              label="ID"
              width="80"
            />
            <el-table-column
              prop="backup_id"
              label="备份ID"
              width="100"
            />
            <el-table-column
              prop="backup_name"
              label="备份名称"
              min-width="220"
              show-overflow-tooltip
            />
            <el-table-column
              prop="status"
              label="状态"
              width="120"
              align="center"
            >
              <template #default="{ row }">
                <el-tag
                  :type="getStatusType(row.status)"
                  size="small"
                >
                  {{ getStatusText(row.status) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              prop="restored_at"
              label="开始时间"
              min-width="170"
            >
              <template #default="{ row }">
                {{ formatTime(row.restored_at) }}
              </template>
            </el-table-column>
            <el-table-column
              prop="error_message"
              label="错误信息"
              min-width="220"
              show-overflow-tooltip
            />
            <el-table-column
              label="操作"
              width="100"
              fixed="right"
            >
              <template #default="{ row }">
                <el-button
                  link
                  type="primary"
                  @click="viewRestoreDetail(row)"
                >
                  详情
                </el-button>
              </template>
            </el-table-column>
            <template #empty>
              <el-empty description="暂无恢复记录" />
            </template>
          </el-table>

          <el-pagination
            v-model:current-page="restorePagination.page"
            v-model:page-size="restorePagination.size"
            :total="restorePagination.total"
            :page-sizes="[10, 20, 50, 100]"
            layout="total, sizes, prev, pager, next, jumper"
            style="margin-top: 15px; text-align: right"
            @current-change="loadRestores"
            @size-change="loadRestores"
          />
        </el-card>
      </el-tab-pane>
    </el-tabs>

    
    <el-dialog
      v-model="createVisible"
      title="创建备份"
      width="520px"
    >
      <el-form
        ref="createFormRef"
        :model="createForm"
        :rules="createRules"
        label-width="100px"
      >
        <el-form-item
          label="备份名称"
          prop="backup_name"
        >
          <el-input
            v-model="createForm.backup_name"
            placeholder="留空将自动生成"
          />
        </el-form-item>
        <el-form-item
          label="备份类型"
          prop="backup_type"
        >
          <el-select
            v-model="createForm.backup_type"
            style="width: 100%"
          >
            <el-option
              label="全量备份"
              value="full"
            />
            <el-option
              label="增量备份"
              value="incremental"
            />
            <el-option
              label="差异备份"
              value="differential"
            />
          </el-select>
        </el-form-item>
        <el-alert
          type="info"
          :closable="false"
          show-icon
        >
          <template #title>
            备份将在后台异步执行，完成后请在列表中查看状态
          </template>
        </el-alert>
      </el-form>
      <template #footer>
        <el-button @click="createVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          :loading="creating"
          @click="submitCreate"
        >
          立即备份
        </el-button>
      </template>
    </el-dialog>

    
    <el-dialog
      v-model="restoreVisible"
      title="恢复备份"
      width="520px"
    >
      <el-descriptions
        v-if="restoreTarget"
        :column="1"
        border
      >
        <el-descriptions-item label="备份ID">
          {{ restoreTarget.id }}
        </el-descriptions-item>
        <el-descriptions-item label="备份名称">
          {{ restoreTarget.backup_name }}
        </el-descriptions-item>
        <el-descriptions-item label="备份类型">
          {{ getTypeText(restoreTarget.backup_type) }}
        </el-descriptions-item>
        <el-descriptions-item label="文件大小">
          {{ formatSize(restoreTarget.file_size) }}
        </el-descriptions-item>
        <el-descriptions-item label="完成时间">
          {{ formatTime(restoreTarget.completed_at) }}
        </el-descriptions-item>
      </el-descriptions>
      <el-alert
        type="warning"
        :closable="false"
        show-icon
        style="margin-top: 12px"
      >
        <template #title>
          恢复操作将覆盖当前数据库中的对应数据，请确认无误后再执行
        </template>
      </el-alert>
      <template #footer>
        <el-button @click="restoreVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          :loading="restoring"
          @click="submitRestore"
        >
          确认恢复
        </el-button>
      </template>
    </el-dialog>

    
    <el-dialog
      v-model="detailVisible"
      title="备份详情"
      width="600px"
    >
      <el-descriptions
        v-if="detailRecord"
        :column="1"
        border
      >
        <el-descriptions-item label="备份ID">
          {{ detailRecord.id }}
        </el-descriptions-item>
        <el-descriptions-item label="备份名称">
          {{ detailRecord.backup_name }}
        </el-descriptions-item>
        <el-descriptions-item label="备份类型">
          {{ getTypeText(detailRecord.backup_type) }}
        </el-descriptions-item>
        <el-descriptions-item label="状态">
          <el-tag
            :type="getStatusType(detailRecord.status)"
            size="small"
          >
            {{ getStatusText(detailRecord.status) }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="文件路径">
          {{ detailRecord.file_path || '-' }}
        </el-descriptions-item>
        <el-descriptions-item label="文件大小">
          {{ formatSize(detailRecord.file_size) }}
        </el-descriptions-item>
        <el-descriptions-item label="创建人">
          {{ detailRecord.created_by || '系统' }}
        </el-descriptions-item>
        <el-descriptions-item label="开始时间">
          {{ formatTime(detailRecord.started_at) }}
        </el-descriptions-item>
        <el-descriptions-item label="完成时间">
          {{ formatTime(detailRecord.completed_at) }}
        </el-descriptions-item>
        <el-descriptions-item
          v-if="detailRecord.error_message"
          label="错误信息"
        >
          <span class="error-text">{{ detailRecord.error_message }}</span>
        </el-descriptions-item>
      </el-descriptions>
    </el-dialog>

    <!--
      恢复影响预览（自 2026-11-10「备份高级版」并入：原 Enhanced.vue 独有功能）。
      预览走 /api/backup/:id/preview 别名（表级行数估算），确认恢复仍走本页
      原有的 /api/backups 权威恢复通道（callRestoreApi），两条路径同源同服务。
    -->
    <el-dialog
      v-model="previewVisible"
      :title="$t('恢复影响预览')"
      width="600px"
    >
      <el-alert
        type="warning"
        :closable="false"
        show-icon
      >
        <template #title>
          {{ $t('即将恢复此备份：会影响以下数据（行数估算）') }}
        </template>
      </el-alert>
      <el-table
        v-loading="previewLoading"
        :data="previewData"
        max-height="320"
        style="margin-top: 12px"
      >
        <el-table-column
          prop="table"
          :label="$t('表名')"
        />
        <el-table-column
          prop="rows"
          :label="$t('行数估算')"
          width="160"
        />
      </el-table>
      <template #footer>
        <el-button @click="previewVisible = false">
          {{ $t('取消') }}
        </el-button>
        <el-button
          type="danger"
          :loading="restoring"
          @click="confirmRestoreFromPreview"
        >
          {{ $t('确认恢复') }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Refresh } from '@element-plus/icons-vue'
import { useUserStore } from '@/stores/user'
import { http } from '@/utils/request'
import {
  getBackupList,
  createBackup,
  deleteBackup,
  restoreBackup as callRestoreApi,
  getRestoreList
} from '@/api/backup.js'
import {
  BACKUP_STATUS,
  getStatusLabel as getStatusLabelFromArr,
  getStatusTagType as getStatusTagTypeFromArr,
} from '@/constants/status';

const userStore = useUserStore()

const activeTab = ref('backup')

const loading = ref(false);
const backups = ref([])
const pagination = reactive({ page: 1, size: 20, total: 0 })
const searchKeyword = ref('')
const filterType = ref('')
const filterStatus = ref('')

const restoreLoading = ref(false);
const restores = ref([])
const restorePagination = reactive({ page: 1, size: 20, total: 0 })
const restoreKeyword = ref('')
const restoreStatus = ref('')

const createVisible = ref(false);
const creating = ref(false)
const createFormRef = ref()
const createForm = reactive({
  backup_name: '',
  backup_type: 'full'
})
const createRules = {
  backup_type: [{ required: true, message: i18n.global.t('请选择备份类型'), trigger: 'change' }]
}

const restoreVisible = ref(false);
const restoring = ref(false)
const restoreTarget = ref(null)

const detailVisible = ref(false);
const detailRecord = ref(null)

// ---- 备份策略（自 2026-11-10「备份高级版」并入；策略端点挂 systemAdmin 组，非 admin 隐藏此卡）----
const savingStrategy = ref(false)
const strategy = reactive({
  enabled: true,
  dailyHour: 2,
  dailyMinute: 0,
  weeklyDay: 0,
  weeklyHour: 3,
  weeklyMinute: 0,
  retentionDays: 30,
  checksum: true
})

const toDateOf = (hour, minute) => {
  const d = new Date(2024, 0, 1, 0, 0)
  d.setHours(hour || 0, minute || 0, 0, 0)
  return d
}
const dailyTime = computed({
  get: () => toDateOf(strategy.dailyHour, strategy.dailyMinute),
  set: (d) => {
    if (d instanceof Date && !isNaN(d.getTime())) {
      strategy.dailyHour = d.getHours()
      strategy.dailyMinute = d.getMinutes()
    }
  }
})
const weeklyTime = computed({
  get: () => toDateOf(strategy.weeklyHour, strategy.weeklyMinute),
  set: (d) => {
    if (d instanceof Date && !isNaN(d.getTime())) {
      strategy.weeklyHour = d.getHours()
      strategy.weeklyMinute = d.getMinutes()
    }
  }
})

const loadStrategy = async () => {
  if (!userStore.isAdmin) return
  try {
    const s = await http.get('/api/backup/strategy')
    if (s && typeof s === 'object') Object.assign(strategy, s)
  } catch { /* 策略回显失败不阻塞页面，保持默认值 */ }
}

const saveStrategy = async () => {
  savingStrategy.value = true
  try {
    await http.put('/api/backup/strategy', {
      enabled: strategy.enabled,
      daily_hour: strategy.dailyHour,
      daily_minute: strategy.dailyMinute,
      weekly_day: strategy.weeklyDay,
      weekly_hour: strategy.weeklyHour,
      weekly_minute: strategy.weeklyMinute,
      retention_days: strategy.retentionDays,
      checksum: strategy.checksum
    })
    ElMessage.success(i18n.global.t('策略已保存'))
  } catch (e) {
    ElMessage.error(e?.message || i18n.global.t('保存策略失败'))
  } finally {
    savingStrategy.value = false
  }
}

// ---- 恢复影响预览（自「备份高级版」并入）----
const previewVisible = ref(false)
const previewLoading = ref(false)
const previewData = ref([])
const previewTarget = ref(null)

const openPreview = async (row) => {
  previewTarget.value = row
  previewData.value = []
  previewVisible.value = true
  previewLoading.value = true
  try {
    const data = await http.get(`/api/backup/${row.id}/preview`)
    previewData.value = Array.isArray(data) ? data : (data?.list || [])
  } catch (e) {
    ElMessage.error(e?.message || i18n.global.t('加载影响预览失败'))
  } finally {
    previewLoading.value = false
  }
}

const confirmRestoreFromPreview = async () => {
  if (!previewTarget.value) return
  restoring.value = true
  try {
    await callRestoreApi({ backup_id: previewTarget.value.id })
    ElMessage.success(i18n.global.t('恢复任务已创建，请稍后查看进度'))
    previewVisible.value = false
    activeTab.value = 'restore'
    await loadRestores()
  } catch (e) {
    ElMessage.error(e?.message || '恢复失败')
  } finally {
    restoring.value = false
  }
}

const stats = computed(() => {
  const result = { completed: 0, running: 0, failed: 0, pending: 0 }
  backups.value.forEach(b => {
    if (b.status === 'completed') result.completed++
    else if (b.status === 'running') result.running++
    else if (b.status === 'failed') result.failed++
    else if (b.status === 'pending') result.pending++
  })
  return result
});

const filteredBackups = computed(() => {
  let result = backups.value
  if (searchKeyword.value) {
    const kw = searchKeyword.value.toLowerCase()
    result = result.filter(b => b.backup_name?.toLowerCase().includes(kw))
  }
  if (filterType.value) result = result.filter(b => b.backup_type === filterType.value)
  if (filterStatus.value) result = result.filter(b => b.status === filterStatus.value)
  return result
})

const filteredRestores = computed(() => {
  let result = restores.value
  if (restoreKeyword.value) {
    const kw = restoreKeyword.value.toLowerCase()
    result = result.filter(r => r.backup_name?.toLowerCase().includes(kw))
  }
  if (restoreStatus.value) result = result.filter(r => r.status === restoreStatus.value)
  return result
})

const getTypeText = (type) => {
  const map = { full: '全量', incremental: '增量', differential: '差异' }
  return map[type] || type || '-'
}
const getTypeTag = (type) => {
  const map = { full: 'primary', incremental: 'success', differential: 'warning' }
  return map[type] || 'info'
}
const getStatusText = (status) => getStatusLabelFromArr(status, BACKUP_STATUS);
const getStatusType = (status) => getStatusTagTypeFromArr(status, BACKUP_STATUS)
const formatSize = (bytes) => {
  if (!bytes || bytes <= 0) return '-'
  const units = ['B', 'KB', 'MB', 'GB']
  let i = 0
  let size = bytes
  while (size >= 1024 && i < units.length - 1) {
    size /= 1024
    i++
  }
  return `${size.toFixed(2)} ${units[i]}`
}
const formatTime = (val) => {
  if (!val) return '-'
  try {
    const d = new Date(val)
    if (isNaN(d.getTime())) return val
    return d.toLocaleString('zh-CN', { hour12: false })
  } catch (e) {
    return val
  }
}

const loadData = async () => {
  loading.value = true
  try {
    const res = await getBackupList({
      page: pagination.page,
      page_size: pagination.size
    })
    const data = res || {}
    backups.value = data.list || []
    pagination.total = data.total || 0
  } catch (e) {
    ElMessage.error(i18n.global.t('加载备份列表失败'))
    backups.value = []
  } finally {
    loading.value = false
  }
}

const loadRestores = async () => {
  restoreLoading.value = true
  try {
    const res = await getRestoreList({
      page: restorePagination.page,
      page_size: restorePagination.size
    })
    const data = res || {}
    restores.value = data.list || []
    restorePagination.total = data.total || 0
  } catch (e) {
    ElMessage.error(i18n.global.t('加载恢复记录失败'))
    restores.value = []
  } finally {
    restoreLoading.value = false
  }
}

const onTabChange = (name) => {
  if (name === 'restore' && restores.value.length === 0) loadRestores()
}

const openCreateDialog = () => {
  createForm.backup_name = ''
  createForm.backup_type = 'full'
  createVisible.value = true
}

const submitCreate = async () => {
  if (!createFormRef.value) return
  await createFormRef.value.validate(async (valid) => {
    if (!valid) return
    creating.value = true
    try {
      await createBackup({
        backup_name: createForm.backup_name,
        backup_type: createForm.backup_type
      })
      ElMessage.success(i18n.global.t('备份任务已创建'))
      createVisible.value = false
      await loadData()
    } catch (e) {
      ElMessage.error(e?.message || '创建备份失败')
    } finally {
      creating.value = false
    }
  })
}

const openRestoreDialog = (row) => {
  restoreTarget.value = row
  restoreVisible.value = true
}

const submitRestore = async () => {
  if (!restoreTarget.value) return
  restoring.value = true
  try {
    await callRestoreApi({ backup_id: restoreTarget.value.id })
    ElMessage.success(i18n.global.t('恢复任务已创建，请稍后查看进度'))
    restoreVisible.value = false
    activeTab.value = 'restore'
    await loadRestores()
  } catch (e) {
    ElMessage.error(e?.message || '恢复失败')
  } finally {
    restoring.value = false
  }
}

const doDeleteBackup = async (row) => {
  try {
    await ElMessageBox.confirm(
      `确定删除备份 "${row.backup_name}"？删除后不可恢复`,
      '警告',
      { type: 'warning' }
    )
    await deleteBackup(row.id)
    ElMessage.success(i18n.global.t('删除成功'))
    await loadData()
  } catch (e) {
    if (e !== 'cancel' && e !== 'close') ElMessage.error(i18n.global.t('删除失败'))
  }
}

const viewDetail = (row) => {
  detailRecord.value = row
  detailVisible.value = true
}

const viewRestoreDetail = (row) => {
  detailRecord.value = row
  detailVisible.value = true
}

onMounted(() => {
  loadData()
  loadStrategy()
})
</script>

<style scoped lang="scss">
.backup-page { padding: 20px; }
.header-card {
  margin-bottom: 20px;
  :deep(.el-card__body) { display: flex; justify-content: space-between; align-items: center; }
  h2 { margin: 0 0 8px 0; }
  .subtitle { color: #909399; margin: 0; }
}
.stat-row {
  margin-bottom: 20px;
  .stat-item {
    text-align: center;
    .stat-label { color: #909399; font-size: 14px; margin-bottom: 10px; }
    .stat-value { font-size: 28px; font-weight: bold; }
    .stat-value.success { color: #10B981; }
    .stat-value.warning { color: #F59E0B; }
    .stat-value.danger { color: #EF4444; }
  }
}
.filter-bar {
  display: flex;
  gap: 10px;
  margin-bottom: 15px;
  flex-wrap: wrap;
}
.strategy-card {
  margin-bottom: 20px;
  .strategy-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  .strategy-form {
    max-width: 480px;
  }
}
.error-text { color: #EF4444; }
</style>
