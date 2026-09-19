import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// 内存版 chrome.storage.local
function makeChromeStorage() {
  const data = {};
  return {
    storage: {
      local: {
        get: vi.fn((keys) => {
          const out = {};
          const arr = Array.isArray(keys) ? keys : [keys];
          for (const k of arr) if (k in data) out[k] = data[k];
          return Promise.resolve(out);
        }),
        set: vi.fn((obj) => { Object.assign(data, obj); return Promise.resolve(); }),
      },
    },
    _data: data,
  };
}

vi.mock('../src/core/http-ingest.js', () => ({
  getOutbox: vi.fn(),
  ackOutbox: vi.fn(),
}));
vi.mock('../src/core/sanitize.js', () => ({
  sanitizeForDisplay: (t) => t,
}));

import { pollDownlink, initDownlink } from '../src/core/downlink.js';
import { getOutbox, ackOutbox } from '../src/core/http-ingest.js';

// 每个用例用独立 channel，避免模块级 SentCache 单例跨用例污染
function cfg() { return async () => ({ serverUrl: 'http://localhost:8204', token: 't' }); }

describe('downlink / SentCache 去重 + 持久化', () => {
  let chrome;
  beforeEach(() => { chrome = makeChromeStorage(); globalThis.chrome = chrome; vi.clearAllMocks(); });
  afterEach(() => { delete globalThis.chrome; });

  it('add 后 has 为 true；持久化到 storage', async () => {
    await initDownlink(['douyin']);
    const adapter = { sendOutbound: vi.fn(async () => ({ ok: true })) };
    getOutbox.mockResolvedValue({ status: 'ok', messages: [{ msg_id: 'm-d1', content: 'hi', conversation_id: 'c1' }] });
    ackOutbox.mockResolvedValue({ status: 'ok' });
    await pollDownlink('douyin', 'acc1', cfg(), { sendOutbound: adapter.sendOutbound });
    const stored = await chrome.storage.local.get(['bridge_sent_douyin']);
    expect(stored['bridge_sent_douyin']).toContain('m-d1|c1');
    expect(adapter.sendOutbound).toHaveBeenCalledTimes(1);
    expect(ackOutbox).toHaveBeenCalledWith(expect.any(Object), ['m-d1'], expect.any(Object));
  });
});

describe('downlink / 本地已发缓存拦截重复下发', () => {
  let chrome;
  beforeEach(() => { chrome = makeChromeStorage(); globalThis.chrome = chrome; vi.clearAllMocks(); });
  afterEach(() => { delete globalThis.chrome; });

  it('同 msg_id 不重复下发（本地缓存拦截）+ 补确认让服务端了结该欠投递行', async () => {
    await initDownlink(['tiktok']);
    const adapter = { sendOutbound: vi.fn(async () => ({ ok: true })) };
    getOutbox.mockResolvedValue({ status: 'ok', messages: [{ msg_id: 'm-t1', content: 'hi', conversation_id: 'c1' }] });
    ackOutbox.mockResolvedValue({ status: 'ok' });
    const c = cfg();
    await pollDownlink('tiktok', 'acc1', c, { sendOutbound: adapter.sendOutbound }); 
    await pollDownlink('tiktok', 'acc1', c, { sendOutbound: adapter.sendOutbound }); 
    expect(adapter.sendOutbound).toHaveBeenCalledTimes(1);
    // 批11 改口径：第 2 次不再"静默跳过"。服务端仍把这行列进待办 = 它还欠一次确认；
    // 不补确认则该行永远留在待办集合里（每轮都被拉回来空转）。补的这次走 v2 items，幂等。
    expect(ackOutbox).toHaveBeenCalledTimes(2);
    expect(ackOutbox.mock.calls[1][1]).toEqual(['m-t1']);
    expect(ackOutbox.mock.calls[1][2].items).toEqual([{ msg_id: 'm-t1', conversation_id: 'c1' }]);
    expect(ackOutbox.mock.calls[1][2].label).toContain('补确认');
  });
});

