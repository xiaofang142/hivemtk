<template>
  <div class="sop-market-page">
    
    <el-card class="header-card" shadow="never">
      <div class="header-content">
        <div>
          <h2>SOP 模板市场</h2>
          <p class="subtitle">浏览、搜索、一键导入预置 SOP 模板，覆盖 5 大业务场景</p>
        </div>
        <div class="header-stats">
          <el-tag type="info" size="large">
            <el-icon><Collection /></el-icon>
            共 {{ builtInTemplates.length }} 个内置模板
          </el-tag>
        </div>
      </div>
    </el-card>

    
    <el-card shadow="never" class="filter-card">
      <el-form :inline="true" :model="filter" @submit.prevent>
        <el-form-item label="关键词">
          <el-input
            v-model="filter.keyword"
            placeholder="搜索模板名称/描述"
            clearable
            style="width: 220px"
            @keyup.enter="onSearch"
            @clear="onSearch"
          />
        </el-form-item>
        <el-form-item label="业务场景">
          <el-select
            v-model="filter.category"
            placeholder="全部场景"
            clearable
            style="width: 180px"
            @change="onSearch"
          >
            <el-option
              v-for="c in categories"
              :key="c.value"
              :label="c.label"
              :value="c.value"
            />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="onSearch">
            <el-icon><Search /></el-icon>
            搜索
          </el-button>
          <el-button @click="resetFilter">重置</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    
    <div class="template-grid" v-loading="loading">
      <el-empty
        v-if="!loading && filteredTemplates.length === 0"
        description="未找到匹配的模板"
      />
      <el-card
        v-for="tpl in filteredTemplates"
        :key="tpl.code"
        class="tpl-card"
        shadow="hover"
      >
        <div class="tpl-card-header">
          <div class="tpl-icon" :style="{ background: tpl.color }">
            <el-icon :size="24"><component :is="tpl.icon" /></el-icon>
          </div>
          <div class="tpl-title-area">
            <h3 class="tpl-title">{{ tpl.name }}</h3>
            <el-tag :type="getCategoryTagType(tpl.category)" size="small">
              {{ getCategoryLabel(tpl.category) }}
            </el-tag>
          </div>
        </div>

        <p class="tpl-desc">{{ tpl.description }}</p>

        <div class="tpl-scenarios">
          <span class="scenario-label">适用场景：</span>
          <el-tag
            v-for="sc in tpl.scenarios"
            :key="sc"
            type="info"
            size="small"
            effect="plain"
            class="scenario-tag"
          >
            {{ sc }}
          </el-tag>
        </div>

        <div class="tpl-stats">
          <div class="stat-item">
            <span class="stat-label">意图</span>
            <el-tag size="small" type="warning">{{ tpl.intent }}</el-tag>
          </div>
          <div class="stat-item">
            <span class="stat-label">阶段</span>
            <el-tag size="small" type="info">{{ tpl.stage }}</el-tag>
          </div>
        </div>

        <div class="tpl-preview">
          <div class="preview-label">模板预览：</div>
          <pre class="preview-content">{{ truncate(tpl.template, 140) }}</pre>
        </div>

        <div class="tpl-actions">
          <el-button
            v-if="tpl.imported"
            type="success"
            disabled
            size="default"
            style="flex: 1"
          >
            <el-icon><Check /></el-icon>
            已导入
          </el-button>
          <el-button
            v-else
            type="primary"
            size="default"
            :loading="tpl._importing"
            style="flex: 1"
            @click="onImport(tpl)"
          >
            <el-icon><Download /></el-icon>
            一键导入
          </el-button>
          <el-button
            size="default"
            @click="showDetail(tpl)"
          >
            <el-icon><View /></el-icon>
            详情
          </el-button>
        </div>
      </el-card>
    </div>

    
    <el-dialog
      v-model="detailVisible"
      :title="currentTpl?.name || '模板详情'"
      width="720px"
      :close-on-click-modal="false"
    >
      <template v-if="currentTpl">
        <el-descriptions :column="2" border>
          <el-descriptions-item label="模板编码">{{ currentTpl.code }}</el-descriptions-item>
          <el-descriptions-item label="业务场景">
            {{ getCategoryLabel(currentTpl.category) }}
          </el-descriptions-item>
          <el-descriptions-item label="意图标签">{{ currentTpl.intent }}</el-descriptions-item>
          <el-descriptions-item label="对话阶段">{{ currentTpl.stage }}</el-descriptions-item>
          <el-descriptions-item label="优先级">{{ currentTpl.priority }}</el-descriptions-item>
          <el-descriptions-item label="最低置信度">{{ (currentTpl.confidence * 100).toFixed(0) }}%</el-descriptions-item>
          <el-descriptions-item label="描述" :span="2">
            {{ currentTpl.description }}
          </el-descriptions-item>
        </el-descriptions>

        <h4 style="margin-top: 16px">模板内容：</h4>
        <pre class="full-template">{{ currentTpl.template }}</pre>

        <h4 style="margin-top: 16px">变量说明：</h4>
        <ul class="vars-list">
          <li v-for="(v, idx) in currentTpl.vars" :key="idx">
            <code v-text="'{{' + v.name + '}}'"></code> - {{ v.desc }}
          </li>
        </ul>
      </template>

      <template #footer>
        <el-button @click="detailVisible = false">关闭</el-button>
        <el-button
          v-if="currentTpl && !currentTpl.imported"
          type="primary"
          :loading="currentTpl._importing"
          @click="onImport(currentTpl)"
        >
          一键导入
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, markRaw, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  Collection,
  Search,
  View,
  Download,
  Check,
  Phone,
  Service,
  Present,
  Sunny,
  UserFilled,
  ChatLineRound
} from '@element-plus/icons-vue'
import { sopTemplateApi } from '@/api/sopTemplate'

