import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { BaseAdapter } from '../src/core/channel-adapter.js';
import { CHANNELS, SENDER, DIRECTION, FRAME } from '../src/core/types.js';
import { buildDouyinAdapter } from '../src/channels/douyin.js';
import { buildXhsAdapter } from '../src/channels/xhs.js';
import { buildTiktokAdapter } from '../src/channels/tiktok.js';

// jsdom 提供 window / document / MutationObserver；location.hostname 需手动 mock
function mockLocation(hostname) {
  Object.defineProperty(window, 'location', {
    value: { hostname, href: `https://${hostname}/`, search: '', pathname: '/' },
    writable: true,
  });
}

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

describe('B1 回归：三渠道 buildXxxAdapter 构造正确', () => {
  it('buildDouyinAdapter 字段完整', () => {
    const a = buildDouyinAdapter();
    expect(a.channel).toBe(CHANNELS.DOUYIN);
    expect(a.name).toBe('douyin');
    expect(a.SEL).toBeTruthy();
    expect(typeof a.hooks.match).toBe('function');
    expect(typeof a.hooks.sendText).toBe('function');
    expect(typeof a.hooks.parseMessageItem).toBe('function');
    expect(typeof a.getMessageItems).toBe('function');
  });

  it('buildXhsAdapter 字段完整', () => {
    const a = buildXhsAdapter();
    expect(a.channel).toBe(CHANNELS.XHS);
    expect(a.name).toBe('xhs');
    expect(a.SEL.INPUT).toContain('#jarvis-reply-textarea');
    expect(a.SEL.INPUT).toContain('xhs-im-input-bar-editor');
  });

  it('buildTiktokAdapter 字段完整', () => {
    const a = buildTiktokAdapter();
    expect(a.channel).toBe(CHANNELS.TIKTOK);
    expect(a.name).toBe('tiktok');
    expect(Array.isArray(a.SEL.MSG_ITEM ? [a.SEL.MSG_ITEM] : null)).toBe(true);
  });

  it('match() 调用 hooks.match（mock location 后返回 true）', () => {
    mockLocation('douyin.com');
    // mock 输入框存在
    const input = document.createElement('div');
    input.setAttribute('contenteditable', 'true');
    input.setAttribute('role', 'textbox');
    document.body.appendChild(input);
    const a = buildDouyinAdapter();
    expect(a.match()).toBe(true);
    document.body.removeChild(input);
  });
});

describe('BaseAdapter hooks 透传', () => {
  function makeAdapter(hooks) {
    return new BaseAdapter({
      name: 'test',
      channel: 'douyin_web',
      SEL: { MSG_ITEM: '.msg' },
      hooks,
    });
  }

  it('getMessageItems 委托 hooks.getMessageItems', () => {
    const adapter = makeAdapter({ getMessageItems: () => ['a', 'b'] });
    expect(adapter.getMessageItems()).toEqual(['a', 'b']);
  });

  it('getAccountId 优先用 hooks.getAccountId，无则用 this.account', () => {
    const adapter = makeAdapter({ getAccountId: () => 'acc-1' });
    expect(adapter.getAccountId()).toBe('acc-1');
    const adapter2 = makeAdapter({});
    adapter2.setAccount('fallback');
    expect(adapter2.getAccountId()).toBe('fallback');
  });

  it('getConversationId 优先用 hooks.getConversationId', () => {
    const adapter = makeAdapter({ getConversationId: () => 'conv-9' });
    expect(adapter.getConversationId()).toBe('conv-9');
  });
});

