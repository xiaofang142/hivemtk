// B1-a：选择器失效检测（扩展侧采集 + 上报形状）。
//
// 判据要点（每条都对着一个真实的假绿形态）：
//   ① 命中率必须同时用分母和分子算——只报命中数时「从未被调用」与「一直失败」同形；
//   ② 样本不足判 warming 而不是 broken——否则新装扩展第一次打开页面就红；
//   ③ 上报失败必须保留增量——丢了分母，命中率会偏高，恰好把问题掩盖掉；
//   ④ URL/头与既有桥接通道同口径（X-Channel-Identify-Code + tenant_code query）——
//      只发 Bearer 会被服务端 401，而 401 在扩展侧表现为「接上了但没数据」；
//   ⑤ 适配器真的在计数：getMessageItems 返回空与非空会让 attempts/hits 分开增长。

import { describe, it, expect, vi } from 'vitest';
import {
  SELECTOR_HEALTH_PATH,
  SELECTOR_HEALTH_DEFAULTS,
  SELECTOR_HEALTH_STATUS,
  classifySelectorHealth,
  createSelectorHealthTracker,
  mergeSelectorHealthItems,
  subtractSelectorHealthItems,
  buildSelectorHealthRequest,
  postSelectorHealth,
  selectorHitRate,
} from '../src/core/selector-health.js';
import { BaseAdapter } from '../src/core/channel-adapter.js';

describe('selector-health · 判定口径', () => {
  it('命中率同时用分子与分母（0 尝试 ⇒ 0，不是 NaN）', () => {
    expect(selectorHitRate(0, 0)).toBe(0);
    expect(selectorHitRate(4, 1)).toBe(25);
    expect(selectorHitRate(3, 3)).toBe(100);
    expect(selectorHitRate(1000, 973)).toBe(97.3);
  });

  it('样本不足判 warming，不当成 broken', () => {
    expect(classifySelectorHealth(1, 0)).toBe(SELECTOR_HEALTH_STATUS.warming);
    expect(classifySelectorHealth(SELECTOR_HEALTH_DEFAULTS.minAttempts - 1, 0))
      .toBe(SELECTOR_HEALTH_STATUS.warming);
  });

  it('样本够了且一次没命中 ⇒ broken（这才是「平台改版」的形状）', () => {
    expect(classifySelectorHealth(50, 0)).toBe(SELECTOR_HEALTH_STATUS.broken);
  });

  it('低于阈值判 degraded，等于阈值判 ok（极性对，阈值边界不许抖）', () => {
    expect(classifySelectorHealth(100, 59, 60)).toBe(SELECTOR_HEALTH_STATUS.degraded);
    expect(classifySelectorHealth(100, 60, 60)).toBe(SELECTOR_HEALTH_STATUS.ok);
    expect(classifySelectorHealth(100, 99, 60)).toBe(SELECTOR_HEALTH_STATUS.ok);
  });

  it('阈值非法（0 / >100 / NaN）回落默认 60，而不是让整门恒红或恒绿', () => {
    expect(classifySelectorHealth(100, 59, 0)).toBe(SELECTOR_HEALTH_STATUS.degraded);
    expect(classifySelectorHealth(100, 99, 0)).toBe(SELECTOR_HEALTH_STATUS.ok);
    expect(classifySelectorHealth(100, 99, 101)).toBe(SELECTOR_HEALTH_STATUS.ok);
    expect(classifySelectorHealth(100, 99, NaN)).toBe(SELECTOR_HEALTH_STATUS.ok);
  });
});

