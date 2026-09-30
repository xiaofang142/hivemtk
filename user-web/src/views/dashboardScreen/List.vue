<template>
  <div class="dashboard-screen-page">
    <div class="dashboard-header">
      <h1>{{ $t('营销数据大屏') }}</h1>
      <div class="header-info">
        <span class="time">{{ currentTime }}</span>
        <el-button type="primary" @click="$router.push('/dashboardScreen/builder')">大屏构建</el-button>
        <el-button type="primary" @click="toggleFullscreen">
          <el-icon><FullScreen /></el-icon>
          {{ isFullscreen ? '退出全屏' : '全屏' }}
        </el-button>
      </div>
    </div>

    <el-row :gutter="15" class="kpi-row">
      <el-col :span="4" v-for="kpi in kpis" :key="kpi.label">
        <div class="kpi-card" :style="{ background: kpi.color }">
          <div class="kpi-label">{{ kpi.label }}</div>
          <div class="kpi-value">{{ kpi.value }}</div>
          <div class="kpi-trend" :class="{ up: kpi.trend > 0, down: kpi.trend < 0 }">
            <span v-if="kpi.trend > 0">▲ {{ kpi.trend }}%</span>
            <span v-else-if="kpi.trend < 0">▼ {{ Math.abs(kpi.trend) }}%</span>
            <span v-else>-</span>
            <span class="trend-label">{{ $t('较昨日') }}</span>
          </div>
        </div>
      </el-col>
    </el-row>

    <el-row :gutter="15" class="charts-row">
      <el-col :span="12">
        <el-card class="chart-card">
          <template #header>
            <span>营销趋势 (近30天)</span>
          </template>
          <div ref="trendChartRef" class="chart-container"></div>
        </el-card>
      </el-col>
      <el-col :span="12">
        <el-card class="chart-card">
          <template #header>
            <span>渠道分布</span>
          </template>
          <div ref="channelChartRef" class="chart-container"></div>
        </el-card>
      </el-col>
    </el-row>

    <el-row :gutter="15" class="charts-row">
      <el-col :span="8">
        <el-card class="chart-card">
          <template #header>
            <span>用户来源 TOP5</span>
          </template>
          <div ref="sourceChartRef" class="chart-container"></div>
        </el-card>
      </el-col>
      <el-col :span="8">
        <el-card class="chart-card">
          <template #header>
            <span>漏斗分析</span>
          </template>
          <div ref="funnelChartRef" class="chart-container"></div>
        </el-card>
      </el-col>
      <el-col :span="8">
        <el-card class="chart-card">
          <template #header>
            <span>实时活动</span>
          </template>
          <div class="realtime-list">
            <div v-for="(item, idx) in realtimeActivities" :key="idx" class="activity-item">
              <el-icon :color="item.color"><component :is="item.icon" /></el-icon>
              <span class="text">{{ item.text }}</span>
              <span class="time">{{ item.time }}</span>
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-row :gutter="15" class="charts-row">
      <el-col :span="12">
        <el-card class="chart-card">
          <template #header>
            <span>地区分布</span>
          </template>
          <div ref="mapChartRef" class="chart-container"></div>
        </el-card>
      </el-col>
      <el-col :span="12">
        <el-card class="chart-card">
          <template #header>
            <span>转化率对比</span>
          </template>
          <div ref="conversionChartRef" class="chart-container"></div>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, markRaw, onMounted, onUnmounted } from 'vue'
import { FullScreen, User, ShoppingCart, ChatLineRound, Document, Promotion, Bell, Money } from '@element-plus/icons-vue'
import * as echarts from 'echarts'
import { safeInit } from '@/utils/echarts'
import { getDashboardData, getRealtimeActivities } from '@/api/dashboardScreen.js'

const currentTime = ref('')
const isFullscreen = ref(false)
const kpis = ref([])
const realtimeActivities = ref([])
const trendChartRef = ref()
const channelChartRef = ref()
const sourceChartRef = ref()
const funnelChartRef = ref()
const mapChartRef = ref()
const conversionChartRef = ref()

let timer
let trendChart, channelChart, sourceChart, funnelChart, mapChart, conversionChart