const builtInTemplates = ref([
  {
    code: 'pre_sale_followup',
    name: '售前跟进 SOP',
    description: '客户首次咨询后的标准化跟进流程，覆盖介绍产品、了解需求、引导试用三个阶段',
    category: 'presale',
    // icon 是 Vue 组件对象。builtInTemplates 是深响应 ref（下面还要就地写 t.imported，
    // 不能改用 shallowRef，否则模板 :116 的 v-if="tpl.imported" 会失去响应性），
    // 组件对象落进去会被 reactive 代理，模板 <component :is="tpl.icon" /> 取到代理组件
    // 就会触发 "Vue received a Component that was made a reactive object"（实测 109 条）。
    // markRaw 与 dashboardScreen/List.vue 同一修法。
    icon: markRaw(Phone),
    color: 'linear-gradient(135deg, #667eea 0%, #764ba2 100%)',
    intent: 'inquiry',
    stage: 'initial',
    priority: 80,
    confidence: 0.85,
    imported: false,
    _importing: false,
    scenarios: ['新客咨询', '产品介绍', '需求摸底'],
    template: `您好 {{customer_name}}，感谢您对{{product_name}}的关注！
我是{{agent_name}}，专注于为{{industry}}客户提供解决方案。

针对您刚才咨询的{{topic}}，我整理了以下资料：
1. 产品功能介绍文档
2. 同行业客户案例（{{case_count}}个）
3. 试用账号（有效期7天）

请问您方便本周{{time_slot}}安排一次 15 分钟的线上沟通吗？`,
    vars: [
      { name: 'customer_name', desc: '客户称呼' },
      { name: 'product_name', desc: '产品名称' },
      { name: 'agent_name', desc: '客服名称' },
      { name: 'industry', desc: '客户行业' },
      { name: 'topic', desc: '咨询主题' },
      { name: 'case_count', desc: '案例数' },
      { name: 'time_slot', desc: '可预约时段' }
    ]
  },
  {
    code: 'aftersale_service',
    name: '售后服务 SOP',
    description: '客户报修/退款/咨询的标准响应流程，确保 1 小时首响应、24 小时解决方案',
    category: 'aftersale',
    icon: markRaw(Service),
    color: 'linear-gradient(135deg, #f093fb 0%, #f5576c 100%)',
    intent: 'support',
    stage: 'middle',
    priority: 90,
    confidence: 0.9,
    imported: false,
    _importing: false,
    scenarios: ['问题反馈', '退换货', '技术咨询'],
    template: `{{customer_name}}您好，我是售后客服{{agent_name}}。

您反馈的{{issue_type}}问题，工单号 #{{ticket_id}}，我们已收到。
当前处理进度：
- 处理人：{{handler}}
- 预计解决时间：{{eta}}
- 补偿方案：{{compensation}}

如有任何疑问，请直接回复本消息，我会第一时间跟进。
您也可以拨打 400-xxx-xxxx 转工单号查询进度。`,
    vars: [
      { name: 'customer_name', desc: '客户称呼' },
      { name: 'agent_name', desc: '客服名称' },
      { name: 'issue_type', desc: '问题类型' },
      { name: 'ticket_id', desc: '工单号' },
      { name: 'handler', desc: '处理人' },
      { name: 'eta', desc: '预计解决时间' },
      { name: 'compensation', desc: '补偿方案' }
    ]
  },
  {
    code: 'festival_marketing',
    name: '节日营销 SOP',
    description: '春节/双11/618 等大促节点的批量触达模板，集成优惠券 + 个性化推荐',
    category: 'marketing',
    icon: markRaw(Present),
    color: 'linear-gradient(135deg, #fa709a 0%, #fee140 100%)',
    intent: 'purchase',
    stage: 'initial',
    priority: 70,
    confidence: 0.75,
    imported: false,
    _importing: false,
    scenarios: ['节日促销', '活动通知', '优惠提醒'],
    template: `🎉 {{festival_name}} 限时特惠！

亲爱的{{customer_name}}：
{{festival_name}}专属福利已到账，错过等一年 ✨

【尊享特权】
- 全场满 {{threshold}} 减 {{discount}}
- 爆款商品低至 5 折
- 专属客服 1v1 服务

👉 立即抢购：[点击领取优惠券 {{coupon_code}}]
⏰ 活动截止：{{deadline}}

祝您和家人{{festival_name}}快乐！`,
    vars: [
      { name: 'festival_name', desc: '节日名' },
      { name: 'customer_name', desc: '客户称呼' },
      { name: 'threshold', desc: '满减门槛' },
      { name: 'discount', desc: '减免金额' },
      { name: 'coupon_code', desc: '优惠券码' },
      { name: 'deadline', desc: '截止时间' }
    ]
  },
  {
    code: 'holiday_greeting',
    name: '节假日问候 SOP',
    description: '元旦/中秋/端午等传统节日的温情问候，提升品牌温度与客户粘性',
    category: 'marketing',
    icon: markRaw(Sunny),
    color: 'linear-gradient(135deg, #4facfe 0%, #00f2fe 100%)',
    intent: 'greeting',
    stage: 'initial',
    priority: 50,
    confidence: 0.7,
    imported: false,
    _importing: false,
    scenarios: ['节日问候', '客户关怀', '品牌建设'],
    template: `🌕 {{customer_name}}，{{holiday_name}}快乐！

值此{{holiday_name}}佳节，{{company_name}}全体员工
向您和家人致以最诚挚的祝福 🎊

【节日小贴士】
{{tip}}

我们将继续为您提供更优质的服务，
期待与您在新的一年继续同行！

——{{company_name}}`,
    vars: [
      { name: 'customer_name', desc: '客户称呼' },
      { name: 'holiday_name', desc: '节日名' },
      { name: 'company_name', desc: '公司名' },
      { name: 'tip', desc: '节日小贴士' }
    ]
  },
  {
    code: 'new_customer_conversion',
    name: '新客转化 SOP',
    description: '首次注册到首次付费的 7 日引导流程，含注册、激活、试用、付费 4 个关键触点',
    category: 'presale',
    icon: markRaw(UserFilled),
    color: 'linear-gradient(135deg, #43e97b 0%, #38f9d7 100%)',
    intent: 'follow_up',
    stage: 'late',
    priority: 85,
    confidence: 0.8,
    imported: false,
    _importing: false,
    scenarios: ['新客激活', '试用引导', '首单转化'],
    template: `Hi {{customer_name}}，欢迎加入{{product_name}}！

我们注意到您已经完成了第{{step}}/4 步：
{{progress_bar}}

【下一步建议】{{next_step}}

📌 新客专属福利：
- 14 天专业版试用
- 1 对 1 培训（30 分钟）
- 首单立减 {{discount}} 元

立即体验：[点击开始 {{next_action}}]

有任何问题随时找我，我是{{agent_name}}。`,
    vars: [
      { name: 'customer_name', desc: '客户称呼' },
      { name: 'product_name', desc: '产品名' },
      { name: 'step', desc: '当前步骤 (1-4)' },
      { name: 'progress_bar', desc: '进度条' },
      { name: 'next_step', desc: '下一步建议' },
      { name: 'discount', desc: '首单优惠' },
      { name: 'next_action', desc: '下一步动作' },
      { name: 'agent_name', desc: '客服名称' }
    ]
  }
]);

