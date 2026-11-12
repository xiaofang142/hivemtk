// 下行流测试（2026-11-12 收口）：weibo/bilibili 此前只有 hooks 直调（sendText 单测），
// 缺「真渠道 hooks 经 BaseAdapter.sendOutbound → assertConv → 回查 send_verified →
// 自报出站」的完整下行流（send-media.test.js 同款驱动方式，G-BRIDGE 下行流口径）。
// 同文件补两渠道 BaseAdapter 构造回归（adapter.test.js 原先只含 douyin/xhs/tiktok）
// 与 weibo detectUnread 用例（patrol-unread-first.test.js 原先只含 bilibili）。
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

vi.mock('../src/core/humanize.js', () => ({
  humanDelay: async () => {},
}));

import { buildWeiboAdapter } from '../src/channels/weibo.js';
import { buildBilibiliAdapter } from '../src/channels/bilibili.js';
import { CHANNELS, SENDER } from '../src/core/types.js';

function stubLocation(url) {
  const u = new URL(url);
  vi.stubGlobal('location', {
    href: u.href,
    pathname: u.pathname,
    search: u.search,
    hash: u.hash,
    hostname: u.hostname,
    host: u.host,
  });
}

beforeEach(() => vi.useFakeTimers());
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

// ── A. 构造回归（adapter.test.js 的 B1 回归同款判据，补齐两渠道） ──────────

describe('构造回归：weibo/bilibili buildXxxAdapter 字段完整', () => {
  it('buildWeiboAdapter 字段完整', () => {
    const a = buildWeiboAdapter();
    expect(a.channel).toBe(CHANNELS.WEIBO);
    expect(a.name).toBe('weibo');
    expect(a.SEL).toBeTruthy();
    expect(typeof a.hooks.match).toBe('function');
    expect(typeof a.hooks.sendText).toBe('function');
    expect(typeof a.hooks.parseMessageItem).toBe('function');
    expect(typeof a.getMessageItems).toBe('function');
  });

  it('buildBilibiliAdapter 字段完整（含 followUser / 未关注人 tab 信号）', () => {
    const a = buildBilibiliAdapter();
    expect(a.channel).toBe(CHANNELS.BILIBILI);
    expect(a.name).toBe('bilibili');
    expect(a.SEL).toBeTruthy();
    expect(typeof a.hooks.match).toBe('function');
    expect(typeof a.hooks.sendText).toBe('function');
    expect(typeof a.hooks.followUser).toBe('function');
    expect(typeof a.getMessageItems).toBe('function');
  });
});

// ── B. weibo detectUnread（M-5）──────────────────────────────────────────

describe('weibo detectUnread（M-5 fixture 口径）', () => {
  beforeEach(() => {
    vi.unstubAllGlobals();
    localStorage.clear();
    stubLocation('https://weibo.com/chat');
  });

  it('getConversationList 带 unread 字段；未读徽章命中 / 无徽章不误判', () => {
    document.body.innerHTML = `
      <div class="chatlist-box">
        <ul class="contactslist">
          <li class="sessionlist active" data-uid="20001"><div class="name">微博好友</div></li>
          <li class="sessionlist" data-uid="20002"><div class="name">有未读的</div><span class="num fff font">2</span></li>
        </ul>
      </div>
      <div class="chat-header"><div class="friendname one-line hvhd"><div><span>微博好友</span></div></div></div>
      <ul class="chat-content"><li class="message-item"><div class="bubble_cont basi left"><p class="puretext">在吗</p></div></li></ul>
      <textarea id="webchat-textarea"></textarea>
    `;
    const a = buildWeiboAdapter();
    const list = a.getConversationList();
    expect(list.length).toBe(2);
    const byId = (id) => list.find((c) => c.id === id);
    expect(byId('20001').unread).toBe(false);
    expect(byId('20002').unread).toBe(true);
  });
});

// ── C. 完整下行流：sendOutbound → 真渠道 sendText → 回查 send_verified ────

// 平台侧渲染回声：sendText 成功后把「我们发出的气泡」补进 DOM（回查判据 =
// 可见同文本计数增加；不补则回查窗口内未见 → sendVerified=false，仅记结论位不重发）。
// 注意：buildXxxAdapter 传入的是模块级共享 hooks 对象——包装 sendText 必须**克隆**
// hooks 再赋回 adapter（直接 a.hooks.sendText=... 会变异共享对象、泄漏到后续用例）。
const WEIBO_ECHO = '<li class="message-item"><div class="bubble_cont basi right"><p class="puretext">{{T}}</p></div></li>';
const BILI_ECHO = '<div class="msg-item"><div class="message-content is-me">{{T}}</div></div>';

function wrapEchoSend(adapter, echoHTML, rootSelector) {
  const orig = adapter.hooks.sendText;
  adapter.hooks = {
    ...adapter.hooks,
    sendText: async (t, o) => {
      await orig(t, o);
      const root = document.querySelector(rootSelector);
      if (root) root.insertAdjacentHTML('beforeend', echoHTML.replaceAll('{{T}}', t));
    },
  };
}

