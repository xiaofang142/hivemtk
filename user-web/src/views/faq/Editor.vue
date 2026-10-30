<template>
  <div class="faq-editor-page">
    <el-card
      class="header-card"
      shadow="never"
    >
      <div class="header-content">
        <div>
          <h2>{{ isEdit ? '编辑 FAQ' : '创建 FAQ' }}</h2>
          <p
            v-if="isEdit"
            class="subtitle"
          >
            FAQ ID：{{ faqId }} · 修改后保存生效
          </p>
          <p
            v-else
            class="subtitle"
          >
            填写 FAQ 条目，启用后将进入 Layer1 快速匹配库
          </p>
        </div>
        <div class="header-actions">
          <el-button @click="goBack">
            返回列表
          </el-button>
        </div>
      </div>
    </el-card>

    <el-form
      ref="formRef"
      v-loading="pageLoading"
      :model="form"
      :rules="rules"
      label-width="120px"
    >
      <el-card
        shadow="never"
        class="section-card"
      >
        <template #header>
          <div class="card-title">
            <el-icon><QuestionFilled /></el-icon>
            FAQ 内容
          </div>
        </template>
        <el-form-item
          label="问题"
          prop="question"
        >
          <el-input
            v-model="form.question"
            type="textarea"
            :rows="2"
            placeholder="用户可能问的问题，如：你们的发货时间多久？"
          />
        </el-form-item>
        <el-form-item
          label="标准答案"
          prop="answer"
        >
          <el-input
            v-model="form.answer"
            type="textarea"
            :rows="4"
            placeholder="命中后返回的标准答案"
          />
        </el-form-item>
        <el-form-item
          label="关键词"
          prop="keywords"
        >
          <el-input
            v-model="form.keywordsInput"
            placeholder="多个关键词以英文逗号分隔，如：发货, 快递, 时效"
          />
          <div class="form-tip">
            用于 Layer1 关键词匹配；多个关键词可提升命中率
          </div>
        </el-form-item>
        <el-form-item
          v-if="form.keywords && form.keywords.length"
          label="预览"
        >
          <div class="keyword-preview">
            <el-tag
              v-for="k in form.keywords"
              :key="k"
              size="small"
              type="info"
              effect="plain"
              closable
              @close="removeKeyword(k)"
            >
              {{ k }}
            </el-tag>
          </div>
        </el-form-item>
      </el-card>

      
      <el-card
        shadow="never"
        class="section-card"
      >
        <template #header>
          <div class="card-title">
            <el-icon><CollectionTag /></el-icon>
            分类与意图
          </div>
        </template>
        <el-row :gutter="20">
          <el-col :span="12">
            <el-form-item label="业务分类">
              <el-select
                v-model="form.category"
                filterable
                allow-create
                clearable
                placeholder="如：logistics / pricing / aftersales"
                style="width: 100%"
              >
                <el-option
                  v-for="c in presetCategories"
                  :key="c"
                  :label="c"
                  :value="c"
                />
              </el-select>
              <div class="form-tip">
                支持自定义输入新分类，便于前台筛选
              </div>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="关联意图">
              <el-input
                v-model="form.intent"
                placeholder="如：shipping / refund / aftersales"
              />
              <div class="form-tip">
                与意图识别结果对齐，命中后此意图将作为前置答案
              </div>
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item
          label="智能体"
          prop="agent_id"
        >
          <el-select
            v-model="form.agent_id"
            filterable
            clearable
            placeholder="该 FAQ 归属的智能体（必选）"
            style="width: 100%"
          >
            <el-option
              v-for="a in agentOptions"
              :key="a.id"
              :label="a.name"
              :value="a.id"
            />
          </el-select>
          <div class="form-tip">
            每条 FAQ 必须归属一个智能体（1 对 1 绑定）
          </div>
        </el-form-item>
      </el-card>

      <el-card
        shadow="never"
        class="section-card"
      >
        <template #header>
          <div class="card-title">
            <el-icon><Setting /></el-icon>
            匹配参数
          </div>
        </template>
        <el-row :gutter="20">
          <el-col :span="12">
            <el-form-item label="基准置信度">
              <el-slider
                v-model="form.confidence"
                :min="0"
                :max="1"
                :step="0.05"
                show-input
                :show-input-controls="false"
              />
              <div class="form-tip">
                建议 0.7+；与 ConfidenceThreshold 共同决定是否 SkipLLM
              </div>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="启用">
              <el-switch v-model="form.enabled" />
              <div class="form-tip">
                禁用后该 FAQ 不参与匹配（已绑定智能体也会忽略）
              </div>
            </el-form-item>
          </el-col>
        </el-row>
      </el-card>

      
      <div class="footer-actions">
        <el-button @click="goBack">
          取消
        </el-button>
        <el-button
          type="primary"
          :loading="saving"
          @click="onSave"
        >
          <el-icon><Check /></el-icon>
          {{ isEdit ? '保存更新' : '创建 FAQ' }}
        </el-button>
      </div>
    </el-form>
  </div>
