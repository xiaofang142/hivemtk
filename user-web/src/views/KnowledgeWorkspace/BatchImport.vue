<template>
  <div class="batch-import-page">
    <el-alert
      :title="$t('批量导入知识库文档')"
      type="info"
      :closable="false"
      show-icon
    >
      <template #default>
        支持两种方式:
        <strong>1) 文件上传</strong> CSV / JSON / Excel(纯文本),
        <strong>2) 文本批量粘贴</strong> JSON 数组直接提交。
        每行记录包含 title、content、category、tags 四个字段(除 content 外均可选)。
      </template>
    </el-alert>

    <el-row
      :gutter="16"
      style="margin-top: 16px"
    >
      <el-col :span="14">
        <el-card>
          <el-tabs
            v-model="activeTab"
            @tab-change="handleTabChange"
          >
            <el-tab-pane
              label="文件上传"
              name="upload"
            >
              <el-form
                label-width="100px"
                :model="uploadForm"
              >
                <el-form-item
                  label="产品"
                  required
                >
                  <el-select
                    v-model="uploadForm.product_id"
                    placeholder="选择产品"
                    style="width: 100%"
                    @change="loadPreview"
                  >
                    <el-option
                      v-for="p in productList"
                      :key="p.id"
                      :label="p.name"
                      :value="p.id"
                    />
                  </el-select>
                </el-form-item>
                <el-form-item label="格式">
                  <el-radio-group v-model="uploadForm.format">
                    <el-radio-button label="auto">
                      自动识别
                    </el-radio-button>
                    <el-radio-button label="csv">
                      CSV
                    </el-radio-button>
                    <el-radio-button label="json">
                      JSON
                    </el-radio-button>
                  </el-radio-group>
                </el-form-item>
                <el-form-item
                  label="文件"
                  required
                >
                  <el-upload
                    ref="uploadRef"
                    :auto-upload="false"
                    :limit="1"
                    :on-change="handleFileChange"
                    :on-exceed="handleExceed"
                    accept=".csv,.json,.txt"
                    drag
                  >
                    <el-icon class="el-icon--upload">
                      <upload-filled />
                    </el-icon>
                    <div class="el-upload__text">
                      将文件拖到此处,或<em>点击上传</em>
                    </div>
                    <template #tip>
                      <div class="el-upload__tip">
                        支持 .csv / .json / .txt,单个文件不超过 {{ maxUploadMB }}MB
                      </div>
                    </template>
                  </el-upload>
                </el-form-item>
              </el-form>
              <el-button
                type="primary"
                :icon="View"
                :disabled="!uploadForm.file"
                style="margin-top: 8px"
                @click="loadPreview"
              >
                预览解析结果
              </el-button>
            </el-tab-pane>

            
            <el-tab-pane
              label="JSON 粘贴"
              name="paste"
            >
              <el-form label-width="100px">
                <el-form-item
                  label="产品"
                  required
                >
                  <el-select
                    v-model="pasteForm.product_id"
                    placeholder="选择产品"
                    style="width: 100%"
                  >
                    <el-option
                      v-for="p in productList"
                      :key="p.id"
                      :label="p.name"
                      :value="p.id"
                    />
                  </el-select>
                </el-form-item>
                <el-form-item
                  label="JSON 内容"
                  required
                >
                  <el-input
                    v-model="pasteForm.jsonText"
                    type="textarea"
                    :rows="12"
                    placeholder="示例:&#10;[&#10;  {&quot;title&quot;: &quot;如何退货&quot;, &quot;content&quot;: &quot;请在订单页面申请&quot;, &quot;category&quot;: &quot;售后&quot;, &quot;tags&quot;: [&quot;退货&quot;,&quot;流程&quot;]},&#10;  {&quot;title&quot;: &quot;产品 A 介绍&quot;, &quot;content&quot;: &quot;...&quot;}&#10;]"
                  />
                </el-form-item>
                <el-form-item>
                  <el-button
                    :icon="View"
                    @click="loadPastePreview"
                  >
                    解析预览
                  </el-button>
                  <el-button @click="loadPasteTemplate">
                    载入模板
                  </el-button>
                </el-form-item>
              </el-form>
            </el-tab-pane>
          </el-tabs>
        </el-card>
      </el-col>

      
      <el-col :span="10">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>解析预览</span>
              <el-tag
                v-if="preview.length"
                type="info"
              >
                共 {{ preview.length }} 条
              </el-tag>
            </div>
          </template>
          <el-table
            :data="preview"
            max-height="320"
            size="small"
            empty-text="先选择产品和文件(或粘贴 JSON),点击「预览解析结果」"
          >
            <el-table-column
              prop="title"
              label="标题"
              min-width="120"
              show-overflow-tooltip
            />
            <el-table-column
              prop="category"
              label="分类"
              width="80"
            />
            <el-table-column
              label="内容预览"
              min-width="160"
              show-overflow-tooltip
            >
              <template #default="{ row }">
                <span class="content-prev">{{ truncateText(row.content, 60) }}</span>
              </template>
            </el-table-column>
            <el-table-column
              label="标签"
              width="120"
              show-overflow-tooltip
            >
              <template #default="{ row }">
                <span v-if="row.tags && row.tags.length">{{ row.tags.join(', ') }}</span>
                <span v-else>-</span>
              </template>
            </el-table-column>
          </el-table>
          <div style="margin-top: 12px; text-align: right">
            <el-button
              type="primary"
              :icon="Upload"
              :loading="importing"
              :disabled="!canImport"
              @click="handleBatchImport"
            >
              确认导入 ({{ preview.length }})
            </el-button>
          </div>
        </el-card>
      </el-col>
    </el-row>

    
    <el-card
      v-if="lastResult"
      style="margin-top: 16px"
    >
      <template #header>
        <div class="card-header">
          <span>
            <el-icon style="vertical-align: middle"><Upload /></el-icon>
            {{ $t('批量导入已受理') }}
            <el-text
              type="info"
              size="small"
              style="margin-left: 8px"
            >批次号: {{ lastResult.batch_no }}</el-text>
          </span>
          <el-button
            link
            type="primary"
            @click="closeResult"
          >
            关闭
          </el-button>
        </div>
      </template>
      <el-text
        type="info"
        size="small"
      >
        {{ $t('已受理文档仍在后台处理中,当前不代表索引已就绪') }}
      </el-text>
      <el-row :gutter="16">
        <el-col :span="6">
          <el-statistic
            title="总数"
            :value="lastResult.total"
          />
        </el-col>
        <el-col :span="6">
          <el-statistic
            :title="$t('已受理')"
            :value="lastResult.accepted"
            :value-style="{ color: '#10B981' }"
          />
        </el-col>
        <el-col :span="6">
          <el-statistic
            title="失败"
            :value="lastResult.rejected"
            :value-style="{ color: '#EF4444' }"
          />
        </el-col>
        <el-col :span="6">
          <el-statistic
            :title="$t('受理率')"
            :value="acceptanceRate"
            suffix="%"
            :precision="1"
          />
        </el-col>
      </el-row>
      <el-divider
        v-if="lastResult.errors && lastResult.errors.length"
        content-position="left"
      >
        失败明细
      </el-divider>
      <el-table
        v-if="lastResult.errors && lastResult.errors.length"
        :data="lastResult.errors"
        max-height="200"
        size="small"
        border
      >
        <el-table-column
          type="index"
          label="序号"
          width="60"
        />
        <el-table-column
          label="错误信息"
        >
          <template #default="scope">
            {{ scope.row }}
          </template>
        </el-table-column>
      </el-table>
      <el-divider
        v-if="indexDocuments.length"
        content-position="left"
      >
        {{ $t('文档索引状态') }}
      </el-divider>
      <div
        v-if="indexDocuments.length"
        class="progress-toolbar"
      >
        <el-text
          type="info"
          size="small"
        >
          {{ $t('自动查询最多进行{count}轮,之后可手动刷新', { count: MAX_PROGRESS_POLLS }) }}
        </el-text>
        <el-button
          size="small"
          @click="refreshProgress"
        >
          {{ $t('刷新进度') }}
        </el-button>
      </div>
      <el-table
        v-if="indexDocuments.length"
        :data="indexDocuments"
        size="small"
        border
        row-key="id"
      >
        <el-table-column
          :label="$t('文档标题')"
          min-width="140"
        >
          <template #default="{ row }">
            <span
              :data-testid="`batch-document-${row.id}`"
              :data-status="row.status"
            >{{ row.title }}</span>
          </template>
        </el-table-column>
        <el-table-column
          :label="$t('索引状态')"
          width="130"
        >
          <template #default="{ row }">
            <el-tag :type="getStatusTagType(row.status, EMBED_STATUS)">
              {{ $t(getStatusLabel(row.status, EMBED_STATUS)) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          :label="$t('处理进度')"
          width="180"
        >
          <template #default="{ row }">
            <template v-if="row.status === 'processing'">
              <el-progress
                :percentage="progressPercent(row.progress)"
                :stroke-width="8"
              />
              <span>{{ progressPercent(row.progress) }}%</span>
            </template>
            <span v-else-if="row.status === 'indexed'">100%</span>
            <span v-else>—</span>
          </template>
        </el-table-column>
        <el-table-column
          :label="$t('状态信息')"
          min-width="180"
        >
          <template #default="{ row }">
            {{ row.status === 'failed' ? $t('索引失败，可点击重试') : row.poll_error ? $t('状态查询失败，请点击刷新进度') : '—' }}
          </template>
        </el-table-column>
        <el-table-column
          :label="$t('操作')"
          width="110"
        >
          <template #default="{ row }">
            <el-button
              v-if="row.status === 'failed'"
              :data-testid="`retry-document-${row.id}`"
              link
              type="primary"
              :loading="retryingDocumentID === row.id"
              @click="retryDocument(row)"
            >
              {{ $t('重试') }}
            </el-button>
          </template>
        </el-table-column>
        <template #empty>
          <span>{{ $t('暂无数据') }}</span>
        </template>
      </el-table>
    </el-card>

    
    <el-card style="margin-top: 16px">
      <template #header>
        <div class="card-header">
          <span>CSV 格式说明</span>
        </div>
      </template>
      <pre class="csv-help">
title,content,category,tags
"如何退货","请在订单页面申请",售后,"退货,流程"
"产品 A 介绍","产品 A 是我们的明星产品",产品,"产品A,介绍"</pre>
      <p style="color: #909399; font-size: 12px">
        第一行为表头,必须包含 <code>content</code> 列(标题、分类、标签可选)。
        多标签用英文逗号分隔。
      </p>
    </el-card>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, reactive, computed, onMounted, onBeforeUnmount } from 'vue'
import { useListEditorChange } from '@/composables/useListEditor'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Upload, View, UploadFilled } from '@element-plus/icons-vue'
import { knowledgeMerchantAPI } from '@/api/knowledgeMerchant'
import { knowledgeAPI } from '@/api/knowledge'
import { ragProductConfigAPI } from '@/api/ragProductConfig'
import { EMBED_STATUS, getStatusLabel, getStatusTagType } from '@/constants/status'
import { rtNum } from '@/utils/runtimeConfig'
import { isUserReportedError } from '@/utils/errorReporting'
import { parseBatchPreview } from './batchImportParser'

