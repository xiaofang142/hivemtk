<template>
  <div class="leads-page">
    <div class="page-head">
      <div>
        <h2 class="page-title">
          判定证据回放
        </h2>
        <span class="page-sub">三路检测（关键词 / 语义 / LLM）独立留证；降级证据标记「不完整」</span>
      </div>
    </div>

    <el-card shadow="never">
      <el-form
        inline
        @submit.prevent
      >
        <el-form-item label="判定ID">
          <el-input
            v-model="judgementId"
            placeholder="lmj_xxx"
            clearable
            style="width: 240px"
            @keyup.enter="load"
          />
        </el-form-item>
        <el-form-item>
          <el-button
            type="primary"
            :loading="loading"
            @click="load"
          >
            加载证据
          </el-button>
        </el-form-item>
      </el-form>

      <el-table
        v-loading="loading"
        :data="rows"
        border
        stripe
        empty-text="输入判定ID后加载"
      >
        <el-table-column
          prop="type"
          label="检测类型"
          width="110"
        >
          <template #default="{ row }">
            <el-tag size="small">
              {{ typeLabel(row.type) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="证据完整"
          width="100"
        >
          <template #default="{ row }">
            <el-tag
              size="small"
              :type="row.complete ? 'success' : 'danger'"
            >
              {{ row.complete ? '完整' : '不完整' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="降级"
          width="80"
        >
          <template #default="{ row }">
            <el-tag
              v-if="row.degraded"
              size="small"
              type="warning"
            >
              降级
            </el-tag>
            <span v-else>-</span>
          </template>
        </el-table-column>
        <el-table-column
          label="证据消息ID"
          min-width="200"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            {{ join(row.evidence_message_ids) }}
          </template>
        </el-table-column>
        <el-table-column
          label="载荷"
          min-width="260"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            {{ stringify(row.payload) }}
          </template>
        </el-table-column>
        <el-table-column
          prop="reason"
          label="说明"
          min-width="200"
          show-overflow-tooltip
        />
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import { leadsApi } from '@/api/leads'

const judgementId = ref('')
const loading = ref(false)
const rows = ref([])

function typeLabel(type) {
  return { keyword: '关键词', semantic: '语义关键词', llm: 'LLM' }[type] || type
}

function join(list) {
  return Array.isArray(list) ? list.join('、') : ''
}

function stringify(value) {
  try {
    return JSON.stringify(value)
  } catch {
    return String(value)
  }
}

async function load() {
  if (!judgementId.value.trim()) {
    ElMessage.warning('请输入判定ID')
    return
  }
  loading.value = true
  try {
    const res = await leadsApi.judgementHits(judgementId.value.trim())
    rows.value = Array.isArray(res) ? res : (Array.isArray(res?.list) ? res.list : [])
  } catch (e) {
    ElMessage.error(e?.message || '加载判定证据失败')
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.leads-page { padding: 16px; }
.page-head { margin-bottom: 12px; }
.page-title { margin: 0; font-size: 18px; }
.page-sub { color: #909399; font-size: 12px; }
</style>
