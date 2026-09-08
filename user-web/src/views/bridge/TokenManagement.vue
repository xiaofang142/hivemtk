<template>
  <div class="p-4">
    <el-card>
      <template #header>
        <div class="flex items-center justify-between">
          <span>桥接通道凭证</span>
          <el-tag :type="status.main_configured ? 'success' : 'danger'" size="small">
            {{ status.main_configured ? `已配置(${status.source === 'db' ? '运行时' : '环境变量'})` : '未配置(通道拒绝访问)' }}
          </el-tag>
        </div>
      </template>

      <el-alert v-if="status.prev_configured" type="warning" :closable="false" show-icon class="mb-3"
        title="灰度窗口中：旧凭据仍可用，请尽快更新各浏览器扩展后再次轮换以移除 PREV" />

      <el-alert type="info" :closable="false" show-icon class="mb-3"
        title="扩展端在设置页粘贴 token，请求头携带 X-Bridge-Token；SSE 场景可用 ?bridge_token= 透传" />

      <el-form label-width="90px">
        <el-form-item label="新凭证">
          <el-input v-model="newToken" readonly placeholder="点击「生成新凭证」后此处显示一次明文">
            <template #append>
              <el-button @click="copyToken" :disabled="!newToken">复制</el-button>
            </template>
          </el-input>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" :loading="resetting" @click="onReset">生成新凭证(轮换)</el-button>
          <span class="ml-3 text-xs text-gray-400">轮换后旧凭据进入灰度窗口仍可用；此明文仅显示一次</span>
        </el-form-item>
      </el-form>
    </el-card>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { getBridgeTokenStatus, resetBridgeToken } from '@/api/bridgeToken.js'

const status = ref({})
const newToken = ref('')
const resetting = ref(false)

const loadStatus = async () => {
  try {
    status.value = (await getBridgeTokenStatus()) || {}
  } catch (e) { console.warn("[request] 后台调用失败(已忽略):", e) }
}

const onReset = async () => {
  resetting.value = true
  try {
    const res = await resetBridgeToken()
    newToken.value = res?.token || ''
    ElMessage.success('已轮换；旧凭据进入灰度窗口')
    await loadStatus()
  } finally {
    resetting.value = false
  }
}

const copyToken = async () => {
  await navigator.clipboard.writeText(newToken.value)
  ElMessage.success('已复制')
}

onMounted(loadStatus)
</script>
