<template>
  <div class="app-container">
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>渠道能力矩阵</span>
          <el-button size="small" @click="copyMd">复制 Markdown</el-button>
        </div>
      </template>
      <el-alert
        type="info"
        :closable="false"
        show-icon
        title="由 internal/capregistry 注册表自动生成（docs/capabilities.md 与注册表测试钉死一致）。新增渠道/能力在 channels.go 登记后自动出现在这里。"
        style="margin-bottom: 12px"
      />
      <el-table v-loading="loading" :data="entries" border size="small">
        <el-table-column prop="platform" label="平台" width="140" />
        <el-table-column prop="capability" label="能力" width="160">
          <template #default="{ row }">
            <el-tag size="small">{{ row.capability }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="via" label="接入方式" width="150" />
        <el-table-column prop="note" label="说明" min-width="240" show-overflow-tooltip />
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { capabilitiesApi } from '@/api/kbConnector'

const entries = ref([])
const markdown = ref('')
const loading = ref(false)

const load = async () => {
  loading.value = true
  try {
    const res = await capabilitiesApi.get()
    const data = res?.data ?? res ?? {}
    entries.value = data.entries ?? []
    markdown.value = data.markdown ?? ''
  } finally {
    loading.value = false
  }
}

const copyMd = async () => {
  try {
    await navigator.clipboard.writeText(markdown.value)
    ElMessage.success('已复制')
  } catch {
    ElMessage.info('复制失败，请手动选择')
  }
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