const activeTab = ref('upload')
const markChanged = useListEditorChange('/knowledge/management')
const _uploading = ref(false)
const importing = ref(false)

const productList = ref([])
const preview = ref([])
const lastResult = ref(null)
const indexDocuments = ref([])
const retryingDocumentID = ref(null)

const PROGRESS_POLL_INTERVAL_MS = 2000
const MAX_PROGRESS_POLLS = 60
const MAX_PROGRESS_CONCURRENCY = 4
let progressPollTimer = null
let progressPollCount = 0
let progressPollInFlight = false
let progressPollGeneration = 0
let componentDisposed = false

const uploadForm = reactive({
  product_id: '',
  format: 'auto',
  file: null
})

const pasteForm = reactive({
  product_id: '',
  jsonText: ''
})

const canImport = computed(() => preview.value.length > 0 && !importing.value)
const acceptanceRate = computed(() => {
  if (!lastResult.value || !lastResult.value.total) return 0
  return (lastResult.value.accepted / lastResult.value.total) * 100
})

const loadProducts = async () => {
  try {
    const res = await ragProductConfigAPI.listProducts()
    if (Array.isArray(res)) productList.value = res
    else if (res?.items) productList.value = res.items
  } catch (e) {
    console.error('加载产品列表失败:', e)
  }
}

