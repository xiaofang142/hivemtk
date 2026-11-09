export default [
  // /system/rag-product-config（RAG 主配置）已于 2026-11-10 下线：它只是
  // RagProductConfig/index.vue 这个 tabs 包装页，挂载的两个子组件本身就是
  // 下面两条独立路由的页面——三个菜单两条内容。菜单节点 ragMainConfig 同步除名。
  {
    path: '/system/rag-account',
    name: 'RagProductAccountConfig',
    component: () => import('@/views/RagProductConfig/AccountConfig.vue'),
    meta: { title: 'RAG 账号配置', group: 'knowledge', icon: 'Key', requiresAuth: true }
  },
  {
    path: '/system/rag-product',
    name: 'RagProductManagement',
    component: () => import('@/views/RagProductConfig/RagProductManagement.vue'),
    meta: { title: 'RAG 产品管理', group: 'knowledge', icon: 'Goods', requiresAuth: true }
  }
]
