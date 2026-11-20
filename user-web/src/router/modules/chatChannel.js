const loadList = () => import('@/views/chatChannel/List.vue')
const listPath = '/chatChannel/list'

export default [{
  path: '/chatChannel/list',
  name: 'ChatChannelList',
  component: loadList,
  meta: { title: '客服渠道', group: 'customer', icon: 'Connection', requiresAuth: true }
}, {
  path: '/chatChannel/create',
  name: 'ChatChannelCreate',
  components: { default: () => import('@/views/chatChannel/Create.vue'), list: loadList },
  meta: { title: '新建客服渠道', breadcrumbKey: '新建渠道', group: 'customer', icon: 'Plus', requiresAuth: true, listEditor: listPath }
}, {
  path: '/chatChannel/edit/:id',
  name: 'ChatChannelEdit',
  components: { default: () => import('@/views/chatChannel/Edit.vue'), list: loadList },
  meta: { title: '编辑客服渠道', group: 'customer', icon: 'Edit', requiresAuth: true, listEditor: listPath }
}, {
  path: '/chatChannel/install-guide/:id?',
  name: 'ChatChannelInstallGuide',
  component: () => import('@/views/chatChannel/InstallGuide.vue'),
  meta: { title: 'Widget 安装引导', group: 'customer', icon: 'Guide', requiresAuth: true }
}];
