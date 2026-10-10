export default [{
  path: '/knowledgeBase',
  name: 'KnowledgeBase',
  component: () => import('@/views/knowledgeBase/List.vue'),
  meta: { menuKey: 'knowledgeBase', menuSection: 'aiAgent', menuParents: ["agentSupport"], menuOrder: 5, menuIcon: 'Files', title: '知识库管理', group: 'knowledge', icon: 'Files' }
}, {
  path: '/knowledgeBase/list',
  name: 'KnowledgeBaseList',
  component: () => import('@/views/knowledgeBase/List.vue'),
  meta: { menuHidden: true, title: '知识库列表', group: 'knowledge', icon: 'List' }
}, {
  path: '/knowledgeBase/create',
  name: 'KnowledgeBaseCreate',
  component: () => import('@/views/knowledgeBase/List.vue'),
  meta: { menuHidden: true, title: '新建知识库', group: 'knowledge', icon: 'Plus' }
}, {
  path: '/knowledgeBase/edit/:id',
  name: 'KnowledgeBaseEdit',
  component: () => import('@/views/knowledgeBase/List.vue'),
  meta: { menuHidden: true, title: '编辑知识库', group: 'knowledge', icon: 'Edit' }
}];
