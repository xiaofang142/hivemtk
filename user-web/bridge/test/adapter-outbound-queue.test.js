// 出站互斥队列（2026-11-14 二次论证修复）——sendOutbound 串行化回归钉。
//
// 病灶：批25 的 _outboundInFlight 是计数器，sendOutbound 入口不检查就 begin——
// SSE 推送与轮询（或 SSE 事件连发）的两条 sendOutbound 可并发进入临界区：
// A 拟人键入到一半（30 字≈2.7s），B 并发切入另一个会话，A 的后半段打进 B 的
// 输入框，assertConv 弃用时留下半截文字、两段键入交错 ⇒ 串台/多段/不全
// （生产实证 2026-11-14 用户截图，抖音/小红书/微博均有）。
//
// 本文件契约：
//   ① 并发发起的两条 sendOutbound 严格串行：第二条的临界区在第一条 endOutbound
//     （inFlight 归零）之后才开始——键入永不互相打断；
//   ② 队列错误不传染：第一条抛错不影响第二条正常执行；
//   ③ beginOutbound 重入出声（队列化后不应再出现并发 begin）。
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { BaseAdapter } from '../src/core/channel-adapter.js';
import { CHANNELS } from '../src/core/types.js';

beforeEach(() => {
  localStorage.clear();
  document.body.innerHTML = '';
});

function makeConvAdapter() {
  let current = 'a1';
  const hooks = {
    match: () => true,
    getAccountId: () => 'acc-q',
    getConversationId: () => current,
    getMessageRoot: () => null,
    getMessageItems: () => [],
    parseMessageItem: () => null,
    getConversationList: () => [],
  };
  return new BaseAdapter({ name: 'queue', channel: CHANNELS.DOUYIN, SEL: {}, hooks });
}

describe('出站互斥队列（sendOutbound 串行化）', () => {
  it('并发两条 sendOutbound 严格串行：B 的临界区在 A endOutbound 之后才开始', async () => {
    const adapter = makeConvAdapter();
    const events = [];
    let releaseA;
    const gateA = new Promise((r) => (releaseA = r));

    adapter._sendOutboundLocked = async (text) => {
      events.push(`start:${text}`);
      if (text === 'A') await gateA; // 人为拉长第一条临界区
      events.push(`end:${text}`);
      return { ok: true };
    };

    const p1 = adapter.sendOutbound('A', 'a1');
    const p2 = adapter.sendOutbound('B', 'a1'); // 与 A 并发发起（模拟 SSE+轮询双路）
    await new Promise((r) => setTimeout(r, 30));
    // A 仍持锁时：inFlight=1（B 在排队，未进入临界区）
    expect(adapter._outboundInFlight).toBe(1);

    releaseA();
    await Promise.all([p1, p2]);

    expect(events.indexOf('start:A')).toBeLessThan(events.indexOf('end:A'));
    expect(events.indexOf('end:A')).toBeLessThan(events.indexOf('start:B'));
    expect(adapter._outboundInFlight).toBe(0);
  });

  it('队列错误不传染：第一条抛错，第二条照常执行', async () => {
    const adapter = makeConvAdapter();
    adapter._sendOutboundLocked = async (text) => {
      if (text === 'A') throw new Error('boom');
      return { ok: true };
    };
    const p1 = adapter.sendOutbound('A', 'a1').catch((e) => ({ failed: true, e }));
    const p2 = adapter.sendOutbound('B', 'a1');
    const [r1, r2] = await Promise.all([p1, p2]);
    expect(r1.failed).toBe(true);
    expect(r2.ok).toBe(true);
    expect(adapter._outboundInFlight).toBe(0);
  });

  it('beginOutbound 重入出声（队列化后并发 begin 不应再发生）', async () => {
    const adapter = makeConvAdapter();
    const warnSpy = vi.spyOn(adapter.log, 'warn').mockImplementation(() => {});
    adapter.beginOutbound();
    adapter.beginOutbound(); // 重入
    expect(warnSpy).toHaveBeenCalledTimes(1);
    adapter.endOutbound();
    adapter.endOutbound();
    warnSpy.mockRestore();
  });
});
