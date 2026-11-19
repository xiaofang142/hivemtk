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
  { path: '/aiAgent/list', component: { render: () => h('p', { role: 'status' }, '已返回列表；可重新编辑记录核对持久化模式') } },
] })
const App = defineComponent({ setup() {
  const version = ref(0), notice = ref('仅本地合成接口；真实运行分派另由 PostgreSQL/HTTP 测试验证')
  const open = async path => { await router.push(path); version.value++ }
  const fail = async () => {
    const response = await fetch('/fixture/fail-next', { method: 'POST' })
    notice.value = response.ok ? '下次保存将被受控拒绝' : '夹具控制失败'
  }
  return () => h('main', [
    h('aside', { style: 'padding:12px;background:#fff7df' }, [
      h('strong', 'FA-AGENT-UI-01 本地受控验收 '),
      h('button', { onClick: () => open('/aiAgent/create') }, '新建记录'),
      h('button', { onClick: () => open('/aiAgent/edit/1') }, '重新编辑记录'),
      h('button', { onClick: fail }, '下次保存拒绝'),
      h('p', { role: 'status' }, notice.value),
    ]),
    h(RouterView, { key: version.value }),
  ])
} })
await router.push('/aiAgent/create')
createApp(App).component('Refresh', Refresh).use(router).use(ElementPlus).use(createI18n({ legacy: false, locale: 'zh', messages: { zh } })).mount('#app')
