import { describe, it, expect, beforeEach, vi } from 'vitest';

vi.mock('../src/core/humanize.js', () => ({
  humanDelay: async () => {},
}));

import { buildWeiboAdapter, getAccountId } from '../src/channels/weibo.js';
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
    <div class="chatlist-box">
      <ul class="contactslist">
        <li class="sessionlist active" data-uid="20001"><div class="name">微博好友</div></li>
        <li class="sessionlist" data-uid="20002"><div class="name">另一个好友</div></li>
      </ul>
    </div>
    <div class="chat-header"><span class="name">微博好友</span></div>
    <ul class="chat-content">
      <li class="message-item other"><div class="text">在吗</div></li>
      <li class="message-item self"><div class="text">在的</div></li>
    </ul>
    <textarea id="webchat-textarea"></textarea>
  `;
}

describe('weibo adapter（实验渠道）', () => {
  beforeEach(() => {
    vi.unstubAllGlobals();
    localStorage.clear();
    stubLocation('https://weibo.com/chat');
    fixture();
  });

  it('match 命中聊天页；会话列表去重', () => {
    const a = buildWeiboAdapter();
    expect(a.match()).toBe(true);
    const list = a.getConversationList();
    expect(list.length).toBe(2);
    expect(list[0].id).toBe('20001');
  });

  it('getConversationId 取活动会话 data-uid', () => {
    const a = buildWeiboAdapter();
    expect(a.getConversationId()).toBe('20001');
  });

  it('parseMessageItem 区分 self/other', () => {
    const a = buildWeiboAdapter();
    const items = a.getMessageItems();
    expect(items.length).toBe(2);
    expect(a.parseMessageItem(items[0]).sender_type).toBe(SENDER.CUSTOMER);
    expect(a.parseMessageItem(items[1]).sender_type).toBe(SENDER.SELF);
    expect(a.parseMessageItem(items[0]).text).toBe('在吗');
  });

  it('sendText 用 Enter 发送并清空输入框', async () => {
    const a = buildWeiboAdapter();
    const input = document.querySelector('#webchat-textarea');
    let seen = '';
    input.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') seen = input.value;
    });
    await a.hooks.sendText('微博回复', {});
    expect(seen).toBe('微博回复');
    expect(input.value).toBe('');
  });

  it('getAccountId 无登录态时返回稳定占位', () => {
    expect(getAccountId()).toBe('weibo-unknown');
  });
});
