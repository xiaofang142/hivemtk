<template>
  <div class="app-container">
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>MCP 凭证（Client ID + API Key）</span>
          <el-button type="primary" size="small" @click="openCreate">签发新凭证</el-button>
        </div>
      </template>

      <el-alert
        type="info"
        :closable="false"
        show-icon
        title="供外部 AI Skill / Agent 通过 MCP 入口（POST /api/mcp）接入：请求头携带 X-Client-Id 与 X-API-Key。API Key 只在签发时显示一次。"
        style="margin-bottom: 12px"
      />

      <el-table v-loading="loading" :data="rows" border size="small">
        <el-table-column prop="client_id" label="Client ID" width="180" />
        <el-table-column prop="name" label="备注" min-width="140" />
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag :type="row.enabled ? 'success' : 'info'" size="small">
              {{ row.enabled ? '启用' : '停用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="last_used_at" label="最近使用" width="170">
          <template #default="{ row }">{{ fmtTime(row.last_used_at) }}</template>
        </el-table-column>
        <el-table-column prop="created_at" label="创建时间" width="170">
          <template #default="{ row }">{{ fmtTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="200" fixed="right">
          <template #default="{ row }">
            <el-button size="small" @click="toggle(row)">{{ row.enabled ? '停用' : '启用' }}</el-button>
            <el-button size="small" type="danger" @click="remove(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- 签发弹窗 -->
    <el-dialog v-model="createVisible" title="签发 MCP 凭证" width="480px">
      <el-form label-width="80px">
        <el-form-item label="备注">
          <el-input v-model="createName" placeholder="如：Alan 的 AI 助手" maxlength="100" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createVisible = false">取消</el-button>
        <el-button type="primary" :loading="creating" @click="doCreate">签发</el-button>
      </template>
    </el-dialog>

    <!-- 签发结果（明文只显示一次） -->
    <el-dialog v-model="issuedVisible" title="凭证已签发（只显示这一次）" width="520px" :close-on-click-modal="false">
      <el-alert type="warning" :closable="false" show-icon title="请立即复制保存，关闭后无法再次查看 API Key" style="margin-bottom: 12px" />
      <el-descriptions :column="1" border>
        <el-descriptions-item label="Client ID">{{ issued.client_id }}</el-descriptions-item>
        <el-descriptions-item label="API Key">
          <span class="mono">{{ issued.api_key }}</span>
        </el-descriptions-item>
      </el-descriptions>
      <template #footer>
        <el-button type="primary" @click="copyAll">复制并关闭</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { mcpCredentialApi } from '@/api/mcpCredential'

const rows = ref([])
const loading = ref(false)
const createVisible = ref(false)
const issuedVisible = ref(false)
const creating = ref(false)
const createName = ref('')
const issued = ref({})

const fmtTime = (v) => (v ? String(v).replace('T', ' ').slice(0, 19) : '—')

const load = async () => {
  loading.value = true
  try {
    const res = await mcpCredentialApi.list()
    rows.value = res?.data ?? res ?? []
  } finally {
    loading.value = false
  }
}

const openCreate = () => {
  createName.value = ''
  createVisible.value = true
}

const doCreate = async () => {
  creating.value = true
  try {
    const res = await mcpCredentialApi.create({ name: createName.value })
    issued.value = res?.data ?? {}
    createVisible.value = false
    issuedVisible.value = true
    await load()
  } catch (e) {
    ElMessage.error(e?.response?.data?.message || '签发失败')
  } finally {
    creating.value = false
  }
}

const copyAll = async () => {
  const text = `Client ID: ${issued.value.client_id}\nAPI Key: ${issued.value.api_key}`
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success('已复制')
  } catch {
    ElMessage.info('请手动复制')
  }
  issuedVisible.value = false
}

const toggle = async (row) => {
  await mcpCredentialApi.update(row.id, { enabled: !row.enabled })
  ElMessage.success(row.enabled ? '已停用' : '已启用')
  await load()
}

const remove = async (row) => {
  await ElMessageBox.confirm(`确认删除凭证 ${row.client_id}？使用它的 Skill/Agent 将立即失效。`, '删除确认', { type: 'warning' })
  await mcpCredentialApi.remove(row.id)
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
.mono {
  font-family: monospace;
  user-select: all;
}
</style>
