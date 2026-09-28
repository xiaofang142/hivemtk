// 执行入口的错误分流：任务列表页与任务详情页共用同一份判据。
// 同一个「执行」按钮在两个入口必须给出同一种结局，否则用户在详情页拿到「Host 未连接」
// 时既不知道要装什么、也不知道去哪儿装。
export const HOST_OFFLINE = 'host-offline'
export const TASK_BUSY = 'task-busy'
export const OTHER = 'other'

export const RUN_ERROR_TEXT = (err) => String(err?.message || err)

// 分流只看业务码，不看文案也不只看 HTTP 状态：409 在域内有三条结论
// （Host 未连接 / 同 Host 串行闸占用 / 依赖未满足），只有第一条该开安装引导。
export function classifyRunError(err) {
  switch (err?.bizCode) {
    case 'BROWSER_HOST_OFFLINE_8001':
      return HOST_OFFLINE
    case 'BROWSER_TASK_BUSY_8002':
      return TASK_BUSY
    default:
      // 兜底：老服务端/网关没带码时仍按 409+文案给引导，别把用户晾在静默里
      if (err?.status === 409 && RUN_ERROR_TEXT(err).includes('未连接')) return HOST_OFFLINE
      return OTHER
  }
}