const handleTabChange = (_tab) => {
  stopProgressPolling()
  preview.value = []
  lastResult.value = null
  indexDocuments.value = []
}

const clearProgressPollTimer = () => {
  if (progressPollTimer !== null) {
    clearTimeout(progressPollTimer)
    progressPollTimer = null
  }
}

const stopProgressPolling = () => {
  progressPollGeneration++
  clearProgressPollTimer()
}

const closeResult = () => {
  stopProgressPolling()
  lastResult.value = null
  indexDocuments.value = []
}

const isTerminalStatus = (status) => status === 'indexed' || status === 'failed'
const progressPercent = (value) => Math.max(0, Math.min(100, Number(value) || 0))

const pollDocumentProgress = async ({ resetLimit = false } = {}) => {
  if (componentDisposed) return
  const generation = progressPollGeneration
  if (resetLimit) {
    progressPollCount = 0
    indexDocuments.value.forEach((row) => {
      if (!isTerminalStatus(row.status)) row.poll_error = false
    })
  }
  if (progressPollInFlight) return
  clearProgressPollTimer()
  const pending = indexDocuments.value.filter(row => !isTerminalStatus(row.status) && !row.poll_error)
  if (pending.length === 0) return
  if (progressPollCount >= MAX_PROGRESS_POLLS) return

  progressPollCount++
  progressPollInFlight = true
  try {
    for (let i = 0; i < pending.length; i += MAX_PROGRESS_CONCURRENCY) {
      if (componentDisposed || generation !== progressPollGeneration) return
      const batch = pending.slice(i, i + MAX_PROGRESS_CONCURRENCY)
      await Promise.all(batch.map(async (row) => {
        try {
          const result = await knowledgeAPI.getDocumentProgress(row.id, { product_id: row.product_id })
          if (componentDisposed || generation !== progressPollGeneration) return
          const current = indexDocuments.value.find(item => item.id === row.id)
          if (!current) return
          current.title = result?.title || current.title
          current.status = result?.embed_status || current.status
          current.progress = result?.embed_progress ?? current.progress
          current.poll_error = ''
        } catch {
          if (componentDisposed || generation !== progressPollGeneration) return
          const current = indexDocuments.value.find(item => item.id === row.id)
          if (current) current.poll_error = true
        }
      }))
    }
  } finally {
    progressPollInFlight = false
  }

  if (componentDisposed) return
  if (generation !== progressPollGeneration) {
    void pollDocumentProgress()
    return
  }
  if (indexDocuments.value.some(row => !isTerminalStatus(row.status) && !row.poll_error) && progressPollCount < MAX_PROGRESS_POLLS) {
    progressPollTimer = setTimeout(() => {
      progressPollTimer = null
      void pollDocumentProgress()
    }, PROGRESS_POLL_INTERVAL_MS)
  }
}

