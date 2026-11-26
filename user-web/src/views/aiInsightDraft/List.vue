<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h2 class="page-title">
          AI 洞察草稿
        </h2>
        <span class="page-sub">{{ t('businessCopy.insight.subtitle') }}</span>
      </div>
      <div class="head-actions">
        <el-button
          :loading="loading"
          @click="load"
        >
          刷新
        </el-button>
        <el-button
          type="primary"
          @click="generateVisible = true"
        >
          生成草稿
        </el-button>
      </div>
    </div>

    <el-card
      shadow="never"
      class="block"
    >
      <el-form
        inline
        label-width="80px"
      >
        <el-form-item label="实体类型">
          <el-input
            v-model="filters.entity_type"
            placeholder="customer"
            clearable
            style="width: 160px"
          />
        </el-form-item>
        <el-form-item label="实体ID">
          <el-input
            v-model="filters.entity_id"
            placeholder="entity_id"
            clearable
            style="width: 160px"
          />
        </el-form-item>
        <el-form-item label="状态">
          <el-select
            v-model="filters.status"
            clearable
            placeholder="全部"
            style="width: 140px"
          >
            <el-option
              label="draft"
              value="draft"
            />
            <el-option
              label="confirmed"
              value="confirmed"
            />
            <el-option
              label="rejected"
              value="rejected"
            />
            <el-option
              label="applied"
              value="applied"
            />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button
            type="primary"
            @click="page=1;load()"
          >
            查询
          </el-button>
        </el-form-item>
      </el-form>

      <el-table
        v-loading="loading"
        :data="rows"
        empty-text="暂无洞察草稿"
      >
        <el-table-column
          prop="draft_id"
          label="草稿ID"
          min-width="170"
          show-overflow-tooltip
        />
        <el-table-column
          prop="entity_type"
          label="实体"
          width="100"
        />
        <el-table-column
          prop="entity_id"
          label="实体ID"
          width="110"
        />
        <el-table-column
          prop="insight_type"
          label="洞察类型"
          width="120"
        />
        <el-table-column
          prop="title"
          label="标题"
          min-width="180"
          show-overflow-tooltip
        />
        <el-table-column
          label="证据"
          width="80"
        >
          <template #default="{ row }">
            <el-tag
              :type="evidenceCount(row) > 0 ? 'success' : 'danger'"
              size="small"
            >
              {{ evidenceCount(row) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="confidence"
          label="置信度"
          width="90"
        >
          <template #default="{ row }">
            {{ formatConfidence(row.confidence) }}
          </template>
        </el-table-column>
        <el-table-column
          prop="status"
          label="状态"
          width="110"
        >
          <template #default="{ row }">
            <el-tag :type="statusTag(row.status)">
              {{ row.status || '-' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="created_at"
          label="创建时间"
          min-width="170"
        >
          <template #default="{ row }">
            {{ fmtTime(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="220"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button
              link
              type="primary"
              @click="openDetail(row)"
            >
              详情
            </el-button>
            <el-button
              link
              type="success"
              :disabled="!canReview(row)"
              @click="openReview(row,'confirm')"
            >
              确认
            </el-button>
            <el-button
              link
              type="danger"
              :disabled="!canReview(row)"
              @click="openReview(row,'reject')"
            >
              拒绝
            </el-button>
          </template>
        </el-table-column>
      </el-table>
      <div class="pager">
        <el-pagination
          v-model:current-page="page"
          v-model:page-size="pageSize"
          :total="total"
          layout="total, prev, pager, next"
          @current-change="load"
          @size-change="load"
        />
      </div>
    </el-card>

    <el-dialog
      v-model="generateVisible"
      :title="t('businessCopy.insight.generateTitle')"
      width="520px"
      destroy-on-close
    >
      <el-alert
        type="info"
        show-icon
        :closable="false"
        style="margin-bottom:12px"
        :title="t('businessCopy.insight.generationDescription')"
      />
      <el-form
        :model="generateForm"
        label-width="90px"
      >
        <el-form-item label="实体类型">
          <el-input
            v-model="generateForm.entity_type"
            placeholder="customer"
          />
        </el-form-item>
        <el-form-item label="实体ID">
          <el-input
            v-model="generateForm.entity_id"
            placeholder="客户/实体ID"
          />
        </el-form-item>
        <el-form-item label="洞察类型">
          <el-select
            v-model="generateForm.insight_type"
            style="width: 100%"
          >
            <el-option
              label="画像 profile"
              value="profile"
            />
            <el-option
              label="分层 segment"
              value="segment"
            />
            <el-option
              label="商机 opportunity"
              value="opportunity"
            />
            <el-option
              label="建议 suggestion"
              value="suggestion"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="标题">
          <el-input
            v-model="generateForm.title"
            placeholder="可选"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="generateVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          :loading="generating"
          @click="doGenerate"
        >
          生成
        </el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="detailVisible"
      title="草稿详情"
      width="860px"
      destroy-on-close
    >
      <template v-if="current">
        <el-alert
          v-if="evidenceCount(current) === 0"
          type="warning"
          show-icon
          :closable="false"
          style="margin-bottom:12px"
          title="无证据草稿：不会展示可应用动作，确认写回将被后端拒绝。"
        />
        <el-descriptions
          :column="2"
          border
          size="small"
          style="margin-bottom:12px"
        >
          <el-descriptions-item label="草稿ID">
            {{ current.draft_id }}
          </el-descriptions-item>
          <el-descriptions-item label="状态">
            <el-tag
              :type="statusTag(current.status)"
              size="small"
            >
              {{ current.status }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="实体">
            {{ current.entity_type }} / {{ current.entity_id }}
          </el-descriptions-item>
          <el-descriptions-item label="洞察类型">
            {{ current.insight_type }}
          </el-descriptions-item>
          <el-descriptions-item
            label="标题"
            :span="2"
          >
            {{ current.title || '-' }}
          </el-descriptions-item>
          <el-descriptions-item label="置信度">
            {{ formatConfidence(current.confidence) }}
          </el-descriptions-item>
          <el-descriptions-item label="生成来源">
            <template v-if="current.generated_by === 'stub_readonly' || current.model_name === 'stub-readonly'">
              {{ t('businessCopy.insight.existingRecordsSource') }}
            </template>
            <template v-else>
              {{ current.generated_by || '-' }} / {{ current.model_name || '-' }} / {{ current.prompt_version || '-' }}
            </template>
          </el-descriptions-item>
          <el-descriptions-item label="创建时间">
            {{ fmtTime(current.created_at) }}
          </el-descriptions-item>
          <el-descriptions-item label="确认时间">
            {{ fmtTime(current.confirmed_at) }}
          </el-descriptions-item>
          <el-descriptions-item
            label="审核备注"
            :span="2"
          >
            {{ current.review_note || '-' }}
          </el-descriptions-item>
        </el-descriptions>

        <el-divider content-position="left">
          证据链（Evidence）
        </el-divider>
        <el-table
          :data="asArray(current.evidence)"
          size="small"
          border
          empty-text="无证据"
          style="margin-bottom:12px"
        >
          <el-table-column
            prop="type"
            label="类型"
            width="140"
          />
          <el-table-column
            prop="source"
            label="来源"
            width="160"
          />
          <el-table-column
            prop="ref"
            label="引用ID"
            width="160"
            show-overflow-tooltip
          />
          <el-table-column
            prop="snippet"
            label="摘要"
            min-width="220"
            show-overflow-tooltip
          />
          <el-table-column
            prop="occurred_at"
            label="发生时间"
            min-width="170"
          />
        </el-table>

        <el-divider content-position="left">
          来源引用（Source Refs）
        </el-divider>
        <pre class="report-json">{{ pretty(current.source_refs) }}</pre>

        <el-divider content-position="left">
          建议动作（Proposed Action）
        </el-divider>
        <el-alert
          v-if="!hasAction(current)"
          type="info"
          show-icon
          :closable="false"
          style="margin-bottom:8px"
          title="无可应用动作。仅在有证据且动作可审计时才允许确认写回。"
        />
        <pre class="report-json">{{ pretty(current.proposed_action) }}</pre>

        <el-divider content-position="left">
          已应用动作（Applied Action）
        </el-divider>
        <pre class="report-json">{{ pretty(current.applied_action) }}</pre>

        <el-divider content-position="left">
          内容（Content）
        </el-divider>
        <pre class="report-json">{{ pretty(current.content) }}</pre>
      </template>
    </el-dialog>

    <el-dialog
      v-model="reviewVisible"
      :title="reviewAction === 'confirm' ? '确认草稿' : '拒绝草稿'"
      width="560px"
      destroy-on-close
    >
      <el-alert
        v-if="reviewAction === 'confirm' && reviewTarget && evidenceCount(reviewTarget) === 0"
        type="error"
        show-icon
        :closable="false"
        style="margin-bottom:12px"
        title="该草稿无证据，后端将拒绝确认写回。"
      />
      <el-alert
        v-if="reviewAction === 'confirm' && reviewTarget && hasAction(reviewTarget)"
        type="warning"
        show-icon
        :closable="false"
        style="margin-bottom:12px"
        :title="t('businessCopy.insight.confirmDescription')"
      />
      <el-form label-width="90px">
        <el-form-item label="草稿ID">
          <span>{{ reviewTarget?.draft_id }}</span>
        </el-form-item>
        <el-form-item label="证据数">
          <span>{{ reviewTarget ? evidenceCount(reviewTarget) : '-' }}</span>
        </el-form-item>
        <el-form-item label="动作">
          <pre class="inline-json">{{ pretty(reviewTarget?.proposed_action) }}</pre>
        </el-form-item>
        <el-form-item label="审核备注">
          <el-input
            v-model="reviewNote"
            type="textarea"
            :rows="3"
            placeholder="记录确认/拒绝原因（建议填写）"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="reviewVisible = false">
          取消
        </el-button>
        <el-button
          :type="reviewAction === 'confirm' ? 'success' : 'danger'"
          :loading="reviewing"
          @click="submitReview"
        >
          {{ reviewAction === 'confirm' ? '确认' : '拒绝' }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { aiInsightDraftApi } from '@/api/aiInsightDraft'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const loading = ref(false)
const generating = ref(false)
const reviewing = ref(false)
const rows = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const filters = reactive({ entity_type: '', entity_id: '', status: '' })

const generateVisible = ref(false)
const generateForm = reactive({ entity_type: 'customer', entity_id: '', insight_type: 'profile', title: '' })

const detailVisible = ref(false)
const current = ref(null)

const reviewVisible = ref(false)
const reviewAction = ref('confirm')
const reviewTarget = ref(null)
const reviewNote = ref('')

function fmtTime(v) {
  if (!v) return '-'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return String(v)
  return d.toLocaleString('zh-CN', { hour12: false })
}

function formatConfidence(v) {
  const n = Number(v)
  if (Number.isNaN(n)) return '-'
  return n.toFixed(2)
}

function statusTag(status) {
  const map = { draft: 'warning', confirmed: 'success', rejected: 'info', applied: 'primary' }
  return map[status] || 'info'
}

function asArray(v) {
  return Array.isArray(v) ? v : []
}

function evidenceCount(row) {
  return asArray(row?.evidence).length
}

function hasAction(row) {
  const a = row?.proposed_action
  return !!(a && typeof a === 'object' && !Array.isArray(a) && Object.keys(a).length > 0)
}

function pretty(v) {
  if (v === undefined || v === null || v === '') return '{}'
  try {
    return JSON.stringify(v, null, 2)
  } catch {
    return String(v)
  }
}

function canReview(row) {
  return row?.status === 'draft'
}

async function load() {
  loading.value = true
  try {
    const res = await aiInsightDraftApi.list({
      page: page.value,
      page_size: pageSize.value,
      entity_type: filters.entity_type || undefined,
      entity_id: filters.entity_id || undefined,
      status: filters.status || undefined,
    })
    rows.value = Array.isArray(res?.list) ? res.list : (Array.isArray(res?.items) ? res.items : [])
    total.value = Number(res?.total || 0)
  } finally {
    loading.value = false
  }
}

async function doGenerate() {
  if (!generateForm.entity_id.trim()) {
    ElMessage.warning('请输入实体ID')
    return
  }
  generating.value = true
  try {
    await aiInsightDraftApi.generate({
      entity_type: generateForm.entity_type || 'customer',
      entity_id: generateForm.entity_id.trim(),
      insight_type: generateForm.insight_type || 'profile',
      title: generateForm.title.trim(),
    })
    ElMessage.success('生成成功')
    generateVisible.value = false
    await load()
  } finally {
    generating.value = false
  }
}

async function openDetail(row) {
  const res = await aiInsightDraftApi.get(row.draft_id)
  current.value = res?.draft_id ? res : (res?.data?.draft_id ? res.data : res)
  detailVisible.value = true
}

function openReview(row, action) {
  reviewTarget.value = row
  reviewAction.value = action
  reviewNote.value = ''
  reviewVisible.value = true
}

async function submitReview() {
  if (!reviewTarget.value) return
  reviewing.value = true
  try {
    const payload = { review_note: reviewNote.value }
    if (reviewAction.value === 'confirm') {
      const res = await aiInsightDraftApi.confirm(reviewTarget.value.draft_id, payload)
      const applied = res?.applied === true || res?.draft?.status === 'applied'
      ElMessage.success(applied ? '已确认并写回' : '已确认')
    } else {
      await aiInsightDraftApi.reject(reviewTarget.value.draft_id, payload)
      ElMessage.success('已拒绝')
    }
    reviewVisible.value = false
    await load()
  } finally {
    reviewing.value = false
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
.pager { margin-top: 12px; display: flex; justify-content: flex-end; }
.report-json { background: #f6f8fa; border: 1px solid #e5e7eb; padding: 12px; border-radius: 6px; max-height: 320px; overflow: auto; font-size: 12px; line-height: 1.5; margin: 0; white-space: pre-wrap; word-break: break-word; }
.inline-json { background: #f6f8fa; border: 1px solid #e5e7eb; padding: 8px; border-radius: 6px; max-height: 160px; overflow: auto; font-size: 12px; line-height: 1.4; margin: 0; white-space: pre-wrap; word-break: break-word; }
</style>
