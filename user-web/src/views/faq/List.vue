<template>
  <div class="faq-list-page">
    <el-card
      class="header-card"
      shadow="never"
    >
      <div class="header-content">
        <div>
          <h2>FAQ 知识库</h2>
          <p class="subtitle">
            Layer1 FAQ 快速匹配知识库：客户消息命中即跳过 LLM 直接返回标准答案
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
          <el-button @click="goTest">
            <el-icon><Aim /></el-icon>
            匹配测试
          </el-button>
          <el-button
            type="primary"
            @click="goCreate"
          >
            <el-icon><Plus /></el-icon>
            新建 FAQ
          </el-button>
        </div>
      </div>
    </el-card>

    
    <el-row
      v-if="stats"
      :gutter="20"
      class="stat-row"
    >
      <el-col :span="6">
        <el-card
          shadow="never"
          class="stat-card"
        >
          <div class="stat-label">
            总数
          </div>
          <div class="stat-value">
            {{ stats.total || 0 }}
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card
          shadow="never"
          class="stat-card"
        >
          <div class="stat-label">
            启用
          </div>
          <div class="stat-value text-success">
            {{ stats.enabled || 0 }}
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card
          shadow="never"
          class="stat-card"
        >
          <div class="stat-label">
            启用率
          </div>
          <div class="stat-value text-primary">
            {{ enableRate }}
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card
          shadow="never"
          class="stat-card"
        >
          <div class="stat-label">
            分类数
          </div>
          <div class="stat-value text-info">
            {{ categoryCount }}
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
        @submit.prevent
      >
        <el-form-item label="关键词">
          <el-input
            v-model="filter.keyword"
            placeholder="搜索问题/答案/关键词"
            clearable
            style="width: 240px"
            @keyup.enter="onSearch"
            @clear="onSearch"
          />
        </el-form-item>
        <el-form-item label="分类">
          <el-select
            v-model="filter.category"
            placeholder="全部分类"
            clearable
            style="width: 160px"
            @change="onSearch"
          >
            <el-option
              v-for="c in categoryOptions"
              :key="c"
              :label="c"
              :value="c"
            />
          </el-select>
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
        <el-form-item label="状态">
          <el-select
            v-model="filter.enabled"
            placeholder="全部状态"
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
          label="问题"
          min-width="280"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            <div class="question-text">
              {{ row.question }}
            </div>
            <div
              v-if="row.keywords && row.keywords.length"
              class="keyword-list"
            >
              <el-tag
                v-for="k in row.keywords.slice(0, 5)"
                :key="k"
                size="small"
                type="info"
                effect="plain"
                class="keyword-tag"
              >
                {{ k }}
              </el-tag>
              <span
                v-if="row.keywords.length > 5"
                class="keyword-more"
              >+{{ row.keywords.length - 5 }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column
          label="答案预览"
          min-width="220"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            {{ truncate(row.answer, 80) }}
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
              type="success"
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
          label="分类"
          width="120"
          align="center"
        >
          <template #default="{ row }">
            <el-tag
              v-if="row.category"
              size="small"
              type="info"
            >
              {{ row.category }}
            </el-tag>
            <span v-else>-</span>
          </template>
        </el-table-column>
        <el-table-column
          label="意图"
          width="120"
          align="center"
        >
          <template #default="{ row }">
            <el-tag
              v-if="row.intent"
              size="small"
              type="warning"
            >
              {{ row.intent }}
            </el-tag>
            <span v-else>-</span>
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
              :show-text="true"
              :color="getConfColor(row.confidence)"
              style="width: 80px"
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
          width="180"
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
              type="primary"
              size="small"
              @click="onTestOne(row)"
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
          <el-empty description="暂无 FAQ 数据" />
        </template>
      </el-table>

      <div class="pagination-wrap">
        <el-pagination
          v-model:current-page="filter.page"
          v-model:page-size="filter.page_size"
          :total="total"
          :page-sizes="[10, 20, 50, 100]"
          layout="total, sizes, prev, pager, next, jumper"
          @size-change="loadList"
          @current-change="loadList"
        />
      </div>
    </el-card>

    
    <el-dialog
      v-model="testDialogVisible"
      title="FAQ 匹配测试"
      width="640px"
      top="6vh"
    >
      <el-form
        :model="testForm"
        label-width="80px"
      >
        <el-form-item label="智能体">
          <el-select
            v-model="testForm.agent_id"
            filterable
            clearable
            placeholder="请选择要匹配的智能体"
            style="width: 100%"
          >
            <el-option
              v-for="a in agentOptions"
              :key="a.id"
              :label="a.name || a.agent_name || ('智能体 ' + a.id)"
              :value="a.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="测试问题">
          <el-input
            v-model="testForm.msg"
            type="textarea"
            :rows="2"
            placeholder="请输入要匹配的测试问题"
          />
        </el-form-item>
        <el-form-item>
          <el-button
            type="primary"
            :loading="testLoading"
            @click="runTest"
          >
            <el-icon><Aim /></el-icon>
            执行匹配
          </el-button>
        </el-form-item>
      </el-form>
      <template v-if="testResult.length">
        <el-divider content-position="left">
          匹配结果
        </el-divider>
        <el-table
          :data="testResult"
          stripe
          size="small"
          border
        >
          <el-table-column
            type="index"
            label="排名"
            width="70"
            align="center"
          />
          <el-table-column
            label="问题"
            min-width="200"
            show-overflow-tooltip
          >
            <template #default="{ row }">
              {{ row.entry?.question }}
            </template>
          </el-table-column>
          <el-table-column
            label="置信度"
            width="100"
            align="center"
          >
            <template #default="{ row }">
              <el-tag
                :type="getMatchTagType(row.score)"
                size="small"
              >
                {{ (row.score * 100).toFixed(0) }}%
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column
            label="匹配类型"
            width="120"
            align="center"
          >
            <template #default="{ row }">
              <el-tag
                size="small"
                type="info"
              >
                {{ row.match_type || '-' }}
              </el-tag>
            </template>
          </el-table-column>
        </el-table>
      </template>
      <el-empty
        v-else-if="!testLoading && testTouched"
        description="无匹配结果"
        :image-size="60"
      />
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, reactive } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh, Plus, Search, Aim } from '@element-plus/icons-vue'
import { faqApi } from '@/api/faq'
import { listKBs } from '@/api/knowledgeBase'
import { listEnabledAgents } from '@/api/aiAgent'
import { isUserReportedError } from '@/utils/errorReporting'

