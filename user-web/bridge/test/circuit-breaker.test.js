import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { CircuitBreaker, circuitBreaker } from '../src/core/circuit-breaker.js';

// 固定非零基准 t0（AV-08 可控时钟）：所有窗口判定随显式推进走，
// 不再依赖真实 CPU/日志耗时（真实墙上时间流逝不影响断言结果）。
const T0 = 1_700_000_000_000;

// 每个用例结束后恢复真实时钟（等价 finally，保证不泄漏到其他测试文件）
afterEach(() => {
  vi.useRealTimers();
});

describe('CircuitBreaker 三态机（2026-08-14 P0-B）', () => {
  let cb;
  beforeEach(() => {
    // 仅 mock Date 的最小可控时钟：setTimeout 等真实定时器不受影响
    vi.useFakeTimers({ now: T0, toFake: ['Date'] });
    cb = new CircuitBreaker({ failureThreshold: 3, openDurationMs: 100, deadManSeconds: 1 });
  });

  it('初始状态：CLOSED、failureCount=0', () => {
    expect(cb.state).toBe('CLOSED');
    expect(cb.failureCount).toBe(0);
    expect(cb.isOpen()).toBe(false);
  });

  it('CLOSED：第 2 败仍 CLOSED，连续 3 次失败后跳到 OPEN（t0 开启瞬间）', () => {
    cb.recordFailure(new Error('e1'));
    cb.recordFailure(new Error('e2'));
    expect(cb.state).toBe('CLOSED');
    expect(cb.isOpen()).toBe(false);
    cb.recordFailure(new Error('e3'));
    expect(cb.state).toBe('OPEN');
    expect(cb.openedAt).toBe(T0); // 开启瞬间由受控时钟决定，与执行速度无关
    expect(cb.isOpen()).toBe(true);
  });

  it('OPEN：isOpen() 阻断请求（t0+99 仍在窗口内）', () => {
    cb.recordFailure(new Error('e1'));
    cb.recordFailure(new Error('e2'));
    cb.recordFailure(new Error('e3'));
    expect(cb.isOpen()).toBe(true);
    vi.setSystemTime(T0 + 99); // 截止前 1ms
    expect(cb.state).toBe('OPEN');
    expect(cb.isOpen()).toBe(true);
  });

  it('OPEN：t0+100 调 isOpen() 才转 HALF_OPEN（时间到不自动转态）', () => {
    cb.recordFailure(new Error('e1'));
    cb.recordFailure(new Error('e2'));
    cb.recordFailure(new Error('e3'));
    vi.setSystemTime(T0 + 100); // 恰好到达 openDurationMs
    expect(cb.state).toBe('OPEN'); // 未调 isOpen() 前不自动转态
    expect(cb.isOpen()).toBe(false); // 调用瞬间才判定并转换
    expect(cb.state).toBe('HALF_OPEN');
  });

  it('HALF_OPEN：探测成功 → CLOSED，重置 failureCount', () => {
    cb.recordFailure(new Error('e1'));
    cb.recordFailure(new Error('e2'));
    cb.recordFailure(new Error('e3'));
    vi.setSystemTime(T0 + 100);
    cb.isOpen(); // → HALF_OPEN
    expect(cb.state).toBe('HALF_OPEN');
    expect(cb.failureCount).toBe(3);
    cb.recordSuccess(); // halfOpenSuccessThreshold 默认 1：一次探测成功即关闭
    expect(cb.state).toBe('CLOSED');
    expect(cb.failureCount).toBe(0);
    expect(cb.halfOpenSuccessCount).toBe(0);
  });

  it('HALF_OPEN：探测失败 → 重新 OPEN（t1 = 当前受控时刻）', () => {
    cb.recordFailure(new Error('e1'));
    cb.recordFailure(new Error('e2'));
    cb.recordFailure(new Error('e3'));
    vi.setSystemTime(T0 + 100);
    cb.isOpen(); // → HALF_OPEN
    cb.recordFailure(new Error('探测失败'));
    expect(cb.state).toBe('OPEN');
    expect(cb.openedAt).toBe(T0 + 100);
    expect(cb.isOpen()).toBe(true);
  });

  it('CLOSED：成功重置 failureCount', () => {
    cb.recordFailure(new Error('e1'));
    cb.recordFailure(new Error('e2'));
    expect(cb.failureCount).toBe(2);
    cb.recordSuccess();
    expect(cb.failureCount).toBe(0);
    expect(cb.state).toBe('CLOSED');
  });

  it('snapshot 返回死开关信号', () => {
    const s = cb.snapshot();
    expect(s).toMatchObject({
      state: 'CLOSED',
      failureCount: 0,
      lastSuccessAt: 0,
      lastFailureAt: 0,
      recentReasons: [],
      deadManSeconds: 1,
    });
    expect(s.healthy).toBe(true);
  });

  it('snapshot.healthy：无任何调用时 healthy=true（无失败即健康）', () => {
    const s = cb.snapshot();
    expect(s.healthy).toBe(true);
  });

  it('snapshot.healthy：成功后 5 秒内 healthy=true（默认 deadManSeconds=5）', () => {
    const c = new CircuitBreaker({ deadManSeconds: 5 });
    c.recordSuccess();
    const s = c.snapshot();
    expect(s.healthy).toBe(true);
  });

  it('snapshot.recentReasons：去重后最多保留 5 种', () => {
    for (let i = 0; i < 10; i++) {
      cb.recordFailure(new Error(`reason-${i % 7}`));
    }
    const s = cb.snapshot();
    expect(s.recentReasons.length).toBeLessThanOrEqual(5);
  });

  it('reset() 回到初始状态', () => {
    cb.failureCount = 3;
    cb.state = 'OPEN';
    cb.openedAt = Date.now();
    cb.reset();
    expect(cb.state).toBe('CLOSED');
    expect(cb.failureCount).toBe(0);
    expect(cb.openedAt).toBe(0);
  });

  it('全局单例 circuitBreaker 存在且类型正确', () => {
    expect(circuitBreaker).toBeInstanceOf(CircuitBreaker);
    expect(typeof circuitBreaker.isOpen).toBe('function');
    expect(typeof circuitBreaker.recordSuccess).toBe('function');
    expect(typeof circuitBreaker.recordFailure).toBe('function');
  });
});