const loading = ref(false);
const filter = ref({ keyword: '', category: '' })
const detailVisible = ref(false)
const currentTpl = ref(null)

const categories = [
  { value: 'presale', label: '售前' },
  { value: 'aftersale', label: '售后' },
  { value: 'marketing', label: '营销' }
]

const filteredTemplates = computed(() => {
  let list = builtInTemplates.value
  if (filter.value.keyword) {
    const kw = filter.value.keyword.toLowerCase()
    list = list.filter(
      (t) =>
        t.name.toLowerCase().includes(kw) ||
        t.description.toLowerCase().includes(kw)
    )
  }
  if (filter.value.category) {
    list = list.filter((t) => t.category === filter.value.category)
  }
  return list
});

const truncate = (text, len) => {
  if (!text) return '-'
  return text.length > len ? text.slice(0, len) + '...' : text
};

const getCategoryLabel = (cat) => {
  const c = categories.find((x) => x.value === cat)
  return c ? c.label : cat
}

const getCategoryTagType = (cat) => {
  if (cat === 'presale') return 'warning'
  if (cat === 'aftersale') return 'danger'
  if (cat === 'marketing') return 'success'
  return 'info'
}

const loadImportedState = async () => {
  try {
    const res = await sopTemplateApi.list({ page: 1, page_size: 100 }).catch(() => null)
    const items = res?.list || []
    const importedCodes = new Set(
      items
        .filter((it) => it.code)
        .map((it) => it.code)
    )
    builtInTemplates.value.forEach((t) => {
      t.imported = importedCodes.has(t.code)
    })
  } catch (e) {
    console.warn('加载已导入状态失败', e)
  }
};

