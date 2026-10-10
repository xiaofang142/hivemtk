<template>
  <div class="app-container">
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>团队管理（租户）</span>
          <el-button type="primary" size="small" @click="openCreate">新建团队</el-button>
        </div>
      </template>

      <el-alert
        type="info"
        :closable="false"
        show-icon
        title="超级管理员专属：全量管理所有团队。团队管理员（team_admin）只能在「我的团队」里管本团队成员；成员调拨后该用户旧令牌立即作废，需重新登录。"
        style="margin-bottom: 12px"
      />

      <el-table v-loading="loading" :data="rows" border size="small">
        <el-table-column prop="id" label="ID" width="60" />
        <el-table-column prop="name" label="团队名称" min-width="140" />
        <el-table-column label="计费档位" width="100">
          <template #default="{ row }">
            <el-tag size="small" :type="row.plan === 'enterprise' ? 'warning' : row.plan === 'pro' ? 'success' : 'info'">
              {{ row.plan }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag :type="row.status === 'active' ? 'success' : 'info'" size="small">
              {{ row.status === 'active' ? '启用' : '停用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="主管理员" width="100">
          <template #default="{ row }">
            <span v-if="row.owner_user_id">用户 #{{ row.owner_user_id }}</span>
            <span v-else class="muted">未指定</span>
          </template>
        </el-table-column>
        <el-table-column label="成员数" width="90">
          <template #default="{ row }">
            <el-tag size="small" type="info">{{ row.member_count ?? 0 }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="remark" label="备注" min-width="140" show-overflow-tooltip />
        <el-table-column prop="created_at" label="创建时间" width="160">
          <template #default="{ row }">{{ fmtTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="240" fixed="right">
          <template #default="{ row }">
            <el-button size="small" type="primary" plain @click="openMembers(row)">成员</el-button>
            <el-button size="small" @click="openEdit(row)">编辑</el-button>
            <el-button size="small" type="danger" @click="remove(row)">删除</el-button>
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

    <!-- 新建/编辑弹窗 -->
    <el-dialog v-model="dialogVisible" :title="editing ? '编辑团队' : '新建团队'" width="480px">
      <el-form label-width="100px">
        <el-form-item label="团队名称">
          <el-input v-model="form.name" maxlength="100" placeholder="如：华东销售一组" />
        </el-form-item>
        <el-form-item label="计费档位">
          <el-radio-group v-model="form.plan">
            <el-radio value="free">free</el-radio>
            <el-radio value="pro">pro</el-radio>
            <el-radio value="enterprise">enterprise</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item v-if="editing" label="状态">
          <el-radio-group v-model="form.status">
            <el-radio value="active">启用</el-radio>
            <el-radio value="disabled">停用</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="主管理员">
          <el-input v-model="form.owner_user_id" placeholder="system_users.id，可留空" style="width: 200px" />
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="form.remark" maxlength="200" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="doSave">保存</el-button>
      </template>
    </el-dialog>

    <!-- 成员抽屉 -->
    <el-drawer v-model="membersVisible" :title="`成员管理 — ${current.name || ''}`" size="680px">
      <div style="margin-bottom: 12px; display: flex; gap: 8px">
        <el-input v-model="moveUserId" placeholder="输入用户 ID 调入本团队" style="width: 220px" size="small" />
        <el-button type="primary" size="small" :loading="moving" @click="doMove">调入成员</el-button>
      </div>
      <el-table v-loading="membersLoading" :data="members" border size="small">
        <el-table-column prop="id" label="ID" width="60" />
        <el-table-column prop="username" label="用户名" min-width="120" />
        <el-table-column label="角色" width="140">
          <template #default="{ row }">
            <el-tag v-if="row.role === 'team_admin'" type="warning" size="small">团队管理员</el-tag>
            <el-tag v-else-if="row.role === 'admin'" type="danger" size="small">平台管理员</el-tag>
            <el-tag v-else size="small">{{ row.role }}</el-tag>
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
    </el-drawer>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { teamAdminApi } from '@/api/teamAdmin'

const rows = ref([])
const loading = ref(false)
const total = ref(0)
const page = ref(1)
const size = 20

const dialogVisible = ref(false)
const editing = ref(false)
const saving = ref(false)
const editingId = ref(0)
const form = ref({ name: '', plan: 'free', status: 'active', owner_user_id: '', remark: '' })

const membersVisible = ref(false)
const membersLoading = ref(false)
const members = ref([])
const current = ref({})
const moveUserId = ref('')
const moving = ref(false)

const fmtTime = (v) => (v ? String(v).replace('T', ' ').slice(0, 19) : '—')

const load = async () => {
  loading.value = true
  try {
    const res = await teamAdminApi.list({ page: page.value, size })
    const data = res?.data ?? res ?? {}
    rows.value = data.list ?? []
    total.value = data.total ?? 0
  } finally {
    loading.value = false
  }
}

const openCreate = () => {
  editing.value = false
  editingId.value = 0
  form.value = { name: '', plan: 'free', status: 'active', owner_user_id: '', remark: '' }
  dialogVisible.value = true
}

const openEdit = (row) => {
  editing.value = true
  editingId.value = row.id
  form.value = {
    name: row.name,
    plan: row.plan || 'free',
    status: row.status || 'active',
    owner_user_id: row.owner_user_id ? String(row.owner_user_id) : '',
    remark: row.remark || ''
  }
  dialogVisible.value = true
}

const doSave = async () => {
  saving.value = true
  try {
    const payload = {
      name: form.value.name,
      plan: form.value.plan,
      remark: form.value.remark
    }
    if (form.value.owner_user_id) payload.owner_user_id = Number(form.value.owner_user_id)
    if (editing.value) {
      payload.status = form.value.status
      await teamAdminApi.update(editingId.value, payload)
    } else {
      await teamAdminApi.create(payload)
    }
    ElMessage.success('已保存')
    dialogVisible.value = false
    await load()
  } catch (e) {
    ElMessage.error(e?.response?.data?.message || '保存失败')
  } finally {
    saving.value = false
  }
}

const remove = async (row) => {
  await ElMessageBox.confirm(
    `确认删除团队「${row.name}」？仅空团队可删除（有成员时会被拒绝）；默认团队不可删除。`,
    '删除确认',
    { type: 'warning' }
  )
  try {
    await teamAdminApi.remove(row.id)
    ElMessage.success('已删除')
    await load()
  } catch (e) {
    ElMessage.error(e?.response?.data?.message || '删除失败')
  }
}

const openMembers = (row) => {
  current.value = row
  membersVisible.value = true
  loadMembers()
}

const loadMembers = async () => {
  membersLoading.value = true
  try {
    const res = await teamAdminApi.members(current.value.id, { page: 1, size: 100 })
    const data = res?.data ?? res ?? {}
    members.value = data.list ?? []
  } finally {
    membersLoading.value = false
  }
}

const setRole = async (row, role) => {
  const label = role === 'team_admin' ? '设为团队管理员' : '撤为普通成员'
  await ElMessageBox.confirm(`确认将用户「${row.username}」${label}？`, '确认', { type: 'warning' })
  try {
    await teamAdminApi.setMemberRole(current.value.id, row.id, role)
    ElMessage.success('已更新')
    await loadMembers()
  } catch (e) {
    ElMessage.error(e?.response?.data?.message || '操作失败')
  }
}

const doMove = async () => {
  const uid = Number(moveUserId.value)
  if (!uid) {
    ElMessage.warning('请输入用户 ID')
    return
  }
  moving.value = true
  try {
    await teamAdminApi.moveMember(current.value.id, uid)
    ElMessage.success('已调入（该用户旧令牌已作废，需重新登录）')
    moveUserId.value = ''
    await loadMembers()
  } catch (e) {
    ElMessage.error(e?.response?.data?.message || '调入失败')
  } finally {
    moving.value = false
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
