import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { BaseAdapter } from '../src/core/channel-adapter.js';
import { SENDER, DIRECTION } from '../src/core/types.js';
import * as selectorAi from '../src/core/selector-ai.js';

function msgEl(text, st, id, ts) {
  const el = document.createElement('div');
  el.textContent = text;
  el.__st = st;
  el.__id = id;
  el.__ts = ts;
  return el;
}

function makeAdapter(items) {
  const root = document.createElement('div');
  document.body.appendChild(root);
  // 气泡必须真的挂在消息容器里：批24b 起 _backfill/_handleIncremental 会判会话归属
  // （root.contains(item)），游离节点在真页面上不存在，判成"上一会话残留"是对的。
  for (const el of items) root.appendChild(el);
  const hooks = {
    match: () => true,
    getMessageListRoot: () => root,
    getMessageItems: () => items,
    getAccountId: () => 'acc1',
    getConversationId: () => 'conv1',
    parseMessageItem: (it) => ({
      text: it.textContent,
      sender_type: it.__st,
      message_id: it.__id,
      timestamp: it.__ts || Date.now(),
    }),
  };
  const a = new BaseAdapter({ name: 't', channel: 'douyin', SEL: { MSG_ITEM: '[data-x]' }, hooks });
  return { a, root };
}

let current = null;
beforeEach(() => { document.body.innerHTML = ''; });
afterEach(() => { if (current) { current.stop(); current = null; } document.body.innerHTML = ''; });

describe('getMessageRoot 回退', () => {
  it('缺失 getMessageRoot 时回退到 getMessageListRoot（非空）', () => {
    const { a, root } = makeAdapter([]);
    expect(a.getMessageRoot()).toBe(root);
  });
});

describe('历史回填 _backfill', () => {
  it('一个会话 = 一条会话级消息，内含全部多轮历史，统一走 onMessage', () => {
    const items = [
      msgEl('客户历史消息A', SENDER.CUSTOMER, 'm1', 1700000000000),
      msgEl('我的历史回复B', SENDER.AGENT, 'm2', 1700000001000),
    ];
    const { a } = makeAdapter(items);
    current = a;
    const messages = [];
    a.start({
      onMessage: (m) => messages.push(m),
    });
    expect(messages.length).toBe(1);
    const frame = messages[0];
    expect(frame.conversation_id).toBe('conv1');
    expect(frame.history).toBeDefined();
    expect(frame.history.length).toBe(2);
    const cust = frame.history.find((h) => h.content === '客户历史消息A');
    const self = frame.history.find((h) => h.content === '我的历史回复B');
    expect(cust.direction).toBe(DIRECTION.INBOUND);
    expect(self.direction).toBe(DIRECTION.OUTBOUND);
  });

  it('回填再次触发（延迟 1.5s）不重复上报：seenNodes 去重 → 不再发帧', () => {
    const items = [
      msgEl('客户历史消息A', SENDER.CUSTOMER, 'm1', 1700000000000),
      msgEl('我的历史回复B', SENDER.AGENT, 'm2', 1700000001000),
    ];
    const { a } = makeAdapter(items);
    current = a;
    const messages = [];
    a.start({
      onMessage: (m) => messages.push(m),
    });
    expect(messages.length).toBe(1); 
    a._backfill(); 
    expect(messages.length).toBe(1);
  });
});

describe('纯桥接：所有消息统一走 onMessage', () => {
  it('客户消息走 onMessage（sender_type=CUSTOMER）', () => {
    const { a, root } = makeAdapter([]);
    current = a;
    const messages = [];
    a.start({
      onMessage: (m) => messages.push(m),
    });
    const el = msgEl('客户新消息', SENDER.CUSTOMER, 'm3', Date.now());
    root.appendChild(el);
    a._handleIncremental(el);
    expect(messages.length).toBe(1);
    expect(messages[0].content).toBe('客户新消息');
  });

  it('切换会话后客户消息仍走 onMessage', () => {
    const { a, root } = makeAdapter([]);
    current = a;
    const messages = [];
    a.start({
      onMessage: (m) => messages.push(m),
    });
    a.conversationId = 'conv1';
    a.hooks.getConversationId = () => 'conv2';
    a.conversationId = 'conv2';
    a._attachConversation();
    const el = msgEl('切回会话后的客户消息', SENDER.CUSTOMER, 'm5', Date.now());
    root.appendChild(el);
    a._handleIncremental(el);
    expect(messages.length).toBe(1);
    expect(messages[0].content).toBe('切回会话后的客户消息');
  });
});

