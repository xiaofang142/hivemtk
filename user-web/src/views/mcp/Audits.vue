<template>
  <div class="app-container">
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <el-tabs v-model="tab" style="flex: 1">
            <el-tab-pane label="调用审计" name="call" />
            <el-tab-pane label="管理审计" name="admin" />
          </el-tabs>
          <div v-if="tab === 'call'" class="filters">
            <el-input
              v-model="filters.client_id"
              placeholder="Client ID"
              clearable
              size="small"
              style="width: 180px"
              @change="load"
            />
            <el-input
              v-model="filters.tool"
              placeholder="工具名"
              clearable
              size="small"
              style="width: 160px"
              @change="load"
            />
            <el-select v-model="filters.event" placeholder="事件" clearable size="small" style="width: 130px" @change="load">
              <el-option label="工具调用" value="call" />
              <el-option label="工具枚举" value="list" />
              <el-option label="鉴权失败" value="auth" />
            </el-select>
            <el-select v-model="filters.allowed" placeholder="结果" clearable size="small" style="width: 110px" @change="load">
              <el-option label="已执行" :value="1" />
              <el-option label="被拒（越权）" :value="0" />
            </el-select>
            <el-button type="primary" size="small" @click="load">刷新</el-button>
          </div>
          <div v-else class="filters">
            <el-select v-model="adminFilters.action" placeholder="动作" clearable filterable size="small" style="width: 180px" @change="loadAdmin">
              <el-option v-for="a in actions" :key="a" :label="a" :value="a" />
            </el-select>
            <el-select v-model="adminFilters.target_type" placeholder="对象" clearable size="small" style="width: 120px" @change="loadAdmin">
              <el-option label="凭证" value="credential" />
              <el-option label="密钥" value="key" />
              <el-option label="Skill" value="skill" />
            </el-select>
            <el-button type="primary" size="small" @click="loadAdmin">刷新</el-button>
          </div>
        </div>
      </template>

      <!-- 调用审计：MCP 入口的使用面留痕 -->
      <template v-if="tab === 'call'">
        <el-alert
          type="info"
          :closable="false"
          show-icon
          title="MCP 入口（POST /api/mcp）每次调用都留痕：谁、哪把凭证哪把钥匙、调了什么、成没成。参数只存摘要（sha256），不落原文；鉴权失败（拿错钥匙撞门）也在此列。"
          style="margin-bottom: 12px"
        />
        <el-table v-loading="loading" :data="rows" border size="small">
          <el-table-column prop="created_at" label="时间" width="170">
            <template #default="{ row }">{{ fmtTime(row.created_at) }}</template>
          </el-table-column>
          <el-table-column prop="client_id" label="Client ID" width="150" />
          <el-table-column label="事件" width="90">
            <template #default="{ row }">
              <el-tag v-if="row.event === 'auth'" type="danger" size="small">鉴权失败</el-tag>
              <el-tag v-else-if="row.event === 'list'" type="info" size="small">枚举</el-tag>
              <el-tag v-else size="small">调用</el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="method" label="方法" width="100" />
          <el-table-column prop="tool" label="工具" min-width="140" />
          <el-table-column label="钥匙" width="70">
            <template #default="{ row }">
              <span v-if="row.key_id" class="mono">#{{ row.key_id }}</span>
              <span v-else class="muted">—</span>
            </template>
          </el-table-column>
          <el-table-column label="结果" width="110">
            <template #default="{ row }">
              <el-tag v-if="row.event === 'auth'" type="danger" size="small">拒绝</el-tag>
              <el-tag v-else-if="row.allowed && row.ok" type="success" size="small">成功</el-tag>
              <el-tag v-else-if="row.allowed" type="warning" size="small">失败</el-tag>
              <el-tag v-else type="danger" size="small">越权拒绝</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="耗时" width="80">
            <template #default="{ row }">{{ row.event === 'auth' ? '—' : row.latency_ms + 'ms' }}</template>
          </el-table-column>
          <el-table-column prop="ip" label="来源 IP" width="130" />
          <el-table-column prop="args_hash" label="参数摘要" width="150">
            <template #default="{ row }">
              <code class="mono">{{ row.args_hash || '—' }}</code>
            </template>
          </el-table-column>
          <el-table-column prop="error_msg" label="错误" min-width="160" show-overflow-tooltip />
        </el-table>
        <el-pagination
          v-model:current-page="page"
          :page-size="size"
          :total="total"
          layout="total, prev, pager, next"
          small
          style="margin-top: 12px; justify-content: flex-end"
          @current-change="load"
        />
      </template>

      <!-- 管理审计：MCP/Skill 管理面的操作账本 -->
      <template v-else>
        <el-alert
          type="info"
          :closable="false"
          show-icon
          title="凭证 / 钥匙 / Skill 的每次管理动作（签发、状态流转、删除、查看明文）都记在这里。明文永不入账——reveal 只记「谁看了哪把钥匙」。"
          style="margin-bottom: 12px"
        />
        <el-table v-loading="adminLoading" :data="adminRows" border size="small">
          <el-table-column prop="created_at" label="时间" width="170">
            <template #default="{ row }">{{ fmtTime(row.created_at) }}</template>
          </el-table-column>
          <el-table-column prop="action" label="动作" width="170">
            <template #default="{ row }">
              <el-tag size="small" :type="actionTagType(row.action)">{{ row.action }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="对象" width="90">
            <template #default="{ row }">
              {{ { credential: '凭证', key: '密钥', skill: 'Skill' }[row.target_type] || row.target_type }} #{{ row.target_id }}
            </template>
          </el-table-column>
          <el-table-column prop="target_label" label="标签" min-width="140" show-overflow-tooltip>
            <template #default="{ row }">{{ row.target_label || '—' }}</template>
          </el-table-column>
          <el-table-column prop="operator_username" label="操作人" width="110">
            <template #default="{ row }">{{ row.operator_username || '—' }}</template>
          </el-table-column>
          <el-table-column prop="ip" label="来源 IP" width="130" />
          <el-table-column prop="detail" label="详情" min-width="240" show-overflow-tooltip>
            <template #default="{ row }">
              <code class="mono">{{ row.detail || '—' }}</code>
            </template>
          </el-table-column>
        </el-table>
        <el-pagination
          v-model:current-page="adminPage"
          :page-size="size"
          :total="adminTotal"
          layout="total, prev, pager, next"
          small
          style="margin-top: 12px; justify-content: flex-end"
          @current-change="loadAdmin"
        />
      </template>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted, watch } from 'vue'
