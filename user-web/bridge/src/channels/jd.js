import { BaseAdapter } from '../core/channel-adapter.js';
import { CHANNELS } from '../core/types.js';
import {
  qs, qsa, cleanText, setValue, fillContentEditableHumanized,
  createLogger, findAnyMessageInput, looksLikeMessagePage, sanitizePeerName,
  assertConvOrAbort, absoluteMediaUrl, avatarImgUrl,
} from '../core/dom.js';
import { humanDelay } from '../core/humanize.js';
import { mergeSelectors } from '../core/selector-ai.js';
import { FRONTEND_DEFAULT_SENDER_TYPE } from '../core/fallback.js';

const log = createLogger('jd', CHANNELS.JD);

function mergedSelectors() {
  const fb = {
    itemSelectors: SEL.MSG_ITEM.split(',').map((s) => s.trim()).filter(Boolean),
    listSelectors: SEL.MSG_LIST.split(',').map((s) => s.trim()).filter(Boolean),
    textSelectors: SEL.TEXT.split(',').map((s) => s.trim()).filter(Boolean),
    inputSelectors: SEL.INPUT.split(',').map((s) => s.trim()).filter(Boolean),
    sendSelectors: SEL.SEND.split(',').map((s) => s.trim()).filter(Boolean),
  };
  return mergeSelectors(CHANNELS.JD, fb);
}

// —— 选择器定义（未真机校准，Experimental）——
// 京东聊天页有登录墙，2026-11-05 未取到真实 DOM；先给多候选 + 通用兜底。
// 京麦是大后台，匹配必须同时满足"会话列表 + 输入框"，否则会把搜索框当聊天框。
export const SEL = {
  CHAT_LIST: '[class*="session-list" i], [class*="conversation-list" i], [class*="chat-list" i], .J_sessionList',
  CONV_ITEM: '[class*="session-item" i], [class*="sessionItem" i], [class*="conversation-item" i], [class*="chat-item" i], li[class*="item" i]',
  MSG_LIST: '[class*="message-list" i], [class*="msg-list" i], [class*="chat-content" i], [class*="messageContent" i]',
  MSG_ITEM: '[class*="message-item" i], [class*="msg-item" i], [class*="chat-item" i], [class*="bubble" i]',
  TEXT: '[class*="message-text" i], [class*="msg-text" i], [class*="text-content" i], [class*="bubble" i]',
  INPUT: 'textarea[class*="input" i], div[contenteditable="true"], [class*="editor" i][contenteditable="true"]',
  SEND: 'button[class*="send" i], [class*="send-btn" i], [aria-label*="发送"]',
  PEER_NAME: '[class*="chat-title" i], [class*="session-title" i], [class*="header" i] [class*="name" i]',
  UNREAD: '[class*="unread" i], [class*="red-dot" i], [data-unread="1"]',
  SYSTEM: '[class*="system" i], [class*="notice" i], [class*="divider" i]',
};

const JD_HOSTS = ['jdm.jd.com', 'dongdong.jd.com', 'chat.jd.com', 'im.jd.com'];

function isJdHost() {
  try {
    const host = (location.hostname || '').toLowerCase();
    return JD_HOSTS.some((h) => host === h || host.endsWith('.' + h));
  } catch (_) {
    return false;
  }
}

function findInputEl() {
  const m = mergedSelectors();
  for (const sel of m.inputSelectors) {
    try {
      const el = qs(sel);
      if (el) return el;
    } catch (_) {  }
  }
  return qs(SEL.INPUT) || findAnyMessageInput();
}

function chatRoot() {
  return qs(SEL.MSG_LIST) || null;
}

function hasConversationList() {
  try {
    return qsa(SEL.CONV_ITEM).length > 0 || qsa(SEL.CHAT_LIST).length > 0;
  } catch (_) {
    return false;
  }
}

// 京麦后台有大量输入框（搜索、筛选、备注），必须同时看到"会话列表 + 输入框"才算聊天页。
function looksLikeChatPage() {
  return !!(findInputEl() && hasConversationList());
}

