import { createApp, defineComponent, h, ref } from 'vue'
import { createRouter, createMemoryHistory, RouterView } from 'vue-router'
import ElementPlus from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import 'element-plus/dist/index.css'
import { createI18n } from 'vue-i18n'
import zh from '../../../src/i18n/locales/zh.json'
import Edit from '../../../src/views/aiAgent/Edit.vue'

const router = createRouter({ history: createMemoryHistory(), routes: [
  { path: '/aiAgent/edit/:id', component: Edit },
  { path: '/aiAgent/list', component: { render: () => h('p', { role: 'status' }, '已返回列表；可重新编辑核对绑定') } },
] })
const App = defineComponent({ setup() {
  const version = ref(0), notice = ref('首次绑定读取失败；已有绑定为原知识库。仅本地合成接口。')
  const reopen = async () => { await router.push('/aiAgent/edit/1'); version.value++ }
  const allowRead = async () => {
    const response = await fetch('/fixture/allow-read', { method: 'POST' })
    notice.value = response.ok ? '后续读取将成功；请在页面点击加载知识库' : '夹具控制失败'
  }
  return () => h('main', [
    h('aside', { style: 'padding:12px;background:#fff7df' }, [
      h('strong', 'FA-AGENT-UI-02 本地受控验收 '),
      h('button', { onClick: reopen }, '重新编辑记录'),
      h('button', { onClick: allowRead }, '允许绑定读取'),
      h('p', { role: 'status' }, notice.value),
    ]),
    h(RouterView, { key: version.value }),
  ])
} })
await router.push('/aiAgent/edit/1')
createApp(App).component('Refresh', Refresh).use(router).use(ElementPlus).use(createI18n({ legacy: false, locale: 'zh', messages: { zh } })).mount('#app')
