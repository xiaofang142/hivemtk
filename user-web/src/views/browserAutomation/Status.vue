<template>
  <div class="page">
    <h2>NM Host 状态</h2>

    <el-card style="margin-top: 12px">
      <el-space direction="vertical" alignment="flex-start" :size="12" style="width: 100%">
        <el-space>
          <el-button :loading="loading" @click="load">刷新</el-button>
          <el-button type="warning" @click="onResetToken">重置 Host Token</el-button>
        </el-space>

        <el-alert v-if="!loading && hosts.length === 0" type="warning" :closable="false" show-icon
          title="当前没有 Host 连上服务端">
          <p>安装步骤：</p>
          <HostInstallGuide />
        </el-alert>

        <div v-if="hosts.length" class="summary">
          连接 {{ summary.count }} 台 · 可服务 {{ summary.servableCount }} 台 · 最近成功回包 {{ summary.lastOkText }}
        </div>

        <el-table v-if="hosts.length" :data="hosts" border>
          <el-table-column prop="user_id" label="用户 ID" width="120" />
          <el-table-column prop="version" label="Host 版本" width="120" />
          <el-table-column prop="pid" label="进程 PID" width="120" />
          <el-table-column label="状态" width="130">
            <template #default="{ row }">
              <el-tag v-if="row.online && row.servable" type="success">可服务</el-tag>
              <el-tag v-else-if="row.online" type="danger">不可服务</el-tag>
              <el-tag v-else type="info">已断开</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="最近成功" min-width="180">
            <template #default="{ row }">
              <span v-if="row.last_cmd_ok_at">{{ formatTime(row.last_cmd_ok_at) }}</span>
              <el-text v-else type="warning">从未回包</el-text>
            </template>
          </el-table-column>
          <el-table-column label="判据" min-width="220">
            <template #default="{ row }">
              <span v-if="!row.online">WS 连接已断，需刷新扩展或检查 Host 进程</span>
              <span v-else-if="!row.servable">连接与注册帧在场，但服务端没拿到过一次命令回包 —— 点执行会报「Host 未连接」</span>
              <span v-else>服务端曾收到该 Host 的命令回包，可作为可执行依据</span>
            </template>
          </el-table-column>
        </el-table>
      </el-space>
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { getBrowserHostStatus, resetBrowserHostToken } from '@/api/browserAutomation'
import HostInstallGuide from './HostInstallGuide.vue'

const hosts = ref([])
const loading = ref(false)
const summaryRaw = ref({ count: 0, servable: false, last_cmd_ok_at: null })

const unpack = (res) => res?.data ?? res
const formatTime = (t) => new Date(t).toLocaleString('zh-CN')

// 顶层 count/servable 由服务端汇总（普通用户只看自己的连接），列表按 row 出读数，
// 两者取其一即可，不一致时以行数为准——row 才是「哪一台不能干活」的答案。
const summary = computed(() => {
  const rows = hosts.value
  return {
    count: rows.length || summaryRaw.value.count || 0,
    servableCount: rows.filter((h) => h.online && h.servable).length,
    lastOkText: (() => {
      const latest = rows.map((h) => h.last_cmd_ok_at).filter(Boolean).sort().pop()
        || summaryRaw.value.last_cmd_ok_at
      return latest ? formatTime(latest) : '—'
    })(),
  }
})

async function load() {
  loading.value = true
  try {
    const res = await getBrowserHostStatus()
    const data = unpack(res)
    hosts.value = data?.hosts || []
    summaryRaw.value = { count: data?.count ?? 0, servable: data?.servable === true, last_cmd_ok_at: data?.last_cmd_ok_at ?? null }
  } catch (e) {
    hosts.value = []
    ElMessage.error(String(e?.message || e))
  } finally {
    loading.value = false
  }
}

async function onResetToken() {
  let go = true
  try {
    await ElMessageBox.confirm(
      '重置后旧 token 将进入过渡期（_prev），已配置的 NM Host 需更新 ~/.hivemtk/nm_host.conf，且改完配置要重启 Host 进程（扩展不会自动重连）。继续？',
      '重置 Host Token', { type: 'warning' },
    )
  } catch { go = false } // 用户取消不是失败
  if (!go) return
  try {
    const res = await resetBrowserHostToken()
    const data = unpack(res)
    await ElMessageBox.alert(
      `新 token：<code style="user-select:all">${data?.token || ''}</code><br/>请将其填入本机 ~/.hivemtk/nm_host.conf 的 token= 行，然后重启 Host 进程`,
      '已重置', { dangerouslyUseHTMLString: true },
    )
  } catch (e) {
    ElMessage.error(`重置失败：${String(e?.message || e)}`)
  }
}

onMounted(load)
</script>

<style scoped>
.page { padding: 16px; }
.summary { color: #606266; font-size: 13px; }
</style>
