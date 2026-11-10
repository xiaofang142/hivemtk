// M-5（2026-11-11）巡检机制优化：未读优先增量巡检
//
// 覆盖面（对应任务 M-5 验收）：
//   1. 未读优先调度：开关开启的渠道巡检只访问带未读徽章的会话（代码层量化
//      「巡检打开会话数下降」：visited = min(unread 数, maxPerRound)，不再是
//      min(全量, maxPerRound)）；
//   2. 配额：未读数超过 maxPerRound 时截断；
//   3. 开关回退：关 = 完全现行行为（顺序全量遍历）；
//   4. fail-open：渠道列表缺 unread 布尔字段（未接线/异常）→ 该轮回顺序遍历；
//   5. 渠道级开关来源：参数中心清单 + 本地 localStorage 覆盖优先；
//   6. rescan 间隔三档（覆盖值 > 未读优先档 30min > 现行档 10min）+ 开关翻转重排；
//   7. capabilities 下发字段的解析容错（getServerCapabilities）。
// 真机口径（徽章检出率/漏检率对比）只能在真机量化，见任务报告。
import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest';
import { BaseAdapter } from '../src/core/channel-adapter.js';
import { SENDER, CHANNELS, RESCAN_DEFAULTS } from '../src/core/types.js';
import { getServerCapabilities } from '../src/core/polling-loop.js';
import { getConversationList as douyinConvList, detectUnread as douyinDetectUnread } from '../src/channels/douyin.js';
import { getConversationList as bilibiliConvList, detectUnread as bilibiliDetectUnread } from '../src/channels/bilibili.js';

beforeEach(() => {
  document.body.innerHTML = '';
  localStorage.clear();
});

afterEach(() => {
  delete globalThis.fetch;
});

// 让 jsdom 元素有可见性（真实浏览器 offsetParent 由布局决定；jsdom 恒为 null）
function makeVisible(el) {
  Object.defineProperty(el, 'offsetParent', { configurable: true, get: () => ({}) });
  return el;
}

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

function makeConv(id, name, { unread = false, wired = true } = {}) {
  const el = makeVisible(document.createElement('button'));
  el.setAttribute('data-e2e', 'conversation-item');
  // wired=false 模拟「渠道未接线 unread」的列表项（字段整体缺失）
  return wired ? { id, name, el, unread } : { id, name, el };
}

// 与 patrol.test.js 同构的适配器：点击会话项后活动会话变为该会话
function buildAdapter(convList, messagesByConv, { channel = CHANNELS.DOUYIN } = {}) {
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
  const adapter = new BaseAdapter({ name: 'test', channel, SEL: {}, hooks });
  convList.forEach((conv) => {
    conv.el.addEventListener('click', () => { current = conv.id; });
  });
  return { adapter, getActive: () => current };
}

