import { http } from '@/utils/request'

// D01 能力运行诊断
//
// 消费 GET /api/capabilities（auth 组，无参数）。响应 data 形如：
//   { capabilities: [{ platform, capability, via, note, implemented, configured, ready, reason_code }],
//     total, ready_count }
//
// 这个接口与 kbConnector.js 里的旧 capabilitiesApi 是同路径：后端 D01 已把它
// 换成运行视图（capabilities/total/ready_count），不再返回 entries/markdown。
export const capabilityDiagnosticsApi = {
  get: () => http.get('/api/capabilities')
}
