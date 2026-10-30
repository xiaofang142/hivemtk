import { createRouter, createWebHistory } from 'vue-router'

const SITE = 'HiveMTK · 私域 AI 营销操作系统'

// 每个路由的 SEO 元数据：标题 + 描述 + 关键词（SPA 客户端注入，弥补无 SSR 的 SEO 短板）
const routes = [
  {
    path: '/',
    name: 'Home',
    component: () => import('../views/HomePage.vue'),
    meta: {
      title: '首页',
      description: 'HiveMTK 是把七端社媒、ReAct 自主智能体（45 工具）、零出域数据安全三件事同时做透的私域 AI 营销操作系统。94 个业务模块 + GEO 智能优化，AGPL-3.0 开源，4 步私有化部署。',
      keywords: 'HiveMTK,私域AI营销操作系统,七端打透,ReAct智能体,GEO,生成式引擎优化,私有化部署,AGPL-3.0开源,数据不出域,AI自动谈单,销冠SOP,客户CDP',
    },
  },
  {
    path: '/docs',
    name: 'Docs',
    component: () => import('../views/DocsPage.vue'),
    meta: {
      title: '安装使用文档',
      description: 'HiveMTK 私有化部署文档：Docker/源码安装、FRP 私域穿透、RAG 知识库配置、自动回复与 94 个业务模块使用指南。',
      keywords: 'HiveMTK文档,私有化部署教程,Docker部署,源码部署,FRP私域穿透,RAG知识库配置,自动回复配置,安装指南',
    },
  },
  {
    path: '/deploy',
    name: 'Deploy',
    component: () => import('../views/DeployPage.vue'),
    meta: {
      title: '部署指南',
      description: 'HiveMTK 开源项目部署指南：Docker 部署、源码部署、FRP 私域穿透配置。基于 AGPL-3.0 开源，4 步完成部署。',
      keywords: 'HiveMTK部署,Docker部署,源码部署,私有化部署,FRP私域穿透,AGPL-3.0开源,4步部署',
    },
  },
  {
    path: '/download',
    redirect: '/deploy',
  },
  {
    path: '/features',
    name: 'Features',
    component: () => import('../views/FeaturesPage.vue'),
    meta: {
      title: '核心功能',
      description: 'HiveMTK 核心功能：七端多账号聚合、AI 自动谈单、销冠 SOP 智能体、客户 CDP、全渠道触达、私域自动化与数据驾驶舱。',
      keywords: 'HiveMTK核心功能,七端多账号聚合,AI自动谈单,销冠SOP智能体,客户CDP,全渠道触达,私域自动化,数据驾驶舱',
    },
  },
  {
    path: '/modules',
    name: 'Modules',
    component: () => import('../views/ModulesPage.vue'),
    meta: {
      title: '功能架构',
      description: 'HiveMTK 功能架构清单：24 个功能模块（7 基础 + 17 增值）× 212 个菜单节点，模块闸、依赖闭包与离线单机 / SaaS 多租户双形态说明，数据与源码一一对应。',
      keywords: 'HiveMTK功能架构,功能模块清单,24个功能模块,模块闸,依赖闭包,菜单节点,多租户架构,离线单机部署',
    },
  },
  {
    path: '/pricing',
    name: 'Pricing',
    component: () => import('../views/PricingPage.vue'),
    meta: {
      title: '按需订阅',
      description: 'HiveMTK 按需订阅：AGPL-3.0 开源基座与 7 个基础模块免费常开，17 个增值模块中 16 个 0 元、仅浏览器自动化 ¥1 买断；云端版软件订阅 9 元、新商户 15 天体验期并赠 10M 模型 token，平台模型算力按输入 / 输出 token 预付费，支持 SaaS 多租户与离线单机双形态。',
      keywords: 'HiveMTK订阅,功能模块免费,浏览器自动化买断,云端版订阅,LLM按量计费,平台模型算力,SaaS多租户,按需订阅',
    },
  },
  {
    path: '/toolchain',
    name: 'Toolchain',
    component: () => import('../views/ToolchainPage.vue'),
    meta: {
      title: '工程能力',
      description: 'HiveMTK 工程能力：ReAct 智能体编排框架、RAG 知识库、消息中台 MQ、LLM 路由网关、合规风控、可观测性与私有化交付。',
      keywords: 'HiveMTK工程能力,ReAct智能体编排,RAG知识库,消息中台,LLM路由网关,合规风控,可观测性,私有化部署',
    },
  },
  {
    path: '/workflow',
    name: 'Workflow',
    component: () => import('../views/WorkflowPage.vue'),
    meta: {
      title: '业务流程',
      description: 'HiveMTK 业务流程：从七端多账号接入、线索承接、AI 谈单、SOP 执行到数据复盘的全链路自动化。',
      keywords: 'HiveMTK业务流程,七端多账号接入,线索承接,AI谈单流程,SOP执行,客户旅程,数据复盘,自动化运营',
    },
  },
  {
    path: '/faq',
    name: 'Faq',
    component: () => import('../views/FaqPage.vue'),
    meta: {
      title: '常见问题',
      description: 'HiveMTK 常见问题：AGPL-3.0 开源协议、私有化部署、数据合规、七端账号接入、AI 谈单效果与二次开发答疑。',
      keywords: 'HiveMTK FAQ,AGPL-3.0开源,私有化部署,数据合规,七端账号接入,AI谈单效果,二次开发,常见问题',
    },
  },
  {
    path: '/:pathMatch(.*)*',
    name: 'NotFound',
    component: () => import('../views/NotFoundPage.vue'),
    meta: {
      title: '页面未找到',
      description: '您访问的页面不存在，请返回 HiveMTK 官网首页。',
      keywords: '404,页面未找到,HiveMTK',
    },
  },
]

