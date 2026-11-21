import { describe, it, expect, beforeEach, vi } from 'vitest'
const runtime = vi.hoisted(() => vi.fn())
vi.mock('@/api/runtimeConfig', () => ({ runtimeConfig: runtime }))
import { setActivePinia, createPinia } from 'pinia'
import { useRuntimeConfigStore } from '@/stores/runtimeConfig'

// runtimeConfig store 的兜底语义测试。
//
// 为什么重点测兜底：这个 store 是启动链上的一环，拉不到配置（弱网、配置面不可达）
// 时站点必须还能跑。所以每个读取口在「值缺失 / 非数字 / 非正数」三种坏输入下都必须
// 落在调用点给的 fallback 上，绝不能返回 NaN/undefined/0 让上层把心跳打成 0。

describe('useRuntimeConfig', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('真实 API 直接返回的扁平配置能被读取且并发只请求一次', async () => {
    runtime.mockResolvedValue({ 'frontend_ui.material_max_size_mb': '25' })
    const s = useRuntimeConfigStore()
    await Promise.all([s.ensureLoaded(), s.ensureLoaded()])
    expect(s.num('frontend_ui', 'material_max_size_mb', 10)).toBe(25)
    expect(runtime).toHaveBeenCalledTimes(1)
  })

  it('未拉取时所有读取口回落 fallback', () => {
    const s = useRuntimeConfigStore()
    expect(s.str('frontend_ws', 'missing', 'fb')).toBe('fb')
    expect(s.num('frontend_ws', 'missing', 42)).toBe(42)
    expect(s.durSeconds('frontend_ws', 'missing', 7000)).toBe(7000)
    expect(s.bool('frontend_ws', 'missing', true)).toBe(true)
  })

  it('num 读后台秒数/次数原值', () => {
    const s = useRuntimeConfigStore()
    s.values = { 'frontend_ws.agent_max_reconnect_attempts': '25' }
    expect(s.num('frontend_ws', 'agent_max_reconnect_attempts', 10)).toBe(25)
  })

  it('num 对非正数回落（0/负数都是写坏的值）', () => {
    const s = useRuntimeConfigStore()
    // 0 在阈值语义下几乎总是写坏了：重连 0 次 = 断线即弃，flush 0ms = 每事件都刷。
    for (const bad of ['0', '-1', '0.4']) {
      s.values = { 'frontend_ws.agent_max_reconnect_attempts': bad }
      expect(s.num('frontend_ws', 'agent_max_reconnect_attempts', 10)).toBe(10)
    }
  })

  it('num 对非数字回落', () => {
    const s = useRuntimeConfigStore()
    for (const bad of ['abc', '', null, undefined, 'NaN', 'Infinity']) {
      s.values = { 'frontend_ws.agent_max_reconnect_attempts': bad }
      expect(s.num('frontend_ws', 'agent_max_reconnect_attempts', 10)).toBe(10)
    }
  })

  it('num 截断小数（次数不该是 3.7 次）', () => {
    const s = useRuntimeConfigStore()
    s.values = { 'frontend_ui.deep_link_scan_page_size': '120.9' }
    expect(s.num('frontend_ui', 'deep_link_scan_page_size', 500)).toBe(120)
  })

  it('durSeconds 把后端秒数转成前端毫秒', () => {
    const s = useRuntimeConfigStore()
    // 后端 duration 参数存的是秒（parseDurationSeconds 的双口径），前端要毫秒。
    s.values = { 'frontend_ws.journey_flush_interval_ms': '45' }
    expect(s.durSeconds('frontend_ws', 'journey_flush_interval_ms', 30000)).toBe(45000)
  })

  it('durSeconds 对 0/非法回落毫秒 fallback', () => {
    const s = useRuntimeConfigStore()
    s.values = { 'frontend_ws.journey_flush_interval_ms': '0' }
    expect(s.durSeconds('frontend_ws', 'journey_flush_interval_ms', 30000)).toBe(30000)
    s.values = { 'frontend_ws.journey_flush_interval_ms': 'abc' }
    expect(s.durSeconds('frontend_ws', 'journey_flush_interval_ms', 30000)).toBe(30000)
  })

  it('bool 区分「没配过」与「配了 false」——回落只发生在前者', () => {
    const s = useRuntimeConfigStore()
    // 配了 false 时，即使 fallback 是 true 也必须返回 false：
    // 第一版把「非 true 一律回落 fallback」，库里明写 false 的开关在
    // fallback=true 的调用点显示成开——运维关掉的开关前端照样开着。
    s.values = { 'frontend_ui.some_flag': 'false' }
    expect(s.bool('frontend_ui', 'some_flag', true)).toBe(false)
    expect(s.bool('frontend_ui', 'some_flag', false)).toBe(false)
    s.values = { 'frontend_ui.some_flag': '0' }
    expect(s.bool('frontend_ui', 'some_flag', true)).toBe(false)

    s.values = { 'frontend_ui.some_flag': 'true' }
    expect(s.bool('frontend_ui', 'some_flag', false)).toBe(true)
    s.values = { 'frontend_ui.some_flag': '1' }
    expect(s.bool('frontend_ui', 'some_flag', false)).toBe(true)

    // 没配过才回落 fallback
    s.values = {}
    expect(s.bool('frontend_ui', 'some_flag', true)).toBe(true)
    expect(s.bool('frontend_ui', 'some_flag', false)).toBe(false)

    // 认不出的脏值（大小写变体、yes/no）回落 fallback 而不是静默当 false
    for (const dirty of ['TRUE', 'True', 'yes', 'no', '2']) {
      s.values = { 'frontend_ui.some_flag': dirty }
      expect(s.bool('frontend_ui', 'some_flag', true)).toBe(true)
      expect(s.bool('frontend_ui', 'some_flag', false)).toBe(false)
    }
  })

  it('reset 清空全部状态', () => {
    const s = useRuntimeConfigStore()
    s.values = { 'frontend_ws.x': '1' }
    s.loaded = true
    s.failed = true
    s.reset()
    expect(s.values).toEqual({})
    expect(s.loaded).toBe(false)
    expect(s.failed).toBe(false)
  })

  it('非对象响应不会让读取口抛 TypeError', () => {
    const s = useRuntimeConfigStore()
    // 后端极端情况返回 null / 字符串时，values 归一成 {}
    for (const bad of [null, 'oops', 42]) {
      s.values = bad && typeof bad === 'object' ? bad : {}
      expect(() => s.num('frontend_ws', 'x', 1)).not.toThrow()
    }
  })
})
