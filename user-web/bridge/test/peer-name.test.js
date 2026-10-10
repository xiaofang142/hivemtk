import { describe, it, expect, beforeEach } from 'vitest';
import { JSDOM } from 'jsdom';

beforeEach(() => {
  document.body.innerHTML = '';
  const dom = new JSDOM('<!DOCTYPE html><html><body></body></html>', { url: 'https://www.xiaohongshu.com/chat' });
  // vitest5 jsdom 环境：global.window/document 是只读 getter，须用 defineProperty 覆盖
  Object.defineProperty(globalThis, 'window', { value: dom.window, writable: true, configurable: true });
  Object.defineProperty(globalThis, 'document', { value: dom.window.document, writable: true, configurable: true });
});

// 让 jsdom 元素可见
function makeVisible(el) {
  Object.defineProperty(el, 'offsetParent', { configurable: true, get: () => ({}) });
  return el;
}

describe('xhs.getPeerName 从聊天 header class 抽取对方昵称', () => {
  it('新版 .xhs-im-chat-title 命中 → 返回对方昵称', async () => {
    document.body.innerHTML = '';
    // header 标题元素
    const title = document.createElement('div');
    title.className = 'xhs-im-chat-title';
    title.textContent = '买家小明';
    makeVisible(title);
    document.body.appendChild(title);
    localStorage.setItem('hivebridge:account:xhs_web', 'me123');

    const { getPeerName } = await import('../src/channels/xhs.js');
    expect(getPeerName()).toBe('买家小明');
  });

  it('header 内带 /user/profile/ 的链接（排除「我」）→ 返回对方昵称', async () => {
    document.body.innerHTML = '';
    const header = document.createElement('div');
    header.className = 'xhs-im-chat-header';
    const me = document.createElement('a');
    me.href = '/user/profile/me123';
    me.textContent = '我';
    const peer = document.createElement('a');
    peer.href = '/user/profile/user456';
    peer.textContent = '客户阿强';
    header.appendChild(me);
    header.appendChild(peer);
    makeVisible(header);
    document.body.appendChild(header);
    localStorage.setItem('hivebridge:account:xhs_web', 'me123');

    const { getPeerName } = await import('../src/channels/xhs.js');
    expect(getPeerName()).toBe('客户阿强');
  });

  it('活动会话项里的昵称元素（header 链接都取不到时兜底）', async () => {
    document.body.innerHTML = '';
    const list = document.createElement('div');
    const item = makeVisible(document.createElement('div'));
    item.className = 'xhs-im-conv-item xhs-im-conv-item--active';
    const nameEl = document.createElement('div');
    nameEl.className = 'nickname';
    nameEl.textContent = '回访客户';
    item.appendChild(nameEl);
    list.appendChild(item);
    document.body.appendChild(list);
    localStorage.setItem('hivebridge:account:xhs_web', 'me123');

    // 必须 mock qs/qsa 命中 CHAT_LIST（getPeerName 内部第 3 步用 qsa(SEL.CHAT_LIST)）
    const { getPeerName } = await import('../src/channels/xhs.js');
    expect(getPeerName()).toBe('回访客户');
  });

  it('排除「私信」「消息」等通用标题词（避免假昵称）', async () => {
    document.body.innerHTML = '';
    const title = document.createElement('div');
    title.className = 'xhs-im-chat-title';
    title.textContent = '私信'; 
    document.body.appendChild(title);
    localStorage.setItem('hivebridge:account:xhs_web', 'me123');
    const { getPeerName } = await import('../src/channels/xhs.js');
    expect(getPeerName()).toBe('');
  });
});

