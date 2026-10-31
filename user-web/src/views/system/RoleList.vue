<template>
  <div class="role-list-page">
    <el-card class="header-card">
      <div class="header-content">
        <div>
          <h2 class="page-title">
            {{ t('role.title') }}
          </h2>
          <p class="page-subtitle">
            {{ t('role.subtitle') }}
          </p>
        </div>
        <el-tag
          type="info"
          size="large"
        >
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

    <el-row
      v-else
      v-loading="loading"
      :gutter="20"
    >
      <el-col
        v-for="role in roles"
        :key="role.code"
        :span="8"
      >
        <el-card
          class="role-card"
          shadow="hover"
        >
          <div class="role-card-header">
            <el-tag
              :type="role.tag_type || 'primary'"
              size="large"
            >
              {{ role.name }}
            </el-tag>
            <el-tooltip
              :content="t('role.cannotEditRole')"
              placement="top"
            >
              <el-icon class="lock-icon">
                <Lock />
              </el-icon>
            </el-tooltip>
          </div>
          <p class="role-desc">
            {{ role.description }}
          </p>
          <div class="role-meta">
            <span class="member-count">
              <el-icon><User /></el-icon>
              {{ t('role.memberCount') }}:
              <strong>{{ role.member_count }}</strong>
            </span>
            <span class="role-actions">
              <el-button
                type="primary"
                link
                :disabled="role.member_count === 0"
                @click="openMembersDialog(role)"
              >
                {{ t('role.viewMembers') }}
              </el-button>
              <el-button
                v-if="role.code !== 'admin'"
                type="primary"
                link
                @click="openMenuDialog(role)"
              >
                {{ t('role.assignMenus', '分配菜单权限') }}
              </el-button>
            </span>
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
        <el-table-column
          :label="t('role.status')"
          width="100"
        >
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
        style="margin-top: 12px; text-align: right"
        @size-change="onMembersSizeChange"
        @current-change="onMembersCurrentChange"
      />
    </el-dialog>

    <!-- 分配菜单权限对话框 -->
    <el-dialog
      v-model="menuDialogVisible"
      :title="menuDialogTitle"
      width="560px"
      :close-on-click-modal="false"
    >
      <el-alert
        v-if="menuRole && menuRole.code !== 'admin'"
        type="info"
        :closable="false"
        show-icon
        style="margin-bottom: 12px"
        :description="t('role.menuAssignHint', '勾选该角色可访问的菜单；超管始终拥有全部菜单，无需分配。')"
      />
      <el-input
        v-model="menuFilterText"
        :placeholder="t('role.filterMenus', '搜索菜单')"
        clearable
        style="margin-bottom: 12px"
      />
      <el-tree
        ref="menuTreeRef"
        :data="menuTree"
        :props="{ label: 'title', children: 'children' }"
        node-key="key"
        show-checkbox
        default-expand-all
        :filter-node-method="filterMenuNode"
        :check-strictly="false"
        style="max-height: 420px; overflow: auto"
      />
      <template #footer>
        <el-button @click="menuDialogVisible = false">
          {{ t('common.cancel', '取消') }}
        </el-button>
        <el-button
          type="primary"
          :loading="menuSaving"
          :disabled="!menuRole || menuRole.code === 'admin'"
          @click="saveRoleMenus"
        >
          {{ t('common.save', '保存') }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { Lock, User } from '@element-plus/icons-vue'
import { listRoles, listRoleMembers } from '@/api/role'
import { getAllMenus, getRoleMenuKeys, updateRoleMenuKeys } from '@/api/menu'
import PageState from '@/components/PageState.vue'
import { isUserReportedError } from '@/utils/errorReporting'

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

// 菜单权限弹窗
const menuDialogVisible = ref(false)
const menuRole = ref(null)
const menuTree = ref([])
const menuTreeRef = ref(null)
const menuSaving = ref(false)
const menuFilterText = ref('')

const menuDialogTitle = computed(() =>
  menuRole.value ? `${menuRole.value.name} - ${t('role.assignMenus', '分配菜单权限')}` : ''
)

watch(menuFilterText, (val) => {
  menuTreeRef.value?.filter(val)
})

function filterMenuNode(value, data) {
  if (!value) return true
  return (data.title || '').includes(value) || (data.key || '').includes(value)
}

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
    if (!isUserReportedError(e)) ElMessage.error(e?.message || t('common.operationFailed'))
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

// 打开菜单权限分配弹窗（超管不可分配）
async function openMenuDialog(role) {
  if (role.code === 'admin') {
    ElMessage.info(t('role.adminFullMenus', '超管拥有全部菜单权限，无需分配'))
    return
  }
  menuRole.value = role
  menuDialogVisible.value = true
  menuFilterText.value = ''
  try {
    const [treeRes, keysRes] = await Promise.all([getAllMenus(), getRoleMenuKeys(role.code)])
    menuTree.value = treeRes?.tree || treeRes?.data?.tree || []
    const checked = keysRes?.menu_keys || keysRes?.data?.menu_keys || []
    // 渲染完成后回填勾选状态
    setTimeout(() => {
      menuTreeRef.value?.setCheckedKeys(checked)
    }, 0)
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(e?.message || t('common.operationFailed'))
    menuDialogVisible.value = false
  }
}

// 保存：勾选 + 半选父节点一并提交（后端按 key 集合覆盖）
async function saveRoleMenus() {
  if (!menuRole.value) return
  const treeRef = menuTreeRef.value
  if (!treeRef) return
  const checked = treeRef.getCheckedKeys()
  const halfChecked = treeRef.getHalfCheckedKeys()
  const menuKeys = [...new Set([...checked, ...halfChecked])]
  menuSaving.value = true
  try {
    await updateRoleMenuKeys(menuRole.value.code, menuKeys)
    ElMessage.success(t('common.operationSuccess', '保存成功'))
    menuDialogVisible.value = false
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(e?.message || t('common.operationFailed'))
  } finally {
    menuSaving.value = false
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
.role-actions {
  display: flex;
  align-items: center;
  gap: 4px;
}
</style>
