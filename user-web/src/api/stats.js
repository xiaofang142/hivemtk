import { http } from '@/utils/request'

export const StatsApi = {
  // Monitor.vue 期望 SystemInfo 结构（uptime 秒/ server_time / hostname / go_version），
  // 该结构由 /api/system/info 提供；此前误指向 /api/stats/system（返回 system_uptime 字符串、
  // 无 hostname/go_version），导致运行时间显示 NaN。
  getSystemInfo: () => http.get('/api/system/info')
}