const onSearch = () => {};

const resetFilter = () => {
  filter.value = { keyword: '', category: '' }
}

const showDetail = (tpl) => {
  currentTpl.value = tpl
  detailVisible.value = true
}

const onImport = async (tpl) => {
  tpl._importing = true
  try {
    await sopTemplateApi.create({
      code: tpl.code,
      name: tpl.name,
      intent: tpl.intent,
      stage: tpl.stage,
      template: tpl.template,
      vars: JSON.stringify(tpl.vars),
      priority: tpl.priority,
      confidence: tpl.confidence,
      enabled: true
    })
    tpl.imported = true
    ElMessage.success(`「${tpl.name}」已成功导入！`)
    if (detailVisible.value) {
      detailVisible.value = false
    }
  } catch (e) {
    ElMessage.error('导入失败：' + (e?.message || '未知错误'))
  } finally {
    tpl._importing = false
  }
}

onMounted(() => {
  loadImportedState()
})
</script>

<style scoped>
.sop-market-page {
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

.filter-card :deep(.el-card__body) {
  padding: 16px 20px 0 20px;
}

.template-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(360px, 1fr));
  gap: 16px;
  padding: 4px;
}

.tpl-card {
  display: flex;
  flex-direction: column;
  transition: transform 0.2s;
}

.tpl-card:hover {
  transform: translateY(-2px);
}

.tpl-card-header {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
}

.tpl-icon {
  width: 48px;
  height: 48px;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  flex-shrink: 0;
}

.tpl-title-area {
  flex: 1;
  min-width: 0;
}

.tpl-title {
  margin: 0 0 4px 0;
  font-size: 16px;
  font-weight: 600;
}

.tpl-desc {
  margin: 0 0 12px 0;
  color: var(--el-text-color-regular);
  font-size: 13px;
  line-height: 1.6;
  min-height: 40px;
}

.tpl-scenarios {
  margin-bottom: 12px;
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  align-items: center;
}

.scenario-label {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-right: 4px;
}

.scenario-tag {
  margin: 0;
}

.tpl-stats {
  display: flex;
  gap: 16px;
  margin-bottom: 12px;
  padding: 8px 12px;
  background: var(--el-fill-color-light);
  border-radius: 4px;
}

.stat-item {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
}

.stat-label {
  color: var(--el-text-color-secondary);
}

.tpl-preview {
  margin-bottom: 12px;
  flex: 1;
}

.preview-label {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-bottom: 4px;
}

.preview-content {
  margin: 0;
  padding: 8px 12px;
  background: #f5f7fa;
  border-radius: 4px;
  font-size: 12px;
  line-height: 1.6;
  color: #606266;
  font-family: 'SF Mono', Monaco, Consolas, monospace;
  max-height: 100px;
  overflow: hidden;
}

.tpl-actions {
  display: flex;
  gap: 8px;
  margin-top: 8px;
}

.full-template {
  margin: 0;
  padding: 12px;
  background: #f5f7fa;
  border-radius: 4px;
  font-size: 13px;
  line-height: 1.7;
  white-space: pre-wrap;
  word-break: break-word;
  max-height: 320px;
  overflow-y: auto;
}

.vars-list {
  margin: 8px 0 0 0;
  padding-left: 20px;
  font-size: 13px;
  line-height: 1.8;
}

.vars-list code {
  background: #f0f9ff;
  color: #1890ff;
  padding: 2px 6px;
  border-radius: 3px;
  font-size: 12px;
}
</style>
