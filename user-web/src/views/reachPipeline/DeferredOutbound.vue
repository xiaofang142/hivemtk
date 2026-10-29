<template>
  <div class="app-container">
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>延迟出站管理</span>
          <el-button
            size="small"
            :loading="loading"
            @click="load"
          >
            刷新
          </el-button>
        </div>
      </template>

      <el-alert
        type="info"
        :closable="false"
        show-icon
        class="intro-alert"
        title="静默时段 / 合规窗口内暂缓的主动触达会进入这里，到期后由后台重发。只有 pending 状态可以取消；取消后不会再发送。"
      />

      <div class="toolbar">
        <el-select
          v-model="filters.status"
          placeholder="状态"
          clearable
          style="width: 150px"
          @change="onFilterChange"
        >
          <el-option
            v-for="s in STATUS_OPTIONS"
            :key="s.value"
            :label="s.label"
            :value="s.value"
          />
        </el-select>
        <el-select
          v-model="filters.channel"
          placeholder="渠道"
          clearable
          style="width: 160px"
          @change="onFilterChange"
        >
          <el-option
            v-for="c in CHANNEL_OPTIONS"
            :key="c.value"
            :label="c.label"
            :value="c.value"
          />
        </el-select>
        <el-button @click="resetFilters">
          重置
        </el-button>
      </div>

      <el-table
        v-loading="loading"
        :data="list"
        border
        size="small"
      >
        <el-table-column
          prop="id"
          label="ID"
          width="80"
        />
        <el-table-column
          label="渠道"
          width="120"
        >
          <template #default="{ row }">
            <el-tag
              size="small"
              effect="plain"
            >
              {{ channelLabel(row.channel) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="recipient_id"
          label="接收方"
          min-width="140"
          show-overflow-tooltip
        />
        <el-table-column
          prop="customer_id"
          label="客户 ID"
          min-width="140"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            {{ row.customer_id || '-' }}
          </template>
        </el-table-column>
        <el-table-column
          label="延后原因"
          width="120"
        >
          <template #default="{ row }">
            {{ purposeText(row.purpose) }}
          </template>
        </el-table-column>
        <el-table-column
          label="计划发送时间"
          width="170"
        >
          <template #default="{ row }">
            {{ formatTime(row.send_at) }}
          </template>
        </el-table-column>
        <el-table-column
          label="状态"
          width="100"
        >
          <template #default="{ row }">
            <el-tag
              :type="statusType(row.status)"
              size="small"
            >
              {{ statusText(row.status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="尝试次数"
          width="90"
          align="center"
        >
          <template #default="{ row }">
            {{ row.attempts ?? 0 }}
          </template>
        </el-table-column>
        <el-table-column
          label="最近错误"
          min-width="200"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            <span :class="{ 'error-text': row.last_error }">{{ row.last_error || '-' }}</span>
          </template>
        </el-table-column>
        <el-table-column
          label="创建时间"
          width="170"
        >
          <template #default="{ row }">
            {{ formatTime(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="100"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button
              v-if="row.status === 'pending'"
              link
              type="danger"
              :loading="cancellingId === row.id"
              @click="cancel(row)"
            >
              取消
            </el-button>
            <span
              v-else
              class="muted"
            >—</span>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty description="没有符合条件的延迟出站记录" />
        </template>
      </el-table>

      <div class="pager">
        <el-pagination
          background
          layout="total, sizes, prev, pager, next, jumper"
          :total="total"
          :page-size="pager.page_size"
          :current-page="pager.page"
          :page-sizes="[10, 20, 50, 100]"
          @size-change="onSizeChange"
          @current-change="onPageChange"
        />
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { reachPipelineApi } from '@/api/reachPipeline.js'
import { CHANNEL_OPTIONS, getChannelLabel } from '@/constants/channel'

const STATUS_OPTIONS = [
  { value: 'pending', label: '待发送' },
  { value: 'sending', label: '发送中' },
  { value: 'sent', label: '已发送' },
  { value: 'failed', label: '发送失败' },
  { value: 'cancelled', label: '已取消' },
  { value: 'unknown', label: '结果未知' }
]

const channelLabel = getChannelLabel

const list = ref([])
const total = ref(0)
const loading = ref(false)
const cancellingId = ref(null)
const filters = reactive({ status: '', channel: '' })
const pager = reactive({ page: 1, page_size: 20 })

const load = async () => {
  loading.value = true
  try {
    const res = await reachPipelineApi.getDeferredOutbounds({
      status: filters.status || undefined,
      channel: filters.channel || undefined,
      page: pager.page,
      page_size: pager.page_size
    })
    list.value = res?.list || []
    total.value = Number(res?.total || 0)
  } catch (e) {
    ElMessage.error(e.message || '加载延迟出站记录失败')
  } finally {
    loading.value = false
  }
}

const onFilterChange = () => {
  pager.page = 1
  load()
}

const resetFilters = () => {
  filters.status = ''
  filters.channel = ''
  pager.page = 1
  load()
}

const onSizeChange = (size) => {
  pager.page_size = size
  pager.page = 1
  load()
}

const onPageChange = (page) => {
  pager.page = page
  load()
}

const cancel = async (row) => {
  try {
    await ElMessageBox.confirm(
      `确认取消延迟出站 #${row.id}？取消后这条触达不会再发送。`,
      '取消延迟出站',
      { type: 'warning', confirmButtonText: '取消发送', cancelButtonText: '返回' }
    )
  } catch {
    return
  }
  cancellingId.value = row.id
  try {
    await reachPipelineApi.cancelDeferredOutbound(row.id)
    ElMessage.success('已取消')
    load()
  } catch (e) {
    if (e.status === 404) {
      ElMessage.error('该记录已不处于待发送状态，无法取消（可能已被发送或取消）')
      load()
    } else {
      ElMessage.error(e.message || '取消失败')
    }
  } finally {
    cancellingId.value = null
  }
}

const statusText = (s) => STATUS_OPTIONS.find((o) => o.value === s)?.label || s || '-'

const statusType = (s) => ({
  pending: 'warning',
  sending: 'primary',
  sent: 'success',
  failed: 'danger',
  cancelled: 'info',
  unknown: 'danger'
}[s] || 'info')

const purposeText = (p) => ({
  quiet_hours: '静默时段',
  compliance: '合规窗口'
}[p] || p || '-')

const formatTime = (value) => {
  if (!value) return '-'
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return '-'
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

onMounted(load)
</script>

<style scoped>
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.intro-alert {
  margin-bottom: 12px;
}

.toolbar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 12px;
}

.pager {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}

.error-text {
  color: #f56c6c;
}

.muted {
  color: #c0c4cc;
}
</style>
