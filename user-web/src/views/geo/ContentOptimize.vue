<template>
  <div class="geo-page">
    <el-card
      shadow="never"
      class="input-card"
    >
      <template #header>
        <span class="card-title">优化输入</span>
      </template>
      <el-form
        :model="form"
        label-width="92px"
      >
        <el-row :gutter="16">
          <el-col
            :xs="24"
            :sm="12"
          >
            <el-form-item label="文章来源">
              <el-radio-group v-model="form.source">
                <el-radio value="textarea">
                  粘贴文本
                </el-radio>
                <el-radio value="article">
                  从历史文章选择
                </el-radio>
              </el-radio-group>
            </el-form-item>
          </el-col>
          <el-col
            v-if="form.source === 'article'"
            :xs="24"
            :sm="12"
          >
            <el-form-item label="历史文章">
              <el-select
                v-model="form.article_id"
                placeholder="选择历史文章"
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
          <el-col
            :xs="24"
            :sm="12"
          >
            <el-form-item label="品牌名称">
              <el-input
                v-model="form.brand_name"
                placeholder="如：hivemtk"
                clearable
              />
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item
          v-if="form.source === 'textarea'"
          label="文章内容"
        >
          <el-input
            v-model="form.content"
            type="textarea"
            :rows="10"
            placeholder="粘贴待优化文章内容"
          />
        </el-form-item>
        <el-form-item>
          <el-button
            type="primary"
            :loading="optimizing"
            @click="handleOptimize"
          >
            <el-icon><MagicStick /></el-icon><span>优化内容</span>
          </el-button>
          <el-button
            :loading="scoringBefore"
            :disabled="!originalContent"
            @click="handleScore('before')"
          >
            优化前评分
          </el-button>
        </el-form-item>
      </el-form>
    </el-card>

    
    <el-card
      v-if="optimizedContent || originalContent"
      shadow="never"
      class="compare-card"
    >
      <template #header>
        <div class="card-header">
          <span class="card-title">优化前后对比</span>
          <div class="score-bar">
            <el-tag type="info">
              优化前：{{ scoreBefore?.total ?? '—' }}
            </el-tag>
            <el-icon class="arrow">
              <Right />
            </el-icon>
            <el-tag type="success">
              优化后：{{ scoreAfter?.total ?? '—' }}
            </el-tag>
          </div>
        </div>
      </template>
      <el-row :gutter="16">
        <el-col
          :xs="24"
          :md="12"
        >
          <div class="compare-col">
            <div class="col-title">
              原文
            </div>
            <div class="compare-text original">
              {{ originalContent || '—' }}
            </div>
          </div>
        </el-col>
        <el-col
          :xs="24"
          :md="12"
        >
          <div class="compare-col">
            <div class="col-title">
              优化后
              <el-button
                v-if="optimizedContent"
                link
                type="primary"
                @click="copyOptimized"
              >
                复制
              </el-button>
            </div>
            <div
              v-loading="optimizing"
              class="compare-text optimized"
            >
              {{ optimizedContent || '—' }}
            </div>
          </div>
        </el-col>
      </el-row>

      <div class="suggest-section">
        <div class="suggest-title">
          优化建议
          <el-button
            v-if="optimizedContent"
            size="small"
            type="success"
            :loading="scoringAfter"
            @click="handleScore('after')"
          >
            优化后评分
          </el-button>
        </div>
        <el-empty
          v-if="!suggestions.length"
          description="暂无优化建议"
          :image-size="60"
        />
        <ul
          v-else
          class="suggest-list"
        >
          <li
            v-for="(s, i) in suggestions"
            :key="i"
          >
            {{ s }}
          </li>
        </ul>
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { MagicStick, Right } from '@element-plus/icons-vue'
import { copyToClipboard } from '@/utils/clipboard'
import { geoApi } from '@/api/geo'
import { isUserReportedError } from '@/utils/errorReporting'

const form = reactive({
  source: 'textarea',
  article_id: '',
  brand_name: '',
  content: ''
})

const articles = ref([])
const optimizing = ref(false)
const scoringBefore = ref(false)
const scoringAfter = ref(false)
const optimizedContent = ref('')
const suggestions = ref([])
const scoreBefore = ref(null)
const scoreAfter = ref(null)

const originalContent = computed(() => form.content)

const loadArticles = async () => {
  try {
    const res = await geoApi.getArticleList({ page: 1, limit: 100 })
    articles.value = res?.list || res?.items || res || []
  } catch (e) {
    articles.value = []
  }
}

const onArticleChange = async (id) => {
  if (!id) {
    form.content = ''
    return
  }
  try {
    const res = await geoApi.getArticleByID(id)
    form.content = res?.content || res?.article || ''
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(e.message || '文章加载失败')
  }
}

const handleOptimize = async () => {
  if (!form.content.trim()) {
    ElMessage.warning('请先输入或选择文章内容')
    return
  }
  optimizing.value = true
  scoreBefore.value = null
  scoreAfter.value = null
  try {
    const res = await geoApi.optimizeContent({
      content: form.content,
      brand_name: form.brand_name,
      article_id: form.article_id || undefined
    })
    optimizedContent.value = res?.optimized_content || res?.content || ''
    const raw = res?.suggestions || '';
    suggestions.value = Array.isArray(raw) ? raw : String(raw).split('\n').map((s) => s.trim()).filter(Boolean)
    ElMessage.success('文章优化完成')
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(e.message || '文章优化失败')
  } finally {
    optimizing.value = false
  }
}

const handleScore = async (stage) => {
  const content = stage === 'before' ? form.content : optimizedContent.value
  if (!content) {
    ElMessage.warning('暂无可评分内容')
    return
  }
  const loading = stage === 'before' ? scoringBefore : scoringAfter
  loading.value = true
  try {
    const res = await geoApi.scoreContent({ content, brand_name: form.brand_name, keyword: '' })
    if (stage === 'before') scoreBefore.value = res
    else scoreAfter.value = res
    ElMessage.success('评分完成')
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(e.message || '评分失败')
  } finally {
    loading.value = false
  }
}

const copyOptimized = async () => {
  if (await copyToClipboard(optimizedContent.value)) {
    ElMessage.success('已复制到剪贴板')
  } else {
    ElMessage.error('复制失败')
  }
}

onMounted(loadArticles)
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
.input-card,
.compare-card {
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
  flex-wrap: wrap;
  gap: $spacing-md;
}
.score-bar {
  display: flex;
  align-items: center;
  gap: $spacing-sm;
}
.score-bar .arrow {
  color: $text-placeholder;
}
.compare-col {
  display: flex;
  flex-direction: column;
}
.col-title {
  font-weight: 600;
  margin-bottom: $spacing-sm;
  color: $text-primary;
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.compare-text {
  white-space: pre-wrap;
  word-break: break-word;
  line-height: 1.8;
  font-size: $font-size-small;
  padding: $spacing-md;
  border-radius: 8px;
  min-height: 200px;
  max-height: 480px;
  overflow-y: auto;
}
.compare-text.original {
  background: $bg-color-page;
  color: $text-regular;
}
.compare-text.optimized {
  background: $primary-light-1;
  color: $text-primary;
  border: 1px solid $primary-light-3;
}
.suggest-section {
  margin-top: $spacing-lg;
  border-top: 1px dashed $border-base;
  padding-top: $spacing-md;
}
.suggest-title {
  font-weight: 600;
  margin-bottom: $spacing-sm;
  color: $text-primary;
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.suggest-list {
  margin: 0;
  padding-left: $spacing-lg;
  color: $text-regular;
  font-size: $font-size-small;
  line-height: 1.9;
}
</style>