const startProgressPolling = async (documentIDs, productID) => {
  stopProgressPolling()
  progressPollCount = 0
  indexDocuments.value = (Array.isArray(documentIDs) ? documentIDs : [])
    .map(id => Number(id))
    .filter(id => Number.isSafeInteger(id) && id > 0)
    .map(id => ({ id, title: `#${id}`, status: 'pending', progress: 0, poll_error: '', product_id: productID }))
  if (indexDocuments.value.length) await pollDocumentProgress({ resetLimit: true })
}

const refreshProgress = async () => {
  await pollDocumentProgress({ resetLimit: true })
}

const retryDocument = async (row) => {
  if (retryingDocumentID.value === row.id || componentDisposed) return
  retryingDocumentID.value = row.id
  try {
    await knowledgeAPI.reindexDocument(row.id, { product_id: row.product_id })
    row.status = 'pending'
    row.progress = 0
    row.poll_error = ''
    await pollDocumentProgress({ resetLimit: true })
  } catch {
    // HTTP interceptor already reports server errors; keep the failed state retryable.
  } finally {
    retryingDocumentID.value = null
  }
}

// 上限与后端同源（knowledge.merchant_batch_max_file_size，单位字节）。
//
// 这一条在前端写死 20MB 的这些年，服务端 BatchImport 那条链路**一个字节上限都没判**
// （parseBatchFile 直接 io.ReadAll），也就是绕过页面直接调接口就能把任意大的文件喂进内存。
// 前端那 20MB 从来不是防线，只是给一个没有防线的后端补的假标签。
//
// 为什么不复用 knowledge.max_upload_file_size（文档上传，默认 50MB）：那是另一个概念
// （整篇 PDF/Word 走解析入库），本条是结构化行数据的批量导入，两者的现行上限本来就不等
// （50 vs 20）。强行并成一条，要么把批量导入静默放宽 2.5 倍，要么把文档上传收紧到 20MB，
// 两种都是行为变更；各配一条才是各自说各自的数。
const maxUploadMB = computed(() => Math.round(rtNum('knowledge', 'merchant_batch_max_file_size', 20971520) / 1024 / 1024))

