import { BaseAdapter } from '../core/channel-adapter.js';
import { CHANNELS, SENDER } from '../core/types.js';
import {
  qs, qsa, cleanText, setValue, fillContentEditableHumanized,
  createLogger, findAnyMessageInput, looksLikeMessagePage, sanitizePeerName,
  assertConvOrAbort,
} from '../core/dom.js';
import { humanDelay } from '../core/humanize.js';
import { mergeSelectors } from '../core/selector-ai.js';
import { FRONTEND_DEFAULT_SENDER_TYPE } from '../core/fallback.js';

const log = createLogger('bilibili', CHANNELS.BILIBILI);

// 合并选择器：用户配置优先（chrome.storage），SEL 默认兜底。
function mergedSelectors() {
  const fb = {
    itemSelectors: SEL.MSG_ITEM.split(',').map((s) => s.trim()).filter(Boolean),
    listSelectors: SEL.MSG_LIST.split(',').map((s) => s.trim()).filter(Boolean),
    textSelectors: SEL.TEXT.split(',').map((s) => s.trim()).filter(Boolean),
    inputSelectors: SEL.INPUT.split(',').map((s) => s.trim()).filter(Boolean),
    sendSelectors: SEL.SEND.split(',').map((s) => s.trim()).filter(Boolean),
  };
  return mergeSelectors(CHANNELS.BILIBILI, fb);
}

// —— 选择器定义 ——
// 来源：2026-11-05 公开 bundle（message-pc/static/js/index.*.js +
// static/css/index.*.css）提取的类名，未在登录态真机校准；因此本渠道标
// Experimental。改版后优先在 popup 选择器面板覆盖，不急着发版。
export const SEL = {
  CHAT_LIST: '.message-sidebar__item, [class*="session-item" i], [class*="conversation-item" i], [class*="chat-item" i]',
  MSG_LIST: '.message-content__inner, .message-section__body, [class*="message-list" i]',
  MSG_ITEM: '.interaction-item__content, [class*="message-item" i], [class*="msg-item" i]',
  TEXT: '.message-text-nodes, [class*="message-text" i], [class*="msg-content" i]',
  INPUT: '.message-reply-box__input, textarea[class*="reply" i], textarea[placeholder*="回复"]',
  SEND: '.message-reply-box__pub, button[class*="send" i], [aria-label*="发送"]',
  PEER_NAME: '.message-header__title, [class*="message-header" i] [class*="title" i]',
  UNREAD: '.message-sidebar__item-dot, .message-sidebar__item-num, [class*="unread" i]',
  SYSTEM: '.message-divider, [class*="system" i], [class*="notice" i]',
};

function isBiliHost() {
  try {
    return /(^|\.)bilibili\.com$/i.test(location.hostname || '');
  } catch (_) {
    return false;
  }
}

