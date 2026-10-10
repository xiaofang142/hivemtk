<template>
  <div class="knowledge-v2-management">
    
    <el-row :gutter="16" class="overview-row">
      <el-col :span="4">
        <el-card class="stat-card">
          <div class="stat-label">{{ $t('文档总数') }}</div>
          <div class="stat-value">{{ overview.total_documents || 0 }}</div>
        </el-card>
      </el-col>
      <el-col :span="4">
        <el-card class="stat-card">
          <div class="stat-label">{{ $t('分段总数') }}</div>
          <div class="stat-value">{{ overview.total_chunks || 0 }}</div>
        </el-card>
      </el-col>
      <el-col :span="4">
        <el-card class="stat-card">
          <div class="stat-label">总 Token</div>
          <div class="stat-value">{{ formatNumber(overview.total_tokens) }}</div>
        </el-card>
      </el-col>
      <el-col :span="4">
        <el-card class="stat-card">
          <div class="stat-label">{{ $t('今日导入') }}</div>
          <div class="stat-value">{{ overview.today_imports || 0 }}</div>
        </el-card>
      </el-col>
      <el-col :span="4">
        <el-card class="stat-card">
          <div class="stat-label">{{ $t('索引就绪率') }}</div>
          <div class="stat-value">{{ formatPercent(overview.index_health?.index_rate) }}</div>
        </el-card>
      </el-col>
      <el-col :span="4">
        <el-card class="stat-card">
          <div class="stat-label">{{ $t('检索命中率') }}</div>
          <div class="stat-value">{{ formatPercent(overview.hit_rate) }}</div>
        </el-card>
      </el-col>
    </el-row>

    
    <el-card class="tools-card" shadow="never">
      <template #header>
        <div class="card-header">
          <span>{{ $t('关联 AI 工具') }}</span>
          <el-button link type="primary" @click="goToToolManagement">{{ $t('管理工具') }} →</el-button>
        </div>
      </template>
      <el-row :gutter="16">
        <el-col :span="6" v-for="tool in knowledgeTools" :key="tool.tool_name">
          <el-card shadow="hover" class="tool-item" @click="goToToolDetail(tool)">
            <div class="tool-icon">
              <el-icon :size="24"><component :is="getToolIcon(tool.tool_name)" /></el-icon>
            </div>
            <div class="tool-info">
              <div class="tool-name">{{ tool.config?.description_zh || tool.tool_name }}</div>
              <div class="tool-status">
                <el-tag :type="tool.is_enabled ? 'success' : 'info'" size="small">
                  {{ tool.is_enabled ? '已启用' : '已禁用' }}
                </el-tag>
              </div>
            </div>
          </el-card>
        </el-col>
      </el-row>
    </el-card>

    
    <el-card class="toolbar-card">
      <div class="toolbar">
        <div class="toolbar-left">
          <el-select v-model="filter.product_id" :placeholder="$t('选择知识库')" clearable style="width: 200px" @change="handleSearch">
            <el-option v-for="p in productList" :key="p.id" :label="p.name" :value="p.id" />
          </el-select>
          <el-select v-model="filter.embed_status" :placeholder="$t('嵌入状态')" clearable style="width: 130px" @change="handleSearch">
            <el-option :label="$t('待处理')" value="pending" />
            <el-option :label="$t('处理中')" value="processing" />
            <el-option :label="$t('已索引')" value="indexed" />
            <el-option :label="$t('失败')" value="failed" />
          </el-select>
          <el-select v-model="filter.source_type" :placeholder="$t('来源类型')" clearable style="width: 130px" @change="handleSearch">
            <el-option :label="$t('文件上传')" value="upload" />
            <el-option :label="$t('文本')" value="text" />
            <el-option label="URL" value="url" />
            <el-option label="OpenAPI" value="openapi" />
          </el-select>
          <el-input v-model="filter.keyword" :placeholder="$t('搜索标题')" clearable style="width: 200px" @keyup.enter="handleSearch" @clear="handleSearch" />
          <el-button type="primary" @click="handleSearch">{{ $t('搜索') }}</el-button>
        </div>
        <div class="toolbar-right">
          <el-button type="primary" :icon="Upload" @click="showImportDialog = true">{{ $t('导入资料') }}</el-button>
          <el-button :icon="Refresh" @click="loadAll">{{ $t('刷新') }}</el-button>
        </div>
      </div>
    </el-card>

    
    <el-card>
      <el-table :data="documents" v-loading="loading" stripe>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="title" :label="$t('标题')" min-width="200" show-overflow-tooltip>
          <template #default="{ row }">
            <el-link type="primary" :underline="false" @click="showDocumentDetail(row)">{{ row.title }}</el-link>
          </template>
        </el-table-column>
        <el-table-column label="所属知识库" min-width="180" show-overflow-tooltip>
          <template #default="{ row }">
            <el-tag
              v-if="getKBName(row.product_id)"
              size="small"
              type="primary"
              effect="plain"
            >
              {{ getKBName(row.product_id) }}
            </el-tag>
            <span v-else class="text-muted">未挂载</span>
          </template>
        </el-table-column>
        <el-table-column label="来源" width="100">
          <template #default="{ row }">
            <el-tag :type="sourceTypeTag(row.source_type)">{{ sourceTypeLabel(row.source_type) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="分类" prop="category" width="120" />
        <el-table-column label="帮助中心" width="100">
          <template #default="{ row }">
            <el-switch :model-value="row.public_visible" size="small" @change="(v) => togglePublic(row, v)" />
          </template>
        </el-table-column>
        <el-table-column prop="file_size" label="大小" width="100">
          <template #default="{ row }">{{ formatFileSize(row.file_size) }}</template>
        </el-table-column>
        <el-table-column label="分段数" prop="chunk_count" width="80" />
        <el-table-column label="状态" width="180">
          <template #default="{ row }">
            <el-tag :type="getEmbedStatusTagType(row.embed_status)" size="small">
              {{ getEmbedStatusLabel(row.embed_status) }}
              <template v-if="row.embed_status === 'processing' && row.embed_progress != null">
                {{ row.embed_progress }}%
              </template>
            </el-tag>
            <el-progress v-if="row.embed_status === 'processing'" :percentage="row.embed_progress" :show-text="false" :stroke-width="4" style="margin-top: 4px" />
          </template>
        </el-table-column>
        <el-table-column prop="created_at" label="导入时间" width="170">
          <template #default="{ row }">{{ formatDate(row.created_at) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="220" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="showDocumentDetail(row)">详情</el-button>
            <el-button link type="primary" size="small" @click="handleReindex(row)">重建索引</el-button>
            <el-button link type="danger" size="small" @click="handleDelete(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-pagination
        v-model:current-page="pagination.page"
        v-model:page-size="pagination.pageSize"
        :total="pagination.total"
        :page-sizes="[10, 20, 50, 100]"
        layout="total, sizes, prev, pager, next, jumper"
        @size-change="loadDocuments"
        @current-change="loadDocuments"
        style="margin-top: 16px; justify-content: flex-end; display: flex"
      />
    </el-card>

    
    <el-dialog v-model="showImportDialog" title="导入资料数据" width="640px" :close-on-click-modal="false">
      <el-tabs v-model="importTab">
        
        <el-tab-pane label="文件上传" name="upload">
          <el-form label-width="80px">
            <el-form-item label="产品" required>
              <el-select v-model="importForm.product_id" placeholder="选择产品" style="width: 100%">
                <el-option v-for="p in productList" :key="p.id" :label="p.name" :value="p.id" />
              </el-select>
            </el-form-item>
            <el-form-item label="标题">
              <el-input v-model="importForm.title" placeholder="留空使用文件名" />
            </el-form-item>
            <el-form-item label="分类">
              <el-input v-model="importForm.category" placeholder="如:产品手册/常见问题" />
            </el-form-item>
            <el-form-item label="文件" required>
              <el-upload
                ref="uploadRef"
                :auto-upload="false"
                :limit="1"
                :on-change="handleFileChange"
                :on-exceed="handleExceed"
                accept=".pdf,.docx,.doc,.txt,.md,.html,.json,.csv"
                drag
              >
                <el-icon class="el-icon--upload"><upload-filled /></el-icon>
                <div class="el-upload__text">将文件拖到此处，或<em>点击上传</em></div>
                <template #tip>
                  <div class="el-upload__tip">支持 PDF / Word / TXT / Markdown / HTML / JSON / CSV,单个文件不超过 50MB</div>
                </template>
              </el-upload>
            </el-form-item>
          </el-form>
        </el-tab-pane>

        
        <el-tab-pane label="文本粘贴" name="text">
          <el-form label-width="80px">
            <el-form-item label="产品" required>
              <el-select v-model="importForm.product_id" placeholder="选择产品" style="width: 100%">
                <el-option v-for="p in productList" :key="p.id" :label="p.name" :value="p.id" />
              </el-select>
            </el-form-item>
            <el-form-item label="标题">
              <el-input v-model="importForm.title" placeholder="文档标题" />
            </el-form-item>
            <el-form-item label="内容" required>
              <el-input v-model="importForm.content" type="textarea" :rows="10" placeholder="请输入或粘贴文本内容" />
            </el-form-item>
          </el-form>
        </el-tab-pane>

        
        <el-tab-pane label="URL 抓取" name="url">
          <el-form label-width="80px">
            <el-form-item label="产品" required>
              <el-select v-model="importForm.product_id" placeholder="选择产品" style="width: 100%">
                <el-option v-for="p in productList" :key="p.id" :label="p.name" :value="p.id" />
              </el-select>
            </el-form-item>
            <el-form-item label="URL" required>
              <el-input v-model="importForm.url" placeholder="https://example.com/article" />
            </el-form-item>
            <el-form-item label="标题">
              <el-input v-model="importForm.title" placeholder="留空使用 URL 末段" />
            </el-form-item>
          </el-form>
        </el-tab-pane>
      </el-tabs>
      <template #footer>
        <el-button @click="showImportDialog = false">取消</el-button>
        <el-button type="primary" :loading="importing" @click="handleImport">开始导入</el-button>
      </template>
    </el-dialog>

    
    <el-dialog v-model="showDetailDialog" :title="`文档详情 - ${currentDoc?.title || ''}`" width="800px">
      <el-descriptions v-if="currentDoc" :column="2" border>
        <el-descriptions-item label="文档ID">{{ currentDoc.id }}</el-descriptions-item>
        <el-descriptions-item label="标题">{{ currentDoc.title }}</el-descriptions-item>
        <el-descriptions-item label="来源">{{ sourceTypeLabel(currentDoc.source_type) }}</el-descriptions-item>
        <el-descriptions-item label="分类">{{ currentDoc.category || '-' }}</el-descriptions-item>
        <el-descriptions-item label="文件类型">{{ currentDoc.file_type }}</el-descriptions-item>
        <el-descriptions-item label="文件大小">{{ formatFileSize(currentDoc.file_size) }}</el-descriptions-item>
        <el-descriptions-item label="分段数">{{ currentDoc.chunk_count }}</el-descriptions-item>
        <el-descriptions-item label="Tokens">{{ currentDoc.total_tokens }}</el-descriptions-item>
        <el-descriptions-item label="检索次数"><el-text type="info">未接入</el-text></el-descriptions-item>
        <el-descriptions-item label="命中次数"><el-text type="info">未接入</el-text></el-descriptions-item>
        <el-descriptions-item label="状态">
          <el-tag :type="embedStatusTag(currentDoc.embed_status)">{{ embedStatusLabel(currentDoc.embed_status) }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="导入时间">{{ formatDate(currentDoc.created_at) }}</el-descriptions-item>
        <el-descriptions-item label="最后索引" :span="2">{{ currentDoc.last_index_at ? formatDate(currentDoc.last_index_at) : '-' }}</el-descriptions-item>
        <el-descriptions-item v-if="currentDoc.error_msg" label="错误信息" :span="2">
          <el-text type="danger">{{ currentDoc.error_msg }}</el-text>
        </el-descriptions-item>
      </el-descriptions>
      <el-divider content-position="left">分段预览(前20个)</el-divider>
      <el-table :data="chunks" max-height="300">
        <el-table-column prop="chunk_index" label="序号" width="80" />
        <el-table-column prop="content" label="内容" show-overflow-tooltip>
          <template #default="{ row }">
            <div class="chunk-preview">{{ truncateText(row.content, 200) }}</div>
          </template>
        </el-table-column>
        <el-table-column prop="token_count" label="Tokens" width="100" />
        <el-table-column prop="char_count" label="字符数" width="100" />
      </el-table>
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, reactive, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Upload, Refresh, UploadFilled, Search, Document, ChatDotRound, Plus, List } from '@element-plus/icons-vue'
import { knowledgeAPI } from '@/api/knowledge'
import { http } from '@/utils/request'
import { ragProductConfigAPI } from '@/api/ragProductConfig'
import { listTools } from '@/api/aiTool'
import { EMBED_STATUS, getStatusLabel, getStatusTagType } from '@/constants/status';
import { getSourceLabel, getSourceTagType } from '@/constants/source'

const router = useRouter()

const loading = ref(false)
const importing = ref(false)
const showImportDialog = ref(false)
const showDetailDialog = ref(false)
const importTab = ref('upload')

const overview = ref({})
const documents = ref([])
const productList = ref([])
const knowledgeTools = ref([])
const chunks = ref([])
const currentDoc = ref(null)

const filter = reactive({
  product_id: '',
  embed_status: '',
  source_type: '',
  keyword: ''
})

const pagination = reactive({ page: 1, pageSize: 20, total: 0 })

const importForm = reactive({
  product_id: '',
  title: '',
  category: '',
  content: '',
  url: '',
  file: null
})

let pollTimer = null

const loadAll = async () => {
  await Promise.all([loadOverview(), loadProducts(), loadDocuments(), loadKnowledgeTools()])
}

const loadOverview = async () => {
  try {
    const res = await knowledgeAPI.getOverviewStats({ product_id: filter.product_id })
    if (res) overview.value = res
  } catch (e) {
    console.error('加载总览失败:', e)
  }
}

const loadProducts = async () => {
  try {
    const res = await ragProductConfigAPI.listProducts()
    if (Array.isArray(res)) {
      productList.value = res
    } else if (Array.isArray(res?.items)) {
      productList.value = res.items
    }
  } catch (e) {
    console.error('加载产品列表失败:', e)
  }
}

const getKBName = (productId) => {
  if (productId == null || productId === '' || productId === 0) return ''
  const p = productList.value.find((x) => String(x.id) === String(productId))
  if (!p) return ''
  return p.kb_code ? `[${p.kb_code}] ${p.name}` : p.name
};

const loadKnowledgeTools = async () => {
  try {
    const res = await listTools({ category: 'knowledge' })
    knowledgeTools.value = res?.list || []
  } catch (e) {
    console.error('加载知识库工具失败:', e)
  }
}

const getToolIcon = (toolName) => {
  const iconMap = {
    'rag.search': 'Search',
    'knowledge.feedback': 'ChatDotRound',
    'knowledge.add_doc': 'Plus',
    'knowledge.list_kb': 'List'
  }
  return iconMap[toolName] || 'Document'
}

const goToToolManagement = () => {
  router.push('/aiAgent/tools')
}

const goToToolDetail = (tool) => {
  router.push('/aiAgent/tools')
}

const loadDocuments = async () => {
  loading.value = true
  try {
    const res = await knowledgeAPI.listDocuments({
      page: pagination.page,
      page_size: pagination.pageSize,
      ...filter
    })
    documents.value = res?.items || []
    pagination.total = res?.total || 0
  } catch (e) {
    ElMessage.error('加载文档列表失败: ' + (e.message || ''))
  } finally {
    loading.value = false
  }
}

const handleSearch = () => {
  pagination.page = 1
  loadDocuments()
}

const handleFileChange = (file) => {
  if (file.size > 50 * 1024 * 1024) {
    ElMessage.error(i18n.global.t('文件大小不能超过 50MB'))
    return false
  }
  importForm.file = file.raw
  if (!importForm.title) {
    importForm.title = file.name
  }
}

const handleExceed = () => {
  ElMessage.warning(i18n.global.t('只能上传 1 个文件'))
}

const handleImport = async () => {
  if (!importForm.product_id) {
    ElMessage.error(i18n.global.t('请选择产品'))
    return
  }
  importing.value = true
  try {
    if (importTab.value === 'upload') {
      if (!importForm.file) {
        ElMessage.error(i18n.global.t('请选择文件'))
        importing.value = false
        return
      }
      const fd = new FormData()
      fd.append('product_id', importForm.product_id)
      fd.append('title', importForm.title)
      fd.append('category', importForm.category)
      fd.append('file', importForm.file)
      await knowledgeAPI.importUpload(fd)
    } else if (importTab.value === 'text') {
      if (!importForm.content) {
        ElMessage.error(i18n.global.t('请输入内容'))
        importing.value = false
        return
      }
      await knowledgeAPI.importText({
        product_id: importForm.product_id,
        title: importForm.title,
        content: importForm.content,
        category: importForm.category
      })
    } else if (importTab.value === 'url') {
      if (!importForm.url) {
        ElMessage.error(i18n.global.t('请输入 URL'))
        importing.value = false
        return
      }
      await knowledgeAPI.importURL({
        product_id: importForm.product_id,
        url: importForm.url,
        title: importForm.title,
        category: importForm.category
      })
    }
    ElMessage.success(i18n.global.t('导入任务已启动,处理可能需要几秒到几分钟'))
    showImportDialog.value = false
    resetImportForm()
    loadAll()
    startPolling()
  } catch (e) {
    ElMessage.error('导入失败: ' + (e.message || ''))
  } finally {
    importing.value = false
  }
}

const resetImportForm = () => {
  importForm.product_id = ''
  importForm.title = ''
  importForm.category = ''
  importForm.content = ''
  importForm.url = ''
  importForm.file = null
}

const showDocumentDetail = async (row) => {
  currentDoc.value = row
  showDetailDialog.value = true
  try {
    const res = await knowledgeAPI.getDocumentChunks(row.id)
    chunks.value = (res?.items || []).slice(0, 20)
  } catch (e) {
    console.error('加载分段失败:', e)
  }
}

const handleReindex = async (row) => {
  try {
    await ElMessageBox.confirm(`确认重建文档「${row.title}」的索引吗?`, '重建索引', { type: 'warning' })
    await knowledgeAPI.reindexDocument(row.id, { product_id: filter.product_id })
    ElMessage.success(i18n.global.t('重建任务已启动'))
    loadDocuments()
    startPolling()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error('重建失败: ' + (e.message || ''))
  }
}

const handleDelete = async (row) => {
  try {
    await ElMessageBox.confirm(`确认删除文档「${row.title}」吗?将同时删除其所有分段`, '删除文档', { type: 'warning' })
    await knowledgeAPI.deleteDocument(row.id, { product_id: filter.product_id })
    ElMessage.success(i18n.global.t('删除成功'))
    loadAll()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error('删除失败: ' + (e.message || ''))
  }
}

const startPolling = () => {
  if (pollTimer) return
  pollTimer = setInterval(async () => {
    const processing = documents.value.some(d => d.embed_status === 'processing' || d.embed_status === 'pending')
    if (!processing) {
      clearInterval(pollTimer)
      pollTimer = null
      return
    }
    await loadDocuments()
    await loadOverview()
  }, 3000)
};

onMounted(() => {
  loadAll()
})

onUnmounted(() => {
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
})

const getEmbedStatusLabel = (s) => getStatusLabel(s, EMBED_STATUS);
const getEmbedStatusTagType = (s) => getStatusTagType(s, EMBED_STATUS)
const embedStatusLabel = getEmbedStatusLabel;
const embedStatusTag = getEmbedStatusTagType
const sourceTypeLabel = (t) => getSourceLabel(t);
const sourceTypeTag = (t) => getSourceTagType(t)
const formatNumber = (n) => n == null ? '-' : Number(n).toLocaleString()
const formatPercent = (n) => n == null ? '-' : (n * 100).toFixed(1) + '%'
const togglePublic = async (row, visible) => {
  try {
    await http.patch(`/api/knowledge/documents/${row.id}/public-visibility`, { visible })
    row.public_visible = visible
    ElMessage.success(visible ? '已发布到帮助中心' : '已从帮助中心下线')
  } catch (e) {
    ElMessage.error(e?.message || '操作失败')
  }
};

const formatFileSize = (b) => {
  if (!b || b <= 0) return '-'
  if (b < 1024) return b + ' B'
  if (b < 1024 * 1024) return (b / 1024).toFixed(1) + ' KB'
  return (b / 1024 / 1024).toFixed(1) + ' MB'
}
const formatDate = (d) => d ? new Date(d).toLocaleString('zh-CN') : '-'
const truncateText = (s, n) => s ? (s.length > n ? s.substring(0, n) + '...' : s) : ''
</script>

<style scoped lang="scss">
.knowledge-v2-management {
  padding: 0;
}

.overview-row {
  margin-bottom: 16px;
}

.stat-card {
  text-align: center;
  .stat-label {
    font-size: 13px;
    color: #909399;
    margin-bottom: 8px;
  }
  .stat-value {
    font-size: 24px;
    font-weight: 600;
    color: #303133;
  }
}

.toolbar-card {
  margin-bottom: 16px;
}

.text-muted {
  color: #c0c4cc;
  font-size: 12px;
}

.tools-card {
  margin-bottom: 16px;
  
  .card-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  
  .tool-item {
    cursor: pointer;
    transition: all 0.3s;
    margin-bottom: 16px;
    
    &:hover {
      transform: translateY(-2px);
    }
    
    .tool-icon {
      display: flex;
      justify-content: center;
      margin-bottom: 12px;
      color: #409EFF;
    }
    
    .tool-info {
      text-align: center;
      
      .tool-name {
        font-size: 14px;
        font-weight: 500;
        color: #303133;
        margin-bottom: 8px;
      }
      
      .tool-status {
        font-size: 12px;
      }
    }
  }
}

.toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;

  .toolbar-left {
    display: flex;
    gap: 8px;
    align-items: center;
    flex-wrap: wrap;
  }
}

.chunk-preview {
  font-size: 12px;
  color: #606266;
  line-height: 1.6;
}
</style>
