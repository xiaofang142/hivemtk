<template>
  <el-drawer
    :model-value="modelValue"
    :title="drawerTitle"
    size="720px"
    direction="rtl"
    :destroy-on-close="false"
    @update:model-value="$emit('update:modelValue', $event)"
  >
    <div
      v-loading="loading"
      class="kb-drawer"
    >
      <template v-if="kbData">
        <el-card
          shadow="never"
          class="section"
        >
          <template #header>
            <div class="section-header">
              <el-icon><InfoFilled /></el-icon>
              <span>基本信息</span>
            </div>
          </template>
          <el-descriptions
            :column="2"
            border
          >
            <el-descriptions-item label="ID">
              {{ kbData.id }}
            </el-descriptions-item>
            <el-descriptions-item label="KB编码">
              {{ kbData.kb_code || '-' }}
            </el-descriptions-item>
            <el-descriptions-item
              label="名称"
              :span="2"
            >
              {{ kbData.name }}
            </el-descriptions-item>
            <el-descriptions-item
              label="类型"
              :span="1"
            >
              <el-tag
                :type="getTypeTagType(kbData.kb_type)"
                size="small"
              >
                {{ getTypeLabel(kbData.kb_type) }}
              </el-tag>
            </el-descriptions-item>
            <el-descriptions-item
              label="状态"
              :span="1"
            >
              <el-tag
                :type="kbData.status === 1 ? 'success' : 'info'"
                size="small"
              >
                {{ kbData.status === 1 ? '启用' : '禁用' }}
              </el-tag>
            </el-descriptions-item>
            <el-descriptions-item
              label="描述"
              :span="2"
            >
              {{ kbData.description || '-' }}
            </el-descriptions-item>
            <el-descriptions-item label="创建时间">
              {{ formatTime(kbData.created_at) }}
            </el-descriptions-item>
            <el-descriptions-item label="更新时间">
              {{ formatTime(kbData.updated_at) }}
            </el-descriptions-item>
          </el-descriptions>
        </el-card>

        
        <el-card
          shadow="never"
          class="section"
        >
          <template #header>
            <div class="section-header">
              <el-icon><DataLine /></el-icon>
              <span>统计信息</span>
            </div>
          </template>
          <el-row :gutter="16">
            <el-col :span="8">
              <div class="mini-stat">
                <div class="mini-stat-label">
                  {{ statsLabel.item }}
                </div>
                <div class="mini-stat-value">
                  {{ stats.item_count ?? '—' }}
                </div>
                <div class="mini-stat-hint">
                  <span v-if="stats.item_count == null">未接入</span>
                  <span v-else class="text-secondary">按成员绑定计数</span>
                </div>
              </div>
            </el-col>
            <el-col :span="8">
              <div class="mini-stat">
                <div class="mini-stat-label">
                  关联智能体
                </div>
                <div class="mini-stat-value text-primary">
                  {{ stats.agent_count }}
                </div>
              </div>
            </el-col>
            <el-col :span="8">
              <div class="mini-stat">
                <div class="mini-stat-label">
                  总命中次数
                </div>
                <div class="mini-stat-value text-success">
                  {{ stats.hit_count ?? '—' }}
                </div>
                <div class="mini-stat-hint">
                  未接入
                </div>
              </div>
            </el-col>
          </el-row>
        </el-card>

        
        <el-card
          shadow="never"
          class="section"
        >
          <template #header>
            <div class="section-header">
              <el-icon><Connection /></el-icon>
              <span>被哪些智能体使用（反向追溯）</span>
              <el-button
                type="primary"
                link
                size="small"
                :loading="loadingAgents"
                style="margin-left: auto"
                @click="loadAgentBindings"
              >
                <el-icon><Refresh /></el-icon>刷新
              </el-button>
            </div>
          </template>
          <el-empty
            v-if="!loadingAgents && agentBindings.length === 0"
            description="暂未被任何智能体挂载"
            :image-size="80"
          />
          <el-table
            v-else
            :data="agentBindings"
            stripe
            size="small"
            border
          >
            <el-table-column
              prop="id"
              label="ID"
              width="70"
              align="center"
            />
            <el-table-column
              label="智能体名称"
              min-width="160"
              show-overflow-tooltip
            >
              <template #default="{ row }">
                <span
                  class="agent-link"
                  role="button"
                  tabindex="0"
                  @click="goEditAgent(row)"
                  @keydown.enter.prevent="goEditAgent(row)"
                  @keydown.space.prevent="goEditAgent(row)"
                >
                  {{ row.name || row.agent_name || '-' }}
                </span>
              </template>
            </el-table-column>
            <el-table-column
              label="编码"
              width="140"
              show-overflow-tooltip
            >
              <template #default="{ row }">
                {{ row.agent_code || row.code || '-' }}
              </template>
            </el-table-column>
            <el-table-column
              label="类型"
              width="110"
              align="center"
            >
              <template #default="{ row }">
                <el-tag size="small">
                  {{ row.agent_type || row.type || '-' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              label="绑定时间"
              width="160"
            >
              <template #default="{ row }">
                {{ formatTime(row.created_at || row.bind_at) }}
              </template>
            </el-table-column>
            <el-table-column
              label="操作"
              width="100"
              align="center"
              fixed="right"
            >
              <template #default="{ row }">
                <el-button
                  link
                  type="primary"
                  size="small"
                  @click="goEditAgent(row)"
                >
                  查看
                </el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-card>

        
        <el-card
          shadow="never"
          class="section"
        >
          <template #header>
            <div class="section-header">
              <el-icon><List /></el-icon>
              <span>{{ statsLabel.item }}预览（前 20 条）</span>
            </div>
          </template>
          <el-empty
            v-if="!loadingItems && itemsPreview.length === 0"
            description="暂无子项"
            :image-size="80"
          />
          <el-table
            v-else
            :data="itemsPreview"
            stripe
            size="small"
            border
            max-height="320"
          >
            <el-table-column
              v-for="col in itemColumns"
              :key="col.prop"
              :prop="col.prop"
              :label="col.label"
              :min-width="col.minWidth || 120"
              :width="col.width"
              show-overflow-tooltip
            />
          </el-table>
        </el-card>
      </template>
      <el-empty
        v-else
        description="未选择知识库"
      />
    </div>

    <template #footer>
      <el-button @click="$emit('update:modelValue', false)">
        关闭
      </el-button>
      <el-button
        v-if="kbData?.id"
        type="primary"
        @click="goEdit"
      >
        <el-icon><Edit /></el-icon>编辑知识库
      </el-button>
    </template>
  </el-drawer>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import {
  InfoFilled,
  DataLine,
  Connection,
  List,
  Refresh,
  Edit
} from '@element-plus/icons-vue'
import { getKB, getKBStats } from '@/api/knowledgeBase'
import { listByKB } from '@/api/agentKBBinding'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  kbData: { type: Object, default: null }
})

