<template>
  <div class="geo-site-config">
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>🏠 静态站部署配置</span>
          <el-button type="primary" @click="openDialog()">+ 新增站点</el-button>
        </div>
      </template>

      <el-table :data="list" border stripe size="small">
        <el-table-column prop="domain" label="域名" />
        <el-table-column prop="provider" label="提供商" width="120">
          <template #default="{ row }">
            <el-tag size="small">{{ row.provider || 'cloudflare' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="hugo_path" label="Hugo 路径" show-overflow-tooltip />
        <el-table-column prop="git_repo" label="Git 仓库" show-overflow-tooltip />
        <el-table-column label="健康" width="80">
          <template #default="{ row }">
            <el-tag :type="row.health_score > 80 ? 'success' : row.health_score > 40 ? 'warning' : 'danger'" size="small">
              {{ row.health_score || 0 }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="80">
          <template #default="{ row }">
            <el-tag :type="row.active ? 'success' : 'info'" size="small">
              {{ row.active ? '启用' : '停用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="180" fixed="right">
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
          :page-sizes="[20, 50, 100]"
          layout="total, sizes, prev, pager, next"
          @size-change="load"
          @current-change="load"
        />
      </div>
    </el-card>

    <!-- Dialog -->
    <el-dialog v-model="dialogVisible" :title="isEdit ? '编辑站点' : '新增站点'" width="560px">
      <el-form :model="form" label-width="100px">
        <el-form-item label="域名" required>
          <el-input v-model="form.domain" placeholder="example.com" />
        </el-form-item>
        <el-form-item label="提供商">
          <el-select v-model="form.provider" style="width:100%">
            <el-option label="Cloudflare Pages" value="cloudflare" />
            <el-option label="GitHub Pages" value="github" />
            <el-option label="Vercel" value="vercel" />
            <el-option label="自托管" value="self" />
          </el-select>
        </el-form-item>
        <el-form-item label="Hugo 路径">
          <el-input v-model="form.hugo_path" placeholder="/data/hivemtk-site" />
        </el-form-item>
        <el-form-item label="Git 仓库">
          <el-input v-model="form.git_repo" placeholder="git@github.com:xxx/xxx.git" />
        </el-form-item>
        <el-form-item label="部署命令">
          <el-input v-model="form.deploy_cmd" placeholder="hugo -e production" />
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
import { geoApi } from '@/api/geo'
import { ElMessage, ElMessageBox } from 'element-plus'

const list = ref([])
const page = ref(1)
const limit = ref(20)
const total = ref(0)

const dialogVisible = ref(false)
const isEdit = ref(false)
const form = ref({
  id: '', domain: '', provider: 'cloudflare',
  hugo_path: '', git_repo: '', deploy_cmd: '', active: true,
})

function resetForm() {
  form.value = {
    id: '', domain: '', provider: 'cloudflare',
    hugo_path: '', git_repo: '', deploy_cmd: '', active: true,
  }
  isEdit.value = false
}

async function load() {
  try {
    const res = await geoApi.listSites({ page: page.value, limit: limit.value })
    list.value = res?.list || []
    total.value = res?.total || 0
  } catch (e) {}
}

function openDialog(row) {
  if (row) {
    form.value = { ...row }
    isEdit.value = true
  } else {
    resetForm()
  }
  dialogVisible.value = true
}

async function save() {
  try {
    if (isEdit.value) {
      await geoApi.updateSite(form.value.id, form.value)
    } else {
      await geoApi.createSite(form.value)
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
    await ElMessageBox.confirm(`确认删除 ${row.domain} ?`, '警告', { type: 'warning' })
    await geoApi.deleteSite(row.id)
    ElMessage.success('已删除')
    load()
  } catch (e) {}
}

onMounted(load)
</script>

<style scoped>
.card-header { display: flex; justify-content: space-between; align-items: center; }
.pager { display: flex; justify-content: flex-end; margin-top: 12px; }
</style>
