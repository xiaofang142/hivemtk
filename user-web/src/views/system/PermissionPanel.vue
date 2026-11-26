<template>
  <div class="permission-panel-page">
    <el-card class="header-card">
      <div class="header-content">
        <div>
          <h2 class="page-title">
            {{ t('permission.title') }}
          </h2>
          <p class="page-subtitle">
            {{ t('permission.subtitle') }}
          </p>
        </div>
      </div>
    </el-card>

    
    <el-card class="section-card">
      <template #header>
        <span class="section-title">
          <el-icon><Operation /></el-icon>
          {{ t('permission.quickActions') }}
        </span>
      </template>
      <el-form
        ref="formRef"
        :model="form"
        :rules="formRules"
        label-width="140px"
        :inline="false"
      >
        <el-form-item
          :label="t('permission.targetUserId')"
          prop="userId"
        >
          <EntitySelect
            v-model="form.userId"
            entity="user"
            admin
            :placeholder="t('permission.targetUserId')"
            style="width: 240px"
          />
        </el-form-item>
        <el-form-item :label="t('permission.enableAccount')">
          <el-switch
            v-model="form.enabled"
            :active-text="t('permission.enableAccount')"
            :inactive-text="t('permission.disableAccount')"
            inline-prompt
            style="--el-switch-on-color: var(--el-color-success); --el-switch-off-color: var(--el-color-danger)"
          />
        </el-form-item>
        <el-form-item
          :label="t('permission.newPassword')"
          prop="password"
        >
          <el-input
            v-model="form.password"
            type="password"
            show-password
            style="width: 240px"
            :placeholder="t('permission.newPassword')"
            clearable
          />
        </el-form-item>
        <el-form-item
          :label="t('permission.confirmPassword')"
          prop="confirmPassword"
        >
          <el-input
            v-model="form.confirmPassword"
            type="password"
            show-password
            style="width: 240px"
            :placeholder="t('permission.confirmPassword')"
            clearable
          />
        </el-form-item>
        <el-form-item>
          <el-button
            type="primary"
            :loading="submitting.enabled"
            :icon="form.enabled ? 'CircleCheck' : 'CircleClose'"
            @click="handleToggleEnabled"
          >
            {{ form.enabled ? t('permission.enableAccount') : t('permission.disableAccount') }}
          </el-button>
          <el-button
            type="warning"
            :loading="submitting.password"
            icon="Key"
            style="margin-left: 8px"
            @click="handleResetPassword"
          >
            {{ t('permission.resetPassword') }}
          </el-button>
        </el-form-item>
      </el-form>
    </el-card>

    
    <el-card class="section-card">
      <template #header>
        <div class="card-header">
          <span class="section-title">
            <el-icon><Document /></el-icon>
            {{ t('permission.auditLogs') }}
          </span>
          <div class="filter-bar">
            <el-select
              v-model="filterAction"
              clearable
              :placeholder="t('permission.filterByAction')"
              style="width: 200px"
              @change="refreshLogs"
            >
              <el-option
                v-for="opt in actionOptions"
                :key="opt.value"
                :label="opt.label"
                :value="opt.value"
              />
            </el-select>
            <el-button
              type="primary"
              link
              :icon="Refresh"
              style="margin-left: 8px"
              @click="refreshLogs"
            >
              {{ t('common.refresh') }}
            </el-button>
          </div>
        </div>
      </template>

      <el-table
        v-loading="logsLoading"
        :data="logs"
        stripe
        border
      >
        <el-table-column
          :label="t('permission.actor')"
          width="150"
        >
          <template #default="{ row }">
            <EntityLabel
              entity="user"
              admin
              :value="row.user_id"
            />
          </template>
        </el-table-column>
        <el-table-column
          :label="t('common.username')"
          prop="username"
          width="160"
          show-overflow-tooltip
        />
        <el-table-column
          :label="t('permission.action')"
          width="180"
        >
          <template #default="{ row }">
            <el-tag
              :type="getActionTagType(row.action)"
              size="small"
            >
              {{ row.action }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          :label="t('permission.description')"
          prop="detail"
          min-width="280"
          show-overflow-tooltip
        />
        <el-table-column
          :label="t('permission.createdAt')"
          width="180"
        >
          <template #default="{ row }">
            {{ formatTime(row.created_at) }}
          </template>
        </el-table-column>
        <template #empty>
          <el-empty :description="t('common.noData')" />
        </template>
      </el-table>

      <el-pagination
        v-if="logsPagination.total > 0"
        :current-page="logsPagination.page"
        :page-size="logsPagination.size"
        :page-sizes="[10, 20, 50, 100]"
        :total="logsPagination.total"
        layout="total, sizes, prev, pager, next, jumper"
        style="margin-top: 16px; text-align: right"
        @size-change="onSizeChange"
        @current-change="onCurrentChange"
      />
    </el-card>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, markRaw as _markRaw } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  Operation,
  Document,
  Refresh,
  CircleCheck as _CircleCheck,
  CircleClose as _CircleClose,
  Key as _Key
} from '@element-plus/icons-vue'
import { setUserEnabled, resetUserPassword, listAuditLogs } from '@/api/permission'
import EntitySelect from '@/components/EntitySelect.vue'
import EntityLabel from '@/components/EntityLabel.vue'

