<template>
  <div class="autopilot">
    <!-- 傻瓜式入口：一个输入框 -->
    <el-card shadow="hover" class="hero-card">
      <div class="hero-title">
        <h2>GEO 自动驾驶</h2>
        <p>输入一个关键词，系统自动完成：关键词扩展 → 下拉词抓取 → 长尾展开 → 关键词落库 → 文章生成（可选：蜘蛛推送、站点审计）。文章只生成不发布。</p>
      </div>
      <div class="launch-row">
        <el-input
          v-model="keyword"
          size="large"
          placeholder="输入一个关键词，例如：客服系统"
          class="keyword-input"
          maxlength="100"
          :disabled="running"
          @keyup.enter="launch"
        />
        <el-button
          type="primary"
          size="large"
          :loading="running"
          :disabled="!keyword.trim()"
          @click="launch"
        >
          {{ running ? '执行中…' : '一键启动' }}
        </el-button>
      </div>
      <div class="options-row">
        <el-input
          v-model="siteUrl"
          placeholder="站点 URL（可选，填写后追加蜘蛛推送 + 站点审计）"
          class="site-input"
          :disabled="running"
        />
        <el-input-number
          v-model="articleCount"
          :min="1"
          :max="5"
          label="生成文章数"
          :disabled="running"
        />
        <span class="option-hint">生成文章数</span>
      </div>
    </el-card>

    <!-- 步骤时间线 -->
    <el-card v-if="run" shadow="hover" class="steps-card">
      <template #header>
        <div class="steps-header">
          <span>运行详情</span>
          <div class="steps-meta">
            <el-tag :type="statusTagType(run.status)">{{ statusText(run.status) }}</el-tag>
            <el-progress
              type="circle"
              :percentage="run.progress"
              :width="48"
              :status="run.status === 'failed' ? 'exception' : run.status === 'success' ? 'success' : undefined"
            />
          </div>
        </div>
      </template>
      <el-alert
        v-if="run.error"
        :title="run.error"
        type="error"
        show-icon
        :closable="false"
        class="error-alert"
      />
      <el-timeline>
        <el-timeline-item
          v-for="step in steps"
          :key="step.name"
          :type="stepType(step.status)"
          :hollow="step.status === 'running'"
          :timestamp="step.duration_ms ? `${(step.duration_ms / 1000).toFixed(1)}s` : ''"
        >
          <div class="step-row">
            <span class="step-label">{{ step.label }}</span>
            <el-tag size="small" :type="statusTagType(step.status)">{{ statusText(step.status) }}</el-tag>
          </div>
          <div v-if="step.detail" class="step-detail">{{ step.detail }}</div>
        </el-timeline-item>
      </el-timeline>
      <el-result
        v-if="isTerminal && run.summary"
        icon="success"
        :title="statusText(run.status)"
        :sub-title="run.summary"
      >
        <template #extra>
          <el-button @click="goKeywords">查看关键词</el-button>
          <el-button @click="goArticles">查看文章</el-button>
        </template>
      </el-result>
    </el-card>

    <!-- 历史记录 -->
    <el-card shadow="hover" class="history-card">
      <template #header>
        <span>运行历史</span>
      </template>
      <el-table :data="history" size="small" empty-text="暂无运行记录">
        <el-table-column prop="keyword" label="关键词" min-width="140" />
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag size="small" :type="statusTagType(row.status)">{{ statusText(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="progress" label="进度" width="70">
          <template #default="{ row }">{{ row.progress }}%</template>
        </el-table-column>
        <el-table-column prop="summary" label="摘要" min-width="240" show-overflow-tooltip />
        <el-table-column label="启动时间" width="170">
          <template #default="{ row }">{{ formatTime(row.started_at) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="80">
          <template #default="{ row }">
            <el-button link type="primary" @click="viewRun(row.id)">详情</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { geoAutopilotApi } from '@/api/geoAutopilot'
import { ElMessage } from 'element-plus'

const router = useRouter()
const keyword = ref('')
const siteUrl = ref('')
const articleCount = ref(3)
const run = ref(null)
const history = ref([])
const running = computed(() => !!run.value && run.value.status === 'running')
const isTerminal = computed(() => !!run.value && ['success', 'partial', 'failed'].includes(run.value.status))
const steps = computed(() => {
  if (!run.value || !run.value.steps) return []
  try { return JSON.parse(run.value.steps) } catch (e) { return [] }
})
let pollTimer = null

async function launch() {
  const kw = keyword.value.trim()
  if (!kw) return
  try {
    const res = await geoAutopilotApi.createRun({
      keyword: kw,
      site_url: siteUrl.value.trim(),
      article_count: articleCount.value
    })
    run.value = res.data || res
    ElMessage.success('已启动，自动执行中')
    startPolling()
    loadHistory()
  } catch (e) {
    const msg = (e && e.response && e.response.data && e.response.data.message) || '启动失败'
    ElMessage.error(msg)
  }
}

function startPolling() {
  stopPolling()
  pollTimer = setInterval(async () => {
    if (!run.value || !run.value.id) return
    try {
      const res = await geoAutopilotApi.getRun(run.value.id)
      run.value = res.data || res
      if (isTerminal.value) stopPolling()
    } catch (e) { /* 轮询失败不打断，下一轮重试 */ }
  }, 2000)
}

function stopPolling() {
  if (pollTimer) { clearInterval(pollTimer); pollTimer = null }
}

async function loadHistory() {
  try {
    const res = await geoAutopilotApi.listRuns({ page: 1, limit: 10 })
    const data = res.data || res
    history.value = data.list || data || []
  } catch (e) { /* 历史加载失败不打断 */ }
}

function viewRun(id) {
  geoAutopilotApi.getRun(id).then(res => {
    run.value = res.data || res
    if (running.value) startPolling()
  })
}

function statusTagType(s) {
  if (s === 'success') return 'success'
  if (s === 'failed') return 'danger'
  if (s === 'partial') return 'warning'
  if (s === 'skipped') return 'info'
  return 'primary'
}

function stepType(s) {
  if (s === 'success') return 'success'
  if (s === 'failed') return 'danger'
  if (s === 'skipped') return 'info'
  return 'primary'
}

function statusText(s) {
  return { running: '进行中', success: '已完成', failed: '失败', partial: '部分完成', skipped: '已跳过' }[s] || s
}

function formatTime(t) {
  if (!t) return ''
  return String(t).replace('T', ' ').slice(0, 19)
}

function goKeywords() { router.push('/geo-tools/keyword-mining') }
function goArticles() { router.push('/geo-tools/content-creation') }

onMounted(loadHistory)
onUnmounted(stopPolling)
</script>

<style scoped>
.autopilot { display: flex; flex-direction: column; gap: 16px; }
.hero-card .hero-title h2 { margin: 0 0 8px; }
.hero-card .hero-title p { margin: 0 0 16px; color: #909399; font-size: 13px; }
.launch-row { display: flex; gap: 12px; }
.keyword-input { flex: 1; }
.options-row { display: flex; align-items: center; gap: 12px; margin-top: 12px; }
.site-input { flex: 1; }
.option-hint { color: #909399; font-size: 12px; }
.steps-header { display: flex; justify-content: space-between; align-items: center; }
.steps-meta { display: flex; align-items: center; gap: 12px; }
.error-alert { margin-bottom: 12px; }
.step-row { display: flex; align-items: center; gap: 8px; }
.step-label { font-weight: 600; }
.step-detail { color: #909399; font-size: 12px; margin-top: 4px; }
</style>
