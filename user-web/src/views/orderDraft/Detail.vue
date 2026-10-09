<template>
  <div class="order-draft-detail">
    <el-alert
      v-if="!me"
      class="mb-12"
      type="info"
      show-icon
      :closable="false"
      title="登录态里没有 user id"
      description="确认/取消/改价都要求操作者身份，所以这一页此刻只能看不能改。重新登录通常就能拿回身份。"
    />
    <el-alert
      v-if="loadError"
      class="mb-12"
      :type="loadError === 404 ? 'info' : 'warning'"
      show-icon
      :closable="false"
      :title="loadError === 404 ? '这条草稿不存在（或已被清扫）' : '草稿读取失败'"
      :description="loadError === 404 ? '它可能刚被别人确认/取消，或已过期被清扫器收口。' : '后端回了非 404 的错误，稍后重试；不要当成草稿没了。'"
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
            <span class="mono">{{ draft.customer_id }}</span>
          </el-descriptions-item>
          <el-descriptions-item label="归属 owner_id">
            <span class="mono">{{ draft.owner_id || '—' }}</span>
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
                placeholder="与池子里同客户的同名产品会撞唯一索引（409）"
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
            只把你动过的字段发给后端（没给=不改）；数量至少 1、单价不能为负 —— 要废单请走上方「取消草稿」，不是把数量改成 0。
            保存成功后回读整条草稿，总额会在这里重算刷新。
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
import { useUserStore } from '@/stores/user'

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
const loadError = ref(0) // 0 无 / 404 不存在 / 500 读取失败
const draft = ref(null)
const form = reactive({ product_name: '', quantity: 1, unit_price: 0, note: '' })

const draftId = computed(() => String(route.params.id || ''))

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
  try {
    const d = await orderDraftApi.get(draftId.value)
    draft.value = d
    fillForm(d)
  } catch (err) {
    draft.value = null
    if (err?.status === 404) {
      loadError.value = 404
    } else {
      loadError.value = 500
    }
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
        `草稿已确认，但未拿到订单服务 ⇒ 订单号 ${result.order_id || '（空）'} 是本进程临时生成的，orders 表里没有这一行（需人工补建）`
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
