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
