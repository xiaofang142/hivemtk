// 批24（§8.3-8 的第二刀，随回查一起落）：外层 withTimeout 的预算必须包含回查那一段。
//
// 回查为什么会影响超时预算：pollDownlink 把 sendOutbound 整个包在 withTimeout 里，
// 超时 ⇒ catch ⇒ result=null ⇒ ok=false ⇒ **不记已发、不 ack** ⇒ 服务端下一轮重推。
// 而"点了但被判超时"的那条消息其实已经发出去了（扩展还会把 AGENT 回声上行入库），
// 重推就是双发 —— 本仓唯一不可撤销的那一侧。批24 给 sendOutbound 尾部加了 2.5s 回查，
// 于是"原本刚好卡在外层预算内"的慢发送会被这段回查推出预算。所以外层预算必须是
// 「sendText 长度感知预算 + 回查切片」，而不是继续只给前者。
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';

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
    runtime: { sendMessage: vi.fn(async () => {}) },
    _data: data,
  };
}

vi.mock('../src/core/http-ingest.js', async (importOriginal) => {
  const actual = await importOriginal();
  return { ...actual, getOutbox: vi.fn(), ackOutbox: vi.fn() };
});
vi.mock('../src/core/sanitize.js', () => ({
    sanitizeForDisplay: (t) => t,
    stripMarkdownForDM: (t) => t,
  }));

import { pollDownlink, initDownlink } from '../src/core/downlink.js';
import { getOutbox, ackOutbox } from '../src/core/http-ingest.js';
import { BRIDGE_THREE_CHANNEL } from '../src/core/constants.js';

function cfg() { return async () => ({ serverUrl: 'http://localhost:8204', token: 't' }); }

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

describe('批24：下发外层超时预算必须覆盖回查切片', () => {
  let chrome;
  beforeEach(() => { chrome = makeChromeStorage(); globalThis.chrome = chrome; vi.clearAllMocks(); });
  afterEach(() => { delete globalThis.chrome; });

  it('发送耗时越过 sendText 预算、但落在回查切片内 → 仍算已发并 ack（不得重推）', async () => {
    await initDownlink(['b24a']);
    getOutbox.mockResolvedValue({
      status: 'ok',
      messages: [{ msg_id: 'm1', content: 'hi', conversation_id: 'c1' }],
    });
    ackOutbox.mockResolvedValue({ status: 'ok' });
    // sendOutboundTimeoutMs=50 ⇒ 旧口径下外层预算 = 50 + 2*250 = 550ms；本夹具 700ms 才回包。
    const sendOutbound = vi.fn(async () => { await sleep(700); return { ok: true, sendVerified: true }; });
    await pollDownlink('b24a', 'acc1', cfg(), { sendOutbound, sendOutboundTimeoutMs: 50 });
    expect(ackOutbox).toHaveBeenCalledTimes(1);
    expect(ackOutbox.mock.calls[0][2].items).toEqual([{ msg_id: 'm1', conversation_id: 'c1' }]);
  });

  it('预算仍是上界：永远不回包的发送要在「sendText 预算 + 回查切片」内被切掉并留 pending', async () => {
    await initDownlink(['b24b']);
    getOutbox.mockResolvedValue({
      status: 'ok',
      messages: [{ msg_id: 'm9', content: 'hi', conversation_id: 'c1' }],
    });
    ackOutbox.mockResolvedValue({ status: 'ok' });
    const t0 = Date.now();
    await pollDownlink('b24b', 'acc1', cfg(), {
      sendOutbound: () => new Promise(() => {}), // 永不回包
      sendOutboundTimeoutMs: 50,
    });
    const spent = Date.now() - t0;
    expect(ackOutbox).not.toHaveBeenCalled();
    // 550ms（sendText 预算）+ 回查切片，允许一次节拍误差；不给切片就会只等 550ms。
    expect(spent).toBeGreaterThanOrEqual(550 + BRIDGE_THREE_CHANNEL.sendVerifyMs - 120);
    expect(spent).toBeLessThan(550 + BRIDGE_THREE_CHANNEL.sendVerifyMs + 1200);
  });

  // 这一条只能静态锁：`sendVerified` 只进 debug 结果日志，logger 是模块级单例、
  // 且 verbose 门在测试环境关着 ⇒ 没有行为钩子可断。锁的牙由电池 V10 证明
  // （把 `!!` 加回去 ⇒ 本条必须红）。
  it('静态锁：结果日志原样透传 sendVerified（三态不许被 `!!` 洗成二态）', () => {
    const dl = readFileSync(join(process.cwd(), 'src/core/downlink.js'), 'utf8');
    expect(dl).toContain('sendVerified: result && result.sendVerified,');
    expect(dl).not.toContain('sendVerified: !!(result && result.sendVerified)');
  });
});
