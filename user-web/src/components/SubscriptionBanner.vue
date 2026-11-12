<template>
  <!-- 仅 SaaS 版渲染（003 T023：edition gate，opensource 无订阅概念） -->
  <div
    v-if="visible"
    class="sub-banner"
    :class="bannerClass"
    role="status"
  >
    <el-icon class="sub-banner-icon">
      <component :is="bannerIcon" />
    </el-icon>
    <span class="sub-banner-text">{{ bannerText }}</span>
    <el-button
      v-if="expired"
      size="small"
      type="danger"
      plain
      class="sub-banner-action"
      @click="goRenew"
    >
      立即续费
    </el-button>
    <button
      v-if="trialSoon"
      class="sub-banner-close"
      aria-label="关闭提醒"
      @click="dismiss"
    >
      ×
    </button>
  </div>
</template>

<script setup>
// ============================================================
// 工作台顶栏订阅横幅（003 T023）
//
// - trial：剩 N 天提醒（N ≤ 阈值才显示，不打扰）；
// - expired：红条 + 立即续费；
// - 修订 #6：current_period_end > trial_expires_at 时显示「有效期至 X」
//   而非体验期倒计时 —— 防对已付费用户显示体验期。
// - 仅 edition==='saas' 渲染（T003 public config edition 字段）。
// 状态接口在订阅闸白名单内，expired 态下横幅自身不会 4305。
// ============================================================
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { Clock, WarningFilled, Bell } from '@element-plus/icons-vue'
import { http } from '@/utils/request'
import { subscriptionStatus } from '@/api/moduleMarket'

const router = useRouter()

const edition = ref('')
const snap = ref(null)
const dismissedKey = 'sub_banner_dismissed_date'

// trial 剩 N 天 ≤ 阈值才提醒（15 天体验期，默认第 10 天起提醒）
const trialSoonDays = 5

const visible = computed(() => {
  if (edition.value !== 'saas') return false
  const s = snap.value
  if (!s) return false
  if (s.status === 'expired' && s.locked) return true
  if (s.status === 'trial' && trialDaysLeft.value !== null && trialDaysLeft.value <= trialSoonDays) {
    // 当日已手动关闭则不再打扰（明天再提醒）
    try {
      return sessionStorage.getItem(dismissedKey) !== new Date().toDateString()
    } catch { return true }
  }
  return false
})

const expired = computed(() => snap.value?.status === 'expired' && snap.value?.locked)
const trialSoon = computed(() => snap.value?.status === 'trial' && trialDaysLeft.value !== null && trialDaysLeft.value <= trialSoonDays)

// 体验期剩余天数（仅 trial 态计算）
const trialDaysLeft = computed(() => {
  const s = snap.value
  if (!s || s.status !== 'trial' || !s.trial_expires_at) return null
  const diff = new Date(s.trial_expires_at).getTime() - Date.now()
  return Math.max(0, Math.ceil(diff / 86400000))
})

const bannerClass = computed(() => expired.value ? 'sub-banner--expired' : 'sub-banner--trial')
const bannerIcon = computed(() => expired.value ? WarningFilled : Bell)

// 修订 #6：已付费（current_period_end > trial_expires_at）显示「有效期至 X」
const bannerText = computed(() => {
  const s = snap.value
  if (!s) return ''
  if (expired.value) return '订阅已到期，业务功能已暂停 —— 续费后立即恢复。'
  if (s.status === 'trial') {
    // 付费优先口径：period 晚于 trial → 有效期口径（防对已付费用户显示体验期）
    const pe = s.current_period_end ? new Date(s.current_period_end) : null
    const te = s.trial_expires_at ? new Date(s.trial_expires_at) : null
    if (pe && te && pe > te) {
      return `订阅有效期至 ${formatDate(pe)}`
    }
    const d = trialDaysLeft.value
    return `体验期剩余 ${d} 天，到期后业务功能将暂停 —— 可提前续费。`
  }
  return ''
})

function formatDate (d) {
  const pad = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

function goRenew () {
  router.push('/subscription-wall')
}

function dismiss () {
  try { sessionStorage.setItem(dismissedKey, new Date().toDateString()) } catch { /* 静默 */ }
}

async function load () {
  try {
    const cfg = await http.get('/api/public/config', undefined, { _silent: true })
    edition.value = cfg?.edition || ''
  } catch { edition.value = ''; return }
  if (edition.value !== 'saas') return
  try {
    snap.value = await subscriptionStatus({ _silent: true })
  } catch { /* 横幅是增强面，取数失败不打扰 */ }
}

onMounted(load)
</script>

<style scoped>
.sub-banner {
  display: flex; align-items: center; gap: 8px;
  padding: 6px 16px;
  font-size: 13px;
  line-height: 1.4;
}
.sub-banner--trial {
  background: var(--el-color-warning-light-9);
  color: var(--el-color-warning);
  border-bottom: 1px solid var(--el-color-warning-light-7);
}
.sub-banner--expired {
  background: var(--el-color-danger-light-9);
  color: var(--el-color-danger);
  border-bottom: 1px solid var(--el-color-danger-light-7);
  font-weight: 500;
}
.sub-banner-icon { flex-shrink: 0; }
.sub-banner-text { flex: 1; }
.sub-banner-action { flex-shrink: 0; }
.sub-banner-close {
  flex-shrink: 0;
  border: none; background: transparent; cursor: pointer;
  font-size: 16px; line-height: 1; color: inherit; opacity: 0.7;
}
.sub-banner-close:hover { opacity: 1; }
</style>
