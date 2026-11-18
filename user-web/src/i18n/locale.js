import { messages } from './catalog'

export const SUPPORTED_LOCALES = [
  { code: 'zh', label: '简体中文', el: 'zh-cn', rtl: false },
  { code: 'en', label: 'English', el: 'en', rtl: false },
  { code: 'ja', label: '日本語', el: 'ja', rtl: false },
  {
    code: 'ar',
    label: 'العربية',
    el: 'en',
    rtl: true
  },
  {
    code: 'es',
    label: 'Español',
    el: 'es',
    rtl: false
  },
  { code: 'fr', label: 'Français', el: 'fr', rtl: false },
  { code: 'de', label: 'Deutsch', el: 'de', rtl: false },
  { code: 'ru', label: 'Русский', el: 'ru', rtl: false },
  { code: 'pt', label: 'Português', el: 'pt', rtl: false },
];

const STORAGE_KEY = 'app_locale'

export function isSupported(code) {
  return SUPPORTED_LOCALES.some((l) => l.code === code)
}

export function getStoredLocale() {
  const v = localStorage.getItem(STORAGE_KEY)
  if (v && isSupported(v)) return v
  const nav = (navigator.language || 'zh').toLowerCase();
  const base = nav.split('-')[0]
  if (base === 'en') return 'en'
  if (base === 'ja') return 'ja'
  if (base === 'ar') return 'ar'
  if (base === 'es') return 'es'
  if (base === 'fr') return 'fr'
  if (base === 'de') return 'de'
  if (base === 'ru') return 'ru'
  if (base === 'pt') return 'pt'
  return 'zh'
}

export function setLocale(locale) {
  if (!isSupported(locale)) locale = 'zh'
  localStorage.setItem(STORAGE_KEY, locale)
  applyDirection(locale)
  return locale
}

export function applyDirection(locale) {
  const item = SUPPORTED_LOCALES.find((l) => l.code === locale)
  const dir = item && item.rtl ? 'rtl' : 'ltr'
  document.documentElement.setAttribute('dir', dir)
  document.documentElement.setAttribute('lang', locale)
}

export function elLocaleCode(locale) {
  const item = SUPPORTED_LOCALES.find((l) => l.code === locale)
  return item ? item.el : 'zh-cn'
}

const loadedLocales = new Set();

export async function loadLocaleMessages(locale) {
  if (loadedLocales.has(locale)) return null
  if (!isSupported(locale)) return null
  try {
    const catalog = messages[locale]
    loadedLocales.add(locale)
    return catalog
  } catch (err) {
    console.error(`[i18n] 加载语言包失败: ${locale}`, err)
    return null
  }
}