const updateTime = () => {
  const now = new Date()
  currentTime.value = now.toLocaleString('zh-CN', { hour12: false })
}

const loadData = async () => {
  const res = await getDashboardData()
  const data = res;
  kpis.value = data.kpis || []
  initCharts(data)
}

const activityMeta = {
  clue: { icon: Document, color: '#5470c6' },
  order: { icon: ShoppingCart, color: '#91cc75' },
  customer: { icon: User, color: '#ee6666' },
  message: { icon: ChatLineRound, color: '#fac858' }
}
// icon 是 Vue 组件对象，不是业务数据。realtimeActivities 是深响应 ref，组件对象落进去会被
// reactive 代理，模板 `<component :is="item.icon" />` 取到代理后的组件就会触发
// "Vue received a Component that was made a reactive object" 警告（实测 20 条 / 每条活动 1 条）。
// markRaw 标记为不可代理，与 dashboardScreen/Builder.vue 的既有写法一致。
const pickIcon = (t) => markRaw(activityMeta[t]?.icon || Bell)
const pickColor = (t) => (activityMeta[t]?.color) || '#909399'

const loadRealtime = async () => {
  const res = await getRealtimeActivities()
  const list = Array.isArray(res) ? res : [];
  realtimeActivities.value = list.map((a) => ({
    text: a.title || a.user_name || '',
    time: a.created_at ? new Date(a.created_at).toLocaleTimeString('zh-CN', { hour12: false }) : '',
    color: pickColor(a.type),
    icon: pickIcon(a.type)
  }))
}

const initCharts = (data) => {
  if (trendChartRef.value) {
    trendChart = safeInit(trendChartRef.value)
    trendChart.setOption({
      tooltip: { trigger: 'axis' },
      legend: { data: ['访问', '线索', '转化'], textStyle: { color: '#fff' } },
      grid: { left: '3%', right: '4%', bottom: '3%', containLabel: true },
      xAxis: { type: 'category', data: data.trend?.dates || [], axisLine: { lineStyle: { color: '#fff' } } },
      yAxis: { type: 'value', axisLine: { lineStyle: { color: '#fff' } } },
      series: [
        { name: '访问', type: 'line', smooth: true, data: data.trend?.visits || [], itemStyle: { color: '#5470c6' } },
        { name: '线索', type: 'line', smooth: true, data: data.trend?.clues || [], itemStyle: { color: '#91cc75' } },
        { name: '转化', type: 'line', smooth: true, data: data.trend?.conversions || [], itemStyle: { color: '#ee6666' } }
      ]
    })
  }

  if (channelChartRef.value) {
    channelChart = safeInit(channelChartRef.value)
    channelChart.setOption({
      tooltip: { trigger: 'item' },
      legend: { orient: 'vertical', left: 'left', textStyle: { color: '#fff' } },
      series: [{
        type: 'pie',
        radius: '70%',
        data: data.channels || [],
        emphasis: { itemStyle: { shadowBlur: 10, shadowOffsetX: 0, shadowColor: 'rgba(0, 0, 0, 0.5)' } }
      }]
    })
  }

  if (sourceChartRef.value) {
    sourceChart = safeInit(sourceChartRef.value)
    sourceChart.setOption({
      tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' } },
      grid: { left: '3%', right: '4%', bottom: '3%', containLabel: true },
      xAxis: { type: 'value', axisLine: { lineStyle: { color: '#fff' } } },
      yAxis: { type: 'category', data: data.sources?.map((s) => s.name) || [], axisLine: { lineStyle: { color: '#fff' } } },
      series: [{ type: 'bar', data: data.sources?.map((s) => s.value) || [], itemStyle: { color: '#5470c6' } }]
    })
  }

  if (funnelChartRef.value) {
    funnelChart = safeInit(funnelChartRef.value)
    funnelChart.setOption({
      tooltip: { trigger: 'item', formatter: '{a} <br/>{b}: {c}%' },
      series: [{
        name: '漏斗',
        type: 'funnel',
        left: '10%', top: '10%', bottom: '10%', width: '80%',
        data: data.funnel || []
      }]
    })
  }

  if (mapChartRef.value) {
    mapChart = safeInit(mapChartRef.value)
    const regions = Array.isArray(data.regions) ? data.regions : []
    mapChart.setOption({
      tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' } },
      grid: { left: '3%', right: '4%', bottom: '3%', containLabel: true },
      xAxis: { type: 'value', axisLine: { lineStyle: { color: '#fff' } } },
      yAxis: {
        type: 'category',
        data: regions.map((r) => r.name),
        axisLine: { lineStyle: { color: '#fff' } }
      },
      series: [{
        type: 'bar',
        data: regions.map((r) => r.value),
        itemStyle: { color: '#5470c6' }
      }]
    })
  }

  if (conversionChartRef.value) {
    conversionChart = safeInit(conversionChartRef.value)
    conversionChart.setOption({
      tooltip: { trigger: 'axis' },
      legend: { data: ['本周', '上周'], textStyle: { color: '#fff' } },
      xAxis: { type: 'category', data: data.conversion?.dates || [], axisLine: { lineStyle: { color: '#fff' } } },
      yAxis: { type: 'value', axisLine: { lineStyle: { color: '#fff' } } },
      series: [
        { name: '本周', type: 'bar', data: data.conversion?.thisWeek || [], itemStyle: { color: '#5470c6' } },
        { name: '上周', type: 'bar', data: data.conversion?.lastWeek || [], itemStyle: { color: '#91cc75' } }
      ]
    })
  }
}

