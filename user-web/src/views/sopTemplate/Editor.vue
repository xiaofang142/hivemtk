<template>
  <div class="sop-template-editor-page">
    <el-card
      class="header-card"
      shadow="never"
    >
      <div class="header-content">
        <div>
          <h2>{{ isEdit ? '编辑 SOP 模板' : '创建 SOP 模板' }}</h2>
          <p
            v-if="isEdit"
            class="subtitle"
          >
            模板 ID：{{ templateId }} · 修改后保存生效
          </p>
          <p
            v-else
            class="subtitle"
          >
            填写 SOP 模板，按 (意图, 阶段) 命中后使用 Go text/template 渲染回复
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
            <el-icon><Tickets /></el-icon>
            模板信息
          </div>
        </template>
        <el-row :gutter="20">
          <el-col :span="12">
            <el-form-item
              label="模板名称"
              prop="name"
            >
              <el-input
                v-model="form.name"
                placeholder="如：发货时效问候模板"
              />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item
              label="意图"
              prop="intent"
            >
              <el-select
                v-model="form.intent"
                filterable
                allow-create
                clearable
                placeholder="如: shipping, refund"
                style="width: 100%"
              >
                <el-option
                  v-for="i in presetIntents"
                  :key="i"
                  :label="i"
                  :value="i"
                />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item
              label="阶段"
              prop="stage"
            >
              <el-select
                v-model="form.stage"
                placeholder="选择 SOP 阶段"
                style="width: 100%"
              >
                <el-option
                  v-for="s in stageOptions"
                  :key="s.value"
                  :label="s.label"
                  :value="s.value"
                />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item
              label="智能体"
              prop="agent_id"
            >
              <el-select
                v-model="form.agent_id"
                filterable
                clearable
                placeholder="选择模板归属的智能体"
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
                每条 SOP 模板必须归属一个智能体（1 对 1 绑定）
              </div>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="优先级">
              <el-input-number
                v-model="form.priority"
                :min="-100"
                :max="100"
                :step="1"
                style="width: 100%"
              />
              <div class="form-tip">
                数字越大越优先；同 (intent, stage) 多模板时按此排序
              </div>
            </el-form-item>
          </el-col>
        </el-row>
      </el-card>

      
      <el-card
        shadow="never"
        class="section-card"
      >
        <template #header>
          <div class="card-title">
            <el-icon><Document /></el-icon>
            模板内容（Go text/template）
          </div>
        </template>
        <el-form-item
          label="模板文本"
          prop="template"
        >
          <el-input
            v-model="form.template"
            type="textarea"
            :rows="6"
            placeholder="如：您好 {{.customer_name}}，关于您咨询的 {{.product_name}}，我们将在 {{.shipping_days}} 天内发货。"
          />
          <div class="form-tip">
            变量使用 <code v-pre>{{.var_name}}</code> 格式；保存后会在 Layer1 命中时用真实变量替换
          </div>
        </el-form-item>
        <el-form-item label="变量元数据">
          <el-input
            v-model="form.vars"
            type="textarea"
            :rows="3"
            placeholder="JSON 格式：{&quot;var_name&quot;: {&quot;desc&quot;: &quot;描述&quot;, &quot;example&quot;: &quot;示例&quot;}}"
          />
          <div class="form-tip">
            仅用于编辑器内提示；不影响运行时渲染
          </div>
        </el-form-item>
        <el-form-item
          v-if="varsList.length"
          label="可用变量"
        >
          <div class="var-preview">
            <el-tag
              v-for="v in varsList"
              :key="v.key"
              size="small"
              type="info"
              effect="plain"
              class="var-tag"
            >
              <span
                v-pre
                class="var-name"
              >{{.</span><span class="var-name">{{ v.key }}</span><span
                v-pre
                class="var-name"
              >}}</span>
              <span class="var-desc">{{ v.desc || v.example || v.key }}</span>
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
            <el-icon><Setting /></el-icon>
            匹配参数
          </div>
        </template>
        <el-row :gutter="20">
          <el-col :span="12">
            <el-form-item label="置信度">
              <el-slider
                v-model="form.confidence"
                :min="0"
                :max="1"
                :step="0.05"
                show-input
                :show-input-controls="false"
              />
              <div class="form-tip">
                建议 0.7+；&gt;=0.7 时 SOP 命中会跳过 LLM
              </div>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="启用">
              <el-switch v-model="form.enabled" />
              <div class="form-tip">
                禁用后该模板不参与匹配（已绑定智能体也会忽略）
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
          {{ isEdit ? '保存更新' : '创建模板' }}
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
import { Tickets, Document, Setting, Check } from '@element-plus/icons-vue'
import { sopTemplateApi } from '@/api/sopTemplate'
import { listEnabledAgents } from '@/api/aiAgent'
import { isUserReportedError } from '@/utils/errorReporting'

