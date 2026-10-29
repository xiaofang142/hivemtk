<template>
  <div class="app-container">
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>渠道能力矩阵</span>
          <div>
            <el-button
              v-if="markdown"
              size="small"
              @click="copyMd"
            >
              复制 Markdown
            </el-button>
            <el-button
              size="small"
              type="primary"
              plain
              @click="$router.push('/cap/diagnostics')"
            >
              运行诊断
            </el-button>
          </div>
        </div>
      </template>
      <el-alert
        type="info"
        :closable="false"
        show-icon
        title="由 internal/capregistry 注册表自动生成。这里展示的是「已登记」的渠道×能力；注册表声明不等于本二进制已实现，运行态 implemented / configured / ready 请看「能力运行诊断」。"
        style="margin-bottom: 12px"
      />
      <el-table
        v-loading="loading"
        :data="entries"
        border
        size="small"
      >
        <el-table-column
          prop="platform"
          label="平台"
          width="140"
        />
        <el-table-column
          prop="capability"
          label="能力"
          width="160"
        >
          <template #default="{ row }">
            <el-tag size="small">
              {{ row.capability }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="via"
          label="接入方式"
          width="150"
        />
        <el-table-column
          prop="note"
          label="说明"
          min-width="240"
          show-overflow-tooltip
        />
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { capabilitiesApi } from '@/api/kbConnector'
import { copyToClipboard } from '@/utils/clipboard'

const entries = ref([])
const markdown = ref('')
const loading = ref(false)

const load = async () => {
  loading.value = true
  try {
    const res = await capabilitiesApi.get()
    const data = res?.data ?? res ?? {}
    // D01 起 GET /api/capabilities 返回运行视图（capabilities/total/ready_count），
    // 旧字段 entries/markdown 仅作兼容兜底；两者字段重叠部分是 platform/capability/via/note。
    entries.value = data.capabilities ?? data.entries ?? []
    markdown.value = data.markdown ?? ''
  } finally {
    loading.value = false
  }
}

const copyMd = async () => {
  if (await copyToClipboard(markdown.value)) {
    ElMessage.success('已复制')
  } else {
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
