import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { computeMsgID, Uplink } from '../src/core/uplink.js';
import { contentHash } from '../src/core/types.js';

describe('uplink / computeMsgID（消息 hash 前端完成，与后端 ContentHashMsgID 一致）', () => {
  it('同输入产出同 hash（幂等去重主键）', () => {
    const a = { channel: 'douyin', conversationId: 'c1', content: '你好' };
    const b = { channel: 'douyin', conversationId: 'c1', content: '你好' };
    expect(computeMsgID(a)).toBe(computeMsgID(b));
  });
  it('内容不同 → hash 不同', () => {
    const a = { channel: 'douyin', conversationId: 'c1', content: '你好' };
    const b = { channel: 'douyin', conversationId: 'c1', content: '你好吗' };
    expect(computeMsgID(a)).not.toBe(computeMsgID(b));
  });
  it('hash 以 mh: 前缀（与后端 ContentHashMsgID 同源 FNV-1a），长度稳定', () => {
    const id = computeMsgID({ channel: 'xhs', conversationId: 'c', content: 'x' });
    expect(id.startsWith('mh:')).toBe(true);
    expect(id.length).toBe(11); 
  });
  it('channel / conversationId / content 任一不同 → hash 不同（隔离会话与渠道）', () => {
    const base = { channel: 'douyin', conversationId: 'c1', content: 'x' };
    expect(computeMsgID(base)).not.toBe(computeMsgID({ ...base, channel: 'xhs' }));
    expect(computeMsgID(base)).not.toBe(computeMsgID({ ...base, content: 'y' }));
  });
  it('跨语言契约锚点：contentHash("douyin","c1","你好") === mh:00550fed（与 Go ContentHashMsgID 逐字节一致）', () => {
    expect(contentHash('douyin', 'c1', '你好')).toBe('mh:00550fed');
  });
  it('content 首尾空白被 trim，不影响哈希（与后端 strings.TrimSpace 对齐）', () => {
    expect(contentHash('douyin', 'c1', '  你好  ')).toBe(contentHash('douyin', 'c1', '你好'));
  });
});

describe('uplink / enqueue + flushAll', () => {
  let realFetch;
  beforeEach(() => { realFetch = globalThis.fetch; });
  afterEach(() => { globalThis.fetch = realFetch; vi.restoreAllMocks(); });

  it('M-6 关注信号透传：extra（new_user_unfollowed/new_user_signal）随上行体到达服务端', async () => {
    let captured;
    globalThis.fetch = vi.fn(async (url, init) => { captured = init.body; return new Response('{"ok":true,"ingested":[]}', { status: 200 }); });
    const uplink = new Uplink({ channel: 'douyin', getConfig: async () => ({ serverUrl: 'http://x', token: 't' }) });
    uplink.enqueue({
      channel: 'douyin', account_id: 'acc1', conversation_id: 'c1', sender_type: 'system',
      content: '对方关注了你', timestamp: 1,
      extra: { new_user_unfollowed: true, new_user_signal: 'peer_followed_you' },
    });
    await uplink.flushAll();
    const body = JSON.parse(captured);
    expect(body.messages[0].extra).toEqual({ new_user_unfollowed: true, new_user_signal: 'peer_followed_you' });
  });

  it('enqueue 兜底补全 event_id（前端 hash）', async () => {
    let captured;
    globalThis.fetch = vi.fn(async (url, init) => { captured = init.body; return new Response('{"ok":true,"ingested":[]}', { status: 200 }); });
    const uplink = new Uplink({ channel: 'douyin', getConfig: async () => ({ serverUrl: 'http://x', token: 't' }) });
    uplink.enqueue({ channel: 'douyin', account_id: 'acc1', conversation_id: 'c1', sender_type: 'customer', content: '你好', timestamp: 1 });
    await uplink.flushAll();
    const body = JSON.parse(captured);
    expect(body.messages[0].event_id).toMatch(/^mh:/); 
    expect(body.messages[0].content_hash).toBe(body.messages[0].event_id); 
  });

  it('合并窗口：同会话 3 条消息合并为一次 POST', async () => {
    let fetchCount = 0;
    let lastBody;
    globalThis.fetch = vi.fn(async (_u, init) => { fetchCount += 1; lastBody = init.body; return new Response('{"ok":true,"ingested":[]}', { status: 200 }); });
    const uplink = new Uplink({ channel: 'douyin', getConfig: async () => ({ serverUrl: 'http://x', token: 't' }) });
    for (let i = 0; i < 3; i++) {
      uplink.enqueue({ channel: 'douyin', account_id: 'acc1', conversation_id: 'c1', sender_type: 'customer', content: `m${i}`, timestamp: i });
    }
    await uplink.flushAll();
    expect(fetchCount).toBe(1);
    const body = JSON.parse(lastBody);
    expect(body.messages).toHaveLength(3);
  });

  it('不同会话分别 POST（各自 flush）', async () => {
    let fetchCount = 0;
    globalThis.fetch = vi.fn(async () => { fetchCount += 1; return new Response('{"ok":true,"ingested":[]}', { status: 200 }); });
    const uplink = new Uplink({ channel: 'douyin', getConfig: async () => ({ serverUrl: 'http://x', token: 't' }) });
    uplink.enqueue({ channel: 'douyin', account_id: 'acc1', conversation_id: 'c1', content: 'a', timestamp: 1 });
    uplink.enqueue({ channel: 'douyin', account_id: 'acc1', conversation_id: 'c2', content: 'b', timestamp: 2 });
    await uplink.flushAll();
    expect(fetchCount).toBe(2);
  });
});