const router = useRouter()

const loading = ref(false);
const list = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const stats = ref(null)
const filter = ref({
  keyword: '',
  category: '',
  intent: '',
  enabled: ''
})

const kbMap = ref({});

const loadKBMap = async () => {
  try {
    const res = await listKBs({ kb_type: 'faq', page: 1, page_size: 200 }).catch(() => null)
    const items = Array.isArray(res) ? res : res?.list || res?.items || []
    const map = {}
    items.forEach((it) => {
      map[it.id] = { name: it.name, kb_code: it.kb_code }
    })
    kbMap.value = map
  } catch (e) {
    kbMap.value = {};
  }
};

const getKBName = (kbId) => {
  if (kbId == null || kbId === '' || kbId === 0) return ''
  const info = kbMap.value[kbId]
  if (!info) return ''
  return info.kb_code ? `[${info.kb_code}] ${info.name}` : info.name
}

const categoryOptions = computed(() => {
  const set = new Set()
  list.value.forEach((it) => { if (it.category) set.add(it.category) })
  return Array.from(set)
});

const enableRate = computed(() => {
  if (!stats.value?.total) return '0%'
  return `${Math.round((stats.value.enabled / stats.value.total) * 100)}%`
})

const categoryCount = computed(() => {
  const set = new Set()
  list.value.forEach((it) => { if (it.category) set.add(it.category) })
  return set.size
})

const testDialogVisible = ref(false);
const testLoading = ref(false)
const testTouched = ref(false)
// 后端 /api/faqs/match 要求 agent_id（FAQ 与智能体 1 对 1），不传必然 400
const testForm = ref({ msg: '', agent_id: null })
const agentOptions = ref([])
const testResult = ref([])

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

const getMatchTagType = (s) => {
  if (s >= 0.8) return 'success'
  if (s >= 0.6) return 'warning'
  return 'info'
}

const loadList = async () => {
  loading.value = true
  try {
    const params = {
      page: page.value,
      page_size: pageSize.value
    }
    if (filter.value.keyword) params.keyword = filter.value.keyword
    if (filter.value.category) params.category = filter.value.category
    if (filter.value.intent) params.intent = filter.value.intent
    if (filter.value.enabled !== '' && filter.value.enabled !== null) {
      params.enabled = filter.value.enabled
    }
    const res = await faqApi.list(params)
    list.value = res?.list || []
    total.value = res?.total || 0
  } catch (e) {
    ElMessage.error('加载 FAQ 列表失败：' + (e.message || '未知错误'))
  } finally {
    loading.value = false
  }
};

