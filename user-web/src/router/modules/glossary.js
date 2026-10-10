export default [{
  path: '/glossary',
  name: 'GlossaryList',
  component: () => import('@/views/glossary/List.vue'),
  meta: { menuKey: 'glossary', menuSection: 'knowledge', menuParents: ["i18nManage"], menuOrder: 1, menuRoles: ["admin","manager"], menuIcon: 'Collection', title: '术语表管理', group: 'i18n', icon: 'Collection', requiresAuth: true }
}];
