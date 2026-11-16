/**
 * @file 访客隐私告知同意闸(specs/023)
 * @description 纯 JS、零依赖(ADR-011)。职责只有三件：
 *   1. 判定当前访客是否需要询问(needsConsent)——localStorage 里已同意的 version
 *      与配置 version 一致即免询问；localStorage 不可用(沙箱/隐私模式)按"未同意"处理
 *      (fail-closed：宁可多问一次，不可静默跳过告知)。
 *   2. 渲染同意层(show)——文案 + 可选政策链接 + 同意/拒绝；z-index 跟随实例。
 *   3. 记录与清理(destroy)。会话创建发生在 iframe 加载之后，本层不碰任何网络请求。
 */

/** @typedef {import('./config.js').PrivacyNoticeConfig} PrivacyNoticeConfig */

const TEXTS = {
  'zh-CN': {
    title: '隐私提示',
    defaultText: '在开始对话前请知悉：您发送的消息及预留的联系方式将由商家用于客服回复与回访。继续使用即表示您同意该用途。',
    agree: '同意并继续',
    decline: '暂不使用'
  },
  'en-US': {
    title: 'Privacy Notice',
    defaultText: 'Before starting the chat: messages and contact details you provide will be used by the merchant for customer service replies and follow-ups. Continuing means you agree to this use.',
    agree: 'Agree & continue',
    decline: 'Not now'
  }
}

function textFor(lang) {
  return TEXTS[lang] || TEXTS['zh-CN']
}

/**
 * 安全读 localStorage：沙箱/禁用环境一律回 null（fail-closed ⇒ 每次都询问）
 * @param {string} key
 * @returns {string|null}
 */
function safeGet(key) {
  try {
    if (typeof localStorage === 'undefined') return null
    return localStorage.getItem(key)
  } catch (_) {
    return null
  }
}

/**
 * 安全写 localStorage：不可用环境静默跳过（本轮会话内仍由 needsConsent 把关）
 * @param {string} key
 * @param {string} value
 */
function safeSet(key, value) {
  try {
    if (typeof localStorage === 'undefined') return
    localStorage.setItem(key, value)
  } catch (_) { /* 隐私模式等：写入失败不阻塞同意流程，只是下次会再问 */ }
}

function safeRemove(key) {
  try {
    if (typeof localStorage === 'undefined') return
    localStorage.removeItem(key)
  } catch (_) { /* 同上 */ }
}

export class ConsentGate {
  /**
   * @param {Object} options
   * @param {PrivacyNoticeConfig} options.notice   已消毒的 privacyNotice（required 必为 true）
   * @param {string}   [options.visitorIdKey='mtk_visitor_id']
   * @param {string}   [options.lang='zh-CN']
   * @param {number}   [options.zIndex=9999]
   * @param {Function} [options.onAgree]
   * @param {Function} [options.onDecline]
   */
  constructor(options) {
    this.notice = options.notice
    this.storageKey = `${options.visitorIdKey || 'mtk_visitor_id'}:privacy_consent`
    this.lang = options.lang || 'zh-CN'
    this.zIndex = options.zIndex || 9999
    this.onAgree = options.onAgree || (() => {})
    this.onDecline = options.onDecline || (() => {})
    this.el = null
  }

  /** 已同意过当前 version ⇒ 免询问；localStorage 不可用 ⇒ 恒询问（fail-closed） */
  needsConsent() {
    const accepted = safeGet(this.storageKey)
    return accepted !== (this.notice.version || '1')
  }

  /** 记录同意（version 一致后不再询问） */
  accept() {
    safeSet(this.storageKey, this.notice.version || '1')
  }

  /** 清除已同意记录（version 变更后由 needsConsent 自然重问；供宿主页面/测试复位） */
  revoke() {
    safeRemove(this.storageKey)
  }

  /**
   * 渲染同意层。返回根元素；按钮点击回调构造时注入的 onAgree/onDecline。
   * @returns {Element|null} localStorage 不可用且 DOM 缺失时返回 null（沙箱测试形态）
   */
  show() {
    if (typeof document === 'undefined' || !document.body) return null
    if (this.el) this.destroy()

    const t = textFor(this.lang)
    const root = document.createElement('div')
    root.className = 'mcw-consent'
    root.style.zIndex = String(this.zIndex + 1)

    const card = document.createElement('div')
    card.className = 'mcw-consent-card'

    const title = document.createElement('div')
    title.className = 'mcw-consent-title'
    title.textContent = t.title
    card.appendChild(title)

    const body = document.createElement('div')
    body.className = 'mcw-consent-text'
    body.textContent = this.notice.text || t.defaultText
    card.appendChild(body)

    if (this.notice.policyUrl) {
      const link = document.createElement('a')
      link.className = 'mcw-consent-link'
      link.href = this.notice.policyUrl
      link.target = '_blank'
      link.rel = 'noopener noreferrer'
      link.textContent = this.notice.policyUrl
      card.appendChild(link)
    }

    const actions = document.createElement('div')
    actions.className = 'mcw-consent-actions'

    const agree = document.createElement('button')
    agree.className = 'mcw-consent-agree'
    agree.type = 'button'
    agree.textContent = t.agree
    agree.addEventListener('click', () => {
      this.accept()
      this.destroy()
      this.onAgree()
    })
    actions.appendChild(agree)

    const decline = document.createElement('button')
    decline.className = 'mcw-consent-decline'
    decline.type = 'button'
    decline.textContent = t.decline
    decline.addEventListener('click', () => {
      this.destroy()
      this.onDecline()
    })
    actions.appendChild(decline)

    card.appendChild(actions)
    root.appendChild(card)
    document.body.appendChild(root)
    this.el = root
    return root
  }

  destroy() {
    if (this.el) {
      this.el.remove()
      this.el = null
    }
  }
}
