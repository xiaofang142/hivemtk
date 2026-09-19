import { http } from '@/utils/request'

// 异步审批裁决 API（对应后端 controller/approval.go ApprovalController）。
//
// 这里**没有 list()**，不是漏了：待办的列表口径归 /api/human-tasks（唯一的"等人办"清单
// 事实源），再加一个审批列表就是第二个事实源 —— 两边未读数迟早有一天各说各话。
// 已落定的历史审批只能按 id 读，那是有意的。
//
// 裁决端点在后端挂 ManagerOrAdminMiddleware()：坐席视角能读到详情（GET 不设角色门，
// 他要知道自己为什么被驳回），但只有管理岗点得动按钮。前端不做角色判断来替代它 ——
// 403 就是 403，把按钮按角色藏起来只是让"为什么点不动"变成第二个问题。
const base = '/api/approvals'
const seg = (id) => encodeURIComponent(id)

export const approvalApi = {
  // 详情带 allowed_transitions（这一行现在还能变成什么），前端照它渲染而不是自己写死跃迁表。
  get(id) {
    return http.get(`${base}/${seg(id)}`)
  },

  // verdict 只收 'approved' | 'rejected'（与后端 ApprovalVerdict 同一串字面量；
  // 'expired' 不是裁决，是清扫器按 TTL 落的状态，人不能替它点）。
  //
  // 静默：409"这条已经被别人裁决过了"在这个界面上是常态（两个人同时开着详情抽屉），
  // 处理方式是刷新并显示当前那一行，而不是一句和后端文案撞车的通用报错。
  decide(id, verdict, note) {
    return http.post(`${base}/${seg(id)}/decide`, { verdict, note }, { _silent: true })
  }
}

export default approvalApi