function isMessageCenterUrl() {
  try {
    return /message\.bilibili\.com/i.test(location.host || '') ||
      /\/message\//i.test(location.pathname || '') ||
      /[?&#]\/?(whisper|reply|at|love)/i.test(location.href || '');
  } catch (_) {
    return false;
  }
}

// 输入框：优先 textarea，其次 contenteditable，最后通用扫描。
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

function looksLikeChatPage() {
  return !!(findInputEl() && (chatRoot() || qs(SEL.CHAT_LIST)));
}

// 当前登录 UID。__INITIAL_STATE__ 不存在时退回页面个人链接；再退回缓存。
function getAccountId() {
  try {
    const st = typeof window !== 'undefined' ? window.__INITIAL_STATE__ : null;
    const mid = st && st.userInfo && st.userInfo.mid;
    if (mid) return String(mid);
  } catch (_) {  }
  const links = qsa('header a[href*="/u/"], header a[href*="space.bilibili.com/"], nav a[href*="/u/"], nav a[href*="space.bilibili.com/"], .bili-header a[href*="space.bilibili.com/"]');
  for (const a of links) {
    const href = a.getAttribute('href') || '';
    const m = href.match(/(?:space\.bilibili\.com\/|\/u\/)(\d+)/);
    if (m && m[1]) {
      try { localStorage.setItem(`hivebridge:account:${CHANNELS.BILIBILI}`, m[1]); } catch (_) {  }
      return m[1];
    }
  }
  try {
    const cached = localStorage.getItem(`hivebridge:account:${CHANNELS.BILIBILI}`);
    if (cached) return cached;
  } catch (_) {  }
  // 稳定占位，不返回空串：空串会让服务端 400，历史/实时全断。
  return 'bilibili-unknown';
}

// 会话列表：.message-sidebar__item 首先是左侧导航（回复我的/@我/赞），
// 只有带 data-uid/data-mid 或 /u/<id> 链接的才算私信会话。
function conversationItems() {
  const all = qsa(SEL.CHAT_LIST);
  const NAV_TEXT = /^(回复我的|@我|收到的赞|系统通知|我的消息|消息中心|未读)$/;
  return all.filter((el) => {
    if (el.classList && el.classList.contains('message-sidebar__item')) {
      const label = cleanText(el);
      if (NAV_TEXT.test(label)) return false;
      const href = (el.querySelector('a[href*="/u/"], a[href*="space.bilibili.com/"]') || {}).getAttribute
        ? el.querySelector('a[href*="/u/"], a[href*="space.bilibili.com/"]').getAttribute('href')
        : '';
      return el.hasAttribute('data-uid') || el.hasAttribute('data-mid') || /\/u\/\d+|space\.bilibili\.com\/\d+/.test(href || '');
    }
    return true;
  });
}

function itemName(el) {
  const nameEl = el.querySelector('[class*="name" i], [class*="title" i], .message-sidebar__item-name');
  return sanitizePeerName(nameEl ? cleanText(nameEl) : cleanText(el));
}

function itemId(el) {
  const raw = el.getAttribute('data-uid') || el.getAttribute('data-mid') ||
    el.getAttribute('data-id') || el.getAttribute('data-key') || el.id;
  if (raw) return String(raw).trim();
  const link = el.querySelector('a[href*="/u/"], a[href*="space.bilibili.com/"]');
  const href = link ? (link.getAttribute('href') || '') : '';
  const m = href.match(/(?:space\.bilibili\.com\/|\/u\/)(\d+)/);
  return m ? m[1] : '';
}

function getConversationId() {
  // 1) 聊天 header 的对方链接（最权威）
  const headerLinks = qsa(`${SEL.PEER_NAME} a[href*="/u/"], ${SEL.PEER_NAME} a[href*="space.bilibili.com/"]`);
  for (const a of headerLinks) {
    const m = (a.getAttribute('href') || '').match(/(?:space\.bilibili\.com\/|\/u\/)(\d+)/);
    if (m && m[1]) return m[1];
  }
  // 2) 活动会话项
  const active = qsa(SEL.CHAT_LIST).find((el) =>
    el.classList && (el.classList.contains('is-active') || el.classList.contains('active') || el.getAttribute('aria-selected') === 'true'));
  if (active) {
    const id = itemId(active);
    if (id) return id;
  }
  // 3) URL query
  try {
    const q = new URLSearchParams(location.search);
    const fromQuery = q.get('uid') || q.get('mid') || q.get('talker_id');
    if (fromQuery) return String(fromQuery);
  } catch (_) {  }
  // 4) 昵称派生
  if (active) {
    const name = itemName(active);
    if (name) return 'conv:' + name.slice(0, 80);
  }
  return null;
}

function getConversationList() {
  const out = [];
  const seen = new Set();
  for (const el of conversationItems()) {
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

function isSystemMessage(item) {
  try {
    return !!item.matches(SEL.SYSTEM);
  } catch (_) {
    return false;
  }
}

function parseMessageItem(item) {
  if (!item) return null;
  if (isSystemMessage(item)) {
    return {
      message_id: item.getAttribute('data-id') || item.id || `sys:${cleanText(item).slice(0, 40)}`,
      sender_type: SENDER.SYSTEM,
      text: cleanText(item),
      media_url: '',
      msg_type: 'system',
      is_group: false,
      group_id: '',
      group_name: '',
      sender_name: '',
      timestamp: Date.now(),
      raw: item.outerHTML ? item.outerHTML.slice(0, 500) : '',
    };
  }
  const textEl = item.querySelector(SEL.TEXT) || item;
  const text = cleanText(textEl);
  const img = item.querySelector('img[src]');
  const mediaUrl = img ? (img.getAttribute('src') || '') : '';
  const msgType = !text && mediaUrl ? 'image' : 'text';
  if (!text && !mediaUrl) return null;
  return {
    message_id: item.getAttribute('data-id') || item.id || `c:${text || mediaUrl}`,
    sender_type: FRONTEND_DEFAULT_SENDER_TYPE,
    text: text || '[图片]',
    media_url: mediaUrl,
    msg_type: msgType,
    is_group: false,
    group_id: '',
    group_name: '',
    sender_name: getPeerName(),
    timestamp: Date.now(),
    raw: item.outerHTML ? item.outerHTML.slice(0, 500) : '',
  };
}

function getMessageItems() {
  const root = chatRoot() || document;
  const m = mergedSelectors();
  for (const sel of m.itemSelectors) {
    try {
      const items = Array.from(root.querySelectorAll(sel)).filter((el) => el.offsetParent !== null || true);
      const filtered = items.filter((it) => !isSystemMessage(it));
      if (filtered.length) return filtered;
    } catch (_) {  }
  }
  // 兜底：线程容器下的直接子元素
  if (root && root !== document) {
    return Array.from(root.children).filter((el) => cleanText(el));
  }
  return [];
}

const hooks = {
  match() {
    if (!isBiliHost()) return false;
    if (looksLikeChatPage()) return true;
    if (isMessageCenterUrl() && findAnyMessageInput() && looksLikeMessagePage()) {
      log.warn('match() 走 fallback 模式：B站选择器未命中，使用通用 DOM 扫描');
      return true;
    }
    return false;
  },
  matchMode() {
    if (!isBiliHost()) return null;
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
  async sendText(text, opts = {}) {
    const input = findInputEl();
    if (!input) throw new Error('bilibili input not found');
    const isEditable = input.isContentEditable || input.getAttribute('contenteditable') === 'true';
    if (isEditable) {
      await fillContentEditableHumanized(input, text, { clearBefore: true });
    } else {
      setValue(input, '');
      setValue(input, text);
    }
    await humanDelay('click', { channel: CHANNELS.BILIBILI, account: getAccountId() });
    assertConvOrAbort(input, opts.assertConv);

    const m = mergedSelectors();
    let btn = null;
    for (const sel of m.sendSelectors) {
      try { btn = qs(sel); if (btn) break; } catch (_) {  }
    }
    if (!btn) btn = qs(SEL.SEND);
    if (btn) {
      try { btn.click(); } catch (_) {  }
    } else {
      log.warn('未找到 B站发送按钮，改用回车发送');
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

export { getAccountId, getConversationId, getConversationList, getPeerName, isBiliHost, isMessageCenterUrl };

export function buildBilibiliAdapter() {
  return new BaseAdapter({
    name: 'bilibili',
    channel: CHANNELS.BILIBILI,
    SEL,
    hooks,
    rateLimiter: undefined,
  });
}
