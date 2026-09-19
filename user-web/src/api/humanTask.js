import { http } from '@/utils/request'

// 统一待办中心 API（对应后端 controller/human_task.go HumanTaskController）。
//
// 这套端点是**待办中心与坐席收件箱共用**的读口（C3 裁定的"分离视图"分离在前端的
// 组织方式，不分离在数据出口）。所以本文件里出现 /api/inbox 才是 bug ——
// 由 tests/unit/api_humanTask.test.js 双向钉住（收件箱那一侧也不许出现这里的前缀）。
//
// id 一律 encodeURIComponent：待办 id 是 text 列（生成格式 ht_<unixnano>_<seq>，
// 但列上没这个约束），一个含 / 的 id 不编码就等于让调用方改写请求路径。
const base = '/api/human-tasks'
const seg = (id) => encodeURIComponent(id)

// toQuery 把筛选对象拼成后端读得到的查询串：**重复键**，不带方括号。
//
// 为什么不用 axios 的默认写法：它对数组产出 ?kind[]=a&kind[]=b（转义后是 %5B%5D，
// 肉眼看 URL 都看不出区别），而 gin 的 QueryArray("kind") 只认 ?kind=a&kind=b。
// 错开的后果不是报错，是**筛选项被整个忽略**——勾选"只看审批类"，
// 拿回来的是全池，而页面上看不出任何异常。这条由
// tests/unit/api_humanTask_query.test.js 在 XHR.open 那一层钉住（mock 掉的用例看不见）。
//
// 空值不进串：空数组、空串、undefined 三种"没填"在查询串里一律不出现，
// 于是 ?kind= 这种"传了个空类型"的形状不会出现在网关日志里让人猜一遍。
function toQuery(params) {
  const q = new URLSearchParams()
  for (const [key, value] of Object.entries(params || {})) {
    if (value === undefined || value === null || value === '') continue
    if (Array.isArray(value)) {
      for (const one of value) {
        if (one !== undefined && one !== null && one !== '') q.append(key, String(one))
      }
      continue
    }
    q.append(key, String(value))
  }
  return q.toString()
}

export const humanTaskApi = {
  // 列表。kind / status 可重复给（?kind=a&kind=b）：坐席收件箱要"会话 + 催收"一类合并视图。
  // assignee 给 'me' 由后端换成登录态身份 —— 前端不拼自己的 id，避免"改了 URL 就能看别人名下"。
  list(params) {
    return http.get(base, params, { paramsSerializer: toQuery })
  },

  // 未读数与每类逾期数。静默：这一句是轮询的，失败时页面继续显示上一次读数即可，
  // 每 30s 弹一句"服务异常"会把待办中心变成噪音源。
  counts() {
    return http.get(`${base}/counts`, undefined, { _silent: true })
  },

  get(id) {
    return http.get(`${base}/${seg(id)}`)
  },

  // 认领仅对会话类开放（审批类只能裁决，后端同一判据回 409）。
  claim(id) {
    return http.post(`${base}/${seg(id)}/claim`)
  },

  // 释放只有当前认领人能使。
  release(id) {
    return http.post(`${base}/${seg(id)}/release`)
  },

  // 完成：会话类与催收类用。审批类的待办**不走这里**（见 views/approvalTask/actions.js）。
  complete(id) {
    return http.post(`${base}/${seg(id)}/complete`)
  },

  // 撤销必须给理由（后端对空理由回 400）。
  cancel(id, reason) {
    return http.post(`${base}/${seg(id)}/cancel`, { reason })
  }
}

export default humanTaskApi