defineEmits(['update:modelValue', 'updated'])
const router = useRouter()

const loading = ref(false)
const loadingAgents = ref(false)
const loadingItems = ref(false)
const agentBindings = ref([])
const itemsPreview = ref([])
const stats = ref({ agent_count: 0 })

const drawerTitle = computed(() => {
  if (!props.kbData) return '知识库详情'
  return `知识库详情：${props.kbData.name || props.kbData.kb_code || '#' + props.kbData.id}`
})

const statsLabel = computed(() => {
  const t = props.kbData?.kb_type
  if (t === 'rag') return { item: '文档数' }
  if (t === 'faq') return { item: 'FAQ 条目' }
  if (t === 'sop') return { item: 'SOP 模板' }
  return { item: '子项' }
})

const itemColumns = computed(() => {
  // 成员端点（/api/knowledge-bases/:id/items）的行形状：{id,item_type,item_id,title,created_at,...}。
  // 标题统一在 title 字段（后端按内容表反查 question/name/title），不再依赖各 List 的原始列名。
  return [
    { prop: 'item_id', label: 'ID', width: 70 },
    { prop: 'item_type', label: '类型', width: 80 },
    { prop: 'title', label: '标题', minWidth: 200 },
    { prop: 'created_at', label: '绑定时间', width: 160 }
  ]
})
const getTypeLabel = (type) => {
  const map = { rag: 'RAG 文档', faq: 'FAQ', sop: 'SOP 模板' }
  return map[type] || type || '-'
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

const loadAgentBindings = async () => {
  if (!props.kbData?.id) return
  loadingAgents.value = true
  try {
    const res = await listByKB(props.kbData.id).catch(() => null)
    const list = Array.isArray(res) ? res : res?.list || res?.items || []
    agentBindings.value = list
    stats.value.agent_count = list.length
  } catch (e) {
    agentBindings.value = []
  } finally {
    loadingAgents.value = false
  }
}

const loadStats = async () => {
  if (!props.kbData?.id) return
  try {
    const res = await getKBStats(props.kbData.id).catch(() => null)
    if (res) {
      stats.value = {
        agent_count: res.agent_count ?? stats.value.agent_count,
        item_count: res.item_count ?? null
      }
    }
  } catch { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
}

const loadItemsPreview = async () => {
  if (!props.kbData?.id) return
  loadingItems.value = true
  try {
    // P0-2/P0-3/D-8 方案 A：预览统一走成员端点 /api/knowledge-bases/:id/items，
    // 后端按 kb_item_bindings 只返回真正绑定进该 KB 的内容（附标题）。
    // 不再请求 /api/knowledge（从未注册，恒 404），也不再给全局 List 传 kb_id
    // （FAQ/SOP List 不读它，之前是"参数被静默忽略"的错数据来源）。
    const { http } = await import('@/utils/request')
    const res = await http
      .get(`/api/knowledge-bases/${props.kbData.id}/items`, { page: 1, page_size: 20 })
      .catch(() => null)
    const list = Array.isArray(res) ? res : res?.list || res?.items || []
    itemsPreview.value = list.slice(0, 20)
  } catch {
    itemsPreview.value = []
  } finally {
    loadingItems.value = false
  }
}

const goEdit = () => {
  if (!props.kbData?.id) return
  router.push({ name: 'KnowledgeBaseEdit', params: { id: props.kbData.id } })
}

const goEditAgent = (row) => {
  const id = row.agent_id || row.id
  if (!id) return
  // 绑定关系可能指向已删除的智能体（库内无外键），此时接口不返回名称
  if (!row.name && !row.agent_name) {
    ElMessage.warning('该智能体已不存在，无法编辑')
    return
  }
  // 跨模块跳转必须用 path：aiAgent 模块是懒注册的，若本会话还没进过 /aiAgent，
  // 按 name 跳会抛 "No match for {name:'AIAgentEdit'}"（KnowledgeBase → aiAgent 是首例）
  router.push(`/aiAgent/edit/${id}`)
}

watch(
  () => [props.modelValue, props.kbData?.id],
  ([visible, id]) => {
    if (visible && id) {
      loading.value = true
      Promise.all([loadAgentBindings(), loadStats(), loadItemsPreview()])
        .catch(() => {})
        .finally(() => {
          loading.value = false
        })
    }
  },
  { immediate: true }
)
</script>

<style scoped>
.kb-drawer {
  padding: 0 4px;
}
.section {
  margin-bottom: 16px;
}
.section-header {
  display: flex;
  align-items: center;
  gap: 6px;
  font-weight: 600;
}
.mini-stat {
  text-align: center;
  padding: 12px;
  background: #f5f7fa;
  border-radius: 6px;
}
.mini-stat-label {
  color: #909399;
  font-size: 12px;
}
.mini-stat-value {
  font-size: 22px;
  font-weight: 600;
  margin-top: 4px;
  color: #303133;
}
.mini-stat-hint {
  margin-top: 2px;
  font-size: 11px;
  color: #c0c4cc;
}
.text-primary {
  color: #409eff;
}
.text-success {
  color: #67c23a;
}
.agent-link {
  color: #409eff;
  cursor: pointer;
}
.agent-link:hover {
  text-decoration: underline;
}
</style>
