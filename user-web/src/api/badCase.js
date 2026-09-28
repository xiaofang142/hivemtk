import { http } from '@/utils/request'

// G-2 Bad Case 闭环 API（对应后端 controller/bad_case.go BadCaseController）。
//
// 这一竖只有八个出口，且**没有一个是"顺手写的"**：列表/读数/值域表是读口，
// 补录/打标/撤销/导出是写口，详情留给抽屉。导出走 POST 而不是 GET ——
// 它会把读到的行从 labeled 改成 exported，一次点击两次状态迁移，
// 挂在 GET 上就等于允许浏览器的预取与重试替人签字。
//
// 与待办中心（api/humanTask.js）的关系：两边都读会话，但这是两件事 ——
// 那一竖问"有件事在等人办"，这一竖问"这次答得不好的原因是谁的责任层"。
// 谁也不转调谁，隔离由 tests/unit/api_badCase.test.js 双向钉住。
//
// id 一律 encodeURIComponent：坏例 id 是 text 列（生成格式 bc_<unixnano>_<seq>，
// 列上没这个约束），含 / 或 ? 的 id 不编码就等于让调用方改写请求路径。
const base = '/api/bad-cases'
const seg = (id) => encodeURIComponent(id)

// toQuery 把筛选对象拼成后端读得到的查询串：**重复键**，不带方括号。
//
// 后端读的是 gin 的 QueryArray("label")，它只认 ?label=a&label=b；
// axios 对数组默认产出 ?label[]=a（转义后 %5B%5D），后果不是报错而是**筛选项被整个忽略**——
// 勾选"只看知识库缺词"，拿回来的是全队列，而页面上看不出任何异常。
// 与 api/humanTask.js 同一写法，由 tests/unit/api_badCase_query.test.js 在 XHR.open 那层钉住。
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

export const badCaseApi = {
  // 列表。source / status / label 可重复给；fix_layer 是单值（后端就一个 Where）。
  // 不给 status 时后端只列 pending（待判队列），要看不漏掉 dismissed 得显式给。
  list(params) {
    return http.get(base, params, { paramsSerializer: toQuery })
  },

  // 队列读数（含本卡北极星 labeled_ratio）。静默：这是轮询的，
  // 每 30s 弹一句"服务异常"会把质量页变成噪音源，失败时留着上一次读数即可。
  stats() {
    return http.get(`${base}/stats`, undefined, { _silent: true })
  },

  // 值域表（来源/状态/类目/责任层 + 类目→层）。静默：页面打开时取一次，
  // 取不到就退回"直接显示后端回英文枚举值"，那比弹一句错更好读。
  // 这条是八条出口里唯一**不查装配状态**的（内容全来自 model 常量），所以它不会回 503。
  taxonomy() {
    return http.get(`${base}/taxonomy`, undefined, { _silent: true })
  },

  get(id) {
    return http.get(`${base}/${seg(id)}`)
  },

  // 坐席补录一条（不等置信度判它低质）。后端对 session_id 为空、以及 query_text/answer_text
  // 缺任一条都回 400 —— 补录的那条必须能独立回答"当时问的是什么、答坏了的是什么"。
  // message_id 可缺：手动行的幂等键用的就是它自己的新 id，所以同一会话允许补多条
  // （自动来源没这个自由度，那边缺 message_id 会让两条真坏例互吞）。
  create(input) {
    return http.post(base, input)
  },

  // 打标：label 必须在值域内，note（判定依据）必须非空 —— 两条都是后端 400。
  // note 一定显式给字符串：后端的字段是 *string，"没给"与"给了空串"在服务层同一处理，
  // 前端少传一个字段并不会绕过那道闸，只会让报错文案变成"请求体不是合法 JSON"之外的一句。
  label(id, label, note) {
    return http.post(`${base}/${seg(id)}/label`, { label, note })
  },

  // 撤销必须给理由（同一判据后端回 400）。理由是"这条其实不算坏例"的唯一出处，
  // 没有它，下次同一现场被自动标记时没人知道曾经判过。
  dismiss(id, reason) {
    return http.post(`${base}/${seg(id)}/dismiss`, { reason })
  },

  // 导出评测集。limit 给 0/缺省 = 后端兜默认档；负数不回这里兜，直接 400。
  // 409（没有可导出的行）要由视图自己换文案：那句"队列里没有已打标且未导出的样本"
  // 是业务结论，不是失败，静默才能避免网关再弹一句同义的"服务异常"。
  exportSet(labels, limit) {
    return http.post(`${base}/export`, { labels, limit }, { _silent: true })
  }
}

export default badCaseApi
