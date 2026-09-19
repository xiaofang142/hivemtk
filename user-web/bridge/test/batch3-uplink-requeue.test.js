// B4（批3）uplink flush 失败回插行为测试：postIngest mock 拒绝 → 未确认条目必须回到
// buffer 队首（原实现 catch(e){} + buffers.delete = 瞬态网络故障直接丢上行消息）。
import { describe, it, expect, vi } from 'vitest';

const postIngest = vi.fn();
vi.mock('../src/core/http-ingest.js', () => ({
  postIngest: (...a) => postIngest(...a),
  HTTP_INGEST_DEFAULTS: { longPollTimeoutMs: 30000 },
  getOutbox: vi.fn(),
  ackOutbox: vi.fn(),
}));

const { Uplink } = await import('../src/core/uplink.js');

const msg = (i) => ({
  channel: 'douyin',
  account_id: 'acc-1',
  conversation_id: 'conv-1',
  content: `m${i}`,
  timestamp: Date.now(),
});

describe('uplink flush 失败回插', () => {
  it('postIngest 拒绝 → 全部未确认条目回插 buffer；恢复后重传成功即清空', async () => {
    const up = new Uplink({ channel: 'douyin', getConfig: async () => ({ serverUrl: 'http://x', token: 't' }) });
    // 达到 maxBatch 触发即时 flush
    for (let i = 0; i < up.maxBatch; i++) up.enqueue(msg(i));
    postIngest.mockRejectedValueOnce(new Error('network down'));
    await new Promise((r) => setTimeout(r, 20)); // 等 _flush 的 reject 分支落地
    const buf = up.buffers.get('acc-1|conv-1');
    expect(buf).toBeTruthy();
    expect(buf.items.length).toBe(up.maxBatch);
    expect(buf.items[0].content).toBe('m0'); // 原顺序保留

    // 第二次：服务端确认全部 → buffer 清空
    postIngest.mockImplementation(async (_c, body) => ({
      ingested: body.messages.map((m) => ({ event_id: m.event_id, accepted: true })),
    }));
    await up._flush('acc-1|conv-1');
    expect(up.buffers.get('acc-1|conv-1')?.items.length || 0).toBe(0);
  });

  it('回插封顶 maxBatch*5（离线期有界降级，不无界堆积）', async () => {
    const up = new Uplink({ channel: 'douyin', getConfig: async () => ({}) });
    postIngest.mockRejectedValue(new Error('offline'));
    for (let round = 0; round < 8; round++) {
      for (let i = 0; i < up.maxBatch; i++) up.enqueue(msg(round * 100 + i));
      await new Promise((r) => setTimeout(r, 5));
    }
    await new Promise((r) => setTimeout(r, 30));
    const buf = up.buffers.get('acc-1|conv-1');
    expect(buf.items.length).toBeLessThanOrEqual(up.maxBatch * 5);
  });

  it('flush 成功路径不回插、不重复发送（幂等由 _confirmed 承担）', async () => {
    const up = new Uplink({ channel: 'douyin', getConfig: async () => ({}) });
    for (let i = 0; i < 3; i++) up.enqueue(msg(i));
    up.enqueue(msg(1)); // 同内容 → 同 event_id（contentHash 幂等）
    postIngest.mockImplementation(async (_c, body) => ({
      ingested: body.messages.map((m) => ({ event_id: m.event_id, accepted: true })),
    }));
    await up._flush('acc-1|conv-1');
    expect(up.buffers.has('acc-1|conv-1')).toBe(false);
    expect(up._confirmed.size).toBe(3); // accepted 均入 confirmed（同内容重复 → 同一 event_id）
  });
});
