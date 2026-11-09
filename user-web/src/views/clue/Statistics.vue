<template>
  <div class="clue-stats-page">
    <el-card
      class="header-card"
      shadow="never"
    >
      <div class="page-header">
        <div class="header-text">
          <h2>{{ $t('线索统计') }}</h2>
          <p class="subtitle">
            线索数量、来源、转化与验证情况一览：指标 + 趋势 + 分布
          </p>
        </div>
        <div class="header-actions">
          <el-date-picker
            v-model="dateRange"
            type="daterange"
            range-separator="至"
            start-placeholder="开始"
            end-placeholder="结束"
            value-format="YYYY-MM-DD"
            style="width: 280px"
            @change="fetchAll"
          />
          <el-button @click="fetchAll">
            <el-icon><Refresh /></el-icon>刷新
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
          <div
            class="stat-icon"
            style="background: rgba(79,70,229,.1); color: #4F46E5"
          >
            <el-icon :size="22">
              <DataLine />
            </el-icon>
          </div>
          <div class="stat-body">
            <div class="stat-label">
              线索总数
            </div>
            <div class="stat-value">
              {{ summary.total || 0 }}
            </div>
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
          <div
            class="stat-icon"
            style="background: rgba(16,185,129,.1); color: #10B981"
          >
            <el-icon :size="22">
              <CircleCheck />
            </el-icon>
          </div>
          <div class="stat-body">
            <div class="stat-label">
              已验证
            </div>
            <div class="stat-value">
              {{ summary.verified || 0 }}
            </div>
            <div class="stat-extra">
              验证率 {{ summary.verifyRate || 0 }}%
            </div>
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
          <div
            class="stat-icon"
            style="background: rgba(245,158,11,.1); color: #F59E0B"
          >
            <el-icon :size="22">
              <Warning />
            </el-icon>
          </div>
          <div class="stat-body">
            <div class="stat-label">
              未验证
            </div>
            <div class="stat-value">
              {{ summary.unverified || 0 }}
            </div>
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
          <div
            class="stat-icon"
            style="background: rgba(99,102,241,.1); color: #6366F1"
          >
            <el-icon :size="22">
              <Plus />
            </el-icon>
          </div>
          <div class="stat-body">
            <div class="stat-label">
              本月新增
            </div>
            <div class="stat-value">
              {{ summary.thisMonth || 0 }}
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    
    <el-row :gutter="16">
      <el-col
        :xs="24"
        :lg="14"
      >
        <el-card
          class="chart-card"
          shadow="never"
        >
          <template #header>
            <div class="card-header">
              <span>每日新增趋势（近 30 天）</span>
              <el-radio-group
                v-model="trendType"
                size="small"
                @change="renderTrend"
              >
                <el-radio-button label="line">
                  折线
                </el-radio-button>
                <el-radio-button label="bar">
                  柱状
                </el-radio-button>
              </el-radio-group>
            </div>
          </template>
          <div
            ref="trendRef"
            class="chart-box"
          />
        </el-card>
      </el-col>
      <el-col
        :xs="24"
        :lg="10"
      >
        <el-card
          class="chart-card"
          shadow="never"
        >
          <template #header>
            <span>渠道来源分布</span>
          </template>
          <div
            ref="sourceRef"
            class="chart-box"
          />
        </el-card>
      </el-col>
    </el-row>

    <el-row :gutter="16">
      <el-col
        :xs="24"
        :lg="12"
      >
        <el-card
          class="chart-card"
          shadow="never"
        >
          <template #header>
            <span>线索类型分布</span>
          </template>
          <div
            ref="typeRef"
            class="chart-box"
          />
        </el-card>
      </el-col>
      <el-col
        :xs="24"
        :lg="12"
      >
        <el-card
          class="chart-card"
          shadow="never"
        >
          <template #header>
            <span>验证状态分布</span>
          </template>
          <div
            ref="verifyRef"
            class="chart-box"
          />
        </el-card>
      </el-col>
    </el-row>

    
    <el-card
      class="table-card"
      shadow="never"
    >
      <template #header>
        <span>各类型线索统计</span>
      </template>
      <el-table
        v-loading="loading"
        :data="typeStats"
        stripe
        :empty-text="'暂无统计数据'"
      >
        <el-table-column
          prop="type"
          label="线索类型"
          min-width="140"
        >
          <template #default="{ row }">
            <el-tag size="small">
              {{ getClueType(row.type) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="total"
          label="总数"
          min-width="100"
          align="center"
        />
        <el-table-column
          prop="verify_total"
          label="已验证"
          min-width="100"
          align="center"
        />
        <el-table-column
          prop="unverified"
          label="未验证"
          min-width="100"
          align="center"
        >
          <template #default="{ row }">
            {{ (row.total || 0) - (row.verify_total || 0) }}
          </template>
        </el-table-column>
        <el-table-column
          label="验证率"
          min-width="220"
        >
          <template #default="{ row }">
            <el-progress
              :percentage="row.total ? Math.round(((row.verify_total || 0) * 1000) / row.total) / 10 : 0"
              :stroke-width="10"
              :format="(p) => `${p}%`"
            />
          </template>
        </el-table-column>
        <el-table-column
          prop="today"
          label="今日新增"
          min-width="100"
          align="center"
        />
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted, nextTick, onBeforeUnmount } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh, DataLine, CircleCheck, Warning, Plus } from '@element-plus/icons-vue'
import { safeInit } from '@/utils/echarts'
import { clueApi } from '@/api/clue'
import { getClueTypeLabel } from '@/constants/cardPlatform';

