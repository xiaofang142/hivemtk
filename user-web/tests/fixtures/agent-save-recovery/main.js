import { createApp, defineComponent, h, ref } from 'vue'
import { createRouter, createMemoryHistory, RouterView } from 'vue-router'
import ElementPlus from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import 'element-plus/dist/index.css'
import { createI18n } from 'vue-i18n'
import zh from '../../../src/i18n/locales/zh.json'
import Edit from '../../../src/views/aiAgent/Edit.vue'

const router = createRouter({ history: createMemoryHistory(), routes: [
  { path: '/aiAgent/create', component: Edit },
  { path: '/aiAgent/edit/:id', component: Edit },
  { path: '/aiAgent/list', component: { render: () => h('p', { role: 'status' }, '整体回读已确认，已返回列表') } },
] })
const App = defineComponent({ setup() {
  const version = ref(0), notice = ref('更新场景：下一次绑定写入拒绝，主记录可先保存。只使用本地合成接口。')
  const reset = async scenario => {
    const response = await fetch(`/fixture/reset/${scenario}`, { method: 'POST' })
    if (!response.ok) { notice.value = '夹具重置失败'; return }
    await router.push(scenario === 'update' ? '/aiAgent/edit/1' : '/aiAgent/create')
    version.value++
    notice.value = scenario === 'lost' ? '创建将提交但返回受控 503；重试应精确回读同一记录。' : '下一次绑定写入将被受控拒绝；重试应复用已存主记录。'
  }
  return () => h('main', [
    h('aside', { style: 'padding:12px;background:#fff7df' }, [
      h('strong', 'FA-AGENT-UI-03 本地受控验收 '),
      h('button', { onClick: () => reset('update') }, '建立更新场景'),
      h('button', { onClick: () => reset('create') }, '建立创建场景'),
      h('button', { onClick: () => reset('lost') }, '建立回执丢失场景'),
      h('p', { role: 'status' }, notice.value),
    ]),
    h(RouterView, { key: version.value }),
  ])
} })
await router.push('/aiAgent/edit/1')
createApp(App).component('Refresh', Refresh).use(router).use(ElementPlus).use(createI18n({ legacy: false, locale: 'zh', messages: { zh } })).mount('#app')