describe('M-5 patrol 未读优先调度', () => {
  it('开关开启：只访问带未读徽章的会话（10 会话 2 未读 → visited=2，对照顺序模式 visited=6）', async () => {
    const convList = [];
    for (let i = 0; i < 10; i++) convList.push(makeConv(`c${i}`, `客户${i}`, { unread: i < 2 }));
    const messagesByConv = { c0: [makeMessage('m0', '新消息0')], c1: [makeMessage('m1', '新消息1')] };
    const { adapter } = buildAdapter(convList, messagesByConv);
    adapter.setPatrolUnreadFirstChannels([CHANNELS.DOUYIN]);
    const onInbound = vi.fn();
    adapter.callbacks.onMessage = onInbound;

    const r = await adapter.patrol({ throttleMs: 1, waitActiveMs: 200, switchMinMs: 0, switchMaxMs: 0, maxPerRound: 6 });
    // 量化对比：顺序模式同列表 visited=min(10,6)=6；未读优先 visited=2
    expect(r.scheduleMode).toBe('unread-first');
    expect(r.scannedTotal).toBe(10);
    expect(r.unreadCount).toBe(2);
    expect(r.visited).toBe(2);
    expect(r.visited).toBeLessThan(6);
    // 只点了未读会话
    const convIds = onInbound.mock.calls.map((c) => c[0].conversation_id);
    expect(convIds).toEqual(expect.arrayContaining(['c0', 'c1']));
    expect(convIds).toHaveLength(2);
  });

  it('开关开启且无未读：本轮 0 访问（未读为 0 的会话不再进全量遍历）', async () => {
    const convList = [makeConv('a1', '张三', { unread: false }), makeConv('b2', '李四', { unread: false })];
    const messagesByConv = { a1: [makeMessage('m-a1', '问候')], b2: [makeMessage('m-b2', '咨询')] };
    const { adapter } = buildAdapter(convList, messagesByConv);
    adapter.setPatrolUnreadFirstChannels([CHANNELS.DOUYIN]);
    const onInbound = vi.fn();
    adapter.callbacks.onMessage = onInbound;

    const r = await adapter.patrol({ throttleMs: 1, waitActiveMs: 200, switchMinMs: 0, switchMaxMs: 0 });
    expect(r.scheduleMode).toBe('unread-first');
    expect(r.visited).toBe(0);
    expect(r.captured).toBe(0);
    expect(onInbound).not.toHaveBeenCalled();
  });

  it('开关开启：未读数超过 maxPerRound 时按配额截断（取列表顺序前 N 个未读）', async () => {
    const convList = [];
    for (let i = 0; i < 8; i++) convList.push(makeConv(`u${i}`, `未读${i}`, { unread: true }));
    const { adapter } = buildAdapter(convList, {});
    adapter.setPatrolUnreadFirstChannels([CHANNELS.DOUYIN]);

    const r = await adapter.patrol({ throttleMs: 1, waitActiveMs: 200, switchMinMs: 0, switchMaxMs: 0, maxPerRound: 3 });
    expect(r.scheduleMode).toBe('unread-first');
    expect(r.unreadCount).toBe(8);
    expect(r.visited).toBe(3);
  });

  it('开关关闭：完全现行行为——全量顺序遍历（unread=true 混合列表照样全访问）', async () => {
    const convList = [
      makeConv('a1', '张三', { unread: true }),
      makeConv('b2', '李四', { unread: false }),
      makeConv('c3', '王五', { unread: false }),
    ];
    const messagesByConv = {
      a1: [makeMessage('m-a1', '你好')],
      b2: [makeMessage('m-b2', '在吗')],
      c3: [makeMessage('m-c3', '已读消息')],
    };
    const { adapter } = buildAdapter(convList, messagesByConv);
    // 不设置开关（参数中心清单为空 + 无本地覆盖）→ 关
    const onInbound = vi.fn();
    adapter.callbacks.onMessage = onInbound;

    const r = await adapter.patrol({ throttleMs: 1, waitActiveMs: 200, switchMinMs: 0, switchMaxMs: 0 });
    expect(r.scheduleMode).toBe('sequential');
    expect(r.visited).toBe(3);
    expect(r.captured).toBe(3);
    expect(onInbound).toHaveBeenCalledTimes(3);
  });

  it('fail-open：列表项缺 unread 布尔字段（渠道未接线）→ 本轮回退顺序遍历', async () => {
    const convList = [
      makeConv('a1', '张三', { wired: false }),
      makeConv('b2', '李四', { wired: false }),
    ];
    const messagesByConv = { a1: [makeMessage('m-a1', '问候')], b2: [makeMessage('m-b2', '咨询')] };
    const { adapter } = buildAdapter(convList, messagesByConv);
    adapter.setPatrolUnreadFirstChannels([CHANNELS.DOUYIN]);
    const onInbound = vi.fn();
    adapter.callbacks.onMessage = onInbound;

    const r = await adapter.patrol({ throttleMs: 1, waitActiveMs: 200, switchMinMs: 0, switchMaxMs: 0 });
    expect(r.scheduleMode).toBe('unread-first-fallback');
    expect(r.visited).toBe(2);
    expect(onInbound).toHaveBeenCalledTimes(2);
  });

  it('fail-open：unread 字段为非布尔（异常返回）→ 本轮回退顺序遍历', async () => {
    const convList = [
      { id: 'a1', name: '张三', el: makeVisible(document.createElement('button')), unread: 1 },
      { id: 'b2', name: '李四', el: makeVisible(document.createElement('button')), unread: 'yes' },
    ];
    const messagesByConv = { a1: [makeMessage('m-a1', '问候')], b2: [makeMessage('m-b2', '咨询')] };
    const { adapter } = buildAdapter(convList, messagesByConv);
    adapter.setPatrolUnreadFirstChannels([CHANNELS.DOUYIN]);
    const onInbound = vi.fn();
    adapter.callbacks.onMessage = onInbound;

    const r = await adapter.patrol({ throttleMs: 1, waitActiveMs: 200, switchMinMs: 0, switchMaxMs: 0 });
    expect(r.scheduleMode).toBe('unread-first-fallback');
    expect(r.visited).toBe(2);
  });

  it('visitAllWhenNoUnread 兼容：未读优先 + 无未读 + 选项开启 → 仍全量访问', async () => {
    const convList = [makeConv('a1', '张三', { unread: false }), makeConv('b2', '李四', { unread: false })];
    const messagesByConv = { a1: [makeMessage('m-a1', '问候')], b2: [makeMessage('m-b2', '咨询')] };
    const { adapter } = buildAdapter(convList, messagesByConv);
    adapter.setPatrolUnreadFirstChannels([CHANNELS.DOUYIN]);
    const onInbound = vi.fn();
    adapter.callbacks.onMessage = onInbound;

    const r = await adapter.patrol({ throttleMs: 1, waitActiveMs: 200, switchMinMs: 0, switchMaxMs: 0, visitAllWhenNoUnread: true });
    expect(r.scheduleMode).toBe('unread-first');
    expect(r.visited).toBe(2);
    expect(onInbound).toHaveBeenCalledTimes(2);
  });

  it('scanOnly 返回真实未读数（修复 unreadCount 恒为 0）', async () => {
    const convList = [
      makeConv('a1', '张三', { unread: true }),
      makeConv('b2', '李四', { unread: true }),
      makeConv('c3', '王五', { unread: false }),
    ];
    const { adapter } = buildAdapter(convList, {});
    const r = await adapter.patrol({ scanOnly: true });
    expect(r.scannedTotal).toBe(3);
    expect(r.unreadCount).toBe(2);
    expect(r.visited).toBe(0);
  });

  it('统计面：patrolStatus 暴露 lastScheduleMode / lastUnreadCount', async () => {
    const convList = [makeConv('a1', '张三', { unread: true })];
    const messagesByConv = { a1: [makeMessage('m-a1', '你好')] };
    const { adapter } = buildAdapter(convList, messagesByConv);
    adapter.setPatrolUnreadFirstChannels([CHANNELS.DOUYIN]);
    await adapter.patrol({ throttleMs: 1, waitActiveMs: 200, switchMinMs: 0, switchMaxMs: 0 });
    const s = adapter.patrolStatus();
    expect(s.lastScheduleMode).toBe('unread-first');
    expect(s.lastUnreadCount).toBe(1);
  });
});

