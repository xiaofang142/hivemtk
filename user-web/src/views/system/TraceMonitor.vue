<template>
  <div class="app-container trace-monitor">
    <el-tabs v-model="activeTab" class="trace-tabs">
      
      <el-tab-pane label="消息链路明细" name="trace">
        <el-form :inline="true" class="trace-query" @submit.prevent>
          <el-form-item label="Trace ID">
            <el-input v-model.trim="form.trace_id" placeholder="单轮对话 trace_id" clearable style="width: 260px" />
          </el-form-item>
          <el-form-item label="会话 ID">
            <el-input v-model.trim="form.conversation_id" placeholder="会话 conversation_id" clearable style="width: 260px" />
          </el-form-item>
          <el-form-item label="消息 ID">
            <el-input v-model.trim="form.msg_id" placeholder="消息 msg_id（反查）" clearable style="width: 220px" />
          </el-form-item>
          <el-form-item label="渠道">
            <el-select v-model="form.channel" placeholder="全部" clearable style="width: 160px">
              <el-option v-for="c in channels" :key="c" :label="c" :value="c" />
            </el-select>
          </el-form-item>
          <el-form-item>
            <el-button type="primary" :loading="loadingTree" @click="loadTree">查询</el-button>
            <el-button @click="resetForm">重置</el-button>
          </el-form-item>
        </el-form>

        <el-alert
          v-if="treeError"
          type="error"
          :closable="false"
          :title="treeError"
          show-icon
          class="mb-12"
        />

        <div v-else-if="tree" class="trace-summary mb-12">
          <el-tag type="info" effect="plain">Trace: {{ tree.trace_id || '—' }}</el-tag>
          <el-tag type="info" effect="plain">会话: {{ tree.conversation_id || '—' }}</el-tag>
          <el-tag type="info" effect="plain">渠道: {{ tree.channel || '—' }}</el-tag>
          <span class="trace-count">共 {{ tree.spans ? tree.spans.length : 0 }} 个 Span</span>
        </div>

        <el-empty v-if="!loadingTree && !tree && !treeError" description="输入 Trace ID / 会话 ID 查询完整链路" />
        <el-skeleton v-else-if="loadingTree" :rows="8" animated />

        <div v-else class="trace-tree">
          <template v-for="(item, idx) in flattened" :key="idx">
            
            <el-card
              v-if="!item._turn && !item._tool"
              class="span-card"
              :class="{ abnormal: isAbnormal(item), agent: item.node === 'ai_dispatch' }"
              shadow="hover"
            >
              <div class="span-head" @click="toggle(item)">
                <span class="caret">{{ expanded[item.id] ? '▾' : '▸' }}</span>
                <span class="span-kind kind-lifecycle">生命周期</span>
                <span class="span-title">{{ nodeLabel(item.node) }}</span>
                <span class="span-node">{{ item.node }}</span>
                <el-tag size="small" :type="statusType(item)">{{ statusText(item) }}</el-tag>
                <span class="span-dur">{{ item.duration_ms }} ms</span>
                <span class="span-time">{{ item.created_at }}</span>
              </div>
              <div v-show="expanded[item.id]" class="span-body">
                <el-descriptions :column="1" border size="small">
                  <el-descriptions-item label="入参">
                    <pre class="code-block">{{ fmt(item.input) }}</pre>
                  </el-descriptions-item>
                  <el-descriptions-item label="出参">
                    <pre class="code-block">{{ fmt(item.output) }}</pre>
                  </el-descriptions-item>
                  <el-descriptions-item label="预期结果">{{ item.expected || '—' }}</el-descriptions-item>
                  <el-descriptions-item v-if="item.abnormal || item.error" label="异常">
                    <span class="err-text">{{ item.error || '异常' }}</span>
                  </el-descriptions-item>
                </el-descriptions>
              </div>
            </el-card>

            
            <el-card v-else-if="item._turn" class="span-card turn-card" shadow="hover">
              <div class="span-head" @click="toggle(item.turn)">
                <span class="caret">{{ expanded[item.turn.id] ? '▾' : '▸' }}</span>
                <span class="span-kind kind-turn">Agent 轮</span>
                <span class="span-title">第 {{ item.ti }} 轮</span>
                <span v-if="item.turn.agent_id" class="span-node">{{ item.turn.agent_id }}</span>
                <el-tag size="small" :type="statusType(item.turn)">{{ statusText(item.turn) }}</el-tag>
                <span class="span-dur">{{ item.turn.duration_ms }} ms</span>
                <span class="span-time">{{ item.turn.created_at }}</span>
              </div>
              <div v-show="expanded[item.turn.id]" class="span-body">
                <el-descriptions :column="1" border size="small">
                  <el-descriptions-item label="入参">
                    <pre class="code-block">{{ fmt(item.turn.input) }}</pre>
                  </el-descriptions-item>
                  <el-descriptions-item label="出参">
                    <pre class="code-block">{{ fmt(item.turn.output) }}</pre>
                  </el-descriptions-item>
                  <el-descriptions-item v-if="item.turn.abnormal || item.turn.error" label="异常">
                    <span class="err-text">{{ item.turn.error || '异常' }}</span>
                  </el-descriptions-item>
                </el-descriptions>
              </div>
            </el-card>

            
            <el-card v-else class="span-card tool-card" shadow="never">
              <div class="span-head">
                <span class="span-kind kind-tool">工具</span>
                <span class="span-title">{{ item.tool.tool_name }}</span>
                <el-tag size="small" :type="statusType(item.tool)">{{ statusText(item.tool) }}</el-tag>
                <span class="span-dur">{{ item.tool.duration_ms }} ms</span>
                <span class="span-time">{{ item.tool.created_at }}</span>
              </div>
              <div class="span-body">
                <el-descriptions :column="1" border size="small">
                  <el-descriptions-item label="入参">
                    <pre class="code-block">{{ fmt(item.tool.input) }}</pre>
                  </el-descriptions-item>
                  <el-descriptions-item label="出参">
                    <pre class="code-block">{{ fmt(item.tool.output) }}</pre>
                  </el-descriptions-item>
                  <el-descriptions-item v-if="item.tool.abnormal || item.tool.error" label="异常">
                    <span class="err-text">{{ item.tool.error || '异常' }}</span>
                  </el-descriptions-item>
                </el-descriptions>
              </div>
            </el-card>
          </template>
        </div>
      </el-tab-pane>

      
      <el-tab-pane label="业务健康概览" name="health">
        <div class="stat-grid">
          <el-card v-for="s in healthStats" :key="s.label" shadow="hover" class="stat-card">
            <div class="stat-label">{{ s.label }}</div>
            <div class="stat-value" :class="s.cls">{{ s.value }}</div>
            <div class="stat-unit">{{ s.unit }}</div>
          </el-card>
        </div>
        <el-alert
          v-if="health && ((health.stuck_reachable || 0) > 0 || (health.stuck_unreachable || 0) > 0 || (health.sync_gap_count || 0) > 0)"
          type="warning"
          :closable="false"
          title="存在链路卡住或数据缺口，请见「异常」页"
          class="mt-12"
        />
      </el-tab-pane>

      
      <el-tab-pane label="节点健康" name="node">
        <el-alert v-if="nodeWindow" type="info" :closable="false" :title="'统计窗口：' + nodeWindow" class="mb-12" />
        <el-table :data="nodeRows" border stripe size="small" empty-text="暂无数据">
          <el-table-column prop="channel" label="渠道" width="160" />
          <el-table-column prop="node" label="节点" width="200" />
          <el-table-column prop="total" label="样本数" width="100" />
          <el-table-column prop="avg_duration_ms" label="平均耗时(ms)" width="130" />
          <el-table-column prop="p95_duration_ms" label="P95(ms)" width="120" />
          <el-table-column label="异常率" width="120">
            <template #default="{ row }">{{ (row.abnormal_rate * 100).toFixed(1) }}%</template>
          </el-table-column>
          <el-table-column label="健康" width="100">
            <template #default="{ row }">
              <el-tag :type="row.abnormal_rate > 0.3 ? 'danger' : row.abnormal_rate > 0.05 ? 'warning' : 'success'" size="small">
                {{ row.abnormal_rate > 0.3 ? '差' : row.abnormal_rate > 0.05 ? '中' : '良' }}
              </el-tag>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>

      
      <el-tab-pane label="端到端时延" name="latency">
        <el-table :data="latencyRows" border stripe size="small" empty-text="暂无数据">
          <el-table-column prop="channel" label="渠道" width="180" />
          <el-table-column prop="p50_ms" label="P50(ms)" width="140" />
          <el-table-column prop="p95_ms" label="P95(ms)" width="140" />
          <el-table-column prop="sample_size" label="样本数" width="120" />
        </el-table>
      </el-tab-pane>

      
      <el-tab-pane label="会话链路" name="lifecycle">
        <el-form :inline="true" @submit.prevent>
          <el-form-item label="会话 ID">
            <el-input v-model.trim="lcForm.conversation_id" placeholder="会话 conversation_id" clearable style="width: 260px" />
          </el-form-item>
          <el-form-item label="Trace ID">
            <el-input v-model.trim="lcForm.trace_id" placeholder="单轮 trace_id" clearable style="width: 260px" />
          </el-form-item>
          <el-form-item>
            <el-button type="primary" :loading="loadingLc" @click="loadLifecycle">查询</el-button>
          </el-form-item>
        </el-form>
        <el-table :data="lifecycleRows" border stripe size="small" empty-text="暂无数据" class="mt-12">
          <el-table-column prop="trace_id" label="Trace" width="180" />
          <el-table-column prop="node" label="节点" width="180" />
          <el-table-column prop="channel" label="渠道" width="120" />
          <el-table-column prop="direction" label="方向" width="100" />
          <el-table-column prop="duration_ms" label="耗时(ms)" width="100" />
          <el-table-column label="状态" width="100">
            <template #default="{ row }">
              <el-tag :type="row.abnormal ? 'danger' : 'success'" size="small">
                {{ row.abnormal ? '异常' : '正常' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="expected" label="预期" min-width="160" />
          <el-table-column prop="error" label="异常信息" min-width="160" show-overflow-tooltip />
        </el-table>
      </el-tab-pane>

      
      <el-tab-pane label="链路列表" name="traces">
        <el-table :data="traceRows" border stripe size="small" empty-text="暂无数据">
          <el-table-column prop="trace_id" label="Trace" width="180" />
          <el-table-column prop="conversation_id" label="会话" width="180" />
          <el-table-column prop="channel" label="渠道" width="120" />
          <el-table-column prop="node_count" label="节点数" width="90" />
          <el-table-column label="异常数" width="90">
            <template #default="{ row }">
              <el-tag :type="row.abnormal_count > 0 ? 'danger' : 'info'" size="small">{{ row.abnormal_count }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="first_at" label="首节点" width="180" />
          <el-table-column prop="last_at" label="末节点" width="180" />
          <el-table-column label="操作" width="100" fixed="right">
            <template #default="{ row }">
              <el-button type="primary" link @click="openTrace(row)">查看</el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>

      
      <el-tab-pane label="异常" name="anomalies">
        <el-collapse v-if="anomaly" v-model="anomalyActive">
          <el-collapse-item title="数据缺口（缺同步回执）" name="sync_gap">
            <el-table :data="anomaly.sync_gap || []" border size="small" empty-text="无">
              <el-table-column prop="conversation_id" label="会话" width="200" />
              <el-table-column prop="channel" label="渠道" width="120" />
              <el-table-column prop="message_count" label="缺口数" width="100" />
            </el-table>
          </el-collapse-item>
          <el-collapse-item title="卡住（可直接送达）" name="stuck_reachable">
            <el-table :data="anomaly.stuck_reachable || []" border size="small" empty-text="无">
              <el-table-column prop="conversation_id" label="会话" width="200" />
              <el-table-column prop="channel" label="渠道" width="120" />
              <el-table-column prop="age_min" label="滞留(分)" width="100" />
            </el-table>
          </el-collapse-item>
          <el-collapse-item title="卡住（不可达）" name="stuck_unreachable">
            <el-table :data="anomaly.stuck_unreachable || []" border size="small" empty-text="无">
              <el-table-column prop="conversation_id" label="会话" width="200" />
              <el-table-column prop="channel" label="渠道" width="120" />
              <el-table-column prop="age_min" label="滞留(分)" width="100" />
            </el-table>
          </el-collapse-item>
          <el-collapse-item title="不可达（无激活会话）" name="unreachable">
            <el-table :data="anomaly.unreachable || []" border size="small" empty-text="无">
              <el-table-column prop="conversation_id" label="会话" width="200" />
              <el-table-column prop="channel" label="渠道" width="120" />
            </el-table>
          </el-collapse-item>
          <el-collapse-item title="节点异常" name="node_abnormal">
            <el-table :data="anomaly.node_abnormal || []" border size="small" empty-text="无">
              <el-table-column prop="channel" label="渠道" width="140" />
              <el-table-column prop="node" label="节点" width="180" />
              <el-table-column prop="abnormal_rate" label="异常率" width="120">
                <template #default="{ row }">{{ (row.abnormal_rate * 100).toFixed(1) }}%</template>
              </el-table-column>
            </el-table>
          </el-collapse-item>
        </el-collapse>
        <el-empty v-else description="暂无异常" />
      </el-tab-pane>

      
      <el-tab-pane label="自学习 / 知识库权重" name="learn">
        <div class="learn-toolbar">
          <el-form :inline="true" @submit.prevent>
            <el-form-item label="扫描窗口(小时)">
              <el-input-number v-model="evalHours" :min="1" :max="168" :step="12" style="width: 140px" />
            </el-form-item>
            <el-form-item label="单次条数">
              <el-input-number v-model="evalLimit" :min="1" :max="200" :step="10" style="width: 130px" />
            </el-form-item>
            <el-form-item>
              <el-button type="primary" :loading="triggerLoading" @click="triggerEval">手动触发评估</el-button>
              <el-button :loading="triggerLoading" @click="refreshLearn">刷新</el-button>
            </el-form-item>
          </el-form>
          <el-alert
            v-if="triggerMsg"
            :type="triggerMsgType"
            :closable="false"
            :title="triggerMsg"
            class="mt-12"
            show-icon
          />
        </div>

        <el-divider content-position="left">打分记录（LLM 对每条 trace 完整请求-响应链评分）</el-divider>
        <el-table :data="evalLogs" border stripe size="small" empty-text="暂无打分记录，点上方「手动触发评估」" class="mt-12">
          <el-table-column prop="created_at" label="评估时间" width="180" />
          <el-table-column prop="channel" label="渠道" width="120" />
          <el-table-column prop="trace_id" label="Trace" width="180" show-overflow-tooltip />
          <el-table-column label="评分" width="80">
            <template #default="{ row }">
              <el-tag :type="scoreType(row.score)" size="small">{{ row.score }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="质量" width="80">
            <template #default="{ row }">
              <el-tag :type="row.bad ? 'danger' : row.score >= 85 ? 'success' : 'warning'" size="small" effect="plain">
                {{ row.bad ? '差' : row.score >= 85 ? '优' : '中' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="维度" min-width="230">
            <template #default="{ row }">{{ dimText(row.dimensions) }}</template>
          </el-table-column>
          <el-table-column label="原因" prop="reason" min-width="200" show-overflow-tooltip />
          <el-table-column label="权重调整" width="130">
            <template #default="{ row }">
              <span v-if="adjustedList(row.adjusted_chunks).length">{{ adjustedList(row.adjusted_chunks).length }} 个 chunk</span>
              <span v-else class="muted">无</span>
            </template>
          </el-table-column>
        </el-table>

        <el-divider content-position="left">知识库权重排行（运行时权重，偏离 1.0 最大者优先）</el-divider>
        <el-table :data="weights" border stripe size="small" empty-text="暂无权重偏离（全部为默认 1.0）" class="mt-12">
          <el-table-column prop="id" label="Chunk ID" width="100" />
          <el-table-column prop="content" label="内容" min-width="280" show-overflow-tooltip />
          <el-table-column label="权重" width="120">
            <template #default="{ row }">
              <el-tag :type="row.weight < 1 ? 'danger' : row.weight > 1 ? 'success' : 'info'" size="small">
                {{ Number(row.weight).toFixed(2) }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="hit_count" label="命中次数" width="100" />
        </el-table>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup>
import { ref, reactive, computed, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { MonitorApi as monitorApi } from '@/api/monitor'

const channels = ['douyin', 'xiaohongshu', 'wechat', 'telegram', 'whatsapp', 'instagram', 'facebook', 'line', 'kuaishou', 'weibo']

const activeTab = ref('trace')
const form = reactive({ trace_id: '', conversation_id: '', msg_id: '', channel: '' })
const tree = ref(null)
const treeError = ref('')
const loadingTree = ref(false)
const expanded = reactive({})

const lcForm = reactive({ conversation_id: '', trace_id: '' })
const lifecycleRows = ref([])
const loadingLc = ref(false)

const health = ref(null)
const nodeRows = ref([])
const nodeWindow = ref('')
const latencyRows = ref([])
const traceRows = ref([])
const anomaly = ref(null)
const anomalyActive = ref(['sync_gap'])

const evalLogs = ref([]);
const weights = ref([])
const triggerLoading = ref(false)
const triggerMsg = ref('')
const triggerMsgType = ref('info')
const evalHours = ref(24)
const evalLimit = ref(20)
const learningLoaded = ref(false)

const NODE_LABEL = {
  ingest: '上报接入',
  ai_dispatch: 'AI 调度',
  outbound_enqueue: '下发入队',
  inbox_sync: '收件同步',
  downlink_fetch: '下行拉取',
  delivered_ack: '送达确认'
}

const nodeLabel = (n) => NODE_LABEL[n] || n || '—'

function fmt(v) {
  if (v === null || v === undefined || v === '') return '—'
  if (typeof v === 'string') return v
  try { return JSON.stringify(v, null, 2) } catch (e) { return String(v) }
}

function isAbnormal(sp) {
  return !!(sp.abnormal || sp.status === 'abnormal' || sp.error)
}

function statusType(sp) {
  if (sp.abnormal || sp.status === 'abnormal' || sp.error) return 'danger'
  if (sp.status === 'failed') return 'warning'
  return 'success'
}

function statusText(sp) {
  if (sp.abnormal || sp.status === 'abnormal' || sp.error) return '异常'
  if (sp.status === 'failed') return '失败'
  return '正常'
}

function toggle(sp) {
  if (!sp || sp.id === undefined) return
  expanded[sp.id] = !expanded[sp.id]
}

const flattened = computed(() => {
  const t = tree.value
  if (!t || !t.spans) return []
  const out = []
  for (const sp of t.spans) {
    if (sp.span_kind !== 'lifecycle') continue
    out.push(sp)
    if (sp.node === 'ai_dispatch') {
      const turns = [...new Set(
        t.spans.filter(s => s.span_kind === 'agent_turn' && s.parent_node === 'ai_dispatch')
          .map(s => s.turn_index)
      )].sort((a, b) => a - b)
      for (const ti of turns) {
        const turn = t.spans.find(s => s.span_kind === 'agent_turn' && s.parent_node === 'ai_dispatch' && s.turn_index === ti)
        if (!turn) continue
        out.push({ _turn: true, ti, turn })
        const tools = t.spans
          .filter(s => s.span_kind === 'tool_call' && s.parent_node === 'ai_dispatch' && s.turn_index === ti)
          .sort((a, b) => (a.id || 0) - (b.id || 0))
        for (const tool of tools) out.push({ _tool: true, tool })
      }
    }
  }
  return out
});

const healthStats = computed(() => {
  const h = health.value
  if (!h) return []
  return [
    { label: '入站速率', value: h.inbound_rate_per_min, unit: '条/分' },
    { label: '出站速率', value: h.outbound_rate_per_min, unit: '条/分' },
    { label: '待发送', value: h.pending_count, unit: '条', cls: h.pending_count > 0 ? 'warn' : '' },
    { label: '最早待发滞留', value: h.oldest_pending_min, unit: '分', cls: h.oldest_pending_min > 10 ? 'warn' : '' },
    { label: '已送达', value: h.delivered_count, unit: '条' },
    { label: '失败', value: h.failed_count, unit: '条', cls: h.failed_count > 0 ? 'err' : '' },
    { label: '数据缺口', value: h.sync_gap_count, unit: '个', cls: h.sync_gap_count > 0 ? 'warn' : '' },
    { label: '卡住(可达)', value: h.stuck_reachable, unit: '个', cls: (h.stuck_reachable || 0) > 0 ? 'err' : '' },
    { label: '卡住(不可达)', value: h.stuck_unreachable, unit: '个', cls: (h.stuck_unreachable || 0) > 0 ? 'err' : '' },
    { label: '异常节点', value: h.abnormal_count, unit: '个', cls: h.abnormal_count > 0 ? 'warn' : '' }
  ]
})

function resetForm() {
  form.trace_id = ''
  form.conversation_id = ''
  form.msg_id = ''
  form.channel = ''
  tree.value = null
  treeError.value = ''
}

async function loadTree() {
  const params = {}
  if (form.trace_id) params.trace_id = form.trace_id
  if (form.conversation_id) params.conversation_id = form.conversation_id
  if (form.msg_id) params.msg_id = form.msg_id
  if (form.channel) params.channel = form.channel
  loadingTree.value = true
  treeError.value = ''
  try {
    const res = await monitorApi.traceTree(params)
    tree.value = res || null
    if (!tree.value || !tree.value.spans || !tree.value.spans.length) {
      treeError.value = '未找到链路数据'
    } else {
      const firstAbn = tree.value.spans.find(s => isAbnormal(s));
      if (firstAbn) expanded[firstAbn.id] = true
    }
  } catch (e) {
    treeError.value = (e && e.message) || '查询失败'
  } finally {
    loadingTree.value = false
  }
}

async function loadLifecycle() {
  const params = {}
  if (lcForm.conversation_id) params.conversation_id = lcForm.conversation_id
  if (lcForm.trace_id) params.trace_id = lcForm.trace_id
  if (!params.conversation_id && !params.trace_id) {
    ElMessage.warning('请填写会话 ID 或 Trace ID')
    return
  }
  loadingLc.value = true
  try {
    lifecycleRows.value = await monitorApi.lifecycle(params)
  } catch (e) {
    ElMessage.error((e && e.message) || '查询失败')
  } finally {
    loadingLc.value = false
  }
}

function openTrace(row) {
  form.conversation_id = row.conversation_id || ''
  form.trace_id = row.trace_id || ''
  form.msg_id = ''
  form.channel = ''
  activeTab.value = 'trace'
  loadTree()
}

async function loadHealth() {
  try { health.value = await monitorApi.health() } catch (e) { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
}
async function loadNodeHealth() {
  try {
    const res = await monitorApi.nodeHealth()
    nodeRows.value = res.nodes || []
    nodeWindow.value = res.window || ''
  } catch (e) { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
}
async function loadLatency() {
  try { latencyRows.value = await monitorApi.latency() } catch (e) { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
}
async function loadTraces() {
  try { traceRows.value = await monitorApi.traces({ limit: 50 }) } catch (e) { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
}
async function loadAnomalies() {
  try { anomaly.value = await monitorApi.anomalies() } catch (e) { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
}

function parseJSON(v, fallback) {
  if (v === null || v === undefined || v === '') return fallback
  if (typeof v === 'object') return v
  try { return JSON.parse(v) } catch (e) { return fallback }
}
function dimText(dims) {
  const d = parseJSON(dims, {})
  const order = [
    ['relevance', '相关性'],
    ['accuracy', '准确'],
    ['usefulness', '有用'],
    ['safety', '合规']
  ]
  return order.map(([k, label]) => `${label} ${d[k] != null ? d[k] : '—'}`).join(' · ')
}
function adjustedList(v) {
  const a = parseJSON(v, [])
  return Array.isArray(a) ? a : []
}
function scoreType(s) {
  if (s < 60) return 'danger'
  if (s >= 85) return 'success'
  return 'warning'
}
async function loadEvalLogs() {
  try { evalLogs.value = await monitorApi.evalLogs({ limit: 50 }) } catch (e) { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
}
async function loadWeights() {
  try { weights.value = await monitorApi.knowledgeWeights({ limit: 50 }) } catch (e) { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
}
async function refreshLearn() {
  await Promise.all([loadEvalLogs(), loadWeights()])
}
async function triggerEval() {
  triggerLoading.value = true
  triggerMsg.value = ''
  try {
    const res = await monitorApi.triggerEval({ hours: evalHours.value, limit: evalLimit.value })
    const processed = (res && res.processed) || 0
    triggerMsgType.value = 'success'
    triggerMsg.value = `已处理 ${processed} 条 trace（评分 + 调整知识库权重），详见下方记录`
    await refreshLearn()
  } catch (e) {
    triggerMsgType.value = 'error'
    triggerMsg.value = (e && e.message) || '触发评估失败'
  } finally {
    triggerLoading.value = false
  }
}

watch(activeTab, (tab) => {
  if (tab === 'health' && !health.value) loadHealth()
  if (tab === 'node' && !nodeRows.value.length) loadNodeHealth()
  if (tab === 'latency' && !latencyRows.value.length) loadLatency()
  if (tab === 'traces' && !traceRows.value.length) loadTraces()
  if (tab === 'anomalies' && !anomaly.value) loadAnomalies()
  if (tab === 'learn' && !learningLoaded.value) {
    learningLoaded.value = true
    refreshLearn()
  }
});
</script>

<style scoped>
.trace-monitor { padding: 8px; }
.mb-12 { margin-bottom: 12px; }
.mt-12 { margin-top: 12px; }
.trace-query { background: #f7f8fa; padding: 12px 12px 0; border-radius: 6px; }
.trace-summary { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.trace-count { margin-left: auto; color: #909399; font-size: 13px; }

.span-card { margin-bottom: 10px; border-radius: 8px; }
.span-card.abnormal { border-color: #f56c6c; }
.span-card.agent { border-left: 4px solid #409eff; }
.turn-card { margin-left: 28px; border-left: 4px solid #e6a23c; }
.tool-card { margin-left: 56px; border-left: 4px solid #67c23a; background: #fafcfa; }
.span-head { display: flex; align-items: center; gap: 10px; cursor: pointer; flex-wrap: wrap; }
.span-head .caret { width: 14px; color: #909399; }
.span-kind { font-size: 12px; padding: 1px 6px; border-radius: 4px; color: #fff; }
.kind-lifecycle { background: #409eff; }
.kind-turn { background: #e6a23c; }
.kind-tool { background: #67c23a; }
.span-title { font-weight: 600; }
.span-node { color: #909399; font-size: 12px; font-family: monospace; }
.span-dur { color: #606266; font-size: 13px; }
.span-time { color: #c0c4cc; font-size: 12px; }
.span-body { margin-top: 12px; }
.code-block {
  margin: 0;
  max-height: 280px;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-all;
  font-family: monospace;
  font-size: 12px;
  background: #f5f7fa;
  padding: 8px;
  border-radius: 4px;
}
.err-text { color: #f56c6c; }

.stat-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(180px, 1fr)); gap: 12px; }
.stat-card { text-align: center; }
.stat-label { color: #909399; font-size: 13px; }
.stat-value { font-size: 26px; font-weight: 700; margin: 6px 0; }
.stat-value.warn { color: #e6a23c; }
.stat-value.err { color: #f56c6c; }
.stat-unit { color: #c0c4cc; font-size: 12px; }
.muted { color: #c0c4cc; }
.learn-toolbar { background: #f7f8fa; padding: 12px 12px 0; border-radius: 6px; }
</style>