const dateRange = ref([])
const loading = ref(false)
const summary = ref({ total: 0, verified: 0, unverified: 0, verifyRate: 0, thisMonth: 0 })
const typeStats = ref([])
const sourceStats = ref([])
const trendData = ref([])
const trendType = ref('line')

const trendRef = ref(null)
const sourceRef = ref(null)
const typeRef = ref(null)
const verifyRef = ref(null)
const charts = new Map()

const getClueType = (t) => getClueTypeLabel(t)

/**
 * 分页拉全量线索行（列表接口单页上限由后端分页决定，固定 limit=1000 会在
 * 数据量超过一页时静默截断，统计随之偏小）。每页 500，最多 20 页；达到上限
 * 时保留已取数据并在控制台留痕，至少不会把「截断」伪装成完整统计。
 */
const fetchClueRows = async (filters) => {
  const pageSize = 500
  const maxPages = 20
  const rows = []
  for (let page = 1; page <= maxPages; page += 1) {
    const res = await clueApi.list(page, pageSize, filters)
    const list = Array.isArray(res?.list) ? res.list : []
    rows.push(...list)
    const total = Number(res?.total || 0)
    if (list.length < pageSize) return rows
    if (total > 0 && rows.length >= total) return rows
  }
  console.warn('[clue/statistics] 线索行数超过 10000，统计基于前 10000 行')
  return rows
}

/**
 * 选了日期区间时，用列表接口（支持 start/end，unix 秒）拉区间内线索，
 * 在前端按类型聚合成统计行。
 *
 * 为什么不能直接把区间传给 /api/clues/statistics：该接口只返回「按类型的
 * 全量汇总」，没有任何时间参数，传了也不会生效——旧实现用 el-select 假装
 * 是日期控件，下拉里只有「无数据」，筛不出任何东西（UI 审计实测）。
 * 这里走列表接口做真实聚合，保证控件与数据一致。
 */
const fetchRangeStats = async ([startDay, endDay]) => {
  const start = Math.floor(new Date(`${startDay}T00:00:00`).getTime() / 1000)
  const end = Math.floor(new Date(`${endDay}T23:59:59`).getTime() / 1000)
  const todayStart = Math.floor(new Date(new Date().setHours(0, 0, 0, 0)).getTime() / 1000)
  const rows = await fetchClueRows({ start, end })
  const byType = new Map()
  for (const row of rows) {
    const key = String(row.type)
    const item = byType.get(key) || { type: row.type, total: 0, verify_total: 0, today: 0 }
    item.total += 1
    if (Number(row.is_verify) === 1) item.verify_total += 1
    if (Number(row.create_time) >= todayStart) item.today += 1
    byType.set(key, item)
  }
  return [...byType.values()]
}

