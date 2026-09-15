


/**
 * @file 浮标按钮
 * @description 纯 DOM 实现,无框架依赖;负责挂载 / 卸载 / 开关态与未读角标
 */

/**
 * 浮标位置
 * @typedef {'bottom-right' | 'bottom-left'} McwPosition
 */

/**
 * 浮标按钮构造选项
 * @typedef {Object} FloatingButtonOptions
 * @property {string}      [color='#1989fa']  浮标主色(hex)
 * @property {McwPosition} [position='bottom-right']  浮标位置
 * @property {number}      [zIndex=9999]     浮标层级
 * @property {number}      [offsetX=24]      水平边距(px)
 * @property {number}      [offsetY=24]      垂直边距(px)
 * @property {Function}    [onClick]         点击回调,参数:(opened:boolean)
 * @property {number}      [unread=0]        初始未读数
 */

const ICON_SVG = `
<svg viewBox="0 0 24 24" width="28" height="28" fill="currentColor">
  <path d="M20 2H4c-1.1 0-1.99.9-1.99 2L2 22l4-4h14c1.1 0 2-.9 2-2V4c0-1.1-.9-2-2-2zm-2 12H6v-2h12v2zm0-3H6V9h12v2zm0-3H6V6h12v2z"/>
</svg>
`

const CLOSE_SVG = `
<svg viewBox="0 0 24 24" width="24" height="24" fill="currentColor">
  <path d="M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z"/>
</svg>
`

export class FloatingButton {
  /**
   * @param {FloatingButtonOptions} options
   */
  constructor(options) {
    this.color = options.color || '#1989fa'
    this.position = options.position || 'bottom-right'
    this.zIndex = options.zIndex || 9999
    this.offsetX = options.offsetX || 24
    this.offsetY = options.offsetY || 24
    this.onClick = options.onClick || (() => {})
    this.unread = options.unread || 0
    this.button = null
  }

  mount() {
    if (this.button) return
    const btn = document.createElement('div')
    btn.className = 'mcw-floating-btn'
    btn.setAttribute('role', 'button')
    btn.setAttribute('tabindex', '0')
    btn.setAttribute('aria-label', '打开在线客服')
    btn.setAttribute('aria-expanded', 'false')
    btn.setAttribute('aria-haspopup', 'dialog')
    btn.style.cssText = this.getStyle()
    btn.innerHTML = `<span class="mcw-fab-icon" aria-hidden="true">${ICON_SVG}</span><span class="mcw-fab-badge" aria-hidden="true" style="display:none;position:absolute;top:-4px;right:-4px;min-width:18px;height:18px;padding:0 4px;box-sizing:border-box;border-radius:9px;background:#f56c6c;color:#fff;font-size:12px;line-height:18px;text-align:center;align-items:center;justify-content:center">0</span>`
    btn.addEventListener('click', () => this.toggle())
    btn.addEventListener('keydown', (ev) => {
      if (ev.key === 'Enter' || ev.key === ' ') {
        ev.preventDefault()
        this.toggle()
      }
    })
    document.body.appendChild(btn)
    this.button = btn
  }

  toggle() {
    if (!this.button) return
    const opened = this.button.classList.toggle('mcw-open')
    this.button.setAttribute('aria-expanded', String(opened))
    const icon = this.button.querySelector('.mcw-fab-icon')
    if (icon) icon.innerHTML = opened ? CLOSE_SVG : ICON_SVG
    this.onClick(opened)
  }

  unmount() {
    if (this.button) {
      this.button.remove()
      this.button = null
    }
  }

  setOpen(opened) {
    if (!this.button) return
    const icon = this.button.querySelector('.mcw-fab-icon')
    if (opened) {
      this.button.classList.add('mcw-open')
      if (icon) icon.innerHTML = CLOSE_SVG
      this.setUnread(0)
    } else {
      this.button.classList.remove('mcw-open')
      if (icon) icon.innerHTML = ICON_SVG
    }
  }

  /**
   * 设置未读角标(超过 99 显示 99+)
   * @param {number} count
   * @returns {void}
   */
  setUnread(count) {
    this.unread = count || 0
    const badge = this.button && this.button.querySelector('.mcw-fab-badge')
    if (badge) {
      badge.style.display = this.unread > 0 ? 'flex' : 'none'
      badge.textContent = this.unread > 99 ? '99+' : String(this.unread)
    }
  }

  getStyle() {
    const isLeft = this.position === 'bottom-left';
    return [
      'position: fixed',
      `bottom: ${this.offsetY}px`,
      isLeft ? `inset-inline-start: ${this.offsetX}px` : `inset-inline-end: ${this.offsetX}px`,
      `z-index: ${this.zIndex}`,
      `background: ${this.color}`,
      'color: #fff',
      'width: 56px',
      'height: 56px',
      'border-radius: 50%',
      'box-shadow: 0 4px 16px rgba(0, 0, 0, 0.2)',
      'cursor: pointer',
      'display: flex',
      'align-items: center',
      'justify-content: center',
      'transition: transform 0.2s, box-shadow 0.2s',
      'user-select: none'
    ].join(';')
  }
}

