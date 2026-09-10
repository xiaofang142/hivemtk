<template>
  <div class="page">
    <h2>NM Host 状态</h2>

    <el-card style="margin-top: 12px">
      <el-space direction="vertical" alignment="flex-start" :size="12" style="width: 100%">
        <el-space>
          <el-button :loading="loading" @click="load">刷新</el-button>
          <el-button type="warning" @click="onResetToken">重置 Host Token</el-button>
        </el-space>

        <el-alert v-if="!loading && hosts.length === 0" type="warning" :closable="false" show-icon title="当前没有 Host 在线">
          <p>安装步骤：</p>
          <ol style="margin: 4px 0 0 16px; line-height: 1.9">
            <li>chrome://extensions → 加载已解压扩展 → 选择 <code>user-web/browser_automation/dist</code></li>
            <li>运行 <code>user-server/cmd/nm-host/install.sh &lt;扩展ID&gt;</code></li>
            <li>编辑 <code>~/.hivemtk/nm_host.conf</code> 填入 token</li>
            <li>刷新扩展后，本页再点「刷新」</li>
          </ol>
        </el-alert>

        <el-table v-if="hosts.length" :data="hosts" border>
          <el-table-column prop="user_id" label="用户 ID" width="120" />
          <el-table-column prop="version" label="Host 版本" width="120" />
          <el-table-column prop="pid" label="进程 PID" width="120" />
          <el-table-column label="状态" width="100">
            <template #default><el-tag type="success">在线</el-tag></template>
          </el-table-column>
        </el-table>
      </el-space>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { getBrowserHostStatus, resetBrowserHostToken } from '@/api/browserAutomation'

const hosts = ref([])
const loading = ref(false)

const unpack = (res) => res?.data ?? res

async function load() {
  loading.value = true
  try {
    const res = await getBrowserHostStatus()
    const data = unpack(res)
    hosts.value = data?.hosts || []
  } finally {
    loading.value = false
  }
}

async function onResetToken() {
  await ElMessageBox.confirm(
    '重置后旧 token 将进入过渡期（_prev），已配置的 NM Host 需更新 ~/.hivemtk/nm_host.conf。继续？',
    '重置 Host Token', { type: 'warning' },
  )
  const res = await resetBrowserHostToken()
  const data = unpack(res)
  await ElMessageBox.alert(
    `新 token：<code style="user-select:all">${data?.token || ''}</code><br/>请将其填入本机 ~/.hivemtk/nm_host.conf 的 token= 行`,
    '已重置', { dangerouslyUseHTMLString: true },
  )
}

onMounted(load)
</script>

<style scoped>
.page { padding: 16px; }
</style>