describe('M-5 渠道级开关来源与优先级', () => {
  it('参数中心清单按渠道生效：清单内开、清单外关', async () => {
    const douyin = buildAdapter([makeConv('a1', '张三', { unread: false })], {});
    const xhs = buildAdapter([makeConv('a1', '张三', { unread: false })], {}, { channel: CHANNELS.XHS });
    douyin.adapter.setPatrolUnreadFirstChannels(['douyin', 'tiktok']);
    xhs.adapter.setPatrolUnreadFirstChannels(['douyin', 'tiktok']);
    expect(douyin.adapter._patrolUnreadFirstEnabled()).toBe(true);
    expect(xhs.adapter._patrolUnreadFirstEnabled()).toBe(false);   // xiaohongshu 徽章不稳定，默认关
  });

  it('非数组/含非字符串的清单被清洗（不抛错，仅保留合法渠道名）', () => {
    const { adapter } = buildAdapter([], {});
    expect(adapter.setPatrolUnreadFirstChannels(null)).toBe(false);
    expect(adapter.setPatrolUnreadFirstChannels('douyin')).toBe(false);
    expect(adapter.setPatrolUnreadFirstChannels(['douyin', 42, '', 'xhs'])).toBe(true);
    expect(adapter._patrolUnreadFirstEnabled()).toBe(true);
    expect(adapter._unreadFirstChannels.has('douyin')).toBe(true);
    expect(adapter._unreadFirstChannels.has('xhs')).toBe(true);
    expect(adapter._unreadFirstChannels.has('')).toBe(false);
  });

  it('本地覆盖优先于参数中心清单："0" 强制关、"1" 强制开', () => {
    const { adapter } = buildAdapter([], {});
    adapter.setPatrolUnreadFirstChannels(['douyin']);
    localStorage.setItem(`hivebridge:patrolUnreadFirst:${CHANNELS.DOUYIN}`, '0');
    expect(adapter._patrolUnreadFirstEnabled()).toBe(false);
    localStorage.setItem(`hivebridge:patrolUnreadFirst:${CHANNELS.DOUYIN}`, '1');
    expect(adapter._patrolUnreadFirstEnabled()).toBe(true);
    // 清单不含 douyin 时 "1" 也能强制开（应急逃生口）
    adapter.setPatrolUnreadFirstChannels([]);
    expect(adapter._patrolUnreadFirstEnabled()).toBe(true);
    localStorage.removeItem(`hivebridge:patrolUnreadFirst:${CHANNELS.DOUYIN}`);
    expect(adapter._patrolUnreadFirstEnabled()).toBe(false);
  });
});

