<template>
  <el-container class="app-shell">
    
    <MessageNotification />

    
    <el-header class="app-header">
      <div class="brand">
        <span class="brand-mark"></span>
        <span class="brand-text">{{ t('system.appName') }}</span>
      </div>

      <el-menu
        :default-active="activeTopMenu"
        mode="horizontal"
        :ellipsis="false"
        @select="handleTopMenuSelect"
        class="top-menu"
      >
        <el-menu-item
          v-for="menu in filteredTopMenus"
          :key="menu.key"
          :index="menu.key"
        >
          <el-icon><component :is="iconComponents[menu.icon]" /></el-icon>
          <span>{{ t('menu.' + menu.key) }}</span>
        </el-menu-item>
      </el-menu>

      
      <div v-if="userStore.isLoggedIn" class="notif-bell" @click="router.push({ name: 'Notifications' })">
        <el-badge :value="unreadCount" :hidden="unreadCount === 0" :max="99">
          <el-icon :size="20"><Bell /></el-icon>
        </el-badge>
      </div>

      <div v-if="userStore.isLoggedIn" class="notif-bell geo-alert-bell"
        @click="router.push('/geo-tools/alerts')"
        :title="`GEO 告警：${geoAlertCount} 条未确认`">
        <el-badge :value="geoAlertCount" :hidden="geoAlertCount === 0" :max="99">
          <el-icon :size="20" color="#e6a23c"><Warning /></el-icon>
        </el-badge>
      </div>

      
      <div class="user-area" v-if="userStore.isLoggedIn">
        <LanguageSwitcher />
        <el-dropdown @command="handleUserCommand" trigger="click">
          <span class="user-dropdown">
            <el-avatar :size="30" class="user-avatar">{{ userStore.username?.charAt(0) || 'U' }}</el-avatar>
            <span class="user-name">{{ userStore.username }}</span>
            <el-icon class="el-icon--right"><arrow-down /></el-icon>
          </span>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="profile">
                <el-icon><User /></el-icon>{{ t('layout.profile') }}
              </el-dropdown-item>
              <el-dropdown-item command="help-center">
                <el-icon><QuestionFilled /></el-icon>帮助中心
              </el-dropdown-item>
              <el-dropdown-item command="logout" divided>
                <el-icon><SwitchButton /></el-icon>{{ t('layout.logout') }}
              </el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </div>

      
      <div class="login-button" v-else @click="handleLogin">
        <el-icon><User /></el-icon>
        <span>{{ t('layout.login') }}</span>
      </div>
    </el-header>

    <el-container class="app-body">
      
      <el-aside :width="sidebarCollapsed ? '64px' : '240px'" v-if="currentTopMenu && currentTopMenu.children" class="app-aside" :class="{ 'is-collapsed': sidebarCollapsed }">
        <div class="aside-brand">
          <el-icon v-show="!sidebarCollapsed"><Menu /></el-icon>
          <span v-show="!sidebarCollapsed">{{ t('menu.' + currentTopMenu.key) }}</span>
          <el-button
            class="collapse-btn"
            :class="{ 'collapse-btn--collapsed': sidebarCollapsed }"
            text
            :title="sidebarCollapsed ? t('layout.expandMenu') : t('layout.collapseMenu')"
            @click="toggleSidebar"
          >
            <el-icon><Fold v-if="!sidebarCollapsed" /><Expand v-else /></el-icon>
          </el-button>
        </div>
        <el-scrollbar>
          <el-menu
            :default-active="route.path"
            @select="handleSubMenuSelect"
            :unique-opened="false"
            :collapse="sidebarCollapsed"
            :collapse-transition="true"
            mode="vertical"
          >
            <template v-for="subMenu in currentTopMenu.children" :key="subMenu.key">
              <sub-menu-item :menu="subMenu" :icon-components="iconComponents" />
            </template>
          </el-menu>
        </el-scrollbar>

        
        <div class="license-info" v-if="licenseInfo && !sidebarCollapsed">
          <div class="license-expiry" :class="{ 'expired': isLicenseExpired }">
            <el-icon><Timer /></el-icon>
            <span>{{ t('layout.licenseExpiry') }}: {{ formattedExpiryTime }}</span>
          </div>
          <div class="license-note" v-if="isLicenseExpired">
            <el-icon><Warning /></el-icon>
            <span>{{ t('layout.contactForLicense') }}</span>
          </div>
          <div class="license-note" v-else>
            <el-icon><InfoFilled /></el-icon>
            <span>{{ t('layout.freeTrialHint') }}</span>
          </div>
        </div>
      </el-aside>

      
      <el-main class="app-main">
        <div class="breadcrumb-wrap" v-if="breadcrumb.length">
          <el-breadcrumb separator="/">
            <el-breadcrumb-item v-for="(b, i) in breadcrumb" :key="i">{{ b }}</el-breadcrumb-item>
          </el-breadcrumb>
        </div>
        <router-view v-slot="{ Component }">
          <transition name="fade">
            <component :is="Component" :key="$route.fullPath" />
          </transition>
        </router-view>
      </el-main>
    </el-container>
  </el-container>
</template>

<script setup>
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import SubMenuItem from '@/components/SubMenuItem.vue'
import MessageNotification from '@/components/MessageNotification.vue'
import LanguageSwitcher from '@/components/LanguageSwitcher.vue'
import i18n from '@/i18n'
import { useUserStore } from '@/stores/user'
import { getLicenseStatus } from '@/api/license'
import { Timer, Warning, InfoFilled, Bell, Menu, SwitchButton, Fold, Expand, QuestionFilled } from '@element-plus/icons-vue'
import { routeIconMap } from '@/utils/iconMap'
void Bell; void Timer; void Warning; void InfoFilled; void Menu; void SwitchButton; void Fold; void Expand

