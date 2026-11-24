<template>
  <div class="ai-agent-list-page">
    <el-card
      class="header-card"
      shadow="never"
    >
      <div class="header-content">
        <div>
          <h2>AI 智能体管理</h2>
          <p class="subtitle">
            管理销售/客服/混合智能体，配置人设、知识库、SOP、话术与 LLM 参数
          </p>
        </div>
        <div class="header-actions">
          <el-button
            :loading="loading"
            @click="loadList"
          >
            <el-icon><Refresh /></el-icon>
            {{ $t('刷新') }}
          </el-button>
          <el-button
            v-if="permissionStore.hasGrantedMenuPath('/aiAgent/create')"
            type="primary"
            @click="goCreate"
          >
            <el-icon><Plus /></el-icon>
            {{ $t('新建智能体') }}
          </el-button>
        </div>
      </div>
    </el-card>

    
    <el-card
      shadow="never"
      class="filter-card"
    >
      <el-form
        :inline="true"
        :model="filter"
        @submit.prevent
      >
        <el-form-item :label="$t('智能体类型')">
          <el-select
            v-model="filter.type"
            :placeholder="$t('全部类型')"
            clearable
            style="width: 160px"
            @change="onSearch"
          >
            <el-option
              :label="$t('销售智能体')"
              value="sales"
            />
            <el-option
              :label="$t('客服智能体')"
              value="customer_service"
            />
            <el-option
              :label="$t('混合智能体')"
              value="hybrid"
            />
          </el-select>
        </el-form-item>
        <el-form-item :label="$t('状态')">
          <el-select
            v-model="filter.status"
            :placeholder="$t('全部状态')"
            clearable
            style="width: 140px"
            @change="onSearch"
          >
            <el-option
              :label="$t('启用')"
              :value="1"
            />
            <el-option
              :label="$t('禁用')"
              :value="0"
            />
          </el-select>
        </el-form-item>
        <el-form-item :label="$t('关键词')">
          <el-input
            v-model="filter.keyword"
            :placeholder="$t('搜索名称/编码')"
            clearable
            style="width: 220px"
            @keyup.enter="onSearch"
            @clear="onSearch"
          />
        </el-form-item>
        <el-form-item>
          <el-button
            type="primary"
            @click="onSearch"
          >
            <el-icon><Search /></el-icon>
            {{ $t('搜索') }}
          </el-button>
          <el-button @click="resetFilter">
            {{ $t('重置') }}
          </el-button>
        </el-form-item>
      </el-form>
    </el-card>

    
    <el-card shadow="never">
      <el-table
        v-loading="loading"
        :data="list"
        stripe
        border
      >
        <el-table-column
          prop="id"
          label="ID"
          width="70"
          align="center"
        />
        <el-table-column
          :label="$t('头像')"
          width="80"
          align="center"
        >
          <template #default="{ row }">
            <el-avatar
              v-if="row.avatar"
              :size="36"
              :src="row.avatar"
            >
              <el-icon><User /></el-icon>
            </el-avatar>
            <el-avatar
              v-else
              :size="36"
            >
              <el-icon><User /></el-icon>
            </el-avatar>
          </template>
        </el-table-column>
        <el-table-column
          prop="name"
          label="名称"
          min-width="140"
          show-overflow-tooltip
        />
        <el-table-column
          prop="agent_code"
          label="编码"
          width="160"
          show-overflow-tooltip
        />
        <el-table-column
          label="类型"
          width="120"
        >
          <template #default="{ row }">
            <el-tag
              :type="getTypeTagType(row.agent_type)"
              size="small"
            >
              {{ getTypeLabel(row.agent_type) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="状态"
          width="90"
          align="center"
        >
          <template #default="{ row }">
            <el-tag
              :type="row.status === 1 ? 'success' : 'info'"
              size="small"
            >
              {{ row.status === 1 ? '启用' : '禁用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="llm_model"
          label="LLM模型"
          width="140"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            {{ row.llm_model || '-' }}
          </template>
        </el-table-column>
        <el-table-column
          label="语言"
          width="170"
        >
          <template #default="{ row }">
            <div class="lang-cell">
              <span>内部：{{ getLanguageLabel(row.internal_language || 'zh') }}</span>
              <span>目标：{{ row.target_language ? getLanguageLabel(row.target_language) : '跟随内部' }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column
          label="人设摘要"
          min-width="200"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            <span class="persona-text">{{ truncateText(row.persona, 50) }}</span>
          </template>
        </el-table-column>
        <el-table-column
          label="创建时间"
          width="160"
        >
          <template #default="{ row }">
            {{ formatTime(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="280"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button
              v-if="canEdit(row)"
              link
              type="primary"
              size="small"
              @click="goEdit(row)"
            >
              编辑
            </el-button>
            <el-button
              link
              :type="row.status === 1 ? 'warning' : 'success'"
              size="small"
              @click="onToggle(row)"
            >
              {{ row.status === 1 ? '禁用' : '启用' }}
            </el-button>
            <el-button
              link
              type="primary"
              size="small"
              @click="openTestDialog(row)"
            >
              测试
            </el-button>
            <el-button
              link
              type="danger"
              size="small"
              @click="onDelete(row)"
            >
              删除
            </el-button>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty description="暂无智能体数据" />
        </template>
      </el-table>
    </el-card>

    
    <el-dialog
      v-model="testDialogVisible"
      title="测试智能体"
      width="760px"
      top="6vh"
    >
      <el-form
        :model="testForm"
        label-width="90px"
      >
        <el-form-item label="智能体">
          <el-input
            :model-value="currentAgentLabel"
            disabled
          />
        </el-form-item>
        <el-form-item label="客户ID">
          <el-input
            v-model="testForm.customer_id"
            placeholder="请输入客户ID（可选，留空使用默认上下文）"
          />
        </el-form-item>
        <el-form-item
          label="消息内容"
          required
        >
          <el-input
            v-model="testForm.message"
            type="textarea"
            :rows="3"
            placeholder="请输入要测试的客户消息"
          />
        </el-form-item>
        <el-form-item>
          <el-button
            type="primary"
            :loading="testing"
            @click="runTest"
          >
            <el-icon><VideoPlay /></el-icon>
            执行测试
          </el-button>
          <el-button @click="clearTestResult">
            清空结果
          </el-button>
        </el-form-item>
      </el-form>

      
      <template v-if="testResult">
        <el-divider content-position="left">
          测试结果
        </el-divider>
        <el-descriptions
          :column="2"
          border
          size="small"
        >
          <el-descriptions-item
            label="回复内容"
            :span="2"
          >
            <div class="reply-box">
              {{ testResult.reply || '-' }}
            </div>
          </el-descriptions-item>
          <el-descriptions-item label="LLM模型">
            {{ testResult.llm_model || '-' }}
          </el-descriptions-item>
          <el-descriptions-item label="耗时(ms)">
            {{ testResult.latency_ms || 0 }}
          </el-descriptions-item>
          <el-descriptions-item label="消耗Token">
            {{ testResult.cost_tokens || 0 }}
          </el-descriptions-item>
          <el-descriptions-item label="是否转人工">
            <el-tag
              :type="testResult.transferred_to_human ? 'warning' : 'success'"
              size="small"
            >
              {{ testResult.transferred_to_human ? '是' : '否' }}
            </el-tag>
            <span
              v-if="testResult.transfer_reason"
              class="transfer-reason"
            >（{{ testResult.transfer_reason }}）</span>
          </el-descriptions-item>
        </el-descriptions>

        
        <div class="chain-title">
          9步链路日志
        </div>
        <el-table
          :data="testResult.steps || []"
          stripe
          size="small"
          border
        >
          <el-table-column
            type="index"
            label="#"
            width="50"
            align="center"
          />
          <el-table-column
            prop="step"
            label="步骤"
            min-width="160"
            show-overflow-tooltip
          />
          <el-table-column
            label="状态"
            width="90"
            align="center"
          >
            <template #default="{ row }">
              <el-tag
                :type="getStepStatusType(row.status)"
                size="small"
              >
                {{ row.status || '-' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column
            label="耗时(ms)"
            width="100"
            align="center"
          >
            <template #default="{ row }">
              {{ row.latency_ms || 0 }}
            </template>
          </el-table-column>
          <el-table-column
            prop="detail"
            label="详情"
            min-width="200"
            show-overflow-tooltip
          />
          <el-table-column
            prop="error"
            label="错误"
            min-width="160"
            show-overflow-tooltip
          >
            <template #default="{ row }">
              <span
                v-if="row.error"
                class="error-text"
              >{{ row.error }}</span>
              <span v-else>-</span>
            </template>
          </el-table-column>
        </el-table>
      </template>
      <el-empty
        v-else-if="!testing"
        description="执行测试后显示结果"
        :image-size="80"
      />
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useListEditorRefresh } from '@/composables/useListEditor'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh, Plus, Search, User, VideoPlay } from '@element-plus/icons-vue'
import { listAgents, deleteAgent, toggleAgent, testAgent } from '@/api/aiAgent.js'
import { getLanguageLabel } from '@/constants/languages'
import { usePermissionStore } from '@/stores/permission'

const router = useRouter()
const permissionStore = usePermissionStore()

const currentAgentLabel = computed(() => {
  const agent = currentAgent.value
  if (!agent) return ''
  const name = agent.name || ''
  const code = agent.agent_code || ''
  return code ? `${name}（${code}）` : name
});

const loading = ref(false);
const list = ref([])
const filter = ref({ type: '', status: '', keyword: '' })

const testDialogVisible = ref(false);
const testing = ref(false)
const currentAgent = ref(null)
const testForm = ref({ customer_id: '', message: '' })
const testResult = ref(null)

const typeMap = {
  sales: '销售智能体',
  customer_service: '客服智能体',
  hybrid: '混合智能体',
  lead_mining: '线索发现智能体'
};

const getTypeLabel = (type) => typeMap[type] || type || '-'

const getTypeTagType = (type) => {
  if (type === 'sales') return 'success'
  if (type === 'customer_service') return 'warning'
  if (type === 'hybrid') return 'primary'
  return 'info'
}

const getStepStatusType = (status) => {
  if (status === 'ok') return 'success'
  if (status === 'fail') return 'danger'
  if (status === 'skip') return 'info'
  return 'info'
}

const truncateText = (text, len) => {
  if (!text) return '-'
  return text.length > len ? text.slice(0, len) + '...' : text
}

const formatTime = (t) => {
  if (!t) return '-'
  const d = new Date(t)
  if (isNaN(d.getTime())) return '-'
  const pad = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

const loadList = async () => {
  loading.value = true
  try {
    const params = {}
    if (filter.value.type) params.type = filter.value.type
    if (filter.value.status !== '' && filter.value.status !== null) params.status = filter.value.status
    if (filter.value.keyword) params.keyword = filter.value.keyword
    const res = await listAgents(params)
    list.value = res?.list || [];
  } catch (e) {
    ElMessage.error('加载智能体列表失败：' + (e.message || '未知错误'))
  } finally {
    loading.value = false
  }
};

const onSearch = () => {
  loadList()
}

useListEditorRefresh('/aiAgent/list', loadList)

const resetFilter = () => {
  filter.value = { type: '', status: '', keyword: '' }
  loadList()
}

const goCreate = () => {
  router.push('/aiAgent/create')
};

const editPath = (row) => `/aiAgent/edit/${row.id}`
const canEdit = (row) => permissionStore.hasGrantedMenuPath('/aiAgent/list') && permissionStore.canAccessPath(editPath(row))
const goEdit = (row) => router.push(editPath(row))

const onToggle = (row) => {
  const nextStatus = row.status === 1 ? 0 : 1
  const action = nextStatus === 1 ? '启用' : '禁用'
  ElMessageBox.confirm(`确认${action}智能体「${row.name}」吗？`, '操作确认', {
    type: 'warning',
    confirmButtonText: `确认${action}`,
    cancelButtonText: '取消'
  }).then(async () => {
    try {
      await toggleAgent(row.id, nextStatus)
      ElMessage.success(`${action}成功`)
      loadList()
    } catch (e) {
      ElMessage.error(`${action}失败：` + (e.message || '未知错误'))
    }
  }).catch(() => {})
};

const onDelete = (row) => {
  ElMessageBox.confirm(`确认删除智能体「${row.name}」吗？删除后不可恢复。`, '删除确认', {
    type: 'warning',
    confirmButtonText: '确认删除',
    cancelButtonText: '取消'
  }).then(async () => {
    try {
      await deleteAgent(row.id)
      ElMessage.success(i18n.global.t('删除成功'))
      loadList()
    } catch (e) {
      ElMessage.error('删除失败：' + (e.message || '未知错误'))
    }
  }).catch(() => {})
};

const openTestDialog = (row) => {
  currentAgent.value = row
  testForm.value = { customer_id: '', message: '' }
  testResult.value = null
  testDialogVisible.value = true
};

const runTest = async () => {
  if (!testForm.value.message || !testForm.value.message.trim()) {
    ElMessage.warning(i18n.global.t('请输入消息内容'))
    return
  }
  if (!currentAgent.value) return
  testing.value = true
  testResult.value = null
  try {
    const data = {
      customer_id: testForm.value.customer_id || '',
      message: testForm.value.message
    }
    const res = await testAgent(currentAgent.value.id, data)
    testResult.value = res
    ElMessage.success(i18n.global.t('测试执行完成'))
  } catch (e) {
    ElMessage.error('测试失败：' + (e.message || '未知错误'))
  } finally {
    testing.value = false
  }
}

const clearTestResult = () => {
  testResult.value = null
  testForm.value.message = ''
}

onMounted(() => {
  loadList()
});
</script>

<style scoped lang="scss">
.ai-agent-list-page { padding: 20px; }

.header-card {
  margin-bottom: 16px;
  :deep(.el-card__body) { padding: 16px 20px; }
  .header-content {
    display: flex;
    justify-content: space-between;
    align-items: center;
    h2 { margin: 0 0 6px 0; font-size: 20px; }
    .subtitle { color: #909399; margin: 0; font-size: 13px; }
    .header-actions { display: flex; gap: 8px; }
  }
}

.filter-card { margin-bottom: 16px; }

.persona-text { color: #606266; }

.lang-cell {
  display: flex;
  flex-direction: column;
  font-size: 12px;
  color: #606266;
  line-height: 1.6;
}

.reply-box {
  white-space: pre-wrap;
  word-break: break-word;
  background: #f5f7fa;
  padding: 10px 12px;
  border-radius: 4px;
  line-height: 1.6;
  max-height: 200px;
  overflow-y: auto;
}

.transfer-reason { color: #F59E0B; margin-left: 6px; }

.error-text { color: #EF4444; }

.chain-title {
  margin: 16px 0 8px 0;
  font-weight: 600;
  color: #303133;
  font-size: 14px;
}
</style>
