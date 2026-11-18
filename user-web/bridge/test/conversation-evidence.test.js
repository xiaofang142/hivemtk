import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { buildDouyinAdapter, getConversationId as douyinId, getConversationList as douyinList, getPeerName } from '../src/channels/douyin.js';
import { buildXhsAdapter, getConversationId as xhsId } from '../src/channels/xhs.js';

// 结构来源：Space30 的 douyin-dom-identity.json / xhs-dom-identity.json；正文为合成测试数据。
const PEER = 'MS4wLjABAAAA_test_customer_stable_123456789';
function locationFor(host, path = '/chat') {
  vi.stubGlobal('location', { hostname: host, pathname: path, search: '', href: `https://${host}${path}` });
}
function visible() {
  for (const el of document.querySelectorAll('div, span')) {
    Object.defineProperty(el, 'offsetParent', { configurable: true, get: () => document.body });
  }
}
function douyinFixture(peer = PEER) {
  document.body.innerHTML = `<div data-e2e="conversation-list"><div data-e2e="conversation-item"
    class="conversationConversationItemwrapper conversationConversationItemcurConversation"
    ${peer ? `data-hivebridge-peer-id="${peer}"` : ''}>
    <div class="conversationConversationItemtitleWrapper"><span class="conversationConversationItemtitle">测试客户</span>
    <span class="ConversationItemTagNextToTitlewrapper">4 07:34</span></div></div></div>
    <div class="MessageItemTextcontainer MessageItemTextisFromMe" data-hivebridge-direction="self"
      data-hivebridge-message-conversation="${peer}"><p>合成回复</p></div>`;
  visible();
}

describe('稳定会话与明确方向证据', () => {
  beforeEach(() => { document.body.innerHTML = ''; });
  afterEach(() => { vi.unstubAllGlobals(); document.body.innerHTML = ''; });

  it('抖音昵称叶子不带未读徽章，稳定身份不随徽章变化', () => {
    locationFor('www.douyin.com'); douyinFixture();
    expect(douyinId()).toBe(PEER);
    expect(getPeerName()).toBe('测试客户');
    document.querySelector('.ConversationItemTagNextToTitlewrapper').textContent = '9 08:01';
    expect(douyinId()).toBe(PEER);
    expect(douyinList().map((row) => row.id)).toEqual([PEER]);
  });

  it('没有稳定身份的昵称只能展示，不能枚举或派生出站身份', () => {
    locationFor('www.douyin.com'); douyinFixture('');
    expect(douyinId()).toBeNull();
    expect(douyinList()).toEqual([]);
    expect(getPeerName()).toBe('测试客户');
  });

  it('抖音真实文本气泡可捕获，自方证据只在同一会话生效', () => {
    locationFor('www.douyin.com'); douyinFixture();
    const adapter = buildDouyinAdapter();
    const item = document.querySelector('.MessageItemTextcontainer');
    expect(adapter.hooks.getMessageItems()).toContain(item);
    expect(adapter.hooks.parseMessageItem(item).sender_type).toBe('self');
    item.setAttribute('data-hivebridge-message-conversation', 'MS4wLjABAAAA_other_customer');
    const unknown = adapter.hooks.parseMessageItem(item);
    expect(unknown.sender_type).toBe('customer');
    expect(unknown.extra.direction_source).toBe('unknown');
  });

  it('小红书活动行优先于滞后URL，明确me/other同文不得客户化自方', () => {
    locationFor('www.xiaohongshu.com', '/chat/old-conversation');
    document.body.innerHTML = `<div class="xhs-im-conv-item xhs-im-conv-item--active" data-conv-id="69c730300000000034018cb2"></div>
      <div class="chat-item"><div class="chat-item__bubble chat-item__bubble--me"><p class="xhs-im-bubble__text">好的</p></div></div>
      <div class="chat-item"><div class="chat-item__bubble chat-item__bubble--other"><p class="xhs-im-bubble__text">好的</p></div></div>`;
    visible();
    const adapter = buildXhsAdapter();
    const [self, customer] = [...document.querySelectorAll('.chat-item')].map((item) => adapter.hooks.parseMessageItem(item));
    expect(xhsId()).toBe('69c730300000000034018cb2');
    expect(self.sender_type).toBe('self');
    expect(self.sender_id).not.toBe(xhsId());
    expect(customer.sender_type).toBe('customer');
    expect(customer.sender_id).toBe(xhsId());
    expect(customer.extra.direction_source).toBe('xhs-bubble-class');
  });

  it('历史与实时携带同一方向来源；系统分类仍优先于自方标记', () => {
    locationFor('www.xiaohongshu.com', '/chat/69c730300000000034018cb2');
    document.body.innerHTML = `<div class="xhs-im-conv-item xhs-im-conv-item--active" data-conv-id="69c730300000000034018cb2"></div>
      <div class="chat-item"><div class="chat-item__bubble--me"><p class="xhs-im-bubble__text">合成历史回复</p></div></div>`;
    visible();
    const adapter = buildXhsAdapter();
    const item = document.querySelector('.chat-item');
    const parsed = adapter.hooks.parseMessageItem(item);
    const history = adapter._historyItem(parsed, 'outbound');
    expect(history.sender_type).toBe('self');
    expect(history.direction).toBe('outbound');
    expect(history.extra.direction_source).toBe('xhs-bubble-class');
    const emitted = [];
    adapter.callbacks = { onMessage: (message) => emitted.push(message) };
    adapter._emitMessage(parsed);
    expect(emitted[0].extra.direction_source).toBe('xhs-bubble-class');
    item.querySelector('p').textContent = '撤回了一条消息';
    expect(adapter.hooks.parseMessageItem(item).sender_type).toBe('system');
  });

  it('同名不同稳定ID不合并，旧昵称目标不会匹配新身份', () => {
    locationFor('www.douyin.com'); douyinFixture();
    const clone = document.querySelector('[data-e2e="conversation-item"]').cloneNode(true);
    clone.classList.remove('conversationConversationItemcurConversation');
    clone.setAttribute('data-hivebridge-peer-id', 'MS4wLjABAAAA_different_customer_123456789');
    document.querySelector('[data-e2e="conversation-list"]').append(clone); visible();
    const ids = douyinList().map((row) => row.id);
    expect(ids).toHaveLength(2);
    expect(ids).not.toContain('conv:测试客户');
  });
});