describe('M-5 全量 rescan 降频与间隔配置化', () => {
  it('间隔三档：覆盖值 > 未读优先档 30min > 现行档 10min', () => {
    const { adapter } = buildAdapter([], {});
    // 开关关 = 现行 10min（行为不变）
    expect(adapter._rescanIntervalMs()).toBe(RESCAN_DEFAULTS.intervalMs);
    expect(RESCAN_DEFAULTS.intervalMs).toBe(10 * 60 * 1000);
    // 开关开 = 降频 30min
    adapter.setPatrolUnreadFirstChannels(['douyin']);
    expect(adapter._rescanIntervalMs()).toBe(RESCAN_DEFAULTS.intervalMsUnreadFirst);
    expect(RESCAN_DEFAULTS.intervalMsUnreadFirst).toBe(30 * 60 * 1000);
    // localStorage 覆盖优先于两档
    localStorage.setItem(`hivebridge:rescan:${CHANNELS.DOUYIN}`, '60000');
    expect(adapter._rescanIntervalMs()).toBe(60000);
    localStorage.removeItem(`hivebridge:rescan:${CHANNELS.DOUYIN}`);
  });

  it('开关翻转且 rescan 已在跑 → 重排间隔（新清单生效即按新模式取值）', () => {
    const { adapter } = buildAdapter([], {});
    adapter._startRescan();
    expect(adapter._rescanTimer).not.toBeNull();
    const spy = vi.spyOn(adapter, '_startRescan');
    adapter.setPatrolUnreadFirstChannels(['douyin']);
    expect(spy).toHaveBeenCalledTimes(1);
    expect(adapter._rescanIntervalMs()).toBe(RESCAN_DEFAULTS.intervalMsUnreadFirst);
    spy.mockRestore();
    adapter.stop();
  });

  it('开关未翻转（重复下发同一清单）不重排 rescan', () => {
    const { adapter } = buildAdapter([], {});
    adapter.setPatrolUnreadFirstChannels(['douyin']);
    adapter._startRescan();
    const spy = vi.spyOn(adapter, '_startRescan');
    adapter.setPatrolUnreadFirstChannels(['douyin']);
    expect(spy).not.toHaveBeenCalled();
    spy.mockRestore();
    adapter.stop();
  });
});