describe('BaseAdapter sendOutbound 风控通过则回写并上报 outbound 消息', () => {
  it('调用 hooks.sendText 并触发 onMessage（sender_type=AGENT）', async () => {
    const sendText = vi.fn().mockResolvedValue(undefined);
    const adapter = new BaseAdapter({
      name: 'test',
      channel: 'douyin_web',
      SEL: {},
      hooks: {
        match: () => true,
        getAccountId: () => 'acc1',
        getConversationId: () => 'conv1',
        sendText,
      },
    });
    const onMessage = vi.fn();
    adapter.start({ onMessage });
    // sendOutbound 内有 sleep(waitHintMs) 用于拟人延迟，fake timers 下需异步推进
    // 批24：回查预算必须显式关掉 —— 这里的假时钟只 fake setTimeout、不 fake Date.now()，
    // 而回查的到期判断读的是 Date.now()：虚拟推进烧不掉真实预算，`await p` 会挂到用例超时。
    const p = adapter.sendOutbound('hello', undefined, { sendVerifyMs: 0 });
    await vi.advanceTimersByTimeAsync(4000); 
    const ok = await p;
    expect(ok.ok).toBe(true);
    expect(sendText).toHaveBeenCalledWith('hello');
    expect(onMessage).toHaveBeenCalled();
    const msg = onMessage.mock.calls[0][0];
    expect(msg.sender_type).toBe(SENDER.AGENT);
    expect(msg.content).toBe('hello');
  });

  it('空内容直接跳过，不调用 sendText', async () => {
    const sendText = vi.fn();
    const adapter = new BaseAdapter({
      name: 'test',
      channel: 'xhs_web',
      SEL: {},
      hooks: { match: () => true, getAccountId: () => 'a', getConversationId: () => 'c', sendText },
    });
    adapter.start({});
    const ok = await adapter.sendOutbound('');
    expect(ok.ok).toBe(false);
    expect(sendText).not.toHaveBeenCalled();
  });
});

describe('BaseAdapter _handleIncremental 统一走 onMessage', () => {
  it('CUSTOMER 消息 → onMessage（sender_type=CUSTOMER）', () => {
    const adapter = new BaseAdapter({
      name: 'test',
      channel: 'douyin_web',
      SEL: {},
      hooks: {
        match: () => true,
        getAccountId: () => 'a',
        getConversationId: () => 'c',
        getMessageItems: () => [],
        parseMessageItem: () => ({ sender_type: SENDER.CUSTOMER, text: 'hi', message_id: 'm1', timestamp: 1 }),
      },
    });
    const onMessage = vi.fn();
    adapter.start({ onMessage });
    adapter._handleIncremental({});
    expect(onMessage).toHaveBeenCalled();
    const msg = onMessage.mock.calls[0][0];
    expect(msg.content).toBe('hi');
    expect(msg.channel).toBe('douyin_web');
    expect(msg.sender_type).toBe(SENDER.CUSTOMER);
  });

  it('SELF 消息 → onMessage（sender_type=SELF）', () => {
    const adapter = new BaseAdapter({
      name: 'test',
      channel: 'xhs_web',
      SEL: {},
      hooks: {
        match: () => true,
        getAccountId: () => 'a',
        getConversationId: () => 'c',
        parseMessageItem: () => ({ sender_type: SENDER.SELF, text: 'me too', message_id: 'm2', timestamp: 2 }),
      },
    });
    const onMessage = vi.fn();
    adapter.start({ onMessage });
    adapter._handleIncremental({});
    expect(onMessage).toHaveBeenCalled();
    const msg = onMessage.mock.calls[0][0];
    expect(msg.sender_type).toBe(SENDER.SELF);
  });

  it('稳定键去重：相同内容跨 DOM 重渲染只上行一次（seenNodes 失效也不重复）', () => {
    const adapter = new BaseAdapter({
      name: 'test',
      channel: 'tiktok_web',
      SEL: {},
      hooks: {
        match: () => true,
        getAccountId: () => 'a',
        getConversationId: () => 'c',
        parseMessageItem: () => ({ sender_type: SENDER.CUSTOMER, text: 'dup', message_id: 'm3', timestamp: 3 }),
      },
    });
    const onMessage = vi.fn();
    adapter.start({ onMessage });
    adapter._handleIncremental({});
    adapter._handleIncremental({});
    expect(onMessage).toHaveBeenCalledTimes(1);
  });

  it('seenNodes 节点级去重仍生效：同一 DOM 节点重复调用不重复处理', () => {
    const adapter = new BaseAdapter({
      name: 'test',
      channel: 'tiktok_web',
      SEL: {},
      hooks: {
        match: () => true,
        getAccountId: () => 'a',
        getConversationId: () => 'c',
        parseMessageItem: () => ({ sender_type: SENDER.CUSTOMER, text: 'dup', message_id: 'm3', timestamp: 3 }),
      },
    });
    const onMessage = vi.fn();
    adapter.start({ onMessage });
    const sameNode = {};
    adapter._handleIncremental(sameNode);
    adapter._handleIncremental(sameNode);
    expect(onMessage).toHaveBeenCalledTimes(1); 
  });

  it('不同内容正常上行（稳定键按文本区分，不误删）', () => {
    let n = 0;
    const adapter = new BaseAdapter({
      name: 'test',
      channel: 'tiktok_web',
      SEL: {},
      hooks: {
        match: () => true,
        getAccountId: () => 'a',
        getConversationId: () => 'c',
        parseMessageItem: () => {
          const text = 'msg' + n++;
          return { sender_type: SENDER.CUSTOMER, text, message_id: 'm' + n, timestamp: 3 };
        },
      },
    });
    const onMessage = vi.fn();
    adapter.start({ onMessage });
    adapter._handleIncremental({});
    adapter._handleIncremental({});
    expect(onMessage).toHaveBeenCalledTimes(2); 
  });
});