describe('群聊 + 实时上下文窗口（点3）', () => {
  function groupMsgEl(text, st, id, senderName) {
    const el = msgEl(text, st, id, Date.now());
    el.__senderName = senderName;
    el.__isGroup = true;
    el.__groupId = 'group-1';
    el.__groupName = '产品交流群';
    return el;
  }

  function makeGroupAdapter(items) {
    const root = document.createElement('div');
    document.body.appendChild(root);
    for (const el of items) root.appendChild(el);
    const hooks = {
      match: () => true,
      getMessageListRoot: () => root,
      getMessageItems: () => items,
      getAccountId: () => 'acc1',
      getConversationId: () => 'group-1',
      parseMessageItem: (it) => ({
        text: it.textContent,
        sender_type: it.__st,
        message_id: it.__id,
        timestamp: it.__ts || Date.now(),
        sender_name: it.__senderName || '',
        is_group: !!it.__isGroup,
        group_id: it.__groupId || '',
        group_name: it.__groupName || '',
      }),
    };
    return new BaseAdapter({ name: 't', channel: 'xhs', SEL: { MSG_ITEM: '[data-x]' }, hooks });
  }

  it('群聊会话级回填：1 条帧，is_group/group_id/group_name 与逐轮 sender_name 上送', () => {
    const items = [
      groupMsgEl('有人在吗', SENDER.CUSTOMER, 'g1', '张三'),
      groupMsgEl('在的', SENDER.AGENT, 'g2', '客服小王'),
      groupMsgEl('@客服小王 帮我查下订单', SENDER.CUSTOMER, 'g3', '李四'),
    ];
    const a = makeGroupAdapter(items);
    current = a;
    const messages = [];
    a.start({ onMessage: (m) => messages.push(m) });
    expect(messages.length).toBe(1); 
    const frame = messages[0];
    expect(frame.is_group).toBe(true);
    expect(frame.group_id).toBe('group-1');
    expect(frame.group_name).toBe('产品交流群');
    expect(frame.history.length).toBe(3);
    expect(frame.history.map((h) => h.sender_name)).toEqual(['张三', '客服小王', '李四']);
    expect(frame.sender_id).toBe('group-1');
  });

  function groupParsed(text, st, id, senderName) {
    return {
      text,
      sender_type: st,
      message_id: id,
      timestamp: Date.now(),
      sender_name: senderName,
      is_group: true,
      group_id: 'group-1',
      group_name: '产品交流群',
    };
  }

  it('群聊去重按成员区分：不同成员发相同文本不被误删（需求3 多轮完整性）', () => {
    const a = makeGroupAdapter([]);
    current = a;
    const messages = [];
    a.start({ onMessage: (m) => messages.push(m) });
    a._ingest(groupParsed('好的', SENDER.CUSTOMER, 'g1', '张三'));
    a._ingest(groupParsed('好的', SENDER.CUSTOMER, 'g2', '李四'));
    expect(messages.length).toBe(2);
    expect(messages[0].content).toBe('好的');
    expect(messages[1].content).toBe('好的');
    expect(messages[0].history).toBeDefined();
  });

  it('同一成员重复发相同文本不再 Bridge 端去重（内容 hash 去重交给服务端）', () => {
    const a = makeGroupAdapter([]);
    current = a;
    const messages = [];
    a.start({ onMessage: (m) => messages.push(m) });
    a._ingest(groupParsed('好的', SENDER.CUSTOMER, 'g1', '张三'));
    a._ingest(groupParsed('好的', SENDER.CUSTOMER, 'g2', '张三'));
    expect(messages.length).toBe(2); 
    expect(messages[0].content).toBe('好的');
    expect(messages[1].content).toBe('好的');
  });

  it('实时消息携带该会话最近多轮上下文窗口', () => {
    const a = makeGroupAdapter([]);
    current = a;
    const messages = [];
    a.start({ onMessage: (m) => messages.push(m) });
    a._ingest(groupParsed('第一轮', SENDER.CUSTOMER, 'w1', '张三'));
    a._ingest(groupParsed('我方回复', SENDER.AGENT, 'w2', '客服小王'));
    a._ingest(groupParsed('第二轮', SENDER.CUSTOMER, 'w3', '李四'));
    a._ingest(groupParsed('第三轮请回复', SENDER.CUSTOMER, 'w4', '王五'));
    expect(messages.length).toBe(4);
    const frame = messages[3];
    expect(frame.content).toBe('第三轮请回复');
    expect(frame.history.length).toBe(4);
    expect(frame.history[0].content).toBe('第一轮');
    expect(frame.history[0].direction).toBe(DIRECTION.INBOUND);
    expect(frame.history[1].direction).toBe(DIRECTION.OUTBOUND); 
    expect(frame.history[3].direction).toBe(DIRECTION.INBOUND);
  });
});

