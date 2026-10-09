import { http } from '@/utils/request'

// 销售工作台三条读口的前端出口（对应后端 controller/sales_workbench.go）。
//
// 背景：I6/I7 交付了 overview / team-dashboard / champion 三端点，但此前
// user-web 零引用（grep sales-workbench 空）—— 与 A1 同形状的"后端已交付、
// 前端零消费"假性未完成，本模块补前端半。
//
// 口径（对齐 I6/I7 判据，前端不越权兜底）：
// - overview 的 sales_id 是 query 参数必给（工作台 = "某个销售看自己的概览"，
//   缺失后端 400）；不传等于主动吃 400，所以 API 层不做可选默认。
// - team-dashboard / days：非数 / ≤0 / >365 后端 400，days 缺省后端按 30；
//   前端只送 7/30/90 三档（页面下拉给的就这三档）。
// - 装配缺失（app.InitSalesWorkbenchRuntime 未跑）后端回 503 —— 这与
//   "没数据" 是两件事，页面按状态码分别措辞，不在这里吞掉。
// - SQL 口径的 /api/ai-productivity/* 与此并存不替换（I7 决策：两套字段集
//   不同，换源 = 改历史数字）；本模块只碰事件流口径的三条。
const base = '/api/sales-workbench'

export const salesWorkbenchApi = {
  // 个人概览：聚合待办（todos[].url 深链指向 /dashboard/drafts/<id>，落点
  // 是 orderDraft 页）+ 今日/本月 + AI 产能。
  overview(salesId) {
    return http.get(`${base}/overview`, { sales_id: salesId })
  },

  // 团队仪表盘：Top5 排行 + AI 产能 + 销冠画像 + 漏斗一次取齐。
  // journey 未装配时 funnel=null（少一块 ≠ 500），前端要按 null 分支渲染。
  teamDashboard(days) {
    return http.get(`${base}/team-dashboard`, { days })
  },

  // 销冠画像独立读口：榜内前 10% 销售的共性标签 / 推荐 SOP / 洞察。
  // team-dashboard 里也带一份 champion 摘要，这里是同一数据的完整端点，
  // 两条都消费才算三端点全接。
  champion(days) {
    return http.get(`${base}/champion`, { days })
  }
}

export default salesWorkbenchApi
