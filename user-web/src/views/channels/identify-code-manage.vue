<template>
  <div class="identify-code-manage">
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>渠道识别码管理（一次性替换旧 Bridge token）</span>
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
            {{ channelName(row.channel_id) }}
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

      <div class="create-bar">
        <el-select
          v-model="newChannelId"
          placeholder="选择尚未生成识别码的渠道"
          clearable
          style="width: 260px"
        >
          <el-option
            v-for="c in channels"
            :key="c.id"
            :label="c.name"
            :value="c.id"
          />
        </el-select>
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
import { ref, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import channelAccountApi from '@/api/channelAccount.js'
import channelIdentifyCodeApi from '@/api/channelIdentifyCode.js'

const loading = ref(false)
const rotating = ref(false)
const rotationError = ref('')
const rows = ref([])
const channels = ref([])
const newChannelId = ref(null)

const channelMap = computed(() => {
  const m = {}
  for (const c of channels.value) m[c.id] = c.name
  return m
})

function channelName(id) {
  return channelMap.value[id] || `- (${id})`
}

async function loadList() {
  loading.value = true
  try {
    const [codes, chans] = await Promise.all([
      channelIdentifyCodeApi.list(),
      channelAccountApi.listChannels()
    ])
    rows.value = codes?.list || codes || []
    channels.value = chans?.list || chans || []
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
    `确认重置渠道「${channelName(row.channel_id)}」的识别码？旧码仅在短暂在途窗口内有效。`,
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

async function generateCode() {
  if (!newChannelId.value || rotating.value) return
  const channelId = newChannelId.value
  newChannelId.value = null
  await rotate(channelId)
}

onMounted(loadList)
</script>

<style scoped>
.card-header { display: flex; justify-content: space-between; align-items: center; }
.create-bar { margin-top: 16px; display: flex; gap: 12px; align-items: center; }
</style>
