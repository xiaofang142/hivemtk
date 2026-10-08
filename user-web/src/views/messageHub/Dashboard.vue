<template>
  <div class="message-hub-page">
    <el-alert v-if="loadError" class="load-error" type="warning" :closable="false" show-icon>
      {{ loadError }}
    </el-alert>

    <el-row :gutter="16" class="stats-row">
      <el-col :span="6">
        <el-card class="stat-card">
          <div class="stat-label">总吞吐</div>
          <div class="stat-value">{{ stats.total }}</div>
          <div class="stat-sub">{{ rangeLabel }}</div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="stat-card success">
          <div class="stat-label">发送</div>
          <div class="stat-value">{{ stats.outbound }}</div>
          <div class="stat-sub">outbound 方向（同窗口）</div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="stat-card warning">
          <div class="stat-label">接收</div>
          <div class="stat-value">{{ stats.inbound }}</div>
          <div class="stat-sub">inbound 方向（同窗口）</div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="stat-card danger">
          <div class="stat-label">未读</div>
          <div class="stat-value">{{ stats.unread }}</div>
          <div class="stat-sub">同窗口内未读，非全量积压</div>
        </el-card>
      </el-col>
    </el-row>

    <el-row :gutter="16">
      <el-col :span="16">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>渠道吞吐分布（{{ rangeLabel }}，每 {{ refreshSeconds }} 秒刷新）</span>
              <span class="card-header-extra">近 24 小时累计 {{ stats.recent_24h }} 条</span>
            </div>
          </template>
          <div v-show="hasPlatformRows" ref="chartRef" class="chart" />
          <el-empty v-if="!hasPlatformRows" :description="emptyChartHint" :image-size="60" />
        </el-card>
      </el-col>
      <el-col :span="8">
        <el-card>
          <template #header>
            <span>方向与消息类型（{{ rangeLabel }}）</span>
          </template>
          <el-table :data="distributionRows" class="dist-table" size="small">
            <el-table-column prop="dim" label="维度" width="90" />
            <el-table-column prop="label" label="取值" />
            <el-table-column prop="count" label="条数" width="80" align="right" />
          </el-table>
        </el-card>
      </el-col>
    </el-row>

    <el-card class="dlq-card">
      <template #header>
        <div class="card-header">
          <span>死信队列 (DLQ)｜待重试 {{ dlqTotal }} 条</span>
          <el-button
            type="primary"
            size="small"
            :disabled="dlqTotal === 0"
            :loading="retrying"
            @click="batchRetry"
          >
            批量重试
          </el-button>
        </div>
      </template>
      <el-table :data="dlqList" v-loading="loading" size="small">
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column label="渠道" width="120">
          <template #default="{ row }">{{ getChannelLabel(row.platform) }}</template>
        </el-table-column>
        <el-table-column label="方向" width="100">
          <template #default="{ row }">{{ directionLabel(row.direction) }}</template>
        </el-table-column>
        <el-table-column prop="retries" label="重试次数" width="100" />
        <el-table-column prop="error" label="错误" min-width="180" show-overflow-tooltip />
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
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { ElMessage } from 'element-plus'
import { safeInit, safeDispose } from '@/utils/echarts'
import { http } from '@/utils/request'
import { getChannelLabel } from '@/constants/channel'

// 窗口与刷新都只有一处事实源：卡片副标题、图表标题、请求参数三处都从这里长出来，
// 免得"最近 1 小时"这句文案和实际发出去的时间界各说各话。
const WINDOW_HOURS = 1
const REFRESH_MS = 30000

const stats = ref({
  total: 0,
  inbound: 0,
  outbound: 0,
  unread: 0,
  by_platform: {},
  by_direction: {},
  by_msg_type: {},
  recent_24h: 0
})
const dlqList = ref([])
const dlqTotal = ref(0)
const loading = ref(false)
const retrying = ref(false)
const loadError = ref('')
const chartRef = ref()
let timer = null
let chart = null

const rangeLabel = `最近 ${WINDOW_HOURS} 小时`
const refreshSeconds = REFRESH_MS / 1000
const emptyChartHint = '该窗口内没有消息，画不出渠道分布；窗口外仍有积压时看上面的「近 24 小时累计」'

const DIRECTION_LABELS = {
  inbound: '接收',
  outbound: '发送'
}
const MSG_TYPE_LABELS = {
  text: '文本',
  image: '图片',
  file: '文件',
  audio: '语音',
  video: '视频',
  link: '链接',
  card: '卡片',
  location: '位置',
  event: '会话事件'
}

