<template>
  <div class="lead-scoped-list-page">
    <div class="page-head">
      <h2 class="page-title">
        权限内客户与消息
      </h2>
      <span class="page-sub">{{ t('businessCopy.leads.scopedSubtitle') }}</span>
    </div>

    <el-card shadow="never">
      <el-tabs
        v-model="activeTab"
        @tab-change="reload"
      >
        <el-tab-pane
          label="客户列表"
          name="customers"
        >
          <el-form
            inline
            label-width="60px"
            @submit.prevent
          >
            <el-form-item label="关键词">
              <el-input
                v-model="customerFilters.keyword"
                placeholder="姓名/手机/邮箱"
                clearable
                style="width: 220px"
                @keyup.enter="reload"
              />
            </el-form-item>
            <el-form-item>
              <el-button
                type="primary"
                :loading="loading"
                @click="reload"
              >
                查询
              </el-button>
            </el-form-item>
          </el-form>
          <el-table
            v-loading="loading"
            :data="customers"
            border
            stripe
            empty-text="权限内暂无客户"
          >
            <el-table-column
              prop="id"
              label="客户ID"
              min-width="180"
              show-overflow-tooltip
            />
            <el-table-column
              prop="name"
              label="姓名"
              min-width="140"
            />
            <el-table-column
              prop="phone"
              label="手机号"
              min-width="140"
            />
            <el-table-column
              prop="industry"
              label="行业"
              min-width="110"
            />
            <el-table-column
              label="来源账号"
              min-width="140"
              show-overflow-tooltip
            >
              <template #default="{ row }">
                {{ catalog.accountLabel(row.source_account_id) || '-' }}
              </template>
            </el-table-column>
          </el-table>
          <el-pagination
            v-if="customerTotal > limit"
            v-model:current-page="customerPage"
            :page-size="limit"
            :total="customerTotal"
            layout="prev, pager, next, total"
            class="pagination"
            @current-change="fetchCustomers"
          />
        </el-tab-pane>

        <el-tab-pane
          label="消息列表"
          name="messages"
        >
          <el-form
            inline
            label-width="60px"
            @submit.prevent
          >
            <el-form-item label="平台">
              <ChannelSelect
                v-model="messageFilters.platform"
                placeholder="渠道平台"
                clearable
              />
            </el-form-item>
            <el-form-item label="线索角色">
              <el-input
                v-model="messageFilters.lead_role"
                placeholder="lead/customer"
                clearable
                style="width: 160px"
              />
            </el-form-item>
            <el-form-item label="命中">
              <el-select
                v-model="messageFilters.matched"
                clearable
                placeholder="全部"
                style="width: 120px"
              >
                <el-option
                  label="命中"
                  value="1"
                />
                <el-option
                  label="未命中"
                  value="0"
                />
              </el-select>
            </el-form-item>
            <el-form-item>
              <el-button
                type="primary"
                :loading="loading"
                @click="reload"
              >
                查询
              </el-button>
            </el-form-item>
          </el-form>
          <el-table
            v-loading="loading"
            :data="messages"
            border
            stripe
            empty-text="权限内暂无消息"
          >
            <el-table-column
              prop="id"
              label="消息ID"
              min-width="180"
              show-overflow-tooltip
            />
            <el-table-column
              label="平台"
              min-width="120"
            >
              <template #default="{ row }">
                {{ catalog.channelLabel(row.platform) }}
              </template>
            </el-table-column>
            <el-table-column
              prop="lead_role"
              label="角色"
              min-width="100"
            />
            <el-table-column
              prop="content"
              label="内容"
              min-width="260"
              show-overflow-tooltip
            />
            <el-table-column
              label="判定来源"
              min-width="110"
            >
              <template #default="{ row }">
                <el-tag
                  v-if="row.degraded"
                  size="small"
                  type="warning"
                >
                  降级判定
                </el-tag>
                <el-tag
                  v-else
                  size="small"
                  type="success"
                >
                  LLM
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              prop="created_at"
              label="时间"
              min-width="160"
            />
          </el-table>
          <el-pagination
            v-if="messageTotal > limit"
            v-model:current-page="messagePage"
            :page-size="limit"
            :total="messageTotal"
            layout="prev, pager, next, total"
            class="pagination"
            @current-change="fetchMessages"
          />
        </el-tab-pane>
      </el-tabs>
    </el-card>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { leadsApi } from '@/api/leads'
import { useI18n } from 'vue-i18n'
import ChannelSelect from '@/components/ChannelSelect.vue'
import { useChannelCatalogStore } from '@/stores/channelCatalog'

const { t } = useI18n()
const catalog = useChannelCatalogStore()

const activeTab = ref('customers')
const loading = ref(false)
const limit = 20
const customers = ref([])
const customerTotal = ref(0)
const customerPage = ref(1)
const customerFilters = reactive({ keyword: '' })
const messages = ref([])
const messageTotal = ref(0)
const messagePage = ref(1)
const messageFilters = reactive({ platform: '', lead_role: '', matched: '' })

async function fetchCustomers() {
  loading.value = true
  try {
    const res = await leadsApi.listScopedCustomers({
      keyword: customerFilters.keyword || undefined,
      page: customerPage.value,
      limit,
    })
    customers.value = Array.isArray(res?.list) ? res.list : []
    customerTotal.value = Number(res?.total || 0)
  } catch (e) {
    ElMessage.error(e?.message || '加载客户失败')
  } finally {
    loading.value = false
  }
}

async function fetchMessages() {
  loading.value = true
  try {
    const res = await leadsApi.listScopedMessages({
      platform: messageFilters.platform || undefined,
      lead_role: messageFilters.lead_role || undefined,
      matched: messageFilters.matched || undefined,
      page: messagePage.value,
      limit,
    })
    messages.value = Array.isArray(res?.list) ? res.list : []
    messageTotal.value = Number(res?.total || 0)
  } catch (e) {
    ElMessage.error(e?.message || '加载消息失败')
  } finally {
    loading.value = false
  }
}

function reload() {
  if (activeTab.value === 'customers') {
    customerPage.value = 1
    fetchCustomers()
  } else {
    messagePage.value = 1
    fetchMessages()
  }
}

onMounted(async () => {
  await catalog.ensure()
  await fetchCustomers()
})
</script>

<style scoped>
.lead-scoped-list-page { padding: 16px; }
.page-head { margin-bottom: 12px; }
.page-title { margin: 0; font-size: 18px; }
.page-sub { color: #909399; font-size: 12px; }
.pagination { margin-top: 12px; justify-content: flex-end; }
</style>
