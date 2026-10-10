<template>
  <div class="system-user-page">
    <el-card class="header-card">
      <div>
        <h2>{{ t('systemUser.title') }}</h2>
        <p class="subtitle">{{ t('systemUser.headerSubtitle') }}</p>
      </div>
      <el-button type="primary" @click="showCreateDialog">
        <el-icon><Plus /></el-icon>
        {{ t('systemUser.create') }}
      </el-button>
    </el-card>

    <el-row :gutter="20" class="stats-row">
      <el-col :span="6">
        <el-card>
          <div class="stat-item">
            <div class="stat-label">{{ t('systemUser.totalAccounts') }}</div>
            <div class="stat-value">{{ stats.total }}</div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card>
          <div class="stat-item">
            <div class="stat-label">{{ t('systemUser.admins') }}</div>
            <div class="stat-value" style="color: #EF4444">{{ stats.admins }}</div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card>
          <div class="stat-item">
            <div class="stat-label">{{ t('systemUser.agents') }}</div>
            <div class="stat-value" style="color: #409EFF">{{ stats.agents }}</div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card>
          <div class="stat-item">
            <div class="stat-label">{{ t('systemUser.disabledCount') }}</div>
            <div class="stat-value" style="color: #909399">{{ stats.disabled }}</div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-card>
      <template #header>
        <div class="card-header">
          <span>{{ t('systemUser.list') }}</span>
          <div class="filter-area">
            <el-input
              v-model="searchKeyword"
              :placeholder="t('systemUser.searchPlaceholder')"
              clearable
              style="width: 240px"
              @input="applyFilter"
            />
            <el-select
              v-model="roleFilter"
              :placeholder="t('systemUser.filterByRole')"
              clearable
              style="width: 160px; margin-left: 12px"
              @change="applyFilter"
            >
              <el-option
                v-for="opt in filterRoleOptions"
                :key="opt.value"
                :label="getRoleLabel(opt.value)"
                :value="opt.value"
              />
            </el-select>
          </div>
        </div>
      </template>

      <PageState v-if="error" state="error" :error-text="error" @retry="refreshData" />

      <el-table
        v-if="!error"
        :data="filteredUsers"
        v-loading="loading"
        stripe
        border
      >
        <el-table-column :label="t('systemUser.username')" prop="username" min-width="120" />
        <el-table-column :label="t('systemUser.name')" prop="name" width="120" />
        <el-table-column :label="t('systemUser.email')" prop="email" min-width="180" />
        <el-table-column :label="t('systemUser.role')" prop="role" width="120">
          <template #default="{ row }">
            <el-tag :type="getRoleTagType(row.role)">{{ getRoleLabel(row.role) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('systemUser.phone')" prop="phone" min-width="120" />
        <el-table-column label="团队" min-width="120">
          <template #default="{ row }">
            <el-tag v-if="teamName(row.team_id)" size="small">{{ teamName(row.team_id) }}</el-tag>
            <el-tag v-else-if="row.role === 'admin'" size="small" type="danger">平台侧</el-tag>
            <span v-else class="muted">—</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('systemUser.enabled')" prop="status" width="100">
          <template #default="{ row }">
            <el-tag :type="getEnabledTagType(row.status)">{{ getEnabledLabel(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('systemUser.lastLogin')" prop="last_login_at" width="180">
          <template #default="{ row }">
            {{ formatTime(row.last_login_at) }}
          </template>
        </el-table-column>
        <el-table-column :label="t('systemUser.actions')" width="280" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="editUser(row)">{{ t('systemUser.edit') }}</el-button>
            <el-button link type="warning" @click="resetPassword(row)">{{ t('systemUser.resetPassword') }}</el-button>
            <el-button link type="danger" @click="deleteUser(row)">{{ t('systemUser.delete') }}</el-button>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty :description="t('systemUser.noData')" />
        </template>
      </el-table>

      <el-pagination
        v-if="!error && pagination.total > 0"
        v-model:current-page="pagination.page"
        v-model:page-size="pagination.size"
        :page-sizes="[10, 20, 50, 100]"
        :total="pagination.total"
        layout="total, sizes, prev, pager, next, jumper"
        @size-change="refreshData"
        @current-change="refreshData"
        style="margin-top: 16px; text-align: right"
      />
    </el-card>

    
    <el-dialog
      v-model="dialogVisible"
      :title="form.id ? t('systemUser.editDialogTitle') : t('systemUser.createDialogTitle')"
      width="600px"
      @close="resetForm"
    >
      <el-form :model="form" :rules="formRules" ref="formRef" label-width="100px">
        <el-form-item :label="t('systemUser.username')" prop="username">
          <el-input v-model="form.username" :disabled="!!form.id" />
        </el-form-item>
        <el-form-item :label="t('systemUser.name')" prop="name">
          <el-input v-model="form.name" />
        </el-form-item>
        <el-form-item :label="t('systemUser.email')" prop="email">
          <el-input v-model="form.email" />
        </el-form-item>
        <el-form-item :label="t('systemUser.role')" prop="role">
          <el-select v-model="form.role" style="width: 100%">
            <el-option
              v-for="opt in editableRoleOptions"
              :key="opt.value"
              :label="opt.label"
              :value="opt.value"
            />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('systemUser.phone')" prop="phone">
          <el-input v-model="form.phone" />
        </el-form-item>
        <el-form-item v-if="!form.id" :label="t('systemUser.initialPassword')" prop="password">
          <el-input
            v-model="form.password"
            type="password"
            show-password
            :placeholder="t('systemUser.initialPasswordPlaceholder')"
          />
        </el-form-item>
        <el-form-item v-if="form.id" :label="t('systemUser.enabled')">
          <el-radio-group v-model="form.status">
            <el-radio :value="1">{{ t('systemUser.statusEnabled') }}</el-radio>
            <el-radio :value="0">{{ t('systemUser.statusDisabled') }}</el-radio>
          </el-radio-group>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">{{ t('systemUser.cancel') }}</el-button>
        <el-button type="primary" :loading="submitting" @click="submitForm">{{ t('systemUser.confirm') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { teamAdminApi } from '@/api/teamAdmin'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import {
  listSystemUsers,
  createSystemUser,
  updateSystemUser,
  deleteSystemUser
} from '@/api/systemUser.js'
import { resetUserPassword as resetUserPasswordApi } from '@/api/permission.js'
import PageState from '@/components/PageState.vue'
import { getRoleLabel, getRoleTagType } from '@/constants/role'
import { getEnabledLabel, getEnabledTagType } from '@/constants/enabled'

const { t, locale } = useI18n()

const editableRoleOptions = ['admin', 'customer_service', 'staff'].map((v) => ({
  value: v,
  label: getRoleLabel(v)
}));
const filterRoleOptions = editableRoleOptions.filter((o) =>
  ['admin', 'customer_service', 'staff'].includes(o.value)
);

const loading = ref(false)
const submitting = ref(false)
const error = ref('')
const searchKeyword = ref('')
const roleFilter = ref('')
const users = ref([])
const stats = ref({ total: 0, admins: 0, agents: 0, disabled: 0 })

const pagination = reactive({ page: 1, size: 20, total: 0 })

const dialogVisible = ref(false)
const formRef = ref()
const form = ref({
  id: 0,
  username: '',
  name: '',
  email: '',
  role: 'customer_service',
  phone: '',
  password: '',
  status: 1
})

const formRules = computed(() => ({
  username: [
    { required: true, message: t('systemUser.usernamePlaceholder'), trigger: 'blur' },
    { min: 3, max: 50, message: t('systemUser.usernameLengthError'), trigger: 'blur' }
  ],
  name: [{ required: true, message: t('systemUser.namePlaceholder'), trigger: 'blur' }],
  email: [
    { required: true, type: 'email', message: t('systemUser.validEmail'), trigger: 'blur' }
  ],
  role: [{ required: true, message: t('systemUser.rolePlaceholder'), trigger: 'change' }],
  password: [
    { required: true, min: 8, max: 64, message: t('systemUser.passwordPlaceholder'), trigger: 'blur' }
  ]
}))

const filteredUsers = computed(() => users.value)

const applyFilter = () => {
  pagination.page = 1
  refreshData()
}

const formatTime = (val) => {
  if (!val) return '-'
  const d = typeof val === 'number' ? new Date(val * 1000) : new Date(val)
  if (isNaN(d.getTime())) return '-'
  return d.toLocaleString(locale.value, { hour12: false })
}

const refreshData = async () => {
  loading.value = true
  error.value = ''
  try {
    const params = {
      page: pagination.page,
      size: pagination.size
    }
    if (searchKeyword.value) params.keyword = searchKeyword.value
    if (roleFilter.value) params.role = roleFilter.value
    const res = await listSystemUsers(params)
    const list = res?.list || res?.data?.list || res?.items || []
    users.value = list
    pagination.total = res?.total ?? res?.data?.total ?? list.length
    stats.value = {
      total: pagination.total,
      admins: list.filter((u) => u.role === 'admin').length,
      agents: list.filter((u) => u.role === 'customer_service').length,
      disabled: list.filter((u) => Number(u.status) === 0).length
    };
  } catch (e) {
    error.value = t('systemUser.loadFailed')
    users.value = []
    stats.value = { total: 0, admins: 0, agents: 0, disabled: 0 }
    console.error(e)
  } finally {
    loading.value = false
  }
}

const showCreateDialog = () => {
  resetForm()
  dialogVisible.value = true
}

const editUser = (row) => {
  form.value = {
    id: row.id,
    username: row.username || '',
    name: row.name || '',
    email: row.email || '',
    role: row.role || 'customer_service',
    phone: row.phone || '',
    password: '',
    status: typeof row.status === 'number' ? row.status : 1
  }
  dialogVisible.value = true
}

const resetForm = () => {
  form.value = {
    id: 0,
    username: '',
    name: '',
    email: '',
    role: 'customer_service',
    phone: '',
    password: '',
    status: 1
  }
  if (formRef.value) formRef.value.clearValidate()
}

const submitForm = async () => {
  if (!formRef.value) return
  await formRef.value.validate(async (valid) => {
    if (!valid) return
    submitting.value = true
    try {
      if (form.value.id) {
        await updateSystemUser(form.value.id, form.value)
        ElMessage.success(t('systemUser.updateSuccess'))
      } else {
        await createSystemUser(form.value)
        ElMessage.success(t('systemUser.createSuccess'))
      }
      dialogVisible.value = false
      refreshData()
    } catch (e) {
      ElMessage.error(e?.message || t('systemUser.operationFailed'))
    } finally {
      submitting.value = false
    }
  })
}

const resetPassword = async (row) => {
  // R3-2 修复：原实现 PUT /users/:id {reset_password:true}——后端无此字段，
  // 密码从未被重置（假成功）。现走权限模块专用接口（带密码策略+审计+历史）。
  try {
    const { value } = await ElMessageBox.prompt(
      t('systemUser.resetConfirmMessage', [row.username]),
      t('systemUser.resetConfirmTitle'),
      {
        type: 'warning',
        inputType: 'password',
        inputPlaceholder: t('systemUser.resetPasswordPlaceholder') || '请输入新密码（至少 8 位，含大小写字母/数字）',
        inputPattern: /^.{8,}$/,
        inputErrorMessage: t('systemUser.resetPasswordPlaceholder') || '密码至少 8 位'
      }
    )
    await resetUserPasswordApi(row.id, value)
    ElMessage.success(t('systemUser.resetPasswordSuccess'))
  } catch (e) {
    if (e !== 'cancel' && e !== 'close') ElMessage.error(t('systemUser.resetFailed'))
  }
}

const deleteUser = async (row) => {
  try {
    await ElMessageBox.confirm(
      t('systemUser.deleteConfirmMessage', [row.username]),
      t('systemUser.deleteConfirmTitle'),
      { type: 'warning' }
    )
    await deleteSystemUser(row.id)
    ElMessage.success(t('systemUser.deleteSuccess2'))
    refreshData()
  } catch (e) {
    if (e !== 'cancel' && e !== 'close') ElMessage.error(t('systemUser.deleteFailed'))
  }
}

// 团队名映射（多租户 P1：用户列表显示归属团队；非管理员视角静默降级）
const teamMap = ref({})
const teamName = (id) => teamMap.value[id] || ''
const loadTeams = async () => {
  try {
    const res = await teamAdminApi.list({ page: 1, size: 200 })
    const list = (res?.data ?? res ?? {}).list ?? []
    const m = {}
    list.forEach((t) => { m[t.id] = t.name })
    teamMap.value = m
  } catch { /* 非管理员无权限：静默 */ }
}

onMounted(() => { refreshData(); loadTeams() })
</script>

<style scoped lang="scss">
.system-user-page { padding: 20px; }
.header-card {
  margin-bottom: 20px;
  :deep(.el-card__body) { display: flex; justify-content: space-between; align-items: center; }
  h2 { margin: 0 0 8px 0; }
  .subtitle { color: #909399; margin: 0; }
}
.stats-row {
  margin-bottom: 20px;
  .stat-item {
    text-align: center;
    .stat-label { color: #909399; font-size: 14px; margin-bottom: 10px; }
    .stat-value { font-size: 28px; font-weight: bold; }
  }
}
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  .filter-area { display: flex; align-items: center; }
}
</style>
