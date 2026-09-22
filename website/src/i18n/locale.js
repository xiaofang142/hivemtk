// website 语言列表与持久化工具（与 user-web 一致）。
export const LANGS = [
  { code: 'zh', label: '简体中文' },
  { code: 'en', label: 'English' },
  { code: 'ja', label: '日本語' },
  { code: 'ar', label: 'العربية' },
]

const STORAGE_KEY = 'website-locale'

// 语言选择优先级：URL 的 ?lang= > localStorage > 浏览器默认语言。
// index.html 的 hreflang 与 public/sitemap.xml 共 40 余条 alternate 全部发布成
// `?lang=xx` 形式，首屏若不读它，搜索引擎抓到的四个语言版本就都是中文。
function getLocaleFromURL() {
  if (typeof window === 'undefined') return ''
  const q = new URLSearchParams(window.location.search).get('lang') || ''
  return LANGS.some((l) => l.code === q) ? q : ''
}

export function getStoredLocale() {
  const fromURL = getLocaleFromURL()
  if (typeof localStorage !== 'undefined') {
    if (fromURL) {
      localStorage.setItem(STORAGE_KEY, fromURL)
      return fromURL
    }
    const stored = localStorage.getItem(STORAGE_KEY)
    if (stored && LANGS.some((l) => l.code === stored)) return stored
  } else if (fromURL) {
    return fromURL
  }
  // 跟随浏览器默认语言
  const nav = typeof navigator !== 'undefined' ? navigator.language || '' : ''
  if (nav.startsWith('en')) return 'en'
  if (nav.startsWith('ja')) return 'ja'
  if (nav.startsWith('ar')) return 'ar'
  return 'zh'
}

export function setStoredLocale(code) {
  if (typeof localStorage === 'undefined') return
  localStorage.setItem(STORAGE_KEY, code)
}

// 阿拉伯语从右向左排版
export function applyDirection(locale) {
  if (typeof document === 'undefined') return
  document.documentElement.setAttribute('dir', locale === 'ar' ? 'rtl' : 'ltr')
  document.documentElement.setAttribute('lang', locale)
}