const loadStats = async () => {
  try {
    const res = await faqApi.stats()
    stats.value = res || { total: 0, enabled: 0 }
  } catch (e) {
    console.warn('加载 FAQ 统计失败：', e?.message);
  }
}

const onSearch = () => {
  page.value = 1
  loadList()
}

const resetFilter = () => {
  filter.value = {
    keyword: '',
    category: '',
    intent: '',
    enabled: ''
  }
  page.value = 1
  loadList()
}

const goCreate = () => {
  router.push('/faq/editor')
};

const goEdit = (row) => {
  router.push(`/faq/editor/${row.id}`)
}

const goTest = () => {
  testTouched.value = false
  testResult.value = []
  testForm.value = { msg: '', agent_id: testForm.value.agent_id || null }
  testDialogVisible.value = true
  loadAgentOptions()
}

const loadAgentOptions = async () => {
  if (agentOptions.value.length) return
  try {
    const res = await listEnabledAgents()
    const list = Array.isArray(res) ? res : (res?.list || [])
    agentOptions.value = list
    if (!testForm.value.agent_id && list.length) testForm.value.agent_id = list[0].id
  } catch {
    agentOptions.value = []
  }
}

const onToggle = async (row, v) => {
  row._toggling = true
  try {
    // 专用 toggle 端点只收 enabled，避免全量 Update 的 agent_id required 校验
    await faqApi.toggle(row.id, v)
    ElMessage.success(v ? '已启用' : '已禁用')
    await loadList()
    await loadStats()
  } catch (e) {
    ElMessage.error('状态切换失败：' + (e.message || '未知错误'))
  } finally {
    row._toggling = false
  }
};

const stripToPayload = (row) => ({
  question: row.question,
  answer: row.answer,
  keywords: row.keywords || [],
  category: row.category || '',
  intent: row.intent || '',
  confidence: row.confidence || 0
})

const onDelete = (row) => {
  ElMessageBox.confirm(`确认删除 FAQ「${truncate(row.question, 30)}」吗？删除后不可恢复。`, '删除确认', {
    type: 'warning',
    confirmButtonText: '确认删除',
    cancelButtonText: '取消'
  }).then(async () => {
    try {
      await faqApi.remove(row.id)
      ElMessage.success('删除成功')
      await loadList()
      await loadStats()
    } catch (e) {
      ElMessage.error('删除失败：' + (e.message || '未知错误'))
    }
  }).catch(() => {})
};

const onTestOne = (row) => {
  testTouched.value = true
  testForm.value.msg = row.question
  testDialogVisible.value = true
  loadAgentOptions()
  runTest()
};

const runTest = async () => {
  if (!testForm.value.msg || !testForm.value.msg.trim()) {
    ElMessage.warning('请输入测试问题')
    return
  }
  if (!testForm.value.agent_id) {
    ElMessage.warning('请选择智能体')
    return
  }
  testLoading.value = true
  testTouched.value = true
  try {
    const res = await faqApi.match({ msg: testForm.value.msg, top_k: 5, agent_id: testForm.value.agent_id })
    testResult.value = res || []
  } catch (e) {
    testResult.value = []
    if (!isUserReportedError(e)) ElMessage.error('匹配失败：' + (e.message || '未知错误'))
  } finally {
    testLoading.value = false
  }
};

onMounted(() => {
  loadAgentOptions()
  loadKBMap()
  loadList()
  loadStats()
})
</script>

<style scoped>
.faq-list-page {
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

.stat-row .stat-card :deep(.el-card__body) {
  text-align: center;
  padding: 16px;
}

.stat-label {
  color: var(--el-text-color-secondary);
  font-size: 13px;
  margin-bottom: 6px;
}

.stat-value {
  font-size: 24px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.text-success { color: var(--el-color-success); }
.text-primary { color: var(--el-color-primary); }
.text-info { color: var(--el-color-info); }
.text-muted { color: #c0c4cc; font-size: 12px; }

.filter-card :deep(.el-card__body) {
  padding: 16px 20px 0 20px;
}

.question-text {
  font-weight: 500;
  margin-bottom: 4px;
  color: var(--el-text-color-primary);
}

.keyword-list {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-top: 4px;
}

.keyword-tag {
  font-size: 11px;
}

.keyword-more {
  color: var(--el-text-color-secondary);
  font-size: 11px;
  margin-left: 4px;
}

.pagination-wrap {
  margin-top: 16px;
  display: flex;
  justify-content: flex-end;
}
</style>
