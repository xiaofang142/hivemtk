<template>
  <div class="app-container">
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>我的团队</span>
        </div>
      </template>

      <el-alert
        type="info"
        :closable="false"
        show-icon
        title="团队管理员视角：只能查看与管理本团队（数据范围由服务端强制）。把成员设为团队管理员 = 授予本团队的管理权。"
        style="margin-bottom: 12px"
      />

      <el-descriptions v-if="team" :column="3" border size="small" style="margin-bottom: 16px">
        <el-descriptions-item label="团队名称">{{ team.name }}</el-descriptions-item>
        <el-descriptions-item label="计费档位">{{ team.plan }}</el-descriptions-item>
        <el-descriptions-item label="状态">
          <el-tag :type="team.status === 'active' ? 'success' : 'info'" size="small">
            {{ team.status === 'active' ? '启用' : '停用' }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="备注" :span="3">{{ team.remark || '—' }}</el-descriptions-item>
      </el-descriptions>

      <el-descriptions v-if="billing" :column="3" border size="small" style="margin-bottom: 16px" title="订阅与账单">
        <el-descriptions-item label="当前方案">{{ billing.subscription?.plan_code || '未订阅' }}</el-descriptions-item>
        <el-descriptions-item label="到期时间">{{ fmtTime(billing.subscription?.ends_at) }}</el-descriptions-item>
        <el-descriptions-item label="自动续费">{{ billing.subscription?.auto_renew ? '是' : '否' }}</el-descriptions-item>
        <el-descriptions-item label="最近账单" :span="3">
          <template v-if="(billing.records || []).length">
            <el-tag
              v-for="r in billing.records.slice(0, 5)"
              :key="r.id"
              size="small"
              :type="r.status === 'paid' ? 'success' : r.status === 'pending' ? 'warning' : 'info'"
              style="margin-right: 6px"
            >¥{{ (r.amount_cents / 100).toFixed(2) }}（{{ { pending: '应收', paid: '已收', void: '作废' }[r.status] }}）</el-tag>
          </template>
          <span v-else class="muted">暂无账单</span>
        </el-descriptions-item>
      </el-descriptions>

      <el-table v-loading="loading" :data="members" border size="small">
        <el-table-column prop="id" label="ID" width="60" />
        <el-table-column prop="username" label="用户名" min-width="120" />
        <el-table-column prop="real_name" label="姓名" min-width="100">
          <template #default="{ row }">{{ row.real_name || '—' }}</template>
        </el-table-column>
        <el-table-column label="角色" width="140">
          <template #default="{ row }">
            <el-tag v-if="row.role === 'team_admin'" type="warning" size="small">团队管理员</el-tag>
            <el-tag v-else size="small">成员</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="80">
          <template #default="{ row }">
            <el-tag :type="row.enabled && row.status === 1 ? 'success' : 'info'" size="small">
              {{ row.enabled && row.status === 1 ? '正常' : '禁用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="180" fixed="right">
          <template #default="{ row }">
            <el-button
              v-if="row.role === 'user'"
              size="small"
              type="warning"
              plain
              @click="setRole(row, 'team_admin')"
            >设为团队管理员</el-button>
            <el-button
              v-else-if="row.role === 'team_admin'"
              size="small"
              @click="setRole(row, 'user')"
            >撤为成员</el-button>
          </template>
        </el-table-column>
      </el-table>

      <el-pagination
        v-model:current-page="page"
        :page-size="size"
        :total="total"
        layout="total, prev, pager, next"
        small
        style="margin-top: 12px; justify-content: flex-end"
        @current-change="load"
      />
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { myTeamApi } from '@/api/teamAdmin'
import { http } from '@/utils/request'

const team = ref(null)
const members = ref([])
const loading = ref(false)
const total = ref(0)
const page = ref(1)
const size = 20

const billing = ref(null)
const fmtTime = (v) => (v ? String(v).replace('T', ' ').slice(0, 19) : '—')

const load = async () => {
  loading.value = true
  try {
    const res = await myTeamApi.detail()
    team.value = (res?.data ?? res ?? {}).team ?? null
  } catch (e) {
    // 未归属团队等场景：后端 400，页面留空提示
    team.value = null
  } finally {
    loading.value = false
  }
  // 订阅与账单（只读；无权/无团队静默）
  try {
    const res = await http.get('/api/team/billing')
    billing.value = res?.data ?? res ?? null
  } catch {
    billing.value = null
  }
  await loadMembers()
}

const loadMembers = async () => {
  const res = await myTeamApi.members({ page: page.value, size })
  const data = res?.data ?? res ?? {}
  members.value = data.list ?? []
  total.value = data.total ?? 0
}

const setRole = async (row, role) => {
  const label = role === 'team_admin' ? '设为团队管理员' : '撤为普通成员'
  await ElMessageBox.confirm(`确认将用户「${row.username}」${label}？`, '确认', { type: 'warning' })
  try {
    await myTeamApi.setMemberRole(row.id, role)
    ElMessage.success('已更新')
    await load()
  } catch (e) {
    ElMessage.error(e?.response?.data?.message || '操作失败')
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
.muted {
  color: #999;
  font-size: 12px;
}
</style>