const t = i18n.global.t
const route = useRoute()
const router = useRouter()

const formRef = ref();
const pageLoading = ref(false)
const saving = ref(false)
const templateId = computed(() => route.params.id)
const isEdit = computed(() => !!templateId.value)

const presetIntents = [
  'shipping', 'refund', 'aftersales', 'pricing', 'product', 'payment', 'greeting', 'closing'
];

const stageOptions = [
  { value: 'initial', label: 'initial · 初始' },
  { value: 'middle', label: 'middle · 推进' },
  { value: 'late', label: 'late · 后期' },
  { value: 'objection', label: 'objection · 异议' },
  { value: 'closing', label: 'closing · 收单' }
];

const getDefaultForm = () => ({
  name: '',
  agent_id: null,
  intent: '',
  stage: 'middle',
  template: '',
  vars: '',
  priority: 0,
  confidence: 0.8,
  enabled: true
});

const form = reactive(getDefaultForm())

const agentOptions = ref([])

const loadAgentOptions = async () => {
  try {
    const res = await listEnabledAgents()
    const list = Array.isArray(res) ? res : (res?.list || [])
    agentOptions.value = list
    if (!form.agent_id && list.length) {
      form.agent_id = list[0].id
    }
  } catch (e) {
    console.warn('加载智能体列表失败', e)
  }
}

const rules = {
  name: [
    { required: true, message: t('请输入模板名称'), trigger: 'blur' }
  ],
  intent: [
    { required: true, message: t('请输入关联意图'), trigger: 'change' }
  ],
  agent_id: [
    { required: true, message: '请选择智能体', trigger: 'change' }
  ],
  stage: [
    { required: true, message: t('请选择 SOP 阶段'), trigger: 'change' }
  ],
  template: [
    { required: true, message: t('请输入模板内容'), trigger: 'blur' }
  ]
};

const varsList = computed(() => {
  if (!form.vars) return []
  try {
    const obj = JSON.parse(form.vars)
    return Object.keys(obj).map((k) => ({
      key: k,
      desc: obj[k]?.desc || '',
      example: obj[k]?.example || ''
    }))
  } catch (e) {
    return []
  }
});

const loadDetail = async () => {
  if (!isEdit.value) return
  pageLoading.value = true
  try {
    const res = await sopTemplateApi.get(templateId.value)
    if (res) {
      Object.assign(form, getDefaultForm(), res)
      form.enabled = res.enabled !== false
    }
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error('加载 SOP 模板详情失败：' + (e.message || '未知错误'))
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
    saving.value = true
    try {
      const data = {
        name: form.name,
        agent_id: form.agent_id,
        intent: form.intent,
        stage: form.stage,
        template: form.template,
        vars: form.vars || '',
        priority: form.priority || 0,
        confidence: form.confidence || 0,
        enabled: form.enabled
      }
      if (isEdit.value) {
        await sopTemplateApi.update(templateId.value, data)
        ElMessage.success(t('模板更新成功'))
      } else {
        await sopTemplateApi.create(data)
        ElMessage.success(t('模板创建成功'))
      }
      goBack()
    } catch (e) {
      if (!isUserReportedError(e)) ElMessage.error('保存失败：' + (e.message || '未知错误'))
    } finally {
      saving.value = false
    }
  })
};

const goBack = () => {
  router.push('/sop-template/list')
}

onMounted(() => {
  loadAgentOptions()
  loadDetail()
})
</script>

<style scoped>
.sop-template-editor-page {
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

.form-tip code {
  background: var(--el-fill-color-light);
  padding: 1px 4px;
  border-radius: 3px;
  font-family: 'SF Mono', Menlo, Consolas, monospace;
  font-size: 11px;
  color: var(--el-color-primary);
}

.var-preview {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  padding: 4px 0;
}

.var-tag {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.var-name {
  font-family: 'SF Mono', Menlo, Consolas, monospace;
  font-weight: 600;
  color: var(--el-color-primary);
}

.var-desc {
  color: var(--el-text-color-secondary);
  font-size: 11px;
}

.footer-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding: 16px 0;
}
</style>