const directionLabel = (value) => DIRECTION_LABELS[value] || value

// 计数映射 → 按数量降序的行。后端返回的是 map，顺序不稳定，不排序会让同一份数据每次刷新换个位置；
// 值域外的方向/类型码原样露出（不猜译名）：这一页是运维看数的地方，
// "sticker 显示成 sticker" 才能让人发现词表落后于数据，替它编一个中文名就是把问题藏起来。
function toRows(map) {
  return Object.entries(map || {})
    .map(([key, count]) => ({ key, count: Number(count) || 0 }))
    .sort((a, b) => b.count - a.count || a.key.localeCompare(b.key))
}

const platformRows = computed(() => toRows(stats.value.by_platform))
const hasPlatformRows = computed(() => platformRows.value.length > 0)

const distributionRows = computed(() => [
  ...toRows(stats.value.by_direction).map((r) => ({
    dim: '方向',
    key: r.key,
    label: directionLabel(r.key),
    count: r.count
  })),
  ...toRows(stats.value.by_msg_type).map((r) => ({
    dim: '消息类型',
    key: r.key,
    label: MSG_TYPE_LABELS[r.key] || r.key,
    count: r.count
  }))
])

function renderChart() {
  if (!hasPlatformRows.value || !chartRef.value) return
  chart = safeInit(chartRef.value)
  if (!chart) return
  const rows = platformRows.value
  // notMerge=true：渠道少一档时上一轮的柱子必须跟着消失，合并配置会把它留在图上。
  chart.setOption(
    {
      tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' } },
      grid: { left: 48, right: 24, top: 32, bottom: 56 },
      xAxis: {
        type: 'category',
        data: rows.map((r) => getChannelLabel(r.key)),
        axisLabel: { interval: 0, rotate: rows.length > 6 ? 30 : 0 }
      },
      yAxis: { type: 'value', name: '条数', minInterval: 1 },
      series: [
        {
          name: '消息条数',
          type: 'bar',
          barMaxWidth: 36,
          data: rows.map((r) => r.count),
          label: { show: true, position: 'top' }
        }
      ]
    },
    true
  )
}

async function load() {
  loading.value = true
  try {
    const end = new Date()
    const start = new Date(end.getTime() - WINDOW_HOURS * 3600 * 1000)
    // stats 端点只认 start_time / end_time（RFC3339）：传 window=1h 会被静默忽略，
    // 页面就会把全量读数当成"最近 1 小时"报给客户。
    const [s, dlq] = await Promise.allSettled([
      http.get('/api/message-hub/stats', { start_time: start.toISOString(), end_time: end.toISOString() }),
      http.get('/api/message-hub/dlq', { limit: 50 })
    ])
    const failures = []
    if (s.status === 'fulfilled' && s.value) {
      stats.value = { ...stats.value, ...s.value }
    } else {
      failures.push('统计')
    }
    if (dlq.status === 'fulfilled') {
      dlqList.value = Array.isArray(dlq.value?.list) ? dlq.value.list : []
      dlqTotal.value = Number(dlq.value?.total) || 0
    } else {
      failures.push('死信队列')
    }
    // 读不到就把"读到的是上一次的数"说在明面上：静默保留旧值看起来就像当前读数。
    loadError.value = failures.length
      ? `${failures.join('、')}本次没读到（${failReason(s, dlq)}），下面的数字是上一次成功读到的`
      : ''
  } finally {
    loading.value = false
  }
  renderChart()
}

function failReason(...settled) {
  const rejected = settled.find((r) => r && r.status === 'rejected')
  return rejected?.reason?.message || '请求失败'
}

async function batchRetry() {
  retrying.value = true
  try {
    const res = await http.post('/api/message-hub/dlq/batch-retry', {})
    ElMessage.success(`已重新入队 ${Number(res?.requeued) || 0} 条`)
  } finally {
    retrying.value = false
    await load()
  }
}

async function retryOne(row) {
  await http.post(`/api/message-hub/dlq/${row.id}/retry`, {})
  await load()
}

async function dropOne(row) {
  await http.delete(`/api/message-hub/dlq/${row.id}`)
  await load()
}

onMounted(() => {
  load()
  timer = setInterval(load, REFRESH_MS)
})

onUnmounted(() => {
  if (timer) clearInterval(timer)
  safeDispose(chart)
  chart = null
})
</script>

<style scoped>
.message-hub-page { padding: 16px; }
.load-error { margin-bottom: 12px; }
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
.card-header-extra { color: #94A3B8; font-size: 12px; }
</style>
