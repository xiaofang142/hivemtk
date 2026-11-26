<template>
  <div class="sales-cockpit">
    <PageHeader
      title="AI 销冠驾驶舱"
      subtitle="ReAct 智能体、SOP、RAG、触达四大核心能力全景"
    />

    <el-alert
      :title="t('navigationContent.salesCockpit.metricsNote')"
      type="info"
      :closable="false"
      show-icon
      class="metrics-notice"
    />
    <p
      v-if="cockpitLoading"
      role="status"
    >
      {{ t('navigationContent.salesCockpit.loading') }}
    </p>
    <div
      v-if="cockpitLoadError"
      class="metrics-notice"
    >
      <el-alert
        :title="t('navigationContent.salesCockpit.loadError')"
        type="error"
        :closable="false"
        show-icon
      />
      <el-button
        :loading="cockpitLoading"
        class="retry-button"
        @click="loadCockpit"
      >
        {{ t('navigationContent.salesCockpit.retry') }}
      </el-button>
    </div>

    
    <el-row :gutter="20">
      <el-col :span="6">
        <el-card class="capability-card">
          <div class="capability-icon">
            🤖
          </div>
          <div class="capability-name">
            ReAct 智能体
          </div>
          <div class="capability-stat">
            <span class="big-num">{{ cockpit.react.totalRuns }}</span>
            <span class="unit">本周期调用</span>
          </div>
          <div class="capability-meta">
            5 轮 / 30s 防线
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="capability-card">
          <div class="capability-icon">
            📋
          </div>
          <div class="capability-name">
            SOP 引擎
          </div>
          <div class="capability-stat">
            <span class="big-num">{{ cockpit.sop.executions }}</span>
            <span class="unit">执行中</span>
          </div>
          <div class="capability-meta">
            14 节点 DAG
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="capability-card">
          <div class="capability-icon">
            🔍
          </div>
          <div class="capability-name">
            RAG 三级检索
          </div>
          <div class="capability-stat">
            <span class="big-num">{{ cockpit.rag.queries }}</span>
            <span class="unit">命中</span>
          </div>
          <div class="capability-meta">
            L1 缓存 / L2 PG / L3 Rerank
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="capability-card">
          <div class="capability-icon">
            📡
          </div>
          <div class="capability-name">
            触达 Pipeline
          </div>
          <div class="capability-stat">
            <span class="big-num">{{ cockpit.reach.sentToday }}</span>
            <span class="unit">今日发送</span>
          </div>
          <div class="capability-meta">
            9 渠道 / 限流
          </div>
        </el-card>
      </el-col>
    </el-row>

    
    <!-- LTC 三率（T-P8-05，LTC-29/AC4）：闭环完成率是北极星指标，摆首位 -->
    <el-row
      :gutter="20"
      class="mt-20"
    >
      <el-col :span="8">
        <el-card class="capability-card ltc-north-star">
          <div class="capability-icon">
            🎯
          </div>
          <div class="capability-name">
            闭环完成率
            <el-tag
              type="danger"
              size="small"
            >
              北极星
            </el-tag>
          </div>
          <div class="capability-stat">
            <span class="big-num">
              {{ ltc.closureText }}
            </span>
          </div>
          <div class="capability-meta">
            {{ ltc.closureMeta }}
          </div>
        </el-card>
      </el-col>
      <el-col :span="8">
        <el-card class="capability-card">
          <div class="capability-icon">
            💰
          </div>
          <div class="capability-name">
            回款率
          </div>
          <div class="capability-stat">
            <span class="big-num">
              {{ ltc.collectionText }}
            </span>
          </div>
          <div class="capability-meta">
            {{ ltc.collectionMeta }}
          </div>
        </el-card>
      </el-col>
      <el-col :span="8">
        <el-card class="capability-card">
          <div class="capability-icon">
            ⏰
          </div>
          <div class="capability-name">
            逾期率
          </div>
          <div class="capability-stat">
            <span class="big-num">
              {{ ltc.overdueText }}
            </span>
          </div>
          <div class="capability-meta">
            {{ ltc.overdueMeta }}
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-row
      :gutter="20"
      class="mt-20"
    >
      <el-col :span="12">
        <el-card header="意图分布（12 类）">
          <div
            ref="intentChart"
            style="height: 300px;"
          />
        </el-card>
      </el-col>
      <el-col :span="12">
        <el-card :header="t('navigationContent.salesCockpit.routesTitle')">
          <el-table
            :data="cockpit.llmRoutes"
            stripe
          >
            <el-table-column
              prop="scenario"
              :label="t('navigationContent.salesCockpit.scenario')"
            />
            <el-table-column
              prop="provider"
              :label="t('navigationContent.salesCockpit.provider')"
            />
            <el-table-column
              prop="callsToday"
              :label="t('navigationContent.salesCockpit.callsToday')"
            />
          </el-table>
        </el-card>
      </el-col>
    </el-row>

    
    <el-row
      :gutter="20"
      class="mt-20"
    >
      <el-col :span="12">
        <el-card :header="t('navigationContent.salesCockpit.channelsTitle')">
          <el-table
            :data="cockpit.channelHealth"
            stripe
          >
            <el-table-column
              prop="channel"
              :label="t('navigationContent.salesCockpit.channel')"
            />
            <el-table-column
              prop="accountCount"
              :label="t('navigationContent.salesCockpit.accountCount')"
            />
            <el-table-column :label="t('navigationContent.salesCockpit.status')">
              <template #default="{ row }">
                <el-tag :type="getAccountStatusTagType(row.status) || 'info'">
                  {{ accountStatusText(row.status) }}
                </el-tag>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-col>
      <el-col :span="12">
        <el-card header="智能体工具使用 TOP 10">
          <el-table
            :data="cockpit.topTools"
            stripe
          >
            <el-table-column
              type="index"
              label="#"
              width="50"
            />
            <el-table-column
              prop="name"
              label="工具"
            />
            <el-table-column
              prop="category"
              label="分类"
            />
            <el-table-column
              prop="calls"
              label="调用次数"
            />
            <el-table-column
              prop="avgLatency"
              label="平均延迟"
            />
          </el-table>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import PageHeader from '@/components/PageHeader.vue'
