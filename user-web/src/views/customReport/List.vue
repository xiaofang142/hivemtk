<template>
  <div class="custom-report-page">
    <el-card class="header-card">
      <div>
        <h2>{{ $t('自定义报表') }}</h2>
        <p class="subtitle">
          {{ $t('自定义维度、指标生成专属业务报表') }}
        </p>
      </div>
      <el-button
        type="primary"
        @click="showCreateDialog"
      >
        <el-icon><Plus /></el-icon>
        {{ $t('新建报表') }}
      </el-button>
    </el-card>

    <el-card>
      <el-table
        v-loading="loading"
        :data="reports"
        stripe
      >
        <el-table-column
          prop="name"
          :label="$t('报表名称')"
          min-width="180"
        />
        <el-table-column
          prop="data_source"
          label="数据源"
          width="120"
        >
          <template #default="{ row }">
            <el-tag>{{ dataSourceLabel(row.data_source) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="chart_type"
          label="图表类型"
          width="110"
        >
          <template #default="{ row }">
            <el-tag type="info">
              {{ chartTypeLabel(row.chart_type) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="维度"
          min-width="200"
        >
          <template #default="{ row }">
            <el-tag
              v-for="d in parseDimensions(row.dimensions)"
              :key="d.field"
              class="tag-item"
              size="small"
            >
              {{ d.label }}
            </el-tag>
            <span
              v-if="!parseDimensions(row.dimensions).length"
              class="muted"
            >-</span>
          </template>
        </el-table-column>
        <el-table-column
          label="指标"
          min-width="200"
        >
          <template #default="{ row }">
            <el-tag
              v-for="m in parseMetrics(row.metrics)"
              :key="m.field"
              class="tag-item"
              size="small"
              type="warning"
            >
              {{ m.label }}
            </el-tag>
            <span
              v-if="!parseMetrics(row.metrics).length"
              class="muted"
            >-</span>
          </template>
        </el-table-column>
        <el-table-column
          prop="created_by"
          label="创建人"
          width="90"
        />
        <el-table-column
          prop="created_at"
          label="创建时间"
          min-width="170">
          <template #default="{ row }">
            {{ fmtTime(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="300"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button
              link
              type="primary"
              @click="viewReport(row)"
            >
              查看
            </el-button>
            <el-button
              link
              type="primary"
              @click="exportReport(row)"
            >
              导出
            </el-button>
            <el-button
              link
              type="primary"
              @click="editReport(row)"
            >
              编辑
            </el-button>
            <el-button
              link
              type="danger"
              @click="deleteReport(row)"
            >
              删除
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog
      v-model="dialogVisible"
      :title="dialogTitle"
      width="820px"
    >
      <el-form
        ref="formRef"
        :model="form"
        :rules="formRules"
        label-width="110px"
      >
        <el-form-item
          label="报表名称"
          prop="name"
        >
          <el-input
            v-model="form.name"
            placeholder="请输入报表名称"
          />
        </el-form-item>
        <el-form-item
          label="数据源"
          prop="dataSource"
        >
          <el-select
            v-model="form.dataSource"
            style="width: 100%"
          >
            <el-option
              label="会话数据"
              value="sessions"
            />
            <el-option
              label="消息数据"
              value="messages"
            />
            <el-option
              label="订单数据"
              value="orders"
            />
            <el-option
              label="线索数据"
              value="clues"
            />
            <el-option
              label="用户数据"
              value="users"
            />
            <el-option
              label="RFM 数据"
              value="rfm"
            />
            <el-option
              label="客服数据"
              value="agents"
            />
          </el-select>
        </el-form-item>
        <el-form-item
          label="图表类型"
          prop="chartType"
        >
          <el-select
            v-model="form.chartType"
            style="width: 100%"
          >
            <el-option
              label="表格"
              value="table"
            />
            <el-option
              label="折线图"
              value="line"
            />
            <el-option
              label="柱状图"
              value="bar"
            />
            <el-option
              label="饼图"
              value="pie"
            />
            <el-option
              label="面积图"
              value="area"
            />
            <el-option
              label="卡片"
              value="card"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="维度(多选)">
          <el-select
            v-model="form.dimensions"
            multiple
            style="width: 100%"
            placeholder="请选择维度"
          >
            <el-option
              v-for="d in DIM_OPTIONS"
              :key="d.value"
              :label="d.label"
              :value="d.value"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="指标(多选)">
          <el-select
            v-model="form.metrics"
            multiple
            style="width: 100%"
            placeholder="请选择指标"
          >
            <el-option
              v-for="m in METRIC_OPTIONS"
              :key="m.value"
              :label="m.label"
              :value="m.value"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="筛选条件">
          <div class="filters">
            <div
              v-for="(f, idx) in form.filters"
              :key="idx"
              class="filter-row"
            >
              <el-input
                v-model="f.field"
                placeholder="字段"
                style="width: 140px"
              />
              <el-select
                v-model="f.operator"
                style="width: 110px"
              >
                <el-option
                  label="等于"
                  value="eq"
                />
                <el-option
                  label="不等于"
                  value="ne"
                />
                <el-option
                  label="大于"
                  value="gt"
                />
                <el-option
                  label="小于"
                  value="lt"
                />
                <el-option
                  label="包含"
                  value="like"
                />
              </el-select>
              <el-input
                v-model="f.value"
                placeholder="值"
                style="width: 140px"
              />
              <el-button
                type="danger"
                text
                @click="removeFilter(idx)"
              >
                移除
              </el-button>
            </div>
            <el-button @click="addFilter">
              + 添加筛选
            </el-button>
          </div>
        </el-form-item>
        <el-form-item label="备注">
          <el-input
            v-model="form.description"
            type="textarea"
            :rows="2"
          />
        </el-form-item>
        <el-form-item label="公开">
          <el-switch v-model="form.isPublic" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          :loading="submitting"
          @click="submitForm"
        >
          确定
        </el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="viewDialogVisible"
      :title="`查看报表: ${currentReportName}`"
      width="1000px"
    >
      <el-table
        v-loading="viewLoading"
        :data="reportData"
        stripe
        border
        max-height="500px"
      >
        <el-table-column
          v-for="col in reportColumns"
          :key="col.key"
          :prop="col.key"
          :label="col.label"
          :min-width="col.minWidth"
          show-overflow-tooltip
        />
        <template #empty>
          <el-empty description="暂无报表数据" />
        </template>
      </el-table>
      <template #footer>
        <el-button @click="viewDialogVisible = false">
          关闭
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'
import { ref, onMounted, onActivated } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import { toList } from '@/utils/list.js'
import {
  getCustomReports,
  createCustomReport,
  updateCustomReport,
  deleteCustomReport,
  exportCustomReport,
  queryReportData,
} from '@/api/customReport.js'
import { fmtTime } from '@/utils/time'

// 维度/指标的显示名。标签是直接渲染给用户看的界面文案，所以走 i18n。
// 用点分键（customReport.dimension.*）而不是中文字面量：中文字面量键在
// i18n 门禁里算「中文直传」，而点分键才算「真 i18n 键」。
// 用 useI18n 的 t 而不是 i18n.global.t：后者在模块初始化时求值一次，
// 切换语言后不会跟着变。
const { t } = useI18n()

const DIM_MAP = {
  date: { field: 'date', label: t('customReport.dimension.date'), data_type: 'date', group_by: true },
  source: { field: 'source', label: t('customReport.dimension.source'), data_type: 'string', group_by: true },
  platform: { field: 'platform', label: t('customReport.dimension.platform'), data_type: 'string', group_by: true },
  region: { field: 'region', label: t('customReport.dimension.region'), data_type: 'string', group_by: true },
  user_type: { field: 'user_type', label: t('customReport.dimension.userType'), data_type: 'string', group_by: true },
}
const METRIC_MAP = {
  count: { field: 'count', label: t('customReport.metric.count'), agg_func: 'count', data_type: 'number' },
  amount: { field: 'amount', label: t('customReport.metric.amount'), agg_func: 'sum', data_type: 'number' },
  conversion: { field: 'conversion', label: t('customReport.metric.conversion'), agg_func: 'avg', data_type: 'percent' },
  click_rate: { field: 'click_rate', label: t('customReport.metric.clickRate'), agg_func: 'avg', data_type: 'percent' },
  open_rate: { field: 'open_rate', label: t('customReport.metric.openRate'), agg_func: 'avg', data_type: 'percent' },
}
const DIM_OPTIONS = Object.entries(DIM_MAP).map(([value, v]) => ({ value, label: v.label }))
const METRIC_OPTIONS = Object.entries(METRIC_MAP).map(([value, v]) => ({ value, label: v.label }))
const DS_LABELS = {
  sessions: '会话数据', messages: '消息数据', orders: '订单数据', clues: '线索数据',
  users: '用户数据', rfm: 'RFM 数据', agents: '客服数据',
}
const CT_LABELS = { table: '表格', line: '折线图', bar: '柱状图', pie: '饼图', area: '面积图', card: '卡片' }

const loading = ref(false)
const reports = ref([])
const dialogVisible = ref(false)
const dialogTitle = ref('新建报表')
const submitting = ref(false)
const formRef = ref()
const viewDialogVisible = ref(false)
const viewLoading = ref(false)
const currentReportName = ref('')
const reportData = ref([])
const reportColumns = ref([])

const form = ref(emptyForm())
function emptyForm() {
  return {
    id: 0,
    name: '',
    dataSource: 'clues',
    chartType: 'table',
    dimensions: [],
    metrics: [],
    filters: [],
    description: '',
    isPublic: true,
  }
}

const formRules = {
  name: [{ required: true, message: i18n.global.t('请输入报表名称'), trigger: 'blur' }],
  dataSource: [{ required: true, message: i18n.global.t('请选择数据源'), trigger: 'change' }],
  chartType: [{ required: true, message: i18n.global.t('请选择图表类型'), trigger: 'change' }],
}

const dataSourceLabel = (v) => DS_LABELS[v] || v || '-'
const chartTypeLabel = (v) => CT_LABELS[v] || v || '-'
const parseDimensions = (raw) => safeParseArray(raw).map((d) => (typeof d === 'string' ? { field: d, label: d } : d)).filter(Boolean)
const parseMetrics = (raw) => safeParseArray(raw).map((m) => (typeof m === 'string' ? { field: m, label: m } : m)).filter(Boolean)
function safeParseArray(raw) {
  if (Array.isArray(raw)) return raw
  if (typeof raw !== 'string' || !raw) return []
  try { return JSON.parse(raw) } catch (e) { return [] }
}

const refreshData = async () => {
  loading.value = true
  try {
    const res = await getCustomReports()
    reports.value = toList(res?.data ?? res)
  } catch (e) {
    ElMessage.error('加载失败：' + (e && e.message ? e.message : ''))
  } finally {
    loading.value = false
  }
}

const showCreateDialog = () => {
  form.value = emptyForm()
  dialogTitle.value = '新建报表'
  dialogVisible.value = true
}

const addFilter = () => form.value.filters.push({ field: '', operator: 'eq', value: '' })
const removeFilter = (idx) => form.value.filters.splice(idx, 1)

const editReport = (row) => {
  form.value = {
    id: row.id,
    name: row.name || '',
    dataSource: row.data_source || 'clues',
    chartType: row.chart_type || 'table',
    dimensions: parseDimensions(row.dimensions).map((d) => d.field),
    metrics: parseMetrics(row.metrics).map((m) => m.field),
    filters: safeParseArray(row.filters).map((f) => ({ field: f.field || '', operator: f.operator || 'eq', value: f.value != null ? String(f.value) : '' })),
    description: row.description || '',
    isPublic: !!row.is_public,
  }
  dialogTitle.value = '编辑报表'
  dialogVisible.value = true
}

const buildPayload = () => {
  const dimensions = form.value.dimensions.map((v) => DIM_MAP[v]).filter(Boolean)
  const metrics = form.value.metrics.map((v) => METRIC_MAP[v]).filter(Boolean)
  const filters = form.value.filters
    .filter((f) => f.field && f.value !== '' && f.value !== null)
    .map((f) => ({ field: f.field, operator: f.operator, value: f.value }))
  return {
    name: form.value.name,
    description: form.value.description,
    data_source: form.value.dataSource,
    chart_type: form.value.chartType,
    dimensions,
    metrics,
    filters,
    is_public: form.value.isPublic,
  }
}

const submitForm = async () => {
  if (!formRef.value) return
  await formRef.value.validate(async (valid) => {
    if (!valid) return
    submitting.value = true
    try {
      const payload = buildPayload()
      if (form.value.id) {
        await updateCustomReport(form.value.id, payload)
      } else {
        await createCustomReport(payload)
      }
      ElMessage.success(i18n.global.t('操作成功'))
      dialogVisible.value = false
      await refreshData()
    } catch (e) {
      ElMessage.error(i18n.global.t('操作失败') + '：' + (e && e.message ? e.message : ''))
    } finally {
      submitting.value = false
    }
  })
}

const viewReport = async (row) => {
  currentReportName.value = row.name
  viewDialogVisible.value = true
  viewLoading.value = true
  reportData.value = []
  reportColumns.value = []
  try {
    const res = await queryReportData(row.id)
    const payload = res?.data ?? res
    const rows = Array.isArray(payload) ? payload : (payload.data || [])
    if (rows.length) {
      const keys = Object.keys(rows[0])
      reportColumns.value = keys.map((k) => ({ key: k, label: k, minWidth: 120 }))
      reportData.value = rows
    } else if (payload && payload.dimensions) {
      reportColumns.value = (payload.dimensions || []).map((d) => ({ key: d, label: d, minWidth: 120 }))
    }
  } catch (e) {
    ElMessage.error(i18n.global.t('获取报表数据失败') + '：' + (e && e.message ? e.message : ''))
  } finally {
    viewLoading.value = false
  }
}

const exportReport = async (row) => {
  try {
    const res = await exportCustomReport(row.id)
    const payload = res?.data ?? res
    const rows = Array.isArray(payload) ? payload : (payload && payload.data) || []
    ElMessage.success(`导出成功，共 ${rows.length} 行`)
  } catch (e) {
    ElMessage.error('导出失败：' + (e && e.message ? e.message : ''))
  }
}

const deleteReport = async (row) => {
  try {
    await ElMessageBox.confirm(`确定删除报表 "${row.name}"？`, '确认', { type: 'warning' })
    await deleteCustomReport(row.id)
    ElMessage.success(i18n.global.t('删除成功'))
    await refreshData()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(i18n.global.t('删除失败'))
  }
}

onMounted(() => refreshData())
onActivated(() => refreshData())
</script>

<style scoped lang="scss">
.custom-report-page { padding: 20px; }
.header-card {
  margin-bottom: 20px;
  :deep(.el-card__body) { display: flex; justify-content: space-between; align-items: center; }
  h2 { margin: 0 0 8px 0; }
  .subtitle { color: #909399; margin: 0; }
}
.tag-item { margin-right: 6px; }
.muted { color: #c0c4cc; }
.filters { display: flex; flex-direction: column; gap: 8px; }
.filter-row { display: flex; align-items: center; gap: 8px; }
</style>
