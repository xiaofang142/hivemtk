<template>
  <div class="llm-report-page">
    <!-- 筛选栏 -->
    <el-card
      shadow="never"
      class="filter-card"
    >
      <div class="filter-bar">
        <el-date-picker
          v-model="dateRange"
          type="daterange"
          range-separator="至"
          start-placeholder="开始日期"
          end-placeholder="结束日期"
          value-format="YYYY-MM-DD"
          :clearable="false"
          style="width: 260px"
        />
        <el-select
          v-model="filters.channel"
          placeholder="渠道"
          clearable
          style="width: 140px"
        >
          <el-option
            label="官网"
            value="web"
          />
          <el-option
            label="用户端"
            value="app"
          />
          <el-option
            label="扩展"
            value="extension"
          />
          <el-option
            label="API"
            value="api"
          />
        </el-select>
        <el-select
          v-model="filters.source"
          placeholder="计费来源"
          clearable
          style="width: 140px"
        >
          <el-option
            label="自有模型"
            value="self"
          />
          <el-option
            label="平台垫资"
            value="platform"
          />
        </el-select>
        <el-select
          v-model="filters.model"
          placeholder="模型"
          clearable
          filterable
          style="width: 180px"
        >
          <el-option
            v-for="m in modelOptions"
            :key="m"
            :label="m"
            :value="m"
          />
        </el-select>
        <el-button
          type="primary"
          :loading="loading"
          @click="refresh"
        >
          查询
        </el-button>
      </div>
    </el-card>

    <!-- KPI 卡片行 -->
    <el-row
      :gutter="12"
      class="kpi-row"
    >
      <el-col
        v-for="k in kpiCards"
        :key="k.label"
        :span="5"
      >
        <el-card shadow="hover">
          <div class="kpi-label">
            {{ k.label }}
          </div>
          <div
            class="kpi-value"
            :class="k.danger ? 'kpi-danger' : ''"
          >
            {{ k.value }}
          </div>
        </el-card>
      </el-col>
    </el-row>

    <!-- 趋势图（双轴：调用数柱 + 错误率线） -->
    <el-card
      shadow="never"
      class="chart-card"
    >
      <template #header>
        调用趋势与错误率
      </template>
      <div
        ref="trendRef"
        class="chart chart-tall"
      />
    </el-card>

    <el-row :gutter="12">
      <el-col :span="14">
        <el-card
          shadow="never"
          class="chart-card"
        >
          <template #header>
            <div class="card-header-row">
              <span>模型分布（调用数 / 成本）</span>
              <el-select
                v-model="breakdownBy"
                size="small"
                style="width: 130px"
                @change="loadBreakdown"
              >
                <el-option
                  label="按模型"
                  value="model"
                />
                <el-option
                  label="按场景"
                  value="scenario"
                />
                <el-option
                  label="按渠道"
                  value="channel"
                />
                <el-option
                  label="按厂商"
                  value="provider"
                />
              </el-select>
            </div>
          </template>
          <div
            ref="breakdownRef"
            class="chart chart-mid"
          />
        </el-card>
      </el-col>
      <el-col :span="10">
        <el-card
          shadow="never"
          class="chart-card"
        >
          <template #header>
            错误类型分布
          </template>
          <div
            ref="errorPieRef"
            class="chart chart-mid"
          />
        </el-card>
      </el-col>
    </el-row>

    <!-- 错误明细表 -->
    <el-card
      shadow="never"
      class="chart-card"
    >
      <template #header>
        错误明细
      </template>
      <el-table
        :data="errorRows"
        size="small"
        stripe
      >
        <el-table-column
          prop="created_at"
          label="时间"
          width="170"
        >
          <template #default="{ row }">
            {{ fmtTime(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column
          prop="trace_id"
          label="TraceID"
          width="180"
          show-overflow-tooltip
        />
        <el-table-column
          prop="model"
          label="模型"
          width="160"
          show-overflow-tooltip
        />
        <el-table-column
          label="错误类型"
          width="140"
        >
          <template #default="{ row }">
            <el-tag
              :type="errorClassTagType(row.error_class)"
              size="small"
            >
              {{ errorClassLabel(row.error_class) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="error_msg"
          label="错误消息"
          show-overflow-tooltip
        />
      </el-table>
      <el-pagination
        v-model:current-page="errorPage"
        :page-size="errorPageSize"
        :total="errorTotal"
        layout="prev, pager, next"
        class="err-pager"
        @current-change="loadErrors"
      />
    </el-card>
  </div>
</template>

<script setup>
// LLM 观测报表看板（T3）。
// 数据源：/api/reports/llm/*（llm_usage_daily 日聚合 + 当日明细实时兜底）。
import { ref, reactive, computed, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { ElMessage } from 'element-plus'
import LlmReportApi from '@/api/llmReport'
import { safeInit, safeDispose, echarts } from '@/utils/echarts'

const loading = ref(false)
const now = new Date()
const pad = (n) => String(n).padStart(2, '0')
const dayStr = (d) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
const dateRange = ref([dayStr(new Date(now.getTime() - 6 * 86400000)), dayStr(now)])
const filters = reactive({ channel: '', source: '', model: '' })
const modelOptions = ref([])

const overview = ref(null)
const trendPoints = ref([])
const breakdownRows = ref([])
const errorRows = ref([])
const errorPage = ref(1)
const errorPageSize = 20
const errorTotal = ref(0)
// 错误类聚合（/errors 返回的 by_class），饼图只认这份数据
const errorAgg = ref([])
const breakdownBy = ref('model')

const fmtCents = (c) => (c / 100).toFixed(2)
const fmtRate = (r) => `${(r * 100).toFixed(2)}%`
const fmtNum = (n) => Number(n || 0).toLocaleString()
const fmtTime = (t) => (t ? String(t).replace('T', ' ').slice(0, 19) : '')
// 错误分类标签：后端按 error_msg 文本回推成 auth/rate_limit/timeout/upstream/unknown
const ERROR_CLASS_LABEL = {
  auth: '鉴权失败',
  rate_limit: '限流/额度',
  timeout: '超时',
  upstream: '上游异常',
  unknown: '未分类'
}
const errorClassLabel = (c) => ERROR_CLASS_LABEL[c] || (c ? String(c) : '未分类')
const errorClassTagType = (c) =>
  ({ auth: 'danger', rate_limit: 'warning', timeout: 'info', upstream: 'danger' }[c] || 'info')

const kpiCards = computed(() => {
  const o = overview.value || {}
  const errRate = o.error_rate || 0
  return [
    { label: '调用量', value: fmtNum(o.call_count) },
    { label: '错误率', value: fmtRate(errRate), danger: errRate > 0.05 },
    { label: 'Token 消耗', value: fmtNum((o.prompt_tokens || 0) + (o.completion_tokens || 0)) },
    { label: '成本（元）', value: fmtCents(o.total_cost_cents || 0) },
    { label: 'P95 延迟', value: `${o.p95_latency_ms || 0} ms` }
  ]
})

const rangeParams = () => ({
  from: dateRange.value?.[0],
  to: dateRange.value?.[1],
  channel: filters.channel || undefined,
  source: filters.source || undefined,
  model: filters.model || undefined
})

// ---- 图表 ----
const trendRef = ref(null)
const breakdownRef = ref(null)
const errorPieRef = ref(null)
let trendChart = null
let breakdownChart = null
let errorPieChart = null

function renderTrend() {
  if (!trendChart) return
  const pts = trendPoints.value
  trendChart.setOption({
    tooltip: { trigger: 'axis' },
    legend: { data: ['调用数', '错误率'] },
    grid: { left: 60, right: 60, top: 40, bottom: 30 },
    xAxis: { type: 'category', data: pts.map((p) => p.date) },
    yAxis: [
      { type: 'value', name: '调用数' },
      { type: 'value', name: '错误率', axisLabel: { formatter: (v) => `${(v * 100).toFixed(0)}%` } }
    ],
    series: [
      { name: '调用数', type: 'bar', data: pts.map((p) => p.call_count) },
      { name: '错误率', type: 'line', yAxisIndex: 1, data: pts.map((p) => p.error_rate), smooth: true }
    ]
  })
}

function renderBreakdown() {
  if (!breakdownChart) return
  const rows = breakdownRows.value.slice(0, 12)
  breakdownChart.setOption({
    tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' } },
    legend: { data: ['调用数', '成本(元)'] },
    grid: { left: 140, right: 60, top: 40, bottom: 30 },
    yAxis: { type: 'category', data: rows.map((r) => r.dim || '(未知)'), inverse: true },
    xAxis: [
      { type: 'value', name: '调用数' },
      { type: 'value', name: '元', position: 'top' }
    ],
    series: [
      { name: '调用数', type: 'bar', data: rows.map((r) => r.call_count) },
      {
        name: '成本(元)',
        type: 'bar',
        xAxisIndex: 1,
        data: rows.map((r) => Number((r.total_cost_cents / 100).toFixed(2)))
      }
    ]
  })
}

function renderErrorPie() {
  if (!errorPieChart) return
  // 之前读的是「维度 breakdown」的 error_count（模型维度），饼图要么空要么与错误无关；
  // 这里改用 /errors 的 by_class 聚合，name 走中文标签。
  const data = (errorAgg.value || [])
    .filter((r) => r.dim && Number(r.call_count || r.error_count || 0) > 0)
    .map((r) => ({ name: errorClassLabel(r.dim), value: Number(r.call_count || r.error_count || 0) }))
  errorPieChart.setOption({
    tooltip: { trigger: 'item' },
    series: [{ type: 'pie', radius: ['40%', '70%'], data, label: { formatter: '{b}: {c}' } }]
  })
}

async function loadOverview() {
  // http.get 已把信封拆到 data.data，这里再 `const { data } = ...` 拿到的是
  // undefined ⇒ KPI 永远渲染 0。四个 loader 同一形状，统一改成直接用返回值。
  const data = await LlmReportApi.getOverview(rangeParams())
  overview.value = data || {}
}

async function loadTrend() {
  const data = await LlmReportApi.getTimeseries(rangeParams())
  trendPoints.value = data || []
  renderTrend()
}

async function loadBreakdown() {
  const data = await LlmReportApi.getBreakdown({ ...rangeParams(), by: breakdownBy.value })
  breakdownRows.value = data || []
  // 模型筛选的可选项来自「按模型」维度的实际数据：
  // 之前 modelOptions 声明了却没人填，下拉永远是空的（选了也没用）。
  if (breakdownBy.value === 'model') {
    modelOptions.value = (breakdownRows.value || []).map((r) => r.dim).filter(Boolean)
  }
  renderBreakdown()
}

async function loadErrors() {
  const d = await LlmReportApi.getErrors({
    ...rangeParams(),
    page: errorPage.value,
    page_size: errorPageSize
  }) || {}
  errorRows.value = d.rows || []
  errorTotal.value = d.total || 0
  errorAgg.value = d.by_class || []
  renderErrorPie()
}

async function refresh() {
  loading.value = true
  try {
    await Promise.all([loadOverview(), loadTrend(), loadBreakdown(), loadErrors()])
  } catch (e) {
    ElMessage.error('报表加载失败：' + (e?.message || e))
  } finally {
    loading.value = false
  }
}

function resizeAll() {
  trendChart?.resize()
  breakdownChart?.resize()
  errorPieChart?.resize()
}

onMounted(async () => {
  await nextTick()
  trendChart = safeInit(trendRef.value)
  breakdownChart = safeInit(breakdownRef.value)
  errorPieChart = safeInit(errorPieRef.value)
  window.addEventListener('resize', resizeAll)
  await refresh()
})

onBeforeUnmount(() => {
  window.removeEventListener('resize', resizeAll)
  safeDispose(trendChart)
  safeDispose(breakdownChart)
  safeDispose(errorPieChart)
})
</script>

<style scoped>
.llm-report-page { padding: 16px; }
.filter-bar { display: flex; gap: 12px; align-items: center; flex-wrap: wrap; }
.kpi-row { margin-top: 12px; }
.kpi-label { color: #909399; font-size: 13px; }
.kpi-value { font-size: 24px; font-weight: 600; margin-top: 4px; }
.kpi-danger { color: #f56c6c; }
.chart-card { margin-top: 12px; }
.card-header-row { display: flex; justify-content: space-between; align-items: center; }
.chart { width: 100%; }
.chart-tall { height: 320px; }
.chart-mid { height: 280px; }
.err-pager { margin-top: 12px; justify-content: flex-end; }
</style>
