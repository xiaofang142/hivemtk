import { describe, it, expect, beforeEach, vi } from 'vitest';
import { BaseAdapter } from '../src/core/channel-adapter.js';
import { SENDER, CHANNELS } from '../src/core/types.js';

beforeEach(() => {
  document.body.innerHTML = '';
});

// 让 jsdom 元素有可见性（真实浏览器 offsetParent 由布局决定；jsdom 恒为 null）
function makeVisible(el) {
  Object.defineProperty(el, 'offsetParent', { configurable: true, get: () => ({}) });
  return el;
}

// 构造一条消息 DOM 节点 + 解析元数据
function makeMessage(id, text, { sender = SENDER.CUSTOMER, msgType = 'text' } = {}) {
  return {
    el: makeVisible(document.createElement('div')),
    id,
    sender,
    text,
    msgType,
    ts: Date.now(),
  };
}

// 构造一个会话列表项 + 点击行为
function makeConv(id, name, { unread = false } = {}) {
  const el = makeVisible(document.createElement('button'));
  el.setAttribute('data-e2e', 'conversation-item');
  return { id, name, el, unread };
}

// 构造适配器：点击会话项后活动会话变为该会话；按活动会话返回其消息列表
function buildAdapter(convList, messagesByConv) {
  let current = null;
  const hooks = {
    match: () => true,
    getAccountId: () => 'acc1',
    getConversationId: () => current,
    getMessageRoot: () => null,
    getMessageItems: () => (messagesByConv[current] || []).map((m) => m.el),
    parseMessageItem: (item) => {
      const m = (messagesByConv[current] || []).find((x) => x.el === item);
      if (!m) return null;
      return { sender_type: m.sender, text: m.text, msg_type: m.msgType, message_id: m.id, timestamp: m.ts };
    },
    extractMessages: () => null,
    getConversationList: () => convList,
  };
  const adapter = new BaseAdapter({ name: 'test', channel: CHANNELS.DOUYIN, SEL: {}, hooks });
  convList.forEach((conv) => {
    conv.el.addEventListener('click', () => { current = conv.id; });
  });
  return { adapter, getActive: () => current };
}

