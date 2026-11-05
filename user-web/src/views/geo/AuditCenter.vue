<template>
  <div class="geo-audit-center">
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <div>
            <span class="title">🔍 GEO 体检中心</span>
            <span class="sub">先体检，再按修复清单补 citability / crawler / schema；一个月后用“复查对比”看同口径变化。</span>
          </div>
          <el-tag
            type="info"
            effect="plain"
          >
            站点 rubric v3 · 内容 29 因子
          </el-tag>
        </div>
      </template>

      <el-form
        :inline="true"
        label-width="72px"
      >
        <el-form-item label="体检类型">
          <el-radio-group v-model="form.kind">
            <el-radio-button value="site">
              站点（rubric v3）
            </el-radio-button>
            <el-radio-button value="content">
              内容（29 因子）
            </el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item
          v-if="form.kind === 'site'"
          label="范围"
        >
          <el-radio-group v-model="form.scope">
            <el-radio-button value="page">
              单页
            </el-radio-button>
            <el-radio-button value="site">
              全站（sitemap）
            </el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item
          v-if="form.kind === 'site' && form.scope === 'site'"
          label="最多页数"
        >
          <el-input-number
            v-model="form.max_pages"
            :min="1"
            :max="20"
          />
        </el-form-item>
        <el-form-item label="URL">
          <el-input
            v-model="form.url"
            placeholder="https://example.com/pricing"
            style="width: 340px"
            clearable
          />
        </el-form-item>
        <el-form-item label="品牌名">
          <el-input
            v-model="form.brand"
            placeholder="HiveMtk（可选）"
            style="width: 180px"
            clearable
          />
        </el-form-item>
        <el-form-item>
          <el-button
            type="primary"
            :loading="running"
            @click="runAudit"
          >
            开始体检
          </el-button>
        </el-form-item>
      </el-form>

      <div
        v-if="form.kind === 'content'"
        class="content-input"
      >
        <el-input
          v-model="form.content"
          type="textarea"
          :rows="4"
          placeholder="可选：粘贴正文。留空则抓取上面的 URL 后自动抽取正文、meta 与 JSON-LD。"
        />
        <el-checkbox v-model="form.live">
          附加真实 robots.txt / llms.txt 检查
        </el-checkbox>
      </div>
      <div class="hint">
        体检只访问你显式提供的 URL 及其同域 robots.txt / llms.txt / ai.txt / summary.json；不会携带品牌配置出站。
      </div>
    </el-card>

    <el-card
      v-if="current"
      shadow="never"
      class="result-card"
    >
      <template #header>
        <div class="card-header">
          <span class="title">本次结果</span>
          <div>
            <el-button
              size="small"
              @click="downloadReport('html')"
            >
              打开报告
            </el-button>
            <el-button
              size="small"
              @click="downloadReport('csv')"
            >
              导出 CSV
            </el-button>
          </div>
        </div>
      </template>

      <div class="score-row">
        <div class="score-big">
          {{ current.run.score }}<span class="score-max">/{{ current.run.max_score }}</span>
        </div>
        <div class="score-meta">
          <el-tag :type="gradeType(current.run.grade)">
            {{ current.run.grade }}
          </el-tag>
          <div class="muted">
            {{ current.run.url }} · {{ kindText(current.run.kind) }} · rubric {{ current.run.rubric_version }}
          </div>
          <div>{{ current.run.summary }}</div>
        </div>
      </div>

      <el-tabs v-model="resultTab">
        <el-tab-pane
          label="分类得分"
          name="categories"
        >
          <el-collapse v-model="openCategories">
            <el-collapse-item
              v-for="cat in current.categories"
              :key="cat.category"
              :name="cat.category"
            >
              <template #title>
                <div class="cat-title">
                  <span>{{ cat.label || cat.category }}</span>
                  <span class="muted">{{ cat.score }} / {{ cat.max }}</span>
                </div>
              </template>
              <el-table
                :data="cat.checks"
                size="small"
                border
              >
                <el-table-column
                  prop="check"
                  label="检查项"
                  min-width="180"
                />
                <el-table-column
                  label="得分"
                  width="90"
                >
                  <template #default="{ row }">
                    {{ row.score }} / {{ row.max }}
                  </template>
                </el-table-column>
                <el-table-column
                  label="结果"
                  width="80"
                >
                  <template #default="{ row }">
                    <el-tag
                      :type="row.pass ? 'success' : 'danger'"
                      size="small"
                    >
                      {{ row.pass ? '通过' : '未通过' }}
                    </el-tag>
                  </template>
                </el-table-column>
                <el-table-column
                  prop="detail"
                  label="说明"
                  min-width="220"
                  show-overflow-tooltip
                />
                <el-table-column
                  prop="fix"
                  label="修复建议"
                  min-width="260"
                  show-overflow-tooltip
                />
              </el-table>
            </el-collapse-item>
          </el-collapse>
        </el-tab-pane>

        <el-tab-pane
          label="修复清单"
          name="fixes"
        >
          <el-table
            :data="current.fixes"
            size="small"
            border
          >
            <el-table-column
              prop="category"
              label="分类"
              width="140"
            />
            <el-table-column
              prop="check"
              label="检查项"
              min-width="180"
            />
            <el-table-column
              prop="weight"
              label="分值"
              width="80"
            />
            <el-table-column
              prop="detail"
              label="问题"
              min-width="220"
              show-overflow-tooltip
            />
            <el-table-column
              prop="suggestion"
              label="建议"
              min-width="280"
              show-overflow-tooltip
            />
          </el-table>
          <el-empty
            v-if="!current.fixes.length"
            description="没有未通过项"
          />
        </el-tab-pane>

        <el-tab-pane
          v-if="current.pages && current.pages.length"
          label="页面明细"
          name="pages"
        >
          <el-table
            :data="current.pages"
            size="small"
            border
          >
            <el-table-column
              prop="url"
              label="页面"
              min-width="320"
              show-overflow-tooltip
            />
            <el-table-column
              label="得分"
              width="110"
            >
              <template #default="{ row }">
                {{ row.score }} / {{ row.max_score }}
              </template>
            </el-table-column>
            <el-table-column
              prop="grade"
              label="等级"
              width="90"
            />
            <el-table-column
              label="结果"
              width="120"
            >
              <template #default="{ row }">
                <el-tag
                  v-if="row.error"
                  type="danger"
                  size="small"
                >
                  抓取失败
                </el-tag>
                <el-tag
                  v-else
                  type="success"
                  size="small"
                >
                  已完成
                </el-tag>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>
      </el-tabs>
    </el-card>

    <el-card
      shadow="never"
      class="history-card"
    >
      <template #header>
        <div class="card-header">
          <span class="title">历史与复查</span>
          <div class="filters">
            <el-select
              v-model="filter.kind"
              placeholder="全部类型"
              clearable
              style="width: 130px"
              @change="loadRuns"
            >
              <el-option
                label="站点"
                value="site"
              />
              <el-option
                label="内容"
                value="content"
              />
            </el-select>
            <el-input
              v-model="filter.url"
              placeholder="按 URL 过滤"
              clearable
              style="width: 260px"
              @keyup.enter="loadRuns"
            />
            <el-button @click="loadRuns">
              查询
            </el-button>
          </div>
        </div>
      </template>

      <el-table
        v-loading="loading"
        :data="runs"
        size="small"
        border
        @row-click="openRun"
      >
        <el-table-column
          prop="created_at"
          label="时间"
          width="180"
        />
        <el-table-column
          label="类型"
          width="80"
        >
          <template #default="{ row }">
            {{ kindText(row.kind) }}
          </template>
        </el-table-column>
        <el-table-column
          prop="url"
          label="URL"
          min-width="260"
          show-overflow-tooltip
        />
        <el-table-column
          prop="rubric_version"
          label="rubric"
          width="110"
        />
        <el-table-column
          label="得分"
          width="90"
        >
          <template #default="{ row }">
            <strong>{{ row.score }}</strong>/{{ row.max_score }}
          </template>
        </el-table-column>
        <el-table-column
          prop="grade"
          label="等级"
          width="80"
        />
        <el-table-column
          prop="source"
          label="来源"
          width="90"
        />
        <el-table-column
          label="操作"
          width="140"
        >
          <template #default="{ row }">
            <el-button
              link
              type="primary"
              @click.stop="openRun(row)"
            >
              详情
            </el-button>
            <el-button
              link
              type="primary"
              @click.stop="openReport(row)"
            >
              报告
            </el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-empty
        v-if="!loading && !runs.length"
        description="还没有体检记录"
      />
      <div class="pager">
        <el-pagination
          v-model:current-page="page"
          :page-size="limit"
          :total="total"
          layout="total, prev, pager, next"
          small
          @current-change="loadRuns"
        />
      </div>

      <div class="compare-box">
        <div class="compare-head">
          <span class="title">复查对比</span>
          <span class="muted">只在同 URL / 同 rubric 版本之间比较；跨版本会明确标记“不可比”。</span>
        </div>
        <div class="compare-form">
          <el-select
            v-model="compareFrom"
            placeholder="基线（较早）"
            filterable
            style="width: 340px"
          >
            <el-option
              v-for="row in runs"
              :key="row.id"
              :label="runLabel(row)"
              :value="row.id"
            />
          </el-select>
          <el-select
            v-model="compareTo"
            placeholder="复查（较新）"
            filterable
            style="width: 340px"
          >
            <el-option
              v-for="row in runs"
              :key="row.id"
              :label="runLabel(row)"
              :value="row.id"
            />
          </el-select>
          <el-button
            type="primary"
            :disabled="!compareFrom || !compareTo"
            :loading="comparing"
            @click="doCompare"
          >
            开始对比
          </el-button>
        </div>

        <el-alert
          v-if="compare && !compare.comparable"
          type="warning"
          :closable="false"
          :title="compare.reason || '两次记录不可比'"
        />

        <template v-if="compare && compare.comparable">
          <el-row
            :gutter="12"
            class="compare-stats"
          >
            <el-col :span="4">
              <el-statistic
                title="分数变化"
                :value="compare.score_delta"
              />
            </el-col>
            <el-col :span="4">
              <el-statistic
                title="修复"
                :value="compare.fixed"
              />
            </el-col>
            <el-col :span="4">
              <el-statistic
                title="回退"
                :value="compare.regressed"
              />
            </el-col>
            <el-col :span="4">
              <el-statistic
                title="仍未通过"
                :value="compare.still_failing"
              />
            </el-col>
            <el-col :span="4">
              <el-statistic
                title="新增项"
                :value="compare.added"
              />
            </el-col>
            <el-col :span="4">
              <el-statistic
                title="消失项"
                :value="compare.removed"
              />
            </el-col>
          </el-row>

          <el-table
            :data="compare.categories"
            size="small"
            border
            style="margin-top: 12px"
          >
            <el-table-column
              prop="label"
              label="分类"
              min-width="160"
            />
            <el-table-column
              label="基线"
              width="100"
            >
              <template #default="{ row }">
                {{ row.from_score }} / {{ row.from_max }}
              </template>
            </el-table-column>
            <el-table-column
              label="复查"
              width="100"
            >
              <template #default="{ row }">
                {{ row.to_score }} / {{ row.to_max }}
              </template>
            </el-table-column>
            <el-table-column
              label="变化"
              width="90"
            >
              <template #default="{ row }">
                <span :class="row.delta > 0 ? 'up' : row.delta < 0 ? 'down' : ''">
                  {{ row.delta > 0 ? '+' : '' }}{{ row.delta }}
                </span>
              </template>
            </el-table-column>
          </el-table>

          <el-table
            :data="compare.items"
            size="small"
            border
            style="margin-top: 12px"
            max-height="360"
          >
            <el-table-column
              prop="category"
              label="分类"
              width="120"
            />
            <el-table-column
              prop="check"
              label="检查项"
              min-width="200"
            />
            <el-table-column
              label="状态"
              width="110"
            >
              <template #default="{ row }">
                <el-tag
                  :type="statusType(row.status)"
                  size="small"
                >
                  {{ statusText(row.status) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              label="分变化"
              width="110"
            >
              <template #default="{ row }">
                {{ row.from_score }} → {{ row.to_score }} / {{ row.max }}
              </template>
            </el-table-column>
            <el-table-column
              prop="detail"
              label="说明"
              min-width="240"
              show-overflow-tooltip
            />
          </el-table>
        </template>
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import {
  compareAudits,
  fetchAuditReport,
  getAuditRun,
  listAuditRuns,
  runContentAudit,
  runSiteAudit
} from '@/api/geoAudit'

const form = reactive({
  kind: 'site',
  scope: 'page',
  max_pages: 10,
  url: '',
  brand: '',
  content: '',
  live: false
})
const running = ref(false)
const current = ref(null)
const resultTab = ref('categories')
const openCategories = ref([])

const runs = ref([])
const total = ref(0)
const page = ref(1)
const limit = 20
const loading = ref(false)
const filter = reactive({ kind: '', url: '' })

const compareFrom = ref('')
const compareTo = ref('')
const comparing = ref(false)
const compare = ref(null)

const kindText = (kind) => (kind === 'content' ? '内容' : '站点')
const gradeType = (grade) => {
  if (grade === '优秀') return 'success'
  if (grade === '良好') return 'primary'
  if (grade === '待改进') return 'warning'
  return 'danger'
}
const runLabel = (row) =>
  `${row.created_at?.slice(0, 16).replace('T', ' ')} · ${kindText(row.kind)} · ${row.score}/${row.max_score} · ${row.url}`
const statusText = (status) => {
  const map = {
    same_pass: '保持通过',
    still_failing: '仍未通过',
    fixed: '已修复',
    regressed: '回退',
    added: '新增项',
    removed: '消失项'
  }
  return map[status] || status
}
const statusType = (status) => {
  if (status === 'fixed' || status === 'same_pass') return 'success'
  if (status === 'regressed' || status === 'still_failing') return 'danger'
  return 'info'
}

async function runAudit () {
  if (!form.url && !form.content) {
    ElMessage.warning('请填写 URL 或正文')
    return
  }
  running.value = true
  try {
    let data
    if (form.kind === 'site') {
      data = await runSiteAudit({
        url: form.url,
        brand: form.brand,
        scope: form.scope,
        max_pages: form.max_pages
      })
    } else {
      data = await runContentAudit({
        url: form.url,
        brand: form.brand,
        content: form.content,
        live: form.live
      })
    }
    applyReport(data)
    ElMessage.success('体检完成')
    await loadRuns()
  } finally {
    running.value = false
  }
}

function applyReport (payload) {
  if (!payload || !payload.run) return
  const report = payload.report || {}
  current.value = {
    run: payload.run,
    categories: report.categories || payload.run.categories || [],
    fixes: report.fixes || payload.run.fixes || [],
    pages: report.pages || payload.run.page_results || []
  }
  openCategories.value = current.value.categories.map((c) => c.category)
  resultTab.value = 'categories'
  window.scrollTo({ top: 0, behavior: 'smooth' })
}

async function loadRuns () {
  loading.value = true
  try {
    const data = await listAuditRuns({
      kind: filter.kind || '',
      url: filter.url || '',
      page: page.value,
      limit
    })
    runs.value = data.list || []
    total.value = data.total || 0
    if (!compareFrom.value && runs.value.length > 1) compareFrom.value = runs.value[1].id
    if (!compareTo.value && runs.value.length) compareTo.value = runs.value[0].id
  } finally {
    loading.value = false
  }
}

async function openRun (row) {
  const data = await getAuditRun(row.id)
  applyReport({ run: data.run, report: { categories: data.run.categories, fixes: data.run.fixes } })
}

async function openReport (row) {
  await downloadReport('html', row.id)
}

async function downloadReport (format, id = null) {
  const runId = id || current.value?.run?.id
  if (!runId) return
  const blob = await fetchAuditReport(runId, format)
  const typed = new Blob([blob], {
    type: format === 'csv' ? 'text/csv;charset=utf-8' : 'text/html;charset=utf-8'
  })
  const url = URL.createObjectURL(typed)
  if (format === 'html') {
    window.open(url, '_blank')
  } else {
    const a = document.createElement('a')
    a.href = url
    a.download = `geo-audit-${runId}.csv`
    a.click()
  }
  setTimeout(() => URL.revokeObjectURL(url), 15000)
}

async function doCompare () {
  comparing.value = true
  try {
    compare.value = await compareAudits(compareFrom.value, compareTo.value)
  } finally {
    comparing.value = false
  }
}

onMounted(loadRuns)
</script>

<style scoped>
.geo-audit-center {
  padding: 16px;
}
.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}
.title {
  font-size: 16px;
  font-weight: 600;
}
.sub,
.muted,
.hint {
  color: #6b7280;
  font-size: 12px;
}
.sub {
  margin-left: 10px;
}
.hint {
  margin-top: 8px;
}
.content-input {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-bottom: 8px;
}
.result-card,
.history-card {
  margin-top: 16px;
}
.score-row {
  display: flex;
  align-items: center;
  gap: 24px;
  padding: 12px 16px;
  background: #f8fafc;
  border: 1px solid #e5e7eb;
  border-radius: 10px;
  margin-bottom: 12px;
}
.score-big {
  font-size: 38px;
  font-weight: 700;
  color: #2563eb;
}
.score-max {
  font-size: 15px;
  color: #6b7280;
}
.score-meta {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.cat-title {
  display: flex;
  justify-content: space-between;
  width: 100%;
  padding-right: 12px;
}
.filters {
  display: flex;
  gap: 8px;
}
.pager {
  display: flex;
  justify-content: flex-end;
  margin-top: 8px;
}
.compare-box {
  margin-top: 20px;
  border-top: 1px dashed #e5e7eb;
  padding-top: 16px;
}
.compare-head {
  display: flex;
  align-items: baseline;
  gap: 12px;
  margin-bottom: 10px;
}
.compare-form {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.compare-stats {
  margin-top: 14px;
}
.up {
  color: #059669;
  font-weight: 600;
}
.down {
  color: #dc2626;
  font-weight: 600;
}
</style>