describe('douyin.getPeerName 从聊天 header class 抽取对方昵称', () => {
  it('[data-e2e="chat-header-title"] 命中 → 返回对方昵称', async () => {
    document.body.innerHTML = '';
    const title = document.createElement('div');
    title.setAttribute('data-e2e', 'chat-header-title');
    title.textContent = '咨询客户';
    makeVisible(title);
    document.body.appendChild(title);
    localStorage.setItem('hivebridge:account:douyin_web', 'MS4myid');

    const { getPeerName } = await import('../src/channels/douyin.js');
    expect(getPeerName()).toBe('咨询客户');
  });

  it('chat-header 内 /user/ 链接（排除「我」）→ 返回对方昵称', async () => {
    document.body.innerHTML = '';
    const header = document.createElement('div');
    header.className = 'chat-header';
    const me = document.createElement('a');
    me.href = '/user/MS4myid';
    me.textContent = '我的';
    const peer = document.createElement('a');
    peer.href = '/user/MS4peer789';
    peer.textContent = '客户李姐';
    header.appendChild(me);
    header.appendChild(peer);
    makeVisible(header);
    document.body.appendChild(header);
    localStorage.setItem('hivebridge:account:douyin_web', 'MS4myid');

    const { getPeerName } = await import('../src/channels/douyin.js');
    expect(getPeerName()).toBe('客户李姐');
  });

  it('排除「私信」「消息」「抖音」等通用标题词', async () => {
    document.body.innerHTML = '';
    const title = document.createElement('div');
    title.setAttribute('data-e2e', 'chat-header-title');
    title.textContent = '消息';
    document.body.appendChild(title);
    localStorage.setItem('hivebridge:account:douyin_web', 'MS4myid');
    const { getPeerName } = await import('../src/channels/douyin.js');
    expect(getPeerName()).toBe('');
  });
});
describe('xhs.parseMessageItem 把对方昵称装到 sender_name', () => {
  it('1v1 客户消息 → sender_name = 聊天 header 的对方昵称', async () => {
    document.body.innerHTML = '';
    // 设置 header 对方昵称
    const title = document.createElement('div');
    title.className = 'xhs-im-chat-title';
    title.textContent = '咨询客户阿珍';
    Object.defineProperty(title, 'offsetParent', { configurable: true, get: () => ({}) });
    document.body.appendChild(title);
    // 活动会话项（getConversationId 兜底）
    const active = document.createElement('div');
    active.className = 'xhs-im-conv-item xhs-im-conv-item--active';
    active.setAttribute('data-conv-id', '_CONV_1');
    document.body.appendChild(active);
    // 一条对方文本消息
    const msg = document.createElement('div');
    msg.className = 'chat-item chat-item__content--left';
    const textEl = document.createElement('p');
    textEl.className = 'xhs-im-bubble__text';
    textEl.textContent = '多少钱？';
    msg.appendChild(textEl);
    document.body.appendChild(msg);
    localStorage.setItem('hivebridge:account:xhs_web', 'me123');

    const { buildXhsAdapter } = await import('../src/channels/xhs.js');
    const adapter = buildXhsAdapter();
    const parsed = adapter.parseMessageItem(msg);
    expect(parsed).toBeTruthy();
    expect(parsed.sender_type).toBe('customer');
    expect(parsed.sender_name).toBe('咨询客户阿珍');
    expect(parsed.is_group).toBe(false);
    expect(parsed.group_name).toBe('');
  });

  it('1v1 自己消息 → 前端不再判定 self/other，统一 customer + 取对方昵称（后端权威重判）', async () => {
    document.body.innerHTML = '';
    const title = document.createElement('div');
    title.className = 'xhs-im-chat-title';
    title.textContent = '咨询客户阿珍';
    Object.defineProperty(title, 'offsetParent', { configurable: true, get: () => ({}) });
    document.body.appendChild(title);
    const active = document.createElement('div');
    active.className = 'xhs-im-conv-item xhs-im-conv-item--active';
    active.setAttribute('data-conv-id', 'c1');
    document.body.appendChild(active);
    const msg = document.createElement('div');
    msg.className = 'chat-item chat-item__content--right';
    const textEl = document.createElement('p');
    textEl.className = 'xhs-im-bubble__text';
    textEl.textContent = '在的';
    msg.appendChild(textEl);
    document.body.appendChild(msg);
    localStorage.setItem('hivebridge:account:xhs_web', 'me123');

    const { buildXhsAdapter } = await import('../src/channels/xhs.js');
    const adapter = buildXhsAdapter();
    const parsed = adapter.parseMessageItem(msg);
    expect(parsed).toBeTruthy();
    expect(parsed.sender_type).toBe('customer');
    expect(parsed.sender_name).toBe('咨询客户阿珍');
  });
});