describe('BaseAdapter.patrol 巡检一轮', () => {
  it('遍历所有会话 → 每个会话一条会话级 inbound 帧（2026-08-05 修复：不判断未读，直接遍历）', async () => {
    const A = makeConv('a1', '张三', { unread: true });
    const B = makeConv('b2', '李四', { unread: true });
    const C = makeConv('c3', '王五', { unread: false }); 
    const convList = [A, B, C];
    const messagesByConv = {
      a1: [makeMessage('m-a1', '你好')],
      b2: [makeMessage('m-b2', '在吗')],
      c3: [makeMessage('m-c3', '已读消息')],
    };
    const { adapter } = buildAdapter(convList, messagesByConv);
    const onInbound = vi.fn();
    adapter.callbacks.onMessage = onInbound;

    const r = await adapter.patrol({ throttleMs: 1, waitActiveMs: 200, switchMinMs: 0, switchMaxMs: 0 });

    expect(r.visited).toBe(3);
    expect(r.withNew).toBe(3);
    expect(r.captured).toBe(3);
    expect(onInbound).toHaveBeenCalledTimes(3); 
    const convIds = onInbound.mock.calls.map((c) => c[0].conversation_id);
    expect(convIds).toEqual(expect.arrayContaining(['a1', 'b2', 'c3']));
    // 需求③：一个会话=一条消息，history[] 含本次捕获的多轮
    const first = onInbound.mock.calls[0][0];
    expect(Array.isArray(first.history)).toBe(true);
    expect(first.history.length).toBe(1);
    // 昵称作为客户名/发件人
    const names = onInbound.mock.calls.map((c) => c[0].sender_name);
    expect(names).toEqual(expect.arrayContaining(['张三', '李四', '王五']));
  });

  it('已 seen 的消息靠去重跳过：第二轮巡检不再重复上行（不重复打扰）', async () => {
    const A = makeConv('a1', '张三', { unread: true });
    const convList = [A];
    const messagesByConv = { a1: [makeMessage('m-a1', '你好')] };
    const { adapter } = buildAdapter(convList, messagesByConv);
    const onInbound = vi.fn();
    adapter.callbacks.onMessage = onInbound;

    await adapter.patrol({ throttleMs: 1, waitActiveMs: 200, switchMinMs: 0, switchMaxMs: 0 });
    expect(onInbound).toHaveBeenCalledTimes(1);

    // 第二轮：该消息已在 seen 集合里 → 不再上行
    const r2 = await adapter.patrol({ throttleMs: 1, waitActiveMs: 200 });
    expect(r2.captured).toBe(0);
    expect(r2.withNew).toBe(0);
    expect(onInbound).toHaveBeenCalledTimes(1); 
  });

  it('无 unread 标记时也巡检所有会话（2026-08-05 修复：巡检不再判断未读，直接遍历）', async () => {
    const A = makeConv('a1', '张三', { unread: false });
    const B = makeConv('b2', '李四', { unread: false });
    const convList = [A, B];
    const messagesByConv = {
      a1: [makeMessage('m-a1', '问候')],
      b2: [makeMessage('m-b2', '咨询')],
    };
    const { adapter } = buildAdapter(convList, messagesByConv);
    const onInbound = vi.fn();
    adapter.callbacks.onMessage = onInbound;

    const r = await adapter.patrol({ throttleMs: 1, waitActiveMs: 200, switchMinMs: 0, switchMaxMs: 0 });
    expect(r.visited).toBe(2);
    expect(r.captured).toBe(2);
    expect(onInbound).toHaveBeenCalledTimes(2);
  });

  it('visitAllWhenNoUnread 兼容：无 unread 标记时也巡检全部会话（靠 seen 去重只报新消息）', async () => {
    const A = makeConv('a1', '张三', { unread: false });
    const convList = [A];
    const messagesByConv = { a1: [makeMessage('m-a1', '新消息')] };
    const { adapter } = buildAdapter(convList, messagesByConv);
    const onInbound = vi.fn();
    adapter.callbacks.onMessage = onInbound;

    // 两个 switch 延时必须显式归零：缺省时它们是生产的 3000–5000ms 随机切会话节奏，
    // 本用例真等 ⇒ 单条耗 3–5s，贴着 vitest 默认的 5s 单测超时，CI 上与 jsdom 启动争抢时先红。
    const r = await adapter.patrol({ throttleMs: 1, waitActiveMs: 200, switchMinMs: 0, switchMaxMs: 0, visitAllWhenNoUnread: true });
    expect(r.visited).toBe(1);
    expect(r.captured).toBe(1);
    expect(onInbound).toHaveBeenCalledTimes(1);
  });

  it('需求⑥：非文字消息（image）在巡检中被跳过，仅上行文字消息', async () => {
    const A = makeConv('a1', '张三', { unread: true });
    const convList = [A];
    const messagesByConv = {
      a1: [
        makeMessage('m-img', '', { msgType: 'image' }),
        makeMessage('m-txt', '这是文字消息'),
      ],
    };
    const { adapter } = buildAdapter(convList, messagesByConv);
    const onInbound = vi.fn();
    adapter.callbacks.onMessage = onInbound;

    const r = await adapter.patrol({ throttleMs: 1, waitActiveMs: 200, switchMinMs: 0, switchMaxMs: 0 });
    expect(r.captured).toBe(1); 
    const msg = onInbound.mock.calls[0][0];
    expect(msg.history.length).toBe(1);
    expect(msg.history[0].content).toBe('这是文字消息');
  });

  it('多轮历史：一个未读会话含多条新消息 → 一条 inbound 帧的 history[] 含全部多轮', async () => {
    const A = makeConv('a1', '张三', { unread: true });
    const convList = [A];
    const messagesByConv = {
      a1: [
        makeMessage('m-1', '第一条'),
        makeMessage('m-2', '第二条'),
        makeMessage('m-3', '第三条'),
      ],
    };
    const { adapter } = buildAdapter(convList, messagesByConv);
    const onInbound = vi.fn();
    adapter.callbacks.onMessage = onInbound;

    const r = await adapter.patrol({ throttleMs: 1, waitActiveMs: 200, switchMinMs: 0, switchMaxMs: 0 });
    expect(r.captured).toBe(3);
    expect(onInbound).toHaveBeenCalledTimes(1); 
    const msg = onInbound.mock.calls[0][0];
    expect(msg.history.length).toBe(3);
    expect(msg.history.map((h) => h.content)).toEqual(['第一条', '第二条', '第三条']);
  });
});

describe('BaseAdapter patrol 启停控制', () => {
  it('startPatrol 后 running=true，stopPatrol 后 running=false', () => {
    const convList = [makeConv('a1', '张三', { unread: true })];
    const { adapter } = buildAdapter(convList, {});
    expect(adapter.isPatrolling()).toBe(false);
    const r = adapter.startPatrol({ intervalMs: 5000 });
    expect(r.ok).toBe(true);
    expect(r.intervalMs).toBe(5000);
    expect(adapter.isPatrolling()).toBe(true);
    expect(adapter.patrolStatus().running).toBe(true);
    adapter.stopPatrol();
    expect(adapter.isPatrolling()).toBe(false);
    expect(adapter.patrolStatus().running).toBe(false);
  });

  it('patrolStatus 反映累计统计（rounds/visited/captured/...）', async () => {
    const A = makeConv('a1', '张三', { unread: true });
    const convList = [A];
    const messagesByConv = { a1: [makeMessage('m-a1', '你好')] };
    const { adapter } = buildAdapter(convList, messagesByConv);
    adapter.callbacks.onInbound = vi.fn();
    await adapter.patrol({ throttleMs: 1, waitActiveMs: 200, switchMinMs: 0, switchMaxMs: 0 });
    const s = adapter.patrolStatus();
    expect(s.rounds).toBe(1);
    expect(s.visited).toBe(1);
    expect(s.captured).toBe(1);
    expect(s.withNew).toBe(1);
    expect(s.failures).toBe(0);
    expect(s.lastDurationMs).toBeGreaterThanOrEqual(0);
  });
});