import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { readFileSync } from 'node:fs';
import { refreshDouyinIdentity, installDouyinIdentityReader, readDouyinPeerIdentity, readDouyinDirection } from '../src/channels/douyin-identity.js';
import { buildDouyinAdapter } from '../src/channels/douyin.js';

const A = 'MS4wLjABAAAA_customer_a_123456789';
const B = 'MS4wLjABAAAA_customer_b_123456789';
function fixture() {
  document.body.innerHTML = `<div data-e2e="conversation-item" class="conversationConversationItemcurConversation"></div>
    <div class="MessageItemTextcontainer"><p>合成正文</p></div>`;
  const row = document.querySelector('[data-e2e="conversation-item"]');
  const item = document.querySelector('.MessageItemTextcontainer');
  const conversation = { toParticipantSecUserId: A, _toParticipantSecUserId: A };
  const message = { isMyMessage: true };
  Object.defineProperty(row, '__reactFiber$fixture', { configurable: true, value: { memoizedProps: { conversation } } });
  Object.defineProperty(item, '__reactFiber$fixture', { configurable: true, value: { memoizedProps: { message } } });
  return { row, item, conversation, message };
}

describe('抖音MAIN身份读器：只读字段和会话绑定', () => {
  let cleanup;
  beforeEach(() => { document.body.innerHTML = ''; });
  afterEach(() => { cleanup?.(); cleanup = null; vi.unstubAllGlobals(); document.body.innerHTML = ''; });

  it('读取固定peer/布尔方向，不读取正文或执行属性getter', () => {
    const f = fixture();
    const secretGetter = vi.fn(() => { throw new Error('不得读取正文'); });
    Object.defineProperty(f.message, 'parsedContent', { get: secretGetter });
    refreshDouyinIdentity(document);
    expect(readDouyinPeerIdentity(f.row)).toBe(A);
    expect(readDouyinDirection(f.item, A)).toBe('self');
    expect(secretGetter).not.toHaveBeenCalled();
    const source = readFileSync('src/channels/douyin-identity.js', 'utf8');
    expect(/chrome\.|fetch\(|localStorage|\.click\(|\.textContent|\.innerHTML|eval\(|new Function/.test(source)).toBe(false);
  });

  it('节点回收/方向变化重新提取；两个peer字段冲突清除证据', () => {
    const f = fixture(); refreshDouyinIdentity(document);
    f.conversation.toParticipantSecUserId = B;
    f.conversation._toParticipantSecUserId = B;
    Object.defineProperty(f.item, '__reactFiber$fixture', { configurable: true, value: { memoizedProps: { message: { isMyMessage: false } } } });
    refreshDouyinIdentity(document);
    expect(readDouyinPeerIdentity(f.row)).toBe(B);
    expect(readDouyinDirection(f.item, A)).toBe('unknown');
    expect(readDouyinDirection(f.item, B)).toBe('customer');
    f.conversation._toParticipantSecUserId = A;
    refreshDouyinIdentity(document);
    expect(readDouyinPeerIdentity(f.row)).toBe('');
    expect(readDouyinDirection(f.item, B)).toBe('unknown');
  });

  it('无方向、冲突class与React或非法ID不能猜测', () => {
    const f = fixture();
    f.item.classList.add('MessageItemTextisFromMe'); f.message.isMyMessage = false;
    refreshDouyinIdentity(document);
    expect(readDouyinDirection(f.item, A)).toBe('unknown');
    delete f.message.isMyMessage; f.item.classList.remove('MessageItemTextisFromMe');
    refreshDouyinIdentity(document);
    expect(readDouyinDirection(f.item, A)).toBe('unknown');
    f.row.setAttribute('data-hivebridge-peer-id', 'conv:同名客户');
    expect(readDouyinPeerIdentity(f.row)).toBe('');
  });

  it('固定刷新事件同步更新指定节点，不读取事件detail中的参数', () => {
    const f = fixture(); cleanup = installDouyinIdentityReader(document);
    expect(readDouyinPeerIdentity(f.row)).toBe(A);
    f.conversation.toParticipantSecUserId = B; f.conversation._toParticipantSecUserId = B;
    f.row.dispatchEvent(new CustomEvent('hivebridge:douyin-identity-refresh', { bubbles: true, detail: { script: 'ignored' } }));
    expect(readDouyinPeerIdentity(f.row)).toBe(B);
  });

  it('活动客户变化时，旧消息实体不能被直接重绑给新客户', () => {
    const f = fixture(); refreshDouyinIdentity(document);
    expect(readDouyinDirection(f.item, A)).toBe('self');
    f.conversation.toParticipantSecUserId = B;
    f.conversation._toParticipantSecUserId = B;
    refreshDouyinIdentity(document);
    expect(readDouyinDirection(f.item, B)).toBe('unknown');
    refreshDouyinIdentity(document);
    expect(readDouyinDirection(f.item, B)).toBe('unknown');
    Object.defineProperty(f.item, '__reactFiber$fixture', { configurable: true, value: { memoizedProps: { message: { isMyMessage: false } } } });
    refreshDouyinIdentity(document);
    expect(readDouyinDirection(f.item, B)).toBe('customer');
  });

  it('切换中的旧气泡不能以unknown客户占位重新上报给新客户', () => {
    const f = fixture();
    vi.stubGlobal('location', { href: 'https://www.douyin.com/chat', pathname: '/chat', search: '', hostname: 'www.douyin.com' });
    Object.defineProperty(f.row, 'offsetParent', { configurable: true, get: () => document.body });
    cleanup = installDouyinIdentityReader(document);
    const adapter = buildDouyinAdapter();
    expect(adapter.hooks.parseMessageItem(f.item).sender_type).toBe('self');
    f.conversation.toParticipantSecUserId = B; f.conversation._toParticipantSecUserId = B;
    refreshDouyinIdentity(document);
    expect(adapter.hooks.parseMessageItem(f.item)).toBeNull();
  });

  it('同步会话身份请求不重复扫描整个消息列表', () => {
    const f = fixture(); cleanup = installDouyinIdentityReader(document);
    const query = vi.spyOn(document, 'querySelectorAll');
    document.dispatchEvent(new Event('hivebridge:douyin-identity-refresh'));
    expect(query.mock.calls.filter(([selector]) => selector === '.MessageItemTextcontainer')).toHaveLength(0);
    query.mockRestore();
  });
});
