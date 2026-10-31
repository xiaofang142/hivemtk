<template>
  <div class="glossary-list-page">
    <el-card
      class="header-card"
      shadow="never"
    >
      <div class="header-content">
        <div>
          <h2>{{ $t('术语表管理') }}</h2>
          <p class="subtitle">
            维护品牌词、产品词等术语的多语言译法，配置 preserve 后翻译流程将原样保留
          </p>
        </div>
        <div class="header-actions">
          <el-button
            :loading="loading"
            @click="loadList"
          >
            <el-icon><Refresh /></el-icon>
            {{ $t('刷新') }}
          </el-button>
          <el-button
            type="primary"
            @click="openCreateDialog"
          >
            <el-icon><Plus /></el-icon>
            {{ $t('新增术语') }}
          </el-button>
          <el-button
            type="success"
            plain
            @click="openValidateDialog"
          >
            <el-icon><Search /></el-icon>
            {{ $t('校验预览') }}
          </el-button>
        </div>
      </div>
    </el-card>

    
    <el-card
      shadow="never"
      class="filter-card"
    >
      <el-form
        :inline="true"
        :model="filter"
        @submit.prevent
      >
        <el-form-item :label="$t('分类')">
          <el-select
            v-model="filter.category"
            :placeholder="$t('全部分类')"
            clearable
            style="width: 160px"
            @change="onSearch"
          >
            <el-option
              v-for="opt in categoryOptions"
              :key="opt.value"
              :label="opt.label"
              :value="opt.value"
            />
          </el-select>
        </el-form-item>
        <el-form-item :label="$t('状态')">
          <el-select
            v-model="filter.status"
            :placeholder="$t('全部状态')"
            clearable
            style="width: 140px"
            @change="onSearch"
          >
            <el-option
              :label="$t('启用')"
              :value="1"
            />
            <el-option
              :label="$t('禁用')"
              :value="0"
            />
          </el-select>
        </el-form-item>
        <el-form-item :label="$t('关键词')">
          <el-input
            v-model="filter.keyword"
            :placeholder="$t('搜索 term_id / 译文')"
            clearable
            style="width: 240px"
            @keyup.enter="onSearch"
            @clear="onSearch"
          />
        </el-form-item>
        <el-form-item>
          <el-button
            type="primary"
            @click="onSearch"
          >
            <el-icon><Search /></el-icon>
            {{ $t('搜索') }}
          </el-button>
          <el-button @click="resetFilter">
            {{ $t('重置') }}
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
          prop="term_id"
          label="term_id"
          min-width="160"
          show-overflow-tooltip
        />
        <el-table-column
          label="分类"
          width="120"
        >
          <template #default="{ row }">
            <el-tag
              :type="getCategoryTagType(row.category)"
              size="small"
            >
              {{ getCategoryLabel(row.category) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="保留原样"
          width="100"
          align="center"
        >
          <template #default="{ row }">
            <el-tag
              :type="row.preserve ? 'warning' : 'info'"
              size="small"
            >
              {{ row.preserve ? '是' : '否' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="多语言译法"
          min-width="280"
        >
          <template #default="{ row }">
            <div class="trans-cell">
              <el-tag
                v-for="(text, lang) in normalizeTranslations(row.translations)"
                :key="lang"
                size="small"
                type="success"
                class="trans-tag"
              >
                <span class="trans-lang">{{ getLanguageLabel(lang) }}</span>
                <span class="trans-text">{{ truncateText(text, 20) }}</span>
              </el-tag>
              <span
                v-if="!hasTranslations(row.translations)"
                class="empty-text"
              >-</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column
          prop="pattern"
          label="匹配正则"
          min-width="160"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            {{ row.pattern || '-' }}
          </template>
        </el-table-column>
        <el-table-column
          label="状态"
          width="90"
          align="center"
        >
          <template #default="{ row }">
            <el-tag
              :type="row.status === 1 ? 'success' : 'info'"
              size="small"
            >
              {{ row.status === 1 ? '启用' : '禁用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="created_at"
          label="创建时间"
          width="160"
        >
          <template #default="{ row }">
            {{ formatTime(row.created_at) }}
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
              @click="openEditDialog(row)"
            >
              编辑
            </el-button>
            <el-button
              link
              :type="row.status === 1 ? 'warning' : 'success'"
              size="small"
              @click="onToggleStatus(row)"
            >
              {{ row.status === 1 ? '禁用' : '启用' }}
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
          <el-empty description="暂无术语数据" />
        </template>
      </el-table>

      
      <div
        v-if="total > 0"
        class="pagination-wrap"
      >
        <el-pagination
          v-model:current-page="page"
          v-model:page-size="pageSize"
          :total="total"
          :page-sizes="[10, 20, 50, 100]"
          layout="total, sizes, prev, pager, next, jumper"
          background
          @size-change="loadList"
          @current-change="loadList"
        />
      </div>
    </el-card>

    
    <el-dialog
      v-model="formDialogVisible"
      :title="isEdit ? $t('编辑术语') : $t('新增术语')"
      width="720px"
      top="6vh"
      :close-on-click-modal="false"
    >
      <el-form
        ref="formRef"
        v-loading="formLoading"
        :model="form"
        :rules="rules"
        label-width="110px"
      >
        <el-row :gutter="20">
          <el-col :span="12">
            <el-form-item
              label="term_id"
              prop="term_id"
            >
              <el-input
                v-model="form.term_id"
                placeholder="如 brand_hivemtk，唯一标识"
                :disabled="isEdit"
              />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item
              :label="$t('分类')"
              prop="category"
            >
              <el-select
                v-model="form.category"
                style="width: 100%"
                :placeholder="$t('请选择分类')"
              >
                <el-option
                  v-for="opt in categoryOptions"
                  :key="opt.value"
                  :label="opt.label"
                  :value="opt.value"
                />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item :label="$t('保留原样')">
              <el-switch v-model="form.preserve" />
              <div class="form-tip">
                开启后该术语在翻译时原样保留，不做译写
              </div>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item :label="$t('状态')">
              <el-select
                v-model="form.status"
                style="width: 100%"
              >
                <el-option
                  :label="$t('启用')"
                  :value="1"
                />
                <el-option
                  :label="$t('禁用')"
                  :value="0"
                />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :span="24">
            <el-form-item :label="$t('匹配正则')">
              <el-input
                v-model="form.pattern"
                placeholder="可选，匹配原文的正则表达式，如 ^(HiveMTK|hivemtk)$"
              />
              <div class="form-tip">
                留空时按 term_id 全词匹配
              </div>
            </el-form-item>
          </el-col>
          <el-col :span="24">
            <el-form-item :label="$t('多语言译法')">
              <div class="trans-editor">
                <div
                  v-for="(item, idx) in translationList"
                  :key="idx"
                  class="trans-row"
                >
                  <el-select
                    v-model="item.lang"
                    :placeholder="$t('选择语言')"
                    filterable
                    style="width: 180px"
                  >
                    <el-option
                      v-for="lang in languageOptions"
                      :key="lang.value"
                      :label="lang.label"
                      :value="lang.value"
                    />
                  </el-select>
                  <el-input
                    v-model="item.text"
                    :placeholder="$t('该语言下的译法')"
                    style="flex: 1"
                  />
                  <el-button
                    link
                    type="danger"
                    size="small"
                    @click="removeTranslation(idx)"
                  >
                    <el-icon><Delete /></el-icon>
                  </el-button>
                </div>
                <el-button
                  size="small"
                  type="primary"
                  plain
                  @click="addTranslation"
                >
                  <el-icon><Plus /></el-icon>
                  {{ $t('添加译法') }}
                </el-button>
              </div>
            </el-form-item>
          </el-col>
        </el-row>
      </el-form>
      <template #footer>
        <el-button @click="formDialogVisible = false">
          {{ $t('取消') }}
        </el-button>
        <el-button
          type="primary"
          :loading="submitting"
          @click="submitForm"
        >
          {{ $t('保存') }}
        </el-button>
      </template>
    </el-dialog>

    
    <el-dialog
      v-model="validateDialogVisible"
      :title="$t('术语校验预览')"
      width="760px"
      top="6vh"
      :close-on-click-modal="false"
    >
      <el-form
        :model="validateForm"
        label-width="90px"
      >
        <el-form-item
          :label="$t('目标语言')"
          required
        >
          <el-select
            v-model="validateForm.lang"
            :placeholder="$t('请选择语言')"
            style="width: 220px"
          >
            <el-option
              v-for="lang in languageOptions"
              :key="lang.value"
              :label="lang.label"
              :value="lang.value"
            />
          </el-select>
        </el-form-item>
        <el-form-item
          :label="$t('待校验文本')"
          required
        >
          <el-input
            v-model="validateForm.text"
            type="textarea"
            :rows="5"
            placeholder="粘贴需要校验的文本，系统将标记命中的术语与违规项"
          />
        </el-form-item>
        <el-form-item>
          <el-button
            type="primary"
            :loading="validating"
            @click="runValidate"
          >
            <el-icon><Search /></el-icon>
            {{ $t('执行校验') }}
          </el-button>
          <el-button @click="clearValidateResult">
            {{ $t('清空') }}
          </el-button>
        </el-form-item>
      </el-form>

      <template v-if="validateResult">
        <el-divider content-position="left">
          {{ $t('校验结果') }}
        </el-divider>
        <el-descriptions
          :column="2"
          border
          size="small"
          class="validate-summary"
        >
          <el-descriptions-item :label="$t('命中术语数')">
            {{ validateResult.hit_count ?? (validateResult.hits?.length || 0) }}
          </el-descriptions-item>
          <el-descriptions-item :label="$t('违规项数')">
            <el-tag
              :type="validateViolations.length ? 'danger' : 'success'"
              size="small"
            >
              {{ validateViolations.length }}
            </el-tag>
          </el-descriptions-item>
        </el-descriptions>

        <div class="result-section">
          <div class="result-title">
            {{ $t('命中的术语') }}
          </div>
          <el-table
            :data="validateHits"
            stripe
            size="small"
            border
          >
            <el-table-column
              prop="term_id"
              label="term_id"
              min-width="140"
              show-overflow-tooltip
            />
            <el-table-column
              prop="matched_text"
              label="命中片段"
              min-width="140"
              show-overflow-tooltip
            >
              <template #default="{ row }">
                {{ row.matched_text || row.match || '-' }}
              </template>
            </el-table-column>
            <el-table-column
              prop="translation"
              label="应译为"
              min-width="160"
              show-overflow-tooltip
            >
              <template #default="{ row }">
                {{ row.translation || row.translated || '-' }}
              </template>
            </el-table-column>
            <el-table-column
              label="保留"
              width="80"
              align="center"
            >
              <template #default="{ row }">
                <el-tag
                  v-if="row.preserve"
                  type="warning"
                  size="small"
                >
                  是
                </el-tag>
                <span v-else>-</span>
              </template>
            </el-table-column>
            <template #empty>
              <el-empty
                description="无命中术语"
                :image-size="50"
              />
            </template>
          </el-table>
        </div>

        <div
          v-if="validateViolations.length"
          class="result-section"
        >
          <div class="result-title violation-title">
            {{ $t('违规项') }}
          </div>
          <el-table
            :data="validateViolations"
            stripe
            size="small"
            border
          >
            <el-table-column
              prop="term_id"
              label="term_id"
              min-width="140"
              show-overflow-tooltip
            />
            <el-table-column
              prop="issue"
              label="问题描述"
              min-width="200"
              show-overflow-tooltip
            >
              <template #default="{ row }">
                <span class="violation-text">{{ row.issue || row.reason || row.message || '-' }}</span>
              </template>
            </el-table-column>
            <el-table-column
              prop="snippet"
              label="文本片段"
              min-width="200"
              show-overflow-tooltip
            >
              <template #default="{ row }">
                {{ row.snippet || row.context || '-' }}
              </template>
            </el-table-column>
          </el-table>
        </div>
      </template>
      <el-empty
        v-else-if="!validating"
        :description="$t('执行校验后展示结果')"
        :image-size="80"
      />
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'
import { isUserReportedError } from '@/utils/errorReporting'

import { ref, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh, Plus, Search, Delete } from '@element-plus/icons-vue'
import {
  listGlossaries,
  createGlossary,
  updateGlossary,
  deleteGlossary,
  validateGlossary
} from '@/api/glossary.js'
import {
  SUPPORTED_LANGUAGES,
  getLanguageLabel
} from '@/constants/languages'

const categoryOptions = [
  { value: 'brand', label: '品牌词' },
  { value: 'product', label: '产品词' },
  { value: 'entity', label: '实体名' },
  { value: 'technical', label: '技术术语' },
  { value: 'custom', label: '自定义' }
];
const categoryMap = categoryOptions.reduce((acc, o) => { acc[o.value] = o.label; return acc }, {})
const getCategoryLabel = (v) => categoryMap[v] || v || '-'
const getCategoryTagType = (v) => {
  if (v === 'brand') return 'danger'
  if (v === 'product') return 'warning'
  if (v === 'entity') return 'primary'
  if (v === 'technical') return 'success'
  return 'info'
}

const languageOptions = SUPPORTED_LANGUAGES;

const loading = ref(false);
const list = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const filter = ref({ category: '', status: '', keyword: '' })

const formDialogVisible = ref(false);
const formLoading = ref(false)
const submitting = ref(false)
const isEdit = ref(false)
const formRef = ref(null)
const editingTermId = ref('')
const form = ref({
  term_id: '',
  category: 'brand',
  preserve: false,
  pattern: '',
  status: 1,
  translations: {}
})
const translationList = ref([]);
const rules = {
  term_id: [{ required: true, message: '请输入 term_id', trigger: 'blur' }],
  category: [{ required: true, message: '请选择分类', trigger: 'change' }]
}

const validateDialogVisible = ref(false);
const validating = ref(false)
const validateForm = ref({ lang: 'en', text: '' })
const validateResult = ref(null)

const validateHits = computed(() => {
  const r = validateResult.value
  if (!r) return []
  return Array.isArray(r.hits) ? r.hits : (Array.isArray(r.items) ? r.items : [])
})

const validateViolations = computed(() => {
  const r = validateResult.value
  if (!r) return []
  return Array.isArray(r.violations) ? r.violations : (Array.isArray(r.issues) ? r.issues : [])
})

const normalizeTranslations = (t) => {
  if (!t) return {}
  if (Array.isArray(t)) {
    const obj = {};
    t.forEach((item) => {
      if (item && item.lang) obj[item.lang] = item.text || item.translation || ''
    })
    return obj
  }
  return t
};

const hasTranslations = (t) => {
  const obj = normalizeTranslations(t)
  return Object.keys(obj).length > 0
}

const truncateText = (text, len) => {
  if (text === undefined || text === null || text === '') return '-'
  const s = String(text)
  return s.length > len ? s.slice(0, len) + '...' : s
}

const formatTime = (t) => {
  if (!t) return '-'
  const d = new Date(t)
  if (isNaN(d.getTime())) return '-'
  const pad = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

const loadList = async () => {
  loading.value = true
  try {
    const params = {
      page: page.value,
      page_size: pageSize.value
    }
    if (filter.value.category) params.category = filter.value.category
    if (filter.value.status !== '' && filter.value.status !== null && filter.value.status !== undefined) {
      params.status = filter.value.status
    }
    if (filter.value.keyword) params.keyword = filter.value.keyword
    const res = await listGlossaries(params)
    if (Array.isArray(res)) {
      list.value = res
      total.value = res.length
    } else {
      list.value = res?.list || res?.items || []
      total.value = res?.total ?? list.value.length
    }
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(i18n.global.t('加载术语列表失败') + '：' + (e.message || '未知错误'))
  } finally {
    loading.value = false
  }
};

const onSearch = () => {
  page.value = 1
  loadList()
}

const resetFilter = () => {
  filter.value = { category: '', status: '', keyword: '' }
  page.value = 1
  loadList()
}

const resetForm = () => {
  form.value = {
    term_id: '',
    category: 'brand',
    preserve: false,
    pattern: '',
    status: 1,
    translations: {}
  }
  translationList.value = []
  editingTermId.value = ''
};

const addTranslation = () => {
  translationList.value.push({ lang: '', text: '' })
}

const removeTranslation = (idx) => {
  translationList.value.splice(idx, 1)
}

const openCreateDialog = () => {
  isEdit.value = false
  resetForm()
  formDialogVisible.value = true
}

const openEditDialog = async (row) => {
  isEdit.value = true
  editingTermId.value = row.term_id
  resetForm()
  formDialogVisible.value = true
  formLoading.value = true
  try {
    const detail = row.term_id && !hasTranslations(row.translations)
      ? await getGlossarySafe(row.term_id)
      : row;
    form.value = {
      term_id: detail.term_id || row.term_id,
      category: detail.category || 'brand',
      preserve: !!detail.preserve,
      pattern: detail.pattern || '',
      status: detail.status === 0 ? 0 : 1,
      translations: {}
    }
    const obj = normalizeTranslations(detail.translations)
    translationList.value = Object.keys(obj).map((lang) => ({ lang, text: obj[lang] || '' }))
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(i18n.global.t('加载术语详情失败') + '：' + (e.message || '未知错误'))
  } finally {
    formLoading.value = false
  }
}

const getGlossarySafe = async (termId) => {
  try {
    const { getGlossary } = await import('@/api/glossary.js')
    return await getGlossary(termId)
  } catch (e) {
    return {}
  }
}

const submitForm = async () => {
  if (!formRef.value) return
  try {
    await formRef.value.validate()
  } catch (_) {
    return
  }
  const translations = {};
  let hasInvalid = false
  translationList.value.forEach((item) => {
    if (item.lang && item.text) {
      translations[item.lang] = item.text
    } else if (item.lang || item.text) {
      hasInvalid = true
    }
  })
  if (hasInvalid) {
    ElMessage.warning(i18n.global.t('存在未填写完整的多语言译法，已自动忽略'))
  }

  const payload = {
    term_id: form.value.term_id,
    category: form.value.category,
    preserve: form.value.preserve,
    pattern: form.value.pattern || '',
    status: form.value.status,
    translations
  }

  // 更新必须有 term_id：拼不出 id 时 URL 会变成 /api/glossaries/，后端 404，
  // 用户看到的是「请求地址不存在」，与“保存失败”对不上。
  if (isEdit.value && !editingTermId.value) {
    ElMessage.warning('缺少术语 ID，无法更新')
    return
  }

  submitting.value = true
  try {
    if (isEdit.value) {
      await updateGlossary(editingTermId.value, payload)
      ElMessage.success(i18n.global.t('更新成功'))
    } else {
      await createGlossary(payload)
      ElMessage.success(i18n.global.t('创建成功'))
    }
    formDialogVisible.value = false
    loadList()
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error((isEdit.value ? i18n.global.t('更新失败') : i18n.global.t('创建失败')) + '：' + (e.message || '未知错误'))
  } finally {
    submitting.value = false
  }
}

const onToggleStatus = (row) => {
  const next = row.status === 1 ? 0 : 1
  const action = next === 1 ? '启用' : '禁用'
  ElMessageBox.confirm(
    `确认${action}术语「${row.term_id}」吗？`,
    i18n.global.t('操作确认'),
    { type: 'warning', confirmButtonText: i18n.global.t('确认') + action, cancelButtonText: i18n.global.t('取消') }
  ).then(async () => {
    try {
      await updateGlossary(row.term_id, { status: next })
      ElMessage.success(`${action}成功`)
      loadList()
    } catch (e) {
      if (!isUserReportedError(e)) ElMessage.error(`${action}失败：` + (e.message || '未知错误'))
    }
  }).catch(() => {})
};

const onDelete = (row) => {
  ElMessageBox.confirm(
    `确认删除术语「${row.term_id}」吗？删除后不可恢复。`,
    i18n.global.t('删除确认'),
    { type: 'warning', confirmButtonText: i18n.global.t('确认删除'), cancelButtonText: i18n.global.t('取消') }
  ).then(async () => {
    try {
      await deleteGlossary(row.term_id)
      ElMessage.success(i18n.global.t('删除成功'))
      loadList()
    } catch (e) {
      if (!isUserReportedError(e)) ElMessage.error(i18n.global.t('删除失败') + '：' + (e.message || '未知错误'))
    }
  }).catch(() => {})
};

const openValidateDialog = () => {
  validateForm.value = { lang: 'en', text: '' }
  validateResult.value = null
  validateDialogVisible.value = true
};

const runValidate = async () => {
  if (!validateForm.value.text || !validateForm.value.text.trim()) {
    ElMessage.warning(i18n.global.t('请输入待校验文本'))
    return
  }
  if (!validateForm.value.lang) {
    ElMessage.warning(i18n.global.t('请选择目标语言'))
    return
  }
  validating.value = true
  validateResult.value = null
  try {
    const res = await validateGlossary({
      text: validateForm.value.text,
      lang: validateForm.value.lang
    })
    validateResult.value = res || { hits: [], violations: [] }
    ElMessage.success(i18n.global.t('校验完成'))
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error(i18n.global.t('校验失败') + '：' + (e.message || '未知错误'))
  } finally {
    validating.value = false
  }
}

const clearValidateResult = () => {
  validateResult.value = null
  validateForm.value.text = ''
}

onMounted(() => {
  loadList()
});
</script>

<style scoped lang="scss">
.glossary-list-page { padding: 20px; }

.header-card {
  margin-bottom: 16px;
  :deep(.el-card__body) { padding: 16px 20px; }
  .header-content {
    display: flex;
    justify-content: space-between;
    align-items: center;
    h2 { margin: 0 0 6px 0; font-size: 20px; }
    .subtitle { color: #909399; margin: 0; font-size: 13px; }
    .header-actions { display: flex; gap: 8px; }
  }
}

.filter-card { margin-bottom: 16px; }

.trans-cell {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  .trans-tag {
    display: inline-flex;
    align-items: center;
    max-width: 100%;
    .trans-lang { font-weight: 600; margin-right: 4px; }
    .trans-text { color: #303133; }
  }
  .empty-text { color: #909399; }
}

.pagination-wrap {
  margin-top: 16px;
  display: flex;
  justify-content: flex-end;
}

.form-tip {
  font-size: 12px;
  color: #909399;
  line-height: 1.4;
  margin-top: 2px;
}

.trans-editor {
  width: 100%;
  .trans-row {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 8px;
  }
}

.validate-summary { margin-bottom: 16px; }

.result-section { margin-top: 16px; }
.result-title {
  font-weight: 600;
  margin-bottom: 8px;
  color: #303133;
}
.violation-title { color: #F56C6C; }
.violation-text { color: #F56C6C; }
</style>
