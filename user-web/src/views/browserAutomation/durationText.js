// 时长读数：同一个 duration_ms 列在会话头、步表、命令流三处必须读起来是同一个量纲。
//
// 立项依据（走查实测）：会话头与详情页步表按 `(ms/1000).toFixed(1)}s` 渲染，
// 监控页的步表与命令流按 `${ms}ms` 渲染 ⇒ 同一列同名不同单位：详情页写「9.6s」的这一步，
// 在监控页写「9573ms」。用户读不出这是同一个数还是两个数，也没法把两页对齐着看。
//
// 顺带修掉两处「0 与没数据长得不一样/一样」的形态：
// · 旧写法 `row.duration_ms ? `${ms}ms` : '—'` 把**真的 0 毫秒**短路成破折号（读起来像没采集到），
//   而未采集（null/undefined）也是破折号 ⇒ 两种事实共用一个读数。
// · 会话侧 duration_ms 只在收口那条 UPDATE 里写（执行期恒为 0），旧写法给正在跑的会话
//   显示「0.0s」，那是「瞬时完成」的意思，而它还没结束 ⇒ 未收口要单独说。
const TERMINAL_SESSION_STATUSES = ['completed', 'failed', 'stopped']

// durationText 三档：<1s 用毫秒、<1min 用秒、更长按「分+秒」。
// 取三档而不是一律秒：监控页一屏几十条步，「0ms/46.6s/5分02秒」能扫读，
// 「302000ms」「0.0s」都不能。
export function durationText(ms) {
  if (ms === null || ms === undefined || ms === '') return '—'
  const v = Number(ms)
  if (!Number.isFinite(v)) return '—'
  if (v <= 0) return '0ms'
  if (v < 1000) return `${Math.round(v)}ms`
  if (v < 60000) return `${(v / 1000).toFixed(1)}s`
  const totalSec = Math.round(v / 1000)
  const min = Math.floor(totalSec / 60)
  const sec = totalSec % 60
  return `${min}分${String(sec).padStart(2, '0')}秒`
}

// sessionDurationText 会话级时长：只有终态才有值，未收口不许显示成 0.0s。
export function sessionDurationText(session) {
  if (!session) return '—'
  if (!TERMINAL_SESSION_STATUSES.includes(session.status)) return '未收口'
  return durationText(session.duration_ms)
}
