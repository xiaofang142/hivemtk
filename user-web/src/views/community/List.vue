<template>
  <div class="community-container">
    <div class="page-header">
      <h2>{{ $t('社群管理') }}</h2>
      <div class="action-buttons">
        <el-button
          type="success"
          @click="handleExport"
        >
          <el-icon><Download /></el-icon>
          {{ $t('导出') }}
        </el-button>
        <el-button
          type="warning"
          @click="handleImport"
        >
          <el-icon><Upload /></el-icon>
          {{ $t('导入') }}
        </el-button>
        <el-button
          type="primary"
          @click="handleAdd"
        >
          <el-icon><Plus /></el-icon>
          {{ $t('新增分组') }}
        </el-button>
      </div>
      <input
        ref="importInputRef"
        type="file"
        accept="application/json,.json"
        class="hidden-file-input"
        @change="onImportFileChange"
      >
    </div>

    <div class="search-form">
      <el-form
        :inline="true"
        :model="searchForm"
        class="search-form-content"
      >
        <el-form-item :label="$t('分组名称')">
          <el-input
            v-model="searchForm.name"
            :placeholder="$t('请输入分组名称')"
            clearable
          />
        </el-form-item>
        <el-form-item :label="$t('状态')">
          <el-select
            v-model="searchForm.status"
            :placeholder="$t('请选择状态')"
            clearable
          >
            <el-option
              :label="$t('正常')"
              value="1"
            />
            <el-option
              :label="$t('禁用')"
              value="2"
            />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button
            type="primary"
            @click="handleSearch"
          >
            <el-icon><Search /></el-icon>
            {{ $t('搜索') }}
          </el-button>
          <el-button @click="resetSearch">
            <el-icon><RefreshRight /></el-icon>
            {{ $t('重置') }}
          </el-button>
        </el-form-item>
      </el-form>
    </div>

    
    <el-table
      v-loading="loading"
      :data="groupList"
      border
      style="width: 100%"
    >
      <el-table-column
        prop="id"
        label="ID"
        width="80"
      />
      <el-table-column
        prop="name"
        :label="$t('分组名称')"
        min-width="150"
        show-overflow-tooltip
      />
      <el-table-column
        prop="description"
        :label="$t('描述')"
        min-width="200"
        show-overflow-tooltip
      />
      <el-table-column
        prop="member_count"
        :label="$t('成员数')"
        width="100"
      />
      <el-table-column
        prop="message_count"
        :label="$t('消息数')"
        width="100"
      />
      <el-table-column
        prop="status"
        :label="$t('状态')"
        width="100"
      >
        <template #default="scope">
          <el-tag :type="scope.row.status === 1 ? 'success' : 'danger'">
            {{ scope.row.status === 1 ? '正常' : '禁用' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column
        prop="created_at"
        label="创建时间"
        width="180"
      />
      <el-table-column
        label="操作"
        width="340"
        fixed="right"
      >
        <template #default="scope">
          <el-button
            type="primary"
            size="small"
            @click="handleEdit(scope.row)"
          >
            <el-icon><Edit /></el-icon>
            编辑
          </el-button>
          <el-button
            type="info"
            size="small"
            @click="handleViewMembers(scope.row)"
          >
            <el-icon><User /></el-icon>
            成员
          </el-button>
          <el-button
            type="success"
            size="small"
            @click="handleViewMessages(scope.row)"
          >
            <el-icon><ChatDotSquare /></el-icon>
            消息
          </el-button>
          <el-button
            type="warning"
            size="small"
            @click="handleViewStats(scope.row)"
          >
            <el-icon><DataAnalysis /></el-icon>
            统计
          </el-button>
          <el-button
            type="danger"
            size="small"
            @click="handleDelete(scope.row)"
          >
            <el-icon><Delete /></el-icon>
            删除
          </el-button>
        </template>
      </el-table-column>
    </el-table>

    
    <div class="pagination-container">
      <el-pagination
        v-model:current-page="pagination.page"
        v-model:page-size="pagination.pageSize"
        :page-sizes="[10, 20, 50, 100]"
        layout="total, sizes, prev, pager, next, jumper"
        :total="pagination.total"
        @size-change="handleSizeChange"
        @current-change="handleCurrentChange"
      />
    </div>

    
    <el-dialog
      v-model="dialogVisible"
      :title="dialogTitle"
      width="600px"
      @close="handleDialogClose"
    >
      <el-form
        ref="formRef"
        :model="form"
        :rules="rules"
        label-width="100px"
      >
        <el-form-item
          label="分组名称"
          prop="name"
        >
          <el-input
            v-model="form.name"
            placeholder="请输入分组名称"
          />
        </el-form-item>
        <el-form-item
          label="描述"
          prop="description"
        >
          <el-input
            v-model="form.description"
            type="textarea"
            :rows="4"
            placeholder="请输入分组描述"
          />
        </el-form-item>
        <el-form-item
          label="状态"
          prop="status"
        >
          <el-radio-group v-model="form.status">
            <el-radio :label="1">
              正常
            </el-radio>
            <el-radio :label="2">
              禁用
            </el-radio>
          </el-radio-group>
        </el-form-item>
      </el-form>
      <template #footer>
        <span class="dialog-footer">
          <el-button @click="dialogVisible = false">取消</el-button>
          <el-button
            type="primary"
            :loading="submitting"
            @click="handleSubmit"
          >
            确定
          </el-button>
        </span>
      </template>
    </el-dialog>

    
    <el-dialog
      v-model="membersDialogVisible"
      title="社群成员"
      width="800px"
      top="5vh"
    >
      <el-table
        v-loading="membersLoading"
        :data="memberList"
        border
        style="width: 100%"
      >
        <el-table-column
          prop="id"
          label="ID"
          width="80"
        />
        <el-table-column
          prop="nickname"
          label="昵称"
          min-width="150"
        />
        <el-table-column
          prop="role"
          label="角色"
          width="120"
        />
        <el-table-column
          prop="join_time"
          label="加入时间"
          width="180"
        />
      </el-table>
      <div class="pagination-container">
        <el-pagination
          v-model:current-page="membersPagination.page"
          v-model:page-size="membersPagination.pageSize"
          :page-sizes="[10, 20, 50]"
          layout="total, prev, pager, next"
          :total="membersPagination.total"
          @current-change="handleMembersPageChange"
        />
      </div>
    </el-dialog>

    
    <el-dialog
      v-model="messagesDialogVisible"
      title="社群消息"
      width="800px"
      top="5vh"
    >
      <el-table
        v-loading="messagesLoading"
        :data="messageList"
        border
        style="width: 100%"
      >
        <el-table-column
          prop="id"
          label="ID"
          width="80"
        />
        <el-table-column
          prop="sender"
          label="发送者"
          width="150"
        />
        <el-table-column
          prop="content"
          label="内容"
          min-width="200"
          show-overflow-tooltip
        />
        <el-table-column
          prop="type"
          label="类型"
          width="100"
        />
        <el-table-column
          prop="created_at"
          label="时间"
          width="180"
        />
      </el-table>
      <div class="pagination-container">
        <el-pagination
          v-model:current-page="messagesPagination.page"
          v-model:page-size="messagesPagination.pageSize"
          :page-sizes="[10, 20, 50]"
          layout="total, prev, pager, next"
          :total="messagesPagination.total"
          @current-change="handleMessagesPageChange"
        />
      </div>
    </el-dialog>

    
    <el-dialog
      v-model="statsDialogVisible"
      title="社群统计"
      width="700px"
    >
      <div
        v-loading="statsLoading"
        class="stats-content"
      >
        <el-row :gutter="20">
          <el-col :span="8">
            <el-card class="stats-card">
              <div class="stats-value">
                {{ currentStats.member_count || 0 }}
              </div>
              <div class="stats-label">
                成员总数
              </div>
            </el-card>
          </el-col>
          <el-col :span="8">
            <el-card class="stats-card">
              <div class="stats-value">
                {{ currentStats.message_count || 0 }}
              </div>
              <div class="stats-label">
                消息总数
              </div>
            </el-card>
          </el-col>
          <el-col :span="8">
            <el-card class="stats-card">
              <div class="stats-value">
                {{ currentStats.active_count || 0 }}
              </div>
              <div class="stats-label">
                今日活跃
              </div>
            </el-card>
          </el-col>
        </el-row>
        <el-descriptions
          :column="2"
          border
          style="margin-top: 20px"
        >
          <el-descriptions-item label="分组名称">
            {{ currentGroup.name }}
          </el-descriptions-item>
          <el-descriptions-item label="分组ID">
            {{ currentGroup.id }}
          </el-descriptions-item>
          <el-descriptions-item label="创建时间">
            {{ currentGroup.created_at }}
          </el-descriptions-item>
          <el-descriptions-item label="状态">
            {{ currentGroup.status === 1 ? '正常' : '禁用' }}
          </el-descriptions-item>
        </el-descriptions>
      </div>
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  Plus, Search, RefreshRight, Edit, Delete, User, ChatDotSquare,
  DataAnalysis, Download, Upload
} from '@element-plus/icons-vue'
import { communityApi } from '@/api/community'

const loading = ref(false);
const submitting = ref(false)
const membersLoading = ref(false)
const messagesLoading = ref(false)
const statsLoading = ref(false)
const groupList = ref([])
const dialogVisible = ref(false)
const membersDialogVisible = ref(false)
const messagesDialogVisible = ref(false)
const statsDialogVisible = ref(false)
const isEdit = ref(false)
const formRef = ref(null)
const importInputRef = ref(null)
const currentGroup = ref({})
const memberList = ref([])
const messageList = ref([])
const currentStats = ref({})

const searchForm = reactive({
  name: '',
  status: ''
});

const pagination = reactive({
  page: 1,
  pageSize: 10,
  total: 0
});

const membersPagination = reactive({
  page: 1,
  pageSize: 10,
  total: 0
});

const messagesPagination = reactive({
  page: 1,
  pageSize: 10,
  total: 0
});

const form = reactive({
  id: null,
  name: '',
  description: '',
  status: 1
});

const rules = {
  name: [
    { required: true, message: i18n.global.t('请输入分组名称'), trigger: 'blur' },
    { min: 2, max: 50, message: i18n.global.t('长度在 2 到 50 个字符'), trigger: 'blur' }
  ]
};

const dialogTitle = computed(() => {
  return isEdit.value ? '编辑分组' : '新增分组'
});

onMounted(() => {
  fetchGroupList()
});

const fetchGroupList = async () => {
  loading.value = true
  try {
    const params = {
      page: pagination.page,
      page_size: pagination.pageSize,
      name: searchForm.name,
      status: searchForm.status ? parseInt(searchForm.status) : undefined
    }
    const res = await communityApi.getGroups(params)
    groupList.value = res.list || []
    pagination.total = res.total || 0
  } catch (error) {
    console.error(error)
  } finally {
    loading.value = false
  }
};

const handleSearch = () => {
  pagination.page = 1
  fetchGroupList()
}

const resetSearch = () => {
  searchForm.name = ''
  searchForm.status = ''
  pagination.page = 1
  fetchGroupList()
}

const handleSizeChange = (val) => {
  pagination.pageSize = val
  pagination.page = 1
  fetchGroupList()
}

const handleCurrentChange = (val) => {
  pagination.page = val
  fetchGroupList()
}

const handleAdd = () => {
  isEdit.value = false
  dialogVisible.value = true
  resetForm()
}

const handleEdit = (row) => {
  isEdit.value = true
  dialogVisible.value = true
  form.id = row.id
  form.name = row.name
  form.description = row.description
  form.status = row.status
}

const handleDelete = (row) => {
  ElMessageBox.confirm(
    `确定要删除分组 "${row.name}" 吗？`,
    '提示',
    {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      type: 'warning'
    }
  ).then(async () => {
    try {
      await communityApi.deleteGroup(row.id)
      ElMessage.success(i18n.global.t('删除成功'))
      fetchGroupList()
    } catch (error) {
      ElMessage.error(i18n.global.t('删除失败'))
      console.error(error)
    }
  }).catch((e) => {
    if (e !== 'cancel' && e !== 'close')
      throw e;
  })
}

const handleSubmit = async () => {
  if (!formRef.value) return

  await formRef.value.validate(async (valid) => {
    if (valid) {
      submitting.value = true
      try {
        if (isEdit.value) {
          await communityApi.updateGroup(form.id, form)
        } else {
          await communityApi.createGroup(form)
        }
        ElMessage.success(isEdit.value ? '更新成功' : '创建成功')
        dialogVisible.value = false
        fetchGroupList()
      } catch (error) {
        ElMessage.error(isEdit.value ? '更新失败' : '创建失败')
        console.error(error)
      } finally {
        submitting.value = false
      }
    }
  })
}

const handleDialogClose = () => {
  resetForm()
}

const resetForm = () => {
  form.id = null
  form.name = ''
  form.description = ''
  form.status = 1
  if (formRef.value) {
    formRef.value.resetFields()
  }
}

const handleViewMembers = (row) => {
  currentGroup.value = row
  membersDialogVisible.value = true
  membersPagination.page = 1
  fetchMembers()
};

const fetchMembers = async () => {
  membersLoading.value = true
  try {
    const res = await communityApi.getMembers({
      group_id: currentGroup.value.id,
      page: membersPagination.page,
      page_size: membersPagination.pageSize
    })
    memberList.value = res.list || []
    membersPagination.total = res.total || 0
  } catch (error) {
    console.error(error)
  } finally {
    membersLoading.value = false
  }
}

const handleMembersPageChange = (val) => {
  membersPagination.page = val
  fetchMembers()
}

const handleViewMessages = (row) => {
  currentGroup.value = row
  messagesDialogVisible.value = true
  messagesPagination.page = 1
  fetchMessages()
};

const fetchMessages = async () => {
  messagesLoading.value = true
  try {
    const res = await communityApi.getMessages({
      group_id: currentGroup.value.id,
      page: messagesPagination.page,
      page_size: messagesPagination.pageSize
    })
    messageList.value = res.list || []
    messagesPagination.total = res.total || 0
  } catch (error) {
    console.error(error)
  } finally {
    messagesLoading.value = false
  }
}

const handleMessagesPageChange = (val) => {
  messagesPagination.page = val
  fetchMessages()
}

const handleViewStats = async (row) => {
  currentGroup.value = row
  statsDialogVisible.value = true
  statsLoading.value = true
  try {
    const res = await communityApi.getStats({ group_id: row.id })
    currentStats.value = res || {}
  } catch (error) {
    console.error(error)
  } finally {
    statsLoading.value = false
  }
};

const handleImport = () => {
  if (importInputRef.value) {
    importInputRef.value.value = ''
    importInputRef.value.click()
  }
};

const onImportFileChange = async (event) => {
  const file = event?.target?.files?.[0]
  if (!file) return
  let payload = null
  try {
    const parsed = JSON.parse(await file.text())
    payload = Array.isArray(parsed) ? { groups: parsed } : parsed
  } catch (error) {
    ElMessage.error(i18n.global.t('导入文件不是合法的 JSON'))
    return
  }
  const groups = Array.isArray(payload?.groups) ? payload.groups : []
  if (groups.length === 0) {
    ElMessage.warning(i18n.global.t('导入文件没有分组数据'))
    return
  }
  try {
    const res = await communityApi.importData({ groups })
    const successCount = res?.success_count ?? groups.length
    ElMessage.success(i18n.global.t('导入完成：成功 {n} / 共 {t}', { n: successCount, t: groups.length }))
    fetchGroupList()
  } catch (error) {
    ElMessage.error(i18n.global.t('导入失败'))
    console.error(error)
  }
};

const handleExport = async () => {
  try {
    const res = await communityApi.exportData({})
    const groups = Array.isArray(res?.groups) ? res.groups : []
    if (groups.length === 0) {
      ElMessage.warning(i18n.global.t('暂无可导出的分组数据'))
      return
    }
    const blob = new Blob([JSON.stringify({ groups }, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    const stamp = new Date().toISOString().slice(0, 10)
    link.href = url
    link.download = `community-groups-${stamp}.json`
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    URL.revokeObjectURL(url)
    ElMessage.success(i18n.global.t('已导出 {n} 个分组', { n: groups.length }))
  } catch (error) {
    ElMessage.error(i18n.global.t('导出失败'))
    console.error(error)
  }
};
</script>

<style lang="scss" scoped>
.community-container {
  padding: 20px;

  .hidden-file-input {
    display: none;
  }

  .page-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 20px;

    h2 {
      margin: 0;
      font-size: 24px;
      color: #303133;
    }

    .action-buttons {
      display: flex;
      gap: 10px;
    }
  }

  .search-form {
    margin-bottom: 20px;
    padding: 15px;
    background-color: #f5f7fa;
    border-radius: 4px;

    .search-form-content {
      margin: 0;
    }
  }

  .pagination-container {
    margin-top: 20px;
    display: flex;
    justify-content: center;
  }

  .stats-content {
    .stats-card {
      text-align: center;
      padding: 10px 0;

      .stats-value {
        font-size: 24px;
        font-weight: bold;
        color: #4F46E5;
        margin-bottom: 5px;
      }

      .stats-label {
        font-size: 14px;
        color: #909399;
      }
    }
  }
}
</style>
