<template>
  <div class="workflow-orchestrator-page">
    <el-card class="header-card">
      <div class="header-left">
        <h2>{{ $t('工作流编排') }}</h2>
        <p class="subtitle">{{ $t('可视化编排业务工作流，支持触发器、条件分支和子流程') }}</p>
      </div>
      <div class="header-right">
        <el-button type="primary" @click="showCreateDialog">
          <el-icon><Plus /></el-icon>
          {{ $t('新建工作流') }}
        </el-button>
      </div>
    </el-card>

    <el-card>
      <div class="filter-bar">
        <el-input
          v-model="filterKeyword"
          :placeholder="$t('搜索工作流 ID 或名称')"
          style="width: 300px"
          clearable
          :prefix-icon="Search"
          @clear="onFilterChange"
          @keyup.enter="onFilterChange"
        />
        <el-select v-model="filterStatus" :placeholder="$t('状态')" clearable style="width: 140px" @change="onFilterChange">
          <el-option label="草稿" value="draft" />
          <el-option label="已发布" value="published" />
          <el-option label="已归档" value="archived" />
        </el-select>
        <el-button type="primary" :icon="Search" @click="onFilterChange">{{ $t('查询') }}</el-button>
      </div>

      <el-table :data="filteredFlows" v-loading="loading" stripe>
        <el-table-column prop="workflow_id" :label="$t('工作流 ID')" min-width="160" />
        <el-table-column prop="version" :label="$t('版本')" width="80" align="center">
          <template #default="{ row }">
            <el-tag size="small">{{ row.version }}v</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="name" :label="$t('名称')" min-width="180" />
        <el-table-column prop="status" :label="$t('状态')" width="110" align="center">
          <template #default="{ row }">
            <el-tag :type="statusType(row.status)" effect="dark">{{ statusText(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="updated_at" :label="$t('更新时间')" width="180">
          <template #default="{ row }">{{ formatTime(row.updated_at) }}</template>
        </el-table-column>
        <el-table-column :label="$t('操作')" width="380" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openEditor(row)">{{ $t('编辑') }}</el-button>
            <el-button
              v-if="row.status === 'draft'"
              link
              type="success"
              @click="handlePublish(row)"
              >{{ $t('发布') }}</el-button
            >
            <el-button
              v-if="row.status === 'published'"
              link
              type="warning"
              @click="handleArchive(row)"
              >{{ $t('归档') }}</el-button
            >
            <el-button link type="info" @click="handleExecute(row)">{{ $t('执行') }}</el-button>
            <el-button link type="danger" @click="handleDelete(row)">{{ $t('删除') }}</el-button>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty :description="$t('暂无工作流，点击右上角新建')" />
        </template>
      </el-table>

      <div class="pagination-bar">
        <el-pagination
          v-model:current-page="pagination.page"
          v-model:page-size="pagination.pageSize"
          :page-sizes="[10, 20, 50, 100]"
          layout="total, sizes, prev, pager, next, jumper"
          :total="pagination.total"
          @size-change="onSizeChange"
          @current-change="loadData"
        />
      </div>
    </el-card>

    
    <el-dialog v-model="dialogVisible" :title="$t('新建工作流')" width="600px" :close-on-click-modal="false">
      <el-form :model="form" :rules="formRules" ref="formRef" label-width="120px">
        <el-form-item label="工作流 ID" prop="workflow_id">
          <el-input v-model="form.workflow_id" :placeholder="$t('唯一标识，例如 order_approval')" />
        </el-form-item>
        <el-form-item :label="$t('名称')" prop="name">
          <el-input v-model="form.name" :placeholder="$t('工作流名称')" />
        </el-form-item>
        <el-form-item :label="$t('描述')">
          <el-input v-model="form.description" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">{{ $t('取消') }}</el-button>
        <el-button type="primary" @click="submitCreate">{{ $t('创建') }}</el-button>
      </template>
    </el-dialog>

    
    <el-dialog v-model="executeDialogVisible" :title="$t('执行工作流')" width="560px">
      <el-form :model="executeForm" label-width="120px">
        <el-form-item :label="$t('工作流 ID')">
          <el-input v-model="executeForm.workflow_id" disabled />
        </el-form-item>
        <el-form-item :label="$t('触发参数')">
          <el-input
            v-model="executeForm.trigger_payload"
            type="textarea"
            :rows="6"
            :placeholder="$t('JSON 格式，例如 {&quot;order_id&quot;: 123}')"
          />
        </el-form-item>
        <el-form-item v-if="lastExecution" :label="$t('最近状态')">
          <el-tag :type="execStatusType(lastExecution.status)" effect="dark">
            {{ execStatusText(lastExecution.status) }}
          </el-tag>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="executeDialogVisible = false">{{ $t('取消') }}</el-button>
        <el-button type="primary" :loading="executing" @click="doExecute">{{ $t('执行') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Search } from '@element-plus/icons-vue'
import { workflowOrchestratorApi } from '@/api/workflowOrchestrator.js'

const router = useRouter()
const loading = ref(false)
const filterKeyword = ref('')
const filterStatus = ref('')
const flows = ref([])
const lastExecution = ref(null)

const pagination = reactive({
  page: 1,
  pageSize: 20,
  total: 0
})

const filteredFlows = computed(() => {
  let list = flows.value;
  if (filterKeyword.value) {
    const kw = filterKeyword.value.toLowerCase()
    list = list.filter(f => (f.workflow_id || '').toLowerCase().includes(kw) || (f.name || '').toLowerCase().includes(kw))
  }
  return list
})

const dialogVisible = ref(false);
const formRef = ref()
const form = reactive({
  workflow_id: '',
  name: '',
  description: ''
})
const formRules = {
  workflow_id: [{ required: true, message: '请输入工作流 ID', trigger: 'blur' }],
  name: [{ required: true, message: '请输入名称', trigger: 'blur' }]
}

const showCreateDialog = () => {
  form.workflow_id = ''
  form.name = ''
  form.description = ''
  dialogVisible.value = true
}

const submitCreate = async () => {
  if (!formRef.value) return
  try {
    await formRef.value.validate()
    const definition = { nodes: [], edges: [] }
    await workflowOrchestratorApi.createVersion({
      workflow_id: form.workflow_id,
      name: form.name,
      description: form.description,
      definition
    })
    ElMessage.success('创建成功')
    dialogVisible.value = false
    loadData()
  } catch (e) {
    if (e !== false) ElMessage.error('创建失败: ' + (e?.message || ''))
  }
}

const executeDialogVisible = ref(false);
const executing = ref(false)
const executeForm = reactive({
  workflow_id: '',
  trigger_payload: '{}'
})

const handleExecute = async (row) => {
  executeForm.workflow_id = row.workflow_id
  executeForm.trigger_payload = '{}'
  lastExecution.value = null
  executeDialogVisible.value = true
}

const doExecute = async () => {
  executing.value = true
  try {
    let payload = {}
    try {
      payload = JSON.parse(executeForm.trigger_payload || '{}')
    } catch {
      ElMessage.error('触发参数必须是合法 JSON')
      executing.value = false
      return
    }
    const result = await workflowOrchestratorApi.execute({
      workflow_id: executeForm.workflow_id,
      trigger_payload: payload
    })
    const data = result?.data || result
    lastExecution.value = data
    ElMessage.success('执行已触发')
    if (data?.id) {
      router.push({ name: 'WorkflowOrchestratorExecution', params: { id: data.id } })
    }
    executeDialogVisible.value = false
  } catch (e) {
    ElMessage.error('执行失败: ' + (e?.message || ''))
  } finally {
    executing.value = false
  }
}

const openEditor = (row) => {
  router.push({ name: 'WorkflowOrchestratorEditor', params: { workflow_id: row.workflow_id } })
};

const handlePublish = async (row) => {
  try {
    await ElMessageBox.confirm(`确认发布版本 ${row.version}v？`, '发布确认', { type: 'warning' })
    await workflowOrchestratorApi.publishVersion(row.id)
    ElMessage.success('发布成功')
    loadData()
  } catch (_) { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
}

const handleArchive = async (row) => {
  try {
    await ElMessageBox.confirm(`确认归档版本 ${row.version}v？`, '归档确认', { type: 'warning' })
    await workflowOrchestratorApi.archiveVersion(row.id)
    ElMessage.success('已归档')
    loadData()
  } catch (_) { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
}

const handleDelete = async (row) => {
  try {
    await ElMessageBox.confirm(`确认删除版本 ${row.version}v？此操作不可恢复`, '删除确认', { type: 'error' })
    await workflowOrchestratorApi.deleteVersion(row.id)
    ElMessage.success('删除成功')
    loadData()
  } catch (_) { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
}

const loadData = async () => {
  loading.value = true
  try {
    const params = {
      workflow_id: '',
      status: filterStatus.value || '',
      page: pagination.page,
      page_size: pagination.pageSize
    };
    const result = await workflowOrchestratorApi.listVersions(params)
    const payload = result?.data || result || {}
    flows.value = payload.list || []
    pagination.total = payload.total || 0
  } catch (e) {
    flows.value = []
    pagination.total = 0
    ElMessage.warning('加载失败: ' + (e?.message || ''))
  } finally {
    loading.value = false
  }
};

const onFilterChange = () => {
  pagination.page = 1
  loadData()
};

const onSizeChange = () => {
  pagination.page = 1
  loadData()
};

const statusType = (s) => ({ draft: 'info', published: 'success', archived: 'warning' }[s] || '');
const statusText = (s) => ({ draft: '草稿', published: '已发布', archived: '已归档' }[s] || s)
const execStatusType = (s) => ({ running: 'primary', completed: 'success', failed: 'danger', terminated: 'warning' }[s] || '')
const execStatusText = (s) => ({ running: '运行中', completed: '已完成', failed: '失败', terminated: '已终止' }[s] || s)

const formatTime = (t) => {
  if (!t) return '-'
  const d = new Date(t)
  return d.toLocaleString('zh-CN', { hour12: false })
}

onMounted(loadData)
</script>

<style scoped>
.workflow-orchestrator-page {
  padding: 16px;
}
.header-card {
  margin-bottom: 16px;
}
.header-card .header-left h2 {
  margin: 0 0 4px 0;
}
.header-card .subtitle {
  margin: 0;
  color: var(--el-text-color-secondary);
  font-size: 13px;
}
.header-card :deep(.el-card__body) {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.filter-bar {
  margin-bottom: 16px;
  display: flex;
  gap: 8px;
  align-items: center;
}
.pagination-bar {
  margin-top: 16px;
  display: flex;
  justify-content: flex-end;
}
</style>
