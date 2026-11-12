// ego-helpers.mjs — user-web 回归点验的 ego-browser 助手（规则-1 合规）。
//
// 纪律（hivemtk/CLAUDE.md 规则-1）：本模块只提供「读快照、显式单元素操作、接口探针」
// 三类能力；**不提供**元素枚举、循环点击、跑批驱动——那些是明令禁止的。
// 每个函数的粒度都是"一次一个元素 / 一次一个请求"，调用方逐页、逐元素串行调用。
//
// 用法（ego-browser 脚本无相对导入基，用绝对 file:// URL）：
//   import { openWithConsole, drainConsoleErrors, apiProbe, loginStandalone, loginSaaS }
//     from 'file:///Users/xiaofang/Documents/www/go/hivemtk/hivemtk/scripts/regression/ego-helpers.mjs';

const CDP_ON = new WeakSet()

// 进入页面并挂上 console 采集。返回 page；错误用 drainConsoleErrors(page) 收。
export async function openWithConsole(page, url, { timeout = 30000 } = {}) {
  if (!CDP_ON.has(page)) {
    // 会话态隐藏页面 console，调试需显式关掉自动化伪装再挂 Runtime
    await page.cdp('egoAutomated.disable')
    await page.cdp('Runtime.enable')
    CDP_ON.add(page)
  }
  await page.goto(url, { timeout })
  await page.waitForSelector('body *', { timeout: 10000 }).catch(() => {})
  return page
}

// 收走 console.error 事件（page.events() 取走即清空，含 Runtime.consoleAPICalled）。
// 用法：const errs = await drainConsoleErrorsAsync(page); console.log("console.error:", errs.length)
export async function drainConsoleErrorsAsync(page) {
  const events = (await page.events()) || []
  return events.filter(
    (e) => e.method === 'Runtime.consoleAPICalled' && (e.params || {}).type === 'error'
  )
}

// 带 localStorage token 的接口探针（在页面源里发，同源 cookie/网络语义与真实前端一致）。
// 返回 { status, ok, body }。只读校验与写后复核都用它；POST/PUT/DELETE 是真实写操作，
// 调用方必须自己确认测试数据边界（本地库/测试租户）。
export async function apiProbe(page, method, path, body) {
  const result = await page.evaluate(`(async () => {
    const token = localStorage.getItem('token') || ''
    const r = await fetch('${path}', {
      method: '${method}',
      headers: { 'content-type': 'application/json', 'authorization': 'Bearer ' + token },
      body: ${body === undefined ? 'undefined' : JSON.stringify(JSON.stringify(body))}
    })
    const t = await r.text()
    return JSON.stringify({ status: r.status, ok: r.ok, body: t.slice(0, 2000) })
  })()`)
  return JSON.parse(result)
}

// 独立部署（8320）登录：admin 表单，逐元素显式填值。
export async function loginStandalone(page, { username, password } = {}) {
  await page.goto('http://127.0.0.1:8320/#/login')
  await page.waitForSelector("css:input[placeholder='用户名']", { timeout: 10000 })
  await page.fill("css:input[placeholder='用户名']", username)
  await page.fill("css:input[placeholder='密码']", password)
  await page.click("loc=role:button[name='登录']", { label: '独立形态登录' })
  await page.waitForURL(/#\/(messageHub|profile|inbox)/, { timeout: 15000 }).catch(() => {})
  return page.url()
}

// SaaS（8321）登录：多租户代码表单，逐元素显式填值。
export async function loginSaaS(page, { username, password, tenantCode } = {}) {
  await page.goto('http://127.0.0.1:8321/#/login')
  await page.waitForSelector("css:input[placeholder='用户名']", { timeout: 10000 })
  await page.fill("css:input[placeholder='用户名']", username)
  const tenantInput = "css:input[placeholder='租户代码（如：t42xk，可不填）']"
  if (tenantCode) await page.fill(tenantInput, tenantCode)
  await page.fill("css:input[placeholder='密码']", password)
  await page.click("loc=role:button[name='登录']", { label: 'SaaS 登录' })
  await page.waitForURL(/#\/(profile|messageHub|inbox)/, { timeout: 15000 }).catch(() => {})
  return page.url()
}
