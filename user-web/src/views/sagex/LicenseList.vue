<template>
  <div class="sagex-license">
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>SageX 授权码管理</span>
          <div class="header-actions">
            <el-button type="primary" :loading="issuing" @click="issue">签发授权码</el-button>
            <el-button :loading="loading" @click="load">刷新</el-button>
          </div>
        </div>
      </template>

      <el-table :data="codes" v-loading="loading" stripe>
        <el-table-column prop="code" label="授权码" min-width="260" show-overflow-tooltip />
        <el-table-column prop="status" label="状态" width="110" />
        <el-table-column prop="device_model" label="设备" min-width="160" show-overflow-tooltip />
        <el-table-column prop="ip" label="IP" width="140" />
        <el-table-column prop="phone" label="手机号" width="140" />
        <el-table-column prop="activated_at" label="激活时间" width="180" />
        <el-table-column label="操作" width="200">
          <template #default="{ row }">
            <el-button size="small" @click="showHeartbeats(row.code)">心跳</el-button>
            <el-button
              size="small"
              type="danger"
              :disabled="row.status === 'invalidated'"
              @click="invalidate(row.code)"
            >
              失效
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog v-model="hbVisible" title="心跳记录" width="720px">
      <el-table :data="heartbeats" v-loading="hbLoading" stripe max-height="420">
        <el-table-column prop="device_model" label="设备" min-width="160" />
        <el-table-column prop="ip" label="IP" width="140" />
        <el-table-column prop="phone" label="手机号" width="140" />
        <el-table-column prop="app_version" label="版本" width="100" />
        <el-table-column prop="reported_at" label="上报时间" width="180" />
      </el-table>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { listSageXCodes, issueSageXCode, invalidateSageXCode, listSageXHeartbeats } from '@/api/sagexLicense'

const loading = ref(false)
const issuing = ref(false)
const codes = ref([])

const hbVisible = ref(false)
const hbLoading = ref(false)
const heartbeats = ref([])
const currentCode = ref('')

async function load() {
  loading.value = true
  try {
    const res = await listSageXCodes()
    codes.value = Array.isArray(res) ? res : (res?.data || [])
  } catch (e) {
    ElMessage.error(e?.message || '加载失败')
  } finally {
    loading.value = false
  }
}

async function issue() {
  issuing.value = true
  try {
    const res = await issueSageXCode()
    ElMessage.success('已签发: ' + (res?.code || res?.data?.code || ''))
    await load()
  } catch (e) {
    ElMessage.error(e?.message || '签发失败')
  } finally {
    issuing.value = false
  }
}

async function invalidate(code) {
  try {
    await ElMessageBox.confirm('确认将该授权码置为失效？', '提示', { type: 'warning' })
  } catch {
    return
  }
  try {
    await invalidateSageXCode(code, 'manual')
    ElMessage.success('已失效')
    await load()
  } catch (e) {
    ElMessage.error(e?.message || '操作失败')
  }
}

async function showHeartbeats(code) {
  currentCode.value = code
  hbVisible.value = true
  hbLoading.value = true
  try {
    const res = await listSageXHeartbeats(code)
    heartbeats.value = Array.isArray(res) ? res : (res?.data || [])
  } catch (e) {
    ElMessage.error(e?.message || '加载失败')
  } finally {
    hbLoading.value = false
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
.header-actions {
  display: flex;
  gap: 8px;
}
</style>