describe('downlink / 发送失败重试', () => {
  let chrome;
  beforeEach(() => { chrome = makeChromeStorage(); globalThis.chrome = chrome; vi.clearAllMocks(); });
  afterEach(() => { delete globalThis.chrome; });

  it('sendOutbound 失败：不缓存、不 ack，下轮重试成功', async () => {
    await initDownlink(['xianyu']);
    const adapter = { sendOutbound: vi.fn(async () => ({ ok: false })) };
    getOutbox.mockResolvedValue({ status: 'ok', messages: [{ msg_id: 'm-x1', content: 'hi', conversation_id: 'c1' }] });
    ackOutbox.mockResolvedValue({ status: 'ok' });
    const c = cfg();
    await pollDownlink('xianyu', 'acc1', c, { sendOutbound: adapter.sendOutbound }); 
    expect(ackOutbox).not.toHaveBeenCalled();
    adapter.sendOutbound.mockResolvedValue({ ok: true }); 
    await pollDownlink('xianyu', 'acc1', c, { sendOutbound: adapter.sendOutbound });
    expect(adapter.sendOutbound).toHaveBeenCalledTimes(2);
    expect(ackOutbox).toHaveBeenCalledTimes(1);
  });
});

describe('downlink / ack 失败不丢消息（P0-9 先缓存后 ack 重试）', () => {
  let chrome;
  beforeEach(() => { chrome = makeChromeStorage(); globalThis.chrome = chrome; vi.clearAllMocks(); });
  afterEach(() => { delete globalThis.chrome; });

  it('转发成功但 ack 失败：写本地缓存（用户已收到），入 pendingAck 下轮重试 ack', async () => {
    await initDownlink(['douyin']);
    const adapter = { sendOutbound: vi.fn(async () => ({ ok: true })) };
    getOutbox.mockResolvedValue({ status: 'ok', messages: [{ msg_id: 'm-ackfail', content: 'hi', conversation_id: 'c1' }] });
    ackOutbox.mockResolvedValue({ status: 'error' });
    const c = cfg();
    await pollDownlink('douyin', 'acc1', c, { sendOutbound: adapter.sendOutbound });
    // P0-9：sendOutbound 成功 = 用户已收到 → 写 cache 防止下轮重发
    let stored = await chrome.storage.local.get(['bridge_sent_douyin']);
    expect(stored['bridge_sent_douyin'] || []).toContain('m-ackfail|c1');
    expect(adapter.sendOutbound).toHaveBeenCalledTimes(1);

    // 下轮：cache 命中 → 不重发用户，但两条独立机制各补一次确认
    ackOutbox.mockResolvedValue({ status: 'ok' });
    await pollDownlink('douyin', 'acc1', c, { sendOutbound: adapter.sendOutbound });
    // sendOutbound 不被再次调用（cache 拦截）
    expect(adapter.sendOutbound).toHaveBeenCalledTimes(1);
    // 3 次 = 首次失败 + _pendingAck 队列重试 + 批11 欠投递行补确认。
    // 两者不合并：pendingAck 只活在内存里（页面/SW 重启即丢），补确认是重启后唯一的了结路径。
    // 服务端 ack 幂等（第二次回 duplicate），多一次请求换"重启不悬挂"。
    expect(ackOutbox).toHaveBeenCalledTimes(3);
  });
});

describe('downlink / 空内容不下发', () => {
  let chrome;
  beforeEach(() => { chrome = makeChromeStorage(); globalThis.chrome = chrome; vi.clearAllMocks(); });
  afterEach(() => { delete globalThis.chrome; });

  it('空内容不下发、不 ack', async () => {
    await initDownlink(['xiaohongshu']);
    const adapter = { sendOutbound: vi.fn(async () => ({ ok: true })) };
    getOutbox.mockResolvedValue({ status: 'ok', messages: [{ msg_id: 'm-xh1', content: '', conversation_id: 'c1' }] });
    ackOutbox.mockResolvedValue({ status: 'ok' });
    await pollDownlink('xiaohongshu', 'acc1', cfg(), { sendOutbound: adapter.sendOutbound });
    expect(adapter.sendOutbound).not.toHaveBeenCalled();
    expect(ackOutbox).not.toHaveBeenCalled();
  });
});

