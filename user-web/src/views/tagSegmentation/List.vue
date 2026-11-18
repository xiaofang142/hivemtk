<template>
  <div class="tag-segmentation-page">
    <el-card class="header-card">
      <div class="header-content">
        <h2>{{ $t('标签分层') }}</h2>
        <p class="subtitle">
          管理用户标签、自动标签规则、RFM 分层策略与标签统计
        </p>
      </div>
      <div class="header-actions">
        <el-button
          type="primary"
          @click="showTagDialog()"
        >
          <el-icon><Plus /></el-icon>
          {{ $t('新增标签') }}
        </el-button>
        <el-button @click="refreshAll">
          <el-icon><Refresh /></el-icon>
          {{ $t('刷新') }}
        </el-button>
      </div>
    </el-card>

    <el-tabs
      v-model="activeTab"
      class="content-tabs"
    >
      <el-tab-pane
        :label="$t('标签列表')"
        name="tags"
      >
        <div class="filter-bar">
          <el-input
            v-model="tagSearch"
            :placeholder="$t('搜索标签名称')"
            clearable
            style="width: 220px"
          />
          <el-select
            v-model="tagTypeFilter"
            :placeholder="$t('标签类型')"
            clearable
            style="width: 160px"
          >
            <el-option
              :label="$t('手动')"
              value="manual"
            />
            <el-option
              :label="$t('自动')"
              value="auto"
            />
            <el-option
              :label="$t('系统')"
              value="system"
            />
          </el-select>
        </div>
        <el-table
          v-loading="loading.tags"
          :data="filteredTags"
          stripe
        >
          <template #empty>
            <el-empty description="暂无标签数据" />
          </template>
          <el-table-column
            prop="name"
            label="标签名称"
            min-width="180"
            show-overflow-tooltip
          />
          <el-table-column
            label="类型"
            min-width="90"
          >
            <template #default="{ row }">
              <el-tag
                :type="getTypeColor(row.tag_type || row.type || 'manual')"
                size="small"
              >
                {{ getTypeText(row.tag_type || row.type || 'manual') }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column
            label="分类"
            min-width="120"
            show-overflow-tooltip
          >
            <template #default="{ row }">
              {{ row.group || row.category || '默认' }}
            </template>
          </el-table-column>
          <el-table-column
            label="用户数"
            min-width="90"
            align="center"
          >
            <template #default="{ row }">
              {{ row.user_count ?? row.userCount ?? 0 }}
            </template>
          </el-table-column>
          <el-table-column
            label="状态"
            min-width="90"
            align="center"
          >
            <template #default="{ row }">
              <el-tag
                :type="row.is_active ? 'success' : 'info'"
                size="small"
              >
                {{ row.is_active ? '启用' : '禁用' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column
            label="创建时间"
            min-width="160"
            show-overflow-tooltip
          >
            <template #default="{ row }">
              {{ formatTime(row.created_at || row.createdAt) }}
            </template>
          </el-table-column>
          <el-table-column
            label="操作"
            min-width="160"
            fixed="right"
          >
            <template #default="{ row }">
              <el-button
                link
                type="primary"
                size="small"
                @click="showTagDialog(row)"
              >
                编辑
              </el-button>
              <el-button
                link
                type="danger"
                size="small"
                @click="deleteTag(row)"
              >
                删除
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>

      <el-tab-pane
        label="自动标签规则"
        name="rules"
      >
        <el-card>
          <template #header>
            <div class="card-header">
              <span>条件 → 标签 自动规则</span>
              <el-button
                type="primary"
                size="small"
                @click="showRuleDialog()"
              >
                <el-icon><Plus /></el-icon> 新增规则
              </el-button>
            </div>
          </template>
          <el-table
            v-loading="loading.rules"
            :data="tagRuleList"
            stripe
          >
            <template #empty>
              <el-empty description="暂无规则" />
            </template>
            <el-table-column
              prop="name"
              label="规则名称"
              min-width="150"
            />
            <el-table-column
              prop="condition"
              label="触发条件"
              min-width="260"
              show-overflow-tooltip
            />
            <el-table-column
              prop="tagName"
              label="打标签"
              width="140"
            >
              <template #default="{ row }">
                <el-tag size="small">
                  {{ row.tagName }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              prop="status"
              label="状态"
              width="100"
            >
              <template #default="{ row }">
                <el-tag
                  :type="getStatusTagType(row.status)"
                  size="small"
                >
                  {{ getStatusLabel(row.status) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              prop="matchedCount"
              label="命中数"
              width="100"
              align="center"
            />
            <el-table-column
              label="操作"
              width="180"
              fixed="right"
            >
              <template #default="{ row }">
                <el-button
                  link
                  type="primary"
                  @click="showRuleDialog(row)"
                >
                  编辑
                </el-button>
                <el-button
                  link
                  type="danger"
                  @click="deleteRule(row)"
                >
                  删除
                </el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-tab-pane>

      <el-tab-pane
        label="分层策略"
        name="strategy"
      >
        <el-card v-loading="loading.strategy">
          <template #header>
            <div class="card-header">
              <span>RFM 分层 → 标签组映射</span>
              <el-button
                type="primary"
                size="small"
                @click="saveStrategy"
              >
                保存策略
              </el-button>
            </div>
          </template>
          <el-table
            :data="strategyList"
            stripe
          >
            <template #empty>
              <el-empty description="暂无分层策略" />
            </template>
            <el-table-column
              prop="layer"
              label="RFM 分层"
              width="160"
            >
              <template #default="{ row }">
                <el-tag :type="getLayerColor(row.layer)">
                  {{ row.layer }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              prop="description"
              label="分层说明"
              min-width="200"
            />
            <el-table-column
              label="绑定标签组"
              min-width="280"
            >
              <template #default="{ row }">
                <el-select
                  v-model="row.tagIds"
                  multiple
                  style="width: 100%"
                  placeholder="选择标签"
                >
                  <el-option
                    v-for="t in tags"
                    :key="t.id"
                    :label="t.name"
                    :value="t.id"
                  />
                </el-select>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-tab-pane>

      <el-tab-pane
        label="标签统计"
        name="stats"
      >
        <el-row
          :gutter="20"
          class="stat-row"
        >
          <el-col :span="6">
            <el-card class="stat-card">
              <div class="stat-label">
                标签总数
              </div>
              <div class="stat-value">
                {{ stats.total || 0 }}
              </div>
            </el-card>
          </el-col>
          <el-col :span="6">
            <el-card class="stat-card">
              <div class="stat-label">
                自动标签
              </div>
              <div
                class="stat-value"
                style="color: #4F46E5"
              >
                {{ stats.auto || 0 }}
              </div>
            </el-card>
          </el-col>
          <el-col :span="6">
            <el-card class="stat-card">
              <div class="stat-label">
                手动标签
              </div>
              <div
                class="stat-value"
                style="color: #10B981"
              >
                {{ stats.manual || 0 }}
              </div>
            </el-card>
          </el-col>
          <el-col :span="6">
            <el-card class="stat-card">
              <div class="stat-label">
                已命中用户
              </div>
              <div
                class="stat-value"
                style="color: #F59E0B"
              >
                {{ stats.matchedUsers || 0 }}
              </div>
            </el-card>
          </el-col>
        </el-row>
        <el-card>
          <template #header>
            <span>标签使用 Top 10</span>
          </template>
          <el-table
            v-loading="loading.stats"
            :data="stats.topUsed"
            stripe
          >
            <template #empty>
              <el-empty description="暂无统计数据" />
            </template>
            <el-table-column
              type="index"
              label="排名"
              width="80"
            />
            <el-table-column
              prop="name"
              label="标签"
              min-width="160"
            />
            <el-table-column
              prop="userCount"
              label="用户数"
              width="120"
              align="center"
            />
            <el-table-column
              prop="ratio"
              label="占比"
              width="200"
            >
              <template #default="{ row }">
                <el-progress
                  :percentage="Number(row.ratio || 0)"
                  :stroke-width="8"
                />
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-tab-pane>
    </el-tabs>

    <el-dialog
      v-model="tagDialogVisible"
      :title="tagDialogTitle"
      width="560px"
    >
      <el-form
        ref="tagFormRef"
        :model="tagForm"
        :rules="tagFormRules"
        label-width="100px"
      >
        <el-form-item
          label="标签名称"
          prop="name"
        >
          <el-input
            v-model="tagForm.name"
            placeholder="请输入标签名称"
          />
        </el-form-item>
        <el-form-item
          label="标签类型"
          prop="type"
        >
          <el-select
            v-model="tagForm.type"
            style="width: 100%"
          >
            <el-option
              label="手动"
              value="manual"
            />
            <el-option
              label="自动"
              value="auto"
            />
            <el-option
              label="系统"
              value="system"
            />
          </el-select>
        </el-form-item>
        <el-form-item
          label="分类"
          prop="category"
        >
          <el-input
            v-model="tagForm.category"
            placeholder="如 价值/行为/偏好"
          />
        </el-form-item>
        <el-form-item label="描述">
          <el-input
            v-model="tagForm.description"
            type="textarea"
            :rows="3"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="tagDialogVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          @click="submitTag"
        >
          确定
        </el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="ruleDialogVisible"
      :title="ruleDialogTitle"
      width="640px"
    >
      <el-form
        ref="ruleFormRef"
        :model="ruleForm"
        :rules="ruleFormRules"
        label-width="100px"
      >
        <el-form-item
          label="规则名称"
          prop="name"
        >
          <el-input
            v-model="ruleForm.name"
            placeholder="请输入规则名称"
          />
        </el-form-item>
        <el-form-item
          label="触发条件"
          prop="condition"
        >
          <el-input
            v-model="ruleForm.condition"
            type="textarea"
            :rows="3"
            placeholder="如: 消费金额>=1000 AND 最近30天有下单"
          />
        </el-form-item>
        <el-form-item
          label="打标签"
          prop="tagId"
        >
          <el-select
            v-model="ruleForm.tagId"
            filterable
            placeholder="选择标签"
            style="width: 100%"
          >
            <el-option
              v-for="t in tags"
              :key="t.id"
              :label="t.name"
              :value="t.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="状态">
          <el-switch
            v-model="ruleForm.status"
            active-value="enabled"
            inactive-value="disabled"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="ruleDialogVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          @click="submitRule"
        >
          确定
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, computed, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Refresh } from '@element-plus/icons-vue'
import { TagSegmentationApi } from '@/api/tagSegmentation.js'
import { getEnabledLabel, getEnabledTagType } from '@/constants/enabled';

const getStatusLabel = (s) => getEnabledLabel(s)
const getStatusTagType = (s) => getEnabledTagType(s)

const activeTab = ref('tags')
const loading = reactive({ tags: false, rules: false, strategy: false, stats: false })

const tags = ref([])
const tagRuleList = ref([])
const strategyList = ref([])
const stats = ref({ topUsed: [] })

const tagSearch = ref('')
const tagTypeFilter = ref('')

const tagDialogVisible = ref(false)
const tagDialogTitle = ref('新增标签')
const tagFormRef = ref()
const tagForm = ref({ id: 0, name: '', type: 'manual', category: '', description: '' })
const tagFormRules = {
  name: [{ required: true, message: i18n.global.t('请输入标签名称'), trigger: 'blur' }],
  type: [{ required: true, message: i18n.global.t('请选择标签类型'), trigger: 'change' }]
}

const ruleDialogVisible = ref(false)
const ruleDialogTitle = ref('新增规则')
const ruleFormRef = ref()
const ruleForm = ref({ id: 0, name: '', condition: '', tagId: '', status: 'enabled' })
const ruleFormRules = {
  name: [{ required: true, message: i18n.global.t('请输入规则名称'), trigger: 'blur' }],
  condition: [{ required: true, message: i18n.global.t('请输入触发条件'), trigger: 'blur' }],
  tagId: [{ required: true, message: i18n.global.t('请选择标签'), trigger: 'change' }]
}

const filteredTags = computed(() => {
  return tags.value.filter(t => {
    const matchName = !tagSearch.value || t.name?.includes(tagSearch.value)
    const matchType = !tagTypeFilter.value || t.type === tagTypeFilter.value
    return matchName && matchType
  })
})

const getTypeColor = (type) => {
  const map = { manual: 'primary', auto: 'success', system: 'info' }
  return map[type]}
const getTypeText = (type) => {
  const map = { manual: '手动', auto: '自动', system: '系统' }
  return map[type] || type
}
const getLayerColor = (layer) => {
  if (!layer || typeof layer !== 'string') return 'primary'
  if (layer.includes('高价值') || layer.includes('重要')) return 'success'
  if (layer.includes('流失')) return 'danger'
  if (layer.includes('保持')) return 'warning'
  return 'primary'
}

const loadTags = async () => {
  loading.tags = true
  try {
    const res = await TagSegmentationApi.getTags()
    const data = res
    tags.value = Array.isArray(data) ? data : (data?.items || data?.list || [])
  } catch {
    tags.value = []
  } finally {
    loading.tags = false
  }
}

const loadRules = async () => {
  loading.rules = true
  try {
    const res = await TagSegmentationApi.getTagRules()
    const data = res
    tagRuleList.value = Array.isArray(data) ? data : (data?.items || data?.list || [])
  } catch {
    tagRuleList.value = []
  } finally {
    loading.rules = false
  }
}

const loadStrategy = async () => {
  loading.strategy = true
  try {
    const res = await TagSegmentationApi.getLayerStrategy()
    const data = res
    strategyList.value = Array.isArray(data) ? data : (data?.items || data?.list || [])
  } catch {
    strategyList.value = []
  } finally {
    loading.strategy = false
  }
}

const loadStats = async () => {
  loading.stats = true
  try {
    const res= await TagSegmentationApi.getTagStats()
    const d = res || {}
    stats.value = { topUsed: Array.isArray(d.topUsed) ? d.topUsed : (Array.isArray(d) ? d : []) }
  } catch {
    stats.value = {}
  } finally {
    loading.stats = false
  }
}

const refreshAll = () => {
  loadTags()
  loadRules()
  loadStrategy()
  loadStats()
}

const showTagDialog = (row) => {
  if (row) {
    tagForm.value = { ...row }
    tagDialogTitle.value = '编辑标签'
  } else {
    tagForm.value = { id: 0, name: '', code: '', type: 'manual', category: '', description: '' }
    tagDialogTitle.value = '新增标签'
  }
  tagDialogVisible.value = true
}

const formatDate = (date) => {
  if (!date) return ''
  const d = new Date(date)
  const year = d.getFullYear()
  const month = (d.getMonth() + 1).toString().padStart(2, '0')
  const day = d.getDate().toString().padStart(2, '0')
  return `${year}-${month}-${day}`
}

const formatTime = (timeStr) => {
  if (!timeStr) return ''
  const date = new Date(timeStr)
  const now = new Date()
  const diff = now.getTime() - date.getTime()
  const days = Math.floor(diff / (1000 * 60 * 60 * 24))

  if (days === 0) {
    const hours = Math.floor(diff / (1000 * 60 * 60))
    if (hours === 0) {
      const minutes = Math.floor(diff / (1000 * 60))
      return minutes <= 1 ? '刚刚' : `${minutes}分钟前`
    }
    return `${hours}小时前`
  } else if (days === 1) {
    return '昨天'
  } else if (days < 7) {
    return `${days}天前`
  } else {
    return formatDate(date)
  }
}

const genTagCode = (name) => {
  const base = (name || '').toString().trim()
    .toLowerCase()
    .replace(/[^a-z0-9\u4e00-\u9fa5]+/g, '_')
    .replace(/^_+|_+$/g, '')
  return base ? `tag_${base}` : `tag_${Date.now()}`
}

const submitTag = async () => {
  if (!tagFormRef.value) return
  await tagFormRef.value.validate(async (valid) => {
    if (!valid) return
    try {
      const payload = {
        name: tagForm.value.name,
        code: tagForm.value.code || genTagCode(tagForm.value.name),
        group: tagForm.value.category || 'default',
        description: tagForm.value.description || ''
      };
      if (tagForm.value.id) {
        await TagSegmentationApi.updateTags({ id: tagForm.value.id, ...payload })
      } else {
        await TagSegmentationApi.createTag(payload)
      }
      ElMessage.success(tagForm.value.id ? '更新成功' : '新增成功')
      tagDialogVisible.value = false
      loadTags()
    } catch {
      ElMessage.error(i18n.global.t('保存失败'))
    }
  })
}

const deleteTag = async (row) => {
  try {
    await ElMessageBox.confirm(`确定删除标签 "${row.name}" 吗？`, '确认', { type: 'warning' })
    await TagSegmentationApi.deleteTag(row.id)
    ElMessage.success(i18n.global.t('删除成功'))
    loadTags()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(i18n.global.t('删除失败'))
  }
}

const showRuleDialog = (row) => {
  if (row) {
    ruleForm.value = { ...row }
    ruleDialogTitle.value = '编辑规则'
  } else {
    ruleForm.value = { id: 0, name: '', condition: '', tagId: '', status: 'enabled' }
    ruleDialogTitle.value = '新增规则'
  }
  ruleDialogVisible.value = true
}

const submitRule = async () => {
  if (!ruleFormRef.value) return
  await ruleFormRef.value.validate(async (valid) => {
    if (!valid) return
    const tagName = tags.value.find(t => t.id === ruleForm.value.tagId)?.name
    const payload = { ...ruleForm.value, tagName }
    try {
      if (ruleForm.value.id) {
        await TagSegmentationApi.updateTagRule(ruleForm.value.id, payload)
      } else {
        await TagSegmentationApi.saveTagRule(payload)
      }
      ElMessage.success(ruleForm.value.id ? '更新成功' : '新增成功')
      ruleDialogVisible.value = false
      loadRules()
    } catch {
      ElMessage.error(i18n.global.t('保存失败'))
    }
  })
}

const deleteRule = async (row) => {
  try {
    await ElMessageBox.confirm(`确定删除规则 "${row.name}" 吗？`, '确认', { type: 'warning' })
    await TagSegmentationApi.deleteTagRule(row.id)
    ElMessage.success(i18n.global.t('删除成功'))
    loadRules()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(i18n.global.t('删除失败'))
  }
}

const saveStrategy = async () => {
  try {
    const payload = strategyList.value.map(r => ({
      layer: r.layer,
      description: r.description || ''
    }));
    await TagSegmentationApi.saveLayerStrategy(payload)
    ElMessage.success(i18n.global.t('策略已保存'))
  } catch {
    ElMessage.error(i18n.global.t('保存失败'))
  }
}

onMounted(() => {
  refreshAll()
})
</script>

<style scoped lang="scss">
.tag-segmentation-page { padding: 20px; }
.header-card {
  margin-bottom: 20px;
  :deep(.el-card__body) {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  .header-content h2 { margin: 0 0 8px 0; }
  .subtitle { color: #909399; margin: 0; }
  .header-actions { display: flex; gap: 10px; }
}
.content-tabs { background: #fff; padding: 16px; border-radius: 4px; }
.filter-bar { display: flex; gap: 12px; margin-bottom: 16px; }
.card-header { display: flex; justify-content: space-between; align-items: center; }
.stat-row { margin-bottom: 20px; }
.stat-card {
  text-align: center;
  .stat-label { color: #909399; font-size: 14px; margin-bottom: 10px; }
  .stat-value { font-size: 28px; font-weight: bold; }
}
</style>
