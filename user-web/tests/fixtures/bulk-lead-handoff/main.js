import { createApp, defineComponent, h } from 'vue'
import { createPinia } from 'pinia'
import { createRouter, createWebHashHistory, RouterView, RouterLink } from 'vue-router'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import { useUserStore } from '../../../src/stores/user'
import LeadGroupSelection from '../../../src/views/whatsappBot/LeadGroupSelection.vue'
import BulkMessaging from '../../../src/views/whatsappBot/BulkMessaging.vue'
import { fixtureI18n } from './api'

const pinia = createPinia()
useUserStore(pinia).setUserInfo({ id: 'synthetic-operator', tenant_id: 'synthetic-tenant' })
const router = createRouter({ history: createWebHashHistory(), routes: [
  { path: '/', redirect: '/whatsapp/lead-group-selection' },
  { path: '/whatsapp/lead-group-selection', component: LeadGroupSelection },
  { path: '/whatsapp/group-messaging', component: BulkMessaging },
] })
const App = defineComponent({
  setup() {
    return () => h('main', [
      h('aside', { style: 'padding:12px;background:#fff7df' }, [
        h('strong', 'NAV04 本地受控验收：仅合成线索，不连接第三方'),
        h('p', [
          h(RouterLink, { to: '/whatsapp/lead-group-selection' }, () => '重新选择线索'),
          ' · ',
          h(RouterLink, { to: '/whatsapp/group-messaging' }, () => '直接打开群发页'),
        ]),
      ]),
      h(RouterView, {}, { default: ({ Component }) => h(Component, { key: router.currentRoute.value.fullPath }) }),
    ])
  },
})
createApp(App).use(pinia).use(router).use(ElementPlus).use(fixtureI18n).mount('#app')
