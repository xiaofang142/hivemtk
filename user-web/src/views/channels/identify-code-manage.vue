<template>
  <div class="identify-code-manage">
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>{{ $t('channels.accountIdentifyCode') }}</span>
          <el-alert
            type="warning"
            :closable="false"
            show-icon
            title="每个渠道账号一枚唯一识别码；重置后旧码仅在短暂在途窗口内有效，请同步更新所有扩展/上报端，否则请求会被 401 拒绝。"
          />
        </div>
      </template>

      <el-alert
        v-if="rotationError"
        class="rotation-error"
        type="error"
        :closable="false"
        show-icon
        :title="rotationError"
      />

      <el-table
        v-loading="loading"
        :data="rows"
        size="small"
        border
        stripe
      >
        <el-table-column
          prop="account_id"
          label="账号 ID"
          width="100"
        >
          <template #default="{ row }">
            {{ row.account_id || '—' }}
          </template>
        </el-table-column>
        <el-table-column
          prop="account_name"
          label="账号名称"
          min-width="180"
          show-overflow-tooltip
        />
        <el-table-column label="渠道">
          <template #default="{ row }">
            {{ catalog.channelLabel(row.channel_id) }}
          </template>
        </el-table-column>
        <el-table-column
          prop="code"
          :label="$t('channels.accountIdentifyCode')"
          min-width="240"
          show-overflow-tooltip
        />
        <el-table-column
          label="操作"
          width="170"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button
              link
              type="primary"
              size="small"
              @click="copyCode(row.code)"
            >
              复制
            </el-button>
            <el-button
              link
              type="warning"
              size="small"
              :loading="rotatingAccountId === row.id"
              :disabled="!row.account_id || rotatingAccountId != null"
              @click="resetCode(row)"
            >
              {{ $t('channels.resetAccountCode') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import channelIdentifyCodeApi from '@/api/channelIdentifyCode.js'
import { useChannelCatalogStore } from '@/stores/channelCatalog'

const catalog = useChannelCatalogStore()
const loading = ref(false)
const rotatingAccountId = ref(null)
const rotationError = ref('')
const rows = ref([])

async function loadList() {
  loading.value = true
  try {
    const res = await channelIdentifyCodeApi.list()
    rows.value = res?.list || res || []
  } catch (e) {
    ElMessage.error('加载识别码失败：' + (e.message || '未知错误'))
  } finally {
    loading.value = false
  }
}

async function copyCode(code) {
  try {
    await navigator.clipboard.writeText(code)
    ElMessage.success('已复制识别码')
  } catch {
    ElMessage.warning('复制失败，请手动选择文本复制')
  }
}

function isConfirmCancel(error) {
  return error === 'cancel' || error === 'close'
}

async function resetCode(row) {
  if (!row.account_id || rotatingAccountId.value != null) return
  try {
    await ElMessageBox.confirm(
      `确认重置账号「${row.account_name || row.account_id}」的识别码？旧码仅在短暂在途窗口内有效。`,
      '提示',
      { type: 'warning' }
    )
  } catch (error) {
    if (!isConfirmCancel(error)) {
      const message = error?.message || '未知错误'
      rotationError.value = `识别码操作失败：${message}`
      ElMessage.error(rotationError.value)
    }
    return
  }
  rotatingAccountId.value = row.id
  rotationError.value = ''
  try {
    const res = await channelIdentifyCodeApi.rotateForAccount(row.channel_id, row.account_id)
    const code = res?.code || res?.data?.code || ''
    ElMessage.success(code ? `已重置，新识别码：${code}` : '已重置识别码')
    await Promise.all([loadList(), catalog.ensure(true)])
  } catch (e) {
    rotationError.value = `识别码操作失败：${e?.message || '未知错误'}`
    ElMessage.error(rotationError.value)
  } finally {
    rotatingAccountId.value = null
  }
}

onMounted(async () => {
  await catalog.ensure()
  await loadList()
})
</script>

<style scoped>
.card-header { display: flex; justify-content: space-between; align-items: center; }
</style>
