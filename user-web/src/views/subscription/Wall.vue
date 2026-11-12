<template>
  <div class="sub-wall">
    <el-card
      class="wall-card"
      shadow="never"
    >
      <div class="wall-head">
        <el-icon
          :size="40"
          :color="expired ? '#f56c6c' : '#e6a23c'"
        >
          <Lock v-if="expired" />
          <Clock v-else />
        </el-icon>
        <h1>{{ expired ? '订阅已到期' : '订阅提醒' }}</h1>
      </div>

      <!-- 加载中 -->
      <div
        v-if="loading"
        class="wall-loading"
      >
        <el-icon class="is-loading">
          <Loading />
        </el-icon>
        <span>正在获取订阅状态…</span>
      </div>

      <!-- 状态卡 -->
      <template v-else-if="snap">
        <el-descriptions
          :column="1"
          border
          class="wall-desc"
        >
          <el-descriptions-item label="当前状态">
            <el-tag :type="statusTag">
              {{ STATUS_LABEL[snap.status] || snap.status }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item
            v-if="periodEndText"
            label="有效期至"
          >
            {{ periodEndText }}
          </el-descriptions-item>
          <el-descriptions-item
            v-else-if="trialEndText"
            label="体验期截止"
          >
            {{ trialEndText }}
          </el-descriptions-item>
          <el-descriptions-item
            v-if="priceText"
            label="订阅价格"
          >
            {{ priceText }} / 月（按 30 天计）
          </el-descriptions-item>
          <el-descriptions-item
            v-if="snap.stale"
            label="同步状态"
          >
            <el-tag
              type="warning"
              size="small"
            >
              镜像数据较旧，以平台最终确认为准
            </el-tag>
          </el-descriptions-item>
        </el-descriptions>

        <div
          v-if="snap.no_data"
          class="wall-nodata"
        >
          <el-alert
            type="info"
            :closable="false"
            show-icon
            title="尚未同步到订阅信息"
            description="首次同步完成后这里会显示订阅状态。如已购买，请稍候或在系统设置中手动同步。"
          />
        </div>

        <div class="wall-actions">
          <el-button @click="goWorkbench">
            返回工作台
          </el-button>
          <el-button
            v-if="snap.locked"
            type="primary"
            :loading="renewing"
            @click="openRenew"
          >
            立即续费
          </el-button>
        </div>
      </template>

      <!-- 读取失败 -->
      <div
        v-else
        class="wall-error"
      >
        <el-alert
          type="error"
          :closable="false"
          show-icon
          title="订阅状态获取失败"
          description="请稍后重试；若持续失败请联系平台运营。"
        />
        <div class="wall-actions">
          <el-button @click="load">
            重试
          </el-button>
          <el-button @click="goWorkbench">
            返回工作台
          </el-button>
        </div>
      </div>
    </el-card>

    <!-- 续费收银台 -->
    <CashierDialog
      v-model="cashierVisible"
      :order-no="cashierOrderNo"
      :payment="cashierPayment"
      :guide="cashierGuide"
      :amount-cents="cashierAmountCents"
      @paid="onPaid"
      @closed="onCashierClosed"
    />
  </div>
</template>

<script setup>
// ============================================================
// 续费墙（003 T023）
//
// 4305 → 本页。展示到期时间/订阅价/去支付按钮（订阅收银台）。
// 依赖接口集（/api/subscription/status|renew、pay-status、auth）全在
// 订阅闸白名单内 —— 本页自身永远不会 4305 死循环（oracle P1-12）。
//
// 续费成功恢复通道：收银台 paid → 轮询订阅状态 → 转 active →
// 主动调 /api/modules/sync 刷新镜像 → 提示并手动跳工作台。
// ============================================================
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Lock, Clock, Loading } from '@element-plus/icons-vue'
import { subscriptionStatus, renewSubscription, syncModules } from '@/api/moduleMarket'
import CashierDialog from '@/components/CashierDialog.vue'

const router = useRouter()

const loading = ref(true)
const snap = ref(null)
const renewing = ref(false)

const cashierVisible = ref(false)
const cashierOrderNo = ref('')
const cashierPayment = ref(null)
const cashierGuide = ref('')
const cashierAmountCents = ref(0)

const STATUS_LABEL = { trial: '体验期', active: '已订阅', expired: '已到期', unknown: '未同步' }

const expired = computed(() => snap.value?.status === 'expired' && snap.value?.locked)
const statusTag = computed(() => {
  const s = snap.value?.status
  if (s === 'expired') return 'danger'
  if (s === 'trial') return 'warning'
  if (s === 'active') return 'success'
  return 'info'
})