import { getSalesCockpit } from '@/api/cockpit'
import LtcRatesApi from '@/api/ltcRates'
import * as _echarts from 'echarts'
import { safeInit } from '@/utils/echarts'
import { getAccountStatusTagType } from '@/constants/accountType'

const { t } = useI18n()
const intentChart = ref(null)
const cockpitLoading = ref(false)
const cockpitLoadError = ref(false)
const cockpit = ref({
  react: { totalRuns: '—' },
  sop: { executions: '—' },
  rag: { queries: '—' },
  reach: { sentToday: '—' },
  llmRoutes: [],
  channelHealth: [],
  topTools: [],
  intentDistribution: []
})

/**
 * LTC 三率（T-P8-05）：后端 /api/ltc-rates 返回 {closure, collection, overdue}，
 * 每率 {rate, numerator, denominator}。分子分母摆出来，AC① 对账直接可比。
 */
const ltc = ref({
  closureText: '—', closureMeta: '加载中…',
  collectionText: '—', collectionMeta: '加载中…',
  overdueText: '—', overdueMeta: '加载中…'
})

function fmtRate(v) {
  const n = Number(v && v.rate)
  if (!Number.isFinite(n)) return '—'
  return (n * 100).toFixed(1) + '%'
}

function fmtFrac(v) {
  const num = Number(v && v.numerator || 0)
  const den = Number(v && v.denominator || 0)
  return `${num.toLocaleString()} / ${den.toLocaleString()}`
}

async function loadLtcRates() {
  try {
    const res = await LtcRatesApi.getRates()
    const d = (res && res.data) || res || {}
    ltc.value = {
      closureText: fmtRate(d.closure),
      closureMeta: `已闭环 ${fmtFrac(d.closure)}`,
      collectionText: fmtRate(d.collection),
      collectionMeta: `已回款 ${fmtFrac(d.collection)} 元`,
      overdueText: fmtRate(d.overdue),
      overdueMeta: `逾期 ${fmtFrac(d.overdue)} 单`
    }
  } catch (err) {
    console.error('load ltc rates failed:', err)
    ltc.value = {
      closureText: '—', closureMeta: '加载失败',
      collectionText: '—', collectionMeta: '加载失败',
      overdueText: '—', overdueMeta: '加载失败'
    }
  }
}

/**
 * 将后端 /api/ai/sales-cockpit 返回的 data 字段适配为前端模板期望的结构。
 * 后端原始字段（snake_case / 平铺）→ 前端模板字段（camelCase / 语义化）。
 */
