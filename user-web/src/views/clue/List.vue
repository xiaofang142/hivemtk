<template>
  <div class="clue-list-page">
    <el-card
      class="header-card"
      shadow="never"
    >
      <div class="page-header">
        <div class="header-text">
          <h2>{{ $t('线索列表') }}</h2>
          <p class="subtitle">
            多渠道线索统一管理：导入、筛选、分配、跟进与转化分析
          </p>
        </div>
        <div class="header-actions">
          <el-button @click="fetchCluelist">
            <el-icon><Refresh /></el-icon>
            {{ $t('刷新') }}
          </el-button>
          <el-button
            type="primary"
            @click="openImportDialog"
          >
            <el-icon><Upload /></el-icon>
            {{ $t('导入线索') }}
          </el-button>
        </div>
      </div>
    </el-card>

    
    <el-row
      :gutter="16"
      class="stats-row"
    >
      <el-col
        :xs="12"
        :sm="6"
      >
        <el-card
          class="stat-card"
          shadow="never"
        >
          <div class="stat-label">
            线索总数
          </div>
          <div class="stat-value">
            {{ stats.total || cluetotal || 0 }}
          </div>
        </el-card>
      </el-col>
      <el-col
        :xs="12"
        :sm="6"
      >
        <el-card
          class="stat-card"
          shadow="never"
        >
          <div class="stat-label">
            已验证
          </div>
          <div
            class="stat-value"
            style="color: #10B981"
          >
            {{ stats.verified || 0 }}
          </div>
        </el-card>
      </el-col>
      <el-col
        :xs="12"
        :sm="6"
      >
        <el-card
          class="stat-card"
          shadow="never"
        >
          <div class="stat-label">
            未验证
          </div>
          <div
            class="stat-value"
            style="color: #F59E0B"
          >
            {{ stats.unverified || 0 }}
          </div>
        </el-card>
      </el-col>
      <el-col
        :xs="12"
        :sm="6"
      >
        <el-card
          class="stat-card"
          shadow="never"
        >
          <div class="stat-label">
            今日新增
          </div>
          <div
            class="stat-value"
            style="color: #4F46E5"
          >
            {{ stats.today || 0 }}
          </div>
        </el-card>
      </el-col>
    </el-row>

    
    <el-card
      class="filter-card"
      shadow="never"
    >
      <el-form
        :inline="true"
        :model="filterForm"
        class="filter-form"
      >
        <el-form-item label="关键字">
          <el-input
            v-model="filterForm.keyword"
            placeholder="名称 / 账号 / 城市 / 地址"
            clearable
            style="width: 240px"
            @keyup.enter="handleSearch"
          />
        </el-form-item>
        <el-form-item label="线索类型">
          <el-select
            v-model="filterForm.type"
            placeholder="全部类型"
            clearable
            style="width: 160px"
          >
            <el-option
              v-for="t in clueTypeMap"
              :key="t.value"
              :label="t.label"
              :value="String(t.value)"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="验证状态">
          <el-select
            v-model="filterForm.verified"
            placeholder="全部"
            clearable
            style="width: 140px"
          >
            <el-option
              label="已验证"
              :value="1"
            />
            <el-option
              label="未验证"
              :value="0"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="时间">
          <el-date-picker
            v-model="dateRange"
            type="daterange"
            range-separator="至"
            start-placeholder="开始"
            end-placeholder="结束"
            value-format="YYYY-MM-DD"
            style="width: 280px"
          />
        </el-form-item>
        <el-form-item>
          <el-button
            type="primary"
            @click="handleSearch"
          >
            <el-icon><Search /></el-icon>搜索
          </el-button>
          <el-button @click="resetSearch">
            <el-icon><RefreshRight /></el-icon>重置
          </el-button>
        </el-form-item>
      </el-form>
    </el-card>

    
    <el-card
      class="table-card"
      shadow="never"
    >
      <el-table
        v-loading="loading"
        :data="cluelist"
        stripe
        :empty-text="$t('暂无线索数据')"
      >
        <el-table-column
          prop="id"
          label="ID"
          width="80"
        />
        <el-table-column
          prop="name"
          label="名称"
          min-width="140"
          show-overflow-tooltip
        />
        <el-table-column
          prop="account"
          label="账号"
          min-width="160"
          show-overflow-tooltip
        />
        <el-table-column
          label="类型"
          min-width="100"
        >
          <template #default="{ row }">
            <el-tag size="small">
              {{ getClueType(row.type) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="city"
          label="城市"
          min-width="100"
          show-overflow-tooltip
        />
        <el-table-column
          prop="address"
          label="地址"
          min-width="180"
          show-overflow-tooltip
        />
        <el-table-column
          label="来源"
          min-width="120"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            <el-tag
              :type="getChannelTagType(row.source_id)"
              effect="plain"
              size="small"
            >
              {{ getChannelLabel(row.source_id) || row.source_id || '-' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="验证状态"
          min-width="100"
          align="center"
        >
          <template #default="{ row }">
            <el-tag
              :type="row.is_verify >= 1 ? 'success' : 'info'"
              size="small"
            >
              {{ row.is_verify >= 1 ? '已验证' : '未验证' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="create_time"
          label="创建时间"
          min-width="170"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            {{ formatTime(row.create_time) }}
          </template>
        </el-table-column>
        <el-table-column
          prop="updated_at"
          label="最后跟进"
          min-width="170"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            {{ formatTime(row.updated_at) }}
          </template>
        </el-table-column>
        <el-table-column
          prop="owner_account"
          label="跟进人"
          min-width="120"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            {{ row.owner_account || '-' }}
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          min-width="200"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button
              link
              type="primary"
              size="small"
              @click="viewClue(row)"
            >
              查看
            </el-button>
            <el-button
              link
              type="primary"
              size="small"
              @click="assignClue(row)"
            >
              分配
            </el-button>
            <el-button
              link
              type="warning"
              size="small"
              @click="editClue(row)"
            >
              编辑
            </el-button>
            <el-button
              link
              type="danger"
              size="small"
              @click="deleteClue(row)"
            >
              删除
            </el-button>
          </template>
        </el-table-column>
      </el-table>

      <div class="pagination-container">
        <el-pagination
          :current-page="cluepage"
          :page-size="cluelimit"
          :page-sizes="[10, 20, 50, 100]"
          :total="cluetotal"
          layout="total, sizes, prev, pager, next, jumper"
          @size-change="handleSizeChange"
          @current-change="handlePageChange"
        />
      </div>
    </el-card>

    
    <el-dialog
      v-model="importDialogVisible"
      :title="$t('导入线索')"
      width="600px"
    >
      <el-form
        ref="importFormRef"
        :model="importForm"
        label-width="80px"
      >
        <el-form-item
          :label="$t('线索类型')"
          prop="type"
        >
          <el-select
            v-model="importForm.type"
            :placeholder="$t('请选择线索类型')"
            value-key="value"
            style="width: 100%"
            @change="handleTypeChange"
          >
            <el-option
              v-for="item in clueTypeMap"
              :key="String(item.value)"
              :label="item.label"
              :value="String(item.value)"
            />
          </el-select>
        </el-form-item>
        <el-form-item
          :label="$t('线索内容')"
          prop="content"
        >
          <el-input
            v-model="importForm.content"
            type="textarea"
            :placeholder="$t('请输入多行线索，每行一条，使用逗号分隔名称、账号、城市和地址（例如：张三,123456,北京,海淀区）')"
            :rows="10"
            class="import-textarea"
            @input="handleTextareaInput"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="importDialogVisible = false">
          {{ $t('取消') }}
        </el-button>
        <el-button
          type="primary"
          @click="submitImport"
        >
          {{ $t('导入') }}
        </el-button>
      </template>
    </el-dialog>

    
    <el-dialog
      v-model="detailVisible"
      title="线索详情"
      width="640px"
    >
      <el-descriptions
        v-if="current"
        :column="2"
        border
      >
        <el-descriptions-item label="ID">
          {{ current.id }}
        </el-descriptions-item>
        <el-descriptions-item label="名称">
          {{ current.name }}
        </el-descriptions-item>
        <el-descriptions-item label="账号">
          {{ current.account }}
        </el-descriptions-item>
        <el-descriptions-item label="类型">
          {{ getClueType(current.type) }}
        </el-descriptions-item>
        <el-descriptions-item label="城市">
          {{ current.city || '-' }}
        </el-descriptions-item>
        <el-descriptions-item label="地址">
          {{ current.address || '-' }}
        </el-descriptions-item>
        <el-descriptions-item label="来源">
          {{ getChannelLabel(current.source_id) || current.source_id || '-' }}
        </el-descriptions-item>
        <el-descriptions-item label="验证">
          {{ current.is_verify >= 1 ? '已验证' : '未验证' }}
        </el-descriptions-item>
        <el-descriptions-item label="创建时间">
          {{ formatTime(current.create_time) }}
        </el-descriptions-item>
        <el-descriptions-item label="最后跟进">
          {{ formatTime(current.updated_at) }}
        </el-descriptions-item>
        <el-descriptions-item label="跟进人">
          {{ current.owner_account || '-' }}
        </el-descriptions-item>
      </el-descriptions>
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Search, Refresh, RefreshRight, Upload } from '@element-plus/icons-vue'
import { clueApi } from '@/api/clue'
import { getChannelLabel, getChannelTagType } from '@/constants/channel'
import { getClueTypeLabel, CLUE_TYPE_OPTIONS } from '@/constants/cardPlatform';

const cluelist = ref([]);
const cluetotal = ref(0)
const cluepage = ref(1)
const cluelimit = ref(10)
const loading = ref(false)
const clueTypeMap = ref(CLUE_TYPE_OPTIONS.slice())
const stats = ref({ total: 0, verified: 0, unverified: 0, today: 0 })

const filterForm = reactive({
  keyword: '',
  type: '',
  verified: ''
});
const dateRange = ref([])

const getClueType = (type) => getClueTypeLabel(type)

const handlePageChange = (val) => {
  if (cluepage.value === val) return
  cluepage.value = val
  fetchCluelist()
}

const handleSizeChange = (val) => {
  cluelimit.value = val
  cluepage.value = 1
  fetchCluelist()
}

// 筛选必须下推到后端：客户端只能过滤已经加载的那一页，
// 选中库里真有 29 条的类型也会显示「暂无线索数据」。
const buildFilterQuery = () => {
  const q = {}
  const kw = (filterForm.keyword || '').trim()
  if (kw) q.keyword = kw
  if (filterForm.type !== '' && filterForm.type !== null) q.type = filterForm.type
  if (filterForm.verified !== '' && filterForm.verified !== null) q.is_verify = filterForm.verified
  if (Array.isArray(dateRange.value) && dateRange.value.length === 2) {
    const [s, e] = dateRange.value
    q.start = Math.floor(new Date(`${s}T00:00:00`).getTime() / 1000)
    q.end = Math.floor(new Date(`${e}T23:59:59`).getTime() / 1000)
  }
  return q
}

const handleSearch = () => {
  cluepage.value = 1
  fetchCluelist()
}

const resetSearch = () => {
  filterForm.keyword = ''
  filterForm.type = ''
  filterForm.verified = ''
  dateRange.value = []
  cluepage.value = 1
  fetchCluelist()
}

const fetchCluelist = async () => {
  if (loading.value) return
  loading.value = true
  try {
    const res = await clueApi.list(cluepage.value, cluelimit.value, buildFilterQuery())
    cluelist.value = res.list || []
    cluetotal.value = res.total || 0
    refreshStats()
  } catch (error) {
    console.error('获取线索列表失败:', error)
    cluelist.value = []
    cluetotal.value = 0
  } finally {
    loading.value = false
  }
}

// 统计卡是全局口径（标签写的是「线索总数/已验证/未验证/今日新增」），
// 不能拿当前这一页（10 条）去数 —— 那样「未验证」永远最多 10。
const refreshStats = async () => {
  const todayStart = new Date()
  todayStart.setHours(0, 0, 0, 0)
  const [st, today] = await Promise.all([
    clueApi.statistics().catch(() => []),
    clueApi.list(1, 1, { start: Math.floor(todayStart.getTime() / 1000) }).catch(() => null)
  ])
  const rows = Array.isArray(st) ? st : (st?.list || [])
  const total = rows.reduce((s, x) => s + (x.total || 0), 0)
  const verified = rows.reduce((s, x) => s + (x.verify_total || 0), 0)
  stats.value = {
    total,
    verified,
    unverified: total - verified,
    today: today?.total || 0
  }
}

const deleteClue = async (clue) => {
  try {
    await ElMessageBox.confirm(
      `确定要删除线索 ${clue.name} 吗？`,
      '警告',
      { confirmButtonText: '确定', cancelButtonText: '取消', type: 'warning' }
    )
  } catch {
    return
  }
  try {
    await clueApi.delete(clue.id)
    ElMessage.success(i18n.global.t('线索已删除'))
    fetchCluelist()
  } catch (e) {
    ElMessage.error(i18n.global.t('删除失败'))
  }
}

const viewClue = (row) => {
  current.value = row
  detailVisible.value = true
}
const assignClue = (row) => {
  ElMessage.info(i18n.global.t('已触发分配：') + (row.name || row.id))
}
const editClue = (row) => {
  ElMessage.info(i18n.global.t('编辑线索：') + (row.name || row.id))
}

const detailVisible = ref(false);
const current = ref(null)

const importDialogVisible = ref(false);
const importForm = ref({ type: '', content: '' })
const importFormRef = ref()

const openImportDialog = () => {
  importForm.value = { type: '', content: '' }
  importDialogVisible.value = true
}

const submitImport = async () => {
  if (!importForm.value.type) {
    ElMessage.error(i18n.global.t('请选择线索类型'))
    return
  }
  if (!importForm.value.content) {
    ElMessage.error(i18n.global.t('请输入线索内容'))
    return
  }
  const lines = importForm.value.content.split('\n')
  const clues = []
  for (const line of lines) {
    if (!line.trim()) continue
    const parts = line.split(',')
    if (parts.length < 2) {
      ElMessage.warning(`跳过无效格式行: ${line}`)
      continue
    }
    clues.push({
      name: parts[0].trim(),
      account: parts[1].trim(),
      city: parts.length > 2 ? parts[2].trim() : '',
      address: parts.length > 3 ? parts[3].trim() : '',
      type: importForm.value.type
    })
  }
  if (clues.length === 0) {
    ElMessage.warning(i18n.global.t('没有有效的线索数据'))
    return
  }
  try {
    const res = await clueApi.import(clues)
    // 后端返回 snake_case（success_count / skip_count），这里两种写法都兼容
    const successCount = res?.success_count ?? res?.successCount ?? 0
    const skipCount = res?.skip_count ?? res?.skipCount ?? 0
    ElMessage.success(`成功导入 ${successCount} 条线索，跳过 ${skipCount} 条重复数据`)
    importDialogVisible.value = false
    importForm.value = { type: '', content: '' }
    fetchCluelist()
  } catch (error) {
    console.error('线索导入失败:', error)
    ElMessage.error(i18n.global.t('线索导入失败'))
  }
}

const handleTextareaInput = (value) => {
  importForm.value.content = value
}
const handleTypeChange = (value) => {
  importForm.value.type = String(value)
}

const formatTime = (val) => {
  if (!val) return '-'
  try {
    // create_time / updated_at 是 unix 秒；直接 new Date(秒) 会落回 1970
    const d = new Date(typeof val === 'number' && val < 1e12 ? val * 1000 : val)
    if (Number.isNaN(d.getTime())) return val
    const pad = (n) => String(n).padStart(2, '0')
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
  } catch (e) {
    return val
  }
};

onMounted(() => {
  fetchCluelist()
})
</script>

<style lang="scss" scoped>
.clue-list-page {
  padding: 20px;
  display: flex;
  flex-direction: column;
  gap: 16px;

  .header-card {
    .page-header {
      display: flex;
      justify-content: space-between;
      align-items: center;
      gap: 16px;
      .header-text {
        flex: 1;
        h2 {
          margin: 0;
          font-size: 22px;
          color: #303133;
        }
        .subtitle {
          margin: 6px 0 0;
          font-size: 13px;
          color: #909399;
        }
      }
      .header-actions {
        display: flex;
        gap: 8px;
      }
    }
  }

  .stats-row {
    .stat-card {
      .stat-label {
        font-size: 13px;
        color: #909399;
      }
      .stat-value {
        font-size: 24px;
        font-weight: 600;
        margin-top: 8px;
        color: #303133;
      }
    }
  }

  .filter-card {
    .filter-form {
      margin: 0;
    }
  }

  .table-card {
    .pagination-container {
      margin-top: 16px;
      display: flex;
      justify-content: center;
    }
  }
}
</style>
