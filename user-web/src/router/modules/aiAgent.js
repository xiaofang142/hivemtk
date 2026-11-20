const loadList = () => import('@/views/aiAgent/List.vue')
const loadEditor = () => import('@/views/aiAgent/Edit.vue')
const listPath = '/aiAgent/list'

export default [
  {
    path: '/aiAgent',
    name: 'AIAgent',
    component: loadList,
    meta: { title: '智能体', group: 'aiAgent', icon: 'Cpu', listEditorBase: listPath }
  },
  {
    path: '/aiAgent/list',
    name: 'AIAgentList',
    component: loadList,
    meta: { title: '智能体列表', group: 'aiAgent', icon: 'List' }
  },
  {
    path: '/aiAgent/create',
    name: 'AIAgentCreate',
    components: { default: loadEditor, list: loadList },
    meta: { title: '创建智能体', group: 'aiAgent', icon: 'Plus', listEditor: listPath }
  },
  {
    path: '/aiAgent/edit/:id',
    name: 'AIAgentEdit',
    components: { default: loadEditor, list: loadList },
    meta: { title: '编辑智能体', breadcrumbKey: 'navigation.editAgent', group: 'aiAgent', icon: 'Edit', listEditor: listPath }
  },
  {
    path: '/aiAgent/integrations',
    name: 'AICustomIntegration',
    component: () => import('@/views/aiAgent/CustomIntegration.vue'),
    meta: { title: '工具接入', group: 'aiAgent', icon: 'Link' }
  },
  {
    path: '/aiAgent/tools',
    name: 'AIToolManagement',
    component: () => import('@/views/aiAgent/ToolManagement/ToolList.vue'),
    meta: { title: 'AI 工具管理', group: 'aiAgent', icon: 'Tools' }
  }
]
