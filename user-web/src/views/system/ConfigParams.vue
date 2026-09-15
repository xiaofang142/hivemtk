<template>
  <div class="config-params-page">
    
    <el-row :gutter="16" class="stat-row">
      <el-col :xs="12" :sm="6">
        <el-card shadow="hover" class="stat-card">
          <div class="stat-inner">
            <div class="stat-icon stat-icon--total"><el-icon :size="24"><Setting /></el-icon></div>
            <div class="stat-body">
              <div class="stat-value">{{ stats.total }}</div>
              <div class="stat-label">总参数</div>
            </div>
          </div>
        </el-card>
      </el-col>
      <el-col :xs="12" :sm="6">
        <el-card shadow="hover" class="stat-card">
          <div class="stat-inner">
            <div class="stat-icon stat-icon--group"><el-icon :size="24"><Files /></el-icon></div>
            <div class="stat-body">
              <div class="stat-value">{{ stats.groupCount }}</div>
              <div class="stat-label">分组数</div>
            </div>
          </div>
        </el-card>
      </el-col>
      <el-col :xs="12" :sm="6">
        <el-card shadow="hover" class="stat-card">
          <div class="stat-inner">
            <div class="stat-icon stat-icon--modified"><el-icon :size="24"><Warning /></el-icon></div>
            <div class="stat-body">
              <div class="stat-value">{{ stats.modified }}</div>
              <div class="stat-label">已修改</div>
            </div>
          </div>
        </el-card>
      </el-col>
      <el-col :xs="12" :sm="6">
        <el-card shadow="hover" class="stat-card">
          <div class="stat-inner">
            <div class="stat-icon stat-icon--readonly"><el-icon :size="24"><Lock /></el-icon></div>
            <div class="stat-body">
              <div class="stat-value">{{ stats.readonly }}</div>
              <div class="stat-label">只读</div>
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    
    <el-row :gutter="16" class="main-row">
      
      <el-col :xs="24" :md="5" :lg="4">
        <el-card shadow="hover" class="group-card">
          <template #header>
            <div class="card-header">
              <span>参数分组</span>
              <el-button text size="small" @click="loadAll">
                <el-icon><Refresh /></el-icon>
              </el-button>
            </div>
          </template>
          <div class="group-list">
            <div
              v-for="g in groups"
              :key="g.key"
              class="group-item"
              role="button"
              tabindex="0"
              :class="{ active: currentGroup === g.key }"
              @click="switchGroup(g.key)"
              @keydown.enter.prevent="switchGroup(g.key)"
              @keydown.space.prevent="switchGroup(g.key)"
            >
              <div class="group-item__main">
                <el-icon class="group-icon"><Collection /></el-icon>
                <div class="group-name">{{ g.label }}</div>
              </div>
              <div class="group-item__count">
                <span class="group-count">{{ g.count }}</span>
                <span v-if="getGroupModified(g.key) > 0" class="group-mod">
                  {{ getGroupModified(g.key) }}
                </span>
              </div>
            </div>
          </div>
        </el-card>
      </el-col>

      
      <el-col :xs="24" :md="19" :lg="20">
        <el-card shadow="hover" class="table-card">
          <template #header>
            <div class="card-header">
              <div class="header-title">
                <el-tag type="info" effect="plain" class="group-tag">{{ currentGroupLabel }}</el-tag>
                <span class="header-count">共 {{ filteredList.length }} 条</span>
              </div>
              <div class="header-actions">
                <el-input
                  v-model="keyword"
                  placeholder="搜索 key / 名称 / 描述"
                  clearable
                  size="default"
                  style="width: 260px"
                >
                  <template #prefix><el-icon><Search /></el-icon></template>
                </el-input>
                <el-button :disabled="groupModifiedCount === 0" :loading="resettingGroup" @click="handleBulkReset">
                  <el-icon><RefreshLeft /></el-icon>
                  重置整组
                  <span v-if="groupModifiedCount > 0" class="reset-badge">{{ groupModifiedCount }}</span>
                </el-button>
                <el-button @click="openAuditDrawer">
                  <el-icon><Document /></el-icon>
                  审计日志
                </el-button>
              </div>
            </div>
          </template>

          <el-table
            v-loading="loading"
            :data="filteredList"
            stripe
            border
            empty-text="暂无参数"
            style="width: 100%"
            :header-cell-style="{ background: '#fafafa' }"
          >
            <el-table-column prop="key" label="Key" width="220" show-overflow-tooltip>
              <template #default="{ row }">
                <code class="param-key">{{ row.key }}</code>
              </template>
            </el-table-column>
            <el-table-column prop="name" label="名称" min-width="140" show-overflow-tooltip />
            <el-table-column prop="description" label="描述" min-width="220" show-overflow-tooltip />
            <el-table-column label="类型" width="90" align="center">
              <template #default="{ row }">
                <el-tag :type="typeTagColor(row.type)" size="small">{{ row.type }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="当前值" min-width="180" show-overflow-tooltip>
              <template #default="{ row }">
                <div class="val-cell">
                  <span class="val-current" :class="{ 'val-changed': !isDefault(row) }">
                    {{ formatValue(row, row.value) }}
                  </span>
                  <el-tag v-if="!isDefault(row)" type="warning" size="small" effect="plain" class="changed-badge">已修改</el-tag>
                  <el-tag v-if="row.readonly" type="info" size="small" effect="plain">只读</el-tag>
                </div>
              </template>
            </el-table-column>
            <el-table-column label="默认值" min-width="140" show-overflow-tooltip>
              <template #default="{ row }">
                <span class="val-default">{{ formatValue(row, row.default_value) }}</span>
              </template>
            </el-table-column>
            <el-table-column label="范围" width="170" show-overflow-tooltip>
              <template #default="{ row }">
                <span v-if="row.min !== undefined || row.max !== undefined || row.step !== undefined" class="range-cell">
                  <template v-if="row.min !== undefined && row.max !== undefined">[{{ row.min }}, {{ row.max }}]</template>
                  <template v-else-if="row.min !== undefined">≥ {{ row.min }}</template>
                  <template v-else-if="row.max !== undefined">≤ {{ row.max }}</template>
                  <template v-if="row.step !== undefined && row.step !== 1 && row.step !== 0"> step={{ row.step }}</template>
                </span>
                <span v-else class="muted">-</span>
              </template>
            </el-table-column>
            <el-table-column label="操作" width="160" fixed="right" align="center">
              <template #default="{ row }">
                <el-button link type="primary" size="small" :disabled="row.readonly" @click="openEdit(row)">
                  <el-icon><Edit /></el-icon>编辑
                </el-button>
                <el-button link type="warning" size="small" :disabled="isDefault(row)" :loading="row._resetting" @click="handleResetOne(row)">
                  <el-icon><RefreshLeft /></el-icon>重置
                </el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-col>
    </el-row>

    
    <el-dialog
      v-model="editDialogVisible"
      :title="`编辑参数 · ${editing?.key ?? ''}`"
      width="520px"
      :close-on-click-modal="false"
      destroy-on-close
    >
      <div v-if="editing" class="edit-body">
        <el-descriptions :column="2" border size="small" class="edit-desc">
          <el-descriptions-item label="分组">{{ editing.group }}</el-descriptions-item>
          <el-descriptions-item label="类型">
            <el-tag :type="typeTagColor(editing.type)" size="small">{{ editing.type }}</el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="名称" :span="2">{{ editing.name }}</el-descriptions-item>
          <el-descriptions-item label="描述" :span="2">{{ editing.description || '-' }}</el-descriptions-item>
          <el-descriptions-item label="默认值">{{ formatValue(editing, editing.default_value) }}</el-descriptions-item>
          <el-descriptions-item label="只读">
            <el-tag :type="editing.readonly ? 'info' : 'success'" size="small">{{ editing.readonly ? '是' : '否' }}</el-tag>
          </el-descriptions-item>
          <el-descriptions-item v-if="editing.min !== undefined" label="最小值">{{ editing.min }}</el-descriptions-item>
          <el-descriptions-item v-if="editing.max !== undefined" label="最大值">{{ editing.max }}</el-descriptions-item>
        </el-descriptions>

        <div class="edit-field">
          <div class="edit-field__label">当前值</div>

          
          <template v-if="isBoolType(editing.type)">
            <el-switch v-model="editValueBool" :active-value="true" :inactive-value="false" />
            <span class="muted" style="margin-left:8px">{{ editValueBool ? 'true' : 'false' }}</span>
          </template>

          
          <template v-else-if="isNumericType(editing.type)">
            <div v-if="hasSlider(editing)" class="slider-wrap">
              <el-slider
                v-model="editValueNum"
                :min="editing.min ?? 0"
                :max="editing.max ?? 100"
                :step="editing.step ?? (editing.type === 'float' || editing.type === 'duration' ? 0.1 : 1)"
                :show-tooltip="false"
                style="flex:1; margin-right:12px"
              />
              <el-input-number
                v-model="editValueNum"
                :min="editing.min"
                :max="editing.max"
                :step="editing.step ?? (editing.type === 'float' || editing.type === 'duration' ? 0.1 : 1)"
                :precision="editing.type === 'float' || editing.type === 'duration' ? 3 : 0"
                controls-position="right"
                size="default"
                style="width:140px"
              />
              <span v-if="editing.type === 'duration'" class="unit-label">秒</span>
            </div>
            <el-input-number
              v-else
              v-model="editValueNum"
              :min="editing.min"
              :max="editing.max"
              :step="editing.step ?? (editing.type === 'float' || editing.type === 'duration' ? 0.1 : 1)"
              :precision="editing.type === 'float' || editing.type === 'duration' ? 3 : 0"
              controls-position="right"
              size="default"
              style="width:100%"
            />
          </template>

          
          <template v-else>
            <el-input
              v-model="editValueStr"
              :type="isTextareaType(editing.type) ? 'textarea' : 'text'"
              :rows="3"
              :placeholder="`默认: ${formatValue(editing, editing.default_value)}`"
            />
          </template>
        </div>

        <div v-if="editError" class="edit-error"><el-icon><WarningFilled /></el-icon> {{ editError }}</div>
      </div>

      <template #footer>
        <el-button @click="editDialogVisible = false">取消</el-button>
        <el-button
          type="warning"
          :disabled="editValueUnchanged || editError"
          :loading="saving"
          @click="confirmEdit"
        >
          保存修改
        </el-button>
      </template>
    </el-dialog>

    
    <el-drawer v-model="auditDrawerVisible" title="变更审计日志" size="640px">
      <template v-if="auditLoading">
        <div class="drawer-loading"><el-icon class="is-loading" :size="28"><Loading /></el-icon></div>
      </template>
      <template v-else>
        <div v-if="auditList.length === 0" class="drawer-empty">暂无变更记录</div>
        <el-timeline v-else class="audit-timeline">
          <el-timeline-item
            v-for="(item, idx) in auditList"
            :key="idx"
            :timestamp="formatTs(item.updated_at || item.created_at)"
            placement="top"
          >
            <div class="audit-item">
              <div class="audit-item__head">
                <el-tag size="small" effect="plain">{{ item.group }}</el-tag>
                <code class="audit-key">{{ item.key }}</code>
                <span class="audit-user">{{ item.operator || item.user || item.updated_by || '-' }}</span>
              </div>
              <div class="audit-item__diff">
                <span class="val-default">{{ String(item.old_value ?? item.old ?? '') || '(空)' }}</span>
                <el-icon class="audit-arrow"><ArrowRight /></el-icon>
                <span class="val-current val-changed">{{ String(item.new_value ?? item.value ?? '') || '(空)' }}</span>
              </div>
            </div>
          </el-timeline-item>
        </el-timeline>
      </template>
      <template #footer>
        <el-button @click="loadAuditLogs" :loading="auditLoading">
          <el-icon><Refresh /></el-icon>刷新
        </el-button>
      </template>
    </el-drawer>
  </div>
</template>

<script setup>
import { computed, ref, onMounted, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import * as configParamsApi from '@/api/configParams'

const SEED_GROUPS = [
  { key: 'agent_llm',    label: 'AI 大模型',      count: 16 },
  { key: 'agent_tool',   label: 'AI 工具',        count: 5  },
  { key: 'bridge',       label: '消息桥接',       count: 8  },
  { key: 'cache',        label: '缓存策略',       count: 6  },
  { key: 'channelgw',    label: '渠道网关',       count: 5  },
  { key: 'confidence',   label: '置信度阈值',     count: 7  },
  { key: 'inbox_sales',  label: '私信销售',       count: 4  },
  { key: 'knowledge',    label: '知识库',         count: 12 },
  { key: 'middleware',   label: '中间件',         count: 2  },
  { key: 'misc',         label: '其它',           count: 18 },
  { key: 'pagination',   label: '分页',           count: 3  },
  { key: 'sales',        label: '销售',           count: 2  },
  { key: 'session',      label: '会话',           count: 5  },
  { key: 'smart_cs',     label: '智能客服',       count: 3  },
  { key: 'telemetry',    label: '遥测',           count: 4  },
  { key: 'wechat',       label: '微信',           count: 3  },
  { key: 'wecom',        label: '企业微信',       count: 2  },
  { key: 'workflow',     label: '工作流',         count: 4  }
];

const groups = ref(SEED_GROUPS)
const allParams = ref([])
const currentGroup = ref('agent_llm')
const loading = ref(false)
const saving = ref(false)
const keyword = ref('')

const stats = computed(() => {
  const list = allParams.value
  return {
    total: list.length,
    groupCount: new Set(list.map((p) => p.group)).size || SEED_GROUPS.length,
    modified: list.filter((p) => !isDefault(p)).length,
    readonly: list.filter((p) => p.readonly).length
  }
});

const currentGroupLabel = computed(() => {
  const g = groups.value.find((x) => x.key === currentGroup.value)
  return g ? `${g.label} / ${g.key}` : currentGroup.value
})

const filteredList = computed(() => {
  const kw = keyword.value.trim().toLowerCase()
  const groupList = allParams.value.filter((p) => p.group === currentGroup.value)
  if (!kw) return groupList
  return groupList.filter((p) => {
    const hay = `${p.key} ${p.name} ${p.description || ''}`.toLowerCase()
    return hay.includes(kw)
  })
})

const groupModifiedCount = computed(() => {
  return filteredList.value.filter((p) => !isDefault(p)).length
})

async function loadAll() {
  loading.value = true
  try {
    const res = await configParamsApi.list(undefined);
    const list = Array.isArray(res) ? res : (res && Array.isArray(res.list) ? res.list : [])
    allParams.value = list
  } catch (e) {
    ElMessage.error('加载参数失败')
  } finally {
    loading.value = false
  }
}

async function loadGroup(groupKey) {
  loading.value = true
  try {
    const res = await configParamsApi.list(groupKey)
    const list = Array.isArray(res) ? res : (res && Array.isArray(res.list) ? res.list : [])
    const remain = allParams.value.filter((p) => p.group !== groupKey);
    allParams.value = [...remain, ...list]
    const g = groups.value.find((x) => x.key === groupKey);
    if (g) g.count = list.length
  } catch (e) {
    ElMessage.error('加载分组参数失败')
  } finally {
    loading.value = false
  }
}

function switchGroup(key) {
  if (currentGroup.value === key) return
  currentGroup.value = key
  if (!allParams.value.some((p) => p.group === key)) {
    loadGroup(key)
  }
}

onMounted(() => {
  loadAll()
})

function getGroupModified(groupKey) {
  return allParams.value.filter((p) => p.group === groupKey && !isDefault(p)).length
}

function isDefault(p) {
  if (p.value === undefined || p.value === null) return true
  if (p.default_value === undefined || p.default_value === null) return false
  if (typeof p.value === 'number' || (typeof p.value === 'string' && /^-?\d+(\.\d+)?$/.test(p.value))) {
    const a = Number(p.value)
    const b = Number(p.default_value)
    if (Number.isFinite(a) && Number.isFinite(b)) return a === b
  }
  return String(p.value) === String(p.default_value)
}

function formatValue(row, v) {
  if (v === undefined || v === null) return ''
  if (row && row.type === 'duration') {
    const n = Number(v)
    if (Number.isFinite(n)) return `${n} 秒`
  }
  if (typeof v === 'boolean') return v ? 'true' : 'false'
  if (typeof v === 'object') {
    try { return JSON.stringify(v) } catch { return String(v) }
  }
  return String(v)
}

function typeTagColor(type) {
  if (!type) return 'info'
  switch (type.toLowerCase()) {
    case 'bool':
    case 'boolean': return 'success'
    case 'int':
    case 'integer': return 'primary'
    case 'float': return 'warning'
    case 'duration': return 'danger'
    case 'string':
    case 'text': return ''
    default: return 'info'
  }
}

function isBoolType(t) {
  return typeof t === 'string' && ['bool', 'boolean'].includes(t.toLowerCase())
}
function isNumericType(t) {
  return typeof t === 'string' && ['int', 'integer', 'float', 'duration', 'number'].includes(t.toLowerCase())
}
function isTextareaType(t) {
  return typeof t === 'string' && ['text', 'json', 'yaml'].includes(t.toLowerCase())
}
function hasSlider(row) {
  const hasMinMax = row.min !== undefined && row.max !== undefined && Number(row.min) !== Number(row.max)
  return hasMinMax && (isNumericType(row.type))
}

const editDialogVisible = ref(false);
const editing = ref(null)
const editValueBool = ref(false)
const editValueNum = ref(0)
const editValueStr = ref('')
const editError = ref('')

const editValueUnchanged = computed(() => {
  if (!editing.value) return true
  const p = editing.value
  if (isBoolType(p.type)) return editValueBool.value === (p.value === true || p.value === 'true')
  if (isNumericType(p.type)) {
    if (p.value === undefined || p.value === null) return editValueNum.value === 0
    const a = Number(editValueNum.value)
    const b = Number(p.value)
    return a === b
  }
  return editValueStr.value === String(p.value ?? '')
})

function openEdit(row) {
  if (row.readonly) {
    ElMessage.warning('该参数为只读，不可编辑')
    return
  }
  editing.value = { ...row }
  editError.value = ''
  if (isBoolType(row.type)) {
    const v = row.value === true || row.value === 'true' || row.value === 1
    editValueBool.value = v
  } else if (isNumericType(row.type)) {
    const n = Number(row.value)
    editValueNum.value = Number.isFinite(n) ? n : Number(row.default_value || 0)
  } else {
    editValueStr.value = row.value === undefined || row.value === null ? '' : String(row.value)
  }
  editDialogVisible.value = true
}

function validateEdit() {
  editError.value = ''
  const p = editing.value
  if (isNumericType(p.type)) {
    const n = Number(editValueNum.value)
    if (!Number.isFinite(n)) { editError.value = '必须是合法数字'; return false }
    if (p.min !== undefined && n < Number(p.min)) { editError.value = `不能小于最小值 ${p.min}`; return false }
    if (p.max !== undefined && n > Number(p.max)) { editError.value = `不能大于最大值 ${p.max}`; return false }
  }
  return true
}

watch([editValueBool, editValueNum, editValueStr], () => {
  if (editing.value) validateEdit()
})

async function confirmEdit() {
  if (!validateEdit()) return
  const p = editing.value
  let submitValue
  if (isBoolType(p.type)) submitValue = editValueBool.value
  else if (isNumericType(p.type)) {
    if (p.type === 'int' || p.type === 'integer') submitValue = Math.round(Number(editValueNum.value))
    else submitValue = Number(editValueNum.value)
  } else submitValue = editValueStr.value

  saving.value = true
  try {
    await configParamsApi.update(p.group, p.key, submitValue)
    const target = allParams.value.find((x) => x.group === p.group && x.key === p.key);
    if (target) target.value = submitValue
    editDialogVisible.value = false
    ElMessage.success('参数已更新')
  } catch (e) {
    ElMessage.error('保存失败：' + (e.message || '未知错误'))
  } finally {
    saving.value = false
  }
}

const resettingGroup = ref(false);

async function handleResetOne(row) {
  if (isDefault(row)) { ElMessage.info('已是默认值'); return }
  try {
    await ElMessageBox.confirm(
      `确定要将「${row.key}」重置为默认值吗？`,
      '确认重置',
      { confirmButtonText: '重置', cancelButtonText: '取消', type: 'warning' }
    )
    row._resetting = true
    await configParamsApi.reset(row.group, row.key)
    row.value = row.default_value
    ElMessage.success('已重置为默认值')
  } catch (e) {
    if (e !== 'cancel') ElMessage.error('重置失败：' + (e.message || '未知错误'))
  } finally {
    row._resetting = false
  }
}

async function handleBulkReset() {
  const count = groupModifiedCount.value
  if (count === 0) { ElMessage.info('当前分组没有已修改的参数'); return }
  try {
    await ElMessageBox.confirm(
      `即将重置分组「${currentGroup.value}」下 ${count} 条已修改的参数，全部恢复为默认值。此操作不可撤销。`,
      `重置整组（${count} 条）`,
      { confirmButtonText: '全部重置', cancelButtonText: '取消', type: 'warning' }
    )
    resettingGroup.value = true
    await configParamsApi.bulkReset(currentGroup.value)
    await loadGroup(currentGroup.value);
    ElMessage.success(`已重置 ${count} 条参数`)
  } catch (e) {
    if (e !== 'cancel') ElMessage.error('整组重置失败：' + (e.message || '未知错误'))
  } finally {
    resettingGroup.value = false
  }
}

const auditDrawerVisible = ref(false);
const auditLoading = ref(false)
const auditList = ref([])

function openAuditDrawer() {
  auditDrawerVisible.value = true
  loadAuditLogs()
}

async function loadAuditLogs() {
  auditLoading.value = true
  try {
    const res = await configParamsApi.auditLogs(80)
    auditList.value = Array.isArray(res) ? res : (res && Array.isArray(res.list) ? res.list : [])
  } catch (e) {
    auditList.value = []
    ElMessage.error('加载审计日志失败')
  } finally {
    auditLoading.value = false
  }
}

function formatTs(ts) {
  if (!ts) return '-'
  const d = new Date(ts)
  if (Number.isNaN(d.getTime())) return String(ts)
  const pad = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}
</script>

<style scoped>
.config-params-page {
  padding: 16px;
}
.stat-row { margin-bottom: 16px; }
.stat-card { border-radius: 10px; }
.stat-inner { display: flex; align-items: center; gap: 14px; }
.stat-icon {
  width: 44px; height: 44px;
  border-radius: 10px;
  display: flex; align-items: center; justify-content: center;
  color: #fff;
}
.stat-icon--total    { background: linear-gradient(135deg, #6366f1, #8b5cf6); }
.stat-icon--group   { background: linear-gradient(135deg, #06b6d4, #0ea5e9); }
.stat-icon--modified{ background: linear-gradient(135deg, #f59e0b, #f97316); }
.stat-icon--readonly{ background: linear-gradient(135deg, #64748b, #475569); }
.stat-body { flex: 1; }
.stat-value { font-size: 24px; font-weight: 600; color: #1f2937; line-height: 1.1; }
.stat-label { font-size: 13px; color: #6b7280; margin-top: 4px; }

.main-row { margin-bottom: 16px; }

.card-header {
  display: flex; justify-content: space-between; align-items: center;
}
.group-card { height: 100%; }
.group-list {
  display: flex; flex-direction: column; gap: 4px;
  max-height: calc(100vh - 260px); overflow-y: auto;
}
.group-item {
  display: flex; align-items: center; justify-content: space-between;
  padding: 9px 10px; border-radius: 8px; cursor: pointer;
  transition: background .15s;
}
.group-item:hover { background: #f3f4f6; }
.group-item.active { background: #eef2ff; color: #4338ca; }
.group-item__main { display: flex; align-items: center; gap: 8px; min-width: 0; }
.group-icon { font-size: 15px; flex-shrink: 0; opacity: .75; }
.group-name { font-size: 13px; font-weight: 500; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.group-item__count { display: flex; gap: 4px; flex-shrink: 0; }
.group-count {
  min-width: 22px; text-align: center; padding: 0 6px;
  background: #f3f4f6; border-radius: 10px; font-size: 11px; color: #6b7280; line-height: 18px;
}
.group-item.active .group-count { background: #e0e7ff; color: #4338ca; }
.group-mod {
  min-width: 18px; text-align: center; padding: 0 5px;
  background: #fef3c7; color: #b45309; border-radius: 10px; font-size: 11px; line-height: 18px;
}

.table-card .card-header { gap: 12px; flex-wrap: wrap; }
.header-title { display: flex; align-items: center; gap: 10px; }
.group-tag { font-weight: 600; }
.header-count { font-size: 13px; color: #6b7280; }
.header-actions { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.reset-badge {
  display: inline-block; margin-left: 4px; padding: 0 6px;
  background: #f59e0b; color: #fff; border-radius: 10px; font-size: 11px; line-height: 16px;
}

.param-key {
  background: #f3f4f6; padding: 2px 6px; border-radius: 4px;
  font-size: 12px; color: #4338ca;
}

.val-cell { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.val-current { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 13px; color: #1f2937; }
.val-changed { color: #b45309; font-weight: 500; }
.val-default { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 13px; color: #6b7280; }
.changed-badge { margin-left: 4px; }
.range-cell { font-size: 12px; color: #374151; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.muted { color: #9ca3af; font-size: 12px; }

/* 编辑对话框 */
.edit-body { display: flex; flex-direction: column; gap: 16px; }
.edit-desc { margin-bottom: 4px; }
.edit-field { display: flex; flex-direction: column; gap: 8px; }
.edit-field__label { font-size: 13px; color: #374151; font-weight: 600; }
.slider-wrap { display: flex; align-items: center; }
.unit-label { font-size: 12px; color: #6b7280; margin-left: 8px; }
.edit-error {
  display: flex; align-items: center; gap: 6px;
  color: #dc2626; font-size: 13px; background: #fef2f2;
  border: 1px solid #fecaca; padding: 8px 10px; border-radius: 6px;
}

/* 审计抽屉 */
.drawer-loading, .drawer-empty {
  display: flex; justify-content: center; align-items: center;
  min-height: 120px; color: #6b7280;
}
.audit-timeline { padding: 4px 0; }
.audit-item { padding: 2px 0 10px; }
.audit-item__head { display: flex; align-items: center; gap: 8px; margin-bottom: 4px; flex-wrap: wrap; }
.audit-key {
  background: #f3f4f6; padding: 1px 6px; border-radius: 4px;
  font-size: 12px; color: #4338ca;
}
.audit-user { font-size: 12px; color: #6b7280; margin-left: auto; }
.audit-item__diff {
  display: flex; align-items: center; gap: 10px; flex-wrap: wrap;
  background: #fafafa; border-radius: 6px; padding: 8px 10px;
}
.audit-arrow { color: #9ca3af; font-size: 16px; }

/* 响应式 */
@media (max-width: 768px) {
  .config-params-page { padding: 10px; }
  .header-actions { width: 100%; justify-content: flex-start; }
}
</style>
