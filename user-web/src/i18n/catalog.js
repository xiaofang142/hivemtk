import resources from './resources.json'
import { assembleResources } from './assembleResources'

// eager JSON 导入仍由构建插件预编译为消息函数，不引入运行时编译。
const files = import.meta.glob('./locales/*.json', { eager: true, import: 'default' })
export const messages = assembleResources(resources, file => files[`./locales/${file}`])
