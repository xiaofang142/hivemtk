<template>
  <div class="identify-code-manage">
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>识别码</span>
          <el-alert
            type="warning"
            :closable="false"
            show-icon
            title="重置后旧码仅在短暂在途窗口内有效；请同步更新所有扩展/上报端的识别码，否则请求会被 401 拒绝。"
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
          prop="id"
          label="ID"
          width="80"
        />
        <el-table-column
          prop="channel_id"
          label="渠道 ID"
          width="100"
        />
        <el-table-column label="渠道">
          <template #default="{ row }">
            {{ catalog.channelLabel(row.channel_id) }}
          </template>
        </el-table-column>
        <el-table-column
          prop="code"
          label="识别码"
          min-width="240"
          show-overflow-tooltip
        />
        <el-table-column
          prop="version"
          label="版本"
          width="80"
        />
        <el-table-column
          prop="reset_at"
          label="最近重置"
          width="180"
        />
        <el-table-column
          label="操作"
          width="140"
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
              :loading="rotating"
              :disabled="rotating"
              @click="resetCode(row)"
            >
              重置
            </el-button>
          </template>
        </el-table-column>
      </el-table>

      <h3 class="section-title">
        {{ $t('channels.accountIdentifyCode') }}
      </h3>
      <el-table
        v-loading="loading"
        :data="accountRows"
        size="small"
        border
        stripe
      >
        <el-table-column
          prop="id"
          label="账号 ID"
          width="90"
        />
        <el-table-column
          prop="name"
          label="账号名称"
          min-width="160"
          show-overflow-tooltip
        />
        <el-table-column label="渠道">
          <template #default="{ row }">
            {{ catalog.channelLabel(row.channel_id) }}
          </template>
        </el-table-column>
        <el-table-column
          prop="identify_code"
          :label="$t('channels.accountIdentifyCode')"
          min-width="240"
          show-overflow-tooltip
        />
        <el-table-column
          label="操作"
          width="150"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button
              link
              type="primary"
              size="small"
              @click="copyCode(row.identify_code)"
            >
              复制
            </el-button>
            <el-button
              link
              type="warning"
              size="small"
              :loading="rotatingAccountId === row.id"
              @click="resetAccountCode(row)"
            >
              {{ $t('channels.resetAccountCode') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>

      <div class="create-bar">
        <ChannelSelect
          v-model="newChannelId"
          value-key="id"
          include-disabled
          placeholder="选择尚未生成识别码的渠道"
          style="width: 260px"
        />
        <el-button
          type="primary"
          size="small"
          :loading="rotating"
          :disabled="!newChannelId || rotating"
          @click="generateCode"
        >
          生成识别码
        </el-button>
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import channelCatalogApi from '@/api/channelCatalog.js'
import channelIdentifyCodeApi from '@/api/channelIdentifyCode.js'
import ChannelSelect from '@/components/ChannelSelect.vue'
import { useChannelCatalogStore } from '@/stores/channelCatalog'

const catalog = useChannelCatalogStore()
const loading = ref(false)
const rotating = ref(false)
const rotatingAccountId = ref(null)
const rotationError = ref('')
const rows = ref([])
const accountRows = ref([])
const newChannelId = ref(null)

async function loadList() {
  loading.value = true
  try {
    const [codes, accounts] = await Promise.all([
      channelIdentifyCodeApi.list(),
      channelCatalogApi.managedAccounts()
    ])
    rows.value = codes?.list || codes || []
    accountRows.value = accounts?.list || accounts || []
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

async function rotate(channelId) {
  if (rotating.value) return
  rotating.value = true
  rotationError.value = ''
  try {
    const res = await channelIdentifyCodeApi.rotate(channelId)
    const code = res?.code || res?.data?.code || ''
    ElMessage.success(code ? `已重置，新识别码：${code}` : '已重置识别码')
    await loadList()
  } catch (e) {
    const message = e?.message || '未知错误'
    rotationError.value = `识别码操作失败：${message}`
    ElMessage.error(rotationError.value)
  } finally {
    rotating.value = false
  }
}

function isConfirmCancel(error) {
  return error === 'cancel' || error === 'close'
}

function resetCode(row) {
  ElMessageBox.confirm(
    `确认重置渠道「${catalog.channelLabel(row.channel_id)}」的识别码？旧码仅在短暂在途窗口内有效。`,
    '提示',
    { type: 'warning' }
  ).then(() => rotate(row.channel_id)).catch((error) => {
    if (!isConfirmCancel(error)) {
      const message = error?.message || '未知错误'
      rotationError.value = `识别码操作失败：${message}`
      ElMessage.error(rotationError.value)
    }
  })
}

async function resetAccountCode(row) {
  if (rotatingAccountId.value != null) return
  try {
    await ElMessageBox.confirm(
      `确认重置账号「${row.name}」的识别码？旧码仅在短暂在途窗口内有效。`,
      '提示',
      { type: 'warning' }
    )
  } catch (e) {
    if (e === 'cancel' || e === 'close') return
  }
  rotatingAccountId.value = row.id
  rotationError.value = ''
  try {
    const res = await channelIdentifyCodeApi.rotateForAccount(row.channel_id, row.id)
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

async function generateCode() {
  if (!newChannelId.value || rotating.value) return
  const channelId = newChannelId.value
  newChannelId.value = null
  await rotate(channelId)
}

onMounted(async () => {
  await catalog.ensure()
  await loadList()
})
</script>

<style scoped>
.card-header { display: flex; justify-content: space-between; align-items: center; }
.section-title { margin: 20px 0 8px; font-size: 15px; }
.create-bar { margin-top: 16px; display: flex; gap: 12px; align-items: center; }
</style>
