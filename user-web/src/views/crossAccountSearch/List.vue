<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h2 class="page-title">
          跨账号客户检索
        </h2>
        <span class="page-sub">在授权账号范围内按关键词/行业检索客户（PR-8）</span>
      </div>
    </div>

    <el-card shadow="never">
      <el-form
        inline
        label-width="70px"
        @submit.prevent
      >
        <el-form-item label="关键词">
          <el-input
            v-model="filters.keyword"
            placeholder="姓名/手机/邮箱"
            clearable
            style="width: 240px"
            @keyup.enter="page=1;search()"
          />
        </el-form-item>
        <el-form-item label="行业">
          <el-input
            v-model="filters.industry"
            placeholder="行业（可选）"
            clearable
            style="width: 200px"
            @keyup.enter="page=1;search()"
          />
        </el-form-item>
        <el-form-item>
          <el-button
            type="primary"
            :loading="loading"
            @click="page=1;search()"
          >
            检索
          </el-button>
          <el-button @click="reset">
            重置
          </el-button>
        </el-form-item>
      </el-form>

      <el-alert
        :title="scopeTitle"
        type="info"
        show-icon
        :closable="false"
        class="mb12"
      />

      <el-alert
        v-if="errorMsg"
        :title="errorMsg"
        type="error"
        show-icon
        closable
        class="mb12"
        @close="errorMsg=''"
      />

      <el-table
        v-loading="loading"
        :data="rows"
        empty-text="暂无结果"
      >
        <el-table-column
          prop="id"
          label="客户ID"
          min-width="140"
          show-overflow-tooltip
        />
        <el-table-column
          prop="name"
          label="姓名"
          min-width="140"
          show-overflow-tooltip
        />
        <el-table-column
          prop="phone"
          label="手机号"
          min-width="140"
        />
        <el-table-column
          prop="email"
          label="邮箱"
          min-width="180"
          show-overflow-tooltip
        />
        <el-table-column
          label="来源渠道"
          width="120"
        >
          <template #default="{ row }">
            {{ catalog.channelLabel(row.source_channel) || '-' }}
          </template>
        </el-table-column>
        <el-table-column
          label="来源账号"
          min-width="160"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            {{ catalog.accountLabel(row.source_account_id) || '-' }}
          </template>
        </el-table-column>
        <el-table-column
          prop="company_id"
          label="公司ID"
          width="100"
        >
          <template #default="{ row }">
            {{ row.company_id ?? '-' }}
          </template>
        </el-table-column>
      </el-table>

      <div class="pager">
        <el-pagination
          v-model:current-page="page"
          v-model:page-size="pageSize"
          :total="total"
          layout="total, prev, pager, next, sizes"
          :page-sizes="[10, 20, 50, 100]"
          @current-change="search"
          @size-change="page=1;search()"
        />
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { crossAccountSearchApi } from '@/api/crossAccountSearch'
import { useChannelCatalogStore } from '@/stores/channelCatalog'

const catalog = useChannelCatalogStore()

const loading = ref(false)
const rows = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const errorMsg = ref('')
const scope = ref({ all_accounts: false, restricted: false, authorized_account_count: 0 })
const filters = reactive({ keyword: '', industry: '' })

const scopeTitle = computed(() => {
  if (scope.value?.all_accounts) {
    return '检索范围：全部授权账号（管理员/全量数据权限）；手机号/邮箱已脱敏展示。'
  }
  const count = Number(scope.value?.authorized_account_count || 0)
  if (scope.value?.restricted && count === 0) {
    return '检索范围：当前无授权业务账号，结果为空；手机号/邮箱已脱敏展示。'
  }
  return `检索范围：已授权 ${count} 个业务账号；手机号/邮箱已脱敏展示。`
})

async function search() {
  loading.value = true
  errorMsg.value = ''
  try {
    const res = await crossAccountSearchApi.searchCustomers({
      keyword: filters.keyword.trim() || undefined,
      industry: filters.industry.trim() || undefined,
      page: page.value,
      page_size: pageSize.value,
    })
    rows.value = Array.isArray(res?.list) ? res.list : []
    total.value = Number(res?.total || 0)
    if (res?.scope && typeof res.scope === 'object') {
      scope.value = {
        all_accounts: !!res.scope.all_accounts,
        restricted: !!res.scope.restricted,
        authorized_account_count: Number(res.scope.authorized_account_count || 0),
      }
    }
  } catch (e) {
    rows.value = []
    total.value = 0
    errorMsg.value = e?.message || '检索失败，请稍后重试'
  } finally {
    loading.value = false
  }
}

function reset() {
  filters.keyword = ''
  filters.industry = ''
  page.value = 1
  search()
}

onMounted(() => {
  catalog.ensure()
  search()
})
</script>

<style scoped>
.page { padding: 16px; }
.page-head { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 16px; gap: 12px; }
.page-title { margin: 0; font-size: 20px; font-weight: 600; }
.page-sub { color: var(--el-text-color-secondary); font-size: 13px; }
.mb12 { margin-bottom: 12px; }
.pager { margin-top: 12px; display: flex; justify-content: flex-end; }
</style>
