export default [{
  path: '/faq',
  name: 'FAQ',
  component: () => import('@/views/faq/Index.vue'),
  meta: { menuHidden: true, title: 'FAQ 知识库', group: 'aiAgent', icon: 'Notebook' }
}, {
  path: '/faq/list',
  name: 'FAQList',
  component: () => import('@/views/faq/List.vue'),
  meta: { menuKey: 'faqKb', menuSection: 'aiAgent', menuParents: ["agentSupport"], menuOrder: 4, menuIcon: 'Notebook', title: 'FAQ 列表', group: 'aiAgent', icon: 'List' }
}, {
  path: '/faq/editor',
  name: 'FAQEditor',
  component: () => import('@/views/faq/Editor.vue'),
  meta: { menuHidden: true, title: 'FAQ 编辑', group: 'aiAgent', icon: 'Edit', hideInMenu: true }
}, {
  path: '/faq/editor/:id',
  name: 'FAQEditorEdit',
  component: () => import('@/views/faq/Editor.vue'),
  meta: { menuHidden: true, title: 'FAQ 编辑', group: 'aiAgent', icon: 'Edit', hideInMenu: true }
}];
