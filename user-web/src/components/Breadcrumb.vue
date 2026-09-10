<template>
  <nav class="breadcrumb-bar" role="navigation" aria-label="面包屑导航">
    <el-breadcrumb separator="/" :aria-label="'面包屑：' + (items[items.length - 1]?.title || '')">
      <el-breadcrumb-item v-for="(item, idx) in items" :key="idx" :to="item.to">
        <el-icon v-if="item.icon" class="bc-icon" aria-hidden="true"><component :is="resolveIcon(item.icon)" /></el-icon>
        {{ item.title }}
      </el-breadcrumb-item>
    </el-breadcrumb>
  </nav>
</template>

<script setup>
import i18n from '@/i18n'

import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { resolveRouteIcon } from '@/utils/iconMap'

const route = useRoute()
const router = useRouter()

const resolveIcon = resolveRouteIcon;

const topMenusMap = {
  workspace: { title: i18n.global.t('工作台'), icon: 'Monitor' },
  customer: { title: i18n.global.t('客户中心'), icon: 'UserFilled' },
  aiAgent: { title: i18n.global.t('智能体'), icon: 'Cpu' },
  reach: { title: i18n.global.t('触达运营'), icon: 'Promotion' },
  knowledge: { title: i18n.global.t('知识中心'), icon: 'Collection' },
  analytics: { title: i18n.global.t('数据分析'), icon: 'DataAnalysis' },
  system: { title: i18n.global.t('系统设置'), icon: 'Setting' }
};

const subMenusMap = {
  messageHub: { parent: 'workspace', title: i18n.global.t('消息中台 MQ') },
  inbox: { parent: 'workspace', title: i18n.global.t('统一收件箱') },
  wecomAccount: { parent: 'workspace', title: i18n.global.t('多账号聚合') },
  clue: { parent: 'customer', title: i18n.global.t('线索') },
  customer360: { parent: 'customer', title: i18n.global.t('客户') },
  customerEvent: { parent: 'customer', title: i18n.global.t('客户事件') },
  tagSegmentation: { parent: 'customer', title: i18n.global.t('标签分层') },
  userSegment: { parent: 'customer', title: i18n.global.t('用户分层 RFM') },
  unifiedMessage: { parent: 'customer', title: i18n.global.t('客户消息轨迹') },
  oneid: { parent: 'customer', title: i18n.global.t('客户身份') },
  aiAgent: { parent: 'aiAgent', title: i18n.global.t('智能体管理') },
  customerSession: { parent: 'aiAgent', title: i18n.global.t('客服会话') },
  intentRecognition: { parent: 'aiAgent', title: i18n.global.t('意图识别') },
  dialogueMemory: { parent: 'aiAgent', title: i18n.global.t('对话记忆') },
  llmRouting: { parent: 'aiAgent', title: i18n.global.t('LLM 路由') },
  reachPipeline: { parent: 'aiAgent', title: i18n.global.t('主动触达') },
  marketingFlow: { parent: 'aiAgent', title: i18n.global.t('营销流程') },
  batchOperation: { parent: 'aiAgent', title: i18n.global.t('批量运营') },
  sopAgent: { parent: 'aiAgent', title: i18n.global.t('SOP 智能体') },
  scriptTemplate: { parent: 'aiAgent', title: i18n.global.t('销冠话术库') },
  customerService: { parent: 'aiAgent', title: i18n.global.t('客服工作台') },
  chatChannel: { parent: 'aiAgent', title: i18n.global.t('客服接入') },
  objection: { parent: 'aiAgent', title: i18n.global.t('异议处理') },
  persona: { parent: 'aiAgent', title: i18n.global.t('销冠画像') },
  email: { parent: 'reach', title: i18n.global.t('邮件触达') },
  sms: { parent: 'reach', title: i18n.global.t('短信触达') },
  douyin: { parent: 'reach', title: i18n.global.t('抖音') },
  douyinCard: { parent: 'reach', title: i18n.global.t('抖音卡片') },
  kuaishou: { parent: 'reach', title: i18n.global.t('快手') },
  kuaishouCard: { parent: 'reach', title: i18n.global.t('快手卡片') },
  xiaohongshu: { parent: 'reach', title: i18n.global.t('小红书') },
  xiaohongshuCard: { parent: 'reach', title: i18n.global.t('小红书卡片') },
  xianyu: { parent: 'reach', title: i18n.global.t('闲鱼') },
  xianyuCard: { parent: 'reach', title: i18n.global.t('闲鱼卡片') },
  tiktok: { parent: 'reach', title: 'TikTok' },
  tiktokCard: { parent: 'reach', title: i18n.global.t('TikTok 卡片') },
  whatsapp: { parent: 'reach', title: 'WhatsApp' },
  telegram: { parent: 'reach', title: i18n.global.t('电报社群') },
  feishu: { parent: 'reach', title: i18n.global.t('飞书') },
  community: { parent: 'reach', title: i18n.global.t('社群管理') },
  shortLink: { parent: 'reach', title: i18n.global.t('短链管理') },
  livecode: { parent: 'reach', title: i18n.global.t('活码管理') },
  knowledge: { parent: 'knowledge', title: i18n.global.t('知识库') },
  RagProductConfig: { parent: 'knowledge', title: i18n.global.t('RAG 知识库') },
  dashboardScreen: { parent: 'analytics', title: i18n.global.t('数据大屏') },
  customerJourney: { parent: 'analytics', title: i18n.global.t('客户旅程大屏') },
  conversionFunnel: { parent: 'analytics', title: i18n.global.t('转化漏斗') },
  aiProductivity: { parent: 'analytics', title: i18n.global.t('AI 产能分析') },
  customReport: { parent: 'analytics', title: i18n.global.t('自定义报表') },
  abExperiment: { parent: 'analytics', title: i18n.global.t('A/B 实验') },
  churnPrediction: { parent: 'analytics', title: i18n.global.t('流失预警') },
  system: { parent: 'system', title: i18n.global.t('系统设置') },
  domainPool: { parent: 'system', title: i18n.global.t('域名池') },
  systemUser: { parent: 'system', title: i18n.global.t('人员管理') },
  roleManage: { parent: 'system', title: i18n.global.t('角色管理') },
  permissionManage: { parent: 'system', title: i18n.global.t('授权管理') },
  platformAccount: { parent: 'system', title: i18n.global.t('平台账号') },
  integration: { parent: 'system', title: i18n.global.t('第三方对接') },
  operationLog: { parent: 'system', title: i18n.global.t('操作日志') },
  backup: { parent: 'system', title: i18n.global.t('备份恢复') },
  securityAudit: { parent: 'system', title: i18n.global.t('安全审计') }
};

