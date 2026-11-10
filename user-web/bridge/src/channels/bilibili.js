import { BaseAdapter } from '../core/channel-adapter.js';
import { CHANNELS, SENDER } from '../core/types.js';
import {
  qs, qsa, cleanText, setValue, fillContentEditableHumanized,
  createLogger, findAnyMessageInput, looksLikeMessagePage, sanitizePeerName,
  assertConvOrAbort, absoluteMediaUrl,
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
// 2026-11-06 升级为「实证」：来源 ChatGPT-On-CS v1.4.5 官方安装包内嵌
// sourcemap 还原的 src/main/platforms/bilibili/index.ts：
//   会话 div.list-container > div.list-item（未读 .notify.notify-number）；
//   未关注人消息 tab [title='未关注人消息']（回 [title='我的消息']）；
//   会话面板 .dialog（用户名 .title > span），消息 .message-list > .msg-item，
//   方向按 .msg-item class 含 not-me / .message-content class 含 is-me；
//   输入 #editor，发送 [class*='btn-box send-btn']，图片 .image-upload-btn。
// 旧 bundle 线索保留为兜底候选。改版后在 popup 选择器面板覆盖即可。
export const SEL = {
  CHAT_LIST: ".list-container div[class='list-item'], .message-sidebar__item, [class*=\"session-item\" i]",
  MSG_LIST: ".message-list, .message-content__inner, [class*=\"message-list\" i]",
  MSG_ITEM: '.msg-item, .interaction-item__content, [class*="message-item" i]',
  TEXT: '.message-content, .message-text-nodes, [class*="msg-content" i]',
  INPUT: '#editor, .message-reply-box__input, textarea[class*="reply" i]',
  SEND: "[class*='btn-box send-btn'], .message-reply-box__pub, button[class*=\"send\" i]",
  PEER_NAME: ".dialog div[class='title'] > span, .message-header__title, [class*=\"message-header\" i] [class*=\"title\" i]",
  UNREAD: '.notify.notify-number, .message-sidebar__item-num, [class*="unread" i]',
  UNFOLLOWED_TAB: "[title='未关注人消息']",
  MY_MSG_TAB: "[title='我的消息']",
  IMAGE_UPLOAD: '.image-upload-btn, input[type="file"]',
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
  // 优先精确 class="name"（实证 list-item 内 <span class="name">），
  // 再放宽到 [class*="name"]，最后整项文本兜底。
  const nameEl = el.querySelector('.name, [class*="name" i], [class*="title" i], .message-sidebar__item-name');
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
  // 3.5) 会话面板（.dialog）标题昵称（实证：ChatGPT-On-CS 从 .title > span 取名）
  const dialogName = getPeerName();
  if (dialogName) return 'conv:' + dialogName.slice(0, 80);
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

// 方向判定（实证，ChatGPT-On-CS v1.4.5 platforms/bilibili）：
//   .msg-item class 含 not-me → 对方；.message-content class 含 is-me → 自己。
// 判不出时回落 customer（服务端入站回环判定兜底）。
function directionOf(item) {
  try {
    const itemCls = String(item.className || '');
    if (/not-me/i.test(itemCls)) return SENDER.CUSTOMER;
    const content = item.querySelector('.message-content');
    const contentCls = content ? String(content.className || '') : '';
    if (/is-me/i.test(contentCls)) return SENDER.SELF;
    if (/not-me/i.test(contentCls)) return SENDER.CUSTOMER;
  } catch (_) {  }
  return FRONTEND_DEFAULT_SENDER_TYPE;
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
  // 头像过滤（2026-11-10 审计 F4）+ URL 绝对化（F5），与 xhs/xianyu/tiktok 同口径
  const img = Array.from(item.querySelectorAll('img[src]')).find(
    (el) => !(el.closest && el.closest('[class*="avatar" i], [class*="Avatar" i]'))
  ) || null;
  const mediaUrl = absoluteMediaUrl(img ? (img.getAttribute('src') || '') : '');
  const msgType = !text && mediaUrl ? 'image' : 'text';
  if (!text && !mediaUrl) return null;
  const senderType = directionOf(item);
  return {
    message_id: item.getAttribute('data-id') || item.id || `c:${text || mediaUrl}`,
    sender_type: senderType,
    text: text || '[图片]',
    media_url: mediaUrl,
    msg_type: msgType,
    is_group: false,
    group_id: '',
    group_name: '',
    sender_name: senderType === SENDER.CUSTOMER ? getPeerName() : '',
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
        // jsdom 的 innerText 是独立存储（不生成子节点），真浏览器 innerText=''
        // 等价清空；两者都清保证测试与真实环境语义一致。
        try { input.innerText = ''; } catch (_) {  }
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
