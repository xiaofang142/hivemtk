import { createI18n } from 'vue-i18n'
import { getStoredLocale } from './locale.js'

const modules = import.meta.glob('./modules/*.js', { eager: true })
const messages = { zh: {}, en: {}, ja: {}, ar: {} }

for (const path in modules) {
  const mod = modules[path].default || modules[path]
  for (const loc of ['zh', 'en', 'ja', 'ar']) {
    if (mod[loc]) Object.assign(messages[loc], mod[loc])
  }
}

const i18n = createI18n({
  legacy: false,
  locale: getStoredLocale(),
  fallbackLocale: 'en', 
  messages,
})

export default i18n

