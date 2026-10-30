<template>
  <div class="agent-status-page">
    <el-card class="header-card">
      <div>
        <h2>{{ $t('坐席状态') }}</h2>
        <p class="subtitle">
          {{ $t('维护客服在线状态、当前会话数与接待上限') }}
        </p>
      </div>
      <el-button
        type="primary"
        @click="openCreate"
      >
        <el-icon><Plus /></el-icon>
        {{ $t('新增坐席') }}
      </el-button>
    </el-card>

    <el-row
      :gutter="20"
      class="stat-row"
    >
      <el-col :span="6">
        <el-card>
          <div class="stat-item">
            <div class="stat-label">
              {{ $t('坐席总数') }}
            </div>
            <div class="stat-value">
              {{ agents.length }}
            </div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card>
          <div class="stat-item">
            <div class="stat-label">
              {{ $t('在线坐席') }}
            </div>
            <div
              class="stat-value"
              style="color: #10B981"
            >
              {{ onlineCount }}
            </div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card>
          <div class="stat-item">
            <div class="stat-label">
              {{ $t('当前会话') }}
            </div>
            <div class="stat-value">
              {{ totalSessions }}
            </div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card>
          <div class="stat-item">
            <div class="stat-label">
              {{ $t('离线坐席') }}
            </div>
            <div
              class="stat-value"
              style="color: #EF4444"
            >
              {{ agents.length - onlineCount }}
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-card>
      <template #header>
        <div class="card-header">
          <span>{{ $t('坐席列表') }}</span>
          <el-input
            v-model="search"
            :placeholder="$t('搜索姓名/坐席 ID')"
            clearable
            style="width: 240px"
          />
        </div>
      </template>
      <el-table
        v-loading="loading"
        :data="filtered"
        stripe
      >
        <el-table-column
          prop="agent_id"
          label="坐席 ID"
          width="120"
        />
        <el-table-column
          prop="agent_name"
          label="姓名"
          min-width="120"
        />
        <el-table-column
          prop="status"
          label="状态"
          width="100"
        >
          <template #default="{ row }">
            <el-tag
              :type="getAgentStatusTagType(row.status)"
              size="small"
            >
              {{ getAgentStatusLabel(row.status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="active_sessions"
          label="当前会话"
          width="100"
          align="center"
        />
        <el-table-column
          prop="max_sessions"
          label="接待上限"
          width="100"
          align="center"
        />
        <el-table-column
          prop="last_active_at"
          label="最后活跃"
          min-width="180"
        >
          <template #default="{ row }">
            {{ formatTime(row.last_active_at || row.online_at) }}
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="280"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button
              v-if="row.status !== 'online'"
              link
              type="success"
              @click="handleGoOnline(row)"
            >
              上线
            </el-button>
            <el-button
              v-else
              link
              type="warning"
              @click="handleGoOffline(row)"
            >
              下线
            </el-button>
            <el-button
              link
              type="primary"
              @click="openStatusDialog(row)"
            >
              修改状态
            </el-button>
            <el-button
              link
              type="info"
              @click="loadSessions(row)"
            >
              查看会话
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    
    <el-dialog
      v-model="createVisible"
      title="新增坐席"
      width="420px"
    >
      <el-form
        :model="createForm"
        label-width="100px"
      >
        <el-form-item
          label="坐席 ID"
          required
        >
          <el-input-number
            v-model="createForm.id"
            :min="1"
            :step="1"
            placeholder="唯一坐席 ID (整数)"
            style="width: 100%"
          />
        </el-form-item>
        <el-form-item
          label="姓名"
          required
        >
          <el-input v-model="createForm.name" />
        </el-form-item>
        <el-form-item label="接待上限">
          <el-input-number
            v-model="createForm.max_sessions"
            :min="1"
            :max="50"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          @click="submitCreate"
        >
          保存
        </el-button>
      </template>
    </el-dialog>

    
    <el-dialog
      v-model="statusVisible"
      title="修改坐席状态"
      width="380px"
    >
      <el-form
        :model="statusForm"
        label-width="100px"
      >
        <el-form-item label="状态">
          <el-select
            v-model="statusForm.status"
            style="width: 100%"
          >
            <el-option
              label="在线"
              value="online"
            />
            <el-option
              label="忙碌"
              value="busy"
            />
            <el-option
              label="离线"
              value="offline"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="接待上限">
          <el-input-number
            v-model="statusForm.max_sessions"
            :min="1"
            :max="50"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="statusVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          @click="submitStatus"
        >
          保存
        </el-button>
      </template>
    </el-dialog>

    
    <el-dialog
      v-model="sessionsVisible"
      :title="`坐席 ${currentAgent.agent_name || currentAgent.agent_id || currentAgent.id} 的会话`"
      width="640px"
    >
      <el-table
        v-loading="sessionsLoading"
        :data="agentSessions"
        stripe
      >
        <el-table-column
          prop="id"
          label="会话 ID"
          min-width="100"
        />
        <el-table-column
          prop="user_name"
          label="客户"
          min-width="120"
        />
        <el-table-column
          label="渠道"
          width="100"
        >
          <template #default="{ row }">
            {{ getChannelLabel(row.platform) }}
          </template>
        </el-table-column>
        <el-table-column
          prop="status"
          label="状态"
          width="100"
        >
          <template #default="{ row }">
            <el-tag
              :type="getSessionStatusTagType(row.status)"
              size="small"
            >
              {{ getSessionStatusLabel(row.status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="started_at"
          label="开始时间"
          min-width="160"
        >
          <template #default="{ row }">
            {{ formatTime(row.started_at) }}
          </template>
        </el-table-column>
      </el-table>
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import {
  listAllAgents,
  createAgent,
  updateAgentStatus,
  goOnline,
  goOffline,
  getAgentSessions
} from '@/api/customerService.js'
import { getChannelLabel } from '@/constants/channel';
import { isUserReportedError } from '@/utils/errorReporting';
import { getEnabledLabel, getEnabledTagType } from '@/constants/enabled';
import { AGENT_STATUS, SESSION_STATUS, getStatusLabel, getStatusTagType } from '@/constants/status';

const getAgentStatusLabel = (s) => getStatusLabel(s, AGENT_STATUS);
const getAgentStatusTagType = (s) => getStatusTagType(s, AGENT_STATUS)
const getSessionStatusLabel = (s) => getStatusLabel(s, SESSION_STATUS);
const getSessionStatusTagType = (s) => getStatusTagType(s, SESSION_STATUS)

const loading = ref(false)
const search = ref('')
const agents = ref([])

const createVisible = ref(false)
const createForm = reactive({ id: '', name: '', max_sessions: 5 })

const statusVisible = ref(false)
const statusForm = reactive({ id: null, status: 'online', max_sessions: 5 })

const sessionsVisible = ref(false)
const sessionsLoading = ref(false)
const agentSessions = ref([])
const currentAgent = ref({})

const onlineCount = computed(() => agents.value.filter((a) => a.status === 'online').length)
const totalSessions = computed(() => agents.value.reduce((s, a) => s + (a.active_sessions || 0), 0))

const filtered = computed(() => {
  if (!search.value) return agents.value
  const kw = search.value.toLowerCase()
  return agents.value.filter(
    (a) => String(a.id).includes(kw) || a.name?.toLowerCase().includes(kw)
  )
})

const formatTime = (val) => {
  if (!val) return '-'
  const d = new Date(val)
  if (isNaN(d.getTime())) return '-'
  return d.toLocaleString('zh-CN', { hour12: false })
}

const loadAgents = async () => {
  loading.value = true
  try {
    const res = await listAllAgents()
    const data = res || []
    agents.value = Array.isArray(data) ? data : data.list || []
  } catch (e) {
    ElMessage.error(i18n.global.t('加载坐席失败'))
    agents.value = []
  } finally {
    loading.value = false
  }
}

const openCreate = () => {
  Object.assign(createForm, { id: 1, name: '', max_sessions: 5 })
  createVisible.value = true
}

const submitCreate = async () => {
  if (!createForm.id || !createForm.name) {
    ElMessage.warning(i18n.global.t('请填写坐席 ID 和姓名'))
    return
  }
  const agentId = Number(createForm.id)
  if (!Number.isInteger(agentId) || agentId <= 0) {
    ElMessage.warning(i18n.global.t('坐席 ID 必须为正整数'))
    return
  }
  try {
    await createAgent({
      agent_id: agentId,
      agent_name: createForm.name,
      max_sessions: createForm.max_sessions
    })
    ElMessage.success(i18n.global.t('坐席已创建'))
    createVisible.value = false
    await loadAgents()
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(e?.message || '创建失败')
  }
}

const handleGoOnline = async (row) => {
  try {
    await goOnline(row.agent_id)
    ElMessage.success(i18n.global.t('已上线'))
    await loadAgents()
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(i18n.global.t('上线失败'))
  }
}

const handleGoOffline = async (row) => {
  try {
    await goOffline(row.agent_id)
    ElMessage.success(i18n.global.t('已下线'))
    await loadAgents()
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(i18n.global.t('下线失败'))
  }
}

const openStatusDialog = (row) => {
  statusForm.id = row.agent_id
  statusForm.status = row.status || 'online'
  statusForm.max_sessions = row.max_sessions || 5
  statusVisible.value = true
}

const submitStatus = async () => {
  try {
    await updateAgentStatus(statusForm.id, {
      status: statusForm.status,
      max_sessions: statusForm.max_sessions
    })
    ElMessage.success(i18n.global.t('状态已更新'))
    statusVisible.value = false
    await loadAgents()
  } catch (e) {
    ElMessage.error(i18n.global.t('更新失败'))
  }
}

const loadSessions = async (row) => {
  currentAgent.value = row
  agentSessions.value = []
  sessionsVisible.value = true
  sessionsLoading.value = true
  try {
    const res = await getAgentSessions(row.agent_id)
    const data = res || []
    agentSessions.value = Array.isArray(data) ? data : data.list || []
  } catch (e) {
    ElMessage.error(i18n.global.t('加载会话失败'))
    agentSessions.value = []
  } finally {
    sessionsLoading.value = false
  }
}

onMounted(() => loadAgents())
</script>

<style scoped lang="scss">
.agent-status-page { padding: 20px; }
.header-card {
  margin-bottom: 20px;
  :deep(.el-card__body) { display: flex; justify-content: space-between; align-items: center; }
  h2 { margin: 0 0 8px 0; }
  .subtitle { color: #909399; margin: 0; }
}
.stat-row {
  margin-bottom: 20px;
  .stat-item {
    text-align: center;
    .stat-label { color: #909399; font-size: 14px; margin-bottom: 10px; }
    .stat-value { font-size: 28px; font-weight: bold; }
  }
}
.card-header { display: flex; justify-content: space-between; align-items: center; }
</style>
