<template>
  <div class="obs-dashboard">
    <PageHeader
      title="AI 观测与渠道健康"
      subtitle="统一看板口径：AI 成本 / 送达率 / 转化漏斗 / 渠道在线率 / 封禁信号"
    />

    <el-row :gutter="20">
      <el-col :span="6">
        <el-card class="stat-card">
          <div class="stat-label">
            窗口
          </div>
          <div class="stat-value">
            {{ snapshot.window || '-' }}
          </div>
          <div class="stat-meta">
            生成于 {{ fmtTime(formattedAt) }}
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="stat-card">
          <div class="stat-label">
            AI 成本
          </div>
          <div class="stat-value">
            ¥{{ Number(snapshot.ai_cost?.cost || 0).toFixed(2) }}
          </div>
          <div class="stat-meta">
            {{ snapshot.ai_cost?.calls || 0 }} 次调用 · {{ snapshot.ai_cost?.total_tokens || 0 }} tokens
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="stat-card">
          <div class="stat-label">
            送达率
          </div>
          <div class="stat-value">
            {{ snapshot.delivery?.rate ?? 0 }}%
          </div>
          <div class="stat-meta">
            送达 {{ snapshot.delivery?.delivered || 0 }} · 失败 {{ snapshot.delivery?.failed || 0 }} · 在途 {{ snapshot.delivery?.inflight || 0 }}
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="stat-card">
          <div class="stat-label">
            封禁域名
          </div>
          <div class="stat-value">
            {{ snapshot.channel_health?.domain_banned || 0 }}
          </div>
          <div class="stat-meta">
            共探测 {{ snapshot.channel_health?.domain_total || 0 }} 个域名
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-row
      :gutter="20"
      class="mt-20"
    >
      <el-col :span="8">
        <el-card class="stat-card">
          <div class="stat-label">
            意图准确率（纠错采样口径）
          </div>
          <div class="stat-value">
            {{ snapshot.route?.north_star?.intent_accuracy ?? 0 }}%
          </div>
          <div class="stat-meta">
            识别 {{ snapshot.route?.north_star?.intent_recognized || 0 }} · 纠错 {{ snapshot.route?.north_star?.corrections || 0 }}
          </div>
        </el-card>
      </el-col>
      <el-col :span="8">
        <el-card class="stat-card">
          <div class="stat-label">
            解决率
          </div>
          <div class="stat-value">
            {{ snapshot.route?.north_star?.resolve_rate ?? 0 }}%
          </div>
          <div class="stat-meta">
            已了结 {{ snapshot.route?.north_star?.resolved || 0 }}/{{ snapshot.route?.north_star?.sessions || 0 }} 会话
          </div>
        </el-card>
      </el-col>
      <el-col :span="8">
        <el-card class="stat-card">
          <div class="stat-label">
            转人工率
          </div>
          <div class="stat-value">
            {{ snapshot.route?.north_star?.handoff_rate ?? 0 }}%
          </div>
          <div class="stat-meta">
            转人工 {{ snapshot.route?.north_star?.handoff || 0 }}/{{ snapshot.route?.north_star?.sessions || 0 }} 会话
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-row
      :gutter="20"
      class="mt-20"
    >
      <el-col :span="8">
        <el-card header="意图分布（窗口识别量）">
          <div
            v-if="intentDistribution.length"
            ref="intentChartEl"
            class="chart"
          />
          <el-empty
            v-else
            description="窗口内无意图识别记录"
            :image-size="60"
          />
        </el-card>
      </el-col>
      <el-col :span="8">
        <el-card header="链命中率（命中/跳过堆叠）">
          <div
            v-if="chainHitRate.length"
            ref="chainChartEl"
            class="chart"
          />
          <el-empty
            v-else
            description="窗口内无绑定链调用"
            :image-size="60"
          />
        </el-card>
      </el-col>
      <el-col :span="8">
        <el-card header="工具成功率（真实工具调用）">
          <div
            v-if="toolSuccessRate.length"
            ref="toolChartEl"
            class="chart"
          />
          <el-empty
            v-else
            description="窗口内无工具调用"
            :image-size="60"
          />
        </el-card>
      </el-col>
    </el-row>

    <el-row
      :gutter="20"
      class="mt-20"
    >
      <el-col :span="12">
        <el-card header="渠道健康（在线率 + 封禁信号）">
          <el-table
            :data="channels"
            stripe
          >
            <el-table-column
              prop="channel"
              label="渠道"
              min-width="90"
            />
            <el-table-column
              label="在线"
              min-width="110"
            >
              <template #default="{ row }">
                {{ row.online }}/{{ row.accounts }}（{{ row.online_rate }}%）
              </template>
            </el-table-column>
            <el-table-column
              prop="selector_worst"
              label="选择器"
              min-width="80"
            >
              <template #default="{ row }">
                <el-tag
                  :type="selectorTagType(row.selector_worst)"
                  size="small"
                >
                  {{ selectorText(row.selector_worst) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              label="状态"
              min-width="80"
            >
              <template #default="{ row }">
                <el-tag
                  :type="statusTagType(row.status)"
                  size="small"
                >
                  {{ statusText(row.status) }}
                </el-tag>
              </template>
            </el-table-column>
          </el-table>
          <el-empty
            v-if="!channels.length"
            description="暂无渠道账号（未接入渠道不显示为异常）"
            :image-size="60"
          />
        </el-card>
      </el-col>
      <el-col :span="12">
        <el-card header="AI 全链路转化漏斗">
          <el-table
            :data="funnelStages"
            stripe
          >
            <el-table-column
              prop="node_label"
              label="阶段"
              min-width="110"
            />
            <el-table-column
              prop="count"
              label="消息轮次"
              min-width="80"
            />
            <el-table-column
              label="阶段转化率"
              min-width="100"
            >
              <template #default="{ row }">
                <span v-if="row.conversion > 0">{{ row.conversion }}%</span>
                <span
                  v-else
                  class="dim"
                >—</span>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-col>
    </el-row>

    <el-row
      :gutter="20"
      class="mt-20"
    >
      <el-col :span="12">
        <el-card header="送达率（按渠道）">
          <el-table
            :data="deliveryChannels"
            stripe
          >
            <el-table-column
              prop="channel"
              label="渠道"
              min-width="90"
            />
            <el-table-column
              prop="delivered"
              label="送达"
              min-width="70"
            />
            <el-table-column
              prop="failed"
              label="失败"
              min-width="70"
            />
            <el-table-column
              prop="inflight"
              label="在途"
              min-width="70"
            />
            <el-table-column
              label="送达率"
              min-width="80"
            >
              <template #default="{ row }">
                {{ row.rate }}%
              </template>
            </el-table-column>
          </el-table>
          <el-empty
            v-if="!deliveryChannels.length"
            description="窗口内暂无出站消息"
            :image-size="60"
          />
        </el-card>
      </el-col>
      <el-col :span="12">
        <el-card header="AI 成本（按 provider）">
          <el-table
            :data="providers"
            stripe
          >
            <el-table-column
              prop="key"
              label="Provider"
              min-width="110"
            />
            <el-table-column
              prop="calls"
              label="调用"
              min-width="70"
            />
            <el-table-column
              prop="tokens"
              label="Tokens"
              min-width="90"
            />
            <el-table-column
              label="成本"
              min-width="90"
            >
              <template #default="{ row }">
                ¥{{ Number(row.cost || 0).toFixed(2) }}
              </template>
            </el-table-column>
            <el-table-column
              prop="avg_latency_ms"
              label="均延迟(ms)"
              min-width="90"
            />
          </el-table>
          <el-empty
            v-if="!providers.length"
            description="窗口内无 LLM 调用记录（成本 0 是合法读数）"
            :image-size="60"
          />
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import PageHeader from '@/components/PageHeader.vue'
import { getObservabilityDashboard } from '@/api/observabilityDashboard'
import { fmtTime } from '@/utils/time'
import { safeInit, safeDispose } from '@/utils/echarts'

const snapshot = ref({})
const loading = ref(false)

const channels = computed(() => snapshot.value.channel_health?.channels || [])
const deliveryChannels = computed(() => snapshot.value.delivery?.by_channel || [])
const funnelStages = computed(() => snapshot.value.funnel?.stages || [])
const providers = computed(() => snapshot.value.ai_cost?.by_provider || [])

// TB-11 刀5：北极星三指标 + 三图（数据全部来自 snapshot.route，label 后端已出，
// 前端不背 9 语种 i18n）。
const intentDistribution = computed(() => snapshot.value.route?.intent_distribution || [])
const chainHitRate = computed(() => snapshot.value.route?.chain_hit_rate || [])
const toolSuccessRate = computed(() => snapshot.value.route?.tool_success_rate || [])

const intentChartEl = ref(null)
const chainChartEl = ref(null)
const toolChartEl = ref(null)
let intentChart = null
let chainChart = null
let toolChart = null

function renderRouteCharts() {
  if (intentDistribution.value.length) {
    intentChart = safeInit(intentChartEl.value)
    intentChart?.setOption({
      grid: { left: 40, right: 16, top: 24, bottom: 28 },
      xAxis: { type: 'category', data: intentDistribution.value.map((r) => r.intent_type) },
      yAxis: { type: 'value', minInterval: 1 },
      series: [{ name: '识别量', type: 'bar', data: intentDistribution.value.map((r) => r.count) }]
    })
  }
  if (chainHitRate.value.length) {
    chainChart = safeInit(chainChartEl.value)
    chainChart?.setOption({
      grid: { left: 40, right: 16, top: 24, bottom: 28 },
      legend: { data: ['命中', '跳过'], top: 0 },
      xAxis: { type: 'category', data: chainHitRate.value.map((r) => r.chain_name) },
      yAxis: { type: 'value', minInterval: 1 },
      series: [
        { name: '命中', type: 'bar', stack: 'chain', data: chainHitRate.value.map((r) => r.hits) },
        { name: '跳过', type: 'bar', stack: 'chain', data: chainHitRate.value.map((r) => r.misses) }
      ]
    })
  }
  if (toolSuccessRate.value.length) {
    toolChart = safeInit(toolChartEl.value)
    toolChart?.setOption({
      grid: { left: 40, right: 16, top: 24, bottom: 28 },
      xAxis: { type: 'category', data: toolSuccessRate.value.map((r) => r.tool_name) },
      yAxis: { type: 'value', min: 0, max: 100 },
      series: [{ name: '成功率(%)', type: 'bar', data: toolSuccessRate.value.map((r) => r.success_rate) }]
    })
  }
}

function disposeRouteCharts() {
  safeDispose(intentChart)
  safeDispose(chainChart)
  safeDispose(toolChart)
  intentChart = null
  chainChart = null
  toolChart = null
}

const formattedAt = computed(() => {
  const at = snapshot.value.generated_at
  if (!at) return '-'
  return new Date(at).toLocaleString()
})

function selectorText(v) {
  return { ok: '正常', degraded: '降级', broken: '失效', warming: '观察中', none: '无读数' }[v] || v
}

function selectorTagType(v) {
  return { ok: 'success', degraded: 'warning', broken: 'danger', warming: 'info', none: 'info' }[v] || 'info'
}

function statusText(v) {
  return {
    healthy: '健康',
    degraded: '降级',
    offline: '离线',
    banned: '封禁信号',
    empty: '未接入'
  }[v] || v
}

function statusTagType(v) {
  return { healthy: 'success', degraded: 'warning', offline: 'danger', banned: 'danger', empty: 'info' }[v] || 'info'
}

async function load() {
  loading.value = true
  try {
    const res = await getObservabilityDashboard('7d')
    snapshot.value = res?.data || res || {}
    renderRouteCharts()
  } finally {
    loading.value = false
  }
}

onMounted(load)
onBeforeUnmount(disposeRouteCharts)
</script>

<style scoped>
.obs-dashboard {
  padding: 0;
}

.stat-card {
  text-align: center;
}

.stat-label {
  color: #909399;
  font-size: 13px;
}

.stat-value {
  font-size: 26px;
  font-weight: 600;
  margin: 8px 0;
}

.stat-meta {
  color: #c0c4cc;
  font-size: 12px;
}

.chart {
  width: 100%;
  height: 240px;
}

.mt-20 {
  margin-top: 20px;
}

.dim {
  color: #c0c4cc;
}
</style>
