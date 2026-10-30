<template>
  <div class="tm-review">
    <el-card
      shadow="never"
      class="mb-12"
    >
      <template #header>
        <span>{{ $t('templateMarketPage.entryQueue') }}</span>
      </template>
      <div class="toolbar">
        <el-radio-group
          v-model="entryStatus"
          size="small"
          @change="fetchEntries"
        >
          <el-radio-button value="">
            {{ $t('templateMarketPage.filterAll') }}
          </el-radio-button>
          <el-radio-button
            v-for="(label, k) in MARKET_ENTRY_STATUS_LABEL"
            :key="k"
            :value="k"
          >
            {{ label }}
          </el-radio-button>
        </el-radio-group>
        <el-button
          size="small"
          :loading="loading"
          @click="fetchEntries"
        >
          {{ $t('templateMarketPage.refresh') }}
        </el-button>
      </div>
      <el-table
        v-loading="loading"
        :data="entries"
        size="small"
      >
        <el-table-column
          prop="id"
          label="ID"
          min-width="130"
        >
          <template #default="{ row }">
            <span class="mono">{{ row.id }}</span>
          </template>
        </el-table-column>
        <el-table-column
          prop="name"
          :label="$t('templateMarketPage.fieldName')"
          min-width="130"
        />
        <el-table-column
          :label="$t('templateMarketPage.fieldKind')"
          width="80"
        >
          <template #default="{ row }">
            {{ ENTRY_KIND_LABEL[row.kind] || row.kind }}
          </template>
        </el-table-column>
        <el-table-column
          prop="category"
          :label="$t('templateMarketPage.fieldCategory')"
          min-width="110"
        />
        <el-table-column
          :label="$t('templateMarketPage.fieldPrice')"
          width="90"
        >
          <template #default="{ row }">
            {{ marketPriceText(row.price_cents) }}
          </template>
        </el-table-column>
        <el-table-column
          prop="source"
          :label="$t('templateMarketPage.fieldSource')"
          min-width="180"
        >
          <template #default="{ row }">
            <span class="mono">{{ row.source }}</span>
          </template>
        </el-table-column>
        <el-table-column
          :label="$t('templateMarketPage.statusCol')"
          width="90"
        >
          <template #default="{ row }">
            <el-tag
              size="small"
              :type="MARKET_ENTRY_STATUS_TAG_TYPE[row.status] || 'info'"
              effect="plain"
            >
              {{ MARKET_ENTRY_STATUS_LABEL[row.status] || row.status }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="created_at"
          :label="$t('templateMarketPage.submittedAt')"
          min-width="145"
        >
          <template #default="{ row }">
            {{ fmtTime(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column
          :label="$t('templateMarketPage.actionsCol')"
          min-width="200"
          fixed="right"
        >
          <template #default="{ row }">
            <template v-if="row.status === 'pending'">
              <el-button
                size="small"
                type="success"
                @click="act(row, 'approve')"
              >
                {{ $t('templateMarketPage.approve') }}
              </el-button>
              <el-button
                size="small"
                type="danger"
                plain
                @click="openReject(row)"
              >
                {{ $t('templateMarketPage.reject') }}
              </el-button>
            </template>
            <el-button
              v-if="row.status === 'approved'"
              size="small"
              type="warning"
              plain
              @click="act(row, 'disable')"
            >
              {{ $t('templateMarketPage.disable') }}
            </el-button>
            <el-button
              v-if="row.status === 'disabled'"
              size="small"
              type="success"
              plain
              @click="act(row, 'enable')"
            >
              {{ $t('templateMarketPage.enable') }}
            </el-button>
            <span
              v-if="row.status === 'rejected'"
              class="hint"
            >{{ row.reject_reason }}</span>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty
            :description="$t('templateMarketPage.nothingHere')"
            :image-size="60"
          />
        </template>
      </el-table>
    </el-card>

    <el-card shadow="never">
      <template #header>
        <span>{{ $t('templateMarketPage.orderQueue') }}</span>
      </template>
      <div class="toolbar">
        <el-radio-group
          v-model="orderStatus"
          size="small"
          @change="fetchOrders"
        >
          <el-radio-button value="">
            {{ $t('templateMarketPage.filterAll') }}
          </el-radio-button>
          <el-radio-button
            v-for="(label, k) in MARKET_ORDER_STATUS_LABEL"
            :key="k"
            :value="k"
          >
            {{ label }}
          </el-radio-button>
        </el-radio-group>
        <el-button
          size="small"
          :loading="loading"
          @click="fetchOrders"
        >
          {{ $t('templateMarketPage.refresh') }}
        </el-button>
      </div>
      <el-table
        v-loading="loading"
        :data="orders"
        size="small"
      >
        <el-table-column
          prop="order_no"
          :label="$t('templateMarketPage.orderNo')"
          min-width="150"
        >
          <template #default="{ row }">
            <span class="mono">{{ row.order_no }}</span>
          </template>
        </el-table-column>
        <el-table-column
          prop="entry_name"
          :label="$t('templateMarketPage.fieldName')"
          min-width="130"
        />
        <el-table-column
          prop="buyer_id"
          :label="$t('templateMarketPage.buyer')"
          width="90"
        />
        <el-table-column
          prop="contact_phone"
          :label="$t('templateMarketPage.contactPhone')"
          min-width="120"
        >
          <template #default="{ row }">
            {{ row.contact_phone || '—' }}
          </template>
        </el-table-column>
        <el-table-column
          :label="$t('templateMarketPage.amount')"
          width="100"
        >
          <template #default="{ row }">
            {{ marketPriceText(row.amount_cents) }}
          </template>
        </el-table-column>
        <el-table-column
          :label="$t('templateMarketPage.statusCol')"
          width="95"
        >
          <template #default="{ row }">
            <el-tag
              size="small"
              :type="MARKET_ORDER_STATUS_TAG_TYPE[row.status] || 'info'"
              effect="plain"
            >
              {{ MARKET_ORDER_STATUS_LABEL[row.status] || row.status }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="created_at"
          :label="$t('templateMarketPage.createdAtCol')"
          min-width="145"
        >
          <template #default="{ row }">
            {{ fmtTime(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column
          :label="$t('templateMarketPage.actionsCol')"
          width="120"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button
              v-if="row.status === 'pending_payment'"
              size="small"
              type="primary"
              @click="markPaid(row)"
            >
              {{ $t('templateMarketPage.markPaid') }}
            </el-button>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty
            :description="$t('templateMarketPage.nothingHere')"
            :image-size="60"
          />
        </template>
      </el-table>
    </el-card>

    <el-dialog
      v-model="rejectVisible"
      :title="$t('templateMarketPage.reject')"
      width="420px"
    >
      <el-input
        v-model="rejectReason"
        type="textarea"
        :rows="3"
        :placeholder="$t('templateMarketPage.rejectReasonRequired')"
      />
      <template #footer>
        <el-button @click="rejectVisible = false">
          {{ $t('templateMarketPage.cancelBtn') }}
        </el-button>
        <el-button
          type="danger"
          @click="confirmReject"
        >
          {{ $t('templateMarketPage.reject') }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import {
  marketReviewList, marketReviewAction, marketAdminOrders, marketMarkPaid,
  marketPriceText, ENTRY_KIND_LABEL,
  MARKET_ENTRY_STATUS_LABEL, MARKET_ENTRY_STATUS_TAG_TYPE,
  MARKET_ORDER_STATUS_LABEL, MARKET_ORDER_STATUS_TAG_TYPE
} from '@/api/templateMarket'

// 审核台（S-22）：条目审核（通过/驳回/下架/复架）+ 订单收款确认。
// 审核是公开橱窗的唯一写通道；收款确认是线下到账的显式落账动作。
const { t } = useI18n()

const loading = ref(false)
const entryStatus = ref('')
const orderStatus = ref('')
const entries = ref([])
const orders = ref([])

const rejectVisible = ref(false)
const rejectReason = ref('')
const rejectTarget = ref(null)

function fmtTime (v) {
  if (!v) return '—'
  return String(v).replace('T', ' ').slice(0, 19)
}

async function fetchEntries () {
  loading.value = true
  try {
    const res = await marketReviewList(
      entryStatus.value ? { status: entryStatus.value } : undefined,
      { _silent: true })
    entries.value = res?.list || []
  } finally {
    loading.value = false
  }
}

async function fetchOrders () {
  loading.value = true
  try {
    const res = await marketAdminOrders(
      orderStatus.value ? { status: orderStatus.value } : undefined,
      { _silent: true })
    orders.value = res?.list || []
  } finally {
    loading.value = false
  }
}

async function act (row, action) {
  try {
    if (action === 'approve') {
      await ElMessageBox.confirm(t('templateMarketPage.confirmApprove'), t('templateMarketPage.approve'), { type: 'info' })
    } else if (action === 'disable') {
      await ElMessageBox.confirm(t('templateMarketPage.confirmDisable'), t('templateMarketPage.disable'), { type: 'warning' })
    }
    await marketReviewAction(row.id, { action })
    ElMessage.success(t('templateMarketPage.reviewOk'))
    fetchEntries()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error((e?.message) || String(e))
  }
}

function openReject (row) {
  rejectTarget.value = row
  rejectReason.value = ''
  rejectVisible.value = true
}

async function confirmReject () {
  if (!rejectReason.value.trim()) {
    ElMessage.warning(t('templateMarketPage.rejectReasonRequired'))
    return
  }
  await marketReviewAction(rejectTarget.value.id, { action: 'reject', reason: rejectReason.value.trim() })
  rejectVisible.value = false
  ElMessage.success(t('templateMarketPage.reviewOk'))
  fetchEntries()
}

async function markPaid (row) {
  try {
    await ElMessageBox.confirm(
      t('templateMarketPage.confirmMarkPaid'), t('templateMarketPage.markPaid'), { type: 'warning' })
    await marketMarkPaid(row.order_no)
    ElMessage.success(t('templateMarketPage.markPaidOk'))
    fetchOrders()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error((e?.message) || String(e))
  }
}

onMounted(() => {
  fetchEntries()
  fetchOrders()
})
</script>

<style scoped>
.mb-12 { margin-bottom: 12px; }
.mono { font-family: monospace; font-size: 12px; }
.toolbar { display: flex; justify-content: space-between; margin-bottom: 10px; }
.hint { color: var(--el-text-color-secondary); font-size: 12px; }
</style>
