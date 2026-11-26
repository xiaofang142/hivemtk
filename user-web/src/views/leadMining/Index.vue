<template>
  <div class="lead-mining-page">
    <el-card
      shadow="never"
      class="mb16"
    >
      <div class="page-header">
        <div>
          <h2 class="title">
            线索发掘工作台
          </h2>
          <div class="sub">
            账号级配置、判定记录、质量看板与发掘线索统一管理；规则 / 智能体 / 通知路由见「线索配置」
          </div>
        </div>
        <div class="status-tags">
          <el-tag :type="status.running ? 'success' : 'info'">
            {{ status.running ? '运行中' : '未初始化' }}
          </el-tag>
          <el-tag type="info">
            队列 {{ status.queue_depth ?? 0 }}
          </el-tag>
          <el-tag type="info">
            近期处理 {{ status.recent_processed ?? 0 }}
          </el-tag>
          <el-button
            size="small"
            @click="$router.push('/leads/mining-agents')"
          >
            线索配置
          </el-button>
          <el-button
            size="small"
            @click="refreshAll"
          >
            刷新
          </el-button>
        </div>
      </div>
    </el-card>

    <el-tabs v-model="activeTab">
      <el-tab-pane
        label="账号级配置"
        name="matrix"
      >
        <AccountMatrix />
      </el-tab-pane>
      <el-tab-pane
        label="通知路由"
        name="routes"
      >
        <!-- T02：同一张 lead_mining_notify_routes 表不再有两套 CRUD。
             这里的 tab 只做入口，真实配置统一在「线索配置 / 线索通知路由」。 -->
        <el-card
          shadow="never"
          class="routes-entry-card"
        >
          <div class="page-header">
            <div>
              <h3 class="title">
                线索通知路由
              </h3>
              <div class="sub">
                账号 / 渠道 / 租户三级优先级的唯一配置入口；工作台不再重复维护同一份路由事实。
              </div>
            </div>
            <el-button
              type="primary"
              @click="$router.push('/leads/notify-routes')"
            >
              前往配置
            </el-button>
          </div>
        </el-card>
      </el-tab-pane>

      <el-tab-pane
        label="判定记录"
        name="judgements"
      >
        <el-card shadow="never">
          <div class="toolbar">
            <ChannelSelect
              v-model="judgementFilter.platform"
              placeholder="平台"
              style="width: 130px"
            />
            <ChannelAccountSelect
              v-model="judgementFilter.account_id"
              :channel="judgementFilter.platform"
              placeholder="账号ID"
              style="width: 160px"
            />
            <el-select
              v-model="judgementFilter.lead_role"
              placeholder="角色"
              clearable
              style="width: 130px"
            >
              <el-option
                v-for="r in leadRoleOptions"
                :key="r.value"
                :label="r.label"
                :value="r.value"
              />
            </el-select>
            <el-select
              v-model="judgementFilter.matched"
              placeholder="命中"
              clearable
              style="width: 110px"
            >
              <el-option
                label="命中"
                value="true"
              />
              <el-option
                label="未命中"
                value="false"
              />
            </el-select>
            <el-button
              type="primary"
              @click="loadJudgements(1)"
            >
              查询
            </el-button>
          </div>
          <el-table
            v-loading="judgementsLoading"
            :data="judgements"
            stripe
            style="margin-top: 12px"
          >
            <el-table-column
              prop="created_at"
              label="时间"
              width="170"
            >
              <template #default="{ row }">
                {{ formatTime(row.created_at) }}
              </template>
            </el-table-column>
            <el-table-column
              prop="platform"
              label="平台"
              width="100"
            >
              <template #default="{ row }">
                {{ catalog.channelLabel(row.platform) || '-' }}
              </template>
            </el-table-column>
            <el-table-column
              label="账号"
              min-width="140"
              show-overflow-tooltip
            >
              <template #default="{ row }">
                {{ catalog.accountLabel(row.account_id) || '-' }}
              </template>
            </el-table-column>
            <el-table-column
              label="会话"
              width="90"
            >
              <template #default="{ row }">
                <el-tag
                  size="small"
                  :type="row.is_group ? 'warning' : 'info'"
                >
                  {{ row.is_group ? '群聊' : '单聊' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              prop="sender_id"
              label="发送者"
              min-width="120"
              show-overflow-tooltip
            />
            <el-table-column
              label="命中"
              width="80"
            >
              <template #default="{ row }">
                <el-tag
                  size="small"
                  :type="row.matched ? 'success' : 'info'"
                >
                  {{ row.matched ? '是' : '否' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              label="角色"
              width="100"
            >
              <template #default="{ row }">
                <el-tag
                  size="small"
                  :type="leadRoleTag(row.lead_role)"
                >
                  {{ leadRoleLabel(row.lead_role) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              prop="intent_score"
              label="意向分"
              width="80"
            />
            <el-table-column
              prop="reason"
              label="原因/摘要"
              min-width="220"
              show-overflow-tooltip
            />
            <el-table-column
              label="反馈"
              width="110"
            >
              <template #default="{ row }">
                <el-tag
                  size="small"
                  :type="feedbackTag(row.feedback_status)"
                >
                  {{ feedbackLabel(row.feedback_status) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              label="操作"
              width="220"
              fixed="right"
            >
              <template #default="{ row }">
                <el-button
                  link
                  type="primary"
                  size="small"
                  @click="openJudgementDetail(row)"
                >
                  回放
                </el-button>
                <el-button
                  link
                  type="danger"
                  size="small"
                  @click="submitJudgementFb(row, 'false_positive')"
                >
                  误报
                </el-button>
                <el-button
                  link
                  type="success"
                  size="small"
                  @click="submitJudgementFb(row, 'valid')"
                >
                  有效
                </el-button>
              </template>
            </el-table-column>
          </el-table>
          <el-pagination
            v-if="judgementTotal > judgementLimit"
            layout="total, prev, pager, next"
            :total="judgementTotal"
            :page-size="judgementLimit"
            :current-page="judgementPage"
            style="margin-top: 12px"
            @current-change="loadJudgements"
          />
        </el-card>
      </el-tab-pane>

      <el-tab-pane
        label="质量看板"
        name="quality"
      >
        <el-card shadow="never">
          <div class="toolbar">
            <el-select
              v-model="qualityDays"
              style="width: 140px"
              @change="loadQualityStats"
            >
              <el-option
                :value="7"
                label="近7天趋势"
              />
              <el-option
                :value="14"
                label="近14天趋势"
              />
              <el-option
                :value="30"
                label="近30天趋势"
              />
            </el-select>
            <el-button @click="loadQualityStats">
              刷新
            </el-button>
          </div>
          <div
            class="stat-cards"
            style="margin-top:12px"
          >
            <el-statistic
              title="判定总数"
              :value="quality.total || 0"
            />
            <el-statistic
              title="命中数"
              :value="quality.matched_total || 0"
            />
            <el-statistic
              title="误报反馈"
              :value="quality.false_positive || 0"
            />
            <el-statistic
              title="有效反馈"
              :value="quality.valid_feedback || 0"
            />
          </div>
          <el-row
            :gutter="12"
            style="margin-top:16px"
          >
            <el-col :span="12">
              <h4>角色分布</h4>
              <el-table
                :data="quality.by_role || []"
                size="small"
                border
              >
                <el-table-column
                  prop="lead_role"
                  label="角色"
                />
                <el-table-column
                  prop="total"
                  label="总数"
                  width="90"
                />
                <el-table-column
                  prop="matched_total"
                  label="命中"
                  width="90"
                />
              </el-table>
            </el-col>
            <el-col :span="12">
              <h4>渠道分布</h4>
              <el-table
                :data="quality.by_channel || []"
                size="small"
                border
              >
                <el-table-column
                  label="渠道"
                >
                  <template #default="{ row }">
                    {{ catalog.channelLabel(row.platform) || '-' }}
                  </template>
                </el-table-column>
                <el-table-column
                  prop="total"
                  label="总数"
                  width="90"
                />
                <el-table-column
                  prop="matched_total"
                  label="命中"
                  width="90"
                />
              </el-table>
            </el-col>
          </el-row>
          <h4 style="margin-top:16px">
            近期趋势
          </h4>
          <el-table
            :data="quality.trend || []"
            size="small"
            border
          >
            <el-table-column
              prop="day"
              label="日期"
            />
            <el-table-column
              prop="total"
              label="总数"
              width="90"
            />
            <el-table-column
              prop="matched_total"
              label="命中"
              width="90"
            />
            <el-table-column
              prop="false_positive_total"
              label="误报"
              width="90"
            />
          </el-table>
        </el-card>
      </el-tab-pane>

      <el-tab-pane
        label="发掘线索"
        name="leads"
      >
        <el-card shadow="never">
          <div class="toolbar">
            <el-button @click="$router.push('/clue/list')">
              线索库（全量管理）
            </el-button>
            <el-input
              v-model="leadFilter.keyword"
              placeholder="搜索名称/账号/城市"
              clearable
              style="width: 220px"
              @keyup.enter="loadLeads(1)"
            />
            <el-select
              v-model="leadFilter.lead_role"
              placeholder="角色"
              clearable
              style="width: 120px"
            >
              <el-option
                v-for="r in leadRoleOptions"
                :key="r.value"
                :label="r.label"
                :value="r.value"
              />
            </el-select>
            <el-select
              v-model="leadFilter.status"
              placeholder="状态"
              clearable
              style="width: 120px"
            >
              <el-option
                v-for="s in statusOptions"
                :key="s"
                :label="s"
                :value="s"
              />
            </el-select>
            <ChannelSelect
              v-model="leadFilter.source_channel"
              placeholder="渠道"
              style="width: 120px"
            />
            <el-select
              v-model="leadFilter.is_group"
              placeholder="会话类型"
              clearable
              style="width: 110px"
            >
              <el-option
                label="群聊"
                :value="true"
              />
              <el-option
                label="单聊"
                :value="false"
              />
            </el-select>
            <el-button
              type="primary"
              @click="loadLeads(1)"
            >
              查询
            </el-button>
            <el-button @click="resetLeadFilter">
              重置
            </el-button>
          </div>
          <div
            v-if="leadStats.total !== undefined"
            class="stats-inline"
          >
            <el-tag type="info">
              总数 {{ leadStats.total }}
            </el-tag>
            <el-tag
              v-for="r in (leadStats.by_lead_role || [])"
              :key="r.lead_role"
              :type="leadRoleTag(r.lead_role)"
            >
              {{ leadRoleLabel(r.lead_role) }} {{ r.total }}
            </el-tag>
          </div>
          <el-table
            v-loading="leadsLoading"
            :data="leads"
            stripe
            style="margin-top: 12px"
          >
            <el-table-column
              prop="name"
              label="名称"
              min-width="140"
              show-overflow-tooltip
            />
            <el-table-column
              label="角色"
              width="100"
            >
              <template #default="{ row }">
                <el-tag
                  size="small"
                  :type="leadRoleTag(row.lead_role)"
                >
                  {{ leadRoleLabel(row.lead_role) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              prop="status"
              label="状态"
              width="100"
            >
              <template #default="{ row }">
                <el-tag
                  size="small"
                  type="info"
                >
                  {{ row.status || '-' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              label="渠道/账号"
              min-width="160"
              show-overflow-tooltip
            >
              <template #default="{ row }">
                <div>{{ catalog.channelLabel(row.source_channel) || '-' }}</div>
                <div class="muted">
                  {{ catalog.accountLabel(row.source_account_id || row.account) || '-' }}
                </div>
              </template>
            </el-table-column>
            <el-table-column
              label="会话"
              width="90"
            >
              <template #default="{ row }">
                <el-tag
                  size="small"
                  :type="row.is_group ? 'warning' : 'info'"
                >
                  {{ row.is_group ? '群聊' : '单聊' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              prop="summary"
              label="摘要"
              min-width="240"
              show-overflow-tooltip
            >
              <template #default="{ row }">
                {{ row.summary || row.desc || '-' }}
              </template>
            </el-table-column>
            <el-table-column
              label="意向分"
              width="90"
            >
              <template #default="{ row }">
                <el-tag :type="row.intent_score >= 70 ? 'danger' : (row.intent_score >= 40 ? 'warning' : 'info')">
                  {{ row.intent_score }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              prop="owner_account"
              label="维护人"
              min-width="110"
              show-overflow-tooltip
            />
            <el-table-column
              label="发现时间"
              width="170"
            >
              <template #default="{ row }">
                {{ formatTime(row.create_time) }}
              </template>
            </el-table-column>
          </el-table>
          <el-pagination
            v-if="leadTotal > leadLimit"
            layout="total, prev, pager, next"
            :total="leadTotal"
            :page-size="leadLimit"
            :current-page="leadPage"
            style="margin-top: 12px"
            @current-change="loadLeads"
          />
        </el-card>
      </el-tab-pane>
    </el-tabs>

    <el-drawer
      v-model="judgementDetailVisible"
      title="判定回放"
      size="52%"
    >
      <el-descriptions
        v-if="judgementDetail"
        :column="1"
        border
        size="small"
      >
        <el-descriptions-item label="Judgement ID">
          {{ judgementDetail.judgement_id }}
        </el-descriptions-item>
        <el-descriptions-item label="时间">
          {{ formatTime(judgementDetail.created_at) }}
        </el-descriptions-item>
        <el-descriptions-item label="平台/账号">
          {{ catalog.channelLabel(judgementDetail.platform) || '-' }} / {{ catalog.accountLabel(judgementDetail.account_id) || '-' }}
        </el-descriptions-item>
        <el-descriptions-item label="会话">
          {{ judgementDetail.is_group ? '群聊' : '单聊' }} {{ judgementDetail.conversation_id }}
        </el-descriptions-item>
        <el-descriptions-item label="发送者">
          {{ judgementDetail.sender_id }}
        </el-descriptions-item>
        <el-descriptions-item label="命中/角色/分数">
          {{ judgementDetail.matched ? '命中' : '未命中' }} / {{ leadRoleLabel(judgementDetail.lead_role) }} / {{ judgementDetail.intent_score }}
        </el-descriptions-item>
        <el-descriptions-item label="反馈">
          {{ feedbackLabel(judgementDetail.feedback_status) }} {{ judgementDetail.feedback_reason ? ' - ' + judgementDetail.feedback_reason : '' }}
        </el-descriptions-item>
        <el-descriptions-item label="原因">
          {{ judgementDetail.reason }}
        </el-descriptions-item>
      </el-descriptions>
      <h4 style="margin-top:12px">
        Context JSON
      </h4>
      <el-input
        type="textarea"
        :rows="8"
        :model-value="prettyJson(judgementDetail?.context_json)"
        readonly
      />
      <h4 style="margin-top:12px">
        Result JSON
      </h4>
      <el-input
        type="textarea"
        :rows="10"
        :model-value="prettyJson(judgementDetail?.result_json)"
        readonly
      />
      <template #footer>
        <el-button
          type="danger"
          plain
          @click="submitJudgementFb(judgementDetail, 'false_positive')"
        >
          标记误报
        </el-button>
        <el-button
          type="success"
          plain
          @click="submitJudgementFb(judgementDetail, 'valid')"
        >
          标记有效
        </el-button>
      </template>
    </el-drawer>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  getLeadMiningStatus,
  getLeadMiningJudgements, getLeadMiningClueStats, getLeadMiningJudgementDetail,
  submitLeadMiningJudgementFeedback, getLeadMiningQualityStats
} from '@/api/leadMining'
import { clueApi } from '@/api/clue'
import AccountMatrix from './AccountMatrix.vue'
import ChannelSelect from '@/components/ChannelSelect.vue'
import ChannelAccountSelect from '@/components/ChannelAccountSelect.vue'
import { useChannelCatalogStore } from '@/stores/channelCatalog'

const catalog = useChannelCatalogStore()

const activeTab = ref('matrix')
const status = ref({})

const leadRoleOptions = [
  { value: 'demand', label: '需求方' },
  { value: 'supply', label: '供货方' },
  { value: 'resource', label: '资源方/上下游' },
  { value: 'broker', label: '中介' },
  { value: 'unknown', label: '未知' }
]
const statusOptions = ['new', 'notified', 'following', 'converted', 'ignored', 'invalid']

function unwrap(res) { return res?.data?.data ?? res?.data ?? res ?? {} }
function leadRoleLabel(v) { return leadRoleOptions.find(r => r.value === v)?.label || v || '未知' }
function leadRoleTag(v) { return ({ demand: 'danger', supply: 'success', resource: 'warning', broker: '', unknown: 'info' })[v] || 'info' }
function formatTime(t) {
  if (!t) return '-'
  const d = new Date(typeof t === 'number' && t < 1e12 ? t * 1000 : t)
  if (isNaN(d.getTime())) return String(t)
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth()+1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

async function loadStatus() {
  try { status.value = unwrap(await getLeadMiningStatus()) } catch { status.value = {} }
}

// T02：通知路由的读写入口只在 /leads/notify-routes（线索配置分组），
// 本页 tab 仅保留跳转，避免同一张表出现两个可写 UI。

// judgements
const judgements = ref([])
const judgementsLoading = ref(false)
const judgementTotal = ref(0)
const judgementPage = ref(1)
const judgementLimit = ref(20)
const judgementFilter = reactive({ platform: '', account_id: '', lead_role: '', matched: '' })
const judgementDetailVisible = ref(false)
const judgementDetail = ref(null)
const quality = ref({})
const qualityDays = ref(7)

async function loadJudgements(p = 1) {
  judgementPage.value = p
  judgementsLoading.value = true
  try {
    const res = await getLeadMiningJudgements({ page: judgementPage.value, limit: judgementLimit.value, ...judgementFilter })
    const data = unwrap(res)
    judgements.value = data.list || []
    judgementTotal.value = data.total || 0
  } catch (e) { ElMessage.error('加载判定记录失败：' + (e.message || e)) }
  finally { judgementsLoading.value = false }
}

async function openJudgementDetail(row) {
  if (!row?.judgement_id) return
  try {
    const res = await getLeadMiningJudgementDetail(row.judgement_id)
    judgementDetail.value = unwrap(res)
    judgementDetailVisible.value = true
  } catch (e) { ElMessage.error('加载判定详情失败：' + (e.message || e)) }
}

async function submitJudgementFb(row, status) {
  if (!row?.judgement_id) return
  try {
    let reason = row.feedback_reason || ''
    if (status === 'false_positive') {
      const prompt = await ElMessageBox.prompt('可选填写误报原因', '标记误报', {
        confirmButtonText: '提交', cancelButtonText: '取消', inputValue: reason, inputPlaceholder: '例如：关键词命中但非采购意向'
      }).catch(() => null)
      if (!prompt) return
      reason = prompt.value || ''
    }
    await submitLeadMiningJudgementFeedback(row.judgement_id, { feedback_status: status, feedback_reason: reason })
    ElMessage.success(status === 'false_positive' ? '已标记误报' : '已标记有效')
    await loadJudgements(judgementPage.value)
    await loadQualityStats()
    if (judgementDetail.value?.judgement_id === row.judgement_id) {
      const detail = await getLeadMiningJudgementDetail(row.judgement_id)
      judgementDetail.value = unwrap(detail)
    }
  } catch (e) { if (e !== 'cancel') ElMessage.error('提交反馈失败：' + (e.message || e)) }
}

async function loadQualityStats() {
  try {
    const res = await getLeadMiningQualityStats(qualityDays.value)
    quality.value = unwrap(res) || {}
  } catch (e) { ElMessage.error('加载质量看板失败：' + (e.message || e)) }
}

function feedbackLabel(status) {
  if (status === 'false_positive') return '误报'
  if (status === 'valid') return '有效'
  return '未反馈'
}
function feedbackTag(status) {
  if (status === 'false_positive') return 'danger'
  if (status === 'valid') return 'success'
  return 'info'
}
function prettyJson(v) {
  if (!v) return ''
  if (typeof v === 'string') {
    try { return JSON.stringify(JSON.parse(v), null, 2) } catch { return v }
  }
  try { return JSON.stringify(v, null, 2) } catch { return String(v) }
}

// leads
const leads = ref([])
const leadsLoading = ref(false)
const leadTotal = ref(0)
const leadPage = ref(1)
const leadLimit = ref(20)
const leadFilter = reactive({ keyword: '', lead_role: '', status: '', source_channel: '', is_group: '' })
const leadStats = ref({})

async function loadLeads(p = 1) {
  leadPage.value = p
  leadsLoading.value = true
  try {
    const filters = {}
    Object.entries(leadFilter).forEach(([k, v]) => { if (v !== '' && v !== null && v !== undefined) filters[k] = v })
    const [listRes, statsRes] = await Promise.all([
      clueApi.list(leadPage.value, leadLimit.value, filters),
      getLeadMiningClueStats().catch(() => ({}))
    ])
    const data = listRes?.data ?? listRes
    const payload = data?.data ?? data
    leads.value = payload?.list ?? (Array.isArray(payload) ? payload : [])
    leadTotal.value = payload?.total ?? leads.value.length
    leadStats.value = unwrap(statsRes)
  } catch (e) { ElMessage.error('加载线索失败：' + (e.message || e)) }
  finally { leadsLoading.value = false }
}

function resetLeadFilter() {
  Object.assign(leadFilter, { keyword: '', lead_role: '', status: '', source_channel: '', is_group: '' })
  loadLeads(1)
}

function refreshAll() {
  loadStatus(); loadJudgements(judgementPage.value); loadQualityStats(); loadLeads(leadPage.value)
}

onMounted(() => {
  catalog.ensure()
  refreshAll()
})
</script>

<style scoped>
.lead-mining-page { padding: 16px; }
.mb16 { margin-bottom: 16px; }
.page-header { display: flex; justify-content: space-between; align-items: center; gap: 12px; flex-wrap: wrap; }
.title { margin: 0; font-size: 20px; }
.sub { color: #909399; font-size: 13px; margin-top: 4px; }
.status-tags { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
.toolbar { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
.stats-inline { display: flex; gap: 8px; margin-top: 12px; flex-wrap: wrap; }
.muted { color: #909399; font-size: 12px; }
</style>
