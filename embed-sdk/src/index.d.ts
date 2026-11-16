/**
 * HiveMtk Embed SDK 类型声明
 * 用于 TypeScript 项目集成（widget.iife.js / widget.esm.js）
 */

export interface WidgetConfig {
  /** 业务标识 channel_ref */
  channelRef: string
  /** API 基础 URL（user-server 完整地址，含协议） */
  apiBaseURL: string
  /** 嵌入位置 selector（可选），默认 'body' */
  position?: 'body' | string
  /** WebSocket 路径（可选），默认 '/api/ws/visitor' */
  wsPath?: string
  /** 跨域白名单（postMessage 接收端 origin） */
  allowedOrigins?: string[]
  /** 界面语言 */
  language?: 'zh' | 'en' | 'ja' | 'ar'
  /** 主题色 */
  themeColor?: string
  /** 访客隐私告知同意闸（specs/023）：缺省 null = 关闭，行为与历史一致 */
  privacyNotice?: PrivacyNoticeConfig
  /** 自定义事件回调 */
  events?: WidgetEvents
  /** 调试模式（打印详细日志） */
  debug?: boolean
}

export interface PrivacyNoticeConfig {
  /** 是否启用同意闸（required=true 时首次打开先询问，同意后才创建聊天 iframe） */
  required?: boolean
  /** 告知文案；未配时按 lang 取内置默认（中/英） */
  text?: string
  /** 隐私政策链接；非空时渲染为新窗打开的链接 */
  policyUrl?: string
  /** 文案版本；变更后已同意的访客会重新收到询问 */
  version?: string
}

export interface WidgetEvents {
  onReady?: (payload: { channelRef: string }) => void
  onOpen?: (payload?: any) => void
  onClose?: (payload?: any) => void
  onMessage?: (payload: { type: string; payload: any }) => void
  onUnread?: (payload: { count: number }) => void
  /** specs/023：同意闸用户作出选择时触发 */
  onPrivacyConsent?: (payload: { accepted: boolean }) => void
}

export interface UnreadPayload {
  count: number
}

export interface MessagePayload {
  type: string
  payload: any
}

declare class MarketingChatWidget {
  constructor(config: WidgetConfig)
  init(): void
  open(): void
  close(): void
  toggle(): void
  destroy(): void
  setUnread(count: number): void
}

export interface MarketingChatWidgetGlobal {
  init(): void
  open(): void
  close(): void
  toggle(): void
  destroy(): void
}

declare global {
  interface Window {
    MarketingChatWidget?: typeof MarketingChatWidget
    mcwInstance?: MarketingChatWidgetGlobal
  }
}

export default MarketingChatWidget
export { MarketingChatWidget }
