<template>
  <div class="rag-eval">
    <el-row :gutter="16" class="stats-row">
      <el-col :span="6">
        <el-card class="stat-card">
          <div class="stat-label">Recall@5</div>
          <div class="stat-value">{{ stats.recall5?.toFixed(3) || '—' }}</div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="stat-card">
          <div class="stat-label">MRR</div>
          <div class="stat-value">{{ stats.mrr?.toFixed(3) || '—' }}</div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="stat-card">
          <div class="stat-label">NDCG@5</div>
          <div class="stat-value">{{ stats.ndcg5?.toFixed(3) || '—' }}</div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="stat-card">
          <div class="stat-label">评估集</div>
          <div class="stat-value">{{ stats.evalSetSize || 0 }}</div>
        </el-card>
      </el-col>
    </el-row>

    <el-card>
      <template #header>
        <div class="card-header">
          <span>评估集管理</span>
          <div>
            <el-upload :auto-upload="false" :on-change="onUpload" accept=".csv,.jsonl">
              <el-button>上传评估集（CSV/JSONL）</el-button>
            </el-upload>
            <el-button type="primary" @click="runEval">运行评估</el-button>
          </div>
        </div>
      </template>
      <el-table :data="recentRuns" v-loading="loading">
        <el-table-column prop="id" label="Run ID" width="120" />
        <el-table-column prop="strategy" label="策略" width="160" />
        <el-table-column prop="recall5" label="Recall@5" width="100" />
        <el-table-column prop="mrr" label="MRR" width="100" />
        <el-table-column prop="ndcg5" label="NDCG@5" width="100" />
        <el-table-column prop="createdAt" label="时间" width="180" />
        <el-table-column label="操作" width="120" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="compareWith(row)">对比当前</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-card class="chart-card">
      <template #header><span>指标趋势</span></template>
      <div ref="chartRef" class="chart" />
    </el-card>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue';
import { ElMessage } from 'element-plus'
import * as echarts from 'echarts'
import { safeInit } from '@/utils/echarts'
import { http } from '@/utils/request'

const stats = reactive({ recall5: 0, mrr: 0, ndcg5: 0, evalSetSize: 0 })
const recentRuns = ref([])
const loading = ref(false)
const chartRef = ref()

async function load() {
  loading.value = true
  try {
    const [s, runs] = await Promise.all([
      http.get('/api/rag/eval/latest'),
      http.get('/api/rag/eval/runs', { limit: 20 })
    ])
    Object.assign(stats, s || {})
    recentRuns.value = Array.isArray(runs) ? runs : []
    renderChart(recentRuns.value)
  } finally {
    loading.value = false
  }
}

function renderChart(runs) {
  if (!chartRef.value) return
  const chart = safeInit(chartRef.value)
  chart.setOption({
    tooltip: { trigger: 'axis' },
    legend: { data: ['Recall@5', 'MRR', 'NDCG@5'] },
    xAxis: { type: 'category', data: runs.map((r) => r.id) },
    yAxis: { type: 'value', min: 0, max: 1 },
    series: [
      { name: 'Recall@5', type: 'line', data: runs.map((r) => r.recall5) },
      { name: 'MRR', type: 'line', data: runs.map((r) => r.mrr) },
      { name: 'NDCG@5', type: 'line', data: runs.map((r) => r.ndcg5) }
    ]
  })
}

async function onUpload(file) {
  const form = new FormData()
  form.append('file', file.raw)
  await http.post('/api/rag/eval/upload', form, { headers: { 'Content-Type': 'multipart/form-data' } })
  ElMessage.success('评估集已上传')
  await load()
}

async function runEval() {
  loading.value = true
  try {
    await http.post('/api/rag/eval/run', {})
    ElMessage.success('评估已启动，3-5 分钟完成')
    setTimeout(load, 5000)
  } finally {
    loading.value = false
  }
}

async function compareWith(row) {
  const res = await http.get(`/api/rag/eval/diff?baseline=${row.id}`)
  ElMessage.info(`相比基线：Recall ${res.recall5Delta > 0 ? '+' : ''}${(res.recall5Delta * 100).toFixed(1)}%`)
}

onMounted(load)
</script>

<style scoped>
.rag-eval { padding: 16px; }
.stats-row { margin-bottom: 16px; }
.stat-card { padding: 8px; text-align: center; }
.stat-label { color: #64748B; font-size: 12px; }
.stat-value { font-size: 28px; font-weight: 700; margin: 8px 0; }
.card-header { display: flex; justify-content: space-between; align-items: center; }
.chart { width: 100%; height: 320px; }
.chart-card { margin-top: 16px; }
</style>
