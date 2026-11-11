import { describe, it, expect, beforeEach, vi } from 'vitest';

vi.mock('../src/core/humanize.js', () => ({
  humanDelay: async () => {},
}));

import { buildJdAdapter, getAccountId } from '../src/channels/jd.js';
import { SENDER } from '../src/core/types.js';

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

function fixture() {
  document.body.innerHTML = `
    <div class="session-list">
      <div class="session-item active" data-session-id="jd-1001"><span class="name">买家甲</span></div>
      <div class="session-item" data-session-id="jd-1002"><span class="name">买家乙</span></div>
    </div>
    <div class="chat-title">买家甲</div>
    <div class="message-list">
      <div class="message-item"><span class="message-text">你好，发货吗</span></div>
      <div class="message-item"><span class="message-text">今天发</span></div>
    </div>
    <textarea class="chat-input"></textarea>
    <button class="send-btn">发送</button>
  `;
}

describe('jd adapter（实验渠道）', () => {
  beforeEach(() => {
    vi.unstubAllGlobals();
    localStorage.clear();
    stubLocation('https://jdm.jd.com/');
    fixture();
  });

  it('match 要求会话列表 + 输入框同时存在', () => {
    const a = buildJdAdapter();
    expect(a.match()).toBe(true);
    const list = a.getConversationList();
    expect(list.length).toBe(2);
    expect(list[0].id).toBe('jd-1001');
  });

  it('getConversationId 取活动会话 data-session-id', () => {
    const a = buildJdAdapter();
    expect(a.getConversationId()).toBe('jd-1001');
  });

  it('parseMessageItem 解析文本', () => {
    const a = buildJdAdapter();
    const items = a.getMessageItems();
    expect(items.length).toBe(2);
    const parsed = a.parseMessageItem(items[0]);
    expect(parsed.text).toBe('你好，发货吗');
    expect(parsed.sender_type).toBe(SENDER.CUSTOMER);
  });

  it('sendText 写入输入框并点击发送', async () => {
    const a = buildJdAdapter();
    const input = document.querySelector('.chat-input');
    const btn = document.querySelector('.send-btn');
    let seen = '';
    btn.addEventListener('click', () => { seen = input.value; });
    await a.hooks.sendText('京东回复', {});
    expect(seen).toBe('京东回复');
    expect(input.value).toBe('');
  });

  it('getAccountId 无登录信息时返回稳定占位', () => {
    expect(getAccountId()).toBe('jd-unknown');
  });
});

// M-4（2026-11-11）：jd 媒体分支补齐（审计缺口 video/voice/card）。
// fixture 口径（模板同 douyin/xhs 主力渠道），Experimental 渠道真机未校准（BR-1 同源缺口）。
describe('M-4 jd 媒体分支补齐', () => {
  beforeEach(() => {
    vi.unstubAllGlobals();
    localStorage.clear();
    stubLocation('https://jdm.jd.com/');
    document.body.innerHTML = '';
  });

  const jdItem = (html) => {
    document.body.innerHTML = `<div class="message-list">${html}</div>`;
    return document.querySelector('.message-list > div');
  };

  it('视频 video[src] → msg_type=video + media_url（此前缺 video 分支）', () => {
    const a = buildJdAdapter();
    const p = a.parseMessageItem(jdItem('<div class="message-item"><video src="https://jd.im/v.mp4"></video></div>'));
    expect(p).not.toBeNull();
    expect(p.msg_type).toBe('video');
    expect(p.media_url).toBe('https://jd.im/v.mp4');
  });

  it('语音 audio[src] → msg_type=voice + media_url；提取不到保持类型标注不丢弃', () => {
    const a = buildJdAdapter();
    const withUrl = a.parseMessageItem(jdItem('<div class="message-item"><div class="voice-wrap"><audio src="https://jd.im/v.m4a"></audio></div></div>'));
    expect(withUrl).not.toBeNull();
    expect(withUrl.msg_type).toBe('voice');
    expect(withUrl.media_url).toBe('https://jd.im/v.m4a');
    // 纯语音无 URL：旧门禁「!text && !mediaUrl → null」整条丢弃；M-4 放宽后保留类型标注
    const noUrl = a.parseMessageItem(jdItem('<div class="message-item"><div class="voice-wrap"></div></div>'));
    expect(noUrl).not.toBeNull();
    expect(noUrl.msg_type).toBe('voice');
    expect(noUrl.media_url).toBe('');
    expect(noUrl.text).toBe('[语音]');
  });

  it('卡片（标题/缩略图）→ msg_type=card，标题进 content', () => {
    const a = buildJdAdapter();
    const p = a.parseMessageItem(jdItem('<div class="message-item"><div class="card"><div class="title">订单卡</div><img src="https://jd.im/t.jpg"></div></div>'));
    expect(p).not.toBeNull();
    expect(p.msg_type).toBe('card');
    expect(p.text).toBe('订单卡');
    expect(p.media_url).toBe('https://jd.im/t.jpg');
  });

  it('回归：头像图不误判为消息图（文本+头像 → text + media_url 空）', () => {
    const a = buildJdAdapter();
    const p = a.parseMessageItem(jdItem('<div class="message-item"><img class="avatar" src="https://jd.im/av.jpg"><span class="message-text">在吗</span></div>'));
    expect(p).not.toBeNull();
    expect(p.msg_type).toBe('text');
    expect(p.media_url).toBe('');
    expect(p.text).toBe('在吗');
  });

  it('回归：带文字图片仍按既有口径归 text（M-4 不改 image 语义）', () => {
    const a = buildJdAdapter();
    const p = a.parseMessageItem(jdItem('<div class="message-item"><span class="message-text">看图</span><img src="https://jd.im/p.jpg"></div>'));
    expect(p).not.toBeNull();
    expect(p.msg_type).toBe('text');
    expect(p.media_url).toBe('https://jd.im/p.jpg');
  });
});