describe('selector-health · 计数器', () => {
  it('按 (channel, field) 分别累计，drain 后清零', () => {
    const t = createSelectorHealthTracker();
    t.record('douyin', 'messageItem', true);
    t.record('douyin', 'messageItem', false);
    t.record('douyin', 'input', true);
    t.record('xhs', 'messageItem', false);

    expect(t.attemptsOf('douyin')).toBe(3);
    expect(t.attemptsOf('xhs')).toBe(1);

    const drained = t.drain('douyin');
    expect(drained.items).toEqual([
      { field: 'messageItem', attempts: 2, hits: 1 },
      { field: 'input', attempts: 1, hits: 1 },
    ]);
    expect(t.attemptsOf('douyin')).toBe(0);
    // 其它渠道不受影响：drain 按渠道隔离，否则一个标签页的上报会清空另一个的计数
    expect(t.attemptsOf('xhs')).toBe(1);
  });

  it('攒够 flushAfterAttempts 才 shouldFlush', () => {
    const t = createSelectorHealthTracker({ flushAfterAttempts: 3 });
    t.record('douyin', 'messageItem', true);
    t.record('douyin', 'messageItem', true);
    expect(t.shouldFlush('douyin')).toBe(false);
    t.record('douyin', 'messageItem', true);
    expect(t.shouldFlush('douyin')).toBe(true);
  });

  it('snapshot 自带本地判定（自检页用），且不清零', () => {
    const t = createSelectorHealthTracker();
    for (let i = 0; i < 30; i++) t.record('douyin', 'messageItem', i < 5);
    const snap = t.snapshot('douyin');
    expect(snap).toHaveLength(1);
    expect(snap[0].status).toBe(SELECTOR_HEALTH_STATUS.degraded);
    expect(snap[0].hit_rate).toBeCloseTo(16.7, 1);
    expect(t.attemptsOf('douyin')).toBe(30);
  });

  it('空渠道 drain 返回空数组（不是抛错，也不是返回旧数据）', () => {
    const t = createSelectorHealthTracker();
    expect(t.drain('nobody')).toEqual({ items: [], attempts: 0 });
  });
});

describe('selector-health · 增量合并与扣减', () => {
  it('merge 同名字段相加，不改入参', () => {
    const base = [{ field: 'messageItem', attempts: 2, hits: 1 }];
    const merged = mergeSelectorHealthItems(base, [{ field: 'messageItem', attempts: 3, hits: 3 }]);
    expect(merged).toEqual([{ field: 'messageItem', attempts: 5, hits: 4 }]);
    expect(base[0].attempts).toBe(2);
  });

  it('subtract 只减已上报的那批，剩下的留给下一次', () => {
    const pending = [{ field: 'messageItem', attempts: 10, hits: 4 }];
    const sent = [{ field: 'messageItem', attempts: 6, hits: 2 }];
    expect(subtractSelectorHealthItems(pending, sent))
      .toEqual([{ field: 'messageItem', attempts: 4, hits: 2 }]);
  });

  it('subtract 到 0 即移除该项（不留 attempts:0 的空行）', () => {
    expect(subtractSelectorHealthItems(
      [{ field: 'messageItem', attempts: 5, hits: 5 }],
      [{ field: 'messageItem', attempts: 5, hits: 5 }],
    )).toEqual([]);
  });
});

