import { http } from '@/utils/request'

// 运行时配置（后端 config_params 参数中心）的只读出口。
//
// 走这个而不是 `/api/manage/config-params`：后者是管理端全量列表，每条带
// Name/Description/Min/Max/Restart/ReadOnly，把它暴露给终端用户等于把整套运维
// 决策面摊开。这个端点只返回白名单组的 value，且读不到库时返回空 map——前端据此
// 用代码兜底值，配置面挂了这个 SPA 照样打得开。
//
// 不传 groups 时后端返回白名单全部，省得这里 hardcode 一份清单（那份清单会和后端
// 的 runtimePublicGroups 漂移，然后前端悄悄少读一组）。
export function runtimeConfig(groups) {
  return http.get('/api/config/runtime', { params: groups ? { groups } : {} }, { timeout: 5000, _silent: true })
}

// 业务字典（后端 dictionaries 表，阶段四）。
//
// 为什么只有一个入口而不是每个页面自己拼 URL：字典是「存量行的值域」——
// 前后端必须读同一份，否则前端按本地常量翻译、后端按库里值域判定，两边对同一行的
// 结论会不一致。管理端 CRUD 走 `dictionariesApi`（挂在 admin 鉴权下），
// 终端页面读值域走 `dictionary(parent)`（走只读白名单端点）。
export function dictionary(parent) {
  return http.get(`/api/config/dictionaries/${encodeURIComponent(parent)}`)
}

// 管理端字典 CRUD（admin 鉴权）。
export const dictionariesApi = {
  listParents: () => http.get('/api/manage/dictionaries'),
  get: (parent) => http.get(`/api/manage/dictionaries/${encodeURIComponent(parent)}`),
  upsertItem: (parent, item) => http.put(`/api/manage/dictionaries/${encodeURIComponent(parent)}/items`, item),
  replaceTransitions: (parent, transitions) =>
    http.put(`/api/manage/dictionaries/${encodeURIComponent(parent)}/transitions`, { transitions }),
}
