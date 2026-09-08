<template>
  <div class="geo-page">

    <div class="p-4">
    <el-row :gutter="16" class="mb-4">
      <el-col :span="6">
        <el-card>
          <el-statistic title="HiveMTK SOV" :value="hiveMTKSov" suffix="%" :precision="2">
            <template #suffix>
              <span v-if="hiveMTKSovTrend !== null"
                :class="hiveMTKSovTrend < 0 ? 'text-red-500' : 'text-green-500'"
                style="font-size:12px;margin-left:4px">
                {{ hiveMTKSovTrend >= 0 ? '↑' : '↓' }}{{ Math.abs(hiveMTKSovTrend).toFixed(2) }}%
              </span>
              <span v-else class="text-gray-400" style="font-size:12px;margin-left:4px">首期</span>
            </template>
          </el-statistic>
          <div class="text-xs text-gray-400 mt-1">较上一 {{ trendDays }} 天窗口</div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card>
          <el-statistic title="竞品数（出现/配置）" :value="summary.competitor_count" />
          <div class="text-xs text-gray-400 mt-1">配置 {{ configuredCompetitors.length }} 个</div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card><el-statistic title="总提及次数" :value="summary.total_mentions" /></el-card>
      </el-col>
      <el-col :span="6">
        <el-card><el-statistic title="品牌提及次数" :value="hiveMTKMentions" /></el-card>
      </el-col>
    </el-row>

    <!-- 竞品覆盖缺口提示 -->
    <el-alert
      v-if="uncoveredCompetitors.length"
      type="warning"
      :closable="false"
      class="mb-4"
      show-icon
    >
      <template #title>
        竞品覆盖缺口：以下竞品在探针记录中零提及（没进 AI 回答，也是你的内容机会点）
        <el-tag v-for="c in uncoveredCompetitors" :key="c" size="small" class="ml-1">{{ c }}</el-tag>
      </template>
    </el-alert>

    <el-card class="mb-4">
      <template #header><span class="font-bold">品牌 SOV 对比（AI 引擎声量份额）</span></template>
      <div v-if="!loading && sovData.length === 0" class="py-12 text-center text-gray-400">
        暂无 SOV 数据，请先在"多模型验证"页面运行品牌验证，或到「可见性观测」页点「立即探测一轮」
      </div>
      <div ref="barChartRef" v-show="sovData.length > 0" style="height:320px"></div>
    </el-card>


    <el-card class="mb-4">
      <template #header>
        <div class="flex items-center gap-4">
          <span class="font-bold">品牌明细</span>
          <el-button size="small" type="primary" @click="load" :loading="loading">刷新</el-button>
        </div>
      </template>
      <el-table :data="sovData" v-loading="loading" size="small">
        <el-table-column label="品牌" min-width="140">
          <template #default="{ row }">
            <div class="flex items-center gap-2">
              <el-tag v-if="row.is_own" type="primary" size="small" effect="dark">OWN</el-tag>
              <span :class="row.is_own ? 'font-bold text-primary' : ''">{{ row.brand }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="mentions" label="提及次数" width="100" sortable />
        <el-table-column prop="sov_percent" label="SOV(%)" width="110" sortable>
          <template #default="{ row }">
            <div class="flex items-center gap-2">
              <el-progress :percentage="row.sov_percent" :stroke-width="8" :show-text="false"
                :color="row.is_own ? '#409eff' : (row.sov_percent >= 20 ? '#67c23a' : row.sov_percent >= 10 ? '#e6a23c' : '#f56c6c')"
                style="flex:1" />
              <span class="text-xs w-12 text-right">{{ row.sov_percent.toFixed(2) }}%</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="环比" width="110">
          <template #default="{ row }">
            <span v-if="rowTrend(row) !== null" :class="rowTrend(row) >= 0 ? 'text-green-600' : 'text-red-500'" class="text-xs font-semibold">
              {{ rowTrend(row) >= 0 ? '↑' : '↓' }} {{ Math.abs(rowTrend(row)).toFixed(2) }}
            </span>
            <span v-else class="text-gray-400 text-xs">—</span>
          </template>
        </el-table-column>
        <el-table-column label="情感倾向" width="100">
          <template #default="{ row }">
            <el-tag :type="sentimentTag(row.avg_sentiment)" size="small">{{ sentimentLabel(row.avg_sentiment) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="total_mentions_all_brands" label="全行业总提及" width="120" />
      </el-table>
    </el-card>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, nextTick, onBeforeUnmount } from 'vue'
import * as echarts from 'echarts'
import { safeInit } from '@/utils/echarts'
import { getSOV, getSOVTrend, listCompetitors } from '@/api/geoProbe.js'

const BRAND_HINTS = ['hivemtk', 'hivemtk']
const trendDays = 30

const loading = ref(false)
const sovData = ref([])
const configuredCompetitors = ref([])
const prevWindowSov = ref(null) // 上一窗口自有品牌 SOV（null = 无基准）
const prevSovList = ref([]) // 上一窗口各品牌 SOV（行级环比用）
const barChartRef = ref(null)
let chartInstance = null

const ownBrand = computed(() => {
  const own = sovData.value.find((r) =>
    BRAND_HINTS.some((h) => (r.brand || '').toLowerCase().includes(h))
  )
  return own?.brand || 'HiveMTK'
})

const hiveMTKEntry = computed(() => sovData.value.find(r => r.is_own))
const hiveMTKSov = computed(() => hiveMTKEntry.value?.sov_percent || 0)
const hiveMTKMentions = computed(() => hiveMTKEntry.value?.mentions || 0)

// SOV 环比：当前窗口 SOV − 上一窗口 SOV（来自 /geo/sov/trend）。
// 每行竞品的环比：上一窗口有该品牌才可比，否则显示"—"。
const prevSovByBrand = computed(() => {
  const m = new Map()
  for (const e of prevSovList.value) m.set((e.brand || '').toLowerCase(), Number(e.sov_percent || 0))
  return m
})
const rowTrend = (row) => {
  const key = (row.brand || '').toLowerCase()
  if (!prevSovByBrand.value.has(key)) return null
  return Number((row.sov_percent - prevSovByBrand.value.get(key)).toFixed(2))
}

// 自有品牌环比（概览卡用）：上一窗口没有自有品牌数据则视为无基准（null），不显示误导性涨跌
const hiveMTKSovTrend = computed(() => {
  if (prevWindowSov.value === null) return null
  return Number((hiveMTKSov.value - prevWindowSov.value).toFixed(2))
})

const summary = computed(() => ({
  competitor_count: Math.max(0, sovData.value.filter(r => !r.is_own).length),
  total_mentions: sovData.value.reduce((s, r) => s + r.mentions, 0)
}))

// 竞品覆盖缺口：geo_competitors 配置了但探针记录中没出现的竞品
const uncoveredCompetitors = computed(() => {
  const seen = new Set(sovData.value.map((r) => (r.brand || '').toLowerCase()))
  return configuredCompetitors.value.filter(
    (c) => c && !seen.has(c.toLowerCase()) && !BRAND_HINTS.some((h) => c.toLowerCase().includes(h))
  )
})

const sentimentTag = (s) => {
  if (s === 'positive') return 'success'
  if (s === 'negative') return 'danger'
  return 'info'
}
const sentimentLabel = (s) => {
  if (s === 'positive') return '积极'
  if (s === 'negative') return '负面'
  return '中性'
}

const renderBarChart = () => {
  if (!barChartRef.value || sovData.value.length === 0) return
  if (!chartInstance) {
    chartInstance = safeInit(barChartRef.value)
    window.addEventListener('resize', handleResize)
  }
  const sorted = [...sovData.value].sort((a, b) => b.sov_percent - a.sov_percent)
  chartInstance.setOption({
    tooltip: { trigger: 'axis', formatter: (p) => `${p[0].name}: ${Number(p[0].value).toFixed(2)}%` },
    grid: { left: 50, right: 20, top: 20, bottom: 30 },
    xAxis: { type: 'category', data: sorted.map(r => r.brand), axisLabel: { rotate: 30, fontSize: 12 } },
    yAxis: { type: 'value', name: 'SOV %', max: 100 },
    series: [{
      type: 'bar',
      data: sorted.map(r => ({
        value: r.sov_percent,
        itemStyle: { color: r.is_own ? '#409eff' : '#909399' }
      })),
      barWidth: '40%',
      label: { show: true, position: 'top', formatter: (p) => `${Number(p.value).toFixed(2)}%`, fontSize: 11 }
    }]
  })
}

const handleResize = () => chartInstance && chartInstance.resize()

const load = async () => {
  loading.value = true
  try {
    const results = await Promise.allSettled([getSOV(), getSOVTrend(trendDays), listCompetitors()])

    // 竞品配置（覆盖缺口对比用）
    if (results[2].status === 'fulfilled') {
      const compRes = results[2].value
      const compList = Array.isArray(compRes) ? compRes : (compRes?.list || compRes?.data || [])
      configuredCompetitors.value = compList
        .map((c) => (typeof c === 'string' ? c : (c?.name || c?.brand || '')))
        .filter(Boolean)
    }

    // 上一窗口各品牌 SOV（行级环比基准）
    if (results[1].status === 'fulfilled') {
      const t = results[1].value || {}
      const prev = Array.isArray(t.previous) ? t.previous : []
      prevSovList.value = prev
      const ownEntry = prev.find((e) => BRAND_HINTS.some((h) => (e.brand || '').toLowerCase().includes(h)))
      prevWindowSov.value = ownEntry ? Number(ownEntry.sov_percent || 0) : null
    } else {
      prevSovList.value = []
      prevWindowSov.value = null
    }

    // 主 SOV 数据
    if (results[0].status === 'fulfilled') {
      const data = results[0].value
      const list = Array.isArray(data) ? data : (data?.list || data?.data || []);
      sovData.value = list.map(r => ({
        brand: r.brand || '未知',
        mentions: Number(r.mentions || 0),
        total_mentions_all_brands: Number(r.total_mentions_all_brands || 0),
        sov_percent: Number(r.sov_percent || 0),
        avg_sentiment: r.avg_sentiment || 'neutral',
        is_own: (r.brand || '').toLowerCase() === ownBrand.value.toLowerCase() ||
          BRAND_HINTS.some((h) => (r.brand || '').toLowerCase().includes(h))
      }))
    } else {
      sovData.value = []
    }

    await nextTick()
    renderBarChart()
  } catch (e) {
    sovData.value = []
  } finally {
    loading.value = false
  }
}

onMounted(load)
onBeforeUnmount(() => {
  window.removeEventListener('resize', handleResize)
  if (chartInstance) chartInstance.dispose()
})
</script>
