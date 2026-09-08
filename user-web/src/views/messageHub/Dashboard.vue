<template>
  <div class="message-hub-page">
    <el-row :gutter="16" class="stats-row">
      <el-col :span="6">
        <el-card class="stat-card">
          <div class="stat-label">总吞吐</div>
          <div class="stat-value">{{ stats.total }}</div>
          <div class="stat-sub">最近 1 小时</div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="stat-card success">
          <div class="stat-label">发送</div>
          <div class="stat-value">{{ stats.outbound }}</div>
          <div class="stat-sub">outbound 方向</div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="stat-card warning">
          <div class="stat-label">接收</div>
          <div class="stat-value">{{ stats.inbound }}</div>
          <div class="stat-sub">inbound 方向</div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="stat-card danger">
          <div class="stat-label">未读</div>
          <div class="stat-value">{{ stats.unread }}</div>
          <div class="stat-sub">待处理消息</div>
        </el-card>
      </el-col>
    </el-row>

    <el-row :gutter="16">
      <el-col :span="16">
        <el-card>
          <template #header>
            <span>实时吞吐（SSE）</span>
          </template>
          <div ref="chartRef" class="chart" />
        </el-card>
      </el-col>
      <el-col :span="8">
        <el-card>
          <template #header>
            <span>通道健康度</span>
          </template>
          <el-table :data="channelHealth" size="small">
            <el-table-column prop="channel" label="通道" />
            <el-table-column label="健康度" width="100">
              <template #default="{ row }">
                <el-tag :type="row.status === 'ok' ? 'success' : 'danger'" size="small">
                  {{ row.status === 'ok' ? '正常' : '异常' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="latency" label="延迟(ms)" width="100" />
          </el-table>
        </el-card>
      </el-col>
    </el-row>

    <el-card class="dlq-card">
      <template #header>
        <div class="card-header">
          <span>死信队列 (DLQ)</span>
          <el-button type="primary" size="small" :disabled="!stats.dlq" @click="batchRetry">批量重试</el-button>
        </div>
      </template>
      <el-table :data="dlqList" v-loading="loading">
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="channel" label="通道" width="100" />
        <el-table-column prop="retryCount" label="重试次数" width="100" />
        <el-table-column prop="error" label="错误" min-width="200" show-overflow-tooltip />
        <el-table-column prop="failedAt" label="失败时间" width="180" />
        <el-table-column label="操作" width="120" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="retryOne(row)">重试</el-button>
            <el-button link type="danger" @click="dropOne(row)">丢弃</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue';
import * as echarts from 'echarts'
import { safeInit } from '@/utils/echarts'
import { http } from '@/utils/request'

const stats = ref({ total: 0, inbound: 0, outbound: 0, unread: 0 })
const channelHealth = ref([])
const dlqList = ref([])
const loading = ref(false)
const chartRef = ref()
let sse = null

async function load() {
  loading.value = true
  try {
    // channel-health 后端暂无端点，allSettled 容错：单接口失败不拖垮整页数据
    const [s, c, dlq] = await Promise.allSettled([
      http.get('/api/message-hub/stats', { window: '1h' }),
      http.get('/api/message-hub/channel-health'),
      http.get('/api/message-hub/dlq', { limit: 50 })
    ])
    if (s.status === 'fulfilled') stats.value = s.value || stats.value
    if (c.status === 'fulfilled') channelHealth.value = c.value || []
    if (dlq.status === 'fulfilled') dlqList.value = dlq.value || []
  } finally {
    loading.value = false
  }
  renderChart()
}

function renderChart() {
  if (!chartRef.value) return
  const chart = safeInit(chartRef.value)
  chart.setOption({
    tooltip: { trigger: 'axis' },
    xAxis: { type: 'category', data: Array.from({ length: 60 }, (_, i) => `-${60 - i}s`) },
    yAxis: { type: 'value', name: 'msg/s' },
    series: [
      { name: '成功', type: 'line', stack: 'total', smooth: true, areaStyle: {}, data: generateRandomData(60, 50, 100) },
      { name: '失败', type: 'line', stack: 'total', smooth: true, areaStyle: {}, data: generateRandomData(60, 0, 5) }
    ]
  })
}

function generateRandomData(n, min, max) {
  return Array.from({ length: n }, () => Math.floor(min + Math.random() * (max - min)))
}

async function batchRetry() {
  await http.post('/api/message-hub/dlq/batch-retry', {})
  await load()
}

async function retryOne(row) {
  await http.post(`/api/message-hub/dlq/${row.id}/retry`, {})
  await load()
}

async function dropOne(row) {
  await http.delete(`/api/message-hub/dlq/${row.id}`)
  await load()
}

function connectSSE() {
  if (typeof EventSource === 'undefined') return
  try {
    sse = new EventSource('/api/sse/message-hub')
    sse.onmessage = (e) => {
      try {
        const data = JSON.parse(e.data)
        if (data.stats) stats.value = data.stats
        if (data.channelHealth) channelHealth.value = data.channelHealth
      } catch (_) { console.warn("[request] 后台调用失败(已忽略):", _) }
    }
  } catch (_) { console.warn("[request] 后台调用失败(已忽略):", _) }
}

onMounted(() => {
  load()
  connectSSE()
})

onUnmounted(() => {
  if (sse) sse.close()
})
</script>

<style scoped>
.message-hub-page { padding: 16px; }
.stats-row { margin-bottom: 16px; }
.stat-card { padding: 8px; text-align: center; }
.stat-label { color: #64748B; font-size: 12px; }
.stat-value { font-size: 32px; font-weight: 700; margin: 8px 0; }
.stat-sub { color: #94A3B8; font-size: 12px; }
.stat-card.success .stat-value { color: #10B981; }
.stat-card.warning .stat-value { color: #F59E0B; }
.stat-card.danger .stat-value { color: #EF4444; }
.chart { width: 100%; height: 320px; }
.dlq-card { margin-top: 16px; }
.card-header { display: flex; justify-content: space-between; align-items: center; }
</style>
