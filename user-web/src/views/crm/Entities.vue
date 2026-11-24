<template>
  <div class="crm-entities-page">
    <div class="page-head">
      <div>
        <h2 class="page-title">
          {{ typeMeta.title }}
        </h2>
        <span class="page-sub">{{ typeMeta.subtitle }}（US1 补缺实体）</span>
      </div>
      <el-button
        type="primary"
        :disabled="!customerId"
        @click="openCreate"
      >
        新增
      </el-button>
    </div>

    <el-card shadow="never">
      <el-form
        inline
        label-width="70px"
        @submit.prevent
      >
        <el-form-item label="客户ID">
          <el-input
            v-model="customerId"
            placeholder="先输入客户ID再管理其关联实体"
            clearable
            style="width: 320px"
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
        :data="rows"
        border
        stripe
        empty-text="暂无数据"
      >
        <el-table-column
          prop="id"
          label="ID"
          width="90"
        />
        <el-table-column
          prop="name"
          label="名称"
          min-width="150"
          show-overflow-tooltip
        />
        <el-table-column
          prop="role"
          label="角色"
          min-width="120"
        />
        <el-table-column
          prop="contact_info"
          label="联系方式"
          min-width="180"
          show-overflow-tooltip
        />
        <el-table-column
          prop="remark"
          label="备注"
          min-width="160"
          show-overflow-tooltip
        />
        <el-table-column
          prop="source"
          label="来源"
          min-width="110"
        />
        <el-table-column
          label="操作"
          width="160"
          fixed="right"
        >
          <template #default="{ row }">
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
      :title="editing ? '编辑' : '新增'"
      width="560px"
    >
      <el-form
        :model="form"
        label-width="90px"
      >
        <el-form-item label="名称">
          <el-input v-model="form.name" />
        </el-form-item>
        <el-form-item label="角色">
          <el-input v-model="form.role" />
        </el-form-item>
        <el-form-item label="联系方式">
          <el-input
            v-model="form.contact_info"
            placeholder="JSON 对象，如 {&quot;phone&quot;:&quot;138...&quot;}"
          />
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="form.remark" />
        </el-form-item>
        <el-form-item label="来源">
          <el-input v-model="form.source" />
        </el-form-item>
        <el-form-item label="来源标识">
          <el-input v-model="form.source_ref" />
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
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useRoute } from 'vue-router'
import { crmApi } from '@/api/crm'

const route = useRoute()
const type = computed(() => (route.meta.entityType || 'contact'))
const typeMeta = computed(() => ({
  contact: { title: '联系人', subtitle: '客户联系人', list: crmApi.listContacts, create: crmApi.createContact, update: crmApi.updateContact, del: crmApi.deleteContact },
  supplier: { title: '供应商', subtitle: '客户供应商', list: crmApi.listSuppliers, create: crmApi.createSupplier, update: crmApi.updateSupplier, del: crmApi.deleteSupplier },
  serviceProvider: { title: '服务商', subtitle: '客户服务商', list: crmApi.listServiceProviders, create: crmApi.createServiceProvider, update: crmApi.updateServiceProvider, del: crmApi.deleteServiceProvider },
}[type.value]))

const loading = ref(false)
const saving = ref(false)
const rows = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const customerId = ref(String(route.query.customer_id || ''))
const dialogVisible = ref(false)
const editing = ref(false)
const currentId = ref(0)
const form = reactive({ name: '', role: '', contact_info: '{}', remark: '', source: '', source_ref: '' })

function emptyForm() {
  Object.assign(form, { name: '', role: '', contact_info: '{}', remark: '', source: '', source_ref: '' })
}

async function fetchList() {
  if (!customerId.value) {
    rows.value = []
    total.value = 0
    return
  }
  loading.value = true
  try {
    const res = await typeMeta.value.list(customerId.value, { page: page.value, page_size: pageSize.value })
    rows.value = Array.isArray(res?.list) ? res.list : []
    total.value = Number(res?.total || 0)
  } catch (e) {
    ElMessage.error(e?.message || '加载失败')
  } finally {
    loading.value = false
  }
}

function reload() {
  page.value = 1
  fetchList()
}

function openCreate() {
  editing.value = false
  currentId.value = 0
  emptyForm()
  dialogVisible.value = true
}

function openEdit(row) {
  editing.value = true
  currentId.value = row.id
  Object.assign(form, {
    name: row.name || '', role: row.role || '', contact_info: row.contact_info || '{}',
    remark: row.remark || '', source: row.source || '', source_ref: row.source_ref || '',
  })
  dialogVisible.value = true
}

async function save() {
  saving.value = true
  try {
    const payload = { ...form, customer_id: customerId.value }
    if (editing.value) {
      await typeMeta.value.update(currentId.value, payload)
    } else {
      await typeMeta.value.create(payload)
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
    await ElMessageBox.confirm('确认删除？', '操作确认', { type: 'warning' })
    await typeMeta.value.del(row.id)
    ElMessage.success('删除成功')
    fetchList()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(e?.message || '删除失败')
  }
}

onMounted(fetchList)
</script>

<style scoped>
.crm-entities-page { padding: 16px; }
.page-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; }
.page-title { margin: 0; font-size: 18px; }
.page-sub { color: #909399; font-size: 12px; }
.pagination { margin-top: 12px; justify-content: flex-end; }
</style>
