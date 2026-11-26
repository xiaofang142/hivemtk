<template>
  <div class="order-draft-detail">
    <el-alert
      v-if="!me"
      class="mb-12"
      type="info"
      show-icon
      :closable="false"
      :title="t('businessCopy.identity.title')"
      :description="t('businessCopy.identity.orderDraftDescription')"
    />
    <el-alert
      v-if="loadError"
      class="mb-12"
      :type="loadError === 404 ? 'info' : 'warning'"
      show-icon
      :closable="false"
      :title="errorTitle"
      :description="errorDescription"
    />

    <el-card
      v-loading="loading"
      shadow="never"
    >
      <template #header>
        <div class="card-header">
          <div class="header-left">
            <el-button
              link
              type="primary"
              @click="goBack"
            >
              ← 返回列表
            </el-button>
            <span>草稿详情</span>
            <el-tag
              v-if="draft"
              size="small"
              :type="STATUS_TAGS[draft.status] || 'info'"
            >
              {{ STATUS_LABELS[draft.status] || draft.status }}
            </el-tag>
          </div>
          <div
            v-if="draft && draft.status === 'pending'"
            class="header-actions"
          >
            <el-button
              type="success"
              :disabled="!me"
              :loading="acting === 'confirm'"
              @click="doConfirm"
            >
              确认成单
            </el-button>
            <el-button
              type="danger"
              plain
              :disabled="!me"
              :loading="acting === 'cancel'"
              @click="doCancel"
            >
              取消草稿
            </el-button>
          </div>
        </div>
      </template>

      <template v-if="draft">
        <el-descriptions
          :column="2"
          border
          class="mb-12"
        >
          <el-descriptions-item label="草稿 id">
            <span class="mono">{{ draft.id }}</span>
          </el-descriptions-item>
          <el-descriptions-item label="状态">
            {{ STATUS_LABELS[draft.status] || draft.status }}
          </el-descriptions-item>
          <el-descriptions-item label="客户">
            <EntityLabel
              entity="customer"
              :value="draft.customer_id"
            />
          </el-descriptions-item>
          <el-descriptions-item :label="t('businessCopy.orderDraft.owner')">
            <EntityLabel
              entity="user"
              :value="draft.owner_id"
            />
          </el-descriptions-item>
          <el-descriptions-item label="产品">
            {{ draft.product_name }}
            <el-tag
              v-if="draft.category"
              size="small"
              type="info"
              class="ml-6"
            >
              {{ draft.category }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="置信度">
            {{ pct(draft.confidence) }}
          </el-descriptions-item>
          <el-descriptions-item label="数量">
            {{ draft.quantity }}
          </el-descriptions-item>
          <el-descriptions-item label="单价 × 总额">
            {{ money(draft.unit_price) }} × {{ draft.quantity }} = <b>{{ money(draft.total_amount) }}</b>
          </el-descriptions-item>
          <el-descriptions-item label="来源">
            {{ draft.source }}
          </el-descriptions-item>
          <el-descriptions-item label="意向 id">
            <span class="mono">{{ draft.intent_id || '—' }}</span>
          </el-descriptions-item>
          <el-descriptions-item
            :span="2"
            label="来源原文"
          >
            {{ draft.source_text || '—' }}
          </el-descriptions-item>
          <el-descriptions-item
            :span="2"
            label="备注"
          >
            {{ draft.note || '—' }}
          </el-descriptions-item>
          <el-descriptions-item label="创建">
            {{ formatTime(draft.created_at) }}
          </el-descriptions-item>
          <el-descriptions-item label="到期">
            <span :class="{ overdue: isExpiredSoon(draft) }">{{ formatTime(draft.expires_at) }}</span>
          </el-descriptions-item>
          <el-descriptions-item
            v-if="draft.order_id"
            label="订单 id"
          >
            <span class="mono">{{ draft.order_id }}</span>
          </el-descriptions-item>
          <el-descriptions-item
            v-if="draft.confirmed_at"
            label="确认时间"
          >
            {{ formatTime(draft.confirmed_at) }}
          </el-descriptions-item>
          <el-descriptions-item
            v-if="draft.cancel_reason"
            :span="2"
            label="取消理由"
          >
            {{ draft.cancel_reason }}
          </el-descriptions-item>
        </el-descriptions>

        <el-alert
          v-if="draft.status !== 'pending'"
          class="mb-12"
          type="info"
          show-icon
          :closable="false"
          :title="terminalHint(draft)"
        />

        <el-card
          v-if="draft.status === 'pending'"
          shadow="never"
        >
          <template #header>
            <div class="card-header">
              <span>确认前修改（改价 / 改量 / 改产品名 / 加备注）</span>
              <el-button
                type="primary"
                :disabled="!me || !dirty"
                :loading="acting === 'edit'"
                @click="doEdit"
              >
                保存修改
              </el-button>
            </div>
          </template>

          <el-form
            label-width="96px"
            @submit.prevent
          >
            <el-form-item label="产品名">
              <el-input
                v-model="form.product_name"
                maxlength="200"
                :placeholder="t('businessCopy.orderDraft.productNameHint')"
              />
            </el-form-item>
            <el-form-item label="数量">
              <el-input-number
                v-model="form.quantity"
                :min="1"
                :max="999999"
              />
            </el-form-item>
            <el-form-item label="单价">
              <el-input-number
                v-model="form.unit_price"
                :min="0"
                :precision="2"
                :step="1"
              />
            </el-form-item>
            <el-form-item label="备注">
              <el-input
                v-model="form.note"
                type="textarea"
                :rows="3"
                maxlength="2000"
                show-word-limit
              />
            </el-form-item>
          </el-form>
          <p class="hint">
            {{ t('businessCopy.orderDraft.editHint') }}
          </p>
        </el-card>
      </template>
    </el-card>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { orderDraftApi } from '@/api/orderDraft'
import EntityLabel from '@/components/EntityLabel.vue'
import { useUserStore } from '@/stores/user'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

// 草稿详情 + 确认/取消/改价三个人工动作。深链 /dashboard/drafts/:id 就是
// 工作台聚合待办生成的那个 URL（sales_workbench.go），本页存在之前它是死链。
//
// 终态（已确认/已取消/已过期）是只读的：动作按钮整个消失而不是置灰 ——
// 置灰按钮会被读成「我权限不够」，而真原因是这条已经不需要人再处理了。

const STATUS_TAGS = { pending: 'warning', confirmed: 'success', cancelled: 'info', expired: 'danger' }
const STATUS_LABELS = { pending: '待确认', confirmed: '已确认', cancelled: '已取消', expired: '已过期' }

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()
const me = computed(() => String(userStore.userInfo?.id ?? ''))

const loading = ref(false)
const acting = ref('') // '' | 'confirm' | 'cancel' | 'edit'
const loadError = ref(0) // 0 无 / 404 不存在 / 503 底座未装配 / 其他 读取失败
const loadErrorMsg = ref('')
const draft = ref(null)
const form = reactive({ product_name: '', quantity: 1, unit_price: 0, note: '' })

const draftId = computed(() => String(route.params.id || ''))

// 「稍后重试」对 503 是假话：档位是装配期事实，不重启永远不会自己变好。
// 深链正是工作台待办的落点，用户从这里看到的第一句必须是能行动的那句。
const errorTitle = computed(() => {
  if (loadError.value === 404) return '这条草稿不存在（或已被清扫）'
  if (loadError.value === 503) return t('businessCopy.orderDraft.unavailableTitle')
  return '草稿读取失败'
})
const errorDescription = computed(() => {
  if (loadError.value === 404) return '它可能刚被别人确认/取消，或已过期被清扫器收口。'
  if (loadError.value === 503) return t('businessCopy.orderDraft.detailUnavailableDescription')
  return `${loadErrorMsg.value || '后端回了非 404 的错误'}；不要当成草稿没了。`
})

// dirty 只比较动过的字段：后端对「没给」什么都不做，把原值也发过去
// 等于让「保存」按钮在无改动时也能点出一次无意义的写。
const dirty = computed(() => {
  const d = draft.value
  if (!d) return false
  return (
    form.product_name !== (d.product_name || '') ||
    form.quantity !== d.quantity ||
    form.unit_price !== d.unit_price ||
    form.note !== (d.note || '')
  )
})

function fillForm(d) {
  form.product_name = d.product_name || ''
  form.quantity = d.quantity || 1
  form.unit_price = Number(d.unit_price) || 0
  form.note = d.note || ''
}

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

function isExpiredSoon(d) {
  const t = new Date(d.expires_at).getTime()
  if (Number.isNaN(t)) return false
  return t - Date.now() < 3600 * 1000
}

function terminalHint(d) {
  if (d.status === 'confirmed') return `这条已确认成单${d.order_id ? `（订单 ${d.order_id}）` : ''}，草稿侧不可再改。`
  if (d.status === 'cancelled') return `这条已取消${d.cancel_reason ? `：${d.cancel_reason}` : ''}，不可再操作。`
  if (d.status === 'expired') return '这条已过期，清扫器会按节拍收口；要继续谈就让 AI 重新产一条。'
  return ''
}

async function load() {
  if (!draftId.value) {
    loadError.value = 404
    return
  }
  loading.value = true
  loadError.value = 0
  loadErrorMsg.value = ''
  draft.value = null
  try {
    // 底座没装配时这条深链读到的永远是 503，而工作台待办恰恰会生成这条深链。
    // 先问档位（/stats 不随旗子消失）：关着就不发那条注定失败的请求，
    // 页面直接把「要改旗子并重启」说出口，而不是留一行 503 和一句"稍后重试"。
    const gate = await orderDraftApi.stats().catch(() => null)
    if (gate && gate.assembled === false) {
      loadError.value = 503
      return
    }
    const d = await orderDraftApi.get(draftId.value)
    draft.value = d
    fillForm(d)
  } catch (err) {
    draft.value = null
    loadError.value = err?.status || 500
    loadErrorMsg.value = err?.message || ''
  } finally {
    loading.value = false
  }
}

function goBack() {
  router.push('/dashboard/drafts')
}

async function doConfirm() {
  try {
    await ElMessageBox.confirm(
      `确认把「${draft.value.product_name}」×${draft.value.quantity} 转成正式订单？确认后草稿不可再改。`,
      '确认成单',
      { confirmButtonText: '确认成单', cancelButtonText: '再想想', type: 'warning' }
    )
  } catch {
    return
  }
  acting.value = 'confirm'
  try {
    const result = await orderDraftApi.confirm(draftId.value)
    if (result?.order_provisional) {
      ElMessage.warning(
        t('businessCopy.orderDraft.provisionalOrderWarning', { orderId: result.order_id || '—' })
      )
    } else {
      ElMessage.success(`已确认成单${result?.order_id ? `：订单 ${result.order_id}` : ''}`)
    }
    await load()
  } catch (err) {
    // 404/409：这条已被人处理或已过期 —— 回读一次让页面落到真实终态。
    if (err?.status === 404 || err?.status === 409) await load()
  } finally {
    acting.value = ''
  }
}

async function doCancel() {
  let reason
  try {
    ({ value: reason } = await ElMessageBox.prompt(
      `取消「${draft.value.product_name}」这张草稿，必须给理由`,
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
  acting.value = 'cancel'
  try {
    await orderDraftApi.cancel(draftId.value, reason.trim())
    ElMessage.success('已取消')
    await load()
  } catch (err) {
    if (err?.status === 404 || err?.status === 409) await load()
  } finally {
    acting.value = ''
  }
}

async function doEdit() {
  const d = draft.value
  if (!d) return
  const updates = {}
  if (form.product_name !== (d.product_name || '')) updates.product_name = form.product_name.trim()
  if (form.quantity !== d.quantity) updates.quantity = form.quantity
  if (form.unit_price !== d.unit_price) updates.unit_price = form.unit_price
  if (form.note !== (d.note || '')) updates.note = form.note
  if (!Object.keys(updates).length) return

  acting.value = 'edit'
  try {
    const fresh = await orderDraftApi.edit(draftId.value, updates)
    draft.value = fresh
    fillForm(fresh)
    ElMessage.success('已保存（总额已按新值重算）')
  } catch (err) {
    // 400（校验）由拦截器原样弹后端文案；409（同客户同产品 pending 撞车 / 已过态）回读落态。
    if (err?.status === 404 || err?.status === 409) await load()
  } finally {
    acting.value = ''
  }
}

onMounted(load)
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
.header-actions {
  display: flex;
  gap: 8px;
}
.mb-12 {
  margin-bottom: 12px;
}
.ml-6 {
  margin-left: 6px;
}
.overdue {
  color: #f56c6c;
}
.hint {
  color: #909399;
  font-size: 13px;
}
.mono {
  font-family: monospace;
  font-size: 12px;
}
</style>