const handleFileChange = (file) => {
  if (file.size > maxUploadMB.value * 1024 * 1024) {
    ElMessage.error(i18n.global.t('文件大小不能超过{size}MB', { size: maxUploadMB.value }))
    return false
  }
  uploadForm.file = file.raw
  return true
}

const handleExceed = () => {
  ElMessage.warning(i18n.global.t('只能上传 1 个文件'))
}

const loadPreview = () => {
  if (!uploadForm.file) {
    ElMessage.warning(i18n.global.t('请先选择文件'))
    return
  }
  const reader = new FileReader()
  reader.onload = (e) => {
    const text = e.target.result
    parseTextToPreview(text, uploadForm.format, uploadForm.file.name)
  }
  reader.readAsText(uploadForm.file)
};

const loadPastePreview = () => {
  if (!pasteForm.jsonText.trim()) {
    ElMessage.warning(i18n.global.t('请输入 JSON 内容'))
    return
  }
  parseTextToPreview(pasteForm.jsonText, 'json')
}

const parseTextToPreview = (text, format, filename = '') => {
  try {
    preview.value = parseBatchPreview(text, filename, format)
    if (preview.value.length === 0) {
      ElMessage.warning(i18n.global.t('未解析到任何记录'))
    } else {
      ElMessage.success(`解析成功,共 ${preview.value.length} 条`)
    }
  } catch (e) {
    preview.value = []
    ElMessage.error('解析失败: ' + e.message)
  }
}

const loadPasteTemplate = () => {
  pasteForm.jsonText = JSON.stringify([
    { title: i18n.global.t('示例问题 1'), content: i18n.global.t('这里是问题的答案内容,描述清晰完整。'), category: 'FAQ', tags: ['示例', 'FAQ'] },
    { title: i18n.global.t('示例问题 2'), content: i18n.global.t('另一段内容,可以更详细。'), category: '产品', tags: ['介绍'] }
  ], null, 2)
}

const handleBatchImport = async () => {
  if (!preview.value.length) return
  // 后端要求 product_id 必填：在这里先拦，别让用户看完确认框才吃到「product_id 不能为空」
  const targetProduct = activeTab.value === 'upload' ? uploadForm.product_id : pasteForm.product_id
  if (!targetProduct) {
    ElMessage.warning('请选择目标知识库（产品）后再导入')
    return
  }
  try {
    await ElMessageBox.confirm(
      `确认导入 ${preview.value.length} 条记录吗?`,
      '批量导入',
      { type: 'info' }
    )
  } catch { return }

  importing.value = true
  try {
    let res
    if (activeTab.value === 'upload') {
      const fd = new FormData()
      fd.append('product_id', uploadForm.product_id)
      fd.append('format', uploadForm.format)
      fd.append('file', uploadForm.file)
      res = await knowledgeMerchantAPI.batchUpload(fd)
    } else {
      res = await knowledgeMerchantAPI.batchImport({
        product_id: pasteForm.product_id,
        items: preview.value
      })
    }
    if (res) {
      lastResult.value = res
      lastResult.value.product_id = targetProduct
      void startProgressPolling(res.document_ids, targetProduct)
      if (Number(res.accepted) > 0) markChanged()
      ElMessage.success(i18n.global.t('批次已提交：已受理 {accepted} 条，拒绝 {rejected} 条', {
        accepted: res.accepted,
        rejected: res.rejected
      }))
      preview.value = [];
      if (activeTab.value === 'upload') uploadForm.file = null
      else pasteForm.jsonText = ''
    }
  } catch (e) {
    // 拦截器已经弹过服务端文案，这里不再重复（同一条错误弹两次）
    if (!isUserReportedError(e)) ElMessage.error('导入失败: ' + (e.message || ''))
  } finally {
    importing.value = false
  }
}

const truncateText = (s, n) => s ? (s.length > n ? s.substring(0, n) + '...' : s) : ''

onMounted(() => {
  loadProducts()
})

onBeforeUnmount(() => {
  componentDisposed = true
  stopProgressPolling()
})
</script>

<style scoped lang="scss">
.batch-import-page {
  padding: 0;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.progress-toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin: 8px 0;
}

.content-prev {
  font-size: 12px;
  color: #606266;
}

.csv-help {
  background: #fafafa;
  border: 1px solid #ebeef5;
  border-radius: 4px;
  padding: 12px;
  font-size: 12px;
  font-family: monospace;
  color: #303133;
  white-space: pre-wrap;
  word-break: break-all;
}
</style>
