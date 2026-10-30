<template>
  <div class="geo-page">
    <el-row :gutter="16">
      <el-col
        :xs="24"
        :lg="14"
      >
        <el-card
          v-loading="loading"
          shadow="never"
          class="form-card"
        >
          <template #header>
            <div class="card-header">
              <span class="card-title">GEO 配置</span>
              <el-button
                link
                type="primary"
                :loading="saving"
                @click="handleSave"
              >
                <el-icon><Check /></el-icon><span>保存配置</span>
              </el-button>
            </div>
          </template>
          <el-form
            :model="config"
            label-width="100px"
          >
            <el-form-item label="品牌名称">
              <el-input
                v-model="config.brand"
                placeholder="如：hivemtk"
                clearable
              />
            </el-form-item>
            <el-form-item label="品牌描述">
              <el-input
                v-model="config.description"
                type="textarea"
                :rows="3"
                placeholder="品牌简介 / 定位描述"
              />
            </el-form-item>
            <el-form-item label="核心优势">
              <div class="tags-input">
                <el-tag
                  v-for="(tag, i) in config.advantages"
                  :key="i"
                  closable
                  class="tag-item"
                  @close="config.advantages.splice(i, 1)"
                >
                  {{ tag }}
                </el-tag>
                <el-input
                  v-if="advInputVisible"
                  ref="advInputRef"
                  v-model="advInputValue"
                  size="small"
                  class="tag-input"
                  @keyup.enter="addTag(config.advantages)"
                  @blur="onBlurAddTag('adv')"
                />
                <el-button
                  v-else
                  size="small"
                  class="tag-add"
                  @click="showAdvInput"
                >
                  + 优势
                </el-button>
              </div>
            </el-form-item>
            <el-form-item label="竞品">
              <div class="tags-input">
                <el-tag
                  v-for="(tag, i) in config.competitors"
                  :key="i"
                  closable
                  type="warning"
                  class="tag-item"
                  @close="config.competitors.splice(i, 1)"
                >
                  {{ tag }}
                </el-tag>
                <el-input
                  v-if="compInputVisible"
                  ref="compInputRef"
                  v-model="compInputValue"
                  size="small"
                  class="tag-input"
                  @keyup.enter="addTag(config.competitors, 'comp')"
                  @blur="onBlurAddTag('comp')"
                />
                <el-button
                  v-else
                  size="small"
                  class="tag-add"
                  @click="showCompInput"
                >
                  + 竞品
                </el-button>
              </div>
            </el-form-item>
            <el-form-item label="默认生成模型">
              <el-select
                v-model="config.default_model"
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
            <el-form-item label="验证模型">
              <el-select
                v-model="config.verify_models"
                multiple
                collapse-tags
                collapse-tags-tooltip
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
            <el-form-item>
              <el-button
                type="success"
                :loading="optimizing"
                @click="handleOptimize"
              >
                <el-icon><MagicStick /></el-icon><span>优化配置</span>
              </el-button>
            </el-form-item>
          </el-form>
        </el-card>
      </el-col>

      
      <el-col
        :xs="24"
        :lg="10"
      >
        <el-card
          shadow="never"
          class="suggest-card"
        >
          <template #header>
            <span class="card-title">优化建议</span>
          </template>
          <div v-loading="optimizing">
            <el-empty
              v-if="!suggestions.length && !optimizing"
              description="点击「优化配置」生成建议"
              :image-size="60"
            />
            <div
              v-else
              class="suggest-list"
            >
              <div
                v-for="(s, i) in suggestions"
                :key="i"
                class="suggest-item"
              >
                <div class="suggest-item-title">
                  <el-tag
                    size="small"
                    :type="priorityType(s.priority)"
                  >
                    {{ s.priority || '中' }}
                  </el-tag>
                  <span>{{ s.title }}</span>
                </div>
                <div class="suggest-item-desc">
                  {{ s.description }}
                </div>
              </div>
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, nextTick } from 'vue'
import { ElMessage } from 'element-plus'
import { Check, MagicStick } from '@element-plus/icons-vue'
import { geoApi } from '@/api/geo'
import { http } from '@/utils/request'
import { isUserReportedError } from '@/utils/errorReporting'

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
    if (options.length) {
      if (!config.default_model || !options.some((o) => o.value === config.default_model)) {
        config.default_model = options[0].value
      }
      if (Array.isArray(config.verify_models)) {
        config.verify_models = config.verify_models.filter((v) =>
          options.some((o) => o.value === v)
        )
      }
      if (!config.verify_models.length) {
        config.verify_models = [options[0].value]
      }
    }
  } catch (e) {
    console.warn('加载可用模型列表失败:', e.message)
  }
}

