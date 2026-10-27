<template>
  <div class="page">
    <h2>ego-runner 状态</h2>

    <el-card style="margin-top: 12px">
      <el-space
        direction="vertical"
        alignment="flex-start"
        :size="12"
        style="width: 100%"
      >
        <el-space>
          <el-button
            :loading="loading"
            @click="load"
          >
            刷新
          </el-button>
          <el-button
            type="primary"
            @click="issueVisible = true"
          >
            签发新令牌
          </el-button>
        </el-space>

        <el-alert
          v-if="!loading && runners.length === 0"
          type="warning"
          :closable="false"
          show-icon
          title="当前没有 ego-runner 连上服务端"
        >
          <ego-runner-install-guide />
        </el-alert>

        <el-table
          v-if="runners.length"
          :data="runners"
          border
        >
          <el-table-column
            prop="user_id"
            label="用户 ID"
            width="100"
          />
          <el-table-column
            prop="device_id"
            label="设备"
            min-width="220"
            show-overflow-tooltip
          />
          <el-table-column
            prop="hostname"
            label="主机名"
            min-width="140"
          />
          <el-table-column
            prop="version"
            label="runner 版本"
            width="120"
          />
          <el-table-column
            prop="ego_version"
            label="ego-browser"
            width="140"
          />
          <el-table-column
            prop="workspace"
            label="workspace"
            min-width="220"
            show-overflow-tooltip
          />
        </el-table>
      </el-space>
    </el-card>

    <el-card style="margin-top: 12px">
      <template #header>
        我的 runner 令牌
        <span class="hint">（明文只在签发那一刻显示一次；库内只存 sha256）</span>
      </template>
      <el-table
        :data="tokens"
        border
      >
        <el-table-column
          prop="token_hint"
          label="令牌"
          width="180"
        />
        <el-table-column
          prop="label"
          label="备注"
          min-width="140"
        />
        <el-table-column
          prop="created_at"
          label="签发时间"
          width="180"
        />
        <el-table-column
          prop="last_seen_at"
          label="最近使用"
          width="180"
        >
          <template #default="{ row }">
            {{ row.last_seen_at || '-' }}
          </template>
        </el-table-column>
        <el-table-column
          label="状态"
          width="100"
        >
          <template #default="{ row }">
            <el-tag :type="row.revoked_at ? 'info' : 'success'">
              {{ row.revoked_at ? '已撤销' : '有效' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="110"
        >
          <template #default="{ row }">
            <el-button
              size="small"
              type="danger"
              :disabled="!!row.revoked_at"
              @click="onRevoke(row)"
            >
              撤销
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog
      v-model="issueVisible"
      title="签发 runner 令牌"
      width="520px"
    >
      <el-form label-width="80px">
        <el-form-item label="备注">
          <el-input
            v-model="issueLabel"
            placeholder="例如：办公室 Mac（便于以后认出是哪台机器）"
          />
        </el-form-item>
      </el-form>
      <template v-if="issuedToken">
        <el-alert
          type="success"
          :closable="false"
          show-icon
          title="令牌已签发，请立即复制（关闭后无法再次查看）"
        />
        <div class="token-box">
          <code>{{ issuedToken }}</code>
        </div>
        <el-button
          size="small"
          @click="copyToken"
        >
          复制
        </el-button>
      </template>
      <template #footer>
        <el-button @click="closeIssue">
          关闭
        </el-button>
        <el-button
          type="primary"
          :loading="issuing"
          @click="onIssue"
        >
          签发
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import EgoRunnerInstallGuide from '../EgoRunnerInstallGuide.vue'
import {
  getEgoRunnerStatus,
  issueEgoRunnerToken,
  listEgoRunnerTokens,
  revokeEgoRunnerToken
} from '@/api/egolite'

const loading = ref(false)
const runners = ref([])
const tokens = ref([])
const issueVisible = ref(false)
const issuing = ref(false)
const issueLabel = ref('')
const issuedToken = ref('')

const load = async () => {
  loading.value = true
  try {
    const [status, tokenData] = await Promise.all([getEgoRunnerStatus(), listEgoRunnerTokens()])
    runners.value = status?.runners || []
    tokens.value = tokenData?.list || []
  } finally {
    loading.value = false
  }
}

const onIssue = async () => {
  issuing.value = true
  try {
    const data = await issueEgoRunnerToken(issueLabel.value)
    issuedToken.value = data?.token || ''
    await load()
  } finally {
    issuing.value = false
  }
}

const copyToken = async () => {
  try {
    await navigator.clipboard.writeText(issuedToken.value)
    ElMessage.success('已复制')
  } catch {
    ElMessage.warning('浏览器不允许自动复制，请手动选中复制')
  }
}

const closeIssue = () => {
  issueVisible.value = false
  issueLabel.value = ''
  issuedToken.value = ''
}

const onRevoke = async (row) => {
  await ElMessageBox.confirm(
    `撤销令牌 ${row.token_hint}？使用它的 runner 会立即断开且无法重连。`,
    '撤销令牌',
    { type: 'warning' }
  )
  await revokeEgoRunnerToken(row.id)
  ElMessage.success('已撤销')
  await load()
}

onMounted(load)
</script>

<style scoped>
.page { padding: 12px; }
.hint { color: #909399; font-size: 12px; font-weight: normal; }
.token-box {
  margin: 8px 0;
  padding: 8px 10px;
  background: #f5f7fa;
  border-radius: 4px;
  word-break: break-all;
}
</style>