describe('BaseAdapter 会话切换重挂载', () => {
  it('conversationId 变化触发 _attachConversation 重新挂载', () => {
    let currentConv = 'c1';
    const attachSpy = vi.spyOn(BaseAdapter.prototype, '_attachConversation');
    const adapter = new BaseAdapter({
      name: 'test',
      channel: 'douyin_web',
      SEL: {},
      hooks: {
        match: () => true,
        getAccountId: () => 'a',
        getConversationId: () => currentConv,
        getMessageRoot: () => null,
        getMessageItems: () => [],
      },
    });
    adapter.start({});
    attachSpy.mockClear();
    currentConv = 'c2';
    vi.advanceTimersByTime(2000); 
    expect(attachSpy).not.toHaveBeenCalled();
    vi.advanceTimersByTime(2000); 
    expect(attachSpy).toHaveBeenCalled();
  });
});

describe('渠道 adapter fallback 匹配（平台改版 / 严格选择器失效）', () => {
  beforeEach(() => {
    document.body.innerHTML = '';
  });

  function mockLocationPath(href) {
    Object.defineProperty(window, 'location', {
      value: { hostname: new URL(href).hostname, href, search: new URL(href).search, pathname: new URL(href).pathname },
      writable: true,
      configurable: true,
    });
  }

  function addInput({ tag = 'div', attrs = {}, rect = { width: 200, height: 40, top: 100, left: 0, right: 200, bottom: 140 } } = {}) {
    const el = document.createElement(tag);
    for (const [k, v] of Object.entries(attrs)) el.setAttribute(k, v);
    el.getBoundingClientRect = () => ({ ...rect, x: rect.left, y: rect.top });
    Object.defineProperty(el, 'offsetParent', { configurable: true, get: () => ({}) });
    document.body.appendChild(el);
    return el;
  }

  function addContextHint() {
    // 模拟"页面像私信页"的 DOM 线索
    const hint = document.createElement('div');
    hint.className = 'chat-message-list';
    document.body.appendChild(hint);
  }

  it('抖音：strict 选择器存在 → match=true, matchMode=strict', () => {
    mockLocationPath('https://www.douyin.com/im/chat/');
    addInput({ tag: 'div', attrs: { contenteditable: 'true', role: 'textbox' } });
    const a = buildDouyinAdapter();
    expect(a.match()).toBe(true);
    expect(a.matchMode()).toBe('strict');
  });

  it('抖音：strict 选择器失效 + fallback 命中 → match=true, matchMode=fallback', () => {
    mockLocationPath('https://www.douyin.com/im/chat/');
    addInput({ tag: 'div', attrs: { contenteditable: 'true' }, rect: { width: 300, height: 50, top: 100, left: 0, right: 300, bottom: 150 } });
    addContextHint();
    const a = buildDouyinAdapter();
    expect(a.match()).toBe(true);
    expect(a.matchMode()).toBe('fallback');
  });

  it('抖音：strict 失效 + 页面不像私信页 → match=false', () => {
    mockLocationPath('https://www.douyin.com/');
    addInput({ tag: 'div', attrs: { contenteditable: 'true' } });
    const a = buildDouyinAdapter();
    expect(a.match()).toBe(false);
    expect(a.matchMode()).toBeNull();
  });

  it('小红书：strict 选择器失效 + fallback 命中 → match=true, matchMode=fallback', () => {
    mockLocationPath('https://www.xiaohongshu.com/im/');
    addInput({ tag: 'textarea', attrs: { placeholder: '请输入消息' }, rect: { width: 300, height: 50, top: 100, left: 0, right: 300, bottom: 150 } });
    addContextHint();
    const a = buildXhsAdapter();
    expect(a.match()).toBe(true);
    expect(a.matchMode()).toBe('fallback');
  });

  it('小红书：strict 命中 → matchMode=strict', () => {
    mockLocationPath('https://www.xiaohongshu.com/im/');
    const strict = document.createElement('textarea');
    strict.id = 'jarvis-reply-textarea';
    document.body.appendChild(strict);
    const a = buildXhsAdapter();
    expect(a.matchMode()).toBe('strict');
  });

  it('TikTok：strict INPUT_FALLBACKS 都失效 + 通用 DOM 命中 → matchMode=fallback', () => {
    mockLocationPath('https://www.tiktok.com/inbox');
    addInput({ tag: 'div', attrs: { contenteditable: 'true' }, rect: { width: 300, height: 50, top: 100, left: 0, right: 300, bottom: 150 } });
    addContextHint();
    const a = buildTiktokAdapter();
    expect(a.match()).toBe(true);
    expect(a.matchMode()).toBe('fallback');
  });

  it('非支持域名：抖音页面跳到 google.com → match=false, matchMode=null', () => {
    mockLocationPath('https://google.com/');
    addInput({ tag: 'div', attrs: { contenteditable: 'true' } });
    const a = buildDouyinAdapter();
    expect(a.match()).toBe(false);
    expect(a.matchMode()).toBeNull();
  });

  it('抖音 jingxuan 页面（评论/推荐）→ match=false, matchMode=null', () => {
    mockLocationPath('https://www.douyin.com/jingxuan');
    // 模拟 jingxuan DOM：editor-kit-container > messageEditorinputArea（评论框）
    const wrap = document.createElement('div');
    wrap.className = 'editor-kit-container';
    const ce = document.createElement('div');
    ce.setAttribute('contenteditable', 'true');
    ce.className = 'messageEditorinputArea';
    ce.getBoundingClientRect = () => ({ x: 0, y: 100, width: 300, height: 50, top: 100, left: 0, right: 300, bottom: 150 });
    Object.defineProperty(ce, 'offsetParent', { configurable: true, get: () => ({}) });
    wrap.appendChild(ce);
    document.body.appendChild(wrap);
    // 侧栏推荐列表
    const listWrap = document.createElement('div');
    listWrap.className = 'conversationConversationListwrapper';
    document.body.appendChild(listWrap);
    const a = buildDouyinAdapter();
    expect(a.match()).toBe(false);
    expect(a.matchMode()).toBeNull();
  });

  it('抖音 jingxuan 浮层私信（真实 IM DOM）→ match=true, matchMode=strict', () => {
    mockLocationPath('https://www.douyin.com/jingxuan');
    const editor = document.createElement('div');
    editor.className = 'zone-container editor-kit-container messageEditorinputArea';
    editor.setAttribute('contenteditable', 'true');
    editor.getBoundingClientRect = () => ({ x: 0, y: 400, width: 300, height: 50, top: 400, left: 0, right: 300, bottom: 450 });
    Object.defineProperty(editor, 'offsetParent', { configurable: true, get: () => ({}) });
    document.body.appendChild(editor);
    const listWrap = document.createElement('div');
    listWrap.className = 'conversationConversationListwrapper';
    const item = document.createElement('div');
    item.setAttribute('data-e2e', 'conversation-item');
    listWrap.appendChild(item);
    document.body.appendChild(listWrap);
    const a = buildDouyinAdapter();
    expect(a.match()).toBe(true);
    expect(a.matchMode()).toBe('strict');
  });

  it('抖音 /user/self 个人主页浮层私信（svg 发送按钮）→ match=true, 发送按钮/气泡可定位', () => {
    mockLocationPath('https://www.douyin.com/user/self?from_tab_name=main');
    const editor = document.createElement('div');
    editor.className = 'zone-container editor-kit-container messageEditorinputArea';
    editor.setAttribute('contenteditable', 'true');
    editor.getBoundingClientRect = () => ({ x: 0, y: 400, width: 300, height: 50, top: 400, left: 0, right: 300, bottom: 450 });
    Object.defineProperty(editor, 'offsetParent', { configurable: true, get: () => ({}) });
    document.body.appendChild(editor);
    const listWrap = document.createElement('div');
    listWrap.className = 'conversationConversationListwrapper';
    const item = document.createElement('div');
    item.setAttribute('data-e2e', 'conversation-item');
    listWrap.appendChild(item);
    document.body.appendChild(listWrap);
    // 真实发送按钮：svg.messageMsgInputpublishBtn.e2e-send-msg-btn
    const sendBtn = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    sendBtn.setAttribute('class', 'messageMsgInputpublishBtn e2e-send-msg-btn');
    document.body.appendChild(sendBtn);
    // 真实消息气泡：业务消息必须有 bubble 子结构标识，否则 isSystemMessage ⑤ 结构特征会误杀
    const bubble = document.createElement('div');
    bubble.setAttribute('data-e2e', 'msg-item-content');
    const bubbleBody = document.createElement('div');
    bubbleBody.className = 'bubble-body';
    bubbleBody.textContent = '你好，在吗？';
    bubble.appendChild(bubbleBody);
    document.body.appendChild(bubble);

    const a = buildDouyinAdapter();
    expect(a.match()).toBe(true);
    expect(a.matchMode()).toBe('strict');
    expect(document.querySelector('[class*="e2e-send-msg-btn"]')).toBeTruthy();
    expect(a.getMessageItems().length).toBe(1);
  });

  it('抖音 /im/chat/ + 严格选择器命中 → match=true, matchMode=strict', () => {
    mockLocationPath('https://www.douyin.com/im/chat/abc/');
    addInput({ tag: 'div', attrs: { contenteditable: 'true', role: 'textbox' } });
    const a = buildDouyinAdapter();
    expect(a.match()).toBe(true);
    expect(a.matchMode()).toBe('strict');
  });

  it('抖音 /im/chat/ + 严格选择器失效 + messageList + chatWindow → match=true, matchMode=fallback', () => {
    mockLocationPath('https://www.douyin.com/im/chat/abc/');
    addInput({ tag: 'div', attrs: { contenteditable: 'true' } });
    document.body.appendChild(addInput({ tag: 'div', attrs: { class: 'messageList' }, rect: { width: 200, height: 30, top: 100, left: 0, right: 200, bottom: 130 } }));
    document.body.appendChild(addInput({ tag: 'div', attrs: { class: 'chatWindow' }, rect: { width: 200, height: 30, top: 200, left: 0, right: 200, bottom: 230 } }));
    const a = buildDouyinAdapter();
    expect(a.match()).toBe(true);
    expect(a.matchMode()).toBe('fallback');
  });
});

describe('BaseAdapter matchMode 透传', () => {
  it('hooks.matchMode 返回 "fallback" → adapter.matchMode() 透传', () => {
    const a = new BaseAdapter({
      name: 'test',
      channel: 'douyin_web',
      SEL: {},
      hooks: {
        match: () => true,
        matchMode: () => 'fallback',
      },
    });
    expect(a.matchMode()).toBe('fallback');
  });

  it('hooks.matchMode 未实现 + match=true → 默认 "strict"', () => {
    const a = new BaseAdapter({
      name: 'test',
      channel: 'douyin_web',
      SEL: {},
      hooks: { match: () => true },
    });
    expect(a.matchMode()).toBe('strict');
  });

  it('match=false → matchMode=null（无论 hooks 有无实现）', () => {
    const a1 = new BaseAdapter({
      name: 'test',
      channel: 'douyin_web',
      SEL: {},
      hooks: { match: () => false, matchMode: () => 'fallback' },
    });
    expect(a1.matchMode()).toBeNull();
  });
});

