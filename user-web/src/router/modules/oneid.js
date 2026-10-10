export default [{
  path: '/oneid/list',
  name: 'OneIDList',
  component: () => import('@/views/oneid/List.vue'),
  meta: { menuKey: 'oneidList', menuSection: 'customer', menuParents: ["customerIdentity"], menuOrder: 1, menuIcon: 'List', title: 'OneID 列表', icon: 'List', requiresAuth: true }
}, {
  path: '/oneid/conflicts',
  name: 'OneIDConflicts',
  component: () => import('@/views/oneid/Conflicts.vue'),
  meta: { menuKey: 'oneidConflicts', menuSection: 'customer', menuParents: ["customerIdentity"], menuOrder: 2, menuIcon: 'Warning', title: '身份冲突解决', icon: 'Warning', requiresAuth: true }
}, {
  path: '/oneid/merge-rules',
  name: 'OneIDMergeRules',
  component: () => import('@/views/oneid/MergeRuleConfig.vue'),
  meta: { menuKey: 'oneidMergeRules', menuSection: 'customer', menuParents: ["customerIdentity"], menuOrder: 3, menuIcon: 'Setting', title: '合并规则配置', icon: 'Setting', requiresAuth: true, tag: 'OPT-UX-04' }
}];