// 横幅/墙页同款口径（修订 #6）：current_period_end > trial_expires_at
// → 显示「有效期至 X」而非体验期倒计时。
const periodEndText = computed(() => {
  const s = snap.value
  if (!s?.current_period_end) return ''
  const pe = new Date(s.current_period_end)
  const te = s.trial_expires_at ? new Date(s.trial_expires_at) : null
  // 已付费（period 在未来或晚于 trial）→ 用有效期口径
  if (!te || pe > te || s.status === 'active') {
    return formatDate(pe)
  }
  return ''
})
const trialEndText = computed(() => {
  const s = snap.value
  if (!s?.trial_expires_at) return ''
  if (periodEndText.value) return '' // 已有有效期口径就不重复显示体验期
  return formatDate(new Date(s.trial_expires_at))
})
const priceText = computed(() => {
  const cents = Number(snap.value?.price_cents || 0)
  if (!cents) return ''
  return `¥${(cents / 100).toFixed(2)}`
})

function formatDate (d) {
  if (Number.isNaN(d.getTime())) return ''
  const pad = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

async function load () {
  loading.value = true
  try {
    snap.value = await subscriptionStatus()
  } catch {
    snap.value = null
  } finally {
    loading.value = false
  }
}

async function openRenew () {
  renewing.value = true
  try {
    const res = await renewSubscription({})
    if (res?.payment?.pay_url) {
      cashierOrderNo.value = res.order_no
      cashierPayment.value = res.payment
      cashierGuide.value = res?.payment_guide || ''
      cashierAmountCents.value = Number(res?.total_amount_cents || 0)
      cashierVisible.value = true
    } else if (res?.status === 'fulfilled') {
      // 0 元订单平台直通 fulfilled（修订 #9）
      ElMessage.success('续费已生效！')
      await recover()
    } else {
      cashierOrderNo.value = res.order_no
      cashierPayment.value = null
      cashierGuide.value = res?.payment_guide || '订单已提交，请联系平台运营完成付款。'
      cashierAmountCents.value = Number(res?.total_amount_cents || 0)
      cashierVisible.value = true
    }
  } catch (e) {
    ElMessage.error(e?.message || '续费下单失败')
  } finally {
    renewing.value = false
  }
}

// ---- 续费成功恢复通道 ----
// 收银台确认 paid → 轮询订阅状态直到转 active（sync 刷新镜像后本地判定翻转）
let recoverTimer = null
let recoverTicks = 0
const recoverMaxTicks = 20 // 3s × 20 = 60s 封顶

// 0 元订单直通 fulfilled 时的恢复入口：先立即做一次「镜像刷新 + 状态回读」
// （不等 3s 轮询首拍，续费成功的提示与状态翻转同时到达）；若这一拍还没
// 转 active（同步滞后），转入既有轮询兜底，与收银台 paid 路径行为一致。
// （修复：此函数曾被调用但从未定义——0 元续费成功会抛 ReferenceError，
// 成功提示已弹出但状态卡在旧值。）
async function recover () {
  try {
    await syncModules({ _silent: true })
    const s = await subscriptionStatus({ _silent: true })
    snap.value = s
    if (s && s.status === 'active') return
  } catch { /* 单次刷新失败也转入轮询 */ }
  startRecoverPolling()
}

function onPaid () {
  ElMessage.success('支付成功！正在确认订阅状态…')
  startRecoverPolling()
}

function startRecoverPolling () {
  stopRecoverPolling()
  recoverTicks = 0
  recoverTimer = setInterval(async () => {
    recoverTicks += 1
    try {
      // 主动触发 sync 刷新镜像（白名单内，expired 态可调）
      await syncModules({ _silent: true })
      const s = await subscriptionStatus({ _silent: true })
      snap.value = s
      if (s && s.status === 'active') {
        stopRecoverPolling()
        ElMessage.success('订阅已恢复！点击下方「返回工作台」继续使用。')
        return
      }
    } catch { /* 静默重试 */ }
    if (recoverTicks >= recoverMaxTicks) {
      stopRecoverPolling()
      ElMessage.warning('支付已确认，但订阅状态尚未刷新 —— 可稍后在本页重试或联系平台运营。')
    }
  }, 3000)
}

function stopRecoverPolling () {
  if (recoverTimer) {
    clearInterval(recoverTimer)
    recoverTimer = null
  }
}

function onCashierClosed () {
  // 收银台关闭时若还没确认，再拉一次状态（用户可能在新标签页付完了）
  load()
}

function goWorkbench () {
  router.push('/')
}

onMounted(load)
onBeforeUnmount(stopRecoverPolling)
</script>

<style scoped>
.sub-wall {
  min-height: 100vh;
  display: flex; align-items: flex-start; justify-content: center;
  padding: 60px 16px;
  background: var(--el-fill-color-lighter);
}
.wall-card { width: 520px; }
.wall-head {
  display: flex; align-items: center; gap: 12px;
  margin-bottom: 20px;
}
.wall-head h1 { margin: 0; font-size: 20px; }
.wall-desc { margin-bottom: 16px; }
.wall-loading, .wall-error { display: flex; flex-direction: column; gap: 12px; }
.wall-loading { align-items: center; color: var(--el-text-color-secondary); padding: 20px 0; }
.wall-nodata { margin-bottom: 16px; }
.wall-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 8px; }
</style>
