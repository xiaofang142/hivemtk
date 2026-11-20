<template>
  <div class="tool-management-page">
    <el-card
      class="header-card"
      shadow="never"
    >
      <div class="header-content">
        <div>
          <h2>AI 工具管理</h2>
          <p class="subtitle">
            {{ t('toolListPagination.summary', { total: stats.total }) }}
          </p>
        </div>
        <div class="header-actions">
          <el-button
            :loading="loading"
            @click="loadTools"
          >
            <el-icon><Refresh /></el-icon>
            刷新
          </el-button>
        </div>
      </div>
    </el-card>

    
    <el-row
      :gutter="16"
      class="stats-row"
    >
      <el-col :span="6">
        <el-card
          shadow="never"
          class="stat-card"
        >
          <div class="stat-value">
            {{ stats.total }}
          </div>
          <div class="stat-label">
            {{ t('toolListPagination.filteredTotal') }}
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card
          shadow="never"
          class="stat-card stat-success"
        >
          <div class="stat-value">
            {{ stats.enabled }}
          </div>
          <div class="stat-label">
            {{ t('toolListPagination.pageEnabled') }}
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card
          shadow="never"
          class="stat-card stat-warning"
        >
          <div class="stat-value">
            {{ stats.disabled }}
          </div>
          <div class="stat-label">
            {{ t('toolListPagination.pageDisabled') }}
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card
          shadow="never"
          class="stat-card stat-info"
        >
          <div class="stat-value">
            {{ stats.categories }}
          </div>
          <div class="stat-label">
            工具分类
          </div>
        </el-card>
      </el-col>
    </el-row>

    
    <el-card
      shadow="never"
      class="filter-card"
    >
      <el-form
        :inline="true"
        :model="filter"
      >
        <el-form-item label="分类">
          <el-select
            v-model="filter.category"
            placeholder="全部分类"
            clearable
            @change="applyFilter"
          >
            <el-option
              label="触达工具"
              value="reach"
            />
            <el-option
              label="客户工具"
              value="customer"
            />
            <el-option
              label="知识工具"
              value="knowledge"
            />
            <el-option
              label="业务工具"
              value="business"
            />
            <el-option
              label="私信工具"
              value="private_message"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="状态">
          <el-select
            v-model="filter.enabled"
            placeholder="全部状态"
            clearable
            @change="applyFilter"
          >
            <el-option
              label="已启用"
              :value="true"
            />
            <el-option
              label="已禁用"
              :value="false"
            />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button
            type="primary"
            @click="applyFilter"
          >
            搜索
          </el-button>
          <el-button @click="resetFilter">
            重置
          </el-button>
        </el-form-item>
      </el-form>
    </el-card>

    
    <el-card shadow="never">
      <el-alert
        v-if="loadError"
        :title="loadError"
        type="error"
        :closable="false"
        show-icon
      />
      <el-table
        v-loading="loading"
        :data="tools"
        :empty-text="loadError ? t('toolListPagination.retry') : t('toolListPagination.empty')"
        stripe
        border
      >
        <el-table-column
          label="工具"
          min-width="280"
        >
          <template #default="{ row }">
            <div class="tool-info">
              <div class="tool-name">
                {{ row.tool_name }}
              </div>
              <div class="tool-desc">
                {{ row.config?.description_zh || getCategoryLabel(row.category) }}
              </div>
            </div>
          </template>
        </el-table-column>
        <el-table-column
          label="分类"
          width="120"
        >
          <template #default="{ row }">
            <el-tag
              :type="getCategoryTagType(row.category)"
              size="small"
            >
              {{ getCategoryLabel(row.category) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="状态"
          width="100"
          align="center"
        >
          <template #default="{ row }">
            <el-switch
              v-model="row.is_enabled"
              :loading="row._toggling"
              @change="onToggleTool(row)"
            />
          </template>
        </el-table-column>
        <el-table-column
          label="绑定账号"
          min-width="200"
        >
          <template #default="{ row }">
            <div v-if="row.bound_accounts?.length">
              <el-tag
                v-for="(account, index) in row.bound_accounts"
                :key="index"
                type="success"
                size="small"
                class="account-tag"
              >
                {{ account.account_id }}
                <el-icon
                  v-if="account.is_primary"
                  class="primary-icon"
                >
                  <Star />
                </el-icon>
              </el-tag>
            </div>
            <div v-else-if="needsAccount(row.category)">
              <el-button
                link
                type="primary"
                size="small"
                @click="openBindDialog(row)"
              >
                绑定账号
              </el-button>
            </div>
            <span
              v-else
              class="text-muted"
            >-</span>
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="120"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button
              link
              type="primary"
              size="small"
              @click="openDetail(row)"
            >
              详情
            </el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-pagination
        class="tool-pagination"
        :current-page="pagination.page"
        :page-size="pagination.pageSize"
        :page-sizes="[20, 50, 100]"
        :total="stats.total"
        :disabled="loading"
        layout="total, sizes, prev, pager, next"
        @current-change="changePage"
        @size-change="changePageSize"
      />
    </el-card>

    
    <el-dialog
      v-model="detailVisible"
      title="工具详情"
      width="600px"
    >
      <el-descriptions
        v-if="currentTool"
        :column="2"
        border
      >
        <el-descriptions-item label="工具名称">
          {{ currentTool.tool_name }}
        </el-descriptions-item>
        <el-descriptions-item label="分类">
          <el-tag
            :type="getCategoryTagType(currentTool.category)"
            size="small"
          >
            {{ getCategoryLabel(currentTool.category) }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="状态">
          <el-tag
            :type="currentTool.is_enabled ? 'success' : 'info'"
            size="small"
          >
            {{ currentTool.is_enabled ? '已启用' : '已禁用' }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="显示顺序">
          {{ currentTool.display_order }}
        </el-descriptions-item>
        <el-descriptions-item
          label="配置"
          :span="2"
        >
          <pre class="config-json">{{ JSON.stringify(currentTool.config || {}, null, 2) }}</pre>
        </el-descriptions-item>
      </el-descriptions>
    </el-dialog>

    
    <el-dialog
      v-model="bindDialogVisible"
      :title="`绑定账号 - ${currentTool?.tool_name}`"
      width="600px"
    >
      <el-form
        :model="bindForm"
        label-width="100px"
      >
        <el-form-item label="账号类型">
          <el-select
            v-model="bindForm.account_type"
            placeholder="选择账号类型"
          >
            <el-option
              label="短信"
              value="sms"
            />
            <el-option
              label="邮件"
              value="email"
            />
            <el-option
              label="Telegram"
              value="telegram"
            />
            <el-option
              label="企业微信"
              value="wecom"
            />
            <el-option
              label="飞书"
              value="feishu"
            />
            <el-option
              label="WhatsApp"
              value="whatsapp"
            />
            <el-option
              label="抖音"
              value="douyin"
            />
            <el-option
              label="快手"
              value="kuaishou"
            />
            <el-option
              label="小红书"
              value="xhs"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="账号ID">
          <el-input
            v-model="bindForm.account_id"
            placeholder="输入账号ID"
          />
        </el-form-item>
        <el-form-item label="设为主账号">
          <el-switch v-model="bindForm.is_primary" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="bindDialogVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          @click="onBind"
        >
          确定
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { Refresh, Star } from '@element-plus/icons-vue'
import { listTools, updateToolStatus, bindToolAccount } from '@/api/aiTool.js'

const { t } = useI18n()
const loading = ref(false);
const loadError = ref('')
const pagination = reactive({ page: 1, pageSize: 20 })
let requestVersion = 0
const tools = ref([])
const filter = reactive({
  category: '',
  enabled: null
})
const stats = reactive({
  total: 0,
  enabled: computed(() => tools.value.filter(tool => tool.is_enabled).length),
  disabled: computed(() => tools.value.filter(tool => !tool.is_enabled).length),
  categories: 5
})

const detailVisible = ref(false);
const currentTool = ref(null)

const bindDialogVisible = ref(false);
const bindForm = reactive({
  account_type: '',
  account_id: '',
  is_primary: false
})

const categoryMap = {
  reach: '触达工具',
  customer: '客户工具',
  knowledge: '知识工具',
  business: '业务工具',
  private_message: '私信工具'
};

const getCategoryLabel = (category) => categoryMap[category] || category

const getCategoryTagType = (category) => {
  const map = {
    reach: 'primary',
    customer: 'success',
    knowledge: 'warning',
    business: 'info',
    private_message: 'danger'
  }
  return map[category] || 'info'
}

const needsAccount = (category) => {
  return category === 'reach' || category === 'private_message'
};

const loadTools = async () => {
  const version = ++requestVersion
  const params = { page: pagination.page, page_size: pagination.pageSize }
  if (filter.category) params.category = filter.category
  if (typeof filter.enabled === 'boolean') params.enabled = filter.enabled
  loading.value = true
  loadError.value = ''
  tools.value = []
  try {
    // 有界重查处理末页并发删除；同一请求始终使用同一份筛选快照。
    for (let attempt = 0; attempt < 3; attempt++) {
      const res = await listTools({ ...params })
      if (version !== requestVersion) return
      if (!Array.isArray(res?.list) || !Number.isSafeInteger(res.total) || res.total < 0) {
        throw new Error(t('toolListPagination.inconsistent'))
      }
      const lastPage = Math.max(1, Math.ceil(res.total / params.page_size))
      if (params.page > lastPage) {
        params.page = lastPage
        continue
      }
      if (res.total > 0 && res.list.length === 0) continue
      pagination.page = params.page
      tools.value = res.list
      stats.total = res.total
      return
    }
    throw new Error(t('toolListPagination.inconsistent'))
  } catch (e) {
    if (version !== requestVersion) return
    pagination.page = 1
    stats.total = 0
    loadError.value = t('toolListPagination.loadFailed', { message: e.message })
  } finally {
    if (version === requestVersion) loading.value = false
  }
};

const applyFilter = () => {
  pagination.page = 1
  return loadTools()
}
const changePage = (page) => {
  if (page === pagination.page) return
  pagination.page = page
  return loadTools()
}
const changePageSize = (size) => {
  pagination.pageSize = size
  return applyFilter()
}

const onToggleTool = async (tool) => {
  tool._toggling = true
  try {
    await updateToolStatus(tool.tool_name, tool.is_enabled)
    ElMessage.success(`${tool.is_enabled ? '启用' : '禁用'}成功`)
    await loadTools()
  } catch (e) {
    tool.is_enabled = !tool.is_enabled
    ElMessage.error('操作失败：' + e.message)
  } finally {
    tool._toggling = false
  }
};

const resetFilter = () => {
  filter.category = ''
  filter.enabled = null
  applyFilter()
};

const openDetail = (tool) => {
  currentTool.value = tool
  detailVisible.value = true
};

const openBindDialog = (tool) => {
  currentTool.value = tool
  bindForm.account_type = ''
  bindForm.account_id = ''
  bindForm.is_primary = false
  bindDialogVisible.value = true
};

const onBind = async () => {
  if (!bindForm.account_type || !bindForm.account_id) {
    ElMessage.warning('请选择账号类型并输入账号ID')
    return
  }

  try {
    await bindToolAccount(
      currentTool.value.tool_name,
      bindForm.account_type,
      bindForm.account_id,
      bindForm.is_primary
    )
    ElMessage.success('绑定成功')
    bindDialogVisible.value = false
    loadTools()
  } catch (e) {
    ElMessage.error('绑定失败：' + e.message)
  }
};

onMounted(() => {
  loadTools()
});
onBeforeUnmount(() => { requestVersion++ })
</script>

<style scoped lang="scss">
.tool-management-page {
  padding: 20px;
}

.header-card {
  margin-bottom: 16px;
  
  .header-content {
    display: flex;
    justify-content: space-between;
    align-items: center;
    
    h2 {
      margin: 0 0 6px 0;
      font-size: 20px;
    }
    
    .subtitle {
      color: #909399;
      margin: 0;
      font-size: 13px;
    }
  }
}

.stats-row {
  margin-bottom: 16px;
}

.stat-card {
  text-align: center;
  
  .stat-value {
    font-size: 28px;
    font-weight: bold;
    color: #303133;
  }
  
  .stat-label {
    font-size: 13px;
    color: #909399;
    margin-top: 4px;
  }
  
  &.stat-success .stat-value { color: #67C23A; }
  &.stat-warning .stat-value { color: #E6A23C; }
  &.stat-info .stat-value { color: #409EFF; }
}

.filter-card {
  margin-bottom: 16px;
}

.tool-pagination {
  margin-top: 16px;
  overflow-x: auto;
}

.tool-info {
  .tool-name {
    font-weight: 500;
    color: #303133;
  }
  
  .tool-desc {
    font-size: 12px;
    color: #909399;
    margin-top: 4px;
  }
}

.account-tag {
  margin-right: 4px;
  margin-bottom: 4px;
  
  .primary-icon {
    margin-left: 2px;
    color: #F7BA2A;
  }
}

.text-muted {
  color: #C0C4CC;
}

.config-json {
  background: #f5f7fa;
  padding: 10px;
  border-radius: 4px;
  font-size: 12px;
  max-height: 200px;
  overflow-y: auto;
}
</style>
