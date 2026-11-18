<template>
  <div class="sop-template-list-page">
    <el-card
      class="header-card"
      shadow="never"
    >
      <div class="header-content">
        <div>
          <h2>SOP 模板库</h2>
          <p class="subtitle">
            Layer1 SOP 模板拼接：按 (意图, 阶段) 命中后使用 Go text/template 渲染回复，跳过 LLM
          </p>
        </div>
        <div class="header-actions">
          <el-button
            :loading="loading"
            @click="loadList"
          >
            <el-icon><Refresh /></el-icon>
            刷新
          </el-button>
          <el-button
            type="primary"
            @click="goCreate"
          >
            <el-icon><Plus /></el-icon>
            新建模板
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
        <el-form-item label="名称">
          <el-input
            v-model="filter.keyword"
            placeholder="搜索模板名称"
            clearable
            style="width: 200px"
            @keyup.enter="onSearch"
            @clear="onSearch"
          />
        </el-form-item>
        <el-form-item label="意图">
          <el-input
            v-model="filter.intent"
            placeholder="如: shipping, refund"
            clearable
            style="width: 160px"
            @keyup.enter="onSearch"
            @clear="onSearch"
          />
        </el-form-item>
        <el-form-item label="阶段">
          <el-select
            v-model="filter.stage"
            placeholder="全部阶段"
            clearable
            style="width: 160px"
            @change="onSearch"
          >
            <el-option
              v-for="s in stageOptions"
              :key="s"
              :label="s"
              :value="s"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="状态">
          <el-select
            v-model="filter.enabled"
            placeholder="全部"
            clearable
            style="width: 130px"
            @change="onSearch"
          >
            <el-option
              label="启用"
              :value="true"
            />
            <el-option
              label="禁用"
              :value="false"
            />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button
            type="primary"
            @click="onSearch"
          >
            <el-icon><Search /></el-icon>
            搜索
          </el-button>
          <el-button @click="resetFilter">
            重置
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
          label="模板名称"
          min-width="180"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            <div class="tpl-name">
              {{ row.name }}
            </div>
          </template>
        </el-table-column>
        <el-table-column
          label="意图"
          width="120"
          align="center"
        >
          <template #default="{ row }">
            <el-tag
              size="small"
              type="warning"
            >
              {{ row.intent || '-' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="阶段"
          width="100"
          align="center"
        >
          <template #default="{ row }">
            <el-tag
              size="small"
              type="info"
            >
              {{ row.stage || '-' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="所属知识库"
          width="180"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            <el-tag
              v-if="getKBName(row.kb_id)"
              size="small"
              type="warning"
              effect="plain"
            >
              {{ getKBName(row.kb_id) }}
            </el-tag>
            <span
              v-else
              class="text-muted"
            >未挂载</span>
          </template>
        </el-table-column>
        <el-table-column
          label="模板内容"
          min-width="240"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            <code class="tpl-code">{{ truncate(row.template, 90) }}</code>
          </template>
        </el-table-column>
        <el-table-column
          label="优先级"
          width="80"
          align="center"
        >
          <template #default="{ row }">
            <el-tag
              :type="row.priority >= 50 ? 'success' : row.priority >= 0 ? 'info' : 'warning'"
              size="small"
            >
              {{ row.priority || 0 }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="置信度"
          width="100"
          align="center"
        >
          <template #default="{ row }">
            <el-progress
              :percentage="Math.round((row.confidence || 0) * 100)"
              :stroke-width="10"
              :color="getConfColor(row.confidence)"
            />
          </template>
        </el-table-column>
        <el-table-column
          label="命中次数"
          width="100"
          align="center"
        >
          <template #default="{ row }">
            <el-tag
              size="small"
              :type="row.hit_count > 0 ? 'success' : 'info'"
            >
              {{ row.hit_count || 0 }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="状态"
          width="90"
          align="center"
        >
          <template #default="{ row }">
            <el-switch
              :model-value="row.enabled !== false"
              :loading="row._toggling"
              @change="(v) => onToggle(row, v)"
            />
          </template>
        </el-table-column>
        <el-table-column
          label="更新时间"
          width="160"
        >
          <template #default="{ row }">
            {{ formatTime(row.updated_at) }}
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="160"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button
              link
              type="primary"
              size="small"
              @click="goEdit(row)"
            >
              编辑
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
          <el-empty description="暂无 SOP 模板数据" />
        </template>
      </el-table>

      <div class="pagination-wrap">
        <el-pagination
          v-model:current-page="page"
          v-model:page-size="pageSize"
          :total="total"
          :page-sizes="[10, 20, 50, 100]"
          layout="total, sizes, prev, pager, next, jumper"
          @size-change="loadList"
          @current-change="loadList"
        />
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed as _computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh, Plus, Search } from '@element-plus/icons-vue'
import { sopTemplateApi } from '@/api/sopTemplate'
import { listKBs } from '@/api/knowledgeBase'

const router = useRouter()

const loading = ref(false);
const list = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const filter = ref({
  keyword: '',
  intent: '',
  stage: '',
  enabled: '',
  page: 1,
  page_size: 20
})

const stageOptions = ['initial', 'middle', 'late', 'objection', 'closing'];

const kbMap = ref({});
const loadKBMap = async () => {
  try {
    const res = await listKBs({ kb_type: 'sop', page: 1, page_size: 200 }).catch(() => null)
    const items = Array.isArray(res) ? res : res?.list || res?.items || []
    const map = {}
    items.forEach((it) => {
      map[it.id] = { name: it.name, kb_code: it.kb_code }
    })
    kbMap.value = map
  } catch {
    kbMap.value = {}
  }
}
const getKBName = (kbId) => {
  if (kbId == null || kbId === '' || kbId === 0) return ''
  const info = kbMap.value[kbId]
  if (!info) return ''
  return info.kb_code ? `[${info.kb_code}] ${info.name}` : info.name
}

const truncate = (text, len) => {
  if (!text) return '-'
  return text.length > len ? text.slice(0, len) + '...' : text
};

const formatTime = (t) => {
  if (!t) return '-'
  const d = new Date(t)
  if (isNaN(d.getTime())) return '-'
  const pad = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

const getConfColor = (c) => {
  if (c >= 0.8) return '#67c23a'
  if (c >= 0.6) return '#e6a23c'
  return '#f56c6c'
}

const loadList = async () => {
  loading.value = true
  try {
    const params = {
      page: page.value,
      page_size: pageSize.value
    }
    if (filter.value.keyword) params.keyword = filter.value.keyword
    if (filter.value.intent) params.intent = filter.value.intent
    if (filter.value.stage) params.stage = filter.value.stage
    if (filter.value.enabled !== '' && filter.value.enabled !== null) {
      params.enabled = filter.value.enabled
    }
    const res = await sopTemplateApi.list(params)
    list.value = res?.list || []
    total.value = res?.total || 0
  } catch (e) {
    ElMessage.error('加载 SOP 模板列表失败：' + (e.message || '未知错误'))
  } finally {
    loading.value = false
  }
};

const onSearch = () => {
  page.value = 1
  loadList()
}

const resetFilter = () => {
  filter.value = {
    keyword: '',
    intent: '',
    stage: '',
    enabled: '',
    page: 1,
    page_size: pageSize.value
  }
  page.value = 1
  loadList()
}

const goCreate = () => {
  router.push('/sop-template/editor')
};

const goEdit = (row) => {
  router.push(`/sop-template/editor/${row.id}`)
}

const onToggle = async (row, v) => {
  row._toggling = true
  try {
    // 专用 toggle 端点只收 enabled，避免全量 Update 的 agent_id required 校验
    await sopTemplateApi.toggle(row.id, v)
    ElMessage.success(v ? '已启用' : '已禁用')
    await loadList()
  } catch (e) {
    ElMessage.error('状态切换失败：' + (e.message || '未知错误'))
  } finally {
    row._toggling = false
  }
};

const onDelete = (row) => {
  ElMessageBox.confirm(`确认删除模板「${row.name}」吗？删除后不可恢复。`, '删除确认', {
    type: 'warning',
    confirmButtonText: '确认删除',
    cancelButtonText: '取消'
  }).then(async () => {
    try {
      await sopTemplateApi.remove(row.id)
      ElMessage.success('删除成功')
      await loadList()
    } catch (e) {
      ElMessage.error('删除失败：' + (e.message || '未知错误'))
    }
  }).catch(() => {})
};

onMounted(() => {
  loadKBMap()
  loadList()
})
</script>

<style scoped>
.sop-template-list-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.header-card :deep(.el-card__body) {
  padding: 18px 24px;
}

.header-content {
  display: flex;
  justify-content: space-between;
  align-items: flex-end;
}

.header-content h2 {
  margin: 0 0 4px 0;
  font-size: 20px;
  font-weight: 600;
}

.subtitle {
  margin: 0;
  color: var(--el-text-color-secondary);
  font-size: 13px;
}

.header-actions {
  display: flex;
  gap: 8px;
}

.filter-card :deep(.el-card__body) {
  padding: 16px 20px 0 20px;
}

.tpl-name {
  font-weight: 500;
  color: var(--el-text-color-regular);
}
.tpl-code {
  font-family: 'SF Mono', Monaco, Consolas, monospace;
  font-size: 12px;
  color: #606266;
  background: var(--el-fill-color-light);
  padding: 2px 6px;
  border-radius: 3px;
}
.text-muted { color: #c0c4cc; font-size: 12px; }

.pagination-wrap {
  margin-top: 16px;
  display: flex;
  justify-content: flex-end;
}
</style>
