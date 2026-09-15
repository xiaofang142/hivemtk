<template>
  <div class="kb-list-page">
    
    <el-card class="header-card" shadow="never">
      <div class="header-content">
        <div>
          <h2>知识库管理</h2>
          <p class="subtitle">
            统一管理 RAG 文档库 / FAQ 知识库 / SOP 模板库，配置所属文档并查看被哪些智能体使用
          </p>
        </div>
        <div class="header-actions">
          <el-button @click="loadList(currentType)" :loading="loading">
            <el-icon><Refresh /></el-icon>
            {{ $t('刷新') }}
          </el-button>
          <el-button type="primary" @click="goCreate">
            <el-icon><Plus /></el-icon>
            新建知识库
          </el-button>
        </div>
      </div>
    </el-card>

    
    <el-row :gutter="20" class="stat-row" v-if="stats">
      <el-col :span="6">
        <el-card shadow="never" class="stat-card">
          <div class="stat-label">当前类型总数</div>
          <div class="stat-value">{{ stats.total ?? 0 }}</div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="never" class="stat-card">
          <div class="stat-label">已启用</div>
          <div class="stat-value text-success">{{ stats.enabled ?? 0 }}</div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="never" class="stat-card">
          <div class="stat-label">关联智能体</div>
          <div class="stat-value text-primary">{{ totalAgentBindings }}</div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="never" class="stat-card">
          <div class="stat-label">文档/条目数</div>
          <div class="stat-value text-info">{{ totalItems }}</div>
        </el-card>
      </el-col>
    </el-row>

    
    <el-card shadow="never" class="filter-card">
      <el-tabs v-model="currentType" @tab-change="onTabChange">
        <el-tab-pane label="RAG 文档库" name="rag">
          <span><el-icon><Document /></el-icon> RAG 文档库</span>
        </el-tab-pane>
        <el-tab-pane label="FAQ 知识库" name="faq">
          <span><el-icon><Notebook /></el-icon> FAQ 知识库</span>
        </el-tab-pane>
        <el-tab-pane label="SOP 模板库" name="sop">
          <span><el-icon><Tickets /></el-icon> SOP 模板库</span>
        </el-tab-pane>
      </el-tabs>

      <el-form :inline="true" :model="filter" @submit.prevent>
        <el-form-item :label="$t('关键词')">
          <el-input
            v-model="filter.keyword"
            :placeholder="searchPlaceholder"
            clearable
            style="width: 240px"
            @keyup.enter="onSearch"
            @clear="onSearch"
          />
        </el-form-item>
        <el-form-item :label="$t('状态')">
          <el-select v-model="filter.status" placeholder="全部状态" clearable style="width: 130px" @change="onSearch">
            <el-option label="启用" :value="1" />
            <el-option label="禁用" :value="0" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="onSearch">
            <el-icon><Search /></el-icon>
            {{ $t('搜索') }}
          </el-button>
          <el-button @click="resetFilter">{{ $t('重置') }}</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    
    <el-card shadow="never">
      <el-table :data="list" v-loading="loading" stripe border>
        <el-table-column prop="id" label="ID" width="70" align="center" />
        <el-table-column prop="kb_code" label="KB编码" width="170" show-overflow-tooltip />
        <el-table-column label="名称" min-width="180" show-overflow-tooltip>
          <template #default="{ row }">
            <span class="kb-name" role="button" tabindex="0" :class="{ 'kb-name--clickable': true }" @click="openDetail(row)" @keydown.enter.prevent="openDetail(row)" @keydown.space.prevent="openDetail(row)">
              {{ row.name }}
            </span>
          </template>
        </el-table-column>
        <el-table-column label="类型" width="120" align="center">
          <template #default="{ row }">
            <el-tag :type="getTypeTagType(row.kb_type)" size="small">{{ getTypeLabel(row.kb_type) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="条目数" width="100" align="center">
          <template #default="{ row }">
            <span>{{ row.item_count ?? row.doc_count ?? 0 }}</span>
          </template>
        </el-table-column>
        <el-table-column label="关联智能体" width="120" align="center">
          <template #default="{ row }">
            <el-button
              v-if="row.agent_count > 0"
              link
              type="primary"
              size="small"
              @click="openAgentBindings(row)"
            >
              {{ row.agent_count }} 个
            </el-button>
            <span v-else class="text-muted">0</span>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="90" align="center">
          <template #default="{ row }">
            <el-tag :type="row.status === 1 ? 'success' : 'info'" size="small">
              {{ row.status === 1 ? '启用' : '禁用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="描述" min-width="200" show-overflow-tooltip>
          <template #default="{ row }">{{ row.description || '-' }}</template>
        </el-table-column>
        <el-table-column label="创建时间" width="160">
          <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="240" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="openDetail(row)">详情</el-button>
            <el-button link type="primary" size="small" @click="goEdit(row)">编辑</el-button>
            <el-button
              link
              :type="row.status === 1 ? 'warning' : 'success'"
              size="small"
              @click="onToggle(row)"
            >
              {{ row.status === 1 ? '禁用' : '启用' }}
            </el-button>
            <el-button link type="danger" size="small" @click="onDelete(row)">删除</el-button>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty :description="emptyText">
            <el-button type="primary" @click="openCreateDialog">新建知识库</el-button>
          </el-empty>
        </template>
      </el-table>

      <div class="pagination-wrap" v-if="total > 0">
        <el-pagination
          v-model:current-page="page"
          v-model:page-size="pageSize"
          :page-sizes="[10, 20, 50, 100]"
          :total="total"
          layout="total, sizes, prev, pager, next, jumper"
          background
          @size-change="onSizeChange"
          @current-change="onPageChange"
        />
      </div>
    </el-card>

    
    <KBDrawer
      v-model="drawerVisible"
      :kb-data="currentKB"
      @updated="loadList(currentType)"
    />

    
    <el-dialog
      v-model="formVisible"
      :title="formData.id ? '编辑知识库' : '新建知识库'"
      width="520px"
      :close-on-click-modal="false"
    >
      <el-form :model="formData" label-width="90px" @submit.prevent>
        <el-form-item label="KB编码" required>
          <el-input
            v-model="formData.kb_code"
            placeholder="如 kb-sales-faq"
            :disabled="!!formData.id"
            maxlength="64"
          />
        </el-form-item>
        <el-form-item label="类型" required>
          <el-select v-model="formData.type" :disabled="!!formData.id" style="width: 100%">
            <el-option label="RAG 文档库" value="rag" />
            <el-option label="FAQ 知识库" value="faq" />
            <el-option label="SOP 模板库" value="sop" />
          </el-select>
        </el-form-item>
        <el-form-item label="名称" required>
          <el-input v-model="formData.name" placeholder="请输入知识库名称" maxlength="128" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input
            v-model="formData.description"
            type="textarea"
            :rows="3"
            placeholder="简要说明该知识库的用途（可选）"
            maxlength="500"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="formVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="onSubmitForm">
          {{ formData.id ? '保存' : '创建' }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  Refresh,
  Plus,
  Search,
  Document,
  Notebook,
  Tickets
} from '@element-plus/icons-vue'
import { listKBs, listByType, deleteKB, getKB, createKB, updateKB } from '@/api/knowledgeBase'
import { listByKB } from '@/api/agentKBBinding'
import KBDrawer from './KBDrawer.vue'

const router = useRouter()
const route = useRoute()

const currentType = ref('rag');
const loading = ref(false)
const list = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const stats = ref(null)
const totalAgentBindings = ref(0)
const totalItems = ref(0)

const filter = ref({
  keyword: '',
  status: null
})

const drawerVisible = ref(false)
const currentKB = ref(null)

const formVisible = ref(false);
const submitting = ref(false)
const formData = ref({ id: null, kb_code: '', type: 'rag', name: '', description: '', owner_type: 'shared' })

const openCreateDialog = () => {
  formData.value = { id: null, kb_code: '', type: currentType.value, name: '', description: '', owner_type: 'shared' }
  formVisible.value = true
}

const openEditDialog = (row) => {
  formData.value = {
    id: row.id,
    kb_code: row.kb_code || '',
    type: row.kb_type || row.type || currentType.value,
    name: row.name || '',
    description: row.description || ''
  }
  formVisible.value = true
}

const onSubmitForm = async () => {
  const f = formData.value
  if (!f.kb_code.trim()) return ElMessage.warning('请输入 KB 编码')
  if (!f.name.trim()) return ElMessage.warning('请输入知识库名称')
  submitting.value = true
  try {
    if (f.id) {
      await updateKB(f.id, { type: f.type, name: f.name.trim(), description: f.description })
      ElMessage.success('保存成功')
    } else {
      await createKB({
        kb_code: f.kb_code.trim(),
        type: f.type,
        name: f.name.trim(),
        description: f.description,
        owner_type: f.owner_type || 'shared'
      })
      ElMessage.success('创建成功')
    }
    formVisible.value = false
    loadList(currentType.value)
  } catch (e) {
    ElMessage.error((f.id ? '保存' : '创建') + '失败：' + (e?.message || '未知错误'))
  } finally {
    submitting.value = false
  }
}

const searchPlaceholder = computed(() => {
  if (currentType.value === 'rag') return '搜索名称/编码/描述'
  if (currentType.value === 'faq') return '搜索名称/编码/分类'
  return '搜索名称/编码/阶段'
})

const emptyText = computed(() => {
  if (loading.value) return '加载中...'
  return '暂无该类型知识库'
})

const getTypeLabel = (type) => {
  const map = { rag: 'RAG 文档', faq: 'FAQ', sop: 'SOP 模板' }
  return map[type] || (type || '-')
}

const getTypeTagType = (type) => {
  const map = { rag: 'primary', faq: 'success', sop: 'warning' }
  return map[type] || 'info'
}

const formatTime = (t) => {
  if (!t) return '-'
  try {
    const d = new Date(t)
    if (isNaN(d.getTime())) return String(t)
    const pad = (n) => String(n).padStart(2, '0')
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
  } catch {
    return String(t)
  }
}

const extractList = (r) => {
  if (!r) return []
  if (Array.isArray(r)) return r
  return r.list || r.items || r.data || []
}

const extractTotal = (r) => {
  if (!r) return 0
  if (typeof r.total === 'number') return r.total
  if (r.pagination?.total !== undefined) return r.pagination.total
  return 0
}

const loadList = async (type) => {
  loading.value = true
  try {
    const t = type || currentType.value
    const params = {
      page: page.value,
      page_size: pageSize.value,
      kb_type: t,
      keyword: filter.value.keyword || undefined,
      status: filter.value.status ?? undefined
    }
    const res = await listKBs(params).catch(() => null)
    let items = extractList(res)
    total.value = extractTotal(res)
    if (items.length === 0) {
      const fb = await listByType(t, params).catch(() => null);
      items = extractList(fb)
      total.value = total.value || extractTotal(fb)
    }
    const enriched = await Promise.all(
      items.map(async (it) => {
        let agent_count = it.agent_count ?? it.bind_agent_count ?? null
        if (agent_count == null) {
          try {
            const bindings = await listByKB(it.id).catch(() => null)
            agent_count = Array.isArray(bindings) ? bindings.length : extractList(bindings).length
          } catch {
            agent_count = 0
          }
        }
        return { ...it, agent_count }
      })
    );
    list.value = enriched
    stats.value = {
      total: total.value || enriched.length,
      enabled: enriched.filter((x) => x.status === 1).length
    };
    totalAgentBindings.value = enriched.reduce((s, x) => s + (x.agent_count || 0), 0)
    totalItems.value = enriched.reduce(
      (s, x) => s + (x.item_count ?? x.doc_count ?? 0),
      0
    )
  } catch (e) {
    list.value = []
    total.value = 0
    stats.value = null
  } finally {
    loading.value = false
  }
}

const onTabChange = (t) => {
  currentType.value = t
  page.value = 1
  loadList(t)
}

const onSearch = () => {
  page.value = 1
  loadList()
}

const resetFilter = () => {
  filter.value.keyword = ''
  filter.value.status = null
  page.value = 1
  loadList()
}

const onPageChange = (p) => {
  page.value = p
  loadList()
}

const onSizeChange = (s) => {
  pageSize.value = s
  page.value = 1
  loadList()
}

const goCreate = () => {
  openCreateDialog()
}

const goEdit = (row) => {
  openEditDialog(row)
}

const openDetail = async (row) => {
  try {
    const fresh = await getKB(row.id).catch(() => null)
    currentKB.value = fresh || row
  } catch {
    currentKB.value = row
  }
  drawerVisible.value = true
}

const openAgentBindings = (row) => {
  openDetail(row);
}

const onToggle = async (row) => {
  const next = row.status === 1 ? 0 : 1
  const action = next === 1 ? '启用' : '禁用'
  try {
    await ElMessageBox.confirm(
      `确认要${action}知识库「${row.name}」吗？`,
      `${action}确认`,
      { type: 'warning' }
    )
  } catch {
    return
  }
  try {
    const { updateKB } = await import('@/api/knowledgeBase')
    await updateKB(row.id, { status: next })
    ElMessage.success(`${action}成功`)
    loadList()
  } catch (e) {
    ElMessage.error(`${action}失败：` + (e?.message || '未知错误'))
  }
}

const onDelete = async (row) => {
  try {
    await ElMessageBox.confirm(
      `确认要删除知识库「${row.name}」吗？删除后关联的智能体挂载关系将一并解除。`,
      '删除确认',
      { type: 'warning' }
    )
  } catch {
    return
  }
  try {
    await deleteKB(row.id)
    ElMessage.success('删除成功')
    loadList()
  } catch (e) {
    ElMessage.error('删除失败：' + (e?.message || '未知错误'))
  }
}

onMounted(() => {
  loadList('rag')
  if (route.name === 'KnowledgeBaseCreate')
    openCreateDialog();
})
</script>

<style scoped>
.kb-list-page {
  padding: 0;
}
.header-card {
  margin-bottom: 16px;
}
.header-content {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.header-content h2 {
  margin: 0;
  font-size: 20px;
}
.subtitle {
  margin: 6px 0 0;
  color: #909399;
  font-size: 13px;
}
.header-actions {
  display: flex;
  gap: 8px;
}
.stat-row {
  margin-bottom: 16px;
}
.stat-card {
  text-align: center;
}
.stat-label {
  color: #909399;
  font-size: 13px;
}
.stat-value {
  font-size: 26px;
  font-weight: 600;
  margin-top: 4px;
  color: #303133;
}
.text-success {
  color: #67c23a;
}
.text-primary {
  color: #409eff;
}
.text-info {
  color: #909399;
}
.text-muted {
  color: #c0c4cc;
}
.filter-card {
  margin-bottom: 16px;
}
.kb-name--clickable {
  color: #409eff;
  cursor: pointer;
}
.kb-name--clickable:hover {
  text-decoration: underline;
}
.pagination-wrap {
  margin-top: 16px;
  display: flex;
  justify-content: flex-end;
}
:deep(.el-tabs__header) {
  margin-bottom: 16px;
}
</style>
