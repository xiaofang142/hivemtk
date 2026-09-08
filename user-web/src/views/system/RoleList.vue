<template>
  <div class="role-list-page">
    <el-card class="header-card">
      <div class="header-content">
        <div>
          <h2 class="page-title">{{ t('role.title') }}</h2>
          <p class="page-subtitle">{{ t('role.subtitle') }}</p>
        </div>
        <el-tag type="info" size="large">
          <el-icon><Lock /></el-icon>
          {{ t('role.systemRole') }}
        </el-tag>
      </div>
    </el-card>

    <PageState
      v-if="error"
      state="error"
      :error-text="error"
      @retry="loadRoles"
    />

    <el-row v-else :gutter="20" v-loading="loading">
      <el-col
        v-for="role in roles"
        :key="role.code"
        :span="8"
      >
        <el-card class="role-card" shadow="hover">
          <div class="role-card-header">
            <el-tag :type="role.tag_type || 'primary'" size="large">
              {{ role.name }}
            </el-tag>
            <el-tooltip :content="t('role.cannotEditRole')" placement="top">
              <el-icon class="lock-icon"><Lock /></el-icon>
            </el-tooltip>
          </div>
          <p class="role-desc">{{ role.description }}</p>
          <div class="role-meta">
            <span class="member-count">
              <el-icon><User /></el-icon>
              {{ t('role.memberCount') }}:
              <strong>{{ role.member_count }}</strong>
            </span>
            <el-button
              type="primary"
              link
              :disabled="role.member_count === 0"
              @click="openMembersDialog(role)"
            >
              {{ t('role.viewMembers') }}
            </el-button>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <!-- 成员列表对话框 -->
    <el-dialog
      v-model="membersDialogVisible"
      :title="currentRole ? `${currentRole.name} - ${t('role.memberCount')}` : ''"
      width="720px"
      :close-on-click-modal="false"
    >
      <el-table
        v-loading="membersLoading"
        :data="members"
        stripe
        border
        height="420"
      >
        <el-table-column
          :label="t('role.username')"
          prop="username"
          min-width="120"
          show-overflow-tooltip
        />
        <el-table-column
          :label="t('role.realName')"
          prop="real_name"
          min-width="120"
          show-overflow-tooltip
        />
        <el-table-column
          :label="t('role.email')"
          prop="email"
          min-width="180"
          show-overflow-tooltip
        />
        <el-table-column :label="t('role.status')" width="100">
          <template #default="{ row }">
            <el-tag :type="row.enabled ? 'success' : 'info'">
              {{ row.enabled ? t('role.enabled') : t('role.disabled') }}
            </el-tag>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty :description="t('role.noMembers')" />
        </template>
      </el-table>
      <el-pagination
        v-if="membersPagination.total > 0"
        :current-page="membersPagination.page"
        :page-size="membersPagination.size"
        :page-sizes="[10, 20, 50]"
        :total="membersPagination.total"
        layout="total, sizes, prev, pager, next"
        @size-change="onMembersSizeChange"
        @current-change="onMembersCurrentChange"
        style="margin-top: 12px; text-align: right"
      />
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { Lock, User } from '@element-plus/icons-vue'
import { listRoles, listRoleMembers } from '@/api/role'
import PageState from '@/components/PageState.vue'

const { t } = useI18n()

// 角色列表
const roles = ref([])
const loading = ref(false)
const error = ref('')

// 成员弹窗
const membersDialogVisible = ref(false)
const membersLoading = ref(false)
const currentRole = ref(null)
const members = ref([])
const membersPagination = ref({ page: 1, size: 20, total: 0 })

// 加载角色列表
async function loadRoles() {
  loading.value = true
  error.value = ''
  try {
    const res = await listRoles()
    // 兼容后端返回 {code, data: [...]} 或直接是数组
    const list = Array.isArray(res) ? res : (res?.data || [])
    roles.value = list
  } catch (e) {
    error.value = e?.message || t('role.loadFailed')
  } finally {
    loading.value = false
  }
}

// 打开成员列表
async function openMembersDialog(role) {
  currentRole.value = role
  membersDialogVisible.value = true
  membersPagination.value = { page: 1, size: 20, total: 0 }
  await loadMembers()
}

// 加载成员列表
async function loadMembers() {
  if (!currentRole.value) return
  membersLoading.value = true
  try {
    const res = await listRoleMembers(currentRole.value.code, {
      page: membersPagination.value.page,
      size: membersPagination.value.size
    })
    // 后端 SuccessWithPage 返回 { list, total, page, page_size }
    const data = res?.data || res
    members.value = data?.list || []
    membersPagination.value = {
      page: data?.page || membersPagination.value.page,
      size: data?.page_size || membersPagination.value.size,
      total: data?.total || 0
    }
  } catch (e) {
    ElMessage.error(e?.message || t('common.operationFailed'))
  } finally {
    membersLoading.value = false
  }
}

function onMembersSizeChange(size) {
  membersPagination.value.size = size
  membersPagination.value.page = 1
  loadMembers()
}

function onMembersCurrentChange(page) {
  membersPagination.value.page = page
  loadMembers()
}

onMounted(loadRoles)
</script>

<style scoped>
.role-list-page {
  padding: 20px;
}
.header-card {
  margin-bottom: 20px;
}
.header-content {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
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
.role-card {
  margin-bottom: 20px;
  min-height: 180px;
}
.role-card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
}
.lock-icon {
  color: var(--el-text-color-placeholder);
  font-size: 16px;
  cursor: help;
}
.role-desc {
  color: var(--el-text-color-regular);
  font-size: 13px;
  line-height: 1.6;
  margin: 0 0 16px 0;
  min-height: 40px;
}
.role-meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding-top: 12px;
  border-top: 1px solid var(--el-border-color-lighter);
}
.member-count {
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--el-text-color-secondary);
  font-size: 13px;
}
.member-count strong {
  color: var(--el-color-primary);
  font-size: 16px;
  margin-left: 4px;
}
</style>
