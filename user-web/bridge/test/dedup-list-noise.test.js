import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { BaseAdapter } from '../src/core/channel-adapter.js';
import { SENDER } from '../src/core/types.js';

// 气泡与容器都必须是真实 DOM。首版用纯对象当气泡、`{}` 当容器：`start()` 里
// `new MutationObserver(...).observe({})` 抛 TypeError 被 catch 吞成一句 error 日志 ⇒
// 三条断言其实跑在一个从没启动起来的适配器上（回填 / convPolling / fallbackTimer 全没起来）。
// 现在每腿都断言 start() 返回 true，半启动的适配器不能再假装绿。
function makeFixture(state) {
  const root = document.createElement('div');
  document.body.appendChild(root);
  let items = [];
  const hooks = {
    match: () => true,
    getMessageItems: () => items,
    parseMessageItem: (node) => ({
      message_id: node.__text,
      sender_type: node.__sender,
      text: node.__text,
      media_url: '',
      timestamp: Date.now(),
      raw: '',
    }),
    getConversationId: () => state.convId,
    getMessageRoot: () => root,
  };
  const adapter = new BaseAdapter({ name: 't', channel: 'douyin_web', SEL: {}, hooks });
  return {
    adapter,
    // arrive：新消息挂进当前会话的消息容器（真页面上气泡一定在容器里）
    arrive: (text, senderType = SENDER.CUSTOMER) => {
      const el = document.createElement('div');
      el.__text = text;
      el.__sender = senderType;
      root.appendChild(el);
      items.push(el);
      return el;
    },
    // swapTo：虚拟列表切到另一个会话 —— 旧气泡离开文档
    swapTo: () => { root.innerHTML = ''; items = []; },
  };
}

let current = null;
beforeEach(() => { document.body.innerHTML = ''; });
afterEach(() => { if (current) { current.stop(); current = null; } });

describe('去重 + 列表噪声防护', () => {
  let inbound;

  beforeEach(() => {
    inbound = [];
  });

  it('无活动会话（conv:null）时反复扫描也不上行任何消息', () => {
    const { adapter, arrive } = makeFixture({ convId: null });
    arrive('钓点王');
    arrive('小马哥不空军');
    arrive('吴小小');
    current = adapter;
    const cb = { onMessage: (m) => inbound.push(m) };
    expect(adapter.start(cb)).toBe(true);
    for (let i = 0; i < 5; i++) adapter._scanIncremental();
    expect(inbound).toHaveLength(0);
  });

  it('同一批节点（哪怕 timestamp 每次不同）只上行一次，不会无限重复', () => {
    const { adapter, arrive } = makeFixture({ convId: 'MS4w_test' });
    current = adapter;
    const cb = { onMessage: (m) => inbound.push(m) };
    expect(adapter.start(cb)).toBe(true);
    arrive('你好在吗');
    arrive('怎么收费');
    for (let i = 0; i < 10; i++) adapter._scanIncremental();
    expect(inbound).toHaveLength(2);
    expect(inbound.map((m) => m.content).sort()).toEqual(['你好在吗', '怎么收费']);
  });

  it('会话切换后新节点正常作为新消息上行', () => {
    const state = { convId: 'conv-1' };
    const { adapter, arrive, swapTo } = makeFixture(state);
    current = adapter;
    const cb = { onMessage: (m) => inbound.push(m) };
    expect(adapter.start(cb)).toBe(true);
    arrive('第一批');
    adapter._scanIncremental();
    expect(inbound).toHaveLength(1);

    // 切换会话（convPolling 会调用 _attachConversation；这里直接模拟新会话 + 新节点）
    state.convId = 'conv-2';
    adapter.conversationId = 'conv-2';
    swapTo();
    arrive('第二批');
    adapter._scanIncremental();
    expect(inbound).toHaveLength(2);
    expect(inbound[1].content).toBe('第二批');
  });
});
