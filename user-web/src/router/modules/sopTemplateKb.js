export default [{
  path: '/sop-template',
  name: 'SOPTemplate',
  component: () => import('@/views/sopTemplate/Index.vue'),
  meta: { menuHidden: true, title: 'SOP 模板库', group: 'aiAgent', icon: 'Tickets' }
}, {
  path: '/sop-template/list',
  name: 'SOPTemplateList',
  component: () => import('@/views/sopTemplate/List.vue'),
  meta: { menuKey: 'sopTemplate', menuSection: 'aiAgent', menuParents: ["agentSupport"], menuOrder: 2, menuIcon: 'Tickets', title: 'SOP 模板列表', group: 'aiAgent', icon: 'List' }
}, {
  path: '/sop-template/market',
  name: 'SOPTemplateMarket',
  component: () => import('@/views/sopTemplate/Market.vue'),
  meta: { menuKey: 'sopTemplateMarket', menuSection: 'aiAgent', menuParents: ["agentSupport"], menuOrder: 3, menuIcon: 'ShoppingCart', title: 'SOP 模板市场', group: 'aiAgent', icon: 'Shop', tag: 'OPT-UX-06' }
}, {
  path: '/sop-template/editor',
  name: 'SOPTemplateEditor',
  component: () => import('@/views/sopTemplate/Editor.vue'),
  meta: { menuHidden: true, title: 'SOP 模板编辑', group: 'aiAgent', icon: 'Edit', hideInMenu: true }
}, {
  path: '/sop-template/editor/:id',
  name: 'SOPTemplateEditorEdit',
  component: () => import('@/views/sopTemplate/Editor.vue'),
  meta: { menuHidden: true, title: 'SOP 模板编辑', group: 'aiAgent', icon: 'Edit', hideInMenu: true }
}];
