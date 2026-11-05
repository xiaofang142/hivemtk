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

function fixture() {
  document.body.innerHTML = `
    <ul class="message-sidebar__interactions">
      <li class="message-sidebar__item" data-uid="nav-not-conv"><span class="message-sidebar__item-name">回复我的</span></li>
      <li class="message-sidebar__item is-active" data-uid="10086"><span class="message-sidebar__item-name">测试用户</span></li>
    </ul>
    <div class="message-header__title"><a href="/u/10086">测试用户</a></div>
    <div class="message-content__inner">
      <div class="interaction-item__content"><div class="message-text-nodes">你好，在吗</div></div>
      <div class="interaction-item__content"><div class="message-text-nodes">在的</div></div>
    </div>
    <textarea class="message-reply-box__input"></textarea>
    <button class="message-reply-box__pub">发表评论</button>
  `;
}

describe('bilibili adapter（实验渠道）', () => {
  beforeEach(() => {
    vi.unstubAllGlobals();
    localStorage.clear();
    stubLocation('https://message.bilibili.com/#/whisper');
    fixture();
  });

  it('match 只在私信会话页命中；侧栏导航项不被当成会话', () => {
    const a = buildBilibiliAdapter();
    expect(a.match()).toBe(true);
    const list = a.getConversationList();
    expect(list.length).toBe(1);
    expect(list[0].id).toBe('10086');
    expect(list[0].name).toBe('测试用户');
  });

  it('getConversationId 优先取 header 对方链接', () => {
    const a = buildBilibiliAdapter();
    expect(a.getConversationId()).toBe('10086');
  });

  it('parseMessageItem 解析文本与媒体占位', () => {
    const a = buildBilibiliAdapter();
    const items = a.getMessageItems();
    expect(items.length).toBe(2);
    const parsed = a.parseMessageItem(items[0]);
    expect(parsed.text).toBe('你好，在吗');
    expect(parsed.sender_type).toBe(SENDER.CUSTOMER);
  });

  it('sendText 写入输入框并点击发表按钮', async () => {
    const a = buildBilibiliAdapter();
    const input = document.querySelector('.message-reply-box__input');
    const btn = document.querySelector('.message-reply-box__pub');
    let seen = '';
    btn.addEventListener('click', () => { seen = input.value; });
    await a.hooks.sendText('测试回复', {});
    expect(seen).toBe('测试回复');
    expect(input.value).toBe('');
  });

  it('getAccountId 无登录态时返回稳定占位而非空串', () => {
    expect(getAccountId()).toBe('bilibili-unknown');
  });
});