describe('downlink / 跨会话同 msg_id 不误去重（P0 回归）', () => {
  let chrome;
  beforeEach(() => { chrome = makeChromeStorage(); globalThis.chrome = chrome; vi.clearAllMocks(); });
  afterEach(() => { delete globalThis.chrome; });

  it('同 msg_id 不同 conversation_id：两条都下发、都 ack', async () => {
    await initDownlink(['xiaohongshu']);
    const adapter = { sendOutbound: vi.fn(async () => ({ ok: true, rateLimited: false, notFound: false })) };
    getOutbox.mockResolvedValue({
      status: 'ok',
      messages: [
        { msg_id: 'mh:samehash', content: '你好', conversation_id: 'convA' },
        { msg_id: 'mh:samehash', content: '你好', conversation_id: 'convB' },
      ],
    });
    ackOutbox.mockResolvedValue({ status: 'ok' });
    await pollDownlink('xiaohongshu', 'acc1', cfg(), { sendOutbound: adapter.sendOutbound });

    expect(adapter.sendOutbound).toHaveBeenCalledTimes(2);
    expect(adapter.sendOutbound).toHaveBeenCalledWith('你好', 'convA', expect.any(Object));
    expect(adapter.sendOutbound).toHaveBeenCalledWith('你好', 'convB', expect.any(Object));

    expect(ackOutbox).toHaveBeenCalledTimes(2);

    // 缓存写入两个复合键，不含裸 msg_id
    const stored = await chrome.storage.local.get(['bridge_sent_xiaohongshu']);
    const arr = stored['bridge_sent_xiaohongshu'] || [];
    expect(arr).toContain('mh:samehash|convA');
    expect(arr).toContain('mh:samehash|convB');
    expect(arr).not.toContain('mh:samehash');
  });

  it('跨轮次：第一轮 convA 已 ack，第二轮 convB 仍能下发（不被 convA 缓存误拦截）', async () => {
    await initDownlink(['kuaishou']);
    const adapter = { sendOutbound: vi.fn(async () => ({ ok: true, rateLimited: false, notFound: false })) };
    const c = cfg();
    getOutbox.mockResolvedValue({
      status: 'ok',
      messages: [{ msg_id: 'mh:samehash', content: '你好', conversation_id: 'convA' }],
    });
    ackOutbox.mockResolvedValue({ status: 'ok' });
    await pollDownlink('kuaishou', 'acc1', c, { sendOutbound: adapter.sendOutbound });
    expect(adapter.sendOutbound).toHaveBeenCalledTimes(1);

    getOutbox.mockResolvedValue({
      status: 'ok',
      messages: [{ msg_id: 'mh:samehash', content: '你好', conversation_id: 'convB' }],
    });
    await pollDownlink('kuaishou', 'acc1', c, { sendOutbound: adapter.sendOutbound });

    expect(adapter.sendOutbound).toHaveBeenCalledTimes(2);
    expect(adapter.sendOutbound).toHaveBeenLastCalledWith('你好', 'convB', expect.any(Object));
    expect(ackOutbox).toHaveBeenCalledTimes(2);
  });
});