import { mcpAuditApi, mcpAdminAuditApi } from '@/api/mcpCredential'

const tab = ref('call')
const size = 20

// —— 调用审计 ——
const rows = ref([])
const loading = ref(false)
const total = ref(0)
const page = ref(1)
const filters = ref({ client_id: '', tool: '', allowed: undefined, event: undefined })

// —— 管理审计 ——
const adminRows = ref([])
const adminLoading = ref(false)
const adminTotal = ref(0)
const adminPage = ref(1)
const adminFilters = ref({ action: '', target_type: '' })
const actions = [
  'credential.create', 'credential.update', 'credential.delete',
  'key.create', 'key.enable', 'key.disable', 'key.revoke', 'key.delete', 'key.reveal',
  'skill.create', 'skill.update', 'skill.delete'
]

const fmtTime = (v) => (v ? String(v).replace('T', ' ').slice(0, 19) : '—')
const actionTagType = (a) => {
  if (a?.endsWith('.delete')) return 'danger'
  if (a === 'key.revoke' || a === 'key.disable') return 'warning'
  if (a === 'key.reveal') return 'danger'
  if (a?.endsWith('.create')) return 'success'
  return 'info'
}

const load = async () => {
  loading.value = true
  try {
    const params = { page: page.value, size }
    if (filters.value.client_id) params.client_id = filters.value.client_id
    if (filters.value.tool) params.tool = filters.value.tool
    if (filters.value.event) params.event = filters.value.event
    if (filters.value.allowed !== undefined && filters.value.allowed !== null && filters.value.allowed !== '') {
      params.allowed = filters.value.allowed
    }
    const res = await mcpAuditApi.list({ params })
    const data = res?.data ?? res ?? {}
    rows.value = data.list ?? []
    total.value = data.total ?? 0
  } finally {
    loading.value = false
  }
}

const loadAdmin = async () => {
  adminLoading.value = true
  try {
    const params = { page: adminPage.value, size }
    if (adminFilters.value.action) params.action = adminFilters.value.action
    if (adminFilters.value.target_type) params.target_type = adminFilters.value.target_type
    const res = await mcpAdminAuditApi.list({ params })
    const data = res?.data ?? res ?? {}
    adminRows.value = data.list ?? []
    adminTotal.value = data.total ?? 0
  } finally {
    adminLoading.value = false
  }
}

watch(tab, (v) => {
  if (v === 'admin' && adminRows.value.length === 0) loadAdmin()
})

onMounted(load)
</script>

<style scoped>
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}
.filters {
  display: flex;
  gap: 8px;
  align-items: center;
}
.mono {
  font-family: monospace;
  font-size: 12px;
}
.muted {
  color: #999;
  font-size: 12px;
}
</style>
