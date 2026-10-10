import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest';
import { BaseAdapter } from '../src/core/channel-adapter.js';
import { PATROL_DEFAULTS } from '../src/core/types.js';

// vitest5 jsdom 环境：global.document 是只读 getter，须用 defineProperty 覆盖
Object.defineProperty(globalThis, 'document', {
  value: {
    body: { tagName: 'BODY' },
    querySelector: () => null,
    querySelectorAll: () => [],
    createElement: () => ({ tagName: 'DIV', children: [], classList: { add: () => {}, contains: () => false } }),
  },
  writable: true,
  configurable: true,
});

// vitest5 jsdom 环境：MutationObserver 同样是只读全局，读改走 defineProperty
function withMockMutationObserver(mock, fn) {
  const OrigMO = global.MutationObserver;
  Object.defineProperty(globalThis, 'MutationObserver', { value: mock, writable: true, configurable: true });
  try {
    fn();
  } finally {
    Object.defineProperty(globalThis, 'MutationObserver', { value: OrigMO, writable: true, configurable: true });
  }
}

describe('OOM 巡检 修复', () => {
  it('MutationObserver 不再监听 characterData（防抖屏 OOM）', () => {
    // 验证 _observe 不会传 characterData:true
    const observed = {};
    withMockMutationObserver(class {
      observe(_root, opts) { Object.assign(observed, opts); }
      disconnect() {}
    }, () => {
      const fakeRoot = document.createElement('div');
      const adapter = new BaseAdapter({ name: 'test', channel: 'xiaohongshu', hooks: { getMessageListRoot: () => fakeRoot } });
      adapter._observe(fakeRoot);
      expect('characterData' in observed).toBe(false);
      expect(observed.childList).toBe(true);
      expect(observed.subtree).toBe(true);
    });
  });

  it('内容指纹去重已移至后端（纯桥接：adapter 不再维护 seen Set）', () => {
    // 2026-08-05 架构重构：bridge 只做桥接，内容去重交给后端统一收信中心。
    // adapter.seen Set 已移除，仅保留 seenNodes WeakSet（防 DOM 重复扫描的技术手段）。
    const adapter = new BaseAdapter({ name: 'test', channel: 'xiaohongshu' });
    expect(adapter.seen).toBeUndefined();
    expect(adapter.seenNodes).toBeInstanceOf(WeakSet);
  });

  it('_collectUnseenText 单次封顶 80 条（防 OOM）', async () => {
    // 构造 200 个未 seen 的消息项
    const items = [];
    for (let i = 0; i < 200; i++) {
      items.push({
        textContent: `msg${i}`,
        matches: () => false,
        closest: () => null,
        getAttribute: () => null,
        querySelector: () => null,
        outerHTML: `<div>msg${i}</div>`,
      });
    }
    const adapter = new BaseAdapter({
      name: 'test',
      channel: 'xiaohongshu',
      hooks: {
        getMessageItems: () => items,
        parseMessageItem: (it) => ({
          message_id: `m${it.textContent}`,
          text: it.textContent,
          msg_type: 'text',
          sender_type: 'customer',
          timestamp: Date.now(),
        }),
        getConversationId: () => 'cid',
      },
    });
    adapter._lastPatrolAt = Date.now() - 30 * 1000;
    const batch = adapter._collectUnseenText();
    expect(batch.length).toBe(80);
    expect(batch[0].text).toBe('msg0');
    expect(batch[79].text).toBe('msg79');
  });

  it('_collectUnseenText 首次 patrol 限速 20 条（P1-3 修复）', async () => {
    // 验证 P1-3 修复：首次 patrol 单会话单次最多抓 20 条,避免 20 条消息同时涌入后端。
    const items = [];
    for (let i = 0; i < 100; i++) {
      items.push({
        textContent: `msg${i}`,
        matches: () => false,
        closest: () => null,
        getAttribute: () => null,
        querySelector: () => null,
        outerHTML: `<div>msg${i}</div>`,
      });
    }
    const adapter = new BaseAdapter({
      name: 'test',
      channel: 'xiaohongshu',
      hooks: {
        getMessageItems: () => items,
        parseMessageItem: (it) => ({
          message_id: `m${it.textContent}`,
          text: it.textContent,
          msg_type: 'text',
          sender_type: 'customer',
          timestamp: Date.now(),
        }),
        getConversationId: () => 'cid',
      },
    });
    expect(adapter._isFirstPatrolRun()).toBe(true);
    const batch = adapter._collectUnseenText();
    expect(batch.length).toBe(20);
  });
});

