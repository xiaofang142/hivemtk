<template>
  <div class="csat-page">
    <el-alert
      v-if="error"
      class="load-error"
      type="error"
      :closable="false"
      show-icon
      :title="error"
    />
    <el-row :gutter="16" class="stats-row">
      <el-col :span="6">
        <el-card shadow="hover" class="stat-card">
          <div class="stat-label">CSAT 均分</div>
          <div class="stat-value">{{ stats.responded > 0 ? stats.avgScore : '—' }}</div>
          <div class="stat-sub">满分 5 星</div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="hover" class="stat-card">
          <div class="stat-label">好评率</div>
          <div class="stat-value">{{ stats.responded > 0 ? stats.positiveRate + '%' : '—' }}</div>
          <div class="stat-sub">4-5 星占比</div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="hover" class="stat-card">
          <div class="stat-label">总评分数</div>
          <div class="stat-value">{{ stats.responded }}</div>
          <div class="stat-sub">{{ stats.windowLabel }}</div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="hover" class="stat-card">
          <div class="stat-label">差评数</div>
          <div class="stat-value" style="color: #EF4444">{{ stats.negativeCount }}</div>
          <div class="stat-sub">≤{{ stats.threshold }} 星</div>
        </el-card>
      </el-col>
    </el-row>

    <el-row :gutter="16" class="chart-row">
      <el-col :span="16">
        <el-card>
          <template #header>
            <span>CSAT 趋势（最近 30 天）</span>
          </template>
          <div ref="trendChartRef" class="chart" />
        </el-card>
      </el-col>
      <el-col :span="8">
        <el-card>
          <template #header>
            <span>评分分布</span>
          </template>
          <div ref="distChartRef" class="chart" />
        </el-card>
      </el-col>
    </el-row>

    <el-card class="negative-card">
      <template #header>
        <span>差评列表（≤{{ stats.threshold }} 星）</span>
      </template>
      <el-table :data="negativeList" v-loading="loading">
        <el-table-column prop="session_id" label="会话 ID" width="240" show-overflow-tooltip />
        <el-table-column prop="agent_name" label="坐席" width="120">
          <template #default="{ row }">{{ row.agent_name || '—' }}</template>
        </el-table-column>
        <el-table-column prop="user_name" label="客户" width="120">
          <template #default="{ row }">{{ row.user_name || '—' }}</template>
        </el-table-column>
        <el-table-column prop="score" label="评分" width="80">
          <template #default="{ row }">
            <el-rate v-model="row.score" disabled :max="5" show-score />
          </template>
        </el-table-column>
        <el-table-column prop="comment" label="评论" min-width="200" show-overflow-tooltip />
        <el-table-column label="提交时间" width="180">
          <template #default="{ row }">{{ formatTime(row.responded_at) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="120" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="viewSession(row)">查看会话</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue';
import { getCSATStats, getCSATTrend, getNegativeCSAT } from '@/api/csat'
import { safeInit } from '@/utils/echarts'

// /api/csat/* 的信封：stats 是对象，trend 与 negative 各自包一层 {list, total}，字段一律 snake_case。
// 早先按"接口直接返回数组 / 字段是驼峰"取用，结果 el-table 收到对象（rows is not iterable）、
// 趋势图 data.map 抛错、四张统计卡恒空。
const stats = reactive({
  avgScore: 0,
  positiveRate: 0,
  responded: 0,
  negativeCount: 0,
  threshold: 3,
  windowLabel: '全部'
})
const negativeList = ref([])
const loading = ref(false)
const error = ref('')
const trendChartRef = ref()
const distChartRef = ref()

const formatTime = (val) => {
  if (!val) return '-'
  const d = new Date(val)
  if (isNaN(d.getTime())) return '-'
  return d.toLocaleString('zh-CN', { hour12: false })
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [s, t, neg] = await Promise.all([
      getCSATStats({ window: 'month' }),
      getCSATTrend({ days: 30 }),
      getNegativeCSAT({ limit: 50 })
    ])
    Object.assign(stats, {
      avgScore: Number(s?.avg_score ?? 0),
      positiveRate: Number(s?.positive_rate ?? 0),
      responded: Number(s?.responded ?? 0),
      negativeCount: Number(s?.negative_count ?? 0),
      // 阈值只有一个事实源：差评列表返回的 threshold（模板 low_threshold）
      threshold: Number(neg?.threshold ?? s?.threshold ?? 3),
      windowLabel: s?.window || '全部'
    })
    negativeList.value = Array.isArray(neg?.list) ? neg.list : []
    renderTrend(Array.isArray(t?.list) ? t.list : [])
    renderDist(Array.isArray(s?.distribution) ? s.distribution : [])
  } catch (e) {
    error.value = 'CSAT 数据加载失败：' + (e?.message || e)
  } finally {
    loading.value = false
  }
}

function renderTrend(data) {
  if (!trendChartRef.value) return
  const chart = safeInit(trendChartRef.value)
  chart.setOption({
    tooltip: { trigger: 'axis' },
    xAxis: { type: 'category', data: data.map((d) => d.date) },
    yAxis: [{ type: 'value', max: 5 }],
    series: [{
      name: 'CSAT',
      type: 'line',
      data: data.map((d) => Number(d.avg_score ?? 0)),
      smooth: true,
      areaStyle: { opacity: 0.3 }
    }]
  })
}

function renderDist(data) {
  if (!distChartRef.value) return
  const chart = safeInit(distChartRef.value)
  chart.setOption({
    tooltip: { trigger: 'item' },
    series: [{
      type: 'pie',
      radius: ['40%', '70%'],
      data: data.map((d) => ({ name: `${d.score}星`, value: d.count }))
    }]
  })
}

function viewSession(row) {
  // 路由是 hash 模式：不带 # 的 /customerSession/list 会打到 SPA 兜底页而不是这一页的列表
  // （同仓其它深链一律写成 /#/…，见 userSegment/RfmMatrix.vue）
  const sid = row?.session_id || ''
  window.open(`/#/customerSession/list?session_id=${encodeURIComponent(sid)}`, '_blank');
}

onMounted(load)
</script>

<style scoped>
.csat-page { padding: 16px; }
.load-error { margin-bottom: 16px; }
.stat-card { padding: 8px; }
.stat-card .stat-label { color: #64748B; font-size: 12px; }
.stat-card .stat-value { font-size: 28px; font-weight: 700; color: #0F172A; margin: 8px 0; }
.stat-card .stat-sub { color: #94A3B8; font-size: 12px; }
.stats-row { margin-bottom: 16px; }
.chart-row { margin-bottom: 16px; }
.chart { width: 100%; height: 280px; }
.negative-card { margin-top: 16px; }
</style>
