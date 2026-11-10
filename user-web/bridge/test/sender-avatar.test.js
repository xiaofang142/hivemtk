import { describe, it, expect, beforeEach } from 'vitest';
import { buildDouyinAdapter } from '../src/channels/douyin.js';
import { buildXhsAdapter } from '../src/channels/xhs.js';
import { buildTiktokAdapter } from '../src/channels/tiktok.js';
import { buildXianyuAdapter } from '../src/channels/xianyu.js';
import { buildKuaishouAdapter } from '../src/channels/kuaishou.js';
import { buildJdAdapter } from '../src/channels/jd.js';
import { buildPddAdapter } from '../src/channels/pdd.js';
import { buildBilibiliAdapter } from '../src/channels/bilibili.js';
import { buildWeiboAdapter } from '../src/channels/weibo.js';
import { BaseAdapter } from '../src/core/channel-adapter.js';
import { makeUnifiedMessage } from '../src/core/types.js';
import { Uplink } from '../src/core/uplink.js';

// M-2 客户头像链路（2026-11-11）：content script 提取 → 协议透传 → 上行白名单。
//
// 口径：sender_avatar 与 sender_name 同语义 —— 1v1 抓「对方」头像（聊天 header /
// 活动会话项），群聊/抓不到为空；消息项内头像仍走既有排除判据（不当消息图片）。
// DOM 选择器按既有渠道测试 fixture 样本口径实现，真机校准缺口见 M-2 报告。

function stubOffsetParent(el) {
  Object.defineProperty(el, 'offsetParent', { configurable: true, get: () => ({}) });
  return el;
}

beforeEach(() => {
  document.body.innerHTML = '';
});

// ── 协议层：makeUnifiedMessage / uplink 白名单 ──

describe('M-2 makeUnifiedMessage：sender_avatar 透传', () => {
  it('显式 sender_avatar 原样输出', () => {
    const msg = makeUnifiedMessage({
      channel: 'douyin',
      account_id: 'acc1',
      conversation_id: 'c1',
      sender_name: '张三',
      sender_avatar: 'https://p3-sign.douyinpic.com/av.jpg',
      content: '你好',
      timestamp: 1,
    });
    expect(msg.sender_avatar).toBe('https://p3-sign.douyinpic.com/av.jpg');
  });

  it('缺省 sender_avatar 输出空串（无头像渠道不报错）', () => {
    const msg = makeUnifiedMessage({
      channel: 'jd',
      account_id: 'acc1',
      conversation_id: 'c1',
      content: 'hi',
      timestamp: 1,
    });
    expect(msg.sender_avatar).toBe('');
  });
});

describe('M-2 uplink 白名单：sender_avatar 随 HTTP ingest 上行', () => {
  it('flush 请求体 messages[].sender_avatar 携带头像 URL', async () => {
    let captured;
    const realFetch = globalThis.fetch;
    globalThis.fetch = async (_url, init) => { captured = init.body; return new Response('{"ok":true,"ingested":[]}', { status: 200 }); };
    try {
      const uplink = new Uplink({ channel: 'douyin', getConfig: async () => ({ serverUrl: 'http://x', token: 't' }) });
      uplink.enqueue({
        channel: 'douyin', account_id: 'acc1', conversation_id: 'c1',
        sender_name: '张三', sender_avatar: 'https://f/av.jpg',
        sender_type: 'customer', content: '你好', timestamp: 1,
      });
      await uplink.flushAll();
      const body = JSON.parse(captured);
      expect(body.messages[0].sender_avatar).toBe('https://f/av.jpg');
    } finally {
      globalThis.fetch = realFetch;
    }
  });

  it('无头像消息 sender_avatar 为空串（白名单键始终显式出现）', async () => {
    let captured;
    const realFetch = globalThis.fetch;
    globalThis.fetch = async (_url, init) => { captured = init.body; return new Response('{"ok":true,"ingested":[]}', { status: 200 }); };
    try {
      const uplink = new Uplink({ channel: 'jd', getConfig: async () => ({ serverUrl: 'http://x', token: 't' }) });
      uplink.enqueue({ channel: 'jd', account_id: 'acc1', conversation_id: 'c1', sender_type: 'customer', content: 'hi', timestamp: 1 });
      await uplink.flushAll();
      const body = JSON.parse(captured);
      expect(body.messages[0].sender_avatar).toBe('');
    } finally {
      globalThis.fetch = realFetch;
    }
  });
});

