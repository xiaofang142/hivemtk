<template>
  <div class="app-container">
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>第三方知识库连接器</span>
          <el-button type="primary" size="small" @click="openCreate">新建连接器</el-button>
        </div>
      </template>

      <el-alert
        type="info"
        :closable="false"
        show-icon
        title="把第三方知识库的文档拉取进本地 RAG（分块+向量化），检索体验与本地文档一致。IMA 仅支持文件类内容（笔记类平台不提供导出）。"
        style="margin-bottom: 12px"
      />

      <el-table v-loading="loading" :data="rows" border size="small">
        <el-table-column prop="name" label="名称" min-width="120" />
        <el-table-column prop="type" label="类型" width="100" />
        <el-table-column label="鉴权" width="80">
          <template #default="{ row }">
            <el-tag v-if="hasCred(row)" type="success" size="small">已配</el-tag>
            <el-tag v-else type="info" size="small">未配</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag :type="row.enabled ? 'success' : 'info'" size="small">
              {{ row.enabled ? '启用' : '停用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="last_status" label="最近同步" width="100" />
        <el-table-column prop="last_summary" label="摘要" min-width="180" show-overflow-tooltip />
        <el-table-column label="操作" width="220" fixed="right">
          <template #default="{ row }">
            <el-button size="small" type="primary" :loading="syncingId === row.id" @click="sync(row)">同步</el-button>
            <el-button size="small" @click="openEdit(row)">编辑</el-button>
            <el-button size="small" type="danger" @click="remove(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog v-model="dialogVisible" :title="editId ? '编辑连接器' : '新建连接器'" width="560px">
      <el-form label-width="110px">
        <el-form-item label="类型">
          <el-select v-model="form.type" style="width: 100%" :disabled="!!editId">
            <el-option v-for="t in types" :key="t" :value="t" :label="typeLabel(t)" />
          </el-select>
        </el-form-item>
        <el-form-item label="名称">
          <el-input v-model="form.name" maxlength="100" placeholder="如：IMA 装修攻略库" />
        </el-form-item>
        <el-form-item label="Client ID">
          <el-input v-model="form.client_id" placeholder="IMA OpenAPI 的 client_id" autocomplete="off" />
        </el-form-item>
        <el-form-item label="API Key">
          <el-input v-model="form.api_key" placeholder="IMA OpenAPI 的 api_key" show-password autocomplete="new-password" />
        </el-form-item>
        <el-form-item v-if="form.type === 'ima'" label="知识库 ID">
          <el-input v-model="form.knowledge_base_id" placeholder="留空 = 同步全部知识库" />
        </el-form-item>
        <el-form-item v-if="form.type === 'webhttp'" label="清单 URL">
          <el-input v-model="form.manifest_url" placeholder="https://.../manifest.json" />
        </el-form-item>
        <el-form-item label="同步间隔">
          <el-input-number v-model="form.interval_minutes" :min="0" :max="1440" /> 分钟（0=仅手动）
        </el-form-item>
        <el-form-item label="启用">
          <el-switch v-model="form.enabled" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="save">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { kbConnectorApi } from '@/api/kbConnector'

const rows = ref([])
const types = ref([])
const loading = ref(false)
const saving = ref(false)
const syncingId = ref(null)
const dialogVisible = ref(false)
const editId = ref(null)
const form = ref(defaultForm())

function defaultForm() {
  return { type: 'ima', name: '', client_id: '', api_key: '', knowledge_base_id: '', manifest_url: '', interval_minutes: 0, enabled: true }
}

const typeLabel = (t) => ({ ima: '腾讯 IMA 知识库', webhttp: '通用 HTTP 清单' }[t] || t)
const hasCred = (row) => {
  const m = row.config_masked || ''
  return m.includes('client_id') || m.includes('"api_key"')
}

const load = async () => {
  loading.value = true
  try {
    const res = await kbConnectorApi.list()
    const data = res?.data ?? res ?? {}
    rows.value = data.list ?? []
    types.value = data.types ?? ['ima', 'webhttp']
  } finally {
    loading.value = false
  }
}

const openCreate = () => {
  editId.value = null
  form.value = defaultForm()
  dialogVisible.value = true
}

const openEdit = (row) => {
  editId.value = row.id
  const masked = {}
  try { Object.assign(masked, JSON.parse(row.config_masked || '{}')) } catch { /* 忽略 */ }
  form.value = {
    type: row.type, name: row.name,
    client_id: masked.client_id || '', api_key: '', knowledge_base_id: masked.knowledge_base_id || '',
    manifest_url: masked.manifest_url || '', interval_minutes: row.interval_minutes || 0,
    enabled: !!row.enabled
  }
  dialogVisible.value = true
}

const buildPayload = () => {
  const p = { type: form.value.type, name: form.value.name, enabled: form.value.enabled, interval_minutes: form.value.interval_minutes }
  if (form.value.type === 'ima') {
    p.client_id = form.value.client_id
    if (form.value.api_key) p.api_key = form.value.api_key
    if (form.value.knowledge_base_id) p.knowledge_base_id = form.value.knowledge_base_id
  } else {
    if (form.value.manifest_url) p.manifest_url = form.value.manifest_url
    if (form.value.api_key) p.api_key = form.value.api_key
  }
  return p
}

const save = async () => {
  saving.value = true
  try {
    if (editId.value) await kbConnectorApi.update(editId.value, buildPayload())
    else await kbConnectorApi.create(buildPayload())
    ElMessage.success('已保存')
    dialogVisible.value = false
    await load()
  } catch (e) {
    ElMessage.error(e?.response?.data?.message || '保存失败')
  } finally {
    saving.value = false
  }
}

const sync = async (row) => {
  syncingId.value = row.id
  try {
    const res = await kbConnectorApi.sync(row.id)
    ElMessage.success(res?.message || '同步完成')
    await load()
  } catch (e) {
    ElMessage.error(e?.response?.data?.message || '同步失败')
  } finally {
    syncingId.value = null
  }
}

const remove = async (row) => {
  await ElMessageBox.confirm(`删除连接器「${row.name}」？已导入的文档会保留在知识库。`, '删除确认', { type: 'warning' })
  await kbConnectorApi.remove(row.id)
  ElMessage.success('已删除')
  await load()
}

onMounted(load)
</script>

<style scoped>
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
</style>