describe('巡检机制优化', () => {
  let adapter;
  beforeEach(() => {
    adapter = new BaseAdapter({
      name: 'test',
      channel: 'xiaohongshu',
      hooks: {
        getConversationList: () => [
          { id: 'c1', name: '客户1', unread: true },
          { id: 'c2', name: '客户2', unread: true },
          { id: 'c3', name: '客户3', unread: false },
          { id: 'c4', name: '客户4', unread: true },
          { id: 'c5', name: '客户5', unread: false },
        ],
        getConversationId: () => 'c0', 
        getMessageItems: () => [],
        parseMessageItem: () => null,
        openConversation: vi.fn(async (id) => ({ id })),
        _findListScroller: () => null,
      },
    });
    adapter._patrolOpts = { intervalMs: 60000, throttleMs: 100, waitActiveMs: 100, maxPerRound: 0, scrollLoadMs: 100, maxPasses: 1, visitAllWhenNoUnread: false, switchMinMs: 0, switchMaxMs: 0 };
  });

  it('scanOnly 模式：仅扫描，不点开任何会话', async () => {
    const result = await adapter.patrol({ ...adapter._patrolOpts, scanOnly: true });
    expect(result.scannedTotal).toBe(5);
    expect(result.visited).toBe(0);
    expect(adapter.hooks.openConversation).not.toHaveBeenCalled();
  });

  it('默认模式：遍历所有会话（2026-08-05 修复：不再过滤未读，直接遍历）', async () => {
    adapter._patrolVisit = vi.fn(async (conv) => ({ ok: true, newCount: 0 }));
    const result = await adapter.patrol(adapter._patrolOpts);
    expect(result.scannedTotal).toBe(5);
    expect(adapter._patrolVisit).toHaveBeenCalledTimes(5);
  });

  it('用户已停留的会话：自动跳过（不重复点开）', async () => {
    adapter.hooks.getConversationId = () => 'c2';
    adapter._patrolVisit = vi.fn(async (conv) => ({ ok: true, newCount: 0 }));
    await adapter.patrol(adapter._patrolOpts);
    // 巡检遍历所有会话，c2 被跳过（用户已停留）
    const calledIds = adapter._patrolVisit.mock.calls.map((c) => c[0].id);
    expect(calledIds).toContain('c1');
    expect(calledIds).toContain('c4');
    expect(calledIds).not.toContain('c2');
  });

  it('无 unread 时也遍历所有会话（2026-08-05 修复：不再跳过本轮）', async () => {
    adapter.hooks.getConversationList = () => [
      { id: 'c1', unread: false },
      { id: 'c2', unread: false },
    ];
    adapter._patrolVisit = vi.fn(async () => ({ ok: true, newCount: 0 }));
    const result = await adapter.patrol(adapter._patrolOpts);
    expect(result.visited).toBe(2);
    expect(adapter._patrolVisit).toHaveBeenCalledTimes(2);
  });
});

describe('抖音选择器（2026-08-06：自他判定已移交后端）', () => {
  it('MSG_ITEM 仍覆盖显式自/他 class（仅用于元素选取，不再用于 self/other 判定）', async () => {
    const { SEL } = await import('../src/channels/douyin.js');
    expect(SEL.MSG_ITEM).toContain('chatMessageItemSelf');
    expect(SEL.MSG_ITEM).toContain('chatMessageItemOther');
    expect(SEL.SELF_ITEM).toBeUndefined();
    expect(SEL.OTHER_ITEM).toBeUndefined();
  });
});

