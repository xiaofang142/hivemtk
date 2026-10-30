<template>
  <div class="tm-my">
    <el-card
      shadow="never"
      class="mb-12"
    >
      <template #header>
        <div class="card-head">
          <span>{{ $t('templateMarketPage.ownedTitle') }}</span>
        </div>
      </template>
      <el-table
        v-loading="loading"
        :data="owned"
        size="small"
      >
        <el-table-column
          prop="entry.name"
          :label="$t('templateMarketPage.fieldName')"
          min-width="160"
        />
        <el-table-column
          prop="entry.id"
          label="ID"
          min-width="140"
        >
          <template #default="{ row }">
            <span class="mono">{{ row.entry.id }}</span>
          </template>
        </el-table-column>
        <el-table-column
          :label="$t('templateMarketPage.orderNo')"
          min-width="150"
        >
          <template #default="{ row }">
            <span class="mono">{{ row.order_no }}</span>
          </template>
        </el-table-column>
        <el-table-column
          :label="$t('templateMarketPage.obtainedAt')"
          min-width="150"
        >
          <template #default="{ row }">
            {{ fmtTime(row.obtained_at) }}
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

    <el-card
      shadow="never"
      class="mb-12"
    >
      <template #header>
        <span>{{ $t('templateMarketPage.ordersTitle') }}</span>
      </template>
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
          min-width="140"
        />
        <el-table-column
          :label="$t('templateMarketPage.amount')"
          width="110"
        >
          <template #default="{ row }">
            {{ marketPriceText(row.amount_cents) }}
          </template>
        </el-table-column>
        <el-table-column
          :label="$t('templateMarketPage.statusCol')"
          width="100"
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
          min-width="150"
        >
          <template #default="{ row }">
            {{ fmtTime(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column
          :label="$t('templateMarketPage.actionsCol')"
          width="120"
        >
          <template #default="{ row }">
            <el-button
              v-if="row.status === 'pending_payment'"
              size="small"
              @click="cancel(row)"
            >
              {{ $t('templateMarketPage.cancelOrder') }}
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

    <el-card shadow="never">
      <template #header>
        <div class="card-head">
          <span>{{ $t('templateMarketPage.submissionsTitle') }}</span>
          <el-button
            size="small"
            type="primary"
            @click="$router.push('/template-market')"
          >
            {{ $t('templateMarketPage.publishCta') }}
          </el-button>
        </div>
      </template>
      <el-table
        v-loading="loading"
        :data="submissions"
        size="small"
      >
        <el-table-column
          prop="id"
          label="ID"
          min-width="140"
        >
          <template #default="{ row }">
            <span class="mono">{{ row.id }}</span>
          </template>
        </el-table-column>
        <el-table-column
          prop="name"
          :label="$t('templateMarketPage.fieldName')"
          min-width="140"
        />
        <el-table-column
          :label="$t('templateMarketPage.fieldKind')"
          width="90"
        >
          <template #default="{ row }">
            {{ ENTRY_KIND_LABEL[row.kind] || row.kind }}
          </template>
        </el-table-column>
        <el-table-column
          :label="$t('templateMarketPage.statusCol')"
          width="100"
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
          prop="reject_reason"
          :label="$t('templateMarketPage.rejectReason')"
          min-width="160"
        >
          <template #default="{ row }">
            {{ row.reject_reason || '—' }}
          </template>
        </el-table-column>
        <el-table-column
          prop="created_at"
          :label="$t('templateMarketPage.createdAtCol')"
          min-width="150"
        >
          <template #default="{ row }">
            {{ fmtTime(row.created_at) }}
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
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import {
  myMarketOwned, myMarketOrders, myMarketSubmissions, cancelMarketOrder,
  marketPriceText, ENTRY_KIND_LABEL,
  MARKET_ENTRY_STATUS_LABEL, MARKET_ENTRY_STATUS_TAG_TYPE,
  MARKET_ORDER_STATUS_LABEL, MARKET_ORDER_STATUS_TAG_TYPE
} from '@/api/templateMarket'

// 我的工作台（S-22）：已获得 / 购买订单 / 我的发布三张表。
// 读的都是登录用户自己的数据（后端按 user_id 收口，订单另有租户隔离）。
const { t } = useI18n()

const loading = ref(false)
const owned = ref([])
const orders = ref([])
const submissions = ref([])

function fmtTime (v) {
  if (!v) return '—'
  return String(v).replace('T', ' ').slice(0, 19)
}

async function cancel (row) {
  try {
    await ElMessageBox.confirm(t('templateMarketPage.confirmCancel'), t('templateMarketPage.cancelOrder'), { type: 'warning' })
    await cancelMarketOrder(row.order_no)
    ElMessage.success(t('templateMarketPage.cancelOk'))
    fetch()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error((e?.message) || String(e))
  }
}

async function fetch () {
  loading.value = true
  try {
    const [o, od, sub] = await Promise.all([
      myMarketOwned({ _silent: true }),
      myMarketOrders({ _silent: true }),
      myMarketSubmissions({ _silent: true })
    ])
    owned.value = o?.list || []
    orders.value = od?.list || []
    submissions.value = sub?.list || []
  } finally {
    loading.value = false
  }
}

onMounted(fetch)
</script>

<style scoped>
.mb-12 { margin-bottom: 12px; }
.mono { font-family: monospace; font-size: 12px; }
.card-head { display: flex; justify-content: space-between; align-items: center; }
.hint { color: var(--el-text-color-secondary); font-size: 12px; }
</style>