function getAccountId() {
  const metas = qsa('meta[name*="shop" i], meta[name*="user" i], meta[name*="pin" i]');
  for (const meta of metas) {
    const content = (meta.getAttribute('content') || '').trim();
    if (content) {
      try { localStorage.setItem(`hivebridge:account:${CHANNELS.JD}`, content); } catch (_) {  }
      return content;
    }
  }
  const el = qs('[data-shop-id], [data-pin], [data-user-id]');
  if (el) {
    const id = el.getAttribute('data-shop-id') || el.getAttribute('data-pin') || el.getAttribute('data-user-id');
    if (id) return String(id).trim();
  }
  try {
    const cached = localStorage.getItem(`hivebridge:account:${CHANNELS.JD}`);
    if (cached) return cached;
  } catch (_) {  }
  return 'jd-unknown';
}

function convElements() {
  const items = qsa(SEL.CONV_ITEM);
  if (items.length) return items;
  // 兜底：容器本身没有 item 类名时，取容器的直接子元素（不少后台用 li 无类名）。
  const out = [];
  for (const box of qsa(SEL.CHAT_LIST)) {
    for (const child of box.children) out.push(child);
  }
  return out;
}

function itemName(el) {
  const nameEl = el.querySelector('[class*="name" i], [class*="title" i], [class*="nick" i]');
  return sanitizePeerName(nameEl ? cleanText(nameEl) : cleanText(el));
}

function itemId(el) {
  const raw = el.getAttribute('data-session-id') || el.getAttribute('data-uid') ||
    el.getAttribute('data-pin') || el.getAttribute('data-id') || el.getAttribute('data-key') || el.id;
  return raw ? String(raw).trim() : '';
}

function getConversationId() {
  const active = convElements().find((el) =>
    el.classList && (el.classList.contains('active') || el.classList.contains('selected') ||
      el.getAttribute('aria-selected') === 'true'));
  if (active) {
    const id = itemId(active);
    if (id) return id;
  }
  try {
    const q = new URLSearchParams(location.search);
    const fromQuery = q.get('sessionId') || q.get('session_id') || q.get('uid') || q.get('pin');
    if (fromQuery) return String(fromQuery);
  } catch (_) {  }
  const header = qs(SEL.PEER_NAME);
  if (header) {
    const name = sanitizePeerName(cleanText(header));
    if (name) return 'conv:' + name.slice(0, 80);
  }
  if (active) {
    const name = itemName(active);
    if (name) return 'conv:' + name.slice(0, 80);
  }
  return null;
}

function getConversationList() {
  const out = [];
  const seen = new Set();
  for (const el of convElements()) {
    let id = itemId(el);
    const name = itemName(el);
    if (!id && name) id = 'conv:' + name.slice(0, 80);
    if (!id || seen.has(id)) continue;
    seen.add(id);
    out.push({ id, name, el });
  }
  return out;
}

function getPeerName() {
  const el = qs(SEL.PEER_NAME);
  return el ? sanitizePeerName(cleanText(el)) : '';
}

// —— 对方头像（M-2 客户头像链路，2026-11-11，尽力而为渠道）——
// 京东咚咚结构样本不足，仅抓聊天 header 容器内头像 img（与 PEER_NAME 同 header 口径）；
// 抓不到保持为空，不报错（真机校准缺口见 M-2 报告）。
function getPeerAvatar() {
  for (const sel of ['[class*="chat-header" i] img', '[class*="header" i] img']) {
    try {
      const u = avatarImgUrl(qs(sel));
      if (u) return u;
    } catch (_) {  }
  }
  return '';
}

