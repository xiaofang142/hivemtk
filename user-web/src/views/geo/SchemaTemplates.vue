<template>
  <div class="geo-schema-templates">
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>🧩 Schema 模板库</span>
          <el-button type="primary" @click="openDialog()">+ 新增模板</el-button>
        </div>
      </template>

      <el-row :gutter="8" style="margin-bottom:12px">
        <el-col :span="6">
          <el-select v-model="filterPageType" placeholder="PageType 过滤" clearable style="width:100%" @change="load">
            <el-option label="article" value="article" />
            <el-option label="faq" value="faq" />
            <el-option label="guide" value="guide" />
            <el-option label="product" value="product" />
          </el-select>
        </el-col>
        <el-col :span="6">
          <el-select v-model="filterSchemaType" placeholder="SchemaType 过滤" clearable style="width:100%" @change="load">
            <el-option label="FAQPage" value="FAQPage" />
            <el-option label="Product" value="Product" />
            <el-option label="Article" value="Article" />
            <el-option label="Comparison" value="Comparison" />
            <el-option label="Organization" value="Organization" />
          </el-select>
        </el-col>
      </el-row>

      <el-table :data="list" border stripe size="small">
        <el-table-column label="PageType" width="100">
          <template #default="{ row }">
            <el-tag size="small">{{ row.page_type }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="Schema @type" width="140">
          <template #default="{ row }">
            <el-tag size="small" type="success">{{ row.schema_type }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="模板 JSON 预览">
          <template #default="{ row }">
            <code class="preview">{{ jsonPreview(row.template_json) }}</code>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="80">
          <template #default="{ row }">
            <el-tag :type="row.active ? 'success' : 'info'" size="small">{{ row.active ? '启用' : '停用' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="160" fixed="right">
          <template #default="{ row }">
            <el-button size="small" @click="openDialog(row)">编辑</el-button>
            <el-button size="small" type="danger" @click="remove(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>

      <div class="pager">
        <el-pagination
          v-model:current-page="page"
          v-model:page-size="limit"
          :total="total"
          :page-sizes="[20, 50]"
          layout="total, sizes, prev, pager, next"
          @size-change="load"
          @current-change="load"
        />
      </div>
    </el-card>

    <el-dialog v-model="dialogVisible" :title="isEdit ? '编辑模板' : '新增模板'" width="640px">
      <el-form :model="form" label-width="110px">
        <el-form-item label="PageType" required>
          <el-select v-model="form.page_type" :disabled="isEdit" style="width:100%">
            <el-option label="article" value="article" />
            <el-option label="faq" value="faq" />
            <el-option label="guide" value="guide" />
            <el-option label="product" value="product" />
          </el-select>
        </el-form-item>
        <el-form-item label="Schema @type" required>
          <el-input v-model="form.schema_type" placeholder="FAQPage" :disabled="isEdit" />
        </el-form-item>
        <el-form-item label="模板 JSON">
          <el-input
            v-model="form.template_json_text"
            type="textarea"
            :rows="10"
            placeholder='{"@context":"https://schema.org","@type":"FAQPage","mainEntity":[]}'
          />
        </el-form-item>
        <el-form-item label="启用">
          <el-switch v-model="form.active" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="save">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import request from '@/api/index'
import { ElMessage, ElMessageBox } from 'element-plus'

const list = ref([])
const page = ref(1)
const limit = ref(20)
const total = ref(0)
const filterPageType = ref('')
const filterSchemaType = ref('')

const dialogVisible = ref(false)
const isEdit = ref(false)
const form = ref({
  id: '', page_type: '', schema_type: '', template_json_text: '', active: true,
})

function resetForm() {
  form.value = { id: '', page_type: '', schema_type: '', template_json_text: '', active: true }
  isEdit.value = false
}

function jsonPreview(v) {
  if (!v) return '—'
  try {
    const s = typeof v === 'string' ? v : JSON.stringify(v)
    return s.length > 120 ? s.substring(0, 120) + '…' : s
  } catch (e) { return '—' }
}

function load() {
  request.get('/geo/schema-templates', {
    params: { page: page.value, limit: limit.value, page_type: filterPageType.value, schema_type: filterSchemaType.value },
  }).then(res => {
    list.value = res.data?.list || []
    total.value = res.data?.total || 0
  }).catch(() => {})
}

function openDialog(row) {
  if (row) {
    form.value = {
      id: row.id, page_type: row.page_type, schema_type: row.schema_type,
      active: row.active,
      template_json_text: typeof row.template_json === 'string' ? row.template_json : JSON.stringify(row.template_json || {}),
    }
    isEdit.value = true
  } else {
    resetForm()
  }
  dialogVisible.value = true
}

async function save() {
  try {
    let tpl = form.value.template_json_text
    try { tpl = JSON.parse(form.value.template_json_text) } catch (e) {}
    const payload = {
      page_type: form.value.page_type,
      schema_type: form.value.schema_type,
      template_json: tpl,
      active: form.value.active,
    }
    if (isEdit.value) {
      await request.put(`/geo/schema-templates/${form.value.id}`, payload)
    } else {
      await request.post('/geo/schema-templates', payload)
    }
    ElMessage.success('已保存')
    dialogVisible.value = false
    load()
  } catch (e) {
    ElMessage.error(e?.response?.data?.message || '保存失败')
  }
}

async function remove(row) {
  try {
    await ElMessageBox.confirm(`确认删除 ${row.page_type}/${row.schema_type} ?`, '警告', { type: 'warning' })
    await request.delete(`/geo/schema-templates/${row.id}`)
    ElMessage.success('已删除')
    load()
  } catch (e) {}
}

onMounted(load)
</script>

<style scoped>
.card-header { display: flex; justify-content: space-between; align-items: center; }
.pager { display: flex; justify-content: flex-end; margin-top: 12px; }
.preview { font-size: 12px; color: #666; font-family: monospace; background: #f5f5f5; padding: 2px 6px; border-radius: 3px; }
</style>
