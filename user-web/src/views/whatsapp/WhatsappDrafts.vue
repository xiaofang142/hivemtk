<template>
  <div class="page">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>WhatsApp 草稿箱</span>
          <el-button
            type="primary"
            :icon="Plus"
            @click="openCreate"
          >
            {{ $t('新建草稿') }}
          </el-button>
        </div>
      </template>

      <el-form
        :inline="true"
        :model="search"
        class="search-form"
      >
        <el-form-item label="标题">
          <el-input
            v-model="search.title"
            placeholder="搜索标题"
            clearable
            @keyup.enter="load"
          />
        </el-form-item>
        <el-form-item>
          <el-button
            type="primary"
            :icon="Search"
            @click="load"
          >
            搜索
          </el-button>
        </el-form-item>
      </el-form>

      <el-table
        v-loading="loading"
        :data="drafts"
        style="width:100%"
        border
      >
        <el-table-column
          prop="id"
          label="ID"
          width="80"
        />
        <el-table-column
          prop="title"
          label="标题"
          min-width="200"
        />
        <el-table-column
          prop="content"
          label="内容"
          min-width="300"
          show-overflow-tooltip
        />
        <el-table-column
          prop="updated_at"
          label="更新时间"
          width="180"
        >
          <template #default="scope">
            {{ formatDate(scope.row.updated_at) }}
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="200"
          fixed="right"
        >
          <template #default="scope">
            <el-button
              size="small"
              type="primary"
              link
              @click="openEdit(scope.row)"
            >
              编辑
            </el-button>
            <el-button
              size="small"
              type="danger"
              link
              @click="confirmDelete(scope.row)"
            >
              删除
            </el-button>
          </template>
        </el-table-column>
      </el-table>

      <div class="pagination">
        <el-pagination
          v-model:current-page="pagination.page"
          v-model:page-size="pagination.page_size"
          :page-sizes="[10, 20, 50]"
          :total="pagination.total"
          layout="total, sizes, prev, pager, next, jumper"
          @size-change="load"
          @current-change="load"
        />
      </div>
    </el-card>

    
    <el-dialog
      v-model="dialogVisible"
      :title="isEdit ? '编辑草稿' : '新建草稿'"
      width="600px"
      @close="resetForm"
    >
      <el-form
        ref="formRef"
        :model="form"
        :rules="rules"
        label-width="80px"
      >
        <el-form-item
          label="标题"
          prop="title"
        >
          <el-input
            v-model="form.title"
            placeholder="请输入草稿标题"
          />
        </el-form-item>
        <el-form-item
          label="内容"
          prop="content"
        >
          <el-input
            v-model="form.content"
            type="textarea"
            :rows="6"
            placeholder="请输入草稿内容"
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
          @click="submit"
        >
          保存
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, reactive, onMounted } from 'vue'
import api from '@/api/whatsapp'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Search } from '@element-plus/icons-vue'

const loading = ref(false)
const saving = ref(false)
const drafts = ref([])
const dialogVisible = ref(false)
const isEdit = ref(false)
const formRef = ref()

const form = reactive({
  id: null,
  title: '',
  content: ''
})

const rules = {
  title: [{ required: true, message: i18n.global.t('请输入标题'), trigger: 'blur' }],
  content: [{ required: true, message: i18n.global.t('请输入内容'), trigger: 'blur' }]
}

const search = reactive({ title: '' })
const pagination = reactive({ page: 1, page_size: 10, total: 0 })

const formatDate = (d) => {
  if (!d) return '-'
  const date = new Date(d)
  return isNaN(date.getTime()) ? '-' : date.toLocaleString('zh-CN', { hour12: false })
}

const load = async () => {
  loading.value = true
  try {
    const res = await api.listDrafts({
      page: pagination.page,
      page_size: pagination.page_size,
      title: search.title
    });
    if (Array.isArray(res)) {
      drafts.value = res
      pagination.total = res.length
    } else if (res && res.list) {
      drafts.value = res.list
      pagination.total = res.total || 0
    } else {
      drafts.value = []
      pagination.total = 0
    }
  } catch  {
    ElMessage.error(i18n.global.t('加载草稿失败'))
    drafts.value = []
  } finally {
    loading.value = false
  }
}

const openCreate = () => {
  isEdit.value = false
  Object.assign(form, { id: null, title: '', content: '' })
  dialogVisible.value = true
}

const openEdit = (row) => {
  isEdit.value = true
  Object.assign(form, { id: row.id, title: row.title, content: row.content })
  dialogVisible.value = true
}

const resetForm = () => {
  formRef.value?.resetFields()
  Object.assign(form, { id: null, title: '', content: '' })
}

const submit = async () => {
  try {
    await formRef.value.validate()
  } catch {
    return
  }
  saving.value = true
  try {
    if (isEdit.value && form.id) {
      await api.updateDraft(form.id, { title: form.title, content: form.content })
      ElMessage.success(i18n.global.t('更新成功'))
    } else {
      await api.createDraft({ title: form.title, content: form.content })
      ElMessage.success(i18n.global.t('创建成功'))
    }
    dialogVisible.value = false
    load()
  } catch (err) {
    ElMessage.error((isEdit.value ? '更新' : '创建') + '失败: ' + (err.message || '未知错误'))
  } finally {
    saving.value = false
  }
}

const confirmDelete = async (row) => {
  try {
    await ElMessageBox.confirm(`确认删除草稿 "${row.title}"?`, '提示', { type: 'warning' })
    await api.deleteDraft(row.id)
    ElMessage.success(i18n.global.t('删除成功'))
    load()
  } catch (err) {
    if (err !== 'cancel') {
      ElMessage.error('删除失败: ' + (err?.message || '未知错误'))
    }
  }
}

onMounted(load)
</script>

<style scoped>
.page { padding: 20px; }
.card-header { display: flex; justify-content: space-between; align-items: center; font-weight: 600; }
.search-form { margin-bottom: 16px; }
.pagination { margin-top: 20px; display: flex; justify-content: flex-end; }
</style>
