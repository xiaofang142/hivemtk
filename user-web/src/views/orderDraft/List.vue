<template>
  <div class="order-draft">
    <el-alert
      v-if="unavailable"
      class="mb-12"
      type="warning"
      show-icon
      :closable="false"
      :title="t('businessCopy.orderDraft.unavailableTitle')"
      :description="t('businessCopy.orderDraft.unavailableDescription')"
    />
    <el-alert
      v-if="loadError"
      class="mb-12"
      type="error"
      show-icon
      :closable="false"
      title="草稿列表读取失败"
      :description="loadError"
    />
    <el-alert
      v-if="!me"
      class="mb-12"
      type="info"
      show-icon
      :closable="false"
      :title="t('businessCopy.identity.title')"
      :description="t('businessCopy.identity.orderDraftDescription')"
    />

    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <div class="header-left">
            <span>订单草稿</span>
            <el-tag
              v-if="stats.mode"
              size="small"
              type="info"
            >
              {{ t(serviceStatusKey) }}
            </el-tag>
          </div>
          <el-button
            :loading="loading"
            @click="refresh"
          >
            刷新
          </el-button>
        </div>
      </template>

      <!-- counts=null 与 counts={} 是两句话：前者「本进程压根没读」（旗子关着），后者「读了且真没有」。
           合并成一个 0 显示等于拿一次装配缺失去支撑「今天没有草稿」。 -->
      <div
        v-if="stats.assembled"
        class="counts"
      >
        <span
          v-for="(n, k) in stats.counts"
          :key="k"
          class="count-chip"
        >
          {{ statusLabel(k) }} <b>{{ n }}</b>
        </span>
        <span class="counts-at">待确认池视图不含已确认/已取消/已过期（要看客户全历史，进详情页）</span>
      </div>
      <div
        v-else-if="stats.assembled === false"
        class="counts"
      >
        <span class="count-chip">{{ t('businessCopy.orderDraft.countsUnavailable') }}</span>
      </div>
      <el-alert
        v-if="stats.warning"
        class="mb-12"
        type="warning"
        show-icon
        :closable="false"
        :title="t('businessCopy.orderDraft.serviceWarning')"
        :description="['memory', 'shadow'].includes(stats.store) ? t('businessCopy.orderDraft.temporaryStorageWarning') : undefined"
      />

      <el-form
        inline
        class="mb-12"
      >
        <el-form-item label="归属">
          <el-select
            v-model="filter.owner"
            class="filter-select"
            @change="reload"
          >
            <el-option
              label="全部待确认（池子）"
              value="pool"
            />
            <el-option
              label="我名下"
              value="me"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="条数">
          <el-select
            v-model="filter.limit"
            class="filter-select"
            @change="reload"
          >
            <el-option
              v-for="n in [50, 100, 200]"
              :key="n"
              :label="`${n} 条`"
              :value="n"
            />
          </el-select>
        </el-form-item>
      </el-form>

      <el-table
        v-loading="loading"
        :data="list"
        stripe
      >
        <el-table-column
          label="产品"
          min-width="200"
        >
          <template #default="{ row }">
            <el-button
              link
              type="primary"
              @click="openDetail(row)"
            >
              {{ row.product_name }}
            </el-button>
            <el-tag
              v-if="row.category"
              size="small"
              type="info"
              class="ml-6"
            >
              {{ row.category }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="客户"
          width="140"
        >
          <template #default="{ row }">
            <EntityLabel
              entity="customer"
              :value="row.customer_id"
            />
          </template>
        </el-table-column>
        <el-table-column
          label="数量"
          width="70"
          prop="quantity"
        />
        <el-table-column
          label="单价"
          width="100"
        >
          <template #default="{ row }">
            {{ money(row.unit_price) }}
          </template>
        </el-table-column>
        <el-table-column
          label="总额"
          width="110"
        >
          <template #default="{ row }">
            <b>{{ money(row.total_amount) }}</b>
          </template>
        </el-table-column>
        <el-table-column
          label="置信度"
          width="80"
        >
          <template #default="{ row }">
            {{ pct(row.confidence) }}
          </template>
        </el-table-column>
        <el-table-column
          label="状态"
          width="90"
        >
          <template #default="{ row }">
            <el-tag
              size="small"
              :type="statusTag(row.status)"
            >
              {{ statusLabel(row.status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="来源"
          width="90"
          prop="source"
        />
        <el-table-column
          label="到期"
          width="170"
        >
          <template #default="{ row }">
            <span :class="{ overdue: isExpiredSoon(row) }">{{ formatTime(row.expires_at) }}</span>
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="200"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button
              link
              type="primary"
              @click="openDetail(row)"
            >
              详情
            </el-button>
            <template v-if="row.status === 'pending'">
              <el-button
                link
                type="success"
                :disabled="!me"
                :loading="actingId === row.id"
                @click="confirmDraft(row)"
              >
                确认成单
              </el-button>
              <el-button
                link
                type="danger"
                :disabled="!me"
                @click="cancelDraft(row)"
              >
                取消
              </el-button>
            </template>
          </template>
        </el-table-column>
      </el-table>

      <p
        v-if="truncated"
        class="hint"
      >
        {{ t('businessCopy.orderDraft.listLimitHint') }}
      </p>
    </el-card>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { orderDraftApi } from '@/api/orderDraft'
import EntityLabel from '@/components/EntityLabel.vue'
import { useUserStore } from '@/stores/user'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

// AI 成单草稿的人工入口（A1 的前端半）。
//
// 这一页只管「池子里现在有哪些单等着人处理」（后端 pool_pending 视图，缺省不分人）：
// AI 建的草稿 owner_id 是 "system"，按人过滤会把整批从每个人列表里抹掉 ——
// 「看自己那份」是销售工作台聚合待办的职责，两边不互相替代。
//
// 写动作的成功判据在响应里：确认回 order_id + order_provisional（临时号必须当告警读，
// 不能当真订单号），取消/编辑回整条新草稿（含重算的 total_amount / cancel_reason）——
// 本地行直接用返回值替换，不重新拉列表（少一次读，且不会被别人的并发改动冲掉）。

const router = useRouter()
const userStore = useUserStore()
// 后端 operator 存的是 user id 十进制字符串；统一成字符串，
// 否则 userInfo.id 是数字时「有没有身份」的判断会出岔子。
const me = computed(() => String(userStore.userInfo?.id ?? ''))

const loading = ref(false)
const actingId = ref('')
const list = ref([])
const truncated = ref(false)
const unavailable = ref(false)
const loadError = ref('')
const stats = reactive({ mode: '', store: '', assembled: undefined, counts: null, warning: '' })
const filter = reactive({ owner: 'pool', limit: 50 })
const serviceStatusKey = computed(() => {
  if (unavailable.value || stats.assembled === false) return 'businessCopy.orderDraft.unavailableTitle'
  if (loadError.value || stats.warning) return 'businessCopy.orderDraft.serviceNeedsAttention'
  if (['memory', 'shadow'].includes(stats.store)) return 'businessCopy.orderDraft.temporaryStorage'
  return 'businessCopy.orderDraft.serviceEnabled'
})

const STATUS_TAGS = { pending: 'warning', confirmed: 'success', cancelled: 'info', expired: 'danger' }
const STATUS_LABELS = { pending: '待确认', confirmed: '已确认', cancelled: '已取消', expired: '已过期' }

const statusTag = (s) => STATUS_TAGS[s] || 'info'
const statusLabel = (s) => STATUS_LABELS[s] || s

function money(v) {
  const n = Number(v)
  return Number.isFinite(n) ? n.toFixed(2) : '—'
}

function pct(v) {
  const n = Number(v)
  return Number.isFinite(n) ? `${Math.round(n * 1000) / 10}%` : '—'
}

function formatTime(value) {
  if (!value) return '—'
  const d = new Date(value)
  return Number.isNaN(d.getTime()) ? String(value) : d.toLocaleString()
}

// 到期列标红只看「已过期或一小时内过期」：草稿过期后清扫器会收口，
// 显示成普通时间会让「这条马上要被扫掉」看不出来。
function isExpiredSoon(row) {
  const t = new Date(row.expires_at).getTime()
  if (Number.isNaN(t)) return false
  return t - Date.now() < 3600 * 1000
}

function queryParams() {
  const q = { limit: filter.limit }
  // 'pool' 不进查询串：后端缺省就是全部待确认池；'me' 才需要 owner_id。
  if (filter.owner === 'me' && me.value) q.owner_id = me.value
  return q
}

async function fetchList() {
  // 旗子关着时读口恒 503，而这是装配期事实（改档要重启）—— 先问档位，关着就不发这条
  // 注定失败的请求：请求发出去只会在控制台留一行 503，而页面的告警位已经把原因说完了。
  if (stats.assembled === false) {
    unavailable.value = true
    loadError.value = ''
    // 这一格清列表与 catch 里"绝不清成 []"不矛盾：catch 是"这次刷新失败、上一轮读数还在"，
    // 而闸门说的是"本进程根本没有读口"——留着的只可能是上一代进程的行，
    // 指着它们说"一次都没读到"是让告警和表格互相打脸。
    list.value = []
    truncated.value = false
    return
  }
  loading.value = true
  try {
    const data = await orderDraftApi.list(queryParams())
    list.value = Array.isArray(data?.list) ? data.list : []
    truncated.value = !!data?.truncated
    unavailable.value = false
    loadError.value = ''
  } catch (err) {
    // 503 是配置状态，页面要长期挂着说一句话；刷新失败绝不清成 [] 假装读到了。
    unavailable.value = err?.status === 503
    loadError.value = unavailable.value ? '' : `${err?.message || '读取草稿列表失败'}（HTTP ${err?.status || '无响应'}）`
  } finally {
    loading.value = false
  }
}

async function fetchStats() {
  try {
    const data = await orderDraftApi.stats()
    if (!data) return
    stats.mode = data.mode || ''
    stats.store = data.store || ''
    stats.assembled = data.assembled
    stats.counts = data.counts
    stats.warning = data.warning || ''
  } catch {
    // 静默失败：档位读数是辅助信息，失败时留着上一轮。唯一要清的是闸门本身 ——
    // 没读到档位就不能武断地认定"底座关着"而不发列表请求。
    // （这里不许写 unavailable.value = false：它是 fetchList 的结论，
    //  refresh() 先 stats 后 list，无条件清会把刚立起来的"底座不可用"告警抹掉，
    //  页面于是举着空表格说一切正常 —— 实测点一次「刷新」就会出这个形态。）
    stats.assembled = undefined
  }
}

function reload() {
  fetchList()
}

async function refresh() {
  await fetchStats()
  await fetchList()
}

function openDetail(row) {
  router.push(`/dashboard/drafts/${encodeURIComponent(row.id)}`)
}

// 确认的两半必须都说出口：provisional 时 order_id 是本进程临时号（orders 表没有这一行），
// 只弹「确认成功」会让下一步开票/发货打在一个不存在的单上。
async function confirmDraft(row) {
  try {
    await ElMessageBox.confirm(
      `确认把「${row.product_name}」×${row.quantity} 转成正式订单？确认后草稿不可再改。`,
      '确认成单',
      { confirmButtonText: '确认成单', cancelButtonText: '再想想', type: 'warning' }
    )
  } catch {
    return
  }
  actingId.value = row.id
  try {
    const result = await orderDraftApi.confirm(row.id)
    if (result?.order_provisional) {
      ElMessage.warning(
        t('businessCopy.orderDraft.provisionalOrderWarning', { orderId: result.order_id || '—' })
      )
    } else {
      ElMessage.success(`已确认成单${result?.order_id ? `：订单 ${result.order_id}` : ''}`)
    }
    refresh()
  } catch (err) {
    // 404（这条已不在）与 409（已被别人确认/取消/已过期）都以刷新收场：
    // 列表页的正确形态是行消失或状态变化，而不是留在原地让人再点一次。
    if (err?.status === 404 || err?.status === 409) refresh()
  } finally {
    actingId.value = ''
  }
}

async function cancelDraft(row) {
  let reason
  try {
    ({ value: reason } = await ElMessageBox.prompt(
      `取消「${row.product_name}」这张草稿，必须给理由`,
      '取消草稿',
      {
        inputType: 'textarea',
        inputPlaceholder: '例如：客户不要了 / 价格谈不拢 / 重复草稿',
        inputValidator: (v) => (v && v.trim() ? true : '取消必须写理由（理由是取消原因分析的唯一原料）'),
        confirmButtonText: '取消草稿',
        cancelButtonText: '返回'
      }
    ))
  } catch {
    return
  }
  try {
    await orderDraftApi.cancel(row.id, reason.trim())
    ElMessage.success('已取消')
    refresh()
  } catch (err) {
    if (err?.status === 404 || err?.status === 409) refresh()
  }
}

onMounted(refresh)
</script>

<style scoped>
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.header-left {
  display: flex;
  align-items: center;
  gap: 10px;
}
.mb-12 {
  margin-bottom: 12px;
}
.ml-6 {
  margin-left: 6px;
}
.filter-select {
  width: 220px;
}
.counts {
  margin-bottom: 12px;
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  align-items: center;
  font-size: 13px;
}
.count-chip {
  white-space: nowrap;
}
.counts-at {
  color: #909399;
  font-size: 12px;
}
.overdue {
  color: #f56c6c;
}
.hint {
  color: #909399;
  font-size: 13px;
  margin-top: 12px;
}
.mono {
  font-family: monospace;
  font-size: 12px;
}
</style>
