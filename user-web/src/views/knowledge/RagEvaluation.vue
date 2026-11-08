<template>
  <div class="rag-eval">
    <el-row
      :gutter="16"
      class="stats-row"
    >
      <el-col :span="6">
        <el-card class="stat-card">
          <div class="stat-label">
            Recall@5
          </div>
          <div class="stat-value">
            {{ stats.recall5?.toFixed(3) || '—' }}
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="stat-card">
          <div class="stat-label">
            MRR
          </div>
          <div class="stat-value">
            {{ stats.mrr?.toFixed(3) || '—' }}
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="stat-card">
          <div class="stat-label">
            NDCG@5
          </div>
          <div class="stat-value">
            {{ stats.ndcg5?.toFixed(3) || '—' }}
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="stat-card">
          <div class="stat-label">
            评估集
          </div>
          <div class="stat-value">
            {{ stats.eval_set_size || 0 }}
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-card>
      <template #header>
        <div class="card-header">
          <div class="header-title">
            <span>评估集管理</span>
            <span class="header-hint">CSV/JSONL 需包含 question,answer 两列；表格仅显示最近 20 次评测</span>
          </div>
          <div>
            <el-upload
              :auto-upload="false"
              :on-change="onUpload"
              accept=".csv,.jsonl"
            >
              <el-button>上传评估集（CSV/JSONL）</el-button>
            </el-upload>
            <el-button
              type="primary"
              @click="runEval"
            >
              运行评估
            </el-button>
          </div>
        </div>
      </template>
      <el-table
        v-loading="loading"
        :data="recentRuns"
      >
        <el-table-column
          prop="id"
          label="Run ID"
          width="100"
        />
        <!-- 后端 RagEvalRun 没有 strategy 字段，原来这列永远空白，改为状态 -->
        <el-table-column
          label="状态"
          width="100"
        >
          <template #default="{ row }">
            <el-tag
              :type="statusTag(row.status)"
              size="small"
            >
              {{ statusText(row.status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="命中/总数"
          width="110"
        >
          <template #default="{ row }">
            {{ row.total > 0 ? `${row.hit}/${row.total}` : '-' }}
          </template>
        </el-table-column>
        <el-table-column
          prop="recall5"
          label="Recall@5"
          width="100"
        />
        <el-table-column
          prop="mrr"
          label="MRR"
          width="100"
        />
        <el-table-column
          prop="ndcg5"
          label="NDCG@5"
          width="100"
        />
        <el-table-column
          prop="created_at"
          label="时间"
          width="180">
          <template #default="{ row }">
            {{ fmtTime(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="120"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button
              link
              type="primary"
              @click="compareWith(row)"
            >
              对比当前
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-card class="chart-card">
      <template #header>
        <span>指标趋势</span>
      </template>
      <div
        ref="chartRef"
        class="chart"
      />
    </el-card>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, onBeforeUnmount } from 'vue';
import { ElMessage, ElMessageBox } from 'element-plus'
import * as echarts from 'echarts'
import { safeInit } from '@/utils/echarts'
import { http } from '@/utils/request'
import { fmtTime } from '@/utils/time'
import { isUserReportedError } from '@/utils/errorReporting'

const stats = reactive({ recall5: 0, mrr: 0, ndcg5: 0, eval_set_size: 0 })
const recentRuns = ref([])
const loading = ref(false)
const chartRef = ref()

const STATUS_TEXT = { running: '进行中', completed: '完成', failed: '失败' }
const STATUS_TAG = { running: 'warning', completed: 'success', failed: 'danger' }
const statusText = (s) => STATUS_TEXT[s] || s || '-'
const statusTag = (s) => STATUS_TAG[s] || 'info'

async function load() {
  loading.value = true
  try {
    const [s, runs] = await Promise.all([
      http.get('/api/rag/eval/latest'),
      http.get('/api/rag/eval/runs', { limit: 20 })
    ])
    // 后端字段是 snake_case（eval_set_size），原来写 evalSetSize 导致「评估集」恒为 0
    Object.assign(stats, {
      recall5: s?.recall5 || 0,
      mrr: s?.mrr || 0,
      ndcg5: s?.ndcg5 || 0,
      eval_set_size: s?.eval_set_size || 0
    })
    recentRuns.value = Array.isArray(runs) ? runs : []
    renderChart(recentRuns.value)
    syncPolling()
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error('加载评测数据失败: ' + (e?.message || e))
  } finally {
    loading.value = false
  }
}

// 有「进行中」的评测时自动轮询，否则用户跑完 3-5 分钟还得手动刷浏览器
let pollTimer = null
function syncPolling() {
  const running = recentRuns.value.some((r) => r.status === 'running')
  if (running && !pollTimer) {
    pollTimer = setInterval(() => {
      if (document.visibilityState === 'visible') load()
    }, 15000)
  } else if (!running && pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}
onBeforeUnmount(() => {
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
})

function renderChart(runs) {
  if (!chartRef.value) return
  const chart = safeInit(chartRef.value)
  // 接口按时间倒序返回，横轴改成正序，趋势线才是从左到右
  const ordered = [...runs].reverse()
  chart.setOption({
    tooltip: { trigger: 'axis' },
    legend: { data: ['Recall@5', 'MRR', 'NDCG@5'] },
    xAxis: { type: 'category', data: ordered.map((r) => r.id) },
    yAxis: { type: 'value', min: 0, max: 1 },
    series: [
      { name: 'Recall@5', type: 'line', data: ordered.map((r) => r.recall5) },
      { name: 'MRR', type: 'line', data: ordered.map((r) => r.mrr) },
      { name: 'NDCG@5', type: 'line', data: ordered.map((r) => r.ndcg5) }
    ]
  })
}

async function onUpload(file) {
  const form = new FormData()
  form.append('file', file.raw)
  try {
    const res = await http.post('/api/rag/eval/upload', form, { headers: { 'Content-Type': 'multipart/form-data' } })
    ElMessage.success(`评估集已上传，导入 ${res?.imported ?? 0} 条`)
    await load()
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error('上传评估集失败: ' + (e?.message || e))
  }
}

async function runEval() {
  // 没有评估集时后端只会写一条失败记录，先本地拦住，避免用户白等 3-5 分钟
  if (!stats.eval_set_size) {
    ElMessage.warning('请先上传评估集（CSV/JSONL，含 question,answer 两列）后再运行评估')
    return
  }
  loading.value = true
  try {
    await http.post('/api/rag/eval/run', {})
    ElMessage.success('评估已启动，3-5 分钟完成')
    // 立刻刷新一次让「进行中」行可见（原来只等 5s 后刷新，用户点完看不到任何变化的行）
    await load()
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error('启动评估失败: ' + (e?.message || e))
  } finally {
    loading.value = false
  }
}

async function compareWith(row) {
  try {
    const res = await http.get(`/api/rag/eval/diff?baseline=${row.id}`)
    const fmt = (v) => `${v > 0 ? '+' : ''}${(Number(v || 0) * 100).toFixed(1)}%`
    await ElMessageBox.alert(
      `基线 Run #${res.baseline_id} → 当前 Run #${res.latest_id}\n` +
        `Recall@5 ${fmt(res.recall5Delta)} MRR ${fmt(res.mrrDelta)} NDCG@5 ${fmt(res.ndcg5Delta)}`,
      '与最新评测对比',
      { confirmButtonText: '知道了', customStyle: { whiteSpace: 'pre-line' } }
    )
  } catch (e) {
    if (e === 'cancel' || e === 'close') return
    if (!isUserReportedError(e)) ElMessage.error('对比失败: ' + (e?.message || e))
  }
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
.header-title { display: flex; flex-direction: column; gap: 4px; }
.header-hint { font-size: 12px; color: #909399; }
.chart { width: 100%; height: 320px; }
.chart-card { margin-top: 16px; }
</style>
