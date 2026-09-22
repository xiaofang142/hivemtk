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
      description: 'HiveMTK 是把七端社媒、ReAct 自主智能体（42 工具）、零出域数据安全三件事同时做透的私域 AI 营销操作系统。94 个业务模块 + GEO 智能优化，AGPL-3.0 开源，4 步私有化部署。',
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
      description: 'HiveMTK 开源项目部署指南：Docker 一键部署、源码部署、FRP 私域穿透配置。基于 AGPL-3.0 开源，4 步完成部署。',
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
// 百度统计：SPA 路由切换上报 PV（首次加载由 hm.js 自动统计，跳过避免重复计数）
let isFirstNavigation = true
router.afterEach((to) => {
  const meta = to.meta || {}
  const title = meta.title ? `${meta.title} | ${SITE}` : SITE
  const desc = meta.description || ''
  const keywords = meta.keywords || ''
  // to.href 带 history base（'/hivemtk/…'），to.fullPath 不带——用 fullPath 拼 canonical
  // 会让 Pages 子路径下的规范链接指向站点根的 404，等于把 SEO 信号发给一个不存在的 URL。
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

  if (!isFirstNavigation && Array.isArray(window._hmt)) {
    window._hmt.push(['_trackPageview', to.fullPath])
  }
  isFirstNavigation = false
})

export default router