describe('M-2 channel-adapter：实时帧与 history 轮次携带 sender_avatar', () => {
  function makeAdapter(hooks = {}) {
    const adapter = new BaseAdapter({
      name: 'test',
      channel: 'douyin_web',
      SEL: {},
      hooks: {
        match: () => true,
        getAccountId: () => 'acc1',
        getConversationId: () => 'conv1',
        getMessageItems: () => [],
        ...hooks,
      },
    });
    return adapter;
  }

  it('_emitMessage 顶层带 parsed.sender_avatar', () => {
    const adapter = makeAdapter();
    const captured = [];
    adapter.callbacks.onMessage = (m) => captured.push(m);
    adapter._emitMessage({ sender_type: 'customer', text: '你好', msg_type: 'text', sender_name: '张三', sender_avatar: 'https://f/av.png', timestamp: 1 }, null);
    expect(captured.length).toBe(1);
    expect(captured[0].sender_avatar).toBe('https://f/av.png');
  });

  it('_historyItem 历史轮带 parsed.sender_avatar（服务端 HistoryItem.SenderAvatar 落库）', () => {
    const adapter = makeAdapter();
    const item = adapter._historyItem({ sender_type: 'customer', text: '你好', msg_type: 'text', sender_name: '张三', sender_avatar: 'https://f/h.png', timestamp: 1 }, 'inbound');
    expect(item.sender_avatar).toBe('https://f/h.png');
  });

  it('_emitPatrolMessage 巡检帧带批量内首个非空头像', () => {
    const adapter = makeAdapter();
    const captured = [];
    adapter.callbacks.onMessage = (m) => captured.push(m);
    const batch = [
      { sender_type: 'customer', text: 'm1', msg_type: 'text', sender_name: '', sender_avatar: 'https://f/p1.png', timestamp: 1, message_id: 'mh:1' },
      { sender_type: 'customer', text: 'm2', msg_type: 'text', sender_name: '张三', sender_avatar: 'https://f/p2.png', timestamp: 2, message_id: 'mh:2' },
    ];
    adapter._emitPatrolMessage('conv1', { name: '张三' }, batch);
    expect(captured.length).toBe(1);
    expect(captured[0].sender_avatar).toBe('https://f/p2.png');
  });
});

// ── 渠道层：主力 4 渠道提取 ──

describe('M-2 douyin getPeerAvatar / parseMessageItem', () => {
  it('聊天 header 头像 → sender_avatar（绝对 URL）', () => {
    const a = buildDouyinAdapter();
    document.body.innerHTML = '<div class="chat-header"><img src="https://p3-sign.douyinpic.com/av.jpg"></div>';
    const item = document.createElement('div');
    item.innerHTML = '<span class="msg-content">你好，在吗</span>';
    const parsed = a.parseMessageItem(item);
    expect(parsed).not.toBeNull();
    expect(parsed.sender_avatar).toBe('https://p3-sign.douyinpic.com/av.jpg');
  });

  it('协议相对 src 绝对化（与 media_url 同口径）', () => {
    const a = buildDouyinAdapter();
    document.body.innerHTML = '<div class="chat-header"><img src="//p3-sign.douyinpic.com/av2.jpg"></div>';
    const item = document.createElement('div');
    item.innerHTML = '<span class="msg-content">在的</span>';
    const parsed = a.parseMessageItem(item);
    // jsdom 页面基址是 http://localhost/，协议相对地址继承 http；真实页面上为 https——
    // 断言的关键是「已补全为绝对 URL」这一语义。
    expect(parsed.sender_avatar).toBe('http://p3-sign.douyinpic.com/av2.jpg');
  });

  it('活动会话项头像兜底（header 缺失时）', () => {
    const a = buildDouyinAdapter();
    const active = document.createElement('li');
    active.className = 'curConversation';
    active.innerHTML = '<img src="https://f/conv-av.jpg">';
    stubOffsetParent(active);
    document.body.appendChild(active);
    const item = document.createElement('div');
    item.innerHTML = '<span class="msg-content">在的</span>';
    const parsed = a.parseMessageItem(item);
    expect(parsed.sender_avatar).toBe('https://f/conv-av.jpg');
  });

  it('页面无头像 → sender_avatar 为空串（不报错）', () => {
    const a = buildDouyinAdapter();
    const item = document.createElement('div');
    item.innerHTML = '<span class="msg-content">你好</span>';
    const parsed = a.parseMessageItem(item);
    expect(parsed).not.toBeNull();
    expect(parsed.sender_avatar).toBe('');
  });

  it('系统消息 sender_avatar 为空（与 sender_name 同口径）', () => {
    const a = buildDouyinAdapter();
    document.body.innerHTML = '<div class="chat-header"><img src="https://f/av.jpg"></div>';
    const item = document.createElement('div');
    item.innerHTML = '<span>你已添加对方为好友</span>';
    const parsed = a.parseMessageItem(item);
    expect(parsed).not.toBeNull();
    expect(parsed.sender_type).toBe('system');
    expect(parsed.sender_avatar).toBe('');
  });

  it('既有头像排除判据不被破坏：消息项内头像图不当图片消息', () => {
    const a = buildDouyinAdapter();
    document.body.innerHTML = '<div class="chat-header"><img src="https://f/header-av.jpg"></div>';
    const item = document.createElement('div');
    item.innerHTML = '<div class="avatar-box"><img src="https://f/bubble-av.jpg"></div><span class="msg-content">仅文本</span>';
    const parsed = a.parseMessageItem(item);
    expect(parsed.msg_type).toBe('text');
    expect(parsed.media_url).toBe('');
    // 提取与判据复用同一 DOM 读取但语义互补：sender_avatar 抓的是「对方」头像（header），
    // 不受消息项内头像排除影响。
    expect(parsed.sender_avatar).toBe('https://f/header-av.jpg');
  });
});

