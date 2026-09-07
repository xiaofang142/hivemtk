<template>
  <div class="ai-agent-edit-page">
    
    <el-card class="header-card" shadow="never">
      <div class="header-content">
        <div>
          <h2>{{ isEdit ? '编辑智能体' : '创建智能体' }}</h2>
          <p class="subtitle" v-if="isEdit">智能体ID：{{ agentId }} · 修改后保存生效</p>
          <p class="subtitle" v-else>{{ $t('填写智能体配置，创建后即可绑定到渠道或客服座席') }}</p>
        </div>
        <div class="header-actions">
          <el-button @click="goBack">{{ $t('返回列表') }}</el-button>
          <el-button type="primary" :loading="testing" @click="openTestDialog">
            <el-icon><VideoPlay /></el-icon>
            {{ $t('测试') }}
          </el-button>
        </div>
      </div>
    </el-card>

    <el-form
      ref="formRef"
      :model="form"
      :rules="rules"
      label-width="120px"
      v-loading="pageLoading"
    >
      
      <el-card shadow="never" class="section-card">
        <template #header>
          <div class="card-title"><el-icon><InfoFilled /></el-icon> {{ $t('基本信息') }}</div>
        </template>
        <el-row :gutter="20">
          <el-col :span="12">
            <el-form-item label="智能体编码" prop="agent_code">
              <el-input v-model="form.agent_code" placeholder="如 sales_agent_01，唯一标识" :disabled="isEdit" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="智能体名称" prop="name">
              <el-input v-model="form.name" placeholder="请输入智能体名称" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="智能体类型" prop="agent_type">
              <el-select v-model="form.agent_type" style="width: 100%">
                <el-option label="销售智能体" value="sales" />
                <el-option label="客服智能体" value="customer_service" />
                <el-option label="混合智能体" value="hybrid" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="智能体模式" prop="agent_mode">
              <el-select v-model="form.agent_mode" style="width: 100%">
                <el-option label="被动响应（用户问才答）" value="passive" />
                <el-option label="主动出击（会主动推荐/推送）" value="active" />
              </el-select>
              <div class="form-tip">被动=客服风格只响应提问；主动=销售风格会主动推荐商品/活动/逼单</div>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="资产包">
              <el-select
                v-model="form.asset_bundle_id"
                placeholder="选择资产包（含话术/SOP/FAQ）"
                clearable
                filterable
                style="width: 100%"
              >
                <el-option
                  v-for="b in assetBundleOptions"
                  :key="b.asset_id"
                  :label="b.title || b.asset_id"
                  :value="b.asset_id"
                >
                  <span>{{ b.title || b.asset_id }}</span>
                  <span style="float: right; color: #8492a6; font-size: 12px">{{ b.industry || '' }}</span>
                </el-option>
              </el-select>
              <div class="form-tip">资产包包含该智能体的话术模板、SOP流程、FAQ知识库。创建时绑定，运行时自动注入 system prompt。</div>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="头像URL">
              <el-input v-model="form.avatar" placeholder="头像图片URL" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="内部语言" prop="internal_language">
              <el-select v-model="form.internal_language" placeholder="请选择内部语言" style="width: 100%">
                <el-option
                  v-for="lang in internalLanguageOptions"
                  :key="lang.value"
                  :label="lang.label"
                  :value="lang.value"
                />
              </el-select>
              <div class="form-tip">商户维护知识库使用的语言，影响内部工具处理精度。默认中文。</div>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="目标语言" prop="target_language">
              <el-select v-model="form.target_language" placeholder="请选择目标语言" clearable style="width: 100%">
                <el-option
                  v-for="lang in targetLanguageOptions"
                  :key="lang.value"
                  :label="lang.label"
                  :value="lang.value"
                />
              </el-select>
              <div class="form-tip">智能体对外输出语言。空表示跟随内部语言（同语种零开销）。</div>
            </el-form-item>
          </el-col>
          <el-col :span="24">
            <el-form-item label="描述">
              <el-input v-model="form.description" type="textarea" :rows="2" placeholder="智能体用途说明" />
            </el-form-item>
          </el-col>
        </el-row>
      </el-card>

      
      <el-card shadow="never" class="section-card">
        <template #header>
          <div class="card-title"><el-icon><UserFilled /></el-icon> 人设配置</div>
        </template>
        <el-form-item label="人设描述" prop="persona">
          <el-input
            v-model="form.persona"
            type="textarea"
            :rows="4"
            placeholder="智能体的角色设定，如：你是一名资深的汽车销售顾问，擅长挖掘客户需求..."
          />
        </el-form-item>
        <el-form-item label="系统提示词">
          <el-input
            v-model="form.system_prompt"
            type="textarea"
            :rows="4"
            placeholder="LLM 系统提示词，附加在人设之上微调行为"
          />
        </el-form-item>
        <el-form-item label="欢迎语">
          <el-input v-model="form.greeting" placeholder="会话开始时的欢迎语" />
        </el-form-item>
      </el-card>

      
      <el-card shadow="never" class="section-card">
        <template #header>
          <div class="card-title">
            <el-icon><Collection /></el-icon> 知识库挂载（树形多选）
            <el-button link type="primary" size="small" style="margin-left: auto" @click="loadKnowledgeBases" :loading="loadingKB">
              <el-icon><Refresh /></el-icon> 加载知识库
            </el-button>
          </div>
        </template>
        <el-form-item label="挂载知识库">
          <el-tree-select
            v-model="form.kb_ids"
            :data="kbTree"
            :props="kbTreeProps"
            node-key="id"
            multiple
            show-checkbox
            check-strictly
            check-on-click-node
            collapse-tags
            collapse-tags-tooltip
            clearable
            filterable
            :default-expand-all="false"
            placeholder="选择要挂载的知识库（按类型分组）"
            style="width: 100%"
            :render-after-expand="false"
            :empty-text="loadingKB ? '加载中...' : '暂无可挂载的知识库，点击右上角【加载知识库】'"
          />
          <div class="form-tip">
            树形结构按 RAG / FAQ / SOP 三个顶层分组。挂载知识库后，对话中自动应用该 KB 下所有文档 / FAQ / SOP 模板。
            旧的 faq_entry_ids / sop_template_ids / rag_product_ids 字段已废弃但仍保留兼容。
          </div>
        </el-form-item>
        <el-row :gutter="20">
          <el-col :span="8">
            <el-form-item label="RAG产品 (旧)">
              <el-select
                v-model="form.rag_product_ids"
                multiple
                filterable
                clearable
                placeholder="兼容旧字段"
                style="width: 100%"
              >
                <el-option
                  v-for="item in ragProductOptions"
                  :key="item.id"
                  :label="item.name"
                  :value="String(item.id)"
                />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :span="8">
            <el-form-item label="FAQ (旧)">
              <el-select
                v-model="form.faq_entry_ids"
                multiple
                filterable
                clearable
                placeholder="兼容旧字段"
                style="width: 100%"
              >
                <el-option
                  v-for="item in faqOptions"
                  :key="item.id"
                  :label="`#${item.id} ${item.question}`"
                  :value="String(item.id)"
                />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :span="8">
            <el-form-item label="SOP (旧)">
              <el-select
                v-model="form.sop_template_ids"
                multiple
                filterable
                clearable
                placeholder="兼容旧字段"
                style="width: 100%"
              >
                <el-option
                  v-for="item in sopTemplateOptions"
                  :key="item.id"
                  :label="`#${item.id} ${item.name}`"
                  :value="String(item.id)"
                />
              </el-select>
            </el-form-item>
          </el-col>
        </el-row>
      </el-card>

      
      <el-card shadow="never" class="section-card">
        <template #header>
          <div class="card-title"><el-icon><Connection /></el-icon> SOP 挂载</div>
        </template>
        <el-form-item label="SOP列表">
          <el-select
            v-model="form.sop_ids"
            multiple
            filterable
            clearable
            placeholder="选择要挂载的 SOP 流程"
            style="width: 100%"
          >
            <el-option
              v-for="item in sopOptions"
              :key="item.id"
              :label="item.name"
              :value="String(item.id)"
            />
          </el-select>
        </el-form-item>
      </el-card>

      
      <el-card shadow="never" class="section-card">
        <template #header>
          <div class="card-title"><el-icon><ChatLineSquare /></el-icon> 话术库挂载</div>
        </template>
        <el-form-item label="话术模板">
          <el-select
            v-model="form.script_library_ids"
            multiple
            filterable
            clearable
            placeholder="选择要挂载的话术模板"
            style="width: 100%"
          >
            <el-option
              v-for="item in scriptOptions"
              :key="item.id"
              :label="item.title"
              :value="String(item.id)"
            />
          </el-select>
        </el-form-item>
      </el-card>

      
      <el-card shadow="never" class="section-card">
        <template #header>
          <div class="card-title"><el-icon><Cpu /></el-icon> LLM 配置</div>
        </template>
        <el-row :gutter="20">
          <el-col :span="12">
            <el-form-item label="LLM模型">
              <el-select v-model="form.llm_model" style="width: 100%">
                <el-option label="gpt-4o-mini" value="gpt-4o-mini" />
                <el-option label="gpt-4o" value="gpt-4o" />
                <el-option label="gpt-4.1-mini" value="gpt-4.1-mini" />
                <el-option label="gpt-4.1" value="gpt-4.1" />
                <el-option label="claude-3-5-sonnet" value="claude-3-5-sonnet" />
                <el-option label="claude-3-5-haiku" value="claude-3-5-haiku" />
                <el-option label="deepseek-chat" value="deepseek-chat" />
                <el-option label="qwen-plus" value="qwen-plus" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="最大Token">
              <el-input-number v-model="form.max_tokens" :min="100" :max="8192" :step="100" style="width: 100%" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="温度">
              <el-slider v-model="form.temperature" :min="0" :max="2" :step="0.1" show-input />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="Top P">
              <el-slider v-model="form.top_p" :min="0" :max="1" :step="0.05" show-input />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="频率惩罚">
              <el-slider v-model="form.frequency_penalty" :min="-2" :max="2" :step="0.1" show-input />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="存在惩罚">
              <el-slider v-model="form.presence_penalty" :min="-2" :max="2" :step="0.1" show-input />
            </el-form-item>
          </el-col>
        </el-row>
      </el-card>

      
      <el-card shadow="never" class="section-card">
        <template #header>
          <div class="card-title"><el-icon><Open /></el-icon> 销售引擎开关</div>
        </template>
        <el-row :gutter="20">
          <el-col :span="8">
            <el-form-item label="RAG检索">
              <el-switch v-model="form.enable_rag" />
            </el-form-item>
          </el-col>
          <el-col :span="8">
            <el-form-item label="话术匹配">
              <el-switch v-model="form.enable_script_match" />
            </el-form-item>
          </el-col>
          <el-col :span="8">
            <el-form-item label="拟人化润色">
              <el-switch v-model="form.enable_humanize_polish" />
            </el-form-item>
          </el-col>
          <el-col :span="8">
            <el-form-item label="内容审核">
              <el-switch v-model="form.enable_content_audit" />
            </el-form-item>
          </el-col>
          <el-col :span="8">
            <el-form-item label="销冠话术">
              <el-switch v-model="form.enable_playbook" />
            </el-form-item>
          </el-col>
        </el-row>
      </el-card>

      
      <el-card shadow="never" class="section-card">
        <template #header>
          <div class="card-title"><el-icon><Setting /></el-icon> 高级参数</div>
        </template>
        <el-row :gutter="20">
          <el-col :span="8">
            <el-form-item label="RAG TopK">
              <el-input-number v-model="form.rag_top_k" :min="1" :max="20" :step="1" style="width: 100%" />
            </el-form-item>
          </el-col>
          <el-col :span="8">
            <el-form-item label="置信度阈值">
              <el-slider v-model="form.confidence_threshold" :min="0" :max="1" :step="0.05" show-input />
            </el-form-item>
          </el-col>
          <el-col :span="8">
            <el-form-item label="AI连续回复上限">
              <el-input-number v-model="form.max_ai_consecutive" :min="1" :max="50" :step="1" style="width: 100%" />
            </el-form-item>
          </el-col>
        </el-row>
      </el-card>

      
      <div class="footer-actions">
        <el-button @click="goBack">取消</el-button>
        <el-button type="primary" :loading="saving" @click="onSave">
          <el-icon><Check /></el-icon>
          {{ isEdit ? '保存更新' : '创建智能体' }}
        </el-button>
      </div>
    </el-form>

    
    <el-dialog v-model="testDialogVisible" title="测试智能体" width="760px" top="6vh">
      <el-form :model="testForm" label-width="90px">
        <el-form-item label="智能体">
          <el-input :model-value="form.name || form.agent_code" disabled />
        </el-form-item>
        <el-form-item label="客户ID">
          <el-input v-model="testForm.customer_id" placeholder="请输入客户ID（可选）" />
        </el-form-item>
        <el-form-item label="消息内容" required>
          <el-input
            v-model="testForm.message"
            type="textarea"
            :rows="3"
            placeholder="请输入要测试的客户消息"
          />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" :loading="testing" @click="runTest">
            <el-icon><VideoPlay /></el-icon>
            执行测试
          </el-button>
          <el-button @click="testResult = null">清空结果</el-button>
        </el-form-item>
      </el-form>

      <template v-if="testResult">
        <el-divider content-position="left">测试结果</el-divider>
        <el-descriptions :column="2" border size="small">
          <el-descriptions-item label="回复内容" :span="2">
            <div class="reply-box">{{ testResult.reply || '-' }}</div>
          </el-descriptions-item>
          <el-descriptions-item label="LLM模型">{{ testResult.llm_model || '-' }}</el-descriptions-item>
          <el-descriptions-item label="耗时(ms)">{{ testResult.latency_ms || 0 }}</el-descriptions-item>
          <el-descriptions-item label="消耗Token">{{ testResult.cost_tokens || 0 }}</el-descriptions-item>
          <el-descriptions-item label="是否转人工">
            <el-tag :type="testResult.transferred_to_human ? 'warning' : 'success'" size="small">
              {{ testResult.transferred_to_human ? '是' : '否' }}
            </el-tag>
            <span v-if="testResult.transfer_reason" class="transfer-reason">（{{ testResult.transfer_reason }}）</span>
          </el-descriptions-item>
        </el-descriptions>

        <div class="chain-title">9步链路日志</div>
        <el-table :data="testResult.steps || []" stripe size="small" border>
          <el-table-column type="index" label="#" width="50" align="center" />
          <el-table-column prop="step" label="步骤" min-width="160" show-overflow-tooltip />
          <el-table-column label="状态" width="90" align="center">
            <template #default="{ row }">
              <el-tag :type="getStepStatusType(row.status)" size="small">{{ row.status || '-' }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="耗时(ms)" width="100" align="center">
            <template #default="{ row }">{{ row.latency_ms || 0 }}</template>
          </el-table-column>
          <el-table-column prop="detail" label="详情" min-width="200" show-overflow-tooltip />
          <el-table-column prop="error" label="错误" min-width="160" show-overflow-tooltip>
            <template #default="{ row }">
              <span v-if="row.error" class="error-text">{{ row.error }}</span>
              <span v-else>-</span>
            </template>
          </el-table-column>
        </el-table>
      </template>
      <el-empty v-else-if="!testing" description="执行测试后显示结果" :image-size="80" />
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, reactive, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import {
  InfoFilled, UserFilled, Collection, Connection, ChatLineSquare,
  Cpu, Open, Setting, Check, VideoPlay
} from '@element-plus/icons-vue'
import { createAgent, updateAgent, getAgent, testAgent } from '@/api/aiAgent.js'
import { ragProductConfigAPI } from '@/api/ragProductConfig'
import { sopApi } from '@/api/sopAgent.js'
import { getScriptTemplateList } from '@/api/scriptTemplate.js'
import { faqApi } from '@/api/faq'
import { sopTemplateApi } from '@/api/sopTemplate'
import { listByType as listKBByType } from '@/api/knowledgeBase'
import { listByAgent as listAgentKBs, replaceAgentKBs } from '@/api/agentKBBinding'
import { listBundles } from '@/api/assetBundle'
import { INTERNAL_LANGUAGE_OPTIONS, TARGET_LANGUAGE_OPTIONS } from '@/constants/languages'

const internalLanguageOptions = INTERNAL_LANGUAGE_OPTIONS
const targetLanguageOptions = TARGET_LANGUAGE_OPTIONS

const route = useRoute()
const router = useRouter()

const formRef = ref();
const pageLoading = ref(false)
const saving = ref(false)
const agentId = computed(() => route.params.id)
const isEdit = computed(() => !!agentId.value)

const ragProductOptions = ref([]);
const sopOptions = ref([])
const scriptOptions = ref([])
const faqOptions = ref([])
const sopTemplateOptions = ref([])
const assetBundleOptions = ref([])

const kbTree = ref([]);
const loadingKB = ref(false)
const kbTreeProps = { value: 'id', label: 'label', children: 'children', isLeaf: 'isLeaf' }

const getDefaultForm = () => ({
  agent_code: '',
  name: '',
  description: '',
  avatar: '',
  agent_type: 'sales',
  agent_mode: 'passive',
  persona: '',
  system_prompt: '',
  greeting: '',
  asset_bundle_id: '',
  internal_language: 'zh',
  target_language: '',
  kb_ids: [],
  rag_product_ids: [],
  faq_entry_ids: [],
  sop_template_ids: [],
  sop_ids: [],
  script_library_ids: [],
  llm_model: 'gpt-4o-mini',
  temperature: 0.7,
  max_tokens: 800,
  top_p: 0.9,
  frequency_penalty: 0.5,
  presence_penalty: 0.5,
  enable_rag: true,
  enable_script_match: true,
  enable_humanize_polish: true,
  enable_content_audit: true,
  enable_playbook: true,
  rag_top_k: 3,
  confidence_threshold: 0.7,
  max_ai_consecutive: 5,
  status: 1
});

const form = reactive(getDefaultForm())

const rules = {
  agent_code: [
    { required: true, message: i18n.global.t('请输入智能体编码'), trigger: 'blur' },
    { pattern: /^[a-zA-Z0-9_\-]+$/, message: i18n.global.t('编码仅支持字母、数字、下划线、连字符'), trigger: 'blur' }
  ],
  name: [
    { required: true, message: i18n.global.t('请输入智能体名称'), trigger: 'blur' }
  ],
  agent_type: [
    { required: true, message: i18n.global.t('请选择智能体类型'), trigger: 'change' }
  ],
  persona: [
    { required: true, message: i18n.global.t('请输入人设描述'), trigger: 'blur' }
  ]
};

const testDialogVisible = ref(false);
const testing = ref(false)
const testForm = ref({ customer_id: '', message: '' })
const testResult = ref(null)

const getStepStatusType = (status) => {
  if (status === 'ok') return 'success'
  if (status === 'fail') return 'danger'
  if (status === 'skip') return 'info'
  return 'info'
}

const loadRagProducts = async () => {
  try {
    const res = await ragProductConfigAPI.getRagProducts({ page: 1, page_size: 100 })
    ragProductOptions.value = res?.items || res?.list || []
  } catch (e) {
    console.warn('加载RAG产品列表失败：', e?.message);
  }
};

const loadSopOptions = async () => {
  try {
    const res = await sopApi.list({ page: 1, page_size: 100 })
    sopOptions.value = res?.list || res?.items || []
  } catch (e) {
    console.warn('加载SOP列表失败：', e?.message)
  }
}

const loadScriptOptions = async () => {
  try {
    const res = await getScriptTemplateList({ page: 1, page_size: 100 })
    scriptOptions.value = res?.list || res?.items || []
  } catch (e) {
    console.warn('加载话术模板列表失败：', e?.message)
  }
}

const loadAssetBundles = async () => {
  try {
    const res = await listBundles({ page: 1, page_size: 200, status: 'active' })
    assetBundleOptions.value = res?.list || res?.items || []
  } catch (e) {
    console.warn('加载资产包列表失败：', e?.message)
  }
}

const loadFaqOptions = async () => {
  try {
    const res = await faqApi.list({ page: 1, page_size: 200 })
    faqOptions.value = res?.list || []
  } catch (e) {
    console.warn('加载 FAQ 列表失败：', e?.message)
  }
}

const loadSopTemplateOptions = async () => {
  try {
    const res = await sopTemplateApi.list({ page: 1, page_size: 100 })
    sopTemplateOptions.value = res?.list || []
  } catch (e) {
    console.warn('加载 SOP 模板列表失败：', e?.message)
  }
}

const buildKBTree = async () => {
  loadingKB.value = true
  try {
    const [ragRes, faqRes, sopRes] = await Promise.all([
      listKBByType('rag', { page: 1, page_size: 200 }).catch(() => null),
      listKBByType('faq', { page: 1, page_size: 200 }).catch(() => null),
      listKBByType('sop', { page: 1, page_size: 200 }).catch(() => null)
    ])
    const extractList = (r) => (r?.list || r?.items || (Array.isArray(r) ? r : []))
    const ragList = extractList(ragRes)
    const faqList = extractList(faqRes)
    const sopList = extractList(sopRes)
    const buildChildren = (list) => list.map((it) => ({
      id: it.id,
      label: `[${it.kb_code || '#' + it.id}] ${it.name}`,
      isLeaf: true,
      kb_type: it.kb_type,
      raw: it
    }))
    const ragChildren = buildChildren(ragList)
    const faqChildren = buildChildren(faqList)
    const sopChildren = buildChildren(sopList)
    const tree = []
    if (ragChildren.length) {
      tree.push({ id: 'kb-type-rag', label: `RAG 文档库 (${ragChildren.length})`, children: ragChildren, isLeaf: false, disabled: true, kb_type: 'rag' })
    }
    if (faqChildren.length) {
      tree.push({ id: 'kb-type-faq', label: `FAQ 知识库 (${faqChildren.length})`, children: faqChildren, isLeaf: false, disabled: true, kb_type: 'faq' })
    }
    if (sopChildren.length) {
      tree.push({ id: 'kb-type-sop', label: `SOP 模板库 (${sopChildren.length})`, children: sopChildren, isLeaf: false, disabled: true, kb_type: 'sop' })
    }
    if (tree.length === 0) {
      tree.push({ id: 'kb-empty', label: '暂未配置任何知识库', isLeaf: true, disabled: true });
    }
    kbTree.value = tree
    return tree
  } catch (e) {
    console.warn('加载知识库列表失败：', e?.message)
    kbTree.value = [{ id: 'kb-error', label: '加载失败：' + (e?.message || '未知错误'), isLeaf: true, disabled: true }]
    return []
  } finally {
    loadingKB.value = false
  }
};

const loadKnowledgeBases = async () => {
  await buildKBTree()
  if (isEdit.value && agentId.value) {
    await loadAgentKBBindings()
  }
}

const loadAgentKBBindings = async () => {
  try {
    const res = await listAgentKBs(agentId.value)
    const list = res?.list || res?.items || (Array.isArray(res) ? res : [])
    form.kb_ids = list.map((it) => it.kb_id ?? it.id)
  } catch (e) {
    console.warn('加载智能体知识库绑定失败：', e?.message)
    form.kb_ids = []
  }
};

const loadAgentDetail = async () => {
  if (!isEdit.value) return
  pageLoading.value = true
  try {
    const res = await getAgent(agentId.value)
    if (res) {
      Object.assign(form, getDefaultForm(), res)
      form.rag_product_ids = (res.rag_product_ids || []).map(String);
      form.faq_entry_ids = (res.faq_entry_ids || []).map(String)
      form.sop_template_ids = (res.sop_template_ids || []).map(String)
      form.sop_ids = (res.sop_ids || []).map(String)
      form.script_library_ids = (res.script_library_ids || []).map(String)
      form.kb_ids = (res.kb_ids || []).map(String)
      form.internal_language = res.internal_language || 'zh';
      form.target_language = res.target_language || ''
    }
  } catch (e) {
    ElMessage.error('加载智能体详情失败：' + (e.message || '未知错误'))
    goBack()
  } finally {
    pageLoading.value = false
  }
};

const onSave = async () => {
  if (!formRef.value) return
  await formRef.value.validate(async (valid) => {
    if (!valid) {
      ElMessage.warning(i18n.global.t('请完善必填项后再提交'))
      return
    }
    saving.value = true
    try {
      const data = {
        agent_code: form.agent_code,
        name: form.name,
        description: form.description,
        avatar: form.avatar,
        agent_type: form.agent_type,
        persona: form.persona,
        system_prompt: form.system_prompt,
        greeting: form.greeting,
        internal_language: form.internal_language || 'zh',
        target_language: form.target_language || '',
        kb_ids: form.kb_ids || [],
        rag_product_ids: form.rag_product_ids || [],
        faq_entry_ids: form.faq_entry_ids || [],
        sop_template_ids: form.sop_template_ids || [],
        sop_ids: form.sop_ids || [],
        script_library_ids: form.script_library_ids || [],
        llm_model: form.llm_model,
        temperature: form.temperature,
        max_tokens: form.max_tokens,
        top_p: form.top_p,
        frequency_penalty: form.frequency_penalty,
        presence_penalty: form.presence_penalty,
        enable_rag: form.enable_rag,
        enable_script_match: form.enable_script_match,
        enable_humanize_polish: form.enable_humanize_polish,
        enable_content_audit: form.enable_content_audit,
        enable_playbook: form.enable_playbook,
        rag_top_k: form.rag_top_k,
        confidence_threshold: form.confidence_threshold,
        max_ai_consecutive: form.max_ai_consecutive,
        status: form.status
      }
      if (isEdit.value) {
        await updateAgent(agentId.value, data)
        try {
          await replaceAgentKBs(agentId.value, form.kb_ids.map(String))
        } catch (kbErr) {
          console.warn('同步智能体知识库挂载失败：', kbErr?.message)
        }
        ElMessage.success(i18n.global.t('智能体更新成功'))
      } else {
        const created = await createAgent(data)
        const newId = created?.id || created?.ID;
        if (newId && form.kb_ids?.length) {
          try {
            await replaceAgentKBs(newId, form.kb_ids.map(String))
          } catch (kbErr) {
            console.warn('挂载智能体知识库失败：', kbErr?.message)
          }
        }
        ElMessage.success(i18n.global.t('智能体创建成功'))
      }
      goBack()
    } catch (e) {
      ElMessage.error('保存失败：' + (e.message || '未知错误'))
    } finally {
      saving.value = false
    }
  })
};

const goBack = () => {
  router.push('/aiAgent/list')
}

const openTestDialog = () => {
  if (isEdit.value && !form.id) {
    ElMessage.warning(i18n.global.t('智能体信息加载中，请稍候'))
    return
  }
  if (!isEdit.value) {
    ElMessage.warning(i18n.global.t('请先创建智能体后再进行测试'))
    return
  }
  testForm.value = { customer_id: '', message: '' }
  testResult.value = null
  testDialogVisible.value = true
};

const runTest = async () => {
  if (!testForm.value.message || !testForm.value.message.trim()) {
    ElMessage.warning(i18n.global.t('请输入消息内容'))
    return
  }
  testing.value = true
  testResult.value = null
  try {
    const data = {
      customer_id: testForm.value.customer_id || '',
      message: testForm.value.message
    }
    const res = await testAgent(agentId.value, data)
    testResult.value = res
    ElMessage.success(i18n.global.t('测试执行完成'))
  } catch (e) {
    ElMessage.error('测试失败：' + (e.message || '未知错误'))
  } finally {
    testing.value = false
  }
}

onMounted(async () => {
  await Promise.all([loadRagProducts(), loadSopOptions(), loadScriptOptions(), loadAssetBundles()])
  if (isEdit.value) {
    await loadAgentDetail()
  }
  buildKBTree().then(() => {
    if (isEdit.value) loadAgentKBBindings()
  });
});
</script>

<style scoped lang="scss">
.ai-agent-edit-page { padding: 20px; }

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

.section-card {
  margin-bottom: 16px;
  .card-title {
    display: flex;
    align-items: center;
    gap: 6px;
    font-weight: 600;
    font-size: 15px;
    color: #303133;
  }
}

.form-tip {
  font-size: 12px;
  color: #909399;
  line-height: 1.5;
  margin-top: 4px;
}

.footer-actions {
  display: flex;
  justify-content: center;
  gap: 12px;
  padding: 16px 0;
}

.reply-box {
  white-space: pre-wrap;
  word-break: break-word;
  background: #f5f7fa;
  padding: 10px 12px;
  border-radius: 4px;
  line-height: 1.6;
  max-height: 200px;
  overflow-y: auto;
}

.transfer-reason { color: #F59E0B; margin-left: 6px; }

.error-text { color: #EF4444; }

.chain-title {
  margin: 16px 0 8px 0;
  font-weight: 600;
  color: #303133;
  font-size: 14px;
}
</style>
