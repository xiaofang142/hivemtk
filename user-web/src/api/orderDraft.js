import { http } from '@/utils/request'

// AI 成单草稿竖的前端出口（对应后端 controller/order_draft.go）。
//
// 后端五条销售操作端点挂在 /api/manage/order-drafts 组（需要登录身份当 operator，
// 没有角色细分）；观察端点单独挂在 /api/agent/order-drafts/stats —— 这条读的是
// 本进程的草稿运行时档位（旗子 FF_LTC_ORDER_DRAFT_DB），和业务数据是两件事，
// 路径因此不同前缀，不是笔误。
//
// id 一律 encodeURIComponent：草稿 id 是 string 列（生成格式 draft_<unixnano>_<seq>
// 用不到 30 字符），但列上没有格式约束，含 / 的 id 不编码就等于让调用方改写请求路径。
//
// 与工作台的关系：/api/sales-workbench/overview 的聚合待办会生成 /dashboard/drafts/<id>
// 的深链（sales_workbench.go），这一页（views/orderDraft/）就是那条深链的落点 ——
// 在它存在之前，工作台点草稿待办是 404。
const base = '/api/manage/order-drafts'
const seg = (id) => encodeURIComponent(id)

export const orderDraftApi = {
  // 待确认池列表（缺省视图 = 全部待确认，不分人 —— AI 建的草稿 owner_id 是 "system"，
  // 按人过滤会把整批从每个人列表里抹掉，"看自己那份"是工作台的职责）。
  // owner_id："all"|"*" 与不传等价；customer_id 给了就切到该客户的全历史视图
  // （含终态，响应 pending=false，别当待办计数用）。
  // limit 1..200，非法值后端 400 而不是静默兜底。
  list(params) {
    return http.get(base, params)
  },

  get(id) {
    return http.get(`${base}/${seg(id)}`)
  },

  // 改价/改量/改产品名/加备注。四个字段都是"没给=不改"，空对象后端 400；
  // quantity<1、unit_price<0 也是 400（要废单走 cancel，不是把数量改成 0）。
  // 成功后后端回读整条草稿（含重算的 total_amount），调用方拿返回值直接替换本地行。
  edit(id, updates) {
    return http.patch(`${base}/${seg(id)}`, updates)
  },

  // 一键确认成单。结果含 order_id 与 order_provisional：
  // provisional=true 说明 order_id 是本进程临时号、orders 表里没有这一行，
  // 前端必须把它当告警读，不能当成功订单号展示完就算。
  confirm(id) {
    return http.post(`${base}/${seg(id)}/confirm`)
  },

  // 取消必须给理由（后端 400 判据：理由是取消原因分析的唯一原料，空串整套分析失效）。
  cancel(id, reason) {
    return http.post(`${base}/${seg(id)}/cancel`, { reason })
  },

  // 运行时档位读数。这是打开页面时取一次的配置状态，不轮询 —— counts=null（旗子关着
  // 压根没读）与 counts={}（读了且真没有）必须原样透给页面，页面负责分别措辞。
  stats() {
    return http.get('/api/agent/order-drafts/stats')
  }
}

export default orderDraftApi
