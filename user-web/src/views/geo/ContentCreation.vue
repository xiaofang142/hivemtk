<template>
  <div class="geo-page">
    <el-row :gutter="16">
      <el-col
        :xs="24"
        :lg="10"
      >
        <el-card
          shadow="never"
          class="form-card"
        >
          <template #header>
            <span class="card-title">生成配置</span>
          </template>
          <el-form
            :model="form"
            label-width="92px"
            class="gen-form"
          >
            <el-form-item label="关键词">
              <el-input
                v-model="form.keyword"
                placeholder="如：中小企业 CRM 推荐"
                clearable
              />
            </el-form-item>
            <el-form-item label="品牌名称">
              <el-input
                v-model="form.brand_name"
                placeholder="如：hivemtk"
                clearable
              />
            </el-form-item>
            <el-form-item label="核心优势">
              <div class="tags-input">
                <el-tag
                  v-for="(tag, i) in form.advantages"
                  :key="i"
                  closable
                  class="adv-tag"
                  @close="form.advantages.splice(i, 1)"
                >
                  {{ tag }}
                </el-tag>
                <el-input
                  v-if="advInputVisible"
                  ref="advInputRef"
                  v-model="advInputValue"
                  size="small"
                  class="adv-input"
                  @keyup.enter="addAdv"
                  @blur="onBlurAddAdv"
                />
                <el-button
                  v-else
                  size="small"
                  class="adv-add"
                  @click="showAdvInput"
                >
                  + 优势
                </el-button>
              </div>
            </el-form-item>
            <el-form-item label="生成模型">
              <el-select
                v-model="form.model"
                style="width: 100%"
              >
                <el-option
                  v-for="m in models"
                  :key="m.value"
                  :label="m.label"
                  :value="m.value"
                />
              </el-select>
            </el-form-item>
            <el-form-item label="字数">
              <el-input-number
                v-model="form.word_count"
                :min="200"
                :max="3000"
                :step="100"
                style="width: 100%"
              />
            </el-form-item>
            <el-form-item label="文风">
              <el-select
                v-model="form.style"
                style="width: 100%"
              >
                <el-option
                  label="专业严谨"
                  value="professional"
                />
                <el-option
                  label="通俗易懂"
                  value="popular"
                />
                <el-option
                  label="口语化"
                  value="casual"
                />
              </el-select>
            </el-form-item>
            <el-form-item>
              <el-button
                type="primary"
                :loading="generating"
                @click="handleGenerate"
              >
                <el-icon><EditPen /></el-icon>
                <span>生成内容</span>
              </el-button>
            </el-form-item>

            <!-- B2 批量生成入口 -->
            <el-divider content-position="left">
              批量生成
            </el-divider>
            <el-form-item label="批量词">
              <el-input
                v-model="batchKeywordsText"
                type="textarea"
                :rows="4"
                placeholder="每行一个关键词（最多 20 个），与上方共用品牌/优势/文风配置"
              />
            </el-form-item>
            <el-form-item>
              <el-button
                type="warning"
                plain
                :loading="batchGenerating"
                :disabled="batchKeywordCount === 0 || batchKeywordCount > 20"
                @click="handleBatchGenerate"
              >
                批量生成（{{ batchKeywordCount }}/20）
              </el-button>
            </el-form-item>
            <!-- 批量进度与逐条结果：成功可查看，失败可单条重试 -->
            <el-form-item v-if="batchItems.length">
              <div class="batch-progress">
                <div class="batch-summary">
                  成功 {{ batchSucceeded }} / 失败 {{ batchFailed }}
                  <span v-if="batchTruncated">（超上限，已截断到前 20 条）</span>
                </div>
                <div
                  v-for="(it, i) in batchItems"
                  :key="i"
                  class="batch-item"
                  :class="{ ok: it.ok, fail: !it.ok }"
                >
                  <span class="kw">{{ it.keyword }}</span>
                  <el-tag
                    v-if="it.ok"
                    type="success"
                    size="small"
                  >
                    成功
                  </el-tag>
                  <template v-else>
                    <el-tooltip :content="it.error">
                      <el-tag
                        type="danger"
                        size="small"
                      >
                        失败
                      </el-tag>
                    </el-tooltip>
                    <el-button
                      size="small"
                      text
                      type="primary"
                      :disabled="batchGenerating"
                      @click="retryBatchItem(i)"
                    >
                      重试
                    </el-button>
                  </template>
                </div>
              </div>
            </el-form-item>
          </el-form>
        </el-card>
      </el-col>

      
      <el-col
        :xs="24"
        :lg="14"
      >
        <el-card
          shadow="never"
          class="result-card"
        >
          <template #header>
            <div class="card-header">
              <span class="card-title">生成结果</span>
              <el-button
                v-if="generatedContent"
                text
                type="primary"
                @click="copyContent"
              >
                <el-icon><DocumentCopy /></el-icon><span>复制</span>
              </el-button>
            </div>
          </template>

          <div
            v-loading="generating"
            class="result-body"
          >
            <el-empty
              v-if="!generatedContent && !generating"
              description="尚未生成内容"
            />
            <div
              v-else
              class="content-text"
            >
              {{ generatedContent }}
            </div>
          </div>

          <div
            v-if="generatedContent"
            class="score-section"
          >
            <div class="score-actions">
              <el-button
                type="success"
                :loading="scoring"
                @click="handleScore"
              >
                <el-icon><DataAnalysis /></el-icon><span>内容评分</span>
              </el-button>
            </div>

            <el-card
              v-if="score"
              shadow="never"
              class="score-card"
            >
              <div class="score-grid">
                <div class="score-item total">
                  <div class="score-value">
                    {{ score.scores?.total ?? '—' }}
                  </div>
                  <div class="score-label">
                    总分
                  </div>
                </div>
                <div class="score-item">
                  <div class="score-value">
                    {{ score.scores?.structure ?? '—' }}
                  </div>
                  <div class="score-label">
                    结构化
                  </div>
                </div>
                <div class="score-item">
                  <div class="score-value">
                    {{ score.scores?.brand_mention ?? '—' }}
                  </div>
                  <div class="score-label">
                    品牌提及
                  </div>
                </div>
                <div class="score-item">
                  <div class="score-value">
                    {{ score.scores?.authority ?? '—' }}
                  </div>
                  <div class="score-label">
                    权威性
                  </div>
                </div>
                <div class="score-item">
                  <div class="score-value">
                    {{ score.scores?.citations ?? '—' }}
                  </div>
                  <div class="score-label">
                    可引用性
                  </div>
                </div>
              </div>
              <div
                v-if="score.improvements?.length"
                class="score-suggest"
              >
                <div class="suggest-title">
                  优化建议
                </div>
                <ul>
                  <li
                    v-for="(s, i) in score.improvements"
                    :key="i"
                  >
                    {{ s }}
                  </li>
                </ul>
              </div>
            </el-card>
          </div>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup>
