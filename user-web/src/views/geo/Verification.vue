<template>
  <div class="geo-page">

    
    <el-card shadow="never" class="form-card">
      <template #header><span class="card-title">验证配置</span></template>
      <el-form :model="form" label-width="92px">
        <el-row :gutter="16">
          <el-col :xs="24" :sm="12">
            <el-form-item label="验证文章">
              <el-select
                v-model="form.article_id"
                placeholder="选择已生成文章（或粘贴内容）"
                filterable
                clearable
                style="width: 100%"
                @change="onArticleChange"
              >
                <el-option
                  v-for="a in articles"
                  :key="a.id"
                  :label="a.title || a.keyword"
                  :value="a.id"
                />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12">
            <el-form-item label="品牌名称">
              <el-input v-model="form.brand_name" placeholder="如：hivemtk" clearable />
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12">
            <el-form-item label="验证模型">
              <el-select
                v-model="form.models"
                multiple
                collapse-tags
                collapse-tags-tooltip
                placeholder="选择参与验证的 LLM"
                style="width: 100%"
              >
                <el-option
                  v-for="m in modelOptions"
                  :key="m.value"
                  :label="m.label"
                  :value="m.value"
                />
              </el-select>
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item label="测试问题">
          <el-input
            v-model="form.queries"
            type="textarea"
            :rows="4"
            placeholder="每行一个问题，可粘贴关键词"
          />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" :loading="verifying" @click="handleVerify">
            <el-icon><CircleCheck /></el-icon><span>验证</span>
          </el-button>
        </el-form-item>
      </el-form>
    </el-card>

    
    <el-card v-if="results.length" shadow="never" class="result-card">
      <template #header><span class="card-title">验证结果</span></template>
      <el-table v-loading="verifying" :data="results" stripe style="width: 100%">
        <el-table-column prop="model" label="验证模型" width="140" />
        <el-table-column prop="query" label="问题" min-width="200" show-overflow-tooltip />
        <el-table-column prop="brand_name" label="品牌" width="120" />
        <el-table-column prop="brand_mentioned" label="品牌提及" width="100" align="center">
          <template #default="{ row }">
            <el-tag :type="row.brand_mentioned ? 'success' : 'info'" size="small">
              {{ row.brand_mentioned ? '是' : '否' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="mention_count" label="提及次数" width="100" align="right" />
        <el-table-column prop="sentiment" label="情感" width="90">
          <template #default="{ row }">
            <el-tag size="small" :type="sentimentType(row.sentiment)">{{ row.sentiment || '—' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="position" label="提及位置" width="130" />
      </el-table>
    </el-card>

    
    <el-card shadow="never" class="negative-card">
      <template #header>
        <div class="card-header">
          <span class="card-title">负面监控</span>
          <el-tooltip :content="form.brand_name.trim() ? '' : '请先填写品牌名称'" :disabled="!!form.brand_name.trim()" placement="top">
            <el-button type="warning" :loading="monitoring" :disabled="!form.brand_name.trim()" @click="handleMonitorNegative">
              <el-icon><Warning /></el-icon><span>检测负面</span>
            </el-button>
          </el-tooltip>
        </div>
      </template>
      <el-empty v-if="!negativeResults.length" description="暂无负面监控数据，点击「检测负面」生成" :image-size="60" />
      <el-table v-else :data="negativeResults" stripe style="width: 100%">
        <el-table-column prop="query" label="负面查询" min-width="200" show-overflow-tooltip />
        <el-table-column prop="mention_count" label="提及次数" width="100" align="right" />
        <el-table-column prop="negative_score" label="负面得分" width="100" align="right" />
        <el-table-column prop="risk_level" label="风险等级" width="100">
          <template #default="{ row }">
            <el-tag size="small" :type="riskType(row.risk_level)">{{ row.risk_level || '低' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="risk_description" label="风险说明" min-width="220" show-overflow-tooltip />
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { CircleCheck, Warning } from '@element-plus/icons-vue'
import { geoApi } from '@/api/geo'
import { http } from '@/utils/request'

const modelOptions = ref([])

const loadModelOptions = async () => {
  try {
    const res = await http.get('/api/llm/models')
    const list = Array.isArray(res) ? res : (res?.list || res?.items || res?.data || [])
    const options = list
      .filter((m) => {
        if (!m || m.enabled === false) return false
        if (m.vendor === 'local')
          return false;
        const url = (m.base_url || m.baseURL || '').toLowerCase()
        if (url.includes('127.0.0.1') || url.includes('localhost') || url.includes('0.0.0.0')) return false
        return true
      })
      .map((m) => ({
        label: `${m.display_name || m.name} (${m.model || m.name})`,
        value: m.name
      }))
    modelOptions.value = options
    if (options.length && (!form.models || !form.models.length)) {
      form.models = [options[0].value]
    }
  } catch (e) {
    console.warn('加载可用模型列表失败:', e.message)
  }
}

const form = reactive({
  article_id: '',
  brand_name: '',
  models: [],
  queries: ''
})

const articles = ref([])
const verifying = ref(false)
const monitoring = ref(false)
const results = ref([])
const negativeResults = ref([])

const sentimentType = (s) => {
  const map = { 正面: 'success', positive: 'success', 中性: 'info', neutral: 'info', 负面: 'danger', negative: 'danger' }
  return map[s] || 'info'
}
const riskType = (r) => {
  const map = { 高: 'danger', 中: 'warning', 低: 'success' }
  return map[r] || 'success'
}

const loadArticles = async () => {
  try {
    const res = await geoApi.getArticleList({ page: 1, limit: 100 })
    articles.value = res?.list || res?.items || res || []
  } catch (e) {
    articles.value = []
  }
}

const onArticleChange = async (id) => {
  if (!id) return
  try {
    await geoApi.getArticleByID(id)
  } catch (e) {
    ElMessage.error(e.message || '文章加载失败')
  }
}

const handleVerify = async () => {
  if (!form.queries.trim()) {
    ElMessage.warning('请输入测试问题')
    return
  }
  if (!form.brand_name.trim()) {
    ElMessage.warning('请填写品牌名称')
    return
  }
  verifying.value = true
  results.value = []
  try {
    const queries = form.queries.split('\n').map((s) => s.trim()).filter(Boolean);
    for (const q of queries) {
      const res = await geoApi.verifyArticle({
        article_id: form.article_id || undefined,
        query: q,
        brand_name: form.brand_name,
        models: form.models
      })
      if (res) results.value.push(res)
    }
    ElMessage.success('验证完成')
  } catch (e) {
    ElMessage.error(e.message || '验证失败')
  } finally {
    verifying.value = false
  }
}

const handleMonitorNegative = async () => {
  if (!form.brand_name.trim()) {
    ElMessage.warning('请先填写品牌名称')
    return
  }
  monitoring.value = true
  try {
    const res = await geoApi.monitorNegative({
      brand_name: form.brand_name
    })
    negativeResults.value = res?.queries || []
    ElMessage.success('负面检测完成')
  } catch (e) {
    ElMessage.error(e.message || '负面检测失败')
  } finally {
    monitoring.value = false
  }
}

onMounted(async () => {
  await loadModelOptions()
  await loadArticles()
})
</script>

<style lang="scss" scoped>
.geo-page {
  padding: $spacing-lg 24px;
}
.page-header h2 {
  margin: 0 0 6px;
  font-size: $font-size-extra-large;
  font-weight: 700;
  color: $text-primary;
}
.page-header .sub {
  margin: 0 0 16px;
  color: $info-color;
  font-size: $font-size-small;
}
.form-card,
.result-card,
.negative-card {
  border: 1px solid $border-base;
  border-radius: 10px;
  margin-bottom: $spacing-md;
}
.card-title {
  font-weight: 600;
  color: $text-primary;
}
.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
</style>
