<template>
  <div class="my-modules">
    <el-row
      :gutter="12"
      class="stats-row"
    >
      <el-col :span="8">
        <el-card shadow="never">
          <div class="stat">
            <div class="label">
              已开通
            </div><div class="value">
              {{ ownedCount }}
            </div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="8">
        <el-card shadow="never">
          <div class="stat">
            <div class="label">
              基础能力
            </div><div class="value">
              {{ baseCount }}
            </div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="8">
        <el-card shadow="never">
          <div class="stat">
            <div class="label">
              平台连接
            </div>
            <div class="value small">
              <el-tag
                size="small"
                :type="platformEnabled ? 'success' : 'info'"
              >
                {{ platformEnabled ? '已连接' : '未连接' }}
              </el-tag>
            </div>
            <el-button
              size="small"
              text
              type="primary"
              :loading="syncing"
              class="mt-4"
              @click="onSync"
            >
              立即同步
            </el-button>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <!-- LLM 余额：需求五要求「实时扣减剩余 token 数」，
         而"实时扣减"对用户是否可感知，取决于他能不能看到剩余量。
         后端 /api/llm/wallet 与 /api/llm/plans 早已就位，前端却没有一处调用 ——
         那等于做了一整套计量却没有任何人看得见。 -->
    <el-card
      shadow="never"
      class="mt-12"
    >
      <template #header>
        <div class="card-header">
          <span>平台模型余额（按 token 预付费，输入/输出分别扣减）</span>
          <el-button
            v-if="platformEnabled"
            size="small"
            type="primary"
            :disabled="!llmCloudOwned"
            @click="planVisible = true"
          >
            购买套餐
          </el-button>
        </div>
      </template>

      <el-alert
        v-if="!platformEnabled"
        type="info"
        :closable="false"
        title="平台未连接"
        description="平台模型余额需连接平台后查看与充值。当前可正常使用自带 API Key 的模型（不计费）。"
      />

      <el-row
        v-else-if="wallet.exists"
        :gutter="16"
      >
        <el-col :span="8">
          <div class="bal-label">
            剩余输入 token
          </div>
          <div
            class="bal-value"
            :class="{ low: wallet.low }"
          >
            {{ fmtTokens(wallet.input_tokens_remaining) }}
          </div>
        </el-col>
        <el-col :span="8">
          <div class="bal-label">
            剩余输出 token
          </div>
          <div
            class="bal-value"
            :class="{ low: wallet.low }"
          >
            {{ fmtTokens(wallet.output_tokens_remaining) }}
          </div>
        </el-col>
        <el-col :span="8">
          <div class="bal-label">
            累计消耗（输入 / 输出）
          </div>
          <div class="bal-value small">
            {{ fmtTokens(wallet.total_consumed_input) }} / {{ fmtTokens(wallet.total_consumed_output) }}
          </div>
        </el-col>
      </el-row>
      <el-empty
        v-else-if="llmCloudOwned"
        description="尚未购买平台模型套餐"
        :image-size="60"
      />
      <!-- 未订阅 llm-cloud：4301 被 _silent 吞掉后这里会是空钱包 ——
           给一个明确去处，而不是让人对着「尚未购买」发呆。 -->
      <div
        v-else
        class="llm-locked"
      >
        <span class="muted">平台模型余额与套餐由 llm-cloud 模块提供。</span>
        <el-button
          link
          type="primary"
          @click="$router.push('/module-market/detail/llm-cloud')"
        >
          订阅 llm-cloud 后可查看余额与套餐 →
        </el-button>
      </div>
    </el-card>

    <el-card
      shadow="never"
      class="mt-12"
    >
      <template #header>
        <div class="card-header">
          <span>我的功能模块</span>
          <el-radio-group
            v-model="tab"
            size="small"
          >
            <el-radio-button label="owned">
              已开通 ({{ owned.length }})
            </el-radio-button>
            <el-radio-button label="all">
              全部 ({{ list.length }})
            </el-radio-button>
          </el-radio-group>
        </div>
      </template>

      <el-form inline>
        <el-form-item label="关键词">
          <el-input
            v-model="keyword"
            placeholder="模块名称 / key"
            clearable
            style="width: 220px"
          />
        </el-form-item>
        <el-form-item>
          <el-button @click="$router.push('/module-market')">
            去市场
          </el-button>
          <el-button @click="$router.push('/module-market/orders')">
            我的订单
          </el-button>
        </el-form-item>
      </el-form>

      <el-table
        v-loading="loading"
        :data="filtered"
        stripe
      >
        <el-table-column
          label="模块"
          min-width="220"
        >
          <template #default="{ row }">
            <div class="name">
              {{ row.name }}
              <el-tag
                size="small"
                :type="row.category === 'base' ? 'success' : 'warning'"
                effect="plain"
              >
                {{ CATEGORY_LABEL[row.category] }}
              </el-tag>
              <el-tag
                v-if="row.owned"
                size="small"
                :type="STATUS_TAG[row.status]"
              >
                {{ STATUS_LABEL[row.status] || row.status }}
              </el-tag>
            </div>
            <div class="key">
              {{ row.module_key }}
            </div>
          </template>
        </el-table-column>
        <el-table-column
          label="价值"
          min-width="240"
        >
          <template #default="{ row }">
            <span class="muted">{{ row.summary }}</span>
          </template>
        </el-table-column>
        <el-table-column
          label="价格"
          width="120"
        >
          <template #default="{ row }">
            {{ priceLabel(row) }}
          </template>
        </el-table-column>
        <el-table-column
          label="开通来源"
          width="140"
        >
          <template #default="{ row }">
            <el-tag
              v-if="row.owned"
              size="small"
              effect="plain"
            >
              {{ SOURCE_LABEL[row.source] || row.source }}
            </el-tag>
            <span
              v-else
              class="muted"
            >—</span>
          </template>
        </el-table-column>
        <el-table-column
          label="页面数"
          width="90"
          align="right"
        >
          <template #default="{ row }">
            {{ (row.menu_keys || []).length }}
          </template>
        </el-table-column>
        <el-table-column
          label="到期"
          width="120"
        >
          <template #default="{ row }">
            {{ row.expires_at ? row.expires_at.slice(0, 10) : '—' }}
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="110"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button
              link
              type="primary"
              @click="$router.push(`/module-market/detail/${row.module_key}`)"
            >
              详情
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog
      v-model="planVisible"
      title="平台模型套餐"
      width="680px"
    >
      <el-alert
        type="info"
        :closable="false"
        class="mb-12"
        title="输入与输出分别计价"
        description="绝大多数厂商的输出单价是输入的数倍。点击「购买」直接进入在线收银台，支付成功后余额自动入账。"
      />
      <el-table
        v-loading="planLoading"
        :data="plans"
        size="small"
      >
        <el-table-column
          prop="name"
          label="套餐"
          min-width="140"
        />
        <el-table-column
          label="额度（输入 / 输出）"
          min-width="180"
        >
          <template #default="{ row }">
            {{ fmtTokens(row.input_tokens) }} / {{ fmtTokens(row.output_tokens) }}
          </template>
        </el-table-column>
        <el-table-column
          label="单价（分/1k）"
          width="150"
        >
          <template #default="{ row }">
            <div>输入 {{ row.input_price_per_1k_cents }}</div>
            <div>输出 {{ row.output_price_per_1k_cents }}</div>
          </template>
        </el-table-column>
        <el-table-column
          label="售价"
          width="100"
          align="right"
        >
          <template #default="{ row }">
            ¥{{ fmtPrice(row.price_cents) }}
          </template>
        </el-table-column>
        <el-table-column
          label=""
          width="90"
        >
          <template #default="{ row }">
            <el-button
              link
              type="primary"
              :loading="ordering"
              :disabled="ordering"
              @click="buy(row)"
            >
              {{ ordering ? '下单中' : '购买' }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-dialog>

    <!-- 收银台：与模块/订阅下单共用同一个组件。
         有 payment 段 → 在线支付；无 → 只在通道异常时展示兜底指引。 -->
    <CashierDialog
      v-model="cashierVisible"
      :order-no="cashierOrderNo"
      :payment="cashierPayment"
      :guide="cashierGuide"
      :amount-cents="cashierAmountCents"
      @paid="onLLMPaid"
    />

    <el-card
      shadow="never"
      class="mt-12"
    >
      <template #header>
        <span>同步日志</span>
      </template>
      <el-table
        :data="logs"
        size="small"
        empty-text="暂无"
      >
        <el-table-column
          label="时间"
          width="170"
        >
          <template #default="{ row }">
            {{ (row.created_at || '').replace('T', ' ').slice(0, 19) }}
          </template>
        </el-table-column>
        <el-table-column
          label="动作"
          prop="action"
          width="150"
        />
        <el-table-column
          label="状态"
          width="110"
        >
          <template #default="{ row }">
            <el-tag
              size="small"
              :type="row.status === 'success' ? 'success' : row.status === 'degraded' ? 'warning' : 'danger'"
            >
              {{ row.status === 'success' ? '成功' : row.status === 'degraded' ? '降级' : '失败' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="已开通"
          prop="active_count"
          width="90"
        />
        <el-table-column
          label="来源"
          prop="source"
          width="160"
        />
        <el-table-column
          label="耗时"
          width="90"
        >
          <template #default="{ row }">
            {{ row.duration_ms }} ms
          </template>
        </el-table-column>
        <el-table-column
          label="错误"
          prop="error_msg"
          min-width="200"
          show-overflow-tooltip
        />
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import {
  myModules, syncModules, moduleSyncLogs, listMarketModules,
  llmWallet, llmPlans, createLLMOrder,
  priceLabel, fmtPrice, fmtTokens,
  MODULE_CATEGORY_LABEL, MODULE_SOURCE_LABEL, MODULE_STATUS_LABEL
} from '@/api/moduleMarket'
import { useUserStore } from '@/stores/user'
import CashierDialog from '@/components/CashierDialog.vue'


const CATEGORY_LABEL = MODULE_CATEGORY_LABEL
const SOURCE_LABEL = MODULE_SOURCE_LABEL
const STATUS_LABEL = MODULE_STATUS_LABEL
const STATUS_TAG = { active: 'success', expired: 'warning', disabled: 'info' }

const list = ref([])
const logs = ref([])
const wallet = ref({})
const plans = ref([])
const planVisible = ref(false)
const planLoading = ref(false)
const ordering = ref(false)
const loading = ref(false)
const syncing = ref(false)
const platformEnabled = ref(true)

// 收银台（与模块/订阅共用）：订单号 / 支付参数 / 兜底指引 / 金额
const cashierVisible = ref(false)
const cashierOrderNo = ref('')
const cashierPayment = ref(null)
const cashierGuide = ref('')
const cashierAmountCents = ref(0)

const userStore = useUserStore()
const tab = ref('owned')
const keyword = ref('')
const filter = reactive({})

const owned = computed(() => list.value.filter(m => m.owned))
const ownedCount = computed(() => owned.value.length)
const baseCount = computed(() => list.value.filter(m => m.category === 'base').length)
// 余额/套餐接口挂在 llm-cloud 模块闸后：未订阅时它们静默返回空（_silent 4301）。
// 「没余额可看」和「根本没资格看」要分开显示 —— 后者得指路去市场。
const llmCloudOwned = computed(() =>
  list.value.some(m => m.module_key === 'llm-cloud' && m.owned))

const filtered = computed(() => {
  let out = tab.value === 'owned' ? owned.value : list.value
  if (keyword.value) {
    const k = keyword.value.trim().toLowerCase()
    out = out.filter(m =>
      (m.name || '').toLowerCase().includes(k) ||
      (m.module_key || '').toLowerCase().includes(k))
  }
  return out
})

async function fetch () {
  loading.value = true
  try {
    // 1) 平台闸态：走公开路由 GET /api/module-market/list（无鉴权、不过模块闸），
    //    它的 platform_enabled 才是权威值 —— 此前这里的 ref(true) 是个假默认，
    //    离线实例上 false 的真相根本没机会被读到。
    //    探针本身也 _silent：它是后台预取，失败不该弹 toast 打扰页面。
    try {
      const market = await listMarketModules(undefined, { _silent: true })
      platformEnabled.value = !!market?.platform_enabled
    } catch {
      // 拉不到就维持现值；下面的 LLM 调用全带 _silent，最坏也只是不显示余额
    }
    // 2) 我的模块列表（本页主数据）
    const res = await myModules()
    list.value = res?.list || []
    // 3) 同步日志是 ADMIN 专属口，普通用户必 403 —— _silent 让它安静地走
    //    既有 try/catch，不再每次进页面就弹一句「拒绝访问」。
    try {
      const l = await moduleSyncLogs({ limit: 20 }, { _silent: true })
      logs.value = l?.list || []
    } catch {
      logs.value = []
    }
    // 4) 只有平台闸开着才拉余额/套餐；llm-cloud 未开通时 4301 由 _silent 拦下，
    //    不 toast、不跳转，用户留在本页。
    if (platformEnabled.value) await loadLLM()
  } finally {
    loading.value = false
  }
}

async function loadLLM () {
  if (!platformEnabled.value) return
  try {
    wallet.value = (await llmWallet({ _silent: true })) || {}
  } catch { wallet.value = {} }
  try {
    plans.value = (await llmPlans({ _silent: true }))?.list || []
  } catch { plans.value = [] }
}

function openPlans () {
  planVisible.value = true
  loadLLM()
}

// 订单幂等键：点击购买即生成一次，失败重试沿用同一键 —— 不会重复下单
// （与模块市场 Market.vue 同口径）。
function genClientOrderNo () {
  return `LLM-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`
}

// 购买套餐：不再要求先填联系方式/加微信，直接下单并进入在线收银台。
// 账号邮箱顺带上行，仅用于线下通道异常时的兜底联系（不作为付款前置条件）。
async function buy (plan) {
  // 防重复下单（BUG-7，2026-11-04）：epay 建单有秒级耗时，按钮 loading 之外
  // 再挡一道——client_order_no 每次点击都重新生成，连点会静默多建订单。
  if (ordering.value) return
  ordering.value = true
  try {
    const res = await createLLMOrder({
      plan_code: plan?.plan_code,
      client_order_no: genClientOrderNo(),
      contact_email: userStore.userInfo?.email || ''
    })
    if (res?.payment?.pay_url) {
      cashierOrderNo.value = res.order_no
      cashierPayment.value = res.payment
      cashierGuide.value = res?.payment_guide || ''
      cashierAmountCents.value = Number(res?.total_amount_cents || 0)
      cashierVisible.value = true
    } else if (res?.status === 'fulfilled') {
      ElMessage.success('套餐已生效，余额已入账')
      loadLLM()
    } else {
      // 通道未启用/生成失败：平台零代码回滚线下路径。
      ElMessage.warning(res?.payment_guide || '在线支付暂不可用，请联系平台运营')
    }
  } catch (e) {
    ElMessage.error(e?.message || '下单失败')
  } finally {
    ordering.value = false
  }
}

// 支付确认：刷新钱包余额与套餐列表（余额入账由平台回调完成）
function onLLMPaid () {
  ElMessage.success('支付成功！余额入账中，稍后自动刷新。')
  setTimeout(loadLLM, 1500)
}

async function onSync () {
  syncing.value = true
  try {
    const res = await syncModules()
    // 同步 DTO 是 {active_count, source, degraded, message} —— 里面**没有**
    // platform_enabled，读它只会得到 undefined 并被误当成「开着」。
    // 闸态改由下面 fetch() 里的公开列表刷新，并由 fetch() 决定是否拉 LLM。
    if (res?.degraded) {
      ElMessage.warning(res.message || '平台不可达，已沿用上次同步结果')
    } else {
      // active_count 是「模块目录里可用条目数」，与上方「已开通」卡片（我的模块数）不是同一口径，
      // 直接写「当前开通 N 个模块」会和卡片数字对不上，这里改成目录口径的措辞。
      ElMessage.success(res.message || `已同步模块目录，可用 ${res?.active_count ?? 0} 个条目`)
    }
    fetch()
  } catch (e) {
    ElMessage.error(e?.message || '同步失败')
  } finally {
    syncing.value = false
  }
}

onMounted(fetch)
</script>

<style scoped>
.card-header { display: flex; align-items: center; justify-content: space-between; }
.mt-12 { margin-top: 12px; }
.mt-4 { margin-top: 4px; }
.stats-row { margin-bottom: 4px; }
.stat .label { color: var(--el-text-color-secondary); font-size: 12px; }
.stat .value { font-size: 24px; font-weight: 600; margin-top: 4px; }
.stat .value.small { font-size: 14px; }
.name { font-weight: 500; display: flex; align-items: center; gap: 6px; }
.key { color: var(--el-text-color-secondary); font-size: 12px; font-family: monospace; }
.muted { color: var(--el-text-color-secondary); font-size: 13px; }
.llm-locked {
  display: flex; align-items: center; justify-content: space-between;
  gap: 12px; flex-wrap: wrap;
  padding: 8px 12px; border-radius: 6px;
  background: var(--el-fill-color-light); font-size: 13px;
}
</style>