const iconComponents = routeIconMap;

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()
const t = i18n.global.t

const activeTopMenu = ref('')
const activeSubMenu = ref(route.path)
const unreadCount = ref(0)
const geoAlertCount = ref(0)
let geoAlertTimer = null
import { getGeoAlertsUnreadCount } from '@/api/geoAlert'
const loadGeoAlertCount = async () => {
  if (!userStore.isLoggedIn) return
  try {
    const res = await getGeoAlertsUnreadCount()
    const data = res?.data || res
    geoAlertCount.value = Number(data?.count || 0)
  } catch { /* 静默失败：铃铛角标非关键路径 */ }
}
const SIDEBAR_COLLAPSED_KEY = 'hivemtk_sidebar_collapsed'
const readSidebarCollapsed = () => {
  try {
    return localStorage.getItem(SIDEBAR_COLLAPSED_KEY) === '1'
  } catch {
    return false
  }
}
const sidebarCollapsed = ref(readSidebarCollapsed())
const persistSidebarCollapsed = () => {
  try {
    localStorage.setItem(SIDEBAR_COLLAPSED_KEY, sidebarCollapsed.value ? '1' : '0')
  } catch { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
}

const licenseInfo = ref(null);
const isLicenseExpired = computed(() => {
  if (!licenseInfo.value || !licenseInfo.value.expire_at) return false
  const expiryTime = new Date(licenseInfo.value.expire_at).getTime()
  const currentTime = new Date().getTime()
  return currentTime > expiryTime
})

const formattedExpiryTime = computed(() => {
  const info = licenseInfo.value
  if (!info) return ''
  if (info.expire_at) {
    const date = new Date(info.expire_at)
    const loc = i18n.global.locale.value
    const localeMap = { zh: 'zh-CN', en: 'en-US', ja: 'ja-JP', ar: 'ar-SA' }
    return date.toLocaleString(localeMap[loc] || 'zh-CN')
  }
  if (info.message)
    return info.message;
  if (info.status === 'active' || info.licensed) return t('layout.licenseActive')
  return t('layout.unknown')
});

const loadLicenseInfo = async () => {
  try {
    const response = await getLicenseStatus({ _silent: true })
    if (response) licenseInfo.value = response
  } catch (error) { console.warn("[request] 后台调用失败(已忽略):", error) }
};

const topMenus = ref([
  {
    key: 'workspace',
    title: '工作台',
    icon: 'Monitor',
    roles: ['admin', 'manager', 'sales', 'viewer', 'staff'],
    children: [
      { key: 'messageHub', title: '消息中台 MQ', icon: 'MessageBox', path: '/messageHub/list', roles: ['admin', 'manager'] },
      { key: 'messageHubDashboard', title: '消息中台看板', icon: 'DataBoard', path: '/messageHub/dashboard', roles: ['admin', 'manager'] },
      { key: 'inbox', title: '统一收件箱', icon: 'Box', path: '/inbox/list' },
      { key: 'salesCockpit', title: '销售驾驶舱', icon: 'DataLine', path: '/sales-cockpit', roles: ['admin', 'manager', 'sales'] },
      { key: 'wecomAccount', title: '多账号聚合', icon: 'Connection', path: '/wecomAccount/list', roles: ['admin', 'manager'] }
    ]
  },
  {
    key: 'customer',
    title: '客户中心',
    icon: 'UserFilled',
    roles: ['admin', 'manager', 'sales', 'viewer', 'staff'],
    children: [
      { key: 'clueList', title: '线索列表', icon: 'Document', path: '/clue/list' },
      { key: 'clueStatistics', title: '线索统计', icon: 'DataAnalysis', path: '/clue/statistics' },
      { key: 'leadMining', title: '线索挖掘', icon: 'Search', path: '/lead-mining/index' },
      { key: 'customer360', title: '客户 360', icon: 'UserFilled', path: '/customer360/list' },
      { key: 'customerEvent', title: '客户事件', icon: 'Bell', path: '/customerEvent/list' },
      { key: 'tagSegmentation', title: '标签分层', icon: 'PriceTag', path: '/tagSegmentation/list', roles: ['admin', 'manager', 'viewer'] },
      { key: 'userSegment', title: '用户分层 RFM', icon: 'PieChart', path: '/userSegment/list', roles: ['admin', 'manager', 'viewer'] },
      { key: 'unifiedMessage', title: '统一消息', icon: 'Message', path: '/unifiedMessage/list' },
      {
        key: 'customerIdentity',
        title: '客户身份',
        icon: 'Connection',
        roles: ['admin', 'manager'],
        children: [
          {
            key: 'oneidList',
            title: 'OneID 列表',
            icon: 'List',
            path: '/oneid/list'
          },
          {
            key: 'oneidConflicts',
            title: '身份冲突解决',
            icon: 'Warning',
            path: '/oneid/conflicts'
          },
          {
            key: 'oneidMergeRules',
            title: '合并规则配置',
            icon: 'Setting',
            path: '/oneid/merge-rules'
          }
        ]
      }
    ]
  },
  {
    key: 'aiAgent',
    title: '智能体',
    icon: 'Cpu',
    roles: ['admin', 'manager', 'customer_service', 'staff'],
    children: [
      {
        key: 'agentManage',
        title: '智能体管理',
        icon: 'Cpu',
        children: [
          { key: 'aiAgentList', title: '智能体列表', icon: 'List', path: '/aiAgent/list' },
          { key: 'aiAgentCreate', title: '创建智能体', icon: 'Plus', path: '/aiAgent/create' }
        ]
      },
      {
        key: 'agentPassive',
        title: '被动应答',
        icon: 'ChatDotRound',
        children: [
          {
            key: 'customerSession',
            title: '客服会话',
            icon: 'Service',
            path: '/customerSession/list'
          },
          { key: 'intentRecognition', title: '意图识别', icon: 'Aim', path: '/intentRecognition/list' },
          { key: 'dialogueMemory', title: '对话记忆', icon: 'ChatDotRound', path: '/dialogueMemory/list' },
          { key: 'llmRouting', title: 'LLM 路由', icon: 'Cpu', path: '/llmRouting/list' }
        ]
      },
      {
        key: 'agentActive',
        title: '主动触达',
        icon: 'Promotion',
        children: [
          { key: 'reachPipeline', title: '触达 Pipeline', icon: 'Promotion', path: '/reachPipeline/list', roles: ['admin', 'manager'] },
          { key: 'marketingFlow', title: '营销流程', icon: 'SetUp', path: '/marketingFlow/list', roles: ['admin', 'manager'] },
          { key: 'batchOperation', title: '批量运营', icon: 'Operation', path: '/batchOperation/list' }
        ]
      },
      {
        key: 'agentSupport',
        title: '能力支撑',
        icon: 'Connection',
        children: [
          { key: 'sopAgent', title: 'SOP 智能体', icon: 'Connection', path: '/sopAgent/list' },
          { key: 'sopTemplate', title: 'SOP 模板库', icon: 'Tickets', path: '/sop-template/list' },
          { key: 'sopTemplateMarket', title: 'SOP 模板市场', icon: 'ShoppingCart', path: '/sop-template/market' },
          { key: 'faqKb', title: 'FAQ 知识库', icon: 'Notebook', path: '/faq/list' },
          { key: 'knowledgeBase', title: '知识库管理', icon: 'Files', path: '/knowledgeBase' },
          { key: 'scriptTemplate', title: '销冠话术库', icon: 'ChatLineSquare', path: '/scriptTemplate/list' },
          { key: 'scriptTemplateAbStats', title: '话术 AB 统计', icon: 'DataLine', path: '/scriptTemplate/ab-stats' },
          { key: 'aiToolManagement', title: 'AI 工具管理', icon: 'Tools', path: '/aiAgent/tools' }
        ]
      },
      {
        key: 'agentWorkbench',
        title: '客服工作台',
        icon: 'Headset',
        roles: ['admin', 'manager', 'sales', 'customer_service'],
        children: [
          { key: 'agentStatus', title: '坐席状态', icon: 'Headset', path: '/customerService/agentStatus' },
          { key: 'csatDashboard', title: 'CSAT 看板', icon: 'Star', path: '/customerService/csat' },
          { key: 'quickReply', title: '快捷回复', icon: 'ChatLineSquare', path: '/customerService/quickReply' },
          { key: 'sessionTag', title: '会话标签', icon: 'CollectionTag', path: '/customerService/sessionTag' },
          { key: 'aiSuggestion', title: 'AI 建议', icon: 'MagicStick', path: '/customerService/aiSuggestion' }
        ]
      },
      {
        key: 'agentChannel',
        title: '客服渠道',
        icon: 'Connection',
        roles: ['admin', 'manager'],
        children: [
          { key: 'chatChannelList', title: '客服渠道', icon: 'Connection', path: '/chatChannel/list' },
          { key: 'whatsappCloud', title: 'WhatsApp Cloud', icon: 'Connection', path: '/whatsapp-cloud' },
          { key: 'dingtalkApp', title: '钉钉应用账号', icon: 'Connection', path: '/dingtalk-app' },
          { key: 'cardsCrossPublish', title: '跨平台发布', icon: 'Share', path: '/cards/cross-publish' }
        ]
      },
      {
        key: 'agentSalesIntel',
        title: '销售智能',
        icon: 'Trophy',
        roles: ['admin', 'manager', 'sales'],
        children: [
          { key: 'objectionList', title: '异议处理', icon: 'ChatLineRound', path: '/objection/list' },
          { key: 'personaList', title: '销冠画像', icon: 'UserFilled', path: '/persona/list' }
        ]
      }
    ]
  },
  {
    key: 'reach',
    title: '触达运营',
    icon: 'Promotion',
    roles: ['admin', 'manager', 'sales', 'viewer', 'staff'],
    children: [
      {
        key: 'emailReach',
        title: '邮件触达',
        icon: 'Message',
        children: [
          { key: 'emailList', title: '邮件列表', icon: 'ChatSquare', path: '/email' },
          { key: 'emailDrafts', title: '我的草稿', icon: 'Document', path: '/email/drafts' },
          { key: 'emailJobs', title: '我的任务', icon: 'Document', path: '/email/jobs' },
          { key: 'emailSmtp', title: '邮件账号', icon: 'Setting', path: '/email/smtp' },
          { key: 'emailInfo', title: '邮件代理', icon: 'Setting', path: '/email/info' },
          { key: 'emailDeliverability', title: '送达分析', icon: 'DataAnalysis', path: '/email/deliverability' },
          { key: 'emailGuide', title: '邮件使用指南', icon: 'Document', path: '/email/guide' }
        ]
      },
      {
        key: 'smsReach',
        title: '短信触达',
        icon: 'ChatDotSquare',
        children: [
          { key: 'smsList', title: '短信列表', icon: 'ChatDotSquare', path: '/sms/list' },
          { key: 'smsDrafts', title: '短信草稿', icon: 'Document', path: '/sms/drafts' },
          { key: 'smsJobs', title: '短信任务', icon: 'List', path: '/sms/jobs' },
          { key: 'smsConfig', title: '短信配置', icon: 'Setting', path: '/sms/config' }
        ]
      },
      {
        key: 'douyin',
        title: '抖音',
        icon: 'VideoPlay',
        children: [
          { key: 'douyinCard', title: '抖音卡片', icon: 'VideoPlay', path: '/douyinCard' },
          { key: 'douyinStats', title: '抖音统计', icon: 'DataAnalysis', path: '/douyin/stats' }
        ]
      },
      {
        key: 'kuaishou',
        title: '快手',
        icon: 'Bell',
        children: [
          { key: 'kuaishouCard', title: '快手卡片', icon: 'ChatDotRound', path: '/kuaishouCard' },
          { key: 'kuaishouStats', title: '快手统计', icon: 'DataAnalysis', path: '/kuaishou/stats' }
        ]
      },
      {
        key: 'xiaohongshu',
        title: '小红书',
        icon: 'Picture',
        children: [
          { key: 'xiaohongshuCard', title: '小红书卡片', icon: 'Picture', path: '/xiaohongshuCard' },
          { key: 'xiaohongshuStats', title: '小红书统计', icon: 'DataAnalysis', path: '/xiaohongshu/stats' }
        ]
      },
      {
        key: 'xianyu',
        title: '闲鱼',
        icon: 'ShoppingBag',
        children: [
          { key: 'xianyuCard', title: '闲鱼卡片', icon: 'ShoppingBag', path: '/xianyuCard' },
          { key: 'xianyuStats', title: '闲鱼统计', icon: 'DataAnalysis', path: '/xianyu/stats' }
        ]
      },
      {
        key: 'tiktok',
        title: 'TikTok',
        icon: 'VideoPlay',
        children: [
          { key: 'tiktokList', title: 'TikTok卡片', icon: 'VideoPlay', path: '/tiktok/list' },
          { key: 'tiktokStats', title: 'TikTok统计', icon: 'DataAnalysis', path: '/tiktok/stats' }
        ]
      },
      {
        key: 'whatsapp',
        title: 'WhatsApp',
        icon: 'ChatDotRound',
        children: [
          { key: 'whatsappAccount', title: '账号管理', icon: 'Cpu', path: '/whatsapp/account' },
          { key: 'whatsappDrafts', title: '草稿箱', icon: 'Document', path: '/whatsapp/drafts' },
          { key: 'whatsappJobs', title: '群发', icon: 'Promotion', path: '/whatsapp/jobs' },
          { key: 'whatsappLeadGroup', title: '群发线索分组', icon: 'UserFilled', path: '/whatsapp/lead-group-selection' },
          { key: 'whatsappBulkMatrix', title: '群发统计矩阵', icon: 'Grid', path: '/whatsapp/bulk-matrix' },
          { key: 'whatsappGroupMessaging', title: '群发矩阵', icon: 'Grid', path: '/whatsapp/group-messaging' }
        ]
      },
      {
        key: 'telegram',
        title: '电报社群',
        icon: 'ChatDotRound',
        children: [
          { key: 'telegramAccount', title: '机器人', icon: 'Cpu', path: '/telegram/account' }
        ]
      },
      {
        key: 'qq',
        title: 'QQ 机器人',
        icon: 'ChatDotRound',
        children: [
          { key: 'qqAccount', title: '机器人账号', icon: 'Cpu', path: '/qq/account' }
        ]
      },
      {
        key: 'feishu',
        title: '飞书',
        icon: 'ChatDotRound',
        roles: ['admin', 'manager'],
        children: [
          { key: 'feishuAccount', title: '飞书账号', icon: 'Cpu', path: '/feishu/account' }
        ]
      },
      { key: 'community', title: '社群管理', icon: 'UserFilled', path: '/community/list' },
      {
        key: 'shortLink',
        title: '短链管理',
        icon: 'Link',
        children: [
          { key: 'shortLinkList', title: '短链列表', icon: 'Link', path: '/shortLink' },
          { key: 'shortLinkStats', title: '短链统计', icon: 'DataAnalysis', path: '/shortLink/stats' }
        ]
      },
      { key: 'livecode', title: '活码管理', icon: 'QrCode', path: '/livecode' },
      { key: 'workflowOrchestrator', title: '工作流编排', icon: 'Share', path: '/workflow-orchestrator/list', roles: ['admin', 'manager'] }
    ]
  },
  {
    key: 'knowledge',
    title: '知识中心',
    icon: 'Collection',
    roles: ['admin', 'manager', 'sales', 'viewer', 'staff'],
    children: [
      {
        key: 'knowledgeWorkspace',
        title: '知识库工作台',
        icon: 'Files',
        roles: ['admin', 'manager'],
        children: [
          { key: 'knowledgeManagement', title: '文档管理', icon: 'Document', path: '/knowledge/management' },
          { key: 'knowledgeBatchImport', title: '批量导入', icon: 'UploadFilled', path: '/knowledge/batch-import' },
          { key: 'knowledgePlayground', title: '检索 Playground', icon: 'Aim', path: '/knowledge/playground' },
          { key: 'knowledgeFeedbacks', title: '反馈管理', icon: 'ChatLineRound', path: '/knowledge/feedbacks' },
          { key: 'knowledgeTokens', title: 'API Token', icon: 'Key', path: '/knowledge/tokens' },
          { key: 'knowledgeExternal', title: '外部系统接入', icon: 'Connection', path: '/knowledge/external' },
          { key: 'knowledgeStatistics', title: '知识库统计', icon: 'DataAnalysis', path: '/knowledge/statistics' },
          { key: 'knowledgeRagEval', title: 'RAG 评测', icon: 'CircleCheck', path: '/knowledge/rag-eval' },
          { key: 'knowledgeOpenAPI', title: 'OpenAPI 集成', icon: 'Connection', path: '/knowledge/openapi' },
          { key: 'knowledgeConnectors', title: '连接器配置', icon: 'Link', path: '/knowledge/connectors' }
        ]
      },
      {
        key: 'ragProductConfig',
        title: 'RAG 知识库',
        icon: 'ChatLineRound',
        roles: ['admin', 'manager'],
        children: [
          { key: 'ragMainConfig', title: 'RAG 主配置', icon: 'Setting', path: '/system/rag-product-config' },
          { key: 'ragAccount', title: 'RAG 账号配置', icon: 'Key', path: '/system/rag-account' },
          { key: 'ragProduct', title: 'RAG 产品管理', icon: 'Goods', path: '/system/rag-product' }
        ]
      },
      { key: 'ragOverview', title: 'RAG 概览', icon: 'Monitor', path: '/system/rag-overview' },
      {
        key: 'i18nManage',
        title: '多语言管理',
        icon: 'Coin',
        roles: ['admin', 'manager'],
        children: [
          { key: 'glossary', title: '术语表管理', icon: 'Collection', path: '/glossary', roles: ['admin', 'manager'] },
          { key: 'i18nDashboard', title: '多语言监控', icon: 'DataLine', path: '/i18n/dashboard', roles: ['admin', 'manager'] }
        ]
      }
    ]
  },
  {
    key: 'analytics',
    title: '数据分析',
    icon: 'DataAnalysis',
    roles: ['admin', 'manager', 'sales', 'viewer', 'staff'],
    children: [
      { key: 'dashboardScreen', title: '数据大屏', icon: 'DataBoard', path: '/dashboardScreen/list' },
      { key: 'conversionFunnel', title: '转化漏斗', icon: 'Filter', path: '/conversionFunnel/list' },
      { key: 'aiProductivity', title: 'AI 产能分析', icon: 'DataAnalysis', path: '/aiProductivity/list', roles: ['admin', 'manager', 'viewer'] },
      { key: 'customReport', title: '自定义报表', icon: 'Document', path: '/customReport/list' },
      { key: 'abExperiment', title: 'A/B 实验', icon: 'DataLine', path: '/abExperiment/list', roles: ['admin', 'manager', 'viewer'] },
      { key: 'churnPrediction', title: '流失预警', icon: 'Warning', path: '/churnPrediction/list', roles: ['admin', 'manager', 'viewer'] },
      { key: 'customerJourney', title: '客户旅程大屏', icon: 'TrendCharts', path: '/customerJourney/dashboard', roles: ['admin', 'manager', 'sales', 'viewer'] },
      { key: 'cohortPath', title: '同期群路径分析', icon: 'Guide', path: '/analytics/cohort-path', roles: ['admin', 'manager', 'viewer'] },
      { key: 'confidencePanel', title: '置信度看板', icon: 'CircleCheck', path: '/confidence/panel', roles: ['admin', 'manager', 'viewer'] },
      { key: 'humanizePanel', title: '拟人度看板', icon: 'MagicStick', path: '/humanize/panel', roles: ['admin', 'manager', 'viewer'] },
      { key: 'feedbackLoopPanel', title: '反馈闭环', icon: 'Refresh', path: '/feedbackLoop/panel', roles: ['admin', 'manager', 'viewer'] },
      {
        key: 'browserAutomation',
        title: '浏览器自动化',
        icon: 'Monitor',
        path: '/browser-automation/tasks',
        children: [
          { key: 'baTasks', title: '任务列表', icon: 'Monitor', path: '/browser-automation/tasks' },
          { key: 'baCreate', title: '新建任务', icon: 'Plus', path: '/browser-automation/tasks/create' },
          { key: 'baCron', title: '定时触发器', icon: 'Timer', path: '/browser-automation/cron' },
          { key: 'baStatus', title: 'Host 状态', icon: 'CircleCheck', path: '/browser-automation/status' },
        ]
      },
      {
        key: 'geoTools',
        title: 'GEO 智能优化',
        icon: 'MagicStick',
        path: '/geo-tools/keyword-mining',
        children: [
          {
            key: 'geoProd',
            title: '生产',
            icon: 'EditPen',
            children: [
              { key: 'geoKeywordMining', title: '关键词蒸馏', icon: 'Search', path: '/geo-tools/keyword-mining' },
              { key: 'geoContentCreation', title: '内容创作', icon: 'EditPen', path: '/geo-tools/content-creation' },
              { key: 'geoContentOptimize', title: '文章优化', icon: 'Document', path: '/geo-tools/content-optimize' },
              { key: 'geoKnowledgeBase', title: 'GEO 知识库', icon: 'FolderOpened', path: '/geo-tools/knowledge-base' },
            ]
          },
          {
            key: 'geoPublish',
            title: '发布',
            icon: 'Connection',
            children: [
              { key: 'geoPlatformPublish', title: '平台发布', icon: 'Connection', path: '/geo-tools/platform-publish' },
              { key: 'geoWorkflow', title: '工作流', icon: 'Setting', path: '/geo-tools/workflow' },
            ]
          },
          {
            key: 'geoMonitor',
            title: '监控',
            icon: 'Monitor',
            children: [
              { key: 'geoVisibilityBoard', title: '可见性观测', icon: 'TrendCharts', path: '/geo-tools/visibility' },
              { key: 'geoSovBoard', title: '竞品 SOV', icon: 'DataLine', path: '/geo-tools/sov-board' },
              { key: 'geoCompetitors', title: '竞品管理', icon: 'UserFilled', path: '/geo-tools/competitors' },
              { key: 'geoCrawlerStats', title: '爬虫统计', icon: 'Monitor', path: '/geo-tools/crawler-stats' },
              { key: 'geoEntityGraph', title: '实体图谱', icon: 'Share', path: '/geo-tools/entity-graph' },
              { key: 'geoVerification', title: '多模型验证', icon: 'CircleCheck', path: '/geo-tools/verification' },
              { key: 'geoAlerts', title: '预警中心', icon: 'Bell', path: '/geo-tools/alerts' },
              { key: 'geoReports', title: '数据报表', icon: 'DataAnalysis', path: '/geo-tools/reports' },
              { key: 'geoDecisionReport', title: '决策报告', icon: 'Document', path: '/geo/decision-report' },
              { key: 'geoConfig', title: '配置优化', icon: 'Setting', path: '/geo-tools/config' },
            ]
          }
        ]
      }
    ]
  },
  {
    key: 'system',
    title: '系统设置',
    icon: 'Setting',
    roles: ['admin', 'manager', 'sales', 'viewer', 'staff'],
    children: [
      {
        key: 'siteConfig',
        title: '站点配置',
        icon: 'Tools',
        roles: ['admin'],
        children: [
          { key: 'systemConfig', title: '站点设置', icon: 'Tools', path: '/system/config' },
          { key: 'systemObsConfig', title: '存储配置', icon: 'Cloud', path: '/system/obs-config' },
          { key: 'systemMaterialLibrary', title: '素材库', icon: 'Picture', path: '/system/material-library' },
          { key: 'systemMonitor', title: '监控', icon: 'Cpu', path: '/system/monitor' },
          { key: 'systemTrace', title: '链路追踪', icon: 'Connection', path: '/system/trace' },
          { key: 'systemGuide', title: '系统使用指南', icon: 'Document', path: '/system/guide' },
          { key: 'domainPool', title: '域名池', icon: 'Link', path: '/domainPool' },
          { key: 'backupList', title: '备份恢复', icon: 'FolderOpened', path: '/backup/list' },
          { key: 'backupEnhanced', title: '备份高级版', icon: 'FolderChecked', path: '/backup/enhanced' },
          { key: 'automationCenter', title: '自动化中心', icon: 'Operation', path: '/system/automation-hub', roles: ['admin'] },
          { key: 'dynamicThreshold', title: '动态阈值参数', icon: 'DataLine', path: '/system/config-params', roles: ['admin'] },
          { key: 'opsOverview', title: '运维总览', icon: 'Monitor', path: '/ops-overview', roles: ['admin'] }
        ]
      },
      {
        key: 'teamGroup',
        title: '团队',
        icon: 'UserFilled',
        roles: ['admin'],
        children: [
          { key: 'systemUser', title: '人员管理', icon: 'UserFilled', path: '/system/users' },
          { key: 'roleManage', title: '角色管理', icon: 'UserFilled', path: '/system/roles' },
          { key: 'permissionManage', title: '授权管理', icon: 'Lock', path: '/system/permissions' }
        ]
      },
      {
        key: 'permissionGroup',
        title: '权限设置',
        icon: 'Lock',
        roles: ['admin'],
        children: [
          { key: 'platformAccount', title: '平台账号', icon: 'Platform', path: '/platformAccount/list' },
          { key: 'integration', title: '第三方对接', icon: 'Connection', path: '/integration/list' },
          { key: 'bridgeToken', title: '桥接凭证', icon: 'Key', path: '/bridge/token' },
          { key: 'operationLog', title: '操作日志', icon: 'Tickets', path: '/operationLog/list' },
          { key: 'securityAudit', title: '安全审计', icon: 'Shield', path: '/securityAudit/list' }
        ]
      },
      {
        key: 'assetBundle',
        title: '资产包',
        icon: 'Box',
        roles: ['admin', 'manager', 'sales', 'viewer', 'staff'],
        children: [
          { key: 'assetMarket', title: '资产市场', icon: 'ShoppingCart', path: '/asset-market' },
          { key: 'myAssets', title: '我的资产', icon: 'Files', path: '/asset-market/my-assets' },
          { key: 'assetBundleList', title: '资产包管理', icon: 'List', path: '/asset-bundle/list' },
          { key: 'assetBundlePlayground', title: '开发者 Playground', icon: 'EditPen', path: '/asset-bundle/playground' },
          { key: 'assetBundleMerchant', title: '商户话术包', icon: 'ChatDotRound', path: '/asset-bundle/merchant-new' },
          { key: 'syncLog', title: '同步日志', icon: 'Refresh', path: '/asset-market/sync-log' }
        ]
      }
    ]
  }
]);

const hasPermission = (menu) => {
  if (!menu.roles || menu.roles.length === 0) return true
  const currentRole = userStore.role
  if (currentRole === 'admin') return true
  return menu.roles.includes(currentRole)
};

const filterMenuByRole = (menu) => {
  if (!hasPermission(menu)) return null
  if (menu.children) {
    const filteredChildren = menu.children.map(child => filterMenuByRole(child)).filter(child => child !== null)
    if (filteredChildren.length === 0) return null
    return { ...menu, children: filteredChildren }
  }
  return { ...menu }
}

const filteredTopMenus = computed(() => {
  return topMenus.value.map(menu => filterMenuByRole(menu)).filter(menu => menu !== null)
})

const currentTopMenu = computed(() => {
  return filteredTopMenus.value.find(menu => menu.key === activeTopMenu.value)
})

const breadcrumb = computed(() => {
  const chain = []
  const build = (items, trail) => {
    for (const it of items) {
      const next = [...trail, it.title]
      if (it.path === route.path) { chain.push(...next); return true }
      if (it.children && build(it.children, next)) return true
    }
    return false
  }
  for (const top of filteredTopMenus.value) {
    if (build([top], [])) break
  }
  return chain
});

const hasActiveChild = (menu, path) => {
  if (menu.path === path) return true
  if (menu.children) {
    for (const child of menu.children) {
      if (hasActiveChild(child, path)) return true
    }
  }
  return false
}

const findFirstChild = (menu) => {
  if (menu.path) return menu
  if (menu.children) {
    for (const child of menu.children) {
      const result = findFirstChild(child)
      if (result) return result
    }
  }
  return null
}

watch(() => route.path, (newPath) => {
  activeSubMenu.value = newPath
  for (const menu of filteredTopMenus.value) {
    if (hasActiveChild(menu, newPath)) {
      activeTopMenu.value = menu.key
      break
    }
  }
}, { immediate: true })

const handleTopMenuSelect = (key) => {
  activeTopMenu.value = key
  const menu = filteredTopMenus.value.find(item => item.key === key)
  if (menu && menu.children) {
    const firstChild = findFirstChild(menu)
    if (firstChild) router.push(firstChild.path)
  }
}

const handleSubMenuSelect = (path) => {
  router.push(path)
}

const toggleSidebar = () => {
  sidebarCollapsed.value = !sidebarCollapsed.value
  persistSidebarCollapsed()
}

const handleUserCommand = (command) => {
  switch (command) {
    case 'profile':
      router.push({ name: 'Profile' })
      break
    case 'help-center':
      router.push({ name: 'HelpCenter' })
      break
    case 'logout':
        userStore.logout()
        ElMessage.success('已退出登录')
        router.replace('/login')
        break
  }
}

const handleLogin = () => {
  router.push('/login')
}

onMounted(async () => {
  try {
    const { updateRequestConfig } = await import('@/utils/request')
    await updateRequestConfig()
    loadLicenseInfo()
  } catch (error) {
    console.error('初始化请求配置失败:', error)
  }
  loadGeoAlertCount()
  geoAlertTimer = setInterval(() => {
    if (document.visibilityState === 'visible') loadGeoAlertCount()
  }, 60000)
})

onUnmounted(() => {
  if (geoAlertTimer) { clearInterval(geoAlertTimer); geoAlertTimer = null }
})
</script>

<style lang="scss" scoped>
.app-shell {
  height: 100vh;
  background: $bg-color-page;
}

/* ===== 顶栏 ===== */
.app-header {
  height: 64px;
  background: $bg-color;
  border-bottom: 1px solid $border-base;
  box-shadow: 0 1px 3px rgba(15, 23, 42, 0.04);
  display: flex;
  align-items: center;
  padding: 0 20px;
  position: relative;
  z-index: 10;
}
.brand {
  display: flex;
  align-items: center;
  gap: $spacing-md;
  margin-right: $spacing-lg;
}
.brand-mark {
  width: 30px;
  height: 30px;
  border-radius: $border-radius-base;
  background: linear-gradient(135deg, $primary-color 0%, $primary-color 100%);
  box-shadow: 0 4px 12px rgba(79, 70, 229, 0.35);
  position: relative;
  flex-shrink: 0;
}
.brand-mark::after {
  content: '';
  position: absolute;
  inset: 7px;
  border: 2px solid rgba(255, 255, 255, 0.92);
  border-radius: $border-radius-small;
  border-bottom-color: transparent;
  border-left-color: transparent;
}
.brand-text {
  font-size: $font-size-medium;
  font-weight: 700;
  color: $text-primary;
  letter-spacing: 0.3px;
  white-space: nowrap;
}
.top-menu {
  flex: 1;
  min-width: 0; /* 允许 flex 子项收缩，配合下方 overflow 让菜单可横向滚动 */
  border-bottom: none;
  background: transparent;
  overflow-x: auto;
  overflow-y: hidden;
  scrollbar-width: thin;
}
/* 强制 el-menu 内部容器按内容宽度展开，避免 el-menu 自行将溢出项 hidden 掉 */
.top-menu.el-menu,
.top-menu > .el-menu,
.top-menu .el-menu--horizontal {
  min-width: max-content;
}
.top-menu::-webkit-scrollbar { height: 4px; }
.top-menu::-webkit-scrollbar-thumb { background: $border-base; border-radius: 2px; }
.top-menu .el-menu-item {
  height: 64px;
  line-height: 64px;
  border-bottom: none;
  font-weight: 500;
  color: $text-regular;
}
.top-menu .el-menu-item:hover {
  color: $primary-color;
  background: transparent;
}
.top-menu .el-menu-item.is-active {
  color: $primary-color;
  font-weight: 600;
  border-bottom: 2px solid $primary-color;
}

/* ===== 通知铃铛 ===== */
.notif-bell {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 38px;
  height: 38px;
  margin: $spacing-sm;
  border-radius: $border-radius-base;
  color: $text-regular;
  cursor: pointer;
  transition: background-color 0.2s ease, color 0.2s ease;
}
.notif-bell:hover {
  background: $border-extra-light;
  color: $primary-color;
}
.geo-alert-bell {
  margin-left: 0;
}

/* ===== 用户区 ===== */
.user-area {
  display: flex;
  align-items: center;
  margin-left: $spacing-md;
}
.user-dropdown {
  display: flex;
  align-items: center;
  gap: $spacing-sm;
  cursor: pointer;
  padding: $spacing-xs 10px;
  border-radius: $border-radius-base;
  transition: background-color 0.2s ease;
}
.user-dropdown:hover {
  background: $border-extra-light;
}
.user-avatar {
  background: linear-gradient(135deg, $primary-color, $primary-color);
  font-weight: 600;
  font-size: $font-size-small;
}
.user-name {
  color: $text-primary;
  font-weight: 500;
  font-size: $font-size-base;
}
.login-button {
  display: flex;
  align-items: center;
  gap: $spacing-sm;
  color: $bg-color;
  background: $primary-color;
  cursor: pointer;
  padding: $spacing-sm 18px;
  border-radius: $border-radius-base;
  font-weight: 500;
  font-size: $font-size-base;
  margin-left: $spacing-md;
  transition: background 0.2s ease;
}
.login-button:hover {
  background: $primary-color;
}

/* ===== 侧边栏（品牌深靛蓝） ===== */
.app-body {
  height: calc(100vh - 64px);
}
.app-aside {
  background: linear-gradient(180deg, $primary-color 0%, $primary-color 100%);
  border-right: none;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  transition: width 0.25s ease;
}
.aside-brand {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: $spacing-md 20px;
  color: $primary-light-3;
  font-size: $font-size-base;
  font-weight: 600;
  letter-spacing: 0.5px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.07);
}
.aside-brand .el-icon {
  font-size: $font-size-large;
  color: $primary-light-2;
}
.collapse-btn {
  margin-left: auto;
  color: $primary-light-3;
}
.app-aside.is-collapsed .aside-brand {
  justify-content: center;
  padding: $spacing-md 0;
  gap: 0;
}
.app-aside.is-collapsed .collapse-btn {
  margin-left: 0;
}
.collapse-btn:hover {
  color: $bg-color;
  background: rgba(255, 255, 255, 0.06);
}
.app-aside :deep(.el-scrollbar) {
  flex: 1;
}
.app-aside :deep(.el-menu) {
  background: transparent;
  border-right: none;
  padding: $spacing-sm;
}
.app-aside :deep(.el-menu-item),
.app-aside :deep(.el-sub-menu__title) {
  color: $primary-light-3;
  border-radius: $border-radius-base;
  margin-bottom: $spacing-xs;
  height: 44px;
  line-height: 44px;
}
.app-aside :deep(.el-menu-item .el-icon),
.app-aside :deep(.el-sub-menu__title .el-icon) {
  color: $primary-light-4;
}
.app-aside :deep(.el-menu-item:hover),
.app-aside :deep(.el-sub-menu__title:hover) {
  background: rgba(255, 255, 255, 0.06);
  color: $bg-color;
}
.app-aside :deep(.el-menu-item.is-active) {
  background: rgba(99, 102, 241, 0.22);
  color: $bg-color;
  font-weight: 600;
  position: relative;
}
.app-aside :deep(.el-menu-item.is-active::before) {
  content: '';
  position: absolute;
  left: 0;
  top: 8px;
  bottom: 8px;
  width: 3px;
  border-radius: 0 3px 3px 0;
  background: $primary-light-2;
}
.app-aside :deep(.el-sub-menu.is-active > .el-sub-menu__title) {
  color: $bg-color;
}

/* ===== 授权信息 ===== */
.license-info {
  padding: $spacing-md;
  border-top: 1px solid rgba(255, 255, 255, 0.08);
  background: rgba(0, 0, 0, 0.15);
  color: $primary-light-3;
  font-size: $font-size-extra-small;
  text-align: center;
}
.license-expiry.expired { color: $danger-color; }
.license-expiry {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: $spacing-xs;
}
.license-note {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: $spacing-xs;
  margin-top: $spacing-sm;
  font-size: $font-size-extra-small;
  color: $text-placeholder;
}

/* ===== 主内容区 ===== */
.app-main {
  background: $bg-color-page;
  padding: 0;
  overflow-y: auto;
}
.breadcrumb-wrap {
  padding: $spacing-md 24px 0;
}
.breadcrumb-wrap :deep(.el-breadcrumb__inner) {
  color: $text-secondary;
  font-weight: 500;
}
.breadcrumb-wrap :deep(.el-breadcrumb__item:last-child .el-breadcrumb__inner) {
  color: $text-primary;
  font-weight: 600;
}

.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.25s ease;
}
.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
</style>
