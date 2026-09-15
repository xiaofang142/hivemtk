<template>
  <div class="geo-pusher-config">
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>🔑 蜘蛛推送配置（5 引擎）</span>
          <el-button type="primary" @click="openDialog()">+ 新增平台</el-button>
        </div>
      </template>

      <el-alert type="info" :closable="false" style="margin-bottom:12px">
        <template #title>
          凭据用 AES-256-GCM 加密后存 JSONB。百度：token + site；Google：多个 Service Account；IndexNow：host + key（无限额）
        </template>
      </el-alert>

      <el-table :data="list" border stripe size="small">
        <el-table-column prop="platform" label="引擎" width="120">
          <template #default="{ row }">
            <el-tag size="small" :type="platformTag(row.platform)">{{ row.platform }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="日配额" width="160">
          <template #default="{ row }">
            <el-progress
              :percentage="row.daily_limit > 0 ? Math.min(100, Math.round((row.used_today / row.daily_limit) * 100)) : 0"
              :status="row.daily_limit - row.used_today > 500 ? 'success' : row.daily_limit - row.used_today > 100 ? 'warning' : 'exception'"
              :format="() => row.daily_limit > 0 ? `${row.used_today}/${row.daily_limit}` : '∞'"
            />
          </template>
        </el-table-column>
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="row.active ? 'success' : 'info'" size="small">{{ row.active ? '启用' : '停用' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="updated_at" label="更新时间" width="180" />
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
    <el-dialog v-model="dialogVisible" :title="isEdit ? '编辑平台' : '新增平台'" width="560px">
      <el-form :model="form" label-width="100px">
        <el-form-item label="平台" required>
          <el-select v-model="form.platform" :disabled="isEdit" style="width:100%">
            <el-option label="百度 (主动推送)" value="baidu" />
            <el-option label="Google (Indexing API)" value="google" />
            <el-option label="IndexNow (Bing/Yandex/Naver)" value="indexnow" />
            <el-option label="头条 (sitemap)" value="toutiao" />
            <el-option label="神马 (sitemap)" value="shenma" />
          </el-select>
        </el-form-item>
        <el-form-item label="配置 JSON">
          <el-input
            v-model="form.config_json_text"
            type="textarea"
            :rows="5"
            placeholder='{"site":"example.com","token":"xxx"}'
          />
        </el-form-item>
        <el-form-item label="日配额">
          <el-input-number v-model="form.daily_limit" :min="0" :max="100000" />
          <span style="margin-left:8px;color:#999">0 = 无限额</span>
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
const dialogVisible = ref(false)
const isEdit = ref(false)
const form = ref({
  id: '', platform: '', config_json_text: '', daily_limit: 0, active: true,
})

function resetForm() {
  form.value = { id: '', platform: '', config_json_text: '', daily_limit: 0, active: true }
  isEdit.value = false
}

function platformTag(p) {
  const m = { baidu: 'warning', google: 'primary', indexnow: 'success', toutiao: 'info', shenma: 'info' }
  return m[p] || ''
}

function load() {
  request.get('/geo/pushers', { params: { page: page.value, limit: limit.value } })
    .then(res => {
      list.value = res.data?.list || []
      total.value = res.data?.total || 0
    }).catch(() => {})
}

function openDialog(row) {
  if (row) {
    form.value = {
      id: row.id, platform: row.platform, daily_limit: row.daily_limit, active: row.active,
      config_json_text: typeof row.config_json === 'string' ? row.config_json : JSON.stringify(row.config_json || {}),
    }
    isEdit.value = true
  } else {
    resetForm()
  }
  dialogVisible.value = true
}

async function save() {
  try {
    // 把 config_json_text 解析成对象再序列化
    let cfgJson = form.value.config_json_text
    try { cfgJson = JSON.parse(form.value.config_json_text) } catch (e) {}
    const payload = {
      platform: form.value.platform,
      config_json: cfgJson,
      daily_limit: form.value.daily_limit,
      active: form.value.active,
    }
    if (isEdit.value) {
      await request.put(`/geo/pushers/${form.value.id}`, payload)
    } else {
      await request.post('/geo/pushers', payload)
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
    await ElMessageBox.confirm(`确认删除 ${row.platform} ?`, '警告', { type: 'warning' })
    await request.delete(`/geo/pushers/${row.id}`)
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