const config = reactive({
  brand: '',
  description: '',
  advantages: [],
  competitors: [],
  default_model: '',
  verify_models: []
})

const loading = ref(false)
const saving = ref(false)
const optimizing = ref(false)
const suggestions = ref([])

const advInputVisible = ref(false)
const advInputValue = ref('')
const advInputRef = ref(null)
const compInputVisible = ref(false)
const compInputValue = ref('')
const compInputRef = ref(null)

const priorityType = (p) => {
  const map = { 高: 'danger', 中: 'warning', 低: 'success' }
  return map[p] || 'warning'
}

const showAdvInput = () => {
  advInputVisible.value = true
  nextTick(() => advInputRef.value?.focus?.())
}
const showCompInput = () => {
  compInputVisible.value = true
  nextTick(() => compInputRef.value?.focus?.())
}
const addTag = (arr, type = 'adv') => {
  const visible = type === 'comp' ? compInputVisible : advInputVisible
  const value = type === 'comp' ? compInputValue : advInputValue
  const v = value.value.trim()
  if (v && !arr.includes(v)) arr.push(v)
  value.value = ''
  visible.value = false
}

const onBlurAddTag = (type = 'adv') => {
  const arr = type === 'comp' ? config.competitors : config.advantages
  setTimeout(() => addTag(arr, type), 100)
};

const splitCN = (s) => String(s || '').split(/[、,，]/).map((x) => x.trim()).filter(Boolean)

const loadConfig = async () => {
  loading.value = true
  try {
    const res = await geoApi.getConfig()
    config.brand = res?.brand_name || ''
    config.description = res?.brand_description || ''
    config.advantages = splitCN(res?.advantages);
    config.competitors = splitCN(res?.competitors)
    config.default_model = res?.default_model || config.default_model || modelOptions.value?.[0]?.value || ''
    const verifyFromDB = splitCN(res?.verify_models)
    config.verify_models = verifyFromDB.length
      ? verifyFromDB
      : (config.verify_models.length ? config.verify_models : (modelOptions.value?.[0] ? [modelOptions.value[0].value] : []))
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(e.message || '配置加载失败')
  } finally {
    loading.value = false
  }
}

const handleSave = async () => {
  if (!config.brand.trim()) {
    ElMessage.warning('请填写品牌名称')
    return
  }
  saving.value = true
  try {
    await geoApi.updateConfig({
      brand: config.brand,
      brand_description: config.description,
      advantages: config.advantages.join('、'),
      competitors: config.competitors,
      default_model: config.default_model,
      verify_models: config.verify_models
    })
    ElMessage.success('配置已保存')
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(e.message || '配置保存失败')
  } finally {
    saving.value = false
  }
}

const handleOptimize = async () => {
  optimizing.value = true
  try {
    const res = await geoApi.optimizeConfig({
      brand_name: config.brand,
      advantages: config.advantages.join('、'),
      competitors: config.competitors
    })
    suggestions.value = res?.suggestions || []
    ElMessage.success(res?.summary || '配置优化建议已生成')
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(e.message || '配置优化失败')
  } finally {
    optimizing.value = false
  }
}

onMounted(async () => {
  await loadModelOptions()
  await loadConfig()
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
.suggest-card {
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
.tag-item {
  margin: 0;
}
.tag-input {
  width: 120px;
}
.suggest-list {
  display: flex;
  flex-direction: column;
  gap: $spacing-md;
}
.suggest-item {
  padding: $spacing-md;
  border-radius: 8px;
  background: $bg-color-page;
  border: 1px solid $border-base;
}
.suggest-item-title {
  display: flex;
  align-items: center;
  gap: $spacing-sm;
  font-weight: 600;
  color: $text-primary;
  margin-bottom: $spacing-sm;
}
.suggest-item-desc {
  font-size: $font-size-small;
  color: $text-regular;
  line-height: 1.6;
}
</style>