function adaptCockpit(src) {
  const s = src || {}

  // 四张能力卡 —— 后端结构已对齐，直接取值
  const react = s.react || {}
  const sop = s.sop || {}
  const rag = s.rag || {}
  const reach = s.reach || {}

  // calls 是业务日累计次数；接口没有 QPS、限额或错误率，不能从计数推算。
  const llmRoutes = (s.llmRoutes || []).map(r => ({
    scenario: r.scenario || '—',
    provider: r.provider || '—',
    callsToday: Number(r.calls || 0).toLocaleString()
  }))

  // cnt 是同一 platform/status 下的账号数量，保留真实状态，不解释为吞吐负载。
  const channelHealth = (s.channelHealth || []).map(r => ({
    channel: r.platform || '—',
    accountCount: Number(r.cnt || 0).toLocaleString(),
    status: r.status
  }))

  // 意图分布 → echarts pie [{name, value}]
  const intentDistribution = (s.intentDistribution || []).map(r => ({
    name: r.intent || '未分类',
    value: Number(r.cnt || 0)
  }))

  // Top 工具：后端 {tool_name, calls}
  //          → 前端 {name, category, calls, avgLatency}
  const topTools = (s.topTools || []).map(r => ({
    name: r.tool_name || '—',
    category: '—',
    calls: Number(r.calls || 0).toLocaleString(),
    avgLatency: '—'
  }))

  return {
    react: { totalRuns: Number(react.totalRuns || 0).toLocaleString() },
    sop:   { executions: Number(sop.executions || 0).toLocaleString() },
    rag:   { queries:    Number(rag.queries || 0).toLocaleString() },
    reach: { sentToday:  Number(reach.sentToday || 0).toLocaleString() },
    llmRoutes,
    channelHealth,
    intentDistribution,
    topTools
  }
}

function accountStatusText(status) {
  const labels = {
    1: 'normal', 2: 'abnormal', 3: 'notLoggedIn',
    active: 'normal', normal: 'normal', online: 'online', offline: 'offline',
    banned: 'banned', warning: 'warning', critical: 'critical'
  }
  const label = Object.hasOwn(labels, status) ? labels[status] : null
  if (label) return t('navigationContent.salesCockpit.' + label)
  const unknown = t('navigationContent.salesCockpit.unknownStatus')
  return status === undefined || status === null || status === '' ? unknown : `${unknown} (${status})`
}

let chartInstance = null
let timer = null
let disposed = false

async function loadCockpit() {
  if (disposed || cockpitLoading.value) return
  cockpitLoading.value = true
  cockpitLoadError.value = false
  try {
    const res = await getSalesCockpit()
    if (disposed) return
    cockpit.value = adaptCockpit(res.data || res)
    await nextTick()
    if (!disposed) renderIntentChart()
  } catch (err) {
    console.error('load cockpit failed:', err)
    if (!disposed) cockpitLoadError.value = true
  } finally {
    if (!disposed) cockpitLoading.value = false
  }
}

function renderIntentChart() {
  if (!intentChart.value) return
  if (chartInstance) {
    chartInstance.dispose()
    chartInstance = null
  }
  chartInstance = safeInit(intentChart.value)
  chartInstance.setOption({
    tooltip: { trigger: 'item' },
    legend: { bottom: 0 },
    series: [{
      type: 'pie',
      radius: ['40%', '70%'],
      data: cockpit.value.intentDistribution,
      label: { formatter: '{b}: {c} ({d}%)' }
    }]
  })
}

onMounted(() => {
  loadCockpit()
  loadLtcRates()
  timer = setInterval(() => { loadCockpit(); loadLtcRates() }, 60000)
})
onUnmounted(() => {
  disposed = true
  if (timer) clearInterval(timer)
  if (chartInstance) {
    chartInstance.dispose()
    chartInstance = null
  }
})
</script>

<style scoped>
.sales-cockpit { padding: 20px; }
.metrics-notice { margin-bottom: 20px; }
.retry-button { margin-top: 12px; }
.capability-card { text-align: center; padding: 24px 0; }
.capability-icon { font-size: 48px; margin-bottom: 12px; }
.capability-name { font-size: 16px; font-weight: 500; color: #303133; }
.capability-stat { margin: 16px 0; }
.capability-stat .big-num { font-size: 36px; font-weight: 600; color: #409eff; }
.capability-stat .unit { font-size: 12px; color: #909399; margin-left: 6px; }
.capability-meta { font-size: 12px; color: #909399; }
.ltc-north-star { border-color: #f56c6c; }
.mt-20 { margin-top: 20px; }
</style>