describe('M-2 xhs getPeerAvatar / parseMessageItem', () => {
  it('聊天 header 头像 → sender_avatar', () => {
    const a = buildXhsAdapter();
    document.body.innerHTML = '<div class="chat-window"><div class="header"><img src="https://img.xhscdn.com/av.jpg"></div></div>';
    const item = document.createElement('div');
    item.className = 'im-msg-item';
    item.innerHTML = '<div class="left"><div class="text-message">你好呀</div></div>';
    const parsed = a.parseMessageItem(item);
    expect(parsed).not.toBeNull();
    expect(parsed.sender_avatar).toBe('https://img.xhscdn.com/av.jpg');
  });

  it('活动会话项头像兜底（.sx-contact-item.active）', () => {
    const a = buildXhsAdapter();
    const active = document.createElement('div');
    active.className = 'sx-contact-item active';
    active.setAttribute('data-id', 'conv1');
    active.innerHTML = '<img src="https://img.xhscdn.com/conv-av.jpg">';
    stubOffsetParent(active);
    document.body.appendChild(active);
    const item = document.createElement('div');
    item.className = 'im-msg-item';
    item.innerHTML = '<div class="left"><div class="text-message">hi</div></div>';
    const parsed = a.parseMessageItem(item);
    expect(parsed.sender_avatar).toBe('https://img.xhscdn.com/conv-av.jpg');
  });

  it('消息项内头像不当图片消息（既有判据保持）', () => {
    const a = buildXhsAdapter();
    const item = document.createElement('div');
    item.className = 'im-msg-item';
    item.innerHTML = '<div class="left"><img class="avatar" src="https://img.xhscdn.com/av.jpg"><div class="text-message">仅文本</div></div>';
    const parsed = a.parseMessageItem(item);
    expect(parsed.msg_type).toBe('text');
    expect(parsed.media_url).toBe('');
  });
});

describe('M-2 tiktok getPeerAvatar / parseMessageItem', () => {
  it('dm-new-chatbox header 头像 → sender_avatar', () => {
    const a = buildTiktokAdapter();
    document.body.innerHTML = '<div data-e2e="dm-new-chatbox"><img src="https://p16-sign-va.tiktokcdn.com/av.jpg"></div>';
    const item = document.createElement('div');
    item.setAttribute('data-e2e', 'dm-new-chat-item');
    // 气泡结构标记：无头像/气泡结构的短文本会被既有系统消息判据（漏斗④）拦截 —— 本用例
    // 同时回归该判据不被头像提取破坏（isSystemMessage 仍以头像/气泡为判据）。
    item.innerHTML = '<div class="bubble"><div data-e2e="dm-new-message-text">hello</div></div>';
    const parsed = a.parseMessageItem(item);
    expect(parsed).not.toBeNull();
    expect(parsed.sender_type).toBe('customer');
    expect(parsed.sender_avatar).toBe('https://p16-sign-va.tiktokcdn.com/av.jpg');
  });

  it('活动会话项头像兜底（aria-selected）', () => {
    const a = buildTiktokAdapter();
    const active = document.createElement('div');
    active.setAttribute('data-e2e', 'dm-new-conversation-item');
    active.setAttribute('aria-selected', 'true');
    active.innerHTML = '<img src="https://f/conv-av.jpg">';
    document.body.appendChild(active);
    const item = document.createElement('div');
    item.setAttribute('data-e2e', 'dm-new-chat-item');
    item.innerHTML = '<div class="bubble"><div data-e2e="dm-new-message-text">hello</div></div>';
    const parsed = a.parseMessageItem(item);
    expect(parsed.sender_avatar).toBe('https://f/conv-av.jpg');
  });
});