/**
 * 「本月新增」等按天统计需要真实数据：拉最近 30 天的线索（list 接口，
 * start=30 天前 0 点），按 create_time 落在哪一天聚合。
 * 返回 { daily: Map<'MM-DD', count>, monthCount }。
 */
const fetchDailyCounts = async () => {
  const now = new Date()
  const monthStart = new Date(now.getFullYear(), now.getMonth(), 1)
  const days30Start = new Date(monthStart.getTime() - 29 * 86400000) // 保证趋势窗口完整覆盖本月
  const start = Math.floor(new Date(days30Start.getFullYear(), days30Start.getMonth(), days30Start.getDate()).getTime() / 1000)
  const rows = await fetchClueRows({ start })
  const daily = new Map()
  let monthCount = 0
  for (const row of rows) {
    const d = new Date(Number(row.create_time) * 1000)
    const key = `${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
    daily.set(key, (daily.get(key) || 0) + 1)
    if (d >= monthStart) monthCount += 1
  }
  return { daily, monthCount }
}

const fetchAll = async () => {
  loading.value = true
  try {
    const hasRange = Array.isArray(dateRange.value) && dateRange.value.length === 2 && dateRange.value[0] && dateRange.value[1]
    const statsRes = hasRange ? await fetchRangeStats(dateRange.value) : await clueApi.statistics()
    const statsList = Array.isArray(statsRes) ? statsRes : (statsRes?.list || [])
    typeStats.value = statsList.map((it) => ({
      type: it.type,
      total: it.total || 0,
      verify_total: it.verify_total || 0,
      today: it.today || 0
    }))

    // 「本月新增」「每日趋势」「来源分布」都需要按天的真实数据；
    // 无日期区间时才拉（有区间时趋势/本月口径与区间无关，保持空态语义更诚实）。
    let daily = new Map()
    let monthCount = 0
    if (!hasRange) {
      const d = await fetchDailyCounts()
      daily = d.daily
      monthCount = d.monthCount
    }

    const total = typeStats.value.reduce((s, x) => s + (x.total || 0), 0);
    const verified = typeStats.value.reduce((s, x) => s + (x.verify_total || 0), 0)
    summary.value = {
      total,
      verified,
      unverified: total - verified,
      verifyRate: total ? Math.round((verified * 1000) / total) / 10 : 0,
      thisMonth: monthCount
    }

    // 来源分布：线索类型本身就是渠道型枚举（CLUE_TYPE），直接按类型真实聚合，
    // 不再用固定渠道列表 + Math.sin 编造（旧实现是假数据，已删）。
    sourceStats.value = typeStats.value.map((x) => ({
      name: getClueType(x.type),
      value: x.total || 0
    }))

    // 每日新增趋势：按 create_time 落天的真实计数，一天一格，无数据的日子就是 0，
    // 不再用 sin/random 编造曲线（旧实现是假数据，已删）。
    trendData.value = buildTrend(daily);

    await nextTick()
    renderAll()
  } catch {
    ElMessage.warning('后端统计接口暂不可用，已使用空数据展示')
    summary.value = { total: 0, verified: 0, unverified: 0, verifyRate: 0, thisMonth: 0 }
    typeStats.value = []
    sourceStats.value = []
    trendData.value = []
    await nextTick()
    renderAll()
  } finally {
    loading.value = false
  }
}

const buildTrend = (daily) => {
  // 30 天窗口，与图例「每日新增趋势（近 30 天）」一致；daily 是 Map<'MM-DD', count>。
  const days = 30
  const today = new Date()
  const labels = []
  const values = []
  for (let i = days - 1; i >= 0; i--) {
    const d = new Date(today.getTime() - i * 86400000)
    const m = String(d.getMonth() + 1).padStart(2, '0')
    const day = String(d.getDate()).padStart(2, '0')
    labels.push(`${m}-${day}`)
    values.push(daily.get(`${m}-${day}`) || 0)
  }
  return { labels, values }
}

const renderAll = () => {
  renderTrend()
  renderSource()
  renderType()
  renderVerify()
}

// 注意：这张页有 4 个图，必须按「DOM 元素」分别保存实例。
// 旧实现用 `const charts = {}` 以元素为 key，JS 对象会把元素强转成字符串
// "[object HTMLDivElement]"，4 个图共用一个 key —— 第一个图初始化后，后 3 个
// initChart 直接拿到同一个实例，setOption 互相覆盖，结果页面上只有第一张图
// 有画布、且显示的是最后一张图（验证状态分布）的饼图。改用 Map 按元素保存。
const initChart = (refEl) => {
  if (!refEl) return null
  const existing = charts.get(refEl)
  if (existing) return existing
  const inst = safeInit(refEl)
  if (inst) charts.set(refEl, inst)
  return inst
}

const renderTrend = () => {
  const el = trendRef.value
  if (!el) return
  const c = initChart(el)
  if (!c) return
  c.setOption({
    tooltip: { trigger: 'axis' },
    grid: { left: 30, right: 16, top: 20, bottom: 30 },
    xAxis: { type: 'category', data: trendData.value.labels || [] },
    yAxis: { type: 'value' },
    series: [
      {
        type: trendType.value,
        smooth: true,
        data: trendData.value.values || [],
        areaStyle: trendType.value === 'line' ? { opacity: 0.15 } : undefined,
        itemStyle: { color: '#4F46E5' }
      }
    ]
  })
}

const renderSource = () => {
  const el = sourceRef.value
  if (!el) return
  const c = initChart(el)
  if (!c) return
  c.setOption({
    tooltip: { trigger: 'item' },
    legend: { bottom: 0, type: 'scroll' },
    series: [
      {
        type: 'pie',
        radius: ['40%', '70%'],
        avoidLabelOverlap: true,
        data: sourceStats.value,
        label: { formatter: '{b}\n{d}%' }
      }
    ]
  })
}

const renderType = () => {
  const el = typeRef.value
  if (!el) return
  const c = initChart(el)
  if (!c) return
  c.setOption({
    tooltip: { trigger: 'axis' },
    grid: { left: 80, right: 16, top: 20, bottom: 30 },
    xAxis: { type: 'value' },
    yAxis: { type: 'category', data: typeStats.value.map((t) => getClueType(t.type)) },
    series: [
      {
        type: 'bar',
        data: typeStats.value.map((t) => t.total || 0),
        itemStyle: { color: '#10B981' }
      }
    ]
  })
}

const renderVerify = () => {
  const el = verifyRef.value
  if (!el) return
  const c = initChart(el)
  if (!c) return
  const verified = summary.value.verified || 0
  const unverified = summary.value.unverified || 0
  c.setOption({
    tooltip: { trigger: 'item' },
    legend: { bottom: 0 },
    series: [
      {
        type: 'pie',
        radius: ['40%', '70%'],
        data: [
          { name: '已验证', value: verified, itemStyle: { color: '#10B981' } },
          { name: '未验证', value: unverified, itemStyle: { color: '#F59E0B' } }
        ],
        label: { formatter: '{b}\n{d}%' }
      }
    ]
  })
}

const onResize = () => {
  charts.forEach((c) => c && c.resize && c.resize())
}

onMounted(() => {
  fetchAll()
  window.addEventListener('resize', onResize)
})
onBeforeUnmount(() => {
  window.removeEventListener('resize', onResize)
  charts.forEach((c) => c && c.dispose && c.dispose())
  charts.clear()
})
</script>

<style lang="scss" scoped>
.clue-stats-page {
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
      flex-wrap: wrap;
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
        align-items: center;
      }
    }
  }

  .stats-row {
    .stat-card {
      display: flex;
      align-items: center;
      gap: 12px;
      .stat-icon {
        width: 48px;
        height: 48px;
        border-radius: 8px;
        display: flex;
        align-items: center;
        justify-content: center;
        flex-shrink: 0;
      }
      .stat-body {
        flex: 1;
        .stat-label {
          font-size: 13px;
          color: #909399;
        }
        .stat-value {
          font-size: 22px;
          font-weight: 600;
          color: #303133;
          line-height: 1.2;
          margin-top: 4px;
        }
        .stat-extra {
          font-size: 12px;
          color: #10B981;
          margin-top: 4px;
        }
      }
    }
  }

  .chart-card {
    .card-header {
      display: flex;
      justify-content: space-between;
      align-items: center;
    }
    .chart-box {
      width: 100%;
      height: 320px;
    }
  }
}
</style>
