// §8.3-17（批23）：同一会话里客户把同一句话说两遍，扩展必须把它当两条真实消息上报。
//
// 原实现的三层身份全是「内容即身份」：_dedupKey=hash(会话|发送者|文本)、
// _canonicalMsgId=contentHash(渠道|会话|文本)，于是第二条同文本气泡在浏览器里就被
// _sentKeys 拦掉，连一帧都不发（客户侧表现＝「说了没回」）。
// 修法：按「本轮扫描里该内容键出现的次数」给第二条起加 #<n> 后缀 —— 首条款的键与
// event_id 字节不变（升级后不会把老消息重报一遍，服务端存量判定也不受影响）。
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { BaseAdapter } from '../src/core/channel-adapter.js';
import { contentHash } from '../src/core/types.js';

// _sentKeys 落 localStorage，同一 channel+domain 的键跨用例共用 ⇒ 上一条用例报过的文本会把
// 本条用例的首扫吞成 0 条。每例先清空，保证「首扫」是真首扫。
beforeEach(() => { localStorage.clear(); });

// 节点用对象身份区分（等价于 DOM 节点）；state.nodes 可整体换成「重渲染后的新一批节点」。
function makeState(texts) {
  return { nodes: texts.map((t) => ({ __text: t })) };
}

function buildAdapter(state, channel = 'douyin_web') {
  return new BaseAdapter({
    name: 'r23-occur',
    channel,
    SEL: {},
    hooks: {
      match: () => true,
      getAccountId: () => 'acct-r23',
      getConversationId: () => 'conv-r23',
      getMessageItems: () => state.nodes,
      parseMessageItem: (node) => {
        if (!node || node.__text === undefined) return null;
        return {
          sender_type: 'customer',
          sender_id: 'cust-1',
          text: node.__text,
          timestamp: 1,
        };
      },
    },
  });
}

describe('§8.3-17 同会话同文本的第二条要作为独立一条上行', () => {
  it('增量路径：先后两条同文本气泡 → 两条都上行，event_id 为裸哈希与 #1', () => {
    const state = makeState(['好的', '好的']);
    const adapter = buildAdapter(state);
    const onMessage = vi.fn();
    adapter.start({ onMessage });

    adapter._handleIncremental(state.nodes[0]);
    adapter._handleIncremental(state.nodes[1]);

    if (onMessage.mock.calls.length !== 2) {
      throw new Error(
        `§8.3-17 未达成：同一会话里第二条「好的」被内容稳定键吞掉，实际只上行 ${onMessage.mock.calls.length} 帧`
      );
    }
    const [first, second] = onMessage.mock.calls.map((c) => c[0]);
    const base = contentHash('douyin_web', 'conv-r23', '好的');
    expect(first.event_id).toBe(base); // 首条款字节不变：存量行与升级期判定不受影响
    expect(second.event_id).toBe(`${base}#1`);
    adapter.stop();
  });

  it('巡检批量路径：一轮扫描里的两条同文本气泡都进 batch，message_id 互不相同', () => {
    const state = makeState(['好的', '在吗', '好的']);
    const adapter = buildAdapter(state);
    adapter.start({ onMessage: vi.fn() });

    const out = adapter.getMessages();
    expect(out.map((m) => m.text)).toEqual(['好的', '在吗', '好的']);
    const base = contentHash('douyin_web', 'conv-r23', '好的');
    expect(out[0].message_id).toBe(base);
    expect(out[2].message_id).toBe(`${base}#1`);
    adapter.stop();
  });

  it('三条同文本 → 裸哈希 / #1 / #2（编号按 DOM 顺序）', () => {
    const state = makeState(['在吗', '在吗', '在吗']);
    const adapter = buildAdapter(state);
    adapter.start({ onMessage: vi.fn() });

    const out = adapter.getMessages();
    const base = contentHash('douyin_web', 'conv-r23', '在吗');
    expect(out.map((m) => m.message_id)).toEqual([base, `${base}#1`, `${base}#2`]);
    adapter.stop();
  });

  it('反向半边：重渲染换掉全部节点后重扫，一条都不许多发（编号稳定 ⇒ 幂等不退化）', () => {
    const state = makeState(['好的', '好的']);
    const adapter = buildAdapter(state);
    adapter.start({ onMessage: vi.fn() });

    expect(adapter.getMessages().length).toBe(2);
    state.nodes = ['好的', '好的'].map((t) => ({ __text: t })); // 虚拟列表重渲染：节点全换、内容不变
    expect(adapter.getMessages().length).toBe(0);
    adapter.stop();
  });
});
