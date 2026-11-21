import { createApp, defineComponent, h, shallowRef } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import * as icons from '@element-plus/icons-vue'
import 'element-plus/dist/index.css'
import i18n from '../../../src/i18n'
import Batch from '../../../src/views/batchOperation/List.vue'
import ToolFlow from '../../../src/views/intentRecognition/ToolFlow.vue'
import BatchImport from '../../../src/views/KnowledgeWorkspace/BatchImport.vue'
import ConfigParams from '../../../src/views/system/ConfigParams.vue'
import MaterialLibrary from '../../../src/views/system/MaterialLibrary.vue'
import HelpCenter from '../../../src/views/public/HelpCenter.vue'
import SeoInfra from '../../../src/views/geo/SeoInfra.vue'
const pages = { '批量操作': Batch, '意图工具流': ToolFlow, '知识批量导入': BatchImport, '参数中心': ConfigParams, '素材库': MaterialLibrary, '帮助中心': HelpCenter, 'SEO预览': SeoInfra }
const App = defineComponent({
  setup() {
    const current = shallowRef(Batch)
    return () => h('main', [
      h('aside', { style: 'padding:12px;background:#fff2cc' }, [
        h('p', '本地受控审计：真实页面组件 + 合成数据。未实现接口返回 501；不连接真实业务或外部服务。'),
        ...Object.entries(pages).map(([label, component]) => h('button', { onClick: () => { current.value = component } }, `打开${label}`)),
      ]),
      h(current.value),
    ])
  },
})
const app = createApp(App).use(createPinia()).use(ElementPlus).use(i18n)
for (const [name, component] of Object.entries(icons)) app.component(name, component)
app.mount('#app')
