<template>
  <div class="crm-page">
    <div class="page-head">
      <div>
        <h2 class="page-title">
          客户统一主数据
        </h2>
        <span class="page-sub">列表按账号 / 行业 / 负责人数据权限自动过滤</span>
      </div>
      <el-button
        type="primary"
        @click="openCreate"
      >
        新增客户
      </el-button>
    </div>

    <WorkflowNavigation
      owner="/crm/list"
      :links="workflowLinks"
    />
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
            style="width: 220px"
            @keyup.enter="reload"
          />
        </el-form-item>
        <el-form-item label="行业">
          <el-input
            v-model="filters.industry"
            placeholder="行业"
            clearable
            style="width: 160px"
          />
        </el-form-item>
        <el-form-item label="负责人ID">
          <el-input
            v-model="filters.owner_user_id"
            placeholder="可选"
            clearable
            style="width: 140px"
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
          <el-button @click="reset">
            重置
          </el-button>
        </el-form-item>
      </el-form>

      <el-table
        v-loading="loading"
        :data="rows"
        border
        stripe
        empty-text="暂无客户"
      >
        <el-table-column
          prop="name"
          label="姓名"
          min-width="130"
          show-overflow-tooltip
        />
        <el-table-column
          prop="phone"
          label="手机号"
          min-width="140"
          show-overflow-tooltip
        />
        <el-table-column
          prop="email"
          label="邮箱"
          min-width="160"
          show-overflow-tooltip
        />
        <el-table-column
          prop="industry"
          label="行业"
          min-width="110"
        />
        <el-table-column
          prop="source_account_id"
          label="来源账号"
          min-width="140"
          show-overflow-tooltip
        />
        <el-table-column
          prop="owner_user_id"
          label="负责人"
          min-width="90"
        />
        <el-table-column
          prop="merged_into_id"
          label="合并指向"
          min-width="120"
          show-overflow-tooltip
        />
        <el-table-column
          label="操作"
          width="220"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button
              link
              type="primary"
              @click="view360(row)"
            >
              360
            </el-button>
            <el-button
              link
              type="primary"
              @click="openEdit(row)"
            >
              编辑
            </el-button>
            <el-button
              link
              type="danger"
              @click="remove(row)"
            >
              删除
            </el-button>
          </template>
        </el-table-column>
      </el-table>

      <el-pagination
        v-if="total > pageSize"
        v-model:current-page="page"
        :page-size="pageSize"
        :total="total"
        layout="prev, pager, next, total"
        class="pagination"
        @current-change="fetchList"
      />
    </el-card>

    <el-dialog
      v-model="dialogVisible"
      :title="editing ? '编辑客户' : '新增客户'"
      width="560px"
    >
      <el-form
        :model="form"
        label-width="90px"
      >
        <el-form-item label="姓名">
          <el-input v-model="form.name" />
        </el-form-item>
        <el-form-item label="手机号">
          <el-input v-model="form.phone" />
        </el-form-item>
        <el-form-item label="邮箱">
          <el-input v-model="form.email" />
        </el-form-item>
        <el-form-item label="行业">
          <el-input v-model="form.industry" />
        </el-form-item>
        <el-form-item label="来源账号">
          <el-input v-model="form.source_account_id" />
        </el-form-item>
        <el-form-item label="来源渠道">
          <el-input v-model="form.source_channel" />
        </el-form-item>
        <el-form-item label="负责人ID">
          <el-input-number
            v-model="form.owner_user_id"
            :min="0"
          />
        </el-form-item>
        <el-form-item label="标签">
          <el-input
            v-model="form.tags"
            placeholder="JSON 数组，如 [&quot;高意向&quot;]"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          :loading="saving"
          @click="save"
        >
          保存
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useRouter } from 'vue-router'
import { crmApi } from '@/api/crm'
import WorkflowNavigation from '@/components/WorkflowNavigation.vue'

const workflowLinks = [
  { key: 'dedupe', path: '/crm/dedupe' },
  { key: 'importExport', path: '/crm/import-export' },
  { key: 'contacts', path: '/crm/contacts' },
  { key: 'suppliers', path: '/crm/suppliers' },
  { key: 'serviceProviders', path: '/crm/service-providers' },
  { key: 'companies', path: '/crm/companies' }
]

const emit = defineEmits(['view-360'])
const router = useRouter()
const loading = ref(false)
const saving = ref(false)
const rows = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const filters = reactive({ keyword: '', industry: '', owner_user_id: '' })
const dialogVisible = ref(false)
const editing = ref(false)
const currentId = ref('')
const form = reactive({
  name: '', phone: '', email: '', industry: '', source_account_id: '',
  source_channel: '', owner_user_id: 0, tags: '[]',
})

function emptyForm() {
  Object.assign(form, {
    name: '', phone: '', email: '', industry: '', source_account_id: '',
    source_channel: '', owner_user_id: 0, tags: '[]',
  })
}

async function fetchList() {
  loading.value = true
  try {
    const res = await crmApi.listCustomers({
      keyword: filters.keyword || undefined,
      industry: filters.industry || undefined,
      owner_user_id: filters.owner_user_id || undefined,
      page: page.value,
      page_size: pageSize.value,
    })
    rows.value = Array.isArray(res?.list) ? res.list : []
    total.value = Number(res?.total || 0)
  } catch (e) {
    ElMessage.error(e?.message || '加载客户失败')
  } finally {
    loading.value = false
  }
}

function reload() {
  page.value = 1
  fetchList()
}

function reset() {
  filters.keyword = ''
  filters.industry = ''
  filters.owner_user_id = ''
  reload()
}

function openCreate() {
  editing.value = false
  currentId.value = ''
  emptyForm()
  dialogVisible.value = true
}

function openEdit(row) {
  editing.value = true
  currentId.value = row.id
  Object.assign(form, {
    name: row.name || '', phone: row.phone || '', email: row.email || '',
    industry: row.industry || '', source_account_id: row.source_account_id || '',
    source_channel: row.source_channel || '', owner_user_id: Number(row.owner_user_id || 0),
    tags: row.tags || '[]',
  })
  dialogVisible.value = true
}

async function save() {
  saving.value = true
  try {
    const payload = { ...form }
    if (editing.value) {
      await crmApi.updateCustomer(currentId.value, payload)
    } else {
      await crmApi.createCustomer(payload)
    }
    ElMessage.success('保存成功')
    dialogVisible.value = false
    fetchList()
  } catch (e) {
    ElMessage.error(e?.message || '保存失败')
  } finally {
    saving.value = false
  }
}

async function remove(row) {
  try {
    await ElMessageBox.confirm(`确认删除客户「${row.name}」？`, '提示', { type: 'warning' })
    await crmApi.deleteCustomer(row.id)
    ElMessage.success('已删除')
    fetchList()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(e?.message || '删除失败')
  }
}

function view360(row) {
  emit('view-360', row.id)
  router.push({ path: `/crm/profile360/${row.id}` })
}

onMounted(fetchList)
</script>

<style scoped>
.crm-page { padding: 16px; }
.page-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; }
.page-title { font-size: 18px; margin: 0 0 4px; }
.page-sub { font-size: 12px; color: #909399; }
.pagination { margin-top: 14px; justify-content: flex-end; }
</style>
