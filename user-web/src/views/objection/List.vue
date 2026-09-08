<template>
  <div class="objection-page">
    <el-card class="header-card">
      <div>
        <h2>{{ $t('异议处理中心') }}</h2>
        <p class="subtitle">智能识别客户异议类型 · 匹配最佳应对话术 · 提升转化率</p>
      </div>
      <el-button type="primary" @click="switchToHandle">
        <el-icon><MagicStick /></el-icon>
        {{ $t('智能处理') }}
      </el-button>
    </el-card>

    <el-row :gutter="20" class="stat-row">
      <el-col :span="6">
        <el-card>
          <div class="stat-item">
            <div class="stat-label">{{ $t('异议类别') }}</div>
            <div class="stat-value">{{ categories.length }}</div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card>
          <div class="stat-item">
            <div class="stat-label">{{ $t('已匹配模板') }}</div>
            <div class="stat-value" style="color: #4F46E5">{{ handleResult?.templates?.length || 0 }}</div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card>
          <div class="stat-item">
            <div class="stat-label">{{ $t('置信度') }}</div>
            <div class="stat-value" style="color: #10B981">
              {{ handleResult ? Math.round((handleResult.confidence || 0) * 100) + '%' : '-' }}
            </div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card>
          <div class="stat-item">
            <div class="stat-label">{{ $t('使用记录') }}</div>
            <div class="stat-value" style="color: #F59E0B">{{ usageCount }}</div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-card class="handle-card">
      <template #header>
        <div class="card-header">
          <span>{{ $t('异议输入与分析') }}</span>
          <el-button size="small" @click="loadCategories">{{ $t('刷新分类') }}</el-button>
        </div>
      </template>

      <el-form :model="form" label-width="100px">
        <el-form-item label="客户异议">
          <el-input
            v-model="form.text"
            type="textarea"
            :rows="3"
            placeholder="输入客户的异议内容，例如：太贵了、再考虑一下、其他家更便宜..."
          />
        </el-form-item>
        <el-form-item label="手动类别">
          <el-select
            v-model="form.category"
            placeholder="自动识别（可手动调整）"
            clearable
            style="width: 240px"
          >
            <el-option
              v-for="c in categories"
              :key="c.code"
              :label="c.name"
              :value="c.code"
            />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="handleSubmit" :loading="handling">
            <el-icon><Search /></el-icon>
            智能匹配
          </el-button>
          <el-button @click="handleClassify">仅分类</el-button>
          <el-button @click="resetForm">清空</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <el-card v-if="handleResult" class="result-card">
      <template #header>
        <div class="card-header">
          <span>
            <el-tag :type="categoryColor(handleResult.category)" size="large">
              {{ handleResult.category_name || handleResult.category }}
            </el-tag>
            <span class="confidence-text">
              置信度 {{ Math.round((handleResult.confidence || 0) * 100) }}%
            </span>
          </span>
        </div>
      </template>

      <div v-if="handleResult.suggestion" class="suggestion-box">
        <div class="box-title">
          <el-icon style="color: #4F46E5"><MagicStick /></el-icon>
          智能建议
        </div>
        <div class="box-content">{{ handleResult.suggestion }}</div>
      </div>

      <div v-if="handleResult.templates && handleResult.templates.length > 0" class="templates-box">
        <div class="box-title">
          <el-icon style="color: #10B981"><Document /></el-icon>
          匹配的应对话术 ({{ handleResult.templates.length }})
        </div>
        <el-table :data="handleResult.templates" stripe size="default">
          <el-table-column prop="title" label="标题" min-width="160" />
          <el-table-column label="类别" width="120">
            <template #default="{ row }">
              <el-tag size="small">{{ row.category || handleResult.category }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="content" label="话术内容" min-width="320" show-overflow-tooltip />
          <el-table-column label="成功率" width="120" align="center">
            <template #default="{ row }">
              <el-progress
                :percentage="Math.round((row.success_rate || 0) * 100)"
                :stroke-width="8"
                :color="row.success_rate > 0.7 ? '#10B981' : row.success_rate > 0.4 ? '#F59E0B' : '#EF4444'"
              />
            </template>
          </el-table-column>
          <el-table-column prop="usage_count" label="使用次数" width="100" align="center" />
          <el-table-column label="操作" width="160" fixed="right">
            <template #default="{ row }">
              <el-button link type="primary" @click="copyTemplate(row)">复制</el-button>
              <el-button link type="success" @click="useTemplate(row)">使用</el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>
    </el-card>

    <el-card class="history-card">
      <template #header>
        <div class="card-header">
          <span>本会话使用记录</span>
          <el-button size="small" @click="clearHistory">清空</el-button>
        </div>
      </template>
      <el-table :data="usageHistory" v-loading="loading" stripe>
        <el-table-column prop="time" label="时间" width="160" />
        <el-table-column prop="text" label="异议内容" min-width="240" show-overflow-tooltip />
        <el-table-column prop="category" label="类别" width="120">
          <template #default="{ row }">
            <el-tag size="small" :type="categoryColor(row.category)">
              {{ row.category_name || row.category }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="template_title" label="使用模板" min-width="160" />
        <el-table-column label="结果" width="100" align="center">
          <template #default="{ row }">
            <el-tag v-if="row.success === true" type="success">成功</el-tag>
            <el-tag v-else-if="row.success === false" type="danger">失败</el-tag>
            <el-tag v-else type="info">未记录</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="200" fixed="right">
          <template #default="{ row, $index }">
            <el-button link type="primary" @click="markSuccess(row)">标记成功</el-button>
            <el-button link type="danger" @click="markFailed(row)">标记失败</el-button>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty description="暂无使用记录" />
        </template>
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, reactive, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { MagicStick, Search, Document } from '@element-plus/icons-vue'
import {
  handleObjection,
  classifyObjection,
  listObjectionCategories,
  recordObjectionUsage
} from '@/api/objection.js'

const form = reactive({
  text: '',
  category: ''
})

const handling = ref(false)
const loading = ref(false)
const categories = ref([])
const handleResult = ref(null)
const usageCount = ref(0)
const usageHistory = ref([])

const categoryColor = (cat) => {
  const colorMap = {
    price: 'danger',
    need: 'info',
    trust: 'warning',
    timing: 'primary',
    compare: 'success',
    feature: '',
    other: 'info'
  }
  return colorMap[cat] || 'info'
}

const loadCategories = async () => {
  try {
    const res = await listObjectionCategories()
    const data = res || []
    const raw = Array.isArray(data) ? data : data.list || []
    categories.value = raw.map((c) => ({ code: c.code || c.category, name: c.name }));
    if (categories.value.length === 0) {
      categories.value = [
        { code: 'price', name: '价格异议' },
        { code: 'need', name: '需求异议' },
        { code: 'trust', name: '信任异议' },
        { code: 'timing', name: '时机异议' },
        { code: 'compare', name: '比较异议' },
        { code: 'feature', name: '特性异议' },
        { code: 'other', name: '其他异议' }
      ];
    }
  } catch (e) {
    categories.value = [
      { code: 'price', name: '价格异议' },
      { code: 'need', name: '需求异议' },
      { code: 'trust', name: '信任异议' },
      { code: 'timing', name: '时机异议' },
      { code: 'compare', name: '比较异议' },
      { code: 'feature', name: '特性异议' }
    ]
  }
}

const handleSubmit = async () => {
  if (!form.text.trim()) {
    ElMessage.warning(i18n.global.t('请输入客户异议内容'))
    return
  }
  handling.value = true
  try {
    const res = await handleObjection({
      text: form.text,
      category: form.category
    })
    handleResult.value = res
    ElMessage.success(i18n.global.t('智能匹配完成'))
  } catch (e) {
    ElMessage.error('匹配失败：' + (e?.message || ''))
  } finally {
    handling.value = false
  }
}

const handleClassify = async () => {
  if (!form.text.trim()) {
    ElMessage.warning(i18n.global.t('请输入客户异议内容'))
    return
  }
  try {
    const res = await classifyObjection({ text: form.text })
    const data = res
    form.category = data.category
    ElMessage.success(`分类结果：${data.category_name}`)
  } catch (e) {
    ElMessage.error('分类失败：' + (e?.message || ''))
  }
}

const resetForm = () => {
  form.text = ''
  form.category = ''
  handleResult.value = null
}

const copyTemplate = async (template) => {
  try {
    await navigator.clipboard.writeText(template.content)
    ElMessage.success(i18n.global.t('已复制到剪贴板'))
  } catch (e) {
    const ta = document.createElement('textarea');
    ta.value = template.content
    document.body.appendChild(ta)
    ta.select()
    document.execCommand('copy')
    document.body.removeChild(ta)
    ElMessage.success(i18n.global.t('已复制到剪贴板'))
  }
}

const useTemplate = async (template) => {
  usageCount.value += 1
  usageHistory.value.unshift({
    time: new Date().toLocaleString('zh-CN', { hour12: false }),
    text: form.text,
    category: handleResult.value?.category,
    category_name: handleResult.value?.category_name,
    template_id: template.id,
    template_title: template.title,
    success: null
  })
  await copyTemplate(template)
  ElMessage.success(`已使用模板：${template.title}`)
}

const markSuccess = async (row) => {
  row.success = true
  if (row.template_id) {
    try {
      await recordObjectionUsage({ template_id: row.template_id, success: true })
      ElMessage.success(i18n.global.t('已标记为成功'))
    } catch (e) { console.warn("[request] 后台调用失败(已忽略):", e) }
  }
}

const markFailed = async (row) => {
  row.success = false
  if (row.template_id) {
    try {
      await recordObjectionUsage({ template_id: row.template_id, success: false })
      ElMessage.success(i18n.global.t('已标记为失败'))
    } catch (e) { console.warn("[request] 后台调用失败(已忽略):", e) }
  }
}

const clearHistory = () => {
  usageHistory.value = []
  ElMessage.success(i18n.global.t('已清空记录'))
}

const switchToHandle = () => {
  document.querySelector('.handle-card')?.scrollIntoView({ behavior: 'smooth' });
}

onMounted(() => {
  loadCategories()
})
</script>

<style scoped lang="scss">
.objection-page { padding: 20px; }
.header-card {
  margin-bottom: 20px;
  :deep(.el-card__body) { display: flex; justify-content: space-between; align-items: center; }
  h2 { margin: 0 0 8px 0; }
  .subtitle { color: #909399; margin: 0; }
}
.stat-row {
  margin-bottom: 20px;
  .stat-item {
    text-align: center;
    .stat-label { color: #909399; font-size: 14px; margin-bottom: 10px; }
    .stat-value { font-size: 28px; font-weight: bold; }
  }
}
.card-header { display: flex; justify-content: space-between; align-items: center; }
.handle-card, .result-card, .history-card { margin-bottom: 20px; }
.form-tip { color: #909399; font-size: 12px; margin-left: 10px; }
.confidence-text { color: #909399; font-size: 12px; margin-left: 10px; }
.suggestion-box, .templates-box {
  padding: 15px;
  background: #f5f7fa;
  border-radius: 6px;
  margin-bottom: 15px;
  .box-title {
    font-weight: 600;
    margin-bottom: 10px;
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .box-content { line-height: 1.6; color: #303133; }
}
</style>
