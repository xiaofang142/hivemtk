import { createApp, defineComponent, h, ref } from 'vue'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import { createI18n } from 'vue-i18n'
import zh from '../../../src/i18n/locales/zh.json'
import ToolList from '../../../src/views/aiAgent/ToolManagement/ToolList.vue'

const App = defineComponent({
  setup() {
    const version = ref(0)
    const notice = ref('仅本地合成数据；不访问生产接口')
    const control = async (action, remount = false) => {
      const response = await fetch(`/fixture/control/${action}`, { method: 'POST' })
      if (!response.ok) throw new Error(`夹具控制失败 ${response.status}`)
      notice.value = await response.text()
      if (remount) version.value++
    }
    return () => h('main', [
      h('aside', { style: 'padding:12px;background:#fff7df' }, [
        h('strong', 'FA-AGENT-TOOL-03 本地受控验收 '),
        h('button', { onClick: () => control('21', true) }, '载入 21 条'),
        h('button', { onClick: () => control('101', true) }, '载入 101 条'),
        h('button', { onClick: () => control('remove-last') }, '移除末项'),
        h('button', { onClick: () => control('fail-next') }, '下次读取失败'),
        h('p', { role: 'status' }, notice.value),
      ]),
      h(ToolList, { key: version.value }),
    ])
  },
})
createApp(App).use(ElementPlus).use(createI18n({ legacy: false, locale: 'zh', messages: { zh } })).mount('#app')
