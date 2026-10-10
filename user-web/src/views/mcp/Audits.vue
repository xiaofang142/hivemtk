<template>
  <div class="app-container">
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>MCP 调用审计</span>
          <div class="filters">
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
            <el-select v-model="filters.allowed" placeholder="结果" clearable size="small" style="width: 110px" @change="load">
              <el-option label="已执行" :value="1" />
              <el-option label="被拒（越权）" :value="0" />
            </el-select>
            <el-button type="primary" size="small" @click="load">刷新</el-button>
          </div>
        </div>
      </template>

      <el-alert
        type="info"
        :closable="false"
        show-icon
        title="MCP 入口（POST /api/mcp）每次工具调用都留痕：谁、哪把凭证、调了什么、成没成。参数只存摘要（sha256），不落原文。"
        style="margin-bottom: 12px"
      />

      <el-table v-loading="loading" :data="rows" border size="small">
        <el-table-column prop="created_at" label="时间" width="170">
          <template #default="{ row }">{{ fmtTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column prop="client_id" label="Client ID" width="150" />
        <el-table-column prop="method" label="方法" width="100" />
        <el-table-column prop="tool" label="工具" min-width="150" />
        <el-table-column label="结果" width="110">
          <template #default="{ row }">
            <el-tag v-if="row.allowed && row.ok" type="success" size="small">成功</el-tag>
            <el-tag v-else-if="row.allowed" type="warning" size="small">失败</el-tag>
            <el-tag v-else type="danger" size="small">越权拒绝</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="耗时" width="80">
          <template #default="{ row }">{{ row.latency_ms }}ms</template>
        </el-table-column>
        <el-table-column prop="ip" label="来源 IP" width="130" />
        <el-table-column prop="args_hash" label="参数摘要" width="160">
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
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { mcpAuditApi } from '@/api/mcpCredential'

const rows = ref([])
const loading = ref(false)
const total = ref(0)
const page = ref(1)
const size = 20
const filters = ref({ client_id: '', tool: '', allowed: undefined })

const fmtTime = (v) => (v ? String(v).replace('T', ' ').slice(0, 19) : '—')

const load = async () => {
  loading.value = true
  try {
    const params = { page: page.value, size }
    if (filters.value.client_id) params.client_id = filters.value.client_id
    if (filters.value.tool) params.tool = filters.value.tool
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
</style>