describe('M-2 xianyu getPeerAvatar / parseMessageItem', () => {
  it('聊天 header 头像 → sender_avatar', () => {
    const a = buildXianyuAdapter();
    document.body.innerHTML = '<div class="chat-header"><img src="https://img.alicdn.com/av.jpg"></div>';
    const item = document.createElement('div');
    // msg-content 同挂 TEXT 选择器与系统消息判据的 hasBubble（漏斗④）——真实气泡具备该结构。
    item.innerHTML = '<div class="msg-content">宝贝还有吗</div>';
    const parsed = a.parseMessageItem(item);
    expect(parsed).not.toBeNull();
    expect(parsed.sender_type).toBe('customer');
    expect(parsed.sender_avatar).toBe('https://img.alicdn.com/av.jpg');
  });

  it('活动会话项头像兜底', () => {
    const a = buildXianyuAdapter();
    const active = document.createElement('div');
    active.className = 'conversation-item active';
    active.innerHTML = '<img src="https://img.alicdn.com/conv-av.jpg">';
    document.body.appendChild(active);
    const item = document.createElement('div');
    item.innerHTML = '<div class="msg-content">在吗</div>';
    const parsed = a.parseMessageItem(item);
    expect(parsed.sender_avatar).toBe('https://img.alicdn.com/conv-av.jpg');
  });
});

// ── 尽力而为渠道：抓不到保持为空，不报错 ──

describe('M-2 尽力而为渠道（kuaishou/jd/pdd/bilibili/weibo）', () => {
  it('kuaishou：无 header 头像 → 空串不报错；有 header 头像 → 提取', () => {
    const a = buildKuaishouAdapter();
    const item = document.createElement('div');
    item.innerHTML = '<div class="text-content">你好</div>';
    let parsed = a.parseMessageItem(item);
    expect(parsed).not.toBeNull();
    expect(parsed.sender_avatar).toBe('');

    document.body.innerHTML = '<div class="chat-header"><img src="https://f/ks-av.jpg"></div>';
    parsed = a.parseMessageItem(item);
    expect(parsed.sender_avatar).toBe('https://f/ks-av.jpg');
  });

  it('jd：无 header 头像 → 空串不报错', () => {
    const a = buildJdAdapter();
    const item = document.createElement('div');
    item.innerHTML = '<div class="message-text">你好</div>';
    const parsed = a.parseMessageItem(item);
    expect(parsed).not.toBeNull();
    expect(parsed.sender_avatar).toBe('');
  });

  it('pdd：.base-info 头像 → 提取；无则空串', () => {
    const a = buildPddAdapter();
    const item = document.createElement('div');
    item.innerHTML = '<div class="msg-content">你好</div>';
    let parsed = a.parseMessageItem(item);
    expect(parsed).not.toBeNull();
    expect(parsed.sender_avatar).toBe('');

    document.body.innerHTML = '<div class="base-info"><img src="https://f/pdd-av.jpg"></div>';
    parsed = a.parseMessageItem(item);
    expect(parsed.sender_avatar).toBe('https://f/pdd-av.jpg');
  });

  it('bilibili：not-me 方向带头像、is-me 方向空（与 sender_name 同口径）', () => {
    const a = buildBilibiliAdapter();
    document.body.innerHTML = '<div class="message-header"><img src="https://f/bili-av.jpg"></div>';
    const other = document.createElement('div');
    other.className = 'msg-item not-me';
    other.innerHTML = '<div class="message-content">hi</div>';
    expect(a.parseMessageItem(other).sender_avatar).toBe('https://f/bili-av.jpg');

    const mine = document.createElement('div');
    mine.className = 'msg-item';
    mine.innerHTML = '<div class="message-content is-me">hi</div>';
    expect(a.parseMessageItem(mine).sender_avatar).toBe('');
  });

  it('weibo：bubble left 方向带头像、right 方向空（与 sender_name 同口径）', () => {
    const a = buildWeiboAdapter();
    document.body.innerHTML = '<div class="chat-header"><img src="https://f/wb-av.jpg"></div>';
    const other = document.createElement('div');
    other.className = 'message-item';
    other.innerHTML = '<div class="bubble_cont left"><span class="text">hi</span></div>';
    expect(a.parseMessageItem(other).sender_avatar).toBe('https://f/wb-av.jpg');

    const mine = document.createElement('div');
    mine.className = 'message-item';
    mine.innerHTML = '<div class="bubble_cont right"><span class="text">hi</span></div>';
    expect(a.parseMessageItem(mine).sender_avatar).toBe('');
  });
});