</template>

<script setup>
import i18n from '@/i18n'
import { ref, reactive, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import {
  QuestionFilled, CollectionTag, Setting, Check
} from '@element-plus/icons-vue'
import { faqApi } from '@/api/faq'
import { listEnabledAgents } from '@/api/aiAgent'
import { isUserReportedError } from '@/utils/errorReporting'

const t = i18n.global.t
const route = useRoute()
const router = useRouter()

const formRef = ref();
const pageLoading = ref(false)
const saving = ref(false)
const agentOptions = ref([])
const faqId = computed(() => route.params.id)
const isEdit = computed(() => !!faqId.value)

const presetCategories = [
  'logistics',
  'pricing',
  'aftersales',
  'product',
  'payment',
  'account',
  'general'
];

const getDefaultForm = () => ({
  agent_id: null,
  question: '',
  answer: '',
  keywords: [],
  keywordsInput: '',
  category: '',
  intent: '',
  confidence: 0.8,
  enabled: true
});

const form = reactive(getDefaultForm())

const rules = {
  agent_id: [
    { required: true, message: '请选择智能体', trigger: 'change' }
  ],
  question: [
    { required: true, message: t('请输入问题'), trigger: 'blur' }
  ],
  answer: [
    { required: true, message: t('请输入标准答案'), trigger: 'blur' }
  ]
};

const removeKeyword = (k) => {
  form.keywords = form.keywords.filter((x) => x !== k)
};

const parseKeywords = (input) => {
  if (!input) return []
  return input
    .split(/[,，;；\s]+/)
    .map((s) => s.trim())
    .filter(Boolean)
}

const loadAgentOptions = async () => {
  try {
    const res = await listEnabledAgents()
    const list = Array.isArray(res) ? res : (res?.list || [])
    agentOptions.value = list
    if (!isEdit.value && !form.agent_id && list.length) {
      form.agent_id = list[0].id
    }
  } catch (e) {
    agentOptions.value = []
  }
}

const loadDetail = async () => {
  if (!isEdit.value) return
  pageLoading.value = true
  try {
    const res = await faqApi.get(faqId.value)
    if (res) {
      Object.assign(form, getDefaultForm(), res)
      form.keywords = Array.isArray(res.keywords) ? [...res.keywords] : []
      form.keywordsInput = form.keywords.join(', ')
      form.enabled = res.enabled !== false
    }
  } catch (e) {
    if (!isUserReportedError(e)) {
      ElMessage.error('加载 FAQ 详情失败：' + (e.message || '未知错误'))
    }
    goBack()
  } finally {
    pageLoading.value = false
  }
};

const onSave = async () => {
  if (!formRef.value) return
  await formRef.value.validate(async (valid) => {
    if (!valid) {
      ElMessage.warning(t('请完善必填项后再提交'))
      return
    }
    const fromInput = parseKeywords(form.keywordsInput);
    const merged = Array.from(new Set([...(form.keywords || []), ...fromInput]))
    saving.value = true
    try {
      const data = {
        agent_id: form.agent_id,
        question: form.question,
        answer: form.answer,
        keywords: merged,
        category: form.category || '',
        intent: form.intent || '',
        confidence: form.confidence || 0,
        enabled: form.enabled
      }
      if (isEdit.value) {
        await faqApi.update(faqId.value, data)
        ElMessage.success(t('FAQ 更新成功'))
      } else {
        await faqApi.create(data)
        ElMessage.success(t('FAQ 创建成功'))
      }
      goBack()
    } catch (e) {
      if (!isUserReportedError(e)) {
        ElMessage.error('保存失败：' + (e.message || '未知错误'))
      }
    } finally {
      saving.value = false
    }
  })
};

const goBack = () => {
  router.push('/faq/list')
}

onMounted(() => {
  loadAgentOptions()
  loadDetail()
})
</script>

<style scoped>
.faq-editor-page {
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

.section-card {
  margin-top: 0;
}

.section-card :deep(.el-card__body) {
  padding: 18px 20px 4px 20px;
}

.card-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.form-tip {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-top: 4px;
  line-height: 1.4;
}

.keyword-preview {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  padding: 4px 0;
}

.footer-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding: 16px 0;
}
</style>