import { ref, reactive, computed, nextTick, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { EditPen, DocumentCopy, DataAnalysis } from '@element-plus/icons-vue'
import { geoApi } from '@/api/geo'
import { http } from '@/utils/request'
import { copyToClipboard } from '@/utils/clipboard'
import { isUserReportedError } from '@/utils/errorReporting'

const models = ref([])

const form = reactive({
  keyword: '',
  brand_name: '',
  advantages: [],
  model: '',
  word_count: 800,
  style: 'professional'
})

const loadModels = async () => {
  try {
    const list = await http.get('/api/llm/models')
    const cloud = (Array.isArray(list) ? list : list?.list || [])
      .filter(m => {
        const url = (m.base_url || '').toLowerCase()
        const vendor = (m.vendor || '').toLowerCase()
        return m.enabled !== false &&
          vendor !== 'local' &&
          !url.includes('127.0.0.1') &&
          !url.includes('localhost') &&
          !url.includes('0.0.0.0')
      })
      .map(m => ({ label: `${m.name || m.vendor || 'Unknown'}`, value: m.name }))
    models.value = cloud
    if (cloud.length && !form.model) form.model = cloud[0].value
  } catch { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
};
onMounted(loadModels)

const generating = ref(false)
const scoring = ref(false)
const generatedContent = ref('')
const score = ref(null)

// ---- B2 批量生成（2026-10-24）----
// 后端是同步有界批（上限 20），这里解析多行输入、渲染逐条结果；失败单条可重试。
const batchKeywordsText = ref('')
const batchGenerating = ref(false)
const batchItems = ref([])
const batchTruncated = ref(false)
const batchKeywordCount = computed(() =>
  batchKeywordsText.value.split('\n').map(s => s.trim()).filter(Boolean).length
)
const batchSucceeded = computed(() => batchItems.value.filter(i => i.ok).length)
const batchFailed = computed(() => batchItems.value.filter(i => !i.ok).length)

const parseBatchKeywords = () =>
  batchKeywordsText.value.split('\n').map(s => s.trim()).filter(Boolean)

const handleBatchGenerate = async () => {
  const keywords = parseBatchKeywords()
  if (!keywords.length) {
    ElMessage.warning('请填写批量关键词（每行一个）')
    return
  }
  if (!form.brand_name.trim()) {
    ElMessage.warning('请填写品牌名称')
    return
  }
  batchGenerating.value = true
  batchItems.value = []
  batchTruncated.value = false
  try {
    const res = await geoApi.batchGenerateContent({
      keywords,
      brand_name: form.brand_name,
      advantages: form.advantages,
      word_count: form.word_count,
      style: form.style,
      lang: 'zh'
    })
    batchItems.value = res?.items || []
    batchTruncated.value = (res?.truncated_to || 0) > 0
    if (batchFailed.value > 0) {
      ElMessage.warning(`批量生成完成：成功 ${batchSucceeded.value} 条，失败 ${batchFailed.value} 条（可单条重试）`)
    } else {
      ElMessage.success(`批量生成完成：成功 ${batchSucceeded.value} 条`)
    }
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(e.message || '批量生成失败')
  } finally {
    batchGenerating.value = false
  }
}

// 失败单条重试：走单条接口，成功后原位更新
const retryBatchItem = async (idx) => {
  const it = batchItems.value[idx]
  if (!it || it.ok) return
  batchGenerating.value = true
  try {
    const res = await geoApi.generateContent({
      keyword: it.keyword,
      brand_name: form.brand_name,
      advantages: form.advantages,
      word_count: form.word_count,
      style: form.style
    })
    if (res && (res.content || res.article || typeof res === 'string')) {
      batchItems.value[idx] = { keyword: it.keyword, ok: true, article: res }
      ElMessage.success(`「${it.keyword}」重试成功`)
    } else {
      ElMessage.error('重试返回为空')
    }
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(e.message || '重试失败')
  } finally {
    batchGenerating.value = false
  }
}

const advInputVisible = ref(false)
const advInputValue = ref('')
const advInputRef = ref(null)

const showAdvInput = () => {
  advInputVisible.value = true
  nextTick(() => advInputRef.value?.focus?.())
}
const addAdv = () => {
  const v = advInputValue.value.trim()
  if (v && !form.advantages.includes(v)) form.advantages.push(v)
  advInputVisible.value = false
  advInputValue.value = ''
}

const onBlurAddAdv = () => setTimeout(addAdv, 100);

const handleGenerate = async () => {
  if (!form.keyword.trim() || !form.brand_name.trim()) {
    ElMessage.warning('请填写关键词与品牌名称')
    return
  }
  generating.value = true
  score.value = null
  generatedContent.value = ''
  try {
    const res = await geoApi.generateContent({
      keyword: form.keyword,
      brand_name: form.brand_name,
      advantages: form.advantages,
      model: form.model,
      word_count: form.word_count,
      style: form.style
    })
    generatedContent.value = res?.content || res?.article || (typeof res === 'string' ? res : '')
    ElMessage.success('内容生成完成')
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(e.message || '内容生成失败')
  } finally {
    generating.value = false
  }
}

const handleScore = async () => {
  if (!generatedContent.value) return
  scoring.value = true
  try {
    const res = await geoApi.scoreContent({
      content: generatedContent.value,
      brand_name: form.brand_name,
      keyword: form.keyword
    })
    score.value = res || null
    ElMessage.success('评分完成')
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(e.message || '内容评分失败')
  } finally {
    scoring.value = false
  }
}

const copyContent = async () => {
  if (await copyToClipboard(generatedContent.value)) {
    ElMessage.success('已复制到剪贴板')
  } else {
    ElMessage.error('复制失败，请手动选择文本复制')
  }
}
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
.score-card {
  border: 1px solid $border-base;
  border-radius: 10px;
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
.tags-input {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: $spacing-sm;
}
.adv-tag {
  margin: 0;
}
.adv-input {
  width: 120px;
}
.result-body {
  min-height: 240px;
}
.content-text {
  white-space: pre-wrap;
  word-break: break-word;
  line-height: 1.8;
  color: $text-primary;
  font-size: $font-size-base;
}
.score-section {
  margin-top: $spacing-md;
  border-top: 1px dashed $border-base;
  padding-top: $spacing-md;
}
.score-actions {
  margin-bottom: $spacing-md;
}
.score-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: $spacing-md;
}
.score-item {
  text-align: center;
  padding: $spacing-md 8px;
  border-radius: 8px;
  background: $bg-color-page;
}
.score-item.total {
  background: linear-gradient(135deg, $primary-color, $primary-color);
  color: $bg-color;
}
.score-value {
  font-size: $font-size-extra-large;
  font-weight: 700;
}
.score-label {
  font-size: $font-size-extra-small;
  margin-top: $spacing-xs;
  opacity: 0.85;
}
.score-suggest {
  margin-top: $spacing-md;
}
.suggest-title {
  font-weight: 600;
  margin-bottom: $spacing-sm;
  color: $text-primary;
}
.score-suggest ul {
  margin: 0;
  padding-left: $spacing-lg;
  color: $text-regular;
  font-size: $font-size-small;
  line-height: 1.8;
}
@media (max-width: 768px) {
  .score-grid {
    grid-template-columns: repeat(2, 1fr);
  }
}

/* B2 批量生成进度区 */
.batch-progress {
  width: 100%;
}
.batch-summary {
  margin-bottom: 6px;
  font-size: 13px;
  color: var(--el-text-color-secondary);
}
.batch-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 0;
  border-bottom: 1px dashed var(--el-border-color-lighter);
  font-size: 13px;
  .kw {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
}
</style>
