export default [
  {
    path: '/sms/list',
    name: 'SmsList',
    component: () => import('@/views/sms/List.vue'),
    meta: { menuKey: 'smsList', menuSection: 'reach', menuParents: ["smsReach"], menuOrder: 1, menuIcon: 'ChatDotSquare', title: '短信列表', group: 'reach', icon: 'ChatDotSquare' }
  },
  {
    path: '/sms/drafts',
    name: 'SmsDrafts',
    component: () => import('@/views/sms/Drafts.vue'),
    meta: { menuKey: 'smsDrafts', menuSection: 'reach', menuParents: ["smsReach"], menuOrder: 2, menuIcon: 'Document', title: '短信草稿', group: 'reach', icon: 'Document' }
  },
  {
    path: '/sms/jobs',
    name: 'SmsJobs',
    component: () => import('@/views/sms/Jobs.vue'),
    meta: { menuKey: 'smsJobs', menuSection: 'reach', menuParents: ["smsReach"], menuOrder: 3, menuIcon: 'List', title: '短信任务', group: 'reach', icon: 'List' }
  },
  {
    path: '/sms/config',
    name: 'SmsConfig',
    component: () => import('@/views/sms/Config.vue'),
    meta: { menuKey: 'smsConfig', menuSection: 'reach', menuParents: ["smsReach"], menuOrder: 4, menuIcon: 'Setting', title: '短信配置', group: 'reach', icon: 'Setting' }
  }
]