const items = computed(() => {
  const path = route.path
  const result = []

  if (path === '/' || path === '/messageHub/list') {
    return [{ title: i18n.global.t('首页'), to: '/', icon: 'HomeFilled' }]
  }

  if (path === '/profile') {
    return [
      { title: i18n.global.t('首页'), to: '/', icon: 'HomeFilled' },
      { title: i18n.global.t('个人资料'), to: '/profile' }
    ]
  }

  if (path === '/notifications') {
    return [
      { title: i18n.global.t('首页'), to: '/', icon: 'HomeFilled' },
      { title: i18n.global.t('通知中心'), to: '/notifications' }
    ]
  }

  const seg = path.split('/').filter(Boolean)[0] || '';
  const subMenu = subMenusMap[seg]
  if (subMenu) {
    const topMenu = topMenusMap[subMenu.parent]
    if (topMenu) {
      result.push({ title: i18n.global.t('首页'), to: '/', icon: 'HomeFilled' })
      result.push({ title: topMenu.title, to: '/' })
    }
    const pageTitle = route.meta?.title || subMenu.title;
    result.push({ title: pageTitle })
  } else {
    const title = route.meta?.title || seg;
    result.push({ title: i18n.global.t('首页'), to: '/', icon: 'HomeFilled' })
    result.push({ title })
  }

  return result
})
</script>

<style scoped>
.breadcrumb-bar {
  padding: 12px 20px;
  background: #fff;
  border-bottom: 1px solid #ebeef5;
  margin-bottom: 16px;
}
.bc-icon {
  margin-right: 4px;
  vertical-align: -2px;
}
</style>
