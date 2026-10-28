<template>
  <div class="conflicts-page">
    <el-card class="box-card">
      <template #header>
        <div class="card-header">
          <span>身份冲突解决</span>
          <div class="header-actions">
            <el-tag
              v-if="pendingCount > 0"
              type="danger"
              effect="dark"
            >
              待处理 {{ pendingCount }}
            </el-tag>
            <el-button
              type="primary"
              size="small"
              :loading="loading"
              @click="fetchData"
            >
              刷新
            </el-button>
          </div>
        </div>
      </template>

      <el-alert
        v-if="pendingCount > 0"
        type="warning"
        :closable="false"
        show-icon
        title="检测到身份冲突"
        description="同一身份标识命中了多个客户档案，需指定主档案并合并，避免同一自然人被重复营销。"
        style="margin-bottom: 16px"
      />

      <el-table
        v-loading="loading"
        :data="conflicts"
        border
        stripe
        empty-text="暂无身份冲突"
        style="width: 100%"
      >
        <el-table-column
          prop="identity_type"
          label="冲突类型"
          width="130"
        >
          <template #default="{ row }">
            <el-tag :type="typeTag(row.identity_type)">
              {{ channelLabel(row.identity_type) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="identity_value"
          label="标识值"
          min-width="160"
        />
        <el-table-column
          label="涉及客户"
          min-width="200"
        >
          <template #default="{ row }">
            <el-tag
              v-for="(cid, idx) in row.customer_ids"
              :key="cid"
              :type="idx === 0 ? 'success' : 'info'"
              size="small"
              style="margin-right: 6px"
            >
              {{ short(cid) }}
              <span v-if="idx === 0">（主）</span>
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="status"
          label="状态"
          width="110"
        >
          <template #default="{ row }">
            <el-tag :type="row.status === 'resolved' ? 'success' : 'danger'">
              {{ row.status === 'resolved' ? '已解决' : '待处理' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="created_at"
          label="发现时间"
          width="170"
        >
          <template #default="{ row }">
            {{ formatTime(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="240"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button
              v-if="row.status !== 'resolved'"
              type="danger"
              size="small"
              :loading="row._merging"
              @click="handleMerge(row)"
            >
              合并到主档案
            </el-button>
            <el-button
              v-if="row.status !== 'resolved'"
              type="info"
              size="small"
              :loading="row._ignoring"
              @click="handleIgnore(row)"
            >
              保留分离
            </el-button>
          </template>
        </el-table-column>
      </el-table>

      
      <el-dialog
        v-model="mergeDialog"
        title="确认合并客户"
        width="520px"
      >
        <div v-if="selected">
          <p>冲突标识：<b>{{ channelLabel(selected.identity_type) }}</b> = <b>{{ selected.identity_value }}</b></p>
          <p>将执行：把以下从属客户合并进主档案（主档案 OneID 不变，会话与事件迁移过去）</p>
          <el-table
            :data="mergeRows"
            border
            size="small"
          >
            <el-table-column
              prop="role"
              label="角色"
              width="90"
            />
            <el-table-column
              prop="id"
              label="客户 ID"
            />
          </el-table>
          <el-alert
            type="info"
            :closable="false"
            style="margin-top: 12px"
          >
            合并为不可逆操作，从属客户将被物理删除。
          </el-alert>
        </div>
        <template #footer>
          <el-button @click="mergeDialog = false">
            取消
          </el-button>
          <el-button
            type="danger"
            :loading="submitting"
            @click="confirmMerge"
          >
            确认合并
          </el-button>
        </template>
      </el-dialog>
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { listConflicts, mergeOneID, resolveConflict } from '@/api/oneid'
import { ElMessage, ElMessageBox } from 'element-plus'

const conflicts = ref([])
const loading = ref(false)
const mergeDialog = ref(false)
const submitting = ref(false)
const selected = ref(null)

const pendingCount = computed(() => conflicts.value.filter(c => c.status !== 'resolved').length)
const mergeRows = computed(() => {
  if (!selected.value) return []
  const ids = selected.value.customer_ids || []
  return ids.map((id, idx) => ({ role: idx === 0 ? '主档案' : '从属', id }))
})

function channelLabel(t) {
  const map = {
    phone: '手机号', email: '邮箱', wechat_open_id: '微信',
    douyin_open_id: '抖音', xiaohongshu_id: '小红书'
  }
  return map[t] || t
}
function typeTag() { return 'warning' }
function short(id) { return id ? id.slice(0, 8) : '-' }
function formatTime(t) {
  if (!t) return '-'
  const d = new Date(t)
  return isNaN(d) ? String(t) : d.toLocaleString()
}

async function fetchData() {
  loading.value = true
  try {
    const res = await listConflicts({ status: 'pending' })
    const data = res?.data?.data || res?.data || res || []
    conflicts.value = Array.isArray(data) ? data.map(c => ({ ...c, customer_ids: c.customer_ids || [] })) : []
  } catch (e) {
    ElMessage.error('加载冲突列表失败：' + (e?.message || e))
  } finally {
    loading.value = false
  }
}

function handleMerge(row) {
  selected.value = row
  mergeDialog.value = true
}

async function confirmMerge() {
  const row = selected.value
  if (!row || !row.customer_ids || row.customer_ids.length < 2) return
  const [primary, ...secondary] = row.customer_ids
  try {
    await ElMessageBox.confirm(
      `即将把以下客户合并为主客户 ${primary}：\n从属客户 ${secondary[0]} 将被永久删除，其会话与事件归属迁移至主客户。\n该操作不可撤销，确认继续？`,
      '合并不可逆确认',
      { type: 'warning', confirmButtonText: '确认合并', cancelButtonText: '取消' }
    )
  } catch {
    return
  }
  submitting.value = true
  row._merging = true
  try {
    await mergeOneID({ primary_id: primary, secondary_id: secondary[0] })
    await resolveConflict(row.conflict_id, { action: 'merge', primary_id: primary, secondary_id: secondary[0] })
    ElMessage.success('合并完成，冲突已解决')
    mergeDialog.value = false
    await fetchData()
  } catch (e) {
    ElMessage.error('合并失败：' + (e?.message || e))
  } finally {
    row._merging = false
    submitting.value = false
  }
}

async function handleIgnore(row) {
  try {
    await ElMessageBox.confirm('确认保留这些客户为分离状态（不合并）？', '保留分离', { type: 'warning' })
  } catch {
    return
  }
  row._ignoring = true
  try {
    await resolveConflict(row.conflict_id, { action: 'ignore' })
    ElMessage.success('已标记为保留分离')
    await fetchData()
  } catch (e) {
    ElMessage.error('操作失败：' + (e?.message || e))
  } finally {
    row._ignoring = false
  }
}

onMounted(fetchData)
</script>

<style scoped>
.conflicts-page { padding: 16px; }
.card-header { display: flex; justify-content: space-between; align-items: center; }
.header-actions { display: flex; align-items: center; gap: 12px; }
</style>
