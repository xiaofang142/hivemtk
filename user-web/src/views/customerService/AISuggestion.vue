<template>
  <div class="ai-suggestion-page">
    <el-card class="header-card">
      <div>
        <h2>AI 建议</h2>
        <p class="subtitle">
          查看会话的 AI 回复建议、采纳与反馈
        </p>
      </div>
    </el-card>

    <el-card>
      <template #header>
        <div class="card-header">
          <el-space>
            <el-input
              v-model="sessionId"
              :placeholder="$t('输入会话 ID 加载建议')"
              style="width: 280px"
              clearable
              @keyup.enter="loadSuggestions"
            />
            <el-button
              type="primary"
              @click="loadSuggestions"
            >
              <el-icon><Search /></el-icon>
              {{ $t('加载') }}
            </el-button>
          </el-space>
        </div>
      </template>

      <el-table
        v-loading="loading"
        :data="suggestions"
        stripe
      >
        <el-table-column
          prop="id"
          label="ID"
          width="80"
        />
        <el-table-column
          prop="suggestion"
          label="AI 建议"
          min-width="320"
          show-overflow-tooltip
        />
        <el-table-column
          prop="confidence"
          label="置信度"
          width="120"
          align="center"
        >
          <template #default="{ row }">
            <el-progress
              :percentage="Math.round((row.confidence || 0) * 100)"
              :stroke-width="10"
              :color="confidenceColor(row.confidence)"
            />
          </template>
        </el-table-column>
        <el-table-column
          prop="source"
          label="来源"
          width="120"
        >
          <template #default="{ row }">
            <el-tag size="small">
              {{ row.source || 'rule' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="is_used"
          label="是否已采纳"
          width="120"
          align="center"
        >
          <template #default="{ row }">
            <el-tag
              v-if="row.is_used"
              type="success"
            >
              已采纳
            </el-tag>
            <el-tag
              v-else
              type="info"
            >
              未采纳
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="created_at"
          label="生成时间"
          min-width="160"
        >
          <template #default="{ row }">
            {{ formatTime(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="120"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button
              v-if="!row.is_used"
              link
              type="success"
              @click="handleUse(row)"
            >
              采纳
            </el-button>
            <span
              v-else
              style="color: #909399"
            >已使用</span>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty :description="sessionId ? '该会话暂无 AI 建议' : '请输入会话 ID 后加载建议'" />
        </template>
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Search } from '@element-plus/icons-vue'
import { getAISuggestions, useAISuggestion } from '@/api/customerService.js'

const loading = ref(false)
const sessionId = ref('')
const suggestions = ref([])

const formatTime = (val) => {
  if (!val) return '-'
  const d = new Date(val)
  if (isNaN(d.getTime())) return '-'
  return d.toLocaleString('zh-CN', { hour12: false })
}

const confidenceColor = (c) => {
  if (c >= 0.8) return '#10B981'
  if (c >= 0.5) return '#F59E0B'
  return '#EF4444'
}

const loadSuggestions = async () => {
  if (!sessionId.value) {
    ElMessage.warning(i18n.global.t('请先输入会话 ID'))
    return
  }
  loading.value = true
  try {
    const res = await getAISuggestions(sessionId.value)
    const data = res || []
    suggestions.value = Array.isArray(data) ? data : data.list || []
  } catch  {
    ElMessage.error(i18n.global.t('加载建议失败'))
    suggestions.value = []
  } finally {
    loading.value = false
  }
}

const handleUse = async (row) => {
  try {
    await useAISuggestion(row.id)
    ElMessage.success(i18n.global.t('已采纳'))
    row.is_used = true
  } catch  {
    ElMessage.error(i18n.global.t('采纳失败'))
  }
}

onMounted(() => {})
</script>

<style scoped lang="scss">
.ai-suggestion-page { padding: 20px; }
.header-card {
  margin-bottom: 20px;
  :deep(.el-card__body) { display: flex; justify-content: space-between; align-items: center; }
  h2 { margin: 0 0 8px 0; }
  .subtitle { color: #909399; margin: 0; }
}
.card-header { display: flex; justify-content: space-between; align-items: center; }
</style>