describe('历史项 sender_id 解析（统一收信中心聚合键）', () => {
  function makeCidAdapter(getCid) {
    const hooks = {
      match: () => true,
      getAccountId: () => 'acc1',
      getConversationId: () => getCid(),
      getMessageRoot: () => null,
    };
    return new BaseAdapter({ name: 't', channel: 'xhs', SEL: {}, hooks });
  }

  it('1:1 客户消息 → sender_id = 会话 id（对方），非空', () => {
    const a = makeCidAdapter(() => 'conv-xhs-1');
    const item = a._historyItem(
      { sender_type: SENDER.CUSTOMER, text: '在吗', message_id: 'm1', timestamp: 1 },
      DIRECTION.INBOUND
    );
    expect(item.sender_id).toBe('conv-xhs-1');
    expect(item.receiver_id).toBe('');
  });

  it('自己/AI 回复 → sender_id = account_id，receiver_id = 会话 id', () => {
    const a = makeCidAdapter(() => 'conv-xhs-1');
    const item = a._historyItem(
      { sender_type: SENDER.AGENT, text: '在的', message_id: 'm2', timestamp: 2 },
      DIRECTION.OUTBOUND
    );
    expect(item.sender_id).toBe('acc1');
    expect(item.receiver_id).toBe('conv-xhs-1');
  });

  it('群聊消息 → sender_id = 群 id（group_id 优先）', () => {
    const a = makeCidAdapter(() => 'group-1');
    const item = a._historyItem(
      { sender_type: SENDER.CUSTOMER, text: '@所有人 在吗', message_id: 'g1', timestamp: 1, is_group: true, group_id: 'group-1', sender_name: '张三' },
      DIRECTION.INBOUND
    );
    expect(item.sender_id).toBe('group-1');
    expect(item.sender_name).toBe('张三');
  });
});

describe('纯规则架构（无 LLM 抽取器）', () => {
  function makeCidRootAdapter(getCid, root, items) {
    const hooks = {
      match: () => true,
      getAccountId: () => 'acc1',
      getConversationId: () => getCid(),
      getMessageRoot: () => root,
      getMessageItems: () => items || [],
      parseMessageItem: (it) => ({
        sender_type: SENDER.CUSTOMER,
        text: it.textContent,
        message_id: 'm-' + (it.__id || Math.random()),
        timestamp: Date.now(),
      }),
      extractMessages: () => [{ text: 'should-be-ignored', sender_type: 'customer' }], 
    };
    return new BaseAdapter({ name: 't', channel: 'xhs', SEL: {}, hooks });
  }

  it('_scanIncremental 走 selector 路径（getMessageItems），忽略 extractMessages 残留', () => {
    const root = document.createElement('div');
    document.body.appendChild(root);
    const a = makeCidRootAdapter(() => 'conv1', root, []);
    current = a;
    const messages = [];
    a.start({ onMessage: (m) => messages.push(m) });
    // 新增一条消息（模拟 MutationObserver/新消息到达），_scanIncremental 走 selector 路径
    const msgEl = document.createElement('div');
    msgEl.textContent = '来自选择器路径的消息';
    root.appendChild(msgEl);
    a.hooks.getMessageItems = () => [msgEl];
    a._scanIncremental();
    expect(messages.length).toBe(1);
    expect(messages[0].content).toBe('来自选择器路径的消息');
  });

  it('_backfill 不依赖 extractMessages（纯 getMessageItems 回填）', () => {
    const root = document.createElement('div');
    document.body.appendChild(root);
    const msgEl = document.createElement('div');
    msgEl.textContent = '回填历史消息';
    root.appendChild(msgEl);
    const a = makeCidRootAdapter(() => 'conv1', root, [msgEl]);
    current = a;
    const messages = [];
    a.start({ onMessage: (m) => messages.push(m) });
    a._backfill();
    expect(messages.length).toBeGreaterThan(0);
  });
});

