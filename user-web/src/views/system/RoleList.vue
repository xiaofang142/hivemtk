<template>
  <div class="role-list-page">
    <el-card class="header-card">
      <div class="header-content">
        <div>
          <h2 class="page-title">角色管理（v3.1）</h2>
          <p class="page-subtitle">可视化创建自定义角色，支持菜单权限 + 按钮权限 + 数据范围</p>
        </div>
        <div class="header-actions">
          <el-tooltip content="v3.1 角色收口为三档系统角色，自定义角色将在后续版本开放" placement="top">
            <span>
              <el-button type="primary" disabled>
                <el-icon><Plus /></el-icon>
                新建自定义角色
              </el-button>
            </span>
          </el-tooltip>
        </div>
      </div>
    </el-card>

    <PageState
      v-if="error"
      state="error"
      :error-text="error"
      @retry="loadRoles"
    />

    <el-tabs v-else v-model="activeTab" @tab-change="onTabChange">
      
      <el-tab-pane label="系统角色" name="system">
        <el-row :gutter="20" v-loading="loading">
          <el-col
            v-for="role in systemRoles"
            :key="role.role_code"
            :span="8"
          >
            <el-card class="role-card system-role" shadow="hover">
              <div class="role-card-header">
                <div class="role-icon" :style="{ background: role.color }">
                  <el-icon :size="20"><Lock /></el-icon>
                </div>
                <div class="role-info">
                  <h3 class="role-name">{{ role.name }}</h3>
                  <el-tag size="small" type="info">系统内置</el-tag>
                </div>
                <el-tooltip content="系统角色不可修改" placement="top">
                  <el-icon class="lock-icon"><Lock /></el-icon>
                </el-tooltip>
              </div>
              <p class="role-desc">{{ role.description }}</p>
              <div class="role-meta">
                <span class="member-count">
                  <el-icon><User /></el-icon>
                  成员：<strong>{{ role.member_count || 0 }}</strong>
                </span>
                <el-button
                  type="primary"
                  link
                  :disabled="!role.member_count"
                  @click="openMembersDialog(role)"
                >
                  查看成员
                </el-button>
              </div>
            </el-card>
          </el-col>
        </el-row>
      </el-tab-pane>

      
      <el-tab-pane label="自定义角色" name="custom">
        <el-alert
          type="info"
          :closable="false"
          show-icon
          title="自定义角色暂未启用"
          description="当前版本（v3.1）角色收口为三档系统角色（超管 / 客服 / 员工）。自定义角色的菜单权限、按钮权限与数据范围配置能力将在后续版本开放；如需调整人员权限，请在「用户管理」中为账号分配系统角色。"
          style="margin-bottom: 16px"
        />
        <el-row :gutter="20" v-loading="loading">
          <el-col
            v-for="role in customRoles"
            :key="role.id"
            :span="8"
          >
            <el-card class="role-card" shadow="hover">
              <div class="role-card-header">
                <div class="role-icon" :style="{ background: role.color || '#409eff' }">
                  <el-icon :size="20"><UserFilled /></el-icon>
                </div>
                <div class="role-info">
                  <h3 class="role-name">{{ role.name }}</h3>
                  <el-tag size="small" :type="role.enabled ? 'success' : 'info'">
                    {{ role.enabled ? '已启用' : '已禁用' }}
                  </el-tag>
                </div>
              </div>
              <p class="role-desc">{{ role.description || '暂无描述' }}</p>
              <div class="role-meta-info">
                <el-row :gutter="8">
                  <el-col :span="8">
                    <div class="meta-item">
                      <span class="meta-label">菜单</span>
                      <span class="meta-value">{{ role.menu_count || 0 }}</span>
                    </div>
                  </el-col>
                  <el-col :span="8">
                    <div class="meta-item">
                      <span class="meta-label">按钮</span>
                      <span class="meta-value">{{ role.button_count || 0 }}</span>
                    </div>
                  </el-col>
                  <el-col :span="8">
                    <div class="meta-item">
                      <span class="meta-label">数据范围</span>
                      <el-tag size="small" type="info">{{ getScopeLabel(role.scope_type) }}</el-tag>
                    </div>
                  </el-col>
                </el-row>
              </div>
              <div class="role-meta">
                <span class="member-count">
                  <el-icon><User /></el-icon>
                  成员：<strong>{{ role.member_count || 0 }}</strong>
                </span>
              </div>
            </el-card>
          </el-col>
        </el-row>
      </el-tab-pane>
    </el-tabs>

    
    <el-dialog
      v-model="membersDialogVisible"
      :title="currentRole ? `${currentRole.name} - 成员列表` : ''"
      width="720px"
      :close-on-click-modal="false"
    >
      <el-table v-loading="membersLoading" :data="members" stripe border height="420">
        <el-table-column label="用户名" prop="username" min-width="120" show-overflow-tooltip />
        <el-table-column label="姓名" prop="real_name" min-width="120" show-overflow-tooltip />
        <el-table-column label="邮箱" prop="email" min-width="180" show-overflow-tooltip />
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="row.enabled ? 'success' : 'info'">
              {{ row.enabled ? '启用' : '禁用' }}
            </el-tag>
          </template>
        </el-table-column>
      </el-table>
    </el-dialog>

    

  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import {
  Lock,
  User,
  UserFilled,
  Plus
} from '@element-plus/icons-vue'
import {
  listSystemRoles,
  listCustomRoles,
  listRoleMembers
} from '@/api/role'
import PageState from '@/components/PageState.vue'