describe('M-5 渠道 detectUnread 接线（选择器口径 = 各渠道既有 SEL.UNREAD）', () => {
  function addDouyinConv(wrapper, id, name, { unread = false } = {}) {
    const item = makeVisible(document.createElement('div'));
    item.setAttribute('data-e2e', 'conversation-item');
    const a = document.createElement('a');
    a.href = `/user/${id}`;
    item.appendChild(a);
    const nameEl = document.createElement('div');
    nameEl.className = 'name';
    nameEl.textContent = name;
    item.appendChild(nameEl);
    if (unread) {
      const badge = document.createElement('span');
      badge.className = 'unread-badge';
      badge.textContent = '3';
      item.appendChild(badge);
    }
    wrapper.appendChild(item);
  }

  it('douyin：getConversationList 带 unread 字段且 detectUnread 命中徽章', () => {
    const wrapper = document.createElement('div');
    wrapper.className = 'conversationConversationListwrapper';
    addDouyinConv(wrapper, 'MS4wA1', '张三', { unread: true });
    addDouyinConv(wrapper, 'MS4wB2', '李四', { unread: false });
    document.body.appendChild(wrapper);
    const list = douyinConvList();
    expect(list).toHaveLength(2);
    expect(list[0].unread).toBe(true);
    expect(list[1].unread).toBe(false);
    expect(douyinDetectUnread(list[0].el)).toBe(true);
    expect(douyinDetectUnread(list[1].el)).toBe(false);
  });

  it('bilibili：getConversationList 带 unread 字段且 detectUnread 命中未读 class（真机校准缺口渠道）', () => {
    // fixture 口径与 channel-bilibili.test.js 一致：.list-container > .list-item，
    // 未读徽章为 .notify.notify-number（SEL.UNREAD 既有口径）
    const container = document.createElement('div');
    container.className = 'list-container';
    const read = makeVisible(document.createElement('div'));
    read.className = 'list-item';
    read.innerHTML = '<span class="name">未关注人</span>';
    const unread = makeVisible(document.createElement('div'));
    unread.className = 'list-item';
    unread.innerHTML = '<span class="name">测试用户</span><div class="notify notify-number">2</div>';
    container.appendChild(read);
    container.appendChild(unread);
    document.body.appendChild(container);
    const list = bilibiliConvList();
    expect(list.length).toBe(2);
    const byEl = (el) => list.find((c) => c.el === el);
    expect(byEl(read).unread).toBe(false);
    expect(byEl(unread).unread).toBe(true);
    expect(bilibiliDetectUnread(unread)).toBe(true);
    expect(bilibiliDetectUnread(read)).toBe(false);
  });
});

describe('M-5 capabilities 下发字段解析（getServerCapabilities）', () => {
  function stubCapabilities(body, status = 200) {
    globalThis.fetch = vi.fn(async () =>
      new Response(typeof body === 'string' ? body : JSON.stringify(body), { status })
    );
  }

  it('服务端下发 JSON 数组 → 原样解析出渠道清单', async () => {
    stubCapabilities({ sse_enabled: true, poll_interval_ms: 1500, patrol_unread_first_channels: '["douyin","xhs"]' });
    const caps = await getServerCapabilities({ serverUrl: 'http://127.0.0.1:9', token: '' });
    expect(caps.patrol_unread_first_channels).toEqual(['douyin', 'xhs']);
    expect(caps.sse_enabled).toBe(true);
  });

  it('字段缺失（旧服务端）/非法 JSON/非数组 → 空清单 = 开关关（现行行为）', async () => {
    stubCapabilities({ sse_enabled: false, poll_interval_ms: 1500 });
    let caps = await getServerCapabilities({ serverUrl: 'http://127.0.0.1:9', token: '' });
    expect(caps.patrol_unread_first_channels).toEqual([]);

    stubCapabilities({ sse_enabled: false, poll_interval_ms: 1500, patrol_unread_first_channels: '{bad' });
    caps = await getServerCapabilities({ serverUrl: 'http://127.0.0.1:9', token: '' });
    expect(caps.patrol_unread_first_channels).toEqual([]);

    stubCapabilities({ sse_enabled: false, poll_interval_ms: 1500, patrol_unread_first_channels: 42 });
    caps = await getServerCapabilities({ serverUrl: 'http://127.0.0.1:9', token: '' });
    expect(caps.patrol_unread_first_channels).toEqual([]);
  });

  it('请求失败 → 空清单兜底（不阻塞下行决策）', async () => {
    globalThis.fetch = vi.fn(async () => { throw new Error('net down'); });
    const caps = await getServerCapabilities({ serverUrl: 'http://127.0.0.1:9', token: '' });
    expect(caps.sse_enabled).toBe(false);
    expect(caps.patrol_unread_first_channels).toEqual([]);
  });
});