function weiboFixture() {
  document.body.innerHTML = `
    <div class="chatlist-box">
      <ul class="contactslist">
        <li class="sessionlist active" data-uid="20001"><div class="name">微博好友</div></li>
      </ul>
    </div>
    <div class="chat-header"><div class="friendname one-line hvhd"><div><span>微博好友</span></div></div></div>
    <ul class="chat-content">
      <li class="message-item"><div class="bubble_cont basi left"><p class="puretext">在吗</p></div></li>
    </ul>
    <textarea id="webchat-textarea"></textarea>
  `;
}

function biliFixture() {
  document.body.innerHTML = `
    <div class="list-container">
      <div class="list-item"><span class="name">测试用户</span></div>
    </div>
    <div class="dialog">
      <div class="title"><span>测试用户</span></div>
      <div class="message-list">
        <div class="msg-item not-me"><div class="message-content">你好，在吗</div></div>
      </div>
      <div class="editor-box">
        <div id="editor" contenteditable="true"></div>
        <button class="btn-box send-btn">发送</button>
      </div>
    </div>
  `;
}

describe('下行流：weibo sendOutbound 全链（assertConv + send_verified + 自报）', () => {
  beforeEach(() => {
    localStorage.clear();
    stubLocation('https://weibo.com/chat');
    weiboFixture();
  });

  it('发送成功、回查命中、自报 agent 帧 extra.send_verified=true、输入框清空', async () => {
    const a = buildWeiboAdapter();
    wrapEchoSend(a, WEIBO_ECHO, '.chat-content');
    const onMessage = vi.fn();
    a.start({ onMessage });

    const p = a.sendOutbound('微博自动回复', '20001', { sendVerifyMs: 0 });
    await vi.advanceTimersByTimeAsync(4000);
    const ok = await p;
    expect(ok.ok).toBe(true);
    expect(ok.sendVerified).toBe(true);
    expect(ok.rateLimited).toBe(false);
    expect(ok.notFound).toBe(false);
    // 真渠道 hooks 走到了（输入框被清空 = weibo sendText 完整执行）
    expect(document.querySelector('#webchat-textarea').value).toBe('');
    // 自报出站帧：sender_type=agent + send_verified 结论位随 extra 上行
    const selfReport = onMessage.mock.calls.map((c) => c[0]).find((m) => m.sender_type === SENDER.AGENT);
    expect(selfReport).toBeTruthy();
    expect(selfReport.content).toBe('微博自动回复');
    expect(selfReport.extra.send_verified).toBe(true);
    a.stop();
  });

  it('回查未见（平台未渲染回声）：ok=true 但 sendVerified=false（结论位语义，不重发）', async () => {
    const a = buildWeiboAdapter();
    const onMessage = vi.fn();
    a.start({ onMessage });
    const p = a.sendOutbound('未见回声的回复', '20001', { sendVerifyMs: 0 });
    await vi.advanceTimersByTimeAsync(4000);
    const ok = await p;
    expect(ok.ok).toBe(true);
    expect(ok.sendVerified).toBe(false);
    a.stop();
  });
});

describe('下行流：bilibili sendOutbound 全链（assertConv + send_verified + 自报）', () => {
  beforeEach(() => {
    localStorage.clear();
    stubLocation('https://message.bilibili.com/#/whisper');
    biliFixture();
  });

  it('发送成功、回查命中、自报 agent 帧 extra.send_verified=true、输入框清空', async () => {
    const a = buildBilibiliAdapter();
    wrapEchoSend(a, BILI_ECHO, '.message-list');
    const onMessage = vi.fn();
    a.start({ onMessage });

    const conv = a.getConversationId();
    expect(conv).toBe('conv:测试用户');
    const p = a.sendOutbound('B站自动回复', conv, { sendVerifyMs: 0 });
    await vi.advanceTimersByTimeAsync(4000);
    const ok = await p;
    expect(ok.ok).toBe(true);
    expect(ok.sendVerified).toBe(true);
    expect(ok.rateLimited).toBe(false);
    expect(ok.notFound).toBe(false);
    expect(document.querySelector('#editor').innerText || '').toBe('');
    const selfReport = onMessage.mock.calls.map((c) => c[0]).find((m) => m.sender_type === SENDER.AGENT);
    expect(selfReport).toBeTruthy();
    expect(selfReport.content).toBe('B站自动回复');
    expect(selfReport.extra.send_verified).toBe(true);
    a.stop();
  });

  it('目标会话未打开：notFound=true，不触碰输入框（错会话拒发）', async () => {
    const a = buildBilibiliAdapter();
    const onMessage = vi.fn();
    a.start({ onMessage });
    const p = a.sendOutbound('不该发出去', 'conv:不存在的人', { sendVerifyMs: 0 });
    await vi.advanceTimersByTimeAsync(8000);
    const ok = await p;
    expect(ok.ok).toBe(false);
    expect(ok.notFound).toBe(true);
    // 输入框未被写入（拒发语义：宁可不发也不错发）
    expect(document.querySelector('#editor').innerText || '').toBe('');
    a.stop();
  });
});