const toggleFullscreen = () => {
  if (!document.fullscreenElement) {
    document.documentElement.requestFullscreen()
    isFullscreen.value = true
  } else {
    document.exitFullscreen()
    isFullscreen.value = false
  }
}

const handleResize = () => {
  trendChart?.resize()
  channelChart?.resize()
  sourceChart?.resize()
  funnelChart?.resize()
  mapChart?.resize()
  conversionChart?.resize()
}

onMounted(() => {
  updateTime()
  loadData()
  loadRealtime()
  timer = setInterval(() => {
    updateTime()
    loadRealtime()
  }, 5000)
  window.addEventListener('resize', handleResize)
})

onUnmounted(() => {
  clearInterval(timer)
  window.removeEventListener('resize', handleResize)
})
</script>

<style scoped lang="scss">
.dashboard-screen-page {
  padding: 15px;
  background: linear-gradient(180deg, #0a1a2a 0%, #0d2540 100%);
  min-height: 100vh;
  color: #fff;
}
.dashboard-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
  h1 {
    margin: 0;
    background: linear-gradient(90deg, #00f2fe 0%, #4facfe 100%);
    -webkit-background-clip: text;
    -webkit-text-fill-color: transparent;
    font-size: 28px;
  }
  .header-info {
    display: flex;
    align-items: center;
    gap: 15px;
    .time {
      font-size: 18px;
      color: #00f2fe;
    }
  }
}
.kpi-row {
  margin-bottom: 15px;
  .kpi-card {
    padding: 20px;
    border-radius: 8px;
    color: #fff;
    .kpi-label { font-size: 14px; opacity: 0.9; }
    .kpi-value { font-size: 32px; font-weight: bold; margin: 10px 0; }
    .kpi-trend { font-size: 12px; opacity: 0.8; &.up { color: #10B981; } &.down { color: #EF4444; } .trend-label { margin-left: 8px; } }
  }
}
.charts-row {
  margin-bottom: 15px;
  .chart-card {
    background: rgba(255, 255, 255, 0.05);
    border: 1px solid rgba(255, 255, 255, 0.1);
    :deep(.el-card__header) { color: #fff; border-bottom: 1px solid rgba(255, 255, 255, 0.1); }
    :deep(.el-card__body) { padding: 15px; }
  }
  .chart-container {
    height: 300px;
  }
}
.realtime-list {
  height: 300px;
  overflow-y: auto;
  .activity-item {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px;
    border-bottom: 1px solid rgba(255, 255, 255, 0.1);
    .text { flex: 1; }
    .time { color: #909399; font-size: 12px; }
  }
}
</style>