function parseMessageItem(item) {
  if (!item) return null;
  try {
    if (item.matches(SEL.SYSTEM)) return null;
  } catch (_) {  }
  const textEl = item.querySelector(SEL.TEXT);
  const text = cleanText(textEl || item);
  // 头像过滤（2026-11-10 审计 F4）+ URL 绝对化（F5），与 xhs/xianyu/tiktok 同口径
  const img = Array.from(item.querySelectorAll('img[src]')).find(
    (el) => !(el.closest && el.closest('[class*="avatar" i], [class*="Avatar" i]'))
  ) || null;
  const mediaUrl = absoluteMediaUrl(img ? (img.getAttribute('src') || '') : '');
  if (!text && !mediaUrl) return null;
  return {
    message_id: item.getAttribute('data-id') || item.id || `c:${text || mediaUrl}`,
    sender_type: FRONTEND_DEFAULT_SENDER_TYPE,
    text: text || '[图片]',
    media_url: mediaUrl,
    msg_type: !text && mediaUrl ? 'image' : 'text',
    is_group: false,
    group_id: '',
    group_name: '',
    sender_name: getPeerName(),
    // M-2 客户头像链路：尽力而为抓对方头像，抓不到为空。
    sender_avatar: getPeerAvatar(),
    timestamp: Date.now(),
    raw: item.outerHTML ? item.outerHTML.slice(0, 500) : '',
  };
}

function getMessageItems() {
  const root = chatRoot() || document;
  const m = mergedSelectors();
  for (const sel of m.itemSelectors) {
    try {
      const items = Array.from(root.querySelectorAll(sel));
      if (items.length) return items;
    } catch (_) {  }
  }
  if (root && root !== document) return Array.from(root.children).filter((el) => cleanText(el));
  return [];
}

const hooks = {
  match() {
    if (!isJdHost()) return false;
    if (looksLikeChatPage()) return true;
    if (findInputEl() && looksLikeMessagePage() && hasConversationList()) {
      log.warn('match() 走 fallback 模式：京东选择器未命中，使用通用 DOM 扫描');
      return true;
    }
    return false;
  },
  matchMode() {
    if (!isJdHost()) return null;
    if (looksLikeChatPage()) return 'strict';
    return 'fallback';
  },
  selectors: SEL,
  getMessageRoot() {
    return chatRoot();
  },
  getMessageListRoot() {
    return chatRoot();
  },
  getMessageItems,
  parseMessageItem,
  getAccountId,
  getConversationId,
  getConversationList,
  mergedSelectors,
  getPeerName,
  getPeerAvatar,
  async sendText(text, opts = {}) {
    const input = findInputEl();
    if (!input) throw new Error('jd input not found');
    const isEditable = input.isContentEditable || input.getAttribute('contenteditable') === 'true';
    if (isEditable) {
      await fillContentEditableHumanized(input, text, { clearBefore: true });
    } else {
      setValue(input, '');
      setValue(input, text);
    }
    await humanDelay('click', { channel: CHANNELS.JD, account: getAccountId() });
    assertConvOrAbort(input, opts.assertConv);

    let btn = null;
    const m = mergedSelectors();
    for (const sel of m.sendSelectors) {
      try { btn = qs(sel); if (btn) break; } catch (_) {  }
    }
    if (!btn) btn = qs(SEL.SEND);
    if (btn) {
      try { btn.click(); } catch (_) {  }
    } else {
      log.warn('未找到京东发送按钮，改用回车发送');
      for (const type of ['keydown', 'keyup']) {
        input.dispatchEvent(new KeyboardEvent(type, {
          key: 'Enter', code: 'Enter', keyCode: 13, which: 13, bubbles: true, cancelable: true,
        }));
      }
    }
    try {
      if (isEditable) {
        while (input.firstChild) input.removeChild(input.firstChild);
        input.dispatchEvent(new InputEvent('input', { bubbles: true }));
      } else {
        setValue(input, '');
      }
    } catch (_) {  }
  },
};

export { getAccountId, getConversationId, getConversationList, getPeerName, getPeerAvatar, isJdHost };

export function buildJdAdapter() {
  return new BaseAdapter({
    name: 'jd',
    channel: CHANNELS.JD,
    SEL,
    hooks,
    rateLimiter: undefined,
  });
}
