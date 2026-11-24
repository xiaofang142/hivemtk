<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h2 class="page-title">客户导入任务</h2>
        <span class="page-sub">上传 CSV → 预检 → 确认执行 → 查看错误报告（PR-6）</span>
      </div>
      <div class="head-actions">
        <el-button :loading="loading" @click="load">刷新</el-button>
      </div>
    </div>

    <el-card shadow="never" class="block">
      <template #header>
        <div class="card-head">
          <span>导入向导</span>
          <el-tag size="small" type="info">1. 上传预检  2. 查看报告  3. 执行导入</el-tag>
        </div>
      </template>

      <el-steps :active="activeStep" finish-status="success" simple class="mb16">
        <el-step title="上传预检" />
        <el-step title="确认报告" />
        <el-step title="执行结果" />
      </el-steps>

      <el-form :model="uploadForm" label-width="100px" class="upload-form">
        <el-row :gutter="16">
          <el-col :xs="24" :sm="12" :md="8">
            <el-form-item label="实体类型">
              <el-select v-model="uploadForm.entity_type" style="width: 100%">
                <el-option label="客户 customer" value="customer" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12" :md="8">
            <el-form-item label="去重策略">
              <el-select v-model="uploadForm.dedup_strategy" style="width: 100%">
                <el-option label="跳过 skip（保留已有）" value="skip" />
                <el-option label="更新 update（覆盖已有）" value="update" />
                <el-option label="报错 error（重复即失败）" value="error" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="24" :md="8">
            <el-form-item label="CSV 文件">
              <el-upload
                ref="uploadRef"
                :auto-upload="false"
                :limit="1"
                accept=".csv,text/csv"
                :on-change="onFileChange"
                :on-remove="onFileRemove"
                style="width: 100%"
              >
                <el-button type="primary" plain>选择文件</el-button>
              </el-upload>
            </el-form-item>
          </el-col>
        </el-row>

        <el-form-item label="字段映射">
          <el-input
            v-model="uploadForm.mapping_json"
            type="textarea"
            :rows="2"
            placeholder='可选，JSON 格式，如 {"客户名":"name","手机号":"phone"}；留空使用模板表头'
          />
        </el-form-item>

        <el-form-item>
          <el-button @click="downloadTemplate">下载模板</el-button>
          <el-button type="primary" :loading="prechecking" :disabled="!selectedFile" @click="doPrecheck">预检</el-button>
          <el-button v-if="currentJobId" type="success" :loading="executing" @click="doExecute(currentJobId)">执行导入</el-button>
          <el-button v-if="currentJobId" @click="viewErrors(currentJobId)">查看错误报告</el-button>
          <el-button v-if="currentJobId" @click="downloadErrors(currentJobId)">下载错误报告</el-button>
        </el-form-item>
      </el-form>

      <el-alert
        v-if="precheckReport"
        :title="reportSummary"
        :type="reportAlertType"
        show-icon
        :closable="false"
        class="mb12"
      />

      <el-descriptions v-if="precheckReport" :column="4" border size="small" class="mb12">
        <el-descriptions-item label="任务ID">{{ precheckReport.job_id || '-' }}</el-descriptions-item>
        <el-descriptions-item label="状态">
          <el-tag :type="statusTag(precheckReport.status)">{{ precheckReport.status || 'prechecked' }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="总行数">{{ num(precheckReport.total_rows) }}</el-descriptions-item>
        <el-descriptions-item label="有效行">{{ num(precheckReport.valid_rows) }}</el-descriptions-item>
        <el-descriptions-item label="无效行">{{ num(precheckReport.invalid_rows) }}</el-descriptions-item>
        <el-descriptions-item label="重复行">{{ num(precheckReport.duplicate_rows) }}</el-descriptions-item>
        <el-descriptions-item label="成功">{{ num(precheckReport.success_rows) }}</el-descriptions-item>
        <el-descriptions-item label="失败">{{ num(precheckReport.failed_rows) }}</el-descriptions-item>
        <el-descriptions-item label="跳过">{{ num(precheckReport.skipped_rows) }}</el-descriptions-item>
        <el-descriptions-item label="更新">{{ num(precheckReport.updated_rows) }}</el-descriptions-item>
        <el-descriptions-item label="去重策略">{{ precheckReport.dedup_strategy || uploadForm.dedup_strategy }}</el-descriptions-item>
        <el-descriptions-item label="完成时间">{{ fmtTime(precheckReport.completed_at || precheckReport.generated_at) }}</el-descriptions-item>
      </el-descriptions>

      <div v-if="reportErrors.length" class="mb12">
        <div class="section-title">错误/预检明细（前 {{ reportErrorsPreview.length }} 条）</div>
        <el-table :data="reportErrorsPreview" size="small" border max-height="260">
          <el-table-column prop="row" label="行号" width="80" />
          <el-table-column prop="field" label="字段" width="140" />
          <el-table-column prop="message" label="说明" min-width="260" show-overflow-tooltip />
        </el-table>
        <div v-if="reportErrors.length > reportErrorsPreview.length" class="muted mt8">
          共 {{ reportErrors.length }} 条，已截断显示；可点击“下载错误报告”获取完整 CSV。
        </div>
      </div>
    </el-card>

    <el-card shadow="never">
      <template #header>
        <div class="card-head">
          <span>导入任务列表</span>
          <span class="muted">共 {{ total }} 条</span>
        </div>
      </template>
      <el-table v-loading="loading" :data="rows" empty-text="暂无导入任务">
        <el-table-column prop="job_id" label="任务ID" min-width="180" show-overflow-tooltip />
        <el-table-column prop="entity_type" label="实体" width="90" />
        <el-table-column prop="file_name" label="文件名" min-width="160" show-overflow-tooltip />
        <el-table-column prop="status" label="状态" width="110">
          <template #default="{ row }">
            <el-tag :type="statusTag(row.status)">{{ statusText(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="行数" min-width="220">
          <template #default="{ row }">
            <span class="stat">总 {{ num(row.total_rows) }}</span>
            <span class="stat ok">成 {{ num(row.success_rows) }}</span>
            <span class="stat bad">败 {{ num(row.failed_rows) }}</span>
            <span class="stat warn">跳 {{ num(row.skipped_rows) }}</span>
            <span class="stat">更 {{ num(row.updated_rows) }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="dedup_strategy" label="去重策略" width="100" />
        <el-table-column prop="created_at" label="创建时间" min-width="170">
          <template #default="{ row }">{{ fmtTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="240" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="loadDetail(row.job_id)">详情</el-button>
            <el-button
              link
              type="success"
              :disabled="row.status === 'completed' || row.status === 'running'"
              @click="doExecute(row.job_id)"
            >执行</el-button>
            <el-button link @click="viewErrors(row.job_id)">错误</el-button>
            <el-button link @click="downloadErrors(row.job_id)">下载</el-button>
          </template>
        </el-table-column>
      </el-table>
      <div class="pager">
        <el-pagination
          v-model:current-page="page"
          v-model:page-size="pageSize"
          :total="total"
          layout="total, prev, pager, next, sizes"
          :page-sizes="[20, 50, 100]"
          @current-change="load"
          @size-change="() => { page = 1; load() }"
        />
      </div>
    </el-card>

    <el-dialog v-model="detailVisible" title="任务详情" width="820px" destroy-on-close>
      <template v-if="detail">
        <el-descriptions :column="3" border size="small" class="mb12">
          <el-descriptions-item label="任务ID">{{ detail.job_id }}</el-descriptions-item>
          <el-descriptions-item label="状态">
            <el-tag :type="statusTag(detail.status)">{{ statusText(detail.status) }}</el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="实体">{{ detail.entity_type }}</el-descriptions-item>
          <el-descriptions-item label="文件名">{{ detail.file_name || '-' }}</el-descriptions-item>
          <el-descriptions-item label="去重策略">{{ detail.dedup_strategy || '-' }}</el-descriptions-item>
          <el-descriptions-item label="创建时间">{{ fmtTime(detail.created_at) }}</el-descriptions-item>
          <el-descriptions-item label="总行数">{{ num(detail.total_rows) }}</el-descriptions-item>
          <el-descriptions-item label="成功">{{ num(detail.success_rows) }}</el-descriptions-item>
          <el-descriptions-item label="失败">{{ num(detail.failed_rows) }}</el-descriptions-item>
          <el-descriptions-item label="跳过">{{ num(detail.skipped_rows) }}</el-descriptions-item>
          <el-descriptions-item label="更新">{{ num(detail.updated_rows) }}</el-descriptions-item>
          <el-descriptions-item label="更新时间">{{ fmtTime(detail.updated_at) }}</el-descriptions-item>
        </el-descriptions>

        <el-tabs>
          <el-tab-pane label="预检报告">
            <pre class="report-json">{{ prettyJson(detail.precheck_report) }}</pre>
          </el-tab-pane>
          <el-tab-pane label="错误报告">
            <div class="mb12">
              <el-button size="small" @click="downloadErrors(detail.job_id)">下载错误 CSV</el-button>
            </div>
            <el-table :data="parseErrors(detail.error_report)" size="small" border max-height="320">
              <el-table-column prop="row" label="行号" width="80" />
              <el-table-column prop="field" label="字段" width="140" />
              <el-table-column prop="message" label="说明" min-width="260" show-overflow-tooltip />
            </el-table>
          </el-tab-pane>
          <el-tab-pane label="原始JSON">
            <pre class="report-json">{{ JSON.stringify(detail, null, 2) }}</pre>
          </el-tab-pane>
        </el-tabs>
      </template>
    </el-dialog>

    <el-dialog v-model="errorsVisible" title="错误报告" width="820px" destroy-on-close>
      <div class="mb12 report-actions">
        <el-button size="small" @click="downloadErrors(currentErrorJobId)">下载错误 CSV</el-button>
        <span class="muted">任务: {{ currentErrorJobId }}</span>
      </div>
      <el-table :data="errorRows" size="small" border max-height="420" empty-text="无错误记录">
        <el-table-column prop="row" label="行号" width="80" />
        <el-table-column prop="field" label="字段" width="140" />
        <el-table-column prop="message" label="说明" min-width="260" show-overflow-tooltip />
      </el-table>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { customerImportJobApi } from '@/api/customerImportJob'

const loading = ref(false)
const prechecking = ref(false)
const executing = ref(false)
const rows = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const uploadRef = ref()
const selectedFile = ref(null)
const precheckReport = ref(null)
const detailVisible = ref(false)
const detail = ref(null)
const errorsVisible = ref(false)
const errorRows = ref([])
const currentErrorJobId = ref('')

const uploadForm = reactive({
  entity_type: 'customer',
  dedup_strategy: 'skip',
  mapping_json: '{}',
})

const currentJobId = computed(() => precheckReport.value?.job_id || '')
const reportErrors = computed(() => normalizeErrors(precheckReport.value?.errors))
const reportErrorsPreview = computed(() => reportErrors.value.slice(0, 50))
const activeStep = computed(() => {
  if (!precheckReport.value) return 0
  if (['completed', 'failed', 'running'].includes(precheckReport.value.status)) return 3
  return 1
})
const reportSummary = computed(() => {
  const r = precheckReport.value
  if (!r) return ''
  const status = r.status || 'prechecked'
  return `任务 ${r.job_id || '-'} 状态=${statusText(status)}，总行 ${num(r.total_rows)}，有效 ${num(r.valid_rows)}，无效 ${num(r.invalid_rows)}，重复 ${num(r.duplicate_rows)}，成功 ${num(r.success_rows)}，失败 ${num(r.failed_rows)}，跳过 ${num(r.skipped_rows)}，更新 ${num(r.updated_rows)}`
})
const reportAlertType = computed(() => {
  const r = precheckReport.value
  if (!r) return 'info'
  if (r.status === 'failed') return 'error'
  if (r.status === 'completed') return 'success'
  if (Number(r.invalid_rows || r.failed_rows || 0) > 0) return 'warning'
  return 'success'
})

function num(v) {
  const n = Number(v)
  return Number.isFinite(n) ? n : 0
}

function fmtTime(v) {
  if (!v) return '-'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return String(v)
  return d.toLocaleString('zh-CN', { hour12: false })
}

function statusText(status) {
  const map = {
    pending: '待处理',
    prechecked: '已预检',
    running: '执行中',
    completed: '已完成',
    failed: '失败',
  }
  return map[status] || status || '-'
}

function statusTag(status) {
  const map = { pending: 'info', prechecked: 'warning', running: 'warning', completed: 'success', failed: 'danger' }
  return map[status] || 'info'
}

function normalizeErrors(raw) {
  if (Array.isArray(raw)) return raw
  if (typeof raw === 'string' && raw.trim()) {
    try {
      const parsed = JSON.parse(raw)
      return Array.isArray(parsed) ? parsed : []
    } catch {
      return [{ row: 0, field: '', message: raw }]
    }
  }
  return []
}

function parseErrors(raw) {
  return normalizeErrors(raw)
}

function prettyJson(raw) {
  if (!raw) return '无'
  if (typeof raw === 'string') {
    try { return JSON.stringify(JSON.parse(raw), null, 2) } catch { return raw }
  }
  try { return JSON.stringify(raw, null, 2) } catch { return String(raw) }
}

function onFileChange(file) {
  selectedFile.value = file?.raw || null
}

function onFileRemove() {
  selectedFile.value = null
}

async function load() {
  loading.value = true
  try {
    const res = await customerImportJobApi.list({ page: page.value, page_size: pageSize.value })
    rows.value = Array.isArray(res?.list) ? res.list : []
    total.value = Number(res?.total || 0)
  } finally {
    loading.value = false
  }
}

async function downloadTemplate() {
  try {
    const blob = await customerImportJobApi.template(uploadForm.entity_type)
    const data = blob instanceof Blob ? blob : new Blob([blob], { type: 'text/csv;charset=utf-8' })
    const url = URL.createObjectURL(data)
    const a = document.createElement('a')
    a.href = url
    a.download = `customer_import_${uploadForm.entity_type}_template.csv`
    a.click()
    URL.revokeObjectURL(url)
  } catch (e) {
    ElMessage.error(e?.message || '下载模板失败')
  }
}

function validateMappingJson() {
  const text = (uploadForm.mapping_json || '').trim()
  if (!text) return '{}'
  try {
    const parsed = JSON.parse(text)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      throw new Error('mapping_json 必须是 JSON 对象')
    }
    return JSON.stringify(parsed)
  } catch (e) {
    throw new Error(e?.message || 'mapping_json 格式错误')
  }
}

async function doPrecheck() {
  if (!selectedFile.value) {
    ElMessage.warning('请先选择 CSV 文件')
    return
  }
  let mappingJson
  try {
    mappingJson = validateMappingJson()
  } catch (e) {
    ElMessage.warning(e.message)
    return
  }
  prechecking.value = true
  try {
    const fd = new FormData()
    fd.append('file', selectedFile.value)
    fd.append('entity_type', uploadForm.entity_type)
    fd.append('dedup_strategy', uploadForm.dedup_strategy)
    fd.append('mapping_json', mappingJson)
    const res = await customerImportJobApi.precheck(fd)
    precheckReport.value = res
    ElMessage.success('预检完成')
    await load()
  } catch (e) {
    ElMessage.error(e?.message || '预检失败')
  } finally {
    prechecking.value = false
  }
}

async function doExecute(jobId) {
  if (!jobId) return
  executing.value = true
  try {
    const res = await customerImportJobApi.execute(jobId)
    precheckReport.value = res
    ElMessage.success('导入执行完成')
    await load()
  } catch (e) {
    ElMessage.error(e?.message || '导入执行失败')
  } finally {
    executing.value = false
  }
}

async function loadDetail(jobId) {
  try {
    detail.value = await customerImportJobApi.get(jobId)
    detailVisible.value = true
  } catch (e) {
    ElMessage.error(e?.message || '获取任务详情失败')
  }
}

function downloadCsv(filename, rows) {
  const header = ['row', 'field', 'message']
  const escape = (v) => {
    const s = String(v ?? '')
    if (/[",\n]/.test(s)) return `"${s.replace(/"/g, '""')}"`
    return s
  }
  const lines = [header.join(',')]
  rows.forEach((r) => {
    lines.push([r.row ?? 0, r.field ?? '', r.message ?? ''].map(escape).join(','))
  })
  const blob = new Blob(['\uFEFF' + lines.join('\n')], { type: 'text/csv;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  a.click()
  URL.revokeObjectURL(url)
}

async function viewErrors(jobId) {
  currentErrorJobId.value = jobId
  try {
    const res = await customerImportJobApi.errors(jobId)
    errorRows.value = normalizeErrors(res?.errors)
    errorsVisible.value = true
  } catch (e) {
    ElMessage.error(e?.message || '获取错误报告失败')
  }
}

async function downloadErrors(jobId) {
  if (!jobId) return
  try {
    const blob = await customerImportJobApi.downloadErrors(jobId)
    const data = blob instanceof Blob ? blob : new Blob([blob], { type: 'text/csv;charset=utf-8' })
    const url = URL.createObjectURL(data)
    const a = document.createElement('a')
    a.href = url
    a.download = `customer_import_${jobId}_errors.csv`
    a.click()
    URL.revokeObjectURL(url)
  } catch {
    try {
      const res = await customerImportJobApi.errors(jobId)
      const errs = normalizeErrors(res?.errors)
      downloadCsv(`customer_import_${jobId}_errors.csv`, errs)
    } catch (e) {
      ElMessage.error(e?.message || '下载错误报告失败')
    }
  }
}

onMounted(load)
</script>

<style scoped>
.page { padding: 16px; }
.page-head { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 16px; gap: 12px; }
.page-title { margin: 0; font-size: 20px; font-weight: 600; }
.page-sub { color: var(--el-text-color-secondary); font-size: 13px; }
.head-actions { display: flex; gap: 8px; align-items: center; }
.block { margin-bottom: 16px; }
.mb12 { margin-bottom: 12px; }
.mb16 { margin-bottom: 16px; }
.mt8 { margin-top: 8px; }
.card-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.muted { color: var(--el-text-color-secondary); font-size: 12px; }
.section-title { font-size: 13px; font-weight: 600; margin-bottom: 8px; }
.report-actions { display: flex; align-items: center; gap: 12px; }
.report-json { background: #f6f8fa; border: 1px solid #e5e7eb; padding: 12px; border-radius: 6px; max-height: 360px; overflow: auto; font-size: 12px; line-height: 1.5; margin: 0; }
.pager { margin-top: 12px; display: flex; justify-content: flex-end; }
.stat { margin-right: 8px; font-size: 12px; color: var(--el-text-color-regular); }
.stat.ok { color: var(--el-color-success); }
.stat.bad { color: var(--el-color-danger); }
.stat.warn { color: var(--el-color-warning); }
.upload-form :deep(.el-upload) { width: 100%; }
</style>
