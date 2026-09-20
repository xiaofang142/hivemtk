import { http } from '@/utils/request'

// LTC-25 运营开关（后端 internal/router/ltc_routes.go，落点 system_config_kv 的 ltc.config）。
//
// 拦截器已把 {code,data,message} 拆掉，这里拿到手的就是 data 本身。

// 读回来的不只是"配成什么样"，还包括每个阶段归它管的路由条数（guarded_routes /
// guarded_total）与 reading_hints —— 只有前半句会把"我把开关全打开了"读成
// "现网行为变了"。视图必须把后半句一起显示，别只画开关。
export function getLTCConfig() {
  return http.get('/api/manage/ltc/config')
}

// PUT 是**整份覆盖**：ltc.config 是一个原子文档，没有单项 patch 端点。
// 调用方必须把 GET 到的那一份改完再发回来，只发自己动过的那个字段会被拒
// （后端 DisallowUnknownFields + 四个阈值缺一不可）。
//
// 不静默：后端每条拒绝文案都指向"哪一行写错了"（阶段名拼错、阈值给 0 拆闸…），
// 那条 toast 就是运营要的报错。视图里同时把 err.message 铺在表单上。
export function saveLTCConfig(cfg) {
  return http.put('/api/manage/ltc/config', cfg)
}

export default { getLTCConfig, saveLTCConfig }
