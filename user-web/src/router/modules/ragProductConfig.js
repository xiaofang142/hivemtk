export default [
  {
    path: '/system/rag-product-config',
    name: 'RagProductConfigIndex',
    component: () => import('@/views/RagProductConfig/index.vue'),
    meta: { menuKey: 'ragMainConfig', menuSection: 'knowledge', menuParents: ["ragProductConfig"], menuOrder: 1, menuIcon: 'Setting', title: 'RAG 主配置', group: 'knowledge', icon: 'ChatLineRound', requiresAuth: true }
  },
  {
    path: '/system/rag-account',
    name: 'RagProductAccountConfig',
    component: () => import('@/views/RagProductConfig/AccountConfig.vue'),
    meta: { menuKey: 'ragAccount', menuSection: 'knowledge', menuParents: ["ragProductConfig"], menuOrder: 2, menuIcon: 'Key', title: 'RAG 账号配置', group: 'knowledge', icon: 'Key', requiresAuth: true }
  },
  {
    path: '/system/rag-product',
    name: 'RagProductManagement',
    component: () => import('@/views/RagProductConfig/RagProductManagement.vue'),
    meta: { menuKey: 'ragProduct', menuSection: 'knowledge', menuParents: ["ragProductConfig"], menuOrder: 3, menuIcon: 'Goods', title: 'RAG 产品管理', group: 'knowledge', icon: 'Goods', requiresAuth: true }
  }
]