describe('CircuitBreaker 100ms 窗口时间表（AV-08 可控时钟）', () => {
  let cb;
  beforeEach(() => {
    vi.useFakeTimers({ now: T0, toFake: ['Date'] });
    cb = new CircuitBreaker({ failureThreshold: 3, openDurationMs: 100, deadManSeconds: 1 });
  });

  it('时间表：第 2 败 CLOSED → 第 3 败 t0 OPEN → t0+99 阻断 → t0+100 调 isOpen() 才 HALF_OPEN', () => {
    cb.recordFailure(new Error('e1'));
    cb.recordFailure(new Error('e2'));
    expect(cb.state).toBe('CLOSED');
    expect(cb.isOpen()).toBe(false);

    cb.recordFailure(new Error('e3')); // 第 3 败：t0 瞬间 OPEN
    expect(cb.state).toBe('OPEN');
    expect(cb.openedAt).toBe(T0);

    vi.setSystemTime(T0 + 99); // 精确覆盖 OPEN 截止前
    expect(cb.state).toBe('OPEN');
    expect(cb.isOpen()).toBe(true);

    vi.setSystemTime(T0 + 100); // 截止点
    expect(cb.state).toBe('OPEN'); // 时间到不自动转态
    expect(cb.isOpen()).toBe(false); // 调用瞬间才转 HALF_OPEN
    expect(cb.state).toBe('HALF_OPEN');
  });

  it('探测成功达到配置阈值才 CLOSED，且失败计数复位（halfOpenSuccessThreshold=2）', () => {
    const probe = new CircuitBreaker({
      failureThreshold: 3,
      openDurationMs: 100,
      halfOpenSuccessThreshold: 2,
      deadManSeconds: 1,
    });
    probe.recordFailure(new Error('e1'));
    probe.recordFailure(new Error('e2'));
    probe.recordFailure(new Error('e3'));
    vi.setSystemTime(T0 + 100);
    expect(probe.isOpen()).toBe(false); // → HALF_OPEN
    probe.recordSuccess();
    expect(probe.state).toBe('HALF_OPEN'); // 1 次成功未达阈值，仍探测中
    probe.recordSuccess();
    expect(probe.state).toBe('CLOSED'); // 达到配置阈值才关闭
    expect(probe.failureCount).toBe(0); // 失败计数复位
    expect(probe.halfOpenSuccessCount).toBe(0);
    // 关闭后从 0 重新计数：2 次新失败不足以再次开启
    probe.recordFailure(new Error('f1'));
    probe.recordFailure(new Error('f2'));
    expect(probe.state).toBe('CLOSED');
    expect(probe.isOpen()).toBe(false);
  });

  it('探测失败在 t1 重开：t1+99 仍阻断，t1+100 调 isOpen() 才 HALF_OPEN（重验新窗口）', () => {
    const T1 = T0 + 100;
    cb.recordFailure(new Error('e1'));
    cb.recordFailure(new Error('e2'));
    cb.recordFailure(new Error('e3'));
    vi.setSystemTime(T1);
    expect(cb.isOpen()).toBe(false); // → HALF_OPEN
    expect(cb.state).toBe('HALF_OPEN');

    cb.recordFailure(new Error('探测失败')); // t1 瞬间重开
    expect(cb.state).toBe('OPEN');
    expect(cb.openedAt).toBe(T1);

    vi.setSystemTime(T1 + 99); // 新窗口截止前
    expect(cb.state).toBe('OPEN');
    expect(cb.isOpen()).toBe(true);

    vi.setSystemTime(T1 + 100); // 新窗口截止点
    expect(cb.state).toBe('OPEN'); // 仍不自动转态
    expect(cb.isOpen()).toBe(false);
    expect(cb.state).toBe('HALF_OPEN');
  });

  it('开启瞬间判定不依赖真实 CPU/日志耗时：真实墙上时间流逝 120ms（> openDurationMs）后窗口判定不变', async () => {
    // toFake 仅含 Date：下面的 setTimeout 是真实定时器，模拟慢日志/慢负载占用 >100ms 墙上时间。
    // 该 sleep 不是为了等状态变化，而是证明断言结果与真实耗时无关（旧实现此处会翻转 HALF_OPEN 导致挂）。
    cb.recordFailure(new Error('e1'));
    cb.recordFailure(new Error('e2'));
    cb.recordFailure(new Error('e3'));
    expect(cb.state).toBe('OPEN');
    await new Promise((r) => setTimeout(r, 120));
    expect(Date.now()).toBe(T0); // 受控时钟纹丝不动
    expect(cb.openedAt).toBe(T0);
    expect(cb.isOpen()).toBe(true);
    // 窗口只随受控时钟推进
    vi.setSystemTime(T0 + 100);
    expect(cb.isOpen()).toBe(false);
    expect(cb.state).toBe('HALF_OPEN');
  });
});