const activeTab = ref('system');
const loading = ref(false)
const error = ref('')
const systemRoles = ref([])
const customRoles = ref([])

const membersDialogVisible = ref(false);
const membersLoading = ref(false)
const currentRole = ref(null)
const members = ref([])

const getScopeLabel = (scope) => {
  const map = { all: '全部', dept: '部门', self: '自己', custom: '自定义' }
  return map[scope] || scope
};

const loadRoles = async () => {
  loading.value = true
  error.value = ''
  try {
    const [sysRes, customRes] = await Promise.all([
      listSystemRoles().catch(() => []),
      listCustomRoles().catch(() => [])
    ])
    systemRoles.value = Array.isArray(sysRes) ? sysRes : sysRes?.data || []
    customRoles.value = Array.isArray(customRes) ? customRes : customRes?.data || []
  } catch (e) {
    error.value = e?.message || '加载角色失败'
  } finally {
    loading.value = false
  }
};

const onTabChange = (tab) => {
  if (tab === 'system') loadSystemRoles()
  if (tab === 'custom') loadCustomRoles()
}

const loadSystemRoles = async () => {
  try {
    const res = await listSystemRoles().catch(() => [])
    systemRoles.value = Array.isArray(res) ? res : res?.data || []
  } catch {}
}

const loadCustomRoles = async () => {
  try {
    const res = await listCustomRoles().catch(() => [])
    customRoles.value = Array.isArray(res) ? res : res?.data || []
  } catch {}
}

const openMembersDialog = async (role) => {
  currentRole.value = role
  membersDialogVisible.value = true
  membersLoading.value = true
  try {
    const res = await listRoleMembers(role.role_code || role.code, {
      page: 1,
      size: 200
    }).catch(() => null)
    const data = res?.data || res
    members.value = data?.list || data || []
  } catch (e) {
    ElMessage.error('加载成员失败')
  } finally {
    membersLoading.value = false
  }
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
.header-actions {
  display: flex;
  gap: 8px;
}
.role-card {
  margin-bottom: 20px;
  min-height: 200px;
}
.role-card-header {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
}
.role-icon {
  width: 40px;
  height: 40px;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  flex-shrink: 0;
}
.role-info {
  flex: 1;
  min-width: 0;
}
.role-name {
  margin: 0 0 4px 0;
  font-size: 16px;
  font-weight: 600;
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
.role-meta-info {
  margin-bottom: 12px;
  padding: 8px 12px;
  background: var(--el-fill-color-light);
  border-radius: 4px;
}
.meta-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
  align-items: center;
}
.meta-label {
  font-size: 11px;
  color: var(--el-text-color-secondary);
}
.meta-value {
  font-size: 18px;
  font-weight: 600;
  color: var(--el-color-primary);
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
.role-actions {
  display: flex;
  gap: 8px;
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px solid var(--el-border-color-lighter);
}
.perm-card {
  width: 100%;
  max-height: 240px;
  overflow-y: auto;
}
.perm-card :deep(.el-card__body) {
  padding: 12px;
}
</style>
