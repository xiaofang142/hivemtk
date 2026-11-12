<template>
  <el-dialog
    v-model="visible"
    title="收银台"
    width="460px"
    :close-on-click-modal="false"
    @closed="onClosed"
  >
    <!-- 在线支付：跳转收银台 -->
    <div
      v-if="payment && payment.pay_url"
      class="cashier-body"
    >
      <p class="cashier-hint">
        订单 <b>{{ orderNo }}</b> 金额 <b>¥{{ amountText }}</b>
      </p>
      <p class="cashier-sub">
        {{ payment.mock ? '当前为模拟支付通道（EPAY_MOCK），点击下方按钮完成模拟付款。' : '点击下方按钮跳转支付页面完成付款。' }}
      </p>
      <div
        v-if="polling"
        class="cashier-wait"
      >
        <el-icon class="is-loading">
          <Loading />
        </el-icon>
        <span>等待支付确认…（{{ pollCount }}/{{ maxPolls }}）</span>
      </div>
      <div
        v-else-if="paid"
        class="cashier-done"
      >
        <el-icon color="#67c23a">
          <CircleCheckFilled />
        </el-icon>
        <span>支付成功，权益开通中…</span>
      </div>
      <div
        v-else-if="exhausted"
        class="cashier-exhausted"
      >
        <span>轮询超时 —— 若已完成支付，可点击「手动刷新」。</span>
      </div>
    </div>

    <!-- 无在线支付参数：线下付款指引 -->
    <div
      v-else
      class="cashier-body"
    >
      <el-alert
        type="warning"
        :closable="false"
        show-icon
        title="在线支付通道未启用"
        description="本单未返回在线支付参数，请按下方指引联系平台运营完成线下付款；付款确认后权益自动开通。"
      />
      <p
        v-if="guide"
        class="cashier-guide"
      >
        {{ guide }}
      </p>
    </div>

    <template #footer>
      <el-button @click="visible = false">
        关闭
      </el-button>
      <el-button
        v-if="payment && payment.pay_url && !paid"
        type="primary"
        @click="openPayUrl"
      >
        {{ payment.method === 'POST' ? '前往支付' : '立即支付' }}
      </el-button>
      <el-button
        v-if="payment && payment.pay_url && exhausted && !paid"
        type="primary"
        plain
        @click="restartPolling"
      >
        手动刷新
      </el-button>
      <el-button
        v-if="paid"
        type="success"
        @click="visible = false"
      >
        完成
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup>
// ============================================================
// 收银台 Dialog（003 T019）
//
// 下单成功后弹出：有 payment.pay_url → 跳转支付 + 轮询 pay-status；
// 无 → 展示线下付款指引（平台关渠道 enabled 的零代码回滚路径）。
//
// 轮询纪律：3s × 40 次封顶后转手动刷新（防无限轮询）——
// 轮询停止条件（paid/exhausted/close）由组件单测钉住。
// ============================================================
import { ref, computed, watch } from 'vue'
import { Loading, CircleCheckFilled } from '@element-plus/icons-vue'
import { orderPayStatus } from '@/api/moduleMarket'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  orderNo: { type: String, default: '' },
  // payment：平台下单响应的 payment 段（pay_url/params/method/mock）
  payment: { type: Object, default: null },
  // guide：无 payment 时的线下付款指引
  guide: { type: String, default: '' },
  // amountCents：展示金额（分）
  amountCents: { type: Number, default: 0 }
})
const emit = defineEmits(['update:modelValue', 'paid', 'closed'])

const visible = computed({
  get: () => props.modelValue,
  set: (v) => emit('update:modelValue', v)
})

const amountText = computed(() => {
  const v = Number(props.amountCents || 0) / 100
  return v.toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
})

// ---- 轮询状态机 ----
const pollIntervalMs = 3000
const maxPolls = 40
const polling = ref(false)
const pollCount = ref(0)
const paid = ref(false)
const exhausted = ref(false)
let timer = null

function stopPolling () {
  if (timer) {
    clearInterval(timer)
    timer = null
  }
  polling.value = false
}

async function tick () {
  if (!props.orderNo) return
  pollCount.value += 1
  try {
    const res = await orderPayStatus(props.orderNo, { _silent: true })
    if (res && (res.paid || res.status === 'paid' || res.status === 'fulfilled')) {
      paid.value = true
      stopPolling()
      emit('paid', res)
      return
    }
  } catch { /* 轮询失败不弹 toast：静默通道，下一轮再试 */ }
  if (pollCount.value >= maxPolls) {
    exhausted.value = true
    stopPolling()
  }
}

function startPolling () {
  stopPolling()
  pollCount.value = 0
  exhausted.value = false
  if (!(props.payment && props.payment.pay_url)) return
  polling.value = true
  tick() // 立即查一次（用户可能已经付完才回到页面）
  timer = setInterval(tick, pollIntervalMs)
}

function restartPolling () {
  startPolling()
}

function openPayUrl () {
  if (!props.payment || !props.payment.pay_url) return
  const url = props.payment.pay_url
  if (props.payment.method === 'POST' && props.payment.params) {
    // POST 表单跳转：构造隐藏表单提交（易支付 POST 型网关）
    try {
      const form = document.createElement('form')
      form.method = 'POST'
      form.action = url
      form.target = '_blank'
      for (const [k, v] of Object.entries(props.payment.params)) {
        const input = document.createElement('input')
        input.type = 'hidden'
        input.name = k
        input.value = v
        form.appendChild(input)
      }
      document.body.appendChild(form)
      form.submit()
      form.remove()
    } catch {
      window.open(url, '_blank')
    }
  } else {
    window.open(url, '_blank')
  }
  // 打开收银台后开始轮询确认
  startPolling()
}

function onClosed () {
  stopPolling()
  emit('closed')
}

// 打开时若有 payment 自动开始轮询（用户可能在新标签页已付完）
watch(() => props.modelValue, (v) => {
  if (v) {
    paid.value = false
    startPolling()
  } else {
    stopPolling()
  }
})
</script>

<style scoped>
.cashier-body { display: flex; flex-direction: column; gap: 10px; }
.cashier-hint { margin: 0; font-size: 14px; }
.cashier-sub { margin: 0; color: var(--el-text-color-secondary); font-size: 13px; }
.cashier-wait, .cashier-done, .cashier-exhausted {
  display: flex; align-items: center; gap: 6px;
  font-size: 13px; color: var(--el-text-color-regular);
}
.cashier-done { color: var(--el-color-success); }
.cashier-guide {
  margin: 4px 0 0; padding: 10px 12px; font-size: 13px; line-height: 1.6;
  background: var(--el-fill-color-light); border-radius: 6px;
  white-space: pre-wrap; word-break: break-word;
}
</style>
