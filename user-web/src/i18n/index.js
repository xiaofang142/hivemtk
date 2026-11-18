import { createI18n } from 'vue-i18n';
import { getStoredLocale, applyDirection } from './locale'
// 直引 locales/*.json：由 vite.config.js 的 hivemtk-i18n-functions 在编译期
// 预编译为消息函数（jit:false），与运行时构建配对；不再经过
// @intlify/unplugin-vue-i18n 的虚拟模块（它只输出 JIT AST，运行时构建解析不了）。
import { messages } from './catalog'

const locale = getStoredLocale()
applyDirection(locale)

const i18n = createI18n({
  legacy: false,
  locale,
  fallbackLocale: 'zh',
  messages,
  missingWarn: false,
  fallbackWarn: false,
})

export default i18n