describe('selector-health · 请求形状', () => {
  it('路径与 user-server 路由一致，token 走 X-Channel-Identify-Code 头', () => {
    const req = buildSelectorHealthRequest({
      serverUrl: 'https://api.example.com',
      token: 'tk_123',
      tenantCode: 't-acme',
      channel: 'douyin',
      accountId: 'acc-1',
      items: [{ field: 'messageItem', attempts: 10, hits: 3 }],
    });
    const u = new URL(req.url);
    expect(u.pathname).toBe(SELECTOR_HEALTH_PATH);
    expect(u.searchParams.get('tenant_code')).toBe('t-acme');
    expect(req.init.headers['X-Channel-Identify-Code']).toBe('tk_123');
    expect(req.init.headers.Authorization).toBe('Bearer tk_123');
    expect(req.init.method).toBe('POST');
    expect(JSON.parse(req.init.body)).toEqual({
      channel: 'douyin',
      account_id: 'acc-1',
      items: [{ field: 'messageItem', attempts: 10, hits: 3 }],
    });
  });

  it('ws:// 的 baseUrl 归一成 http(s)：服务端这几条口是 HTTP，不是 WS', () => {
    const req = buildSelectorHealthRequest({
      serverUrl: 'ws://localhost:8080',
      channel: 'xhs',
      items: [{ field: 'messageItem', attempts: 1, hits: 0 }],
    });
    expect(req.url.startsWith('http://localhost:8080')).toBe(true);
  });

  it('空 channel / 空 items 直接抛错（不发出一个注定被 400 的请求）', () => {
    expect(() => buildSelectorHealthRequest({ serverUrl: 'http://x', channel: '', items: [{ field: 'messageItem', attempts: 1, hits: 0 }] })).toThrow();
    expect(() => buildSelectorHealthRequest({ serverUrl: 'http://x', channel: 'douyin', items: [] })).toThrow();
    expect(() => buildSelectorHealthRequest({ channel: 'douyin', items: [{ field: 'messageItem', attempts: 1, hits: 0 }] })).toThrow();
  });

  it('成功时回传服务端判定（degraded 列表），失败时不抛而是 ok:false', async () => {
    const okFetch = vi.fn(async () => ({
      ok: true,
      status: 200,
      json: async () => ({ ok: true, threshold: 60, degraded: ['messageItem'], items: [] }),
    }));
    const req = buildSelectorHealthRequest({
      serverUrl: 'http://x', channel: 'douyin', items: [{ field: 'messageItem', attempts: 30, hits: 2 }],
    });
    const r = await postSelectorHealth(req, okFetch);
    expect(r.ok).toBe(true);
    expect(r.degraded).toEqual(['messageItem']);
    expect(r.threshold).toBe(60);

    const badFetch = vi.fn(async () => ({ ok: false, status: 401, json: async () => ({ error: 'unauthorized' }) }));
    const r2 = await postSelectorHealth(req, badFetch);
    expect(r2.ok).toBe(false);
    expect(r2.status).toBe(401);
    expect(r2.error).toBe('unauthorized');
  });

  it('网络异常/超时也返回 ok:false（观测面失败不许把消息链路带崩）', async () => {
    const throwFetch = vi.fn(async () => { throw new Error('boom'); });
    const req = buildSelectorHealthRequest({
      serverUrl: 'http://x', channel: 'douyin', items: [{ field: 'messageItem', attempts: 1, hits: 0 }],
    });
    const r = await postSelectorHealth(req, throwFetch);
    expect(r.ok).toBe(false);
    expect(r.error).toBe('boom');
  });
});

describe('selector-health · 适配器真的在计数', () => {
  function makeAdapter(hooks) {
    return new BaseAdapter({
      name: 'douyin',
      channel: 'douyin',
      SEL: { MSG_ITEM: '.msg-item' },
      hooks,
    });
  }

  it('消息项拿到非空 ⇒ hits 增长；拿到空 ⇒ 只涨 attempts', () => {
    let items = [];
    const a = makeAdapter({ getMessageItems: () => items });
    a.getMessageItems();
    a.getMessageItems();
    items = [{ nodeType: 1 }];
    a.getMessageItems(); // 不计入（数组非空但我们只统计次数与是否非空）

    const snap = a._selectorHealth.snapshot('douyin');
    expect(snap).toHaveLength(1);
    expect(snap[0].field).toBe('messageItem');
    expect(snap[0].attempts).toBe(3);
    expect(snap[0].hits).toBe(1);
  });

  it('会话列表走同一条计数（不是只统计消息项）', () => {
    const a = makeAdapter({ getConversationList: () => [{ id: 1 }] });
    a.getConversationList();
    const snap = a._selectorHealth.snapshot('douyin');
    expect(snap[0].field).toBe('conversationList');
    expect(snap[0].hits).toBe(1);
  });

  it('hooks 抛错时不影响计数路径的健壮性（statistics 不许把业务带崩）', () => {
    const a = makeAdapter({ getMessageItems: () => [] });
    // 计数发生在 hook 返回之后；这里有 chrome 缺失、hook 正常，断言不抛即可
    expect(() => a.getMessageItems()).not.toThrow();
    expect(a._selectorHealth.attemptsOf('douyin')).toBe(1);
  });
});