describe('downlink / 多会话不串行（2026-08-07 核心修复）', () => {
  let chrome;
  beforeEach(() => { chrome = makeChromeStorage(); globalThis.chrome = chrome; vi.clearAllMocks(); });
  afterEach(() => { delete globalThis.chrome; });

  it('两个会话的 pending outbound 全部下发 + 全部 ack（不被全局 minInterval 互相阻断）', async () => {
    await initDownlink(['xiaohongshu']);
    const adapter = {
      sendOutbound: vi.fn(async (text, convId) => {
        return { ok: true, rateLimited: false, notFound: false };
      })
    };
    getOutbox.mockResolvedValue({
      status: 'ok',
      messages: [
        { msg_id: 'm-convA', content: '回复 A 的消息', conversation_id: 'convA' },
        { msg_id: 'm-convB', content: '回复 B 的消息', conversation_id: 'convB' },
      ],
    });
    ackOutbox.mockResolvedValue({ status: 'ok' });
    await pollDownlink('xiaohongshu', 'acc1', cfg(), { sendOutbound: adapter.sendOutbound });

    expect(adapter.sendOutbound).toHaveBeenCalledTimes(2);
    expect(adapter.sendOutbound).toHaveBeenCalledWith('回复 A 的消息', 'convA', expect.any(Object));
    expect(adapter.sendOutbound).toHaveBeenCalledWith('回复 B 的消息', 'convB', expect.any(Object));

    expect(ackOutbox).toHaveBeenCalledTimes(2);
    const ackConvIds = ackOutbox.mock.calls.map((c) => (c[2] && c[2].label) || '');
    expect(ackConvIds.some((l) => l.includes('convA'))).toBe(true);
    expect(ackConvIds.some((l) => l.includes('convB'))).toBe(true);
  });

  it('会话 A 被限速拦截，会话 B 仍正常下发（不跨会话阻断、不污染）', async () => {
    await initDownlink(['xiaohongshu']);
    const adapter = {
      sendOutbound: vi.fn(async (text, convId) => {
        if (convId === 'convA') return { ok: false, rateLimited: true, notFound: false };
        return { ok: true, rateLimited: false, notFound: false };
      })
    };
    getOutbox.mockResolvedValue({
      status: 'ok',
      messages: [
        { msg_id: 'm-A1', content: 'A1', conversation_id: 'convA' },
        { msg_id: 'm-B1', content: 'B1', conversation_id: 'convB' },
        { msg_id: 'm-B2', content: 'B2', conversation_id: 'convB' },
      ],
    });
    ackOutbox.mockResolvedValue({ status: 'ok' });
    await pollDownlink('xiaohongshu', 'acc1', cfg(), {
      sendOutbound: adapter.sendOutbound,
      rateRetryWaitMs: 1,
    });

    // 会话 A：持续被限速 → 初始 1 次 + 有限重试（MAX_RATE_RETRIES=3）后放弃，不在 A 注入 B
    const convACalls = adapter.sendOutbound.mock.calls.filter((c) => c[1] === 'convA');
    expect(convACalls.length).toBeGreaterThanOrEqual(2); 
    // 会话 B：2 次调用都成功（会话 A 限速不阻断会话 B）
    const convBCalls = adapter.sendOutbound.mock.calls.filter((c) => c[1] === 'convB');
    expect(convBCalls.length).toBe(2);
    expect(ackOutbox).toHaveBeenCalledTimes(1);
    expect(ackOutbox).toHaveBeenCalledWith(expect.any(Object), ['m-B1', 'm-B2'], expect.any(Object));
  });

  it('串行转发：同一会话文案只进入自己的会话输入框（无跨会话污染）', async () => {
    await initDownlink(['xiaohongshu']);
    // 真实 sendOutbound 签名 (text, convId)；断言每条文案确实按 conversation_id 派发到对应会话
    const adapter = {
      sendOutbound: vi.fn(async (text, convId) => ({ ok: true, rateLimited: false, notFound: false })),
    };
    getOutbox.mockResolvedValue({
      status: 'ok',
      messages: [
        { msg_id: 'm-A', content: '给A的回复', conversation_id: 'convA' },
        { msg_id: 'm-B', content: '给B的回复', conversation_id: 'convB' },
      ],
    });
    ackOutbox.mockResolvedValue({ status: 'ok' });
    await pollDownlink('xiaohongshu', 'acc1', cfg(), { sendOutbound: adapter.sendOutbound });
    expect(adapter.sendOutbound).toHaveBeenCalledWith('给A的回复', 'convA', expect.any(Object));
    expect(adapter.sendOutbound).toHaveBeenCalledWith('给B的回复', 'convB', expect.any(Object));
  });
});

