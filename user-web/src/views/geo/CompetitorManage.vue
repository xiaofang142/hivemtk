<template>
  <div class="geo-page p-4">
    <el-card>
      <template #header>
        <div class="flex items-center justify-between">
          <span class="font-bold">竞品管理</span>
          <el-button type="primary" size="small" @click="openCreate">+ 新增竞品</el-button>
        </div>
      </template>

      <el-table :data="list" v-loading="loading" size="default" stripe>
        <el-table-column prop="id" label="ID" width="60" />
        <el-table-column prop="name" label="名称" width="140" />
        <el-table-column prop="domain" label="域名" width="220">
          <template #default="{ row }">
            <a :href="'https://' + row.domain" target="_blank" class="font-mono text-blue-600">{{ row.domain }}</a>
          </template>
        </el-table-column>
        <el-table-column label="爬取路径" min-width="240">
          <template #default="{ row }">
            <div class="flex flex-wrap gap-1">
              <el-tag v-for="p in (row.paths || [])" :key="p" size="small" effect="plain">{{ p }}</el-tag>
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="category" label="分类" width="100">
          <template #default="{ row }">
            <el-tag size="small" :type="categoryTag(row.category)">{{ categoryLabel(row.category) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="priority" label="优先级" width="80" sortable />
        <el-table-column prop="status" label="状态" width="90">
          <template #default="{ row }">
            <el-tag size="small" :type="row.status === 'active' ? 'success' : 'info'">{{ row.status }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="180">
          <template #default="{ row }">
            <el-button size="small" @click="openEdit(row)">编辑</el-button>
            <el-button size="small" type="danger" plain @click="onDelete(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    
    <el-dialog v-model="dialogVisible" :title="editing ? '编辑竞品' : '新增竞品'" width="520px" destroy-on-close>
      <el-form :model="form" label-width="90px" size="default">
        <el-form-item label="名称">
          <el-input v-model="form.name" placeholder="如 微伴助手" />
        </el-form-item>
        <el-form-item label="域名">
          <el-input v-model="form.domain" placeholder="如 weibanzhushou.com" :disabled="!!editing" />
        </el-form-item>
        <el-form-item label="爬取路径">
          <div>
            <el-tag v-for="(p, i) in form.paths" :key="i" closable @close="form.paths.splice(i, 1)" size="default" style="margin-right:4px;margin-bottom:4px">
              {{ p }}
            </el-tag>
            <el-input v-if="newPath" v-model="newPath" size="small" style="width:120px" @keyup.enter="addPath" />
            <el-button v-else size="small" plain @click="newPath = '/'">+ 添加路径</el-button>
          </div>
        </el-form-item>
        <el-form-item label="分类">
          <el-select v-model="form.category">
            <el-option label="直接竞品" value="direct" />
            <el-option label="海外巨头" value="global" />
            <el-option label="间接竞品" value="indirect" />
          </el-select>
        </el-form-item>
        <el-form-item label="优先级">
          <el-slider v-model="form.priority" :min="1" :max="10" show-input style="width:240px" />
        </el-form-item>
        <el-form-item label="状态">
          <el-switch v-model="form.status" active-value="active" inactive-value="paused" active-text="active" inactive-text="paused" />
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="form.notes" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="onSave">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { listCompetitors, createCompetitor, updateCompetitor, deleteCompetitor } from '@/api/geoProbe.js'

const list = ref([])
const loading = ref(false)
const dialogVisible = ref(false)
const editing = ref(null)
const saving = ref(false)
const newPath = ref('')

const form = ref({
  name: '', domain: '', paths: ['/', '/product'],
  category: 'direct', priority: 5, status: 'active', notes: ''
})

const categoryLabel = (c) => ({ direct: '直接竞品', global: '海外巨头', indirect: '间接竞品' }[c] || c)
const categoryTag = (c) => ({ direct: '', global: 'warning', indirect: 'info' }[c] || '')

async function load() {
  loading.value = true
  try {
    list.value = await listCompetitors()
  } finally {
    loading.value = false
  }
}

function resetForm() {
  form.value = {
    name: '', domain: '', paths: ['/', '/product'],
    category: 'direct', priority: 5, status: 'active', notes: ''
  }
  newPath.value = ''
}

function openCreate() {
  editing.value = null
  resetForm()
  dialogVisible.value = true
}

function openEdit(row) {
  editing.value = row
  form.value = JSON.parse(JSON.stringify(row))
  if (!form.value.paths || form.value.paths.length === 0) {
    form.value.paths = ['/']
  }
  dialogVisible.value = true
}

function addPath() {
  if (newPath.value && !form.value.paths.includes(newPath.value)) {
    form.value.paths.push(newPath.value)
  }
  newPath.value = ''
}

async function onSave() {
  if (!form.value.name || !form.value.domain) {
    ElMessage.warning('名称和域名必填')
    return
  }
  saving.value = true
  try {
    if (editing.value) {
      await updateCompetitor(editing.value.id, form.value)
      ElMessage.success('更新成功')
    } else {
      await createCompetitor(form.value)
      ElMessage.success('创建成功')
    }
    dialogVisible.value = false
    await load()
  } catch (e) {
    ElMessage.error(e?.response?.data?.message || '保存失败')
  } finally {
    saving.value = false
  }
}

async function onDelete(row) {
  try {
    await ElMessageBox.confirm(`确认删除竞品 "${row.name}" 吗？`, '提示', { type: 'warning' })
    await deleteCompetitor(row.id)
    ElMessage.success('已删除')
    await load()
  } catch { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
}

onMounted(load)
</script>
