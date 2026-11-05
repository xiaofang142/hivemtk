import { describe, it, expect, beforeEach, vi } from 'vitest';

vi.mock('../src/core/humanize.js', () => ({
  humanDelay: async () => {},
}));

import { buildBilibiliAdapter, getAccountId } from '../src/channels/bilibili.js';
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

// 2026-11-06 fixture 对齐 ChatGPT-On-CS v1.4.5 实证 DOM：
// .list-container > .list-item（.notify.notify-number 未读）、
// .dialog（.title > span 用户名）、.message-list > .msg-item
// （class not-me / .message-content class is-me）、#editor + btn-box send-btn。
function fixture() {
  document.body.innerHTML = `
    <div class="list-container">
      <div class="list-item"><span class="name">未关注人</span></div>
      <div class="list-item"><span class="name">测试用户</span><div class="notify notify-number">2</div></div>
    </div>
    <div class="dialog">
      <div class="title"><span>测试用户</span></div>
      <div class="message-list">
        <div class="msg-item not-me"><div class="message-content">你好，在吗</div></div>
        <div class="msg-item"><div class="message-content is-me">在的</div></div>
        <div class="msg-item not-me"><div class="message-content"><img src="https://i0.hdslb.com/bfs/x.jpg"></div></div>
      </div>
      <div class="editor-box">
        <div id="editor" contenteditable="true"></div>
        <button class="btn-box send-btn">发送</button>
      </div>
    </div>
  `;
}

describe('bilibili adapter（实验渠道，选择器对齐 ChatGPT-On-CS 实证）', () => {
  beforeEach(() => {
    vi.unstubAllGlobals();
    localStorage.clear();
    stubLocation('https://message.bilibili.com/#/whisper');
    fixture();
  });

  it('match 命中私信页；会话列表取 .list-item', () => {
    const a = buildBilibiliAdapter();
    expect(a.match()).toBe(true);
    const list = a.getConversationList();
    expect(list.length).toBe(2);
    expect(list[0].name).toBe('未关注人');
    expect(list[1].name).toBe('测试用户');
  });

  it('getConversationId 兜底用 .dialog 面板昵称派生', () => {
    const a = buildBilibiliAdapter();
    expect(a.getConversationId()).toBe('conv:测试用户');
  });

  it('parseMessageItem 按 not-me/is-me 区分方向并识别图片', () => {
    const a = buildBilibiliAdapter();
    const items = a.getMessageItems();
    expect(items.length).toBe(3);
    const other = a.parseMessageItem(items[0]);
    expect(other.sender_type).toBe(SENDER.CUSTOMER);
    expect(other.text).toBe('你好，在吗');
    expect(a.parseMessageItem(items[1]).sender_type).toBe(SENDER.SELF);
    const imgMsg = a.parseMessageItem(items[2]);
    expect(imgMsg.msg_type).toBe('image');
    expect(imgMsg.media_url).toContain('hdslb.com');
  });

  it('sendText 写入 #editor 并点击 btn-box send-btn', async () => {
    const a = buildBilibiliAdapter();
    const input = document.querySelector('#editor');
    const btn = document.querySelector('button.btn-box.send-btn');
    let seen = '';
    btn.addEventListener('click', () => { seen = input.innerText || input.textContent || ''; });
    await a.hooks.sendText('测试回复', {});
    expect(seen).toBe('测试回复');
    expect(input.innerText || input.textContent || '').toBe('');
  });

  it('getAccountId 无登录态时返回稳定占位而非空串', () => {
    expect(getAccountId()).toBe('bilibili-unknown');
  });
});
