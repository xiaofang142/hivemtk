// MAIN/ISOLATED 共用的数据接口；页面可篡改这些标记，不能作为身份授权。
const ROW = '[data-e2e="conversation-item"]';
const MESSAGE = '.MessageItemTextcontainer';
const PEER = 'data-hivebridge-peer-id';
const DIRECTION = 'data-hivebridge-direction';
const CONVERSATION = 'data-hivebridge-message-conversation';
const STALE = 'data-hivebridge-message-stale';
const REFRESH = 'hivebridge:douyin-identity-refresh';
const MESSAGE_REFRESH = 'hivebridge:douyin-message-refresh';
const STOP = 'hivebridge:douyin-identity-stop';
const messageBindings = new WeakMap();

function validPeer(value) {
  return typeof value === 'string' && /^MS4w[A-Za-z0-9_-]{12,96}$/.test(value) ? value : '';
}

// 仅访问白名单自有数据字段，绝不调用React对象上的getter或业务方法。
function ownValue(object, key) {
  if (!object || typeof object !== 'object') return undefined;
  const descriptor = Object.getOwnPropertyDescriptor(object, key);
  return descriptor && Object.prototype.hasOwnProperty.call(descriptor, 'value') ? descriptor.value : undefined;
}

function reactEntity(element, property) {
  for (let node = element, depth = 0; node && depth < 4; node = node.parentElement, depth += 1) {
    const key = Object.getOwnPropertyNames(node).find((name) => name.startsWith('__reactFiber$'));
    let fiber = key ? ownValue(node, key) : null;
    for (let i = 0; fiber && i < 8; i += 1, fiber = ownValue(fiber, 'return')) {
      const props = ownValue(fiber, 'memoizedProps');
      const data = ownValue(props, property);
      if (!data) continue;
      // 最近的对应实体优先，不能从更远处混入其他会话/消息。
      return data;
    }
  }
  return null;
}

function reactValues(element, property, fieldNames) {
  const data = reactEntity(element, property);
  return fieldNames.map((field) => ownValue(data, field)).filter((value) => value !== undefined);
}

function setEvidence(element, name, value) {
  if (!value) element.removeAttribute(name);
  else if (element.getAttribute(name) !== value) element.setAttribute(name, value);
}

function refreshRows(root) {
  const rows = [...root.querySelectorAll(ROW)].slice(0, 500);
  for (const row of rows) {
    const values = reactValues(row, 'conversation', ['toParticipantSecUserId', '_toParticipantSecUserId']);
    const peers = values.map(validPeer);
    const id = peers.length && peers.every((peer) => peer && peer === peers[0]) ? peers[0] : '';
    setEvidence(row, PEER, id);
  }
  const active = rows.filter((row) => row.classList.contains('conversationConversationItemcurConversation') || row.getAttribute('aria-selected') === 'true');
  return active.length === 1 ? readDouyinPeerIdentity(active[0]) : '';
}

function refreshMessage(item, current) {
    const entity = reactEntity(item, 'message');
    const value = ownValue(entity, 'isMyMessage');
    const values = value === undefined ? [] : [value];
    const previous = messageBindings.get(item);
    // 活动行先改变而旧气泡尚未替换时，不能把旧实体绑定到新客户。
    const stale = previous && previous.conversation !== current && previous.entity === (entity || item);
    if (current && !stale) messageBindings.set(item, { conversation: current, entity: entity || item });
    const explicitSelf = item.classList.contains('MessageItemTextisFromMe');
    let direction = 'unknown';
    if (values.length && values.every((value) => typeof value === 'boolean' && value === values[0])) {
      direction = values[0] ? 'self' : (explicitSelf ? 'unknown' : 'customer');
    } else if (!values.length && explicitSelf) direction = 'self';
    setEvidence(item, DIRECTION, current && !stale && direction !== 'unknown' ? direction : '');
    setEvidence(item, CONVERSATION, current && !stale && direction !== 'unknown' ? current : '');
    setEvidence(item, STALE, current && stale ? current : '');
}

export function refreshDouyinIdentity(root = document) {
  const current = refreshRows(root);
  for (const item of [...root.querySelectorAll(MESSAGE)].slice(0, 1000)) {
    refreshMessage(item, current);
  }
}

export function readDouyinPeerIdentity(row) {
  return validPeer(row?.getAttribute(PEER));
}

export function readDouyinDirection(item, conversationId) {
  if (!validPeer(conversationId)) return 'unknown';
  const element = item?.matches(MESSAGE) ? item : item?.querySelector(MESSAGE) || item?.closest(MESSAGE);
  if (!element || element.getAttribute(CONVERSATION) !== conversationId) return 'unknown';
  const direction = element.getAttribute(DIRECTION);
  return direction === 'self' || direction === 'customer' ? direction : 'unknown';
}

export function requestDouyinIdentityRefresh(root = document) {
  root.dispatchEvent(new Event(REFRESH));
}

export function requestDouyinMessageRefresh(item) {
  const element = item?.matches(MESSAGE) ? item : item?.querySelector(MESSAGE) || item?.closest(MESSAGE);
  element?.dispatchEvent(new Event(MESSAGE_REFRESH, { bubbles: true }));
}

export function isDouyinMessageStale(item, conversationId) {
  const element = item?.matches(MESSAGE) ? item : item?.querySelector(MESSAGE) || item?.closest(MESSAGE);
  return !!validPeer(conversationId) && element?.getAttribute(STALE) === conversationId;
}

export function installDouyinIdentityReader(root = document) {
  // 重注入只保留一个读器；固定事件无参数、无执行/网络/凭证能力。
  root.dispatchEvent(new Event(STOP));
  let timer;
  const refresh = () => refreshDouyinIdentity(root);
  const refreshIdentity = () => refreshRows(root);
  const refreshOneMessage = (event) => {
    if (event.target?.matches?.(MESSAGE)) refreshMessage(event.target, refreshRows(root));
  };
  const observer = new MutationObserver(() => {
    if (timer !== undefined) return;
    timer = setTimeout(() => { timer = undefined; refresh(); }, 50);
  });
  const cleanup = () => {
    observer.disconnect();
    clearTimeout(timer);
    root.removeEventListener(REFRESH, refreshIdentity);
    root.removeEventListener(MESSAGE_REFRESH, refreshOneMessage);
    root.removeEventListener(STOP, cleanup);
  };
  root.addEventListener(REFRESH, refreshIdentity);
  root.addEventListener(MESSAGE_REFRESH, refreshOneMessage);
  root.addEventListener(STOP, cleanup);
  if (root.documentElement) observer.observe(root.documentElement, { childList: true, subtree: true, attributes: true, attributeFilter: ['class', 'aria-selected'] });
  refresh();
  return cleanup;
}
