<template>
  <div class="ab-experiment">
    <el-card class="filter-container">
      <div class="stats-row" v-if="stats">
        <div class="stat-card">
          <div class="stat-value">{{ stats.running }}</div>
          <div class="stat-label">{{ t('ab.running') }}</div>
        </div>
        <div class="stat-card">
          <div class="stat-value">{{ stats.completed }}</div>
          <div class="stat-label">{{ t('ab.completed') }}</div>
        </div>
        <div class="stat-card">
          <div class="stat-value">{{ stats.winner }}</div>
          <div class="stat-label">{{ t('ab.winner') }}</div>
        </div>
        <div class="stat-card">
          <div class="stat-value">{{ stats.totalUsers }}</div>
          <div class="stat-label">{{ t('ab.totalUsers') }}</div>
        </div>
      </div>

      <el-form :inline="true" class="filter-form">
        <el-form-item :label="t('ab.status')">
          <el-select v-model="statusFilter" :placeholder="t('common.all')" clearable style="width: 140px">
            <el-option label="Draft" value="draft" />
            <el-option label="Running" value="running" />
            <el-option label="Paused" value="paused" />
            <el-option label="Completed" value="completed" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('ab.experimentName')">
          <el-input v-model="searchKeyword" :placeholder="t('ab.experimentNamePlaceholder')" clearable style="width: 200px" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="handleSearch">{{ t('common.search') }}</el-button>
          <el-button @click="handleReset">{{ t('common.reset') }}</el-button>
          <el-button @click="refreshData">{{ t('common.refresh') }}</el-button>
          <el-button type="success" @click="showCreateDialog">{{ t('ab.newExperiment') }}</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <el-card>
      <el-table :data="filteredExperiments" v-loading="loading" stripe>
        <el-table-column prop="name" :label="t('ab.experimentName')" min-width="160" />
        <el-table-column :label="t('ab.sourceType')" min-width="160">
          <template #default="{ row }">
            <span>{{ row.source_type || '-' }}</span>
            <span v-if="row.source_id" class="source-id">#{{ row.source_id }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('ab.trafficSplit')" width="120">
          <template #default="{ row }">{{ row.traffic_split || 0 }}%</template>
        </el-table-column>
        <el-table-column :label="t('ab.status')" width="120">
          <template #default="{ row }">
            <el-tag :type="statusTagType(row.status)">{{ statusLabel(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('ab.startDate')" width="120">
          <template #default="{ row }">{{ fmtDate(row.start_date) }}</template>
        </el-table-column>
        <el-table-column :label="t('ab.endDate')" width="120">
          <template #default="{ row }">{{ fmtDate(row.end_date) }}</template>
        </el-table-column>
        <el-table-column :label="t('ab.operation')" width="240" fixed="right">
          <template #default="{ row }">
            <el-button size="small" @click="showDetail(row)">{{ t('ab.detail') }}</el-button>
            <el-button size="small" type="primary" @click="showEditDialog(row)">{{ t('ab.edit') }}</el-button>
            <el-button
              v-if="row.status === 'running'"
              size="small"
              type="warning"
              @click="handlePause(row)"
            >{{ t('ab.pause') }}</el-button>
            <el-button
              v-else-if="row.status !== 'completed'"
              size="small"
              type="success"
              @click="handleStart(row)"
            >{{ t('ab.resume') }}</el-button>
            <el-button size="small" type="danger" @click="handleDelete(row)">{{ t('ab.delete') }}</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    
    <el-dialog :title="editingId ? t('ab.edit') : t('ab.create')" v-model="createDialogVisible" width="720px">
      <el-form :model="createForm" label-width="120px">
        <el-form-item :label="t('ab.experimentName')" required>
          <el-input v-model="createForm.name" :placeholder="t('ab.experimentNamePlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('ab.sourceType')" required>
          <el-select v-model="createForm.source_type" :placeholder="t('ab.sourceTypePlaceholder')" style="width: 100%">
            <el-option :label="t('ab.sourceTypePage')" value="page" />
            <el-option :label="t('ab.sourceTypeComponent')" value="component" />
            <el-option :label="t('ab.sourceTypeMessage')" value="message" />
            <el-option :label="t('ab.sourceTypeOther')" value="other" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('ab.sourceId')" required>
          <el-input v-model="createForm.source_id" :placeholder="t('ab.sourceIdPlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('ab.description')">
          <el-input v-model="createForm.description" type="textarea" :rows="2" :placeholder="t('ab.descriptionPlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('ab.trafficSplit')">
          <el-slider v-model="createForm.traffic_split" :min="0" :max="100" show-input style="width: 100%" />
        </el-form-item>
        <el-form-item :label="t('ab.startDate')">
          <el-date-picker v-model="createForm.start_date" type="date" :placeholder="t('ab.startDate')" value-format="YYYY-MM-DD" />
        </el-form-item>
        <el-form-item :label="t('ab.endDate')">
          <el-date-picker v-model="createForm.end_date" type="date" :placeholder="t('ab.endDate')" value-format="YYYY-MM-DD" />
        </el-form-item>
        <el-form-item :label="t('ab.addVariant')">
          <div class="variants">
            <div v-for="(v, idx) in createForm.variants" :key="idx" class="variant-row">
              <el-input v-model="v.name" :placeholder="t('ab.variantName')" style="width: 140px" />
              <el-checkbox v-model="v.is_control">{{ t('ab.isControl') }}</el-checkbox>
              <el-input-number v-model="v.weight" :min="0" :max="100" :placeholder="t('ab.weight')" />
              <el-button type="danger" text @click="removeVariant(idx)">{{ t('ab.removeVariant') }}</el-button>
            </div>
            <el-button @click="addVariant">{{ t('ab.addVariant') }}</el-button>
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createDialogVisible = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="submitting" @click="submitForm">{{ t('common.confirm') }}</el-button>
      </template>
    </el-dialog>

    
    <el-dialog :title="t('ab.detail')" v-model="detailDialogVisible" width="760px">
      <template v-if="detailData">
        <el-descriptions :column="2" border>
          <el-descriptions-item :label="t('ab.experimentName')">{{ detailData.name }}</el-descriptions-item>
          <el-descriptions-item :label="t('ab.status')">
            <el-tag :type="statusTagType(detailData.status)">{{ statusLabel(detailData.status) }}</el-tag>
          </el-descriptions-item>
          <el-descriptions-item :label="t('ab.sourceType')">{{ detailData.source_type }}</el-descriptions-item>
          <el-descriptions-item :label="t('ab.sourceId')">{{ detailData.source_id }}</el-descriptions-item>
          <el-descriptions-item :label="t('ab.trafficSplit')">{{ detailData.traffic_split || 0 }}%</el-descriptions-item>
          <el-descriptions-item :label="t('ab.startDate')">{{ fmtDate(detailData.start_date) }}</el-descriptions-item>
          <el-descriptions-item :label="t('ab.endDate')">{{ fmtDate(detailData.end_date) }}</el-descriptions-item>
          <el-descriptions-item :label="t('ab.description')">{{ detailData.description || '-' }}</el-descriptions-item>
        </el-descriptions>

        <div class="results-title">{{ t('ab.results') }}</div>
        <el-table :data="results" v-loading="resultsLoading" stripe empty-text="No data">
          <el-table-column prop="variant_name" :label="t('ab.resultVariant')" />
          <el-table-column prop="traffic_count" :label="t('ab.resultTraffic')" />
          <el-table-column prop="conversion_count" :label="t('ab.resultConversion')" />
          <el-table-column :label="t('ab.resultRate')">
            <template #default="{ row }">{{ (row.conversion_rate || 0).toFixed(2) }}%</template>
          </el-table-column>
          <el-table-column :label="t('ab.winner')">
            <template #default="{ row }">
              <el-tag v-if="row.is_winner" type="success">Winner</el-tag>
              <span v-else>-</span>
            </template>
          </el-table-column>
        </el-table>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onActivated } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  getExperiments,
  createExperiment,
  updateExperiment,
  deleteExperiment,
  startExperiment,
  pauseExperiment,
  getExperimentDetail,
  getExperimentResults,
} from '@/api/abExperiment.js'

const { t } = useI18n()

const experiments = ref([])
const loading = ref(false)
const statusFilter = ref('')
const searchKeyword = ref('')
const createDialogVisible = ref(false)
const detailDialogVisible = ref(false)
const editingId = ref(null)
const submitting = ref(false)
const detailData = ref(null)
const results = ref([])
const resultsLoading = ref(false)

const stats = reactive({ running: 0, completed: 0, winner: 0, totalUsers: 0 })

const createForm = reactive({
  name: '',
  source_type: '',
  source_id: '',
  description: '',
  traffic_split: 50,
  start_date: null,
  end_date: null,
  variants: [
    { name: 'A', is_control: true, weight: 50 },
    { name: 'B', is_control: false, weight: 50 },
  ],
})

const filteredExperiments = computed(() => {
  return experiments.value.filter((e) => {
    if (statusFilter.value && e.status !== statusFilter.value) return false
    if (searchKeyword.value && !(e.name || '').toLowerCase().includes(searchKeyword.value.toLowerCase())) return false
    return true
  })
})

function computeStats() {
  stats.running = experiments.value.filter((e) => e.status === 'running').length
  stats.completed = experiments.value.filter((e) => e.status === 'completed').length
  stats.winner = 0
  stats.totalUsers = 0
}

async function refreshData() {
  loading.value = true
  try {
    const res = await getExperiments({ page: 1, page_size: 100 })
    experiments.value = res.list || res.data || []
    computeStats()
  } catch (e) {
    ElMessage.error('加载失败：' + (e && e.message ? e.message : ''))
  } finally {
    loading.value = false
  }
}

function handleSearch() {}
function handleReset() {
  statusFilter.value = ''
  searchKeyword.value = ''
}

function statusTagType(status) {
  if (status === 'running') return 'success'
  if (status === 'paused') return 'warning'
  if (status === 'completed') return 'info'
  return ''
}
function statusLabel(status) {
  if (status === 'running') return t('ab.statusRunning')
  if (status === 'paused') return t('ab.statusPaused')
  if (status === 'completed') return t('ab.statusCompleted')
  if (status === 'draft') return t('ab.statusDraft')
  return status || '-'
}

function fmtDate(d) {
  if (!d) return '-'
  const dt = new Date(d)
  if (isNaN(dt.getTime())) return '-'
  const y = dt.getFullYear()
  const m = String(dt.getMonth() + 1).padStart(2, '0')
  const day = String(dt.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

function addVariant() {
  createForm.variants.push({ name: '', is_control: false, weight: 50 })
}
function removeVariant(idx) {
  if (createForm.variants.length > 1) createForm.variants.splice(idx, 1)
}

function resetForm() {
  createForm.name = ''
  createForm.source_type = ''
  createForm.source_id = ''
  createForm.description = ''
  createForm.traffic_split = 50
  createForm.start_date = null
  createForm.end_date = null
  createForm.variants = [
    { name: 'A', is_control: true, weight: 50 },
    { name: 'B', is_control: false, weight: 50 },
  ]
}

function showCreateDialog() {
  editingId.value = null
  resetForm()
  createDialogVisible.value = true
}

async function showEditDialog(row) {
  editingId.value = row.id
  resetForm()
  try {
    const detail = await getExperimentDetail(row.id)
    createForm.name = detail.name || ''
    createForm.source_type = detail.source_type || ''
    createForm.source_id = detail.source_id || ''
    createForm.description = detail.description || ''
    createForm.traffic_split = detail.traffic_split || 50
    createForm.start_date = detail.start_date ? fmtDate(detail.start_date) : null
    createForm.end_date = detail.end_date ? fmtDate(detail.end_date) : null
  } catch (e) {
    ElMessage.error('加载详情失败：' + (e && e.message ? e.message : ''))
  }
  createDialogVisible.value = true
}

function buildPayload() {
  return {
    name: createForm.name,
    source_type: createForm.source_type,
    source_id: createForm.source_id,
    description: createForm.description,
    traffic_split: createForm.traffic_split || 0,
    start_date: createForm.start_date || null,
    end_date: createForm.end_date || null,
    variants: createForm.variants.map((v) => ({
      name: v.name,
      is_control: !!v.is_control,
      weight: v.weight || 0,
    })),
  }
}

async function submitForm() {
  if (!createForm.name) return ElMessage.warning(t('ab.experimentNamePlaceholder'))
  if (!createForm.source_type) return ElMessage.warning(t('ab.sourceTypePlaceholder'))
  if (!createForm.source_id) return ElMessage.warning(t('ab.sourceIdPlaceholder'))
  submitting.value = true
  try {
    const payload = buildPayload()
    if (editingId.value) {
      await updateExperiment(editingId.value, payload)
      ElMessage.success(t('common.updateSuccess'))
    } else {
      await createExperiment(payload)
      ElMessage.success(t('common.createSuccess'))
    }
    createDialogVisible.value = false
    await refreshData()
  } catch (e) {
    ElMessage.error('保存失败：' + (e && e.message ? e.message : ''))
  } finally {
    submitting.value = false
  }
}

async function handleStart(row) {
  try {
    await startExperiment(row.id)
    ElMessage.success(t('common.operationSuccess'))
    await refreshData()
  } catch (e) {
    ElMessage.error('操作失败：' + (e && e.message ? e.message : ''))
  }
}

async function handlePause(row) {
  try {
    await pauseExperiment(row.id)
    ElMessage.success(t('common.operationSuccess'))
    await refreshData()
  } catch (e) {
    ElMessage.error('操作失败：' + (e && e.message ? e.message : ''))
  }
}

async function handleDelete(row) {
  try {
    await ElMessageBox.confirm(t('common.confirmDelete'), t('common.warning'), {
      type: 'warning',
      confirmButtonText: t('common.confirm'),
      cancelButtonText: t('common.cancel'),
    })
  } catch {
    return
  }
  try {
    await deleteExperiment(row.id)
    ElMessage.success(t('common.deleteSuccess'))
    await refreshData()
  } catch (e) {
    ElMessage.error('删除失败：' + (e && e.message ? e.message : ''))
  }
}

async function showDetail(row) {
  detailData.value = row
  detailDialogVisible.value = true
  resultsLoading.value = true
  results.value = []
  try {
    const res = await getExperimentResults(row.id)
    results.value = Array.isArray(res) ? res : (res.list || [])
  } catch (e) { /* 忽略：清理/存储/恢复类 best-effort 操作 */ } finally {
    resultsLoading.value = false
  }
}

onMounted(() => {
  refreshData()
})

onActivated(() => {
  refreshData()
})
</script>

<style scoped>
.filter-container { margin-bottom: 16px; }
.stats-row { display: flex; gap: 16px; margin-bottom: 16px; }
.stat-card {
  flex: 1;
  background: #f5f7fa;
  border-radius: 8px;
  padding: 16px;
  text-align: center;
}
.stat-value { font-size: 28px; font-weight: 600; color: #409eff; }
.stat-label { font-size: 13px; color: #909399; margin-top: 4px; }
.source-id { color: #909399; margin-left: 6px; font-size: 12px; }
.variants { display: flex; flex-direction: column; gap: 8px; }
.variant-row { display: flex; align-items: center; gap: 12px; }
.results-title { font-weight: 600; margin: 16px 0 8px; }
</style>