const router = createRouter({
  // BASE_URL 由 vite.config.js 的 base 注入（'/hivemtk/'）；写死 '/' 会让所有路由在 Pages 子路径下 404。
  history: createWebHistory(import.meta.env.BASE_URL),
  routes,
  scrollBehavior(to, from, savedPosition) {
    if (to.hash) {
      return { el: to.hash, top: 90 }
    }
    if (savedPosition) return savedPosition
    return { top: 0 }
  },
})

// 客户端动态注入 SEO meta（title / description / keywords / og / twitter / canonical）
function setMeta(attr, key, content) {
  if (!content) return
  let el = document.head.querySelector(`meta[${attr}="${key}"]`)
  if (!el) {
    el = document.createElement('meta')
    el.setAttribute(attr, key)
    document.head.appendChild(el)
  }
  el.setAttribute('content', content)
}
function setCanonical(href) {
  let link = document.head.querySelector('link[rel="canonical"]')
  if (!link) {
    link = document.createElement('link')
    link.setAttribute('rel', 'canonical')
    document.head.appendChild(link)
  }
  link.setAttribute('href', href)
}
router.afterEach((to) => {
  const meta = to.meta || {}
  const title = meta.title ? `${meta.title} | ${SITE}` : SITE
  const desc = meta.description || ''
  const keywords = meta.keywords || ''
  // canonical / og:url 用「当前 origin + 路由路径」现场拼：线上是站点根路径托管
  // （base='/'），to.href 已带 history base，换成 to.fullPath 等价；写死域名反而会在
  // 本地预览（127.0.0.1:8213）时把规范链接指到线上站。
  const url = window.location.origin + to.href

  document.title = title
  setMeta('name', 'description', desc)
  setMeta('name', 'keywords', keywords)
  setMeta('property', 'og:title', title)
  setMeta('property', 'og:description', desc)
  setMeta('property', 'og:url', url)
  setMeta('name', 'twitter:title', title)
  setMeta('name', 'twitter:description', desc)
  setCanonical(url)
})

export default router