const { t } = useI18n()

const formRef = ref(null);
const form = reactive({
  userId: null,
  enabled: true,
  password: '',
  confirmPassword: ''
})

const formRules = {
  userId: [
    { required: true, message: () => t('permission.targetUserId'), trigger: 'blur' },
    { type: 'number', min: 1, message: () => t('common.pleaseInput'), trigger: 'blur' }
  ]
}

const submitting = reactive({
  enabled: false,
  password: false
})

const filterAction = ref('');
const actionOptions = [
  { value: 'user.enable', label: 'user.enable' },
  { value: 'user.disable', label: 'user.disable' },
  { value: 'user.reset_password', label: 'user.reset_password' }
]
const logs = ref([])
const logsLoading = ref(false)
const logsPagination = ref({ page: 1, size: 20, total: 0 })

async function handleToggleEnabled() {
  if (!form.userId) {
    ElMessage.warning(t('permission.targetUserId'))
    return
  }
  try {
    await ElMessageBox.confirm(
      t('permission.confirmToggle', { enabled: form.enabled ? t('permission.enableAccount') : t('permission.disableAccount') }),
      t('common.hint'),
      { type: 'warning' }
    )
  } catch {
    return
  }
  submitting.enabled = true
  try {
    await setUserEnabled(form.userId, form.enabled)
    ElMessage.success(t('permission.operationSuccess'))
    refreshLogs()
  } catch (e) {
    ElMessage.error(e?.message || t('permission.operationFailed'))
  } finally {
    submitting.enabled = false
  }
}

async function handleResetPassword() {
  if (!form.userId) {
    ElMessage.warning(t('permission.targetUserId'))
    return
  }
  if (!form.password) {
    ElMessage.warning(t('permission.newPassword'))
    return
  }
  if (form.password !== form.confirmPassword) {
    ElMessage.warning(t('permission.passwordMismatch'))
    return
  }
  try {
    await ElMessageBox.confirm(
      t('permission.confirmResetPassword'),
      t('common.hint'),
      { type: 'warning' }
    )
  } catch {
    return
  }
  submitting.password = true
  try {
    await resetUserPassword(form.userId, form.password)
    ElMessage.success(t('permission.operationSuccess'))
    form.password = ''
    form.confirmPassword = ''
    refreshLogs()
  } catch (e) {
    ElMessage.error(e?.message || t('permission.operationFailed'))
  } finally {
    submitting.password = false
  }
}

async function loadLogs() {
  logsLoading.value = true
  try {
    const res = await listAuditLogs({
      action: filterAction.value || undefined,
      page: logsPagination.value.page,
      page_size: logsPagination.value.size
    })
    const data = res?.data || res
    logs.value = data?.list || []
    logsPagination.value = {
      page: data?.page || logsPagination.value.page,
      size: data?.page_size || logsPagination.value.size,
      total: data?.total || 0
    }
  } catch (e) {
    ElMessage.error(e?.message || t('common.operationFailed'))
  } finally {
    logsLoading.value = false
  }
}

function refreshLogs() {
  logsPagination.value.page = 1
  loadLogs()
}

function onSizeChange(size) {
  logsPagination.value.size = size
  logsPagination.value.page = 1
  loadLogs()
}

function onCurrentChange(page) {
  logsPagination.value.page = page
  loadLogs()
}

function getActionTagType(action) {
  switch (action) {
    case 'user.enable':
      return 'success'
    case 'user.disable':
      return 'danger'
    case 'user.reset_password':
      return 'warning'
    default:
      return 'info'
  }
}

function formatTime(t) {
  if (!t) return '-'
  try {
    return new Date(t).toLocaleString()
  } catch {
    return t
  }
}

onMounted(loadLogs)
</script>

<style scoped>
.permission-panel-page {
  padding: 20px;
}
.header-card {
  margin-bottom: 20px;
}
.header-content {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.page-title {
  margin: 0 0 4px 0;
  font-size: 20px;
  font-weight: 600;
}
.page-subtitle {
  margin: 0;
  color: var(--el-text-color-secondary);
  font-size: 13px;
}
.section-card {
  margin-bottom: 20px;
}
.section-title {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-weight: 600;
}
.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.filter-bar {
  display: flex;
  align-items: center;
}
</style>
