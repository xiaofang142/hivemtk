<template>
  <div class="geo-page p-4">
    <!-- 概览卡片区 -->
    <el-row :gutter="12" class="mb-4">
      <el-col :xs="12" :sm="6">
        <el-card shadow="hover">
          <div class="stat-block">
            <div class="stat-label">AI 可见率（近 {{ days }} 天）</div>
            <div class="stat-value" :style="{ color: visibilityColor(summary.current_avg) }">
              {{ pct(summary.current_avg) }}
            </div>
            <div class="stat-sub" v-if="hasCompare">
              环比
              <span :class="summary.change >= 0 ? 'trend-up' : 'trend-down'">
                {{ summary.change >= 0 ? '↑' : '↓' }}{{ pct(Math.abs(summary.change)) }}
              </span>
            </div>
            <div class="stat-sub muted" v-else>数据不足，暂无环比</div>
          </div>
        </el-card>
      </el-col>
      <el-col :xs="12" :sm="6">
        <el-card shadow="hover">
          <div class="stat-block">
            <div class="stat-label">探针总数</div>
            <div class="stat-value">{{ formatInt(summary.total_probes) }}</div>
            <div class="stat-sub">品牌命中 {{ formatInt(summary.total_brand_hits) }} 次</div>
          </div>
        </el-card>
      </el-col>
      <el-col :xs="12" :sm="6">
        <el-card shadow="hover">
          <div class="stat-block">
            <div class="stat-label">引擎覆盖</div>
            <div class="stat-value">{{ engines.length }}</div>
            <div class="stat-sub">最弱引擎：{{ weakestEngine || '—' }}</div>
          </div>
        </el-card>
      </el-col>
      <el-col :xs="12" :sm="6">
        <el-card shadow="hover">
          <div class="stat-block">
            <div class="stat-label">负面提及</div>
            <div class="stat-value" :class="totalNegative > 0 ? 'text-danger' : ''">{{ formatInt(totalNegative) }}</div>
            <div class="stat-sub">0 为安全，&gt;0 需到告警中心排查</div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <!-- 筛选 + 操作 -->
    <el-card class="mb-4" shadow="never">
      <div class="flex items-center gap-3 flex-wrap">
        <el-radio-group v-model="days" @change="loadAll">
          <el-radio-button :value="7">近 7 天</el-radio-button>
          <el-radio-button :value="30">近 30 天</el-radio-button>
          <el-radio-button :value="90">近 90 天</el-radio-button>
        </el-radio-group>
        <el-button type="primary" :loading="probing" @click="runProbe" plain>
          <el-icon><VideoPlay /></el-icon><span>立即探测一轮</span>
        </el-button>
        <el-button @click="loadAll" :loading="loading">刷新</el-button>
      </div>
    </el-card>

    <!-- 可见性趋势折线图 -->
    <el-card class="mb-4" shadow="never">
      <template #header>
        <span class="font-bold">品牌可见率趋势（品牌被 AI 引擎提及的探针占比）</span>
      </template>
      <div v-if="!loading && !hasTrendData" class="py-12 text-center text-gray-400">
        暂无观测数据。点击上方「立即探测一轮」，或在「多模型验证」页运行验证后回来看趋势。
      </div>
      <div ref="trendChartRef" v-show="hasTrendData" style="height: 320px"></div>
    </el-card>

    <!-- 引擎对比 -->
    <el-card class="mb-4" shadow="never">
      <template #header>
        <span class="font-bold">引擎对比 — 谁看得见你，谁在忽略你</span>
      </template>
      <el-empty v-if="!loading && engines.length === 0" description="暂无引擎数据" :image-size="60" />
      <el-table v-else :data="engines" v-loading="loading" size="default">
        <el-table-column prop="engine" label="引擎" min-width="160">
          <template #default="{ row }">
            <span class="font-semibold">{{ row.engine }}</span>
          </template>
        </el-table-column>
        <el-table-column label="可见率" min-width="220">
          <template #default="{ row }">
            <div class="flex items-center gap-2">
              <el-progress
                :percentage="Math.round(row.visibility * 100)"
                :stroke-width="10"
                :color="visibilityColor(row.visibility)"
                style="flex: 1"
              />
              <span class="font-mono w-12 text-right">{{ pct(row.visibility) }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="probe_count" label="探针数" width="100" align="right" sortable />
        <el-table-column prop="brand_hits" label="品牌命中" width="100" align="right" sortable />
        <el-table-column label="平均引用" width="100" align="right">
          <template #default="{ row }">{{ (row.avg_citations || 0).toFixed(2) }}</template>
        </el-table-column>
        <el-table-column label="负面" width="90" align="right">
          <template #default="{ row }">
            <el-tag v-if="row.negative_count > 0" type="danger" size="small">{{ row.negative_count }}</el-tag>
            <span v-else class="text-gray-400">0</span>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- 探针运行记录 -->
    <el-card shadow="never">
      <template #header>
        <div class="flex items-center justify-between">
          <span class="font-bold">探针运行记录（最近 {{ runsLimit }} 条）</span>
          <div class="flex items-center gap-2">
            <el-select v-model="runsEngine" placeholder="全部引擎" clearable size="small" style="width: 160px" @change="loadRuns">
              <el-option v-for="e in engines" :key="e.engine" :label="e.engine" :value="e.engine" />
            </el-select>
            <el-button size="small" @click="loadRuns" :loading="runsLoading">刷新</el-button>
          </div>
        </div>
      </template>
      <el-table :data="runs" v-loading="runsLoading" size="small">
        <el-table-column prop="created_at" label="时间" width="160">
          <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column prop="engine" label="引擎" width="150">
          <template #default="{ row }"><el-tag size="small" effect="plain">{{ row.engine }}</el-tag></template>
        </el-table-column>
        <el-table-column prop="query" label="探测查询" min-width="180" show-overflow-tooltip />
        <el-table-column label="品牌" width="90" align="center">
          <template #default="{ row }">
            <el-tag :type="row.brand_mentioned ? 'success' : 'info'" size="small">
              {{ row.brand_mentioned ? '提及' : '未提及' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="情感" width="80" align="center">
          <template #default="{ row }">
            <el-tag size="small" :type="sentimentType(row.sentiment)">{{ sentimentLabel(row.sentiment) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="latency_ms" label="耗时(ms)" width="100" align="right" />
        <el-table-column label="回答摘要" min-width="240">
          <template #default="{ row }">
            <el-tooltip :content="snippet(row.response)" placement="top" :show-after="200">
              <span class="text-gray-500 text-xs">{{ snippet(row.response) }}</span>
            </el-tooltip>
          </template>
        </el-table-column>
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { ElMessage } from 'element-plus'
import { VideoPlay } from '@element-plus/icons-vue'
import * as echarts from 'echarts'
import { safeInit } from '@/utils/echarts'
import { getEngineCompare, listProbeRuns, probeAllEngines } from '@/api/geoProbe.js'
import { toList } from '@/utils/list'

const days = ref(30)
const loading = ref(false)
const summary = ref({})
const engines = ref([])
const daily = ref([])

const runs = ref([])
const runsLoading = ref(false)
const runsEngine = ref('')
const runsLimit = 50
const probing = ref(false)

const trendChartRef = ref(null)
let chartInstance = null

const hasTrendData = computed(() => (summary.value?.points?.length || 0) > 0)
const hasCompare = computed(() => (summary.value?.total_probes || 0) > 0 && (summary.value?.previous_avg || 0) > 0)
const totalNegative = computed(() => engines.value.reduce((s, e) => s + (e.negative_count || 0), 0))
const weakestEngine = computed(() => {
  if (!engines.value.length) return ''
  const worst = [...engines.value].sort((a, b) => a.visibility - b.visibility)[0]
  return `${worst.engine} ${pct(worst.visibility)}`
})

const pct = (v) => `${((Number(v) || 0) * 100).toFixed(1)}%`
const visibilityColor = (v) => {
  const n = (Number(v) || 0) * 100
  if (n >= 60) return '#22c55e'
  if (n >= 30) return '#f59e0b'
  return '#ef4444'
}
const formatInt = (n) => Number(n || 0).toLocaleString('zh-CN')
const formatTime = (t) => (t ? new Date(t).toLocaleString('zh-CN', { hour12: false }) : '-')
const sentimentType = (s) => ({ positive: 'success', negative: 'danger' }[s] || 'info')
const sentimentLabel = (s) => ({ positive: '积极', negative: '负面' }[s] || '中性')
const snippet = (text) => {
  const t = String(text || '').replace(/\s+/g, ' ').trim()
  return t.length > 60 ? t.slice(0, 60) + '…' : t || '—'
}

const renderTrendChart = () => {
  const points = summary.value?.points || []
  if (!trendChartRef.value || points.length === 0) return
  if (!chartInstance) {
    chartInstance = safeInit(trendChartRef.value)
    window.addEventListener('resize', handleResize)
  }
  const dates = points.map((p) => p.date)
  chartInstance.setOption({
    tooltip: { trigger: 'axis' },
    legend: { data: ['可见率', '探针数'] },
    grid: { left: 60, right: 60, top: 40, bottom: 30 },
    xAxis: { type: 'category', data: dates },
    yAxis: [
      { type: 'value', name: '可见率', min: 0, max: 1, axisLabel: { formatter: (v) => `${Math.round(v * 100)}%` } },
      { type: 'value', name: '探针数', minInterval: 1 }
    ],
    series: [
      {
        name: '可见率',
        type: 'line',
        smooth: true,
        data: points.map((p) => p.visibility),
        itemStyle: { color: '#409eff' },
        areaStyle: { opacity: 0.12 }
      },
      {
        name: '探针数',
        type: 'bar',
        yAxisIndex: 1,
        data: points.map((p) => p.probe_count),
        itemStyle: { color: '#d3dce6', opacity: 0.7 },
        barMaxWidth: 18
      }
    ]
  }, true)
}

const handleResize = () => chartInstance && chartInstance.resize()

const loadAll = async () => {
  loading.value = true
  try {
    const data = await getEngineCompare(days.value)
    summary.value = data?.summary || {}
    engines.value = data?.engines || []
    daily.value = data?.daily || []
    await nextTick()
    renderTrendChart()
    await loadRuns()
  } catch (e) {
    ElMessage.error(e?.message || '观测数据加载失败')
  } finally {
    loading.value = false
  }
}

const loadRuns = async () => {
  runsLoading.value = true
  try {
    const res = await listProbeRuns(runsEngine.value || undefined, runsLimit)
    runs.value = toList(res)
  } catch {
    runs.value = []
  } finally {
    runsLoading.value = false
  }
}

const runProbe = async () => {
  probing.value = true
  try {
    await probeAllEngines([], '这款产品的主要优势和适用场景是什么？')
    ElMessage.success('探测已启动，云端引擎返回后数据自动落库，稍后刷新查看')
    setTimeout(loadAll, 8000)
  } catch (e) {
    ElMessage.error(e?.message || '探测触发失败')
  } finally {
    probing.value = false
  }
}

onMounted(loadAll)
onBeforeUnmount(() => {
  window.removeEventListener('resize', handleResize)
  if (chartInstance) chartInstance.dispose()
})
</script>

<style lang="scss" scoped>
.stat-block {
  text-align: center;
  padding: 4px 0;
}
.stat-label {
  font-size: 12px;
  color: #909399;
}
.stat-value {
  font-size: 28px;
  font-weight: 700;
  margin: 4px 0;
  color: #303133;
}
.stat-value.text-danger {
  color: #ef4444;
}
.stat-sub {
  font-size: 12px;
  color: #606266;
}
.stat-sub.muted {
  color: #c0c4cc;
}
.trend-up {
  color: #22c55e;
  font-weight: 600;
}
.trend-down {
  color: #ef4444;
  font-weight: 600;
}
.font-mono {
  font-family: 'JetBrains Mono', Consolas, monospace;
}
</style>
