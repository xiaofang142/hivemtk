<template>
  <div class="crm-companies-page">
    <div class="page-head">
      <div>
        <h2 class="page-title">
          公司主数据
        </h2>
        <span class="page-sub">{{ t('businessCopy.crm.companiesSubtitle') }}</span>
      </div>
      <el-button
        type="primary"
        @click="openCreate"
      >
        新增公司
      </el-button>
    </div>

    <el-card shadow="never">
      <el-form
        inline
        label-width="70px"
        @submit.prevent
      >
        <el-form-item label="关键词">
          <el-input
            v-model="keyword"
            placeholder="公司名称"
            clearable
            style="width: 240px"
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
        empty-text="暂无公司"
      >
        <el-table-column
          prop="id"
          label="ID"
          width="90"
        />
        <el-table-column
          prop="name"
          label="公司名称"
          min-width="180"
          show-overflow-tooltip
        />
        <el-table-column
          prop="region"
          label="地区"
          min-width="120"
        />
        <el-table-column
          prop="industry"
          label="行业"
          min-width="120"
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
              @click="openRelations(row)"
            >
              关联客户
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
      :title="editing ? '编辑公司' : '新增公司'"
      width="560px"
    >
      <el-form
        :model="form"
        label-width="90px"
      >
        <el-form-item label="公司名称">
          <el-input v-model="form.name" />
        </el-form-item>
        <el-form-item label="地区">
          <el-input v-model="form.region" />
        </el-form-item>
        <el-form-item label="行业">
          <el-input v-model="form.industry" />
        </el-form-item>
        <el-form-item label="官网">
          <el-input v-model="form.website" />
        </el-form-item>
        <el-form-item label="备注">
          <el-input
            v-model="form.remark"
            type="textarea"
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

    <el-dialog
      v-model="relationsVisible"
      :title="`公司 ${currentName} 的关联客户`"
      width="560px"
    >
      <el-table
        :data="relations"
        border
        empty-text="暂无关联"
      >
        <el-table-column
          prop="customer_id"
          label="客户ID"
          min-width="200"
        />
        <el-table-column
          prop="relation_type"
          label="关系类型"
          min-width="120"
        />
      </el-table>
      <div class="relation-add">
        <el-input
          v-model="relationCustomerId"
          placeholder="输入客户ID新增关联"
          style="width: 260px"
        />
        <el-button
          type="primary"
          @click="addRelation"
        >
          关联
        </el-button>
      </div>
    </el-dialog>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { companyApi } from '@/api/company'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const loading = ref(false)
const saving = ref(false)
const rows = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const keyword = ref('')
const dialogVisible = ref(false)
const editing = ref(false)
const currentId = ref(0)
const form = reactive({ name: '', region: '', industry: '', website: '', remark: '' })
const relationsVisible = ref(false)
const relations = ref([])
const currentName = ref('')
const relationCustomerId = ref('')

function emptyForm() {
  Object.assign(form, { name: '', region: '', industry: '', website: '', remark: '' })
}

async function fetchList() {
  loading.value = true
  try {
    const res = await companyApi.listCompanies({ keyword: keyword.value || undefined, page: page.value, page_size: pageSize.value })
    rows.value = Array.isArray(res?.list) ? res.list : (Array.isArray(res) ? res : [])
    total.value = Number(res?.total || rows.value.length)
  } catch (e) {
    ElMessage.error(e?.message || '加载公司失败')
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
    name: row.name || '', region: row.region || '', industry: row.industry || '',
    website: row.website || '', remark: row.remark || '',
  })
  dialogVisible.value = true
}

async function save() {
  saving.value = true
  try {
    if (editing.value) {
      await companyApi.updateCompany(currentId.value, form)
    } else {
      await companyApi.createCompany(form)
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
    await ElMessageBox.confirm('确认删除该公司？', '操作确认', { type: 'warning' })
    await companyApi.deleteCompany(row.id)
    ElMessage.success('删除成功')
    fetchList()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(e?.message || '删除失败')
  }
}

async function openRelations(row) {
  currentId.value = row.id
  currentName.value = row.name || ''
  relationCustomerId.value = ''
  try {
    const res = await companyApi.listCompanyRelations(row.id)
    relations.value = Array.isArray(res?.list) ? res.list : (Array.isArray(res) ? res : [])
    relationsVisible.value = true
  } catch (e) {
    ElMessage.error(e?.message || '加载关联失败')
  }
}

async function addRelation() {
  if (!relationCustomerId.value) {
    ElMessage.warning('请输入客户ID')
    return
  }
  try {
    await companyApi.upsertCustomerRelation(relationCustomerId.value, { company_id: currentId.value })
    ElMessage.success('关联成功')
    const res = await companyApi.listCompanyRelations(currentId.value)
    relations.value = Array.isArray(res?.list) ? res.list : (Array.isArray(res) ? res : [])
  } catch (e) {
    ElMessage.error(e?.message || '关联失败')
  }
}

onMounted(fetchList)
</script>

<style scoped>
.crm-companies-page { padding: 16px; }
.page-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; }
.page-title { margin: 0; font-size: 18px; }
.page-sub { color: #909399; font-size: 12px; }
.pagination { margin-top: 12px; justify-content: flex-end; }
.relation-add { display: flex; gap: 8px; margin-top: 12px; }
</style>
