<template>
  <div class="uq-page">
    <el-card shadow="never">
      <div class="head-row">
        <div>
          <h3>未知问题运营</h3>
          <div class="sub">
            低置信与未命中问题聚类审核，采纳后写回 FAQ 知识库
          </div>
        </div>
        <div class="head-actions">
          <el-button @click="refresh">
            刷新
          </el-button>
          <el-button @click="$router.push('/faq')">
            去 FAQ 库
          </el-button>
        </div>
      </div>
      <el-tabs
        v-model="tab"
        @tab-change="refresh"
      >
        <el-tab-pane
          label="问题列表"
          name="questions"
        />
        <el-tab-pane
          label="聚类簇"
          name="clusters"
        />
      </el-tabs>
      <div class="filter-bar">
        <el-select
          v-model="filters.status"
          placeholder="状态"
          clearable
          style="width: 150px"
        >
          <el-option
            v-for="(label, key) in statusLabels"
            :key="key"
            :label="label"
            :value="key"
          />
        </el-select>
        <el-select
          v-model="filters.source"
          placeholder="来源"
          clearable
          style="width: 150px"
        >
          <el-option
            label="低置信"
            value="low_confidence"
          />
          <el-option
            label="转人工"
            value="handoff"
          />
          <el-option
            label="坏例"
            value="bad_case"
          />
          <el-option
            label="CSAT负反馈"
            value="csat_negative"
          />
          <el-option
            label="未命中"
            value="no_match"
          />
        </el-select>
        <el-input
          v-model="filters.keyword"
          placeholder="问句模糊搜索"
          clearable
          style="width: 220px"
          @keyup.enter="refresh"
        />
        <el-button
          type="primary"
          :loading="loading"
          @click="refresh"
        >
          查询
        </el-button>
      </div>

      <!-- 问题列表 -->
      <el-table
        v-if="tab === 'questions'"
        :data="rows"
        size="default"
        stripe
      >
        <el-table-column
          prop="question"
          label="问句"
          min-width="240"
          show-overflow-tooltip
        />
        <el-table-column
          prop="frequency"
          label="频次"
          width="80"
        >
          <template #default="{ row }">
            <el-badge
              :value="row.frequency"
              type="warning"
            />
          </template>
        </el-table-column>
        <el-table-column
          prop="source"
          label="来源"
          width="110"
        >
          <template #default="{ row }">
            <el-tag size="small">
              {{ sourceLabel(row.source) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="status"
          label="状态"
          width="110"
        >
          <template #default="{ row }">
            <el-tag
              size="small"
              :type="statusType(row.status)"
            >
              {{ statusLabels[row.status] || row.status }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="last_seen_at"
          label="最近出现"
          width="170"
        >
          <template #default="{ row }">
            {{ fmtTime(row.last_seen_at) }}
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="230"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button
              size="small"
              @click="openDetail(row)"
            >
              详情
            </el-button>
            <el-button
              v-if="row.status === 'draft'"
              size="small"
              type="primary"
              @click="submit(row)"
            >
              提交审核
            </el-button>
            <template v-if="row.status === 'pending_review'">
              <el-button
                size="small"
                type="success"
                @click="openAdopt(row)"
              >
                采纳
              </el-button>
              <el-button
                size="small"
                @click="ignore(row)"
              >
                忽略
              </el-button>
            </template>
          </template>
        </el-table-column>
      </el-table>

      <!-- 聚类簇 -->
      <el-table
        v-else
        :data="clusters"
        size="default"
        stripe
      >
        <el-table-column
          prop="title"
          label="代表问句"
          min-width="220"
          show-overflow-tooltip
        />
        <el-table-column
          prop="question_count"
          label="成员数"
          width="90"
        />
        <el-table-column
          prop="total_frequency"
          label="频次合计"
          width="100"
          sortable
        />
        <el-table-column
          prop="sample_questions"
          label="样本问句"
          min-width="200"
        >
          <template #default="{ row }">
            <el-tooltip placement="top">
              <template #content>
                <div
                  v-for="(s, i) in (row.sample_questions || []).slice(0, 3)"
                  :key="i"
                >
                  {{ s }}
                </div>
              </template>
              <span>{{ (row.sample_questions || []).slice(0, 3).join(' / ') }}</span>
            </el-tooltip>
          </template>
        </el-table-column>
        <el-table-column
          prop="status"
          label="状态"
          width="110"
        >
          <template #default="{ row }">
            <el-tag
              size="small"
              :type="statusType(row.status)"
            >
              {{ statusLabels[row.status] || row.status }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="180"
          fixed="right"
        >
          <template #default="{ row }">
            <template v-if="row.status === 'pending_review'">
              <el-button
                size="small"
                type="success"
                @click="openClusterAdopt(row)"
              >
                批量采纳
              </el-button>
              <el-button
                size="small"
                @click="ignoreCluster(row)"
              >
                忽略
              </el-button>
            </template>
          </template>
        </el-table-column>
      </el-table>

      <el-pagination
        v-model:current-page="page"
        :page-size="pageSize"
        :total="total"
        layout="prev, pager, next, total"
        class="pager"
        @current-change="refresh"
      />
    </el-card>

    <!-- 详情抽屉 -->
    <el-drawer
      v-model="detailVisible"
      title="未知问题详情"
      size="45%"
    >
      <template v-if="detail">
        <el-descriptions
          :column="2"
          border
          size="small"
        >
          <el-descriptions-item
            label="问句"
            :span="2"
          >
            {{ detail.question?.question }}
          </el-descriptions-item>
          <el-descriptions-item label="状态">
            {{ statusLabels[detail.question?.status] }}
          </el-descriptions-item>
          <el-descriptions-item label="频次">
            {{ detail.question?.frequency }}
          </el-descriptions-item>
          <el-descriptions-item label="来源">
            {{ sourceLabel(detail.question?.source) }}
          </el-descriptions-item>
          <el-descriptions-item label="触发置信度">
            {{ detail.question?.low_conf_score }}
          </el-descriptions-item>
        </el-descriptions>
        <h4 style="margin: 16px 0 8px">
          审核流水
        </h4>
        <el-timeline>
          <el-timeline-item
            v-for="l in detail.review_logs || []"
            :key="l.id"
            :timestamp="fmtTime(l.created_at)"
          >
            {{ l.action }}（{{ l.from_status }} → {{ l.to_status }}）by {{ l.operator_id }}
            <div
              v-if="l.detail"
              class="log-detail"
            >
              {{ l.detail }}
            </div>
          </el-timeline-item>
        </el-timeline>
      </template>
    </el-drawer>

    <!-- 采纳弹窗 -->
    <el-dialog
      v-model="adoptVisible"
      title="采纳为 FAQ"
      width="520px"
    >
      <el-form label-width="90px">
        <el-form-item label="问句">
          <el-input
            :model-value="adoptTarget?.question || adoptTarget?.title"
            disabled
          />
        </el-form-item>
        <el-form-item
          label="答案"
          required
        >
          <el-input
            v-model="adoptForm.answer"
            type="textarea"
            :rows="4"
            placeholder="标准答案（必填）"
          />
        </el-form-item>
        <el-form-item label="分类">
          <el-input
            v-model="adoptForm.category"
            placeholder="默认 reflux"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="adoptVisible = false">
          取消
        </el-button>
        <el-button
          type="success"
          :loading="adopting"
          @click="doAdopt"
        >
          确认采纳
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
// 未知问题运营页（T1）：问题列表 / 聚类簇 两个视图 + 详情抽屉 + 采纳弹窗。
// 风格对齐 views/faq/List.vue（el-card shadow=never + el-table + 抽屉编辑）。
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { unknownQuestionApi } from '@/api/unknownQuestion'

const tab = ref('questions')
const loading = ref(false)
const rows = ref([])
const clusters = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const filters = reactive({ status: '', source: '', keyword: '' })

const statusLabels = {
  draft: '草稿', pending_review: '待审核', adopted: '已采纳', ignored: '已忽略', merged: '已合并'
}
const sourceLabels = {
  low_confidence: '低置信', handoff: '转人工', bad_case: '坏例',
  csat_negative: 'CSAT负反馈', no_match: '未命中'
}
const sourceLabel = (s) => sourceLabels[s] || s
const statusType = (s) =>
  ({ pending_review: 'warning', adopted: 'success', ignored: 'info', merged: 'info', draft: '' }[s] || '')
const fmtTime = (t) => (t ? String(t).replace('T', ' ').slice(0, 19) : '')

async function refresh() {
  loading.value = true
  try {
    if (tab.value === 'questions') {
      // http 拦截器已解包到业务 payload（{list,total,...}），再取 .data 会拿空
      const d = await unknownQuestionApi.list({
        page: page.value, page_size: pageSize,
        status: filters.status || undefined,
        source: filters.source || undefined,
        keyword: filters.keyword || undefined
      }) || {}
      rows.value = d.items || d.list || d.rows || []
      total.value = Number(d.total || 0)
    } else {
      const d = await unknownQuestionApi.clusterList({
        page: page.value, page_size: pageSize, status: filters.status || undefined
      }) || {}
      clusters.value = d.items || d.list || d.rows || []
      total.value = Number(d.total || 0)
    }
  } catch (e) {
    ElMessage.error('加载失败：' + (e?.message || e))
  } finally {
    loading.value = false
  }
}

async function submit(row) {
  await unknownQuestionApi.submit(row.id)
  ElMessage.success('已提交审核')
  refresh()
}

async function ignore(row) {
  await unknownQuestionApi.review(row.id, { action: 'ignore' })
  ElMessage.success('已忽略')
  refresh()
}

async function ignoreCluster(row) {
  await unknownQuestionApi.clusterReview(row.id, { action: 'ignore' })
  ElMessage.success('已忽略')
  refresh()
}

// 详情
const detailVisible = ref(false)
const detail = ref(null)
async function openDetail(row) {
  const { data } = await unknownQuestionApi.get(row.id)
  detail.value = data?.data || data
  detailVisible.value = true
}

// 采纳（单条 / 簇级共用弹窗）
const adoptVisible = ref(false)
const adopting = ref(false)
const adoptTarget = ref(null)
const adoptIsCluster = ref(false)
const adoptForm = reactive({ answer: '', category: '' })

function openAdopt(row) {
  adoptTarget.value = row
  adoptIsCluster.value = false
  adoptForm.answer = ''
  adoptForm.category = ''
  adoptVisible.value = true
}
function openClusterAdopt(row) {
  adoptTarget.value = row
  adoptIsCluster.value = true
  adoptForm.answer = ''
  adoptForm.category = ''
  adoptVisible.value = true
}

async function doAdopt() {
  if (!adoptForm.answer.trim()) {
    ElMessage.warning('答案必填')
    return
  }
  adopting.value = true
  try {
    const payload = { action: 'adopt', answer: adoptForm.answer, category: adoptForm.category || undefined }
    const { data } = adoptIsCluster.value
      ? await unknownQuestionApi.clusterReview(adoptTarget.value.id, payload)
      : await unknownQuestionApi.review(adoptTarget.value.id, payload)
    const d = data?.data || data
    ElMessage.success(`已采纳并写回 FAQ（faq_id=${d?.faq_id}）`)
    adoptVisible.value = false
    refresh()
  } catch (e) {
    // 400 泛化关键词拒绝时后端文案直接透出
    ElMessage.error(e?.message || '采纳失败')
  } finally {
    adopting.value = false
  }
}

onMounted(refresh)
</script>

<style scoped>
.uq-page { padding: 16px; }
.head-row { display: flex; justify-content: space-between; align-items: flex-start; }
.head-row h3 { margin: 0 0 4px; }
.sub { color: #909399; font-size: 13px; }
.filter-bar { display: flex; gap: 12px; margin: 12px 0; flex-wrap: wrap; }
.pager { margin-top: 12px; justify-content: flex-end; }
.log-detail { color: #909399; font-size: 12px; }
</style>
