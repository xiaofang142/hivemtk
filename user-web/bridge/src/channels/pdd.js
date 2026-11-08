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
import { mediaPlaceholder } from '../core/types.js';

const log = createLogger('pdd', CHANNELS.PDD);

function mergedSelectors() {
  const fb = {
    itemSelectors: SEL.MSG_ITEM.split(',').map((s) => s.trim()).filter(Boolean),
    listSelectors: SEL.MSG_LIST.split(',').map((s) => s.trim()).filter(Boolean),
    textSelectors: SEL.TEXT.split(',').map((s) => s.trim()).filter(Boolean),
    inputSelectors: SEL.INPUT.split(',').map((s) => s.trim()).filter(Boolean),
    sendSelectors: SEL.SEND.split(',').map((s) => s.trim()).filter(Boolean),
  };
  return mergeSelectors(CHANNELS.PDD, fb);
}

// —— 选择器定义（X 级历史源码实证，未真机校准，Experimental）——
// 来源：ChatGPT-On-CS v1.4.5 静态源码 platforms/pinduoduo（2026-11-06 评估报告 §5.1
// 复读，仅作接口/行为参考，未复制任何源码进本项目）。拼多多商家聊天页在登录墙后
// （mms.pinduoduo.com/chat-merchant/index.html），本方未取到真实 DOM——候选全部来自
// 该 A 源 + 通用 [class*=…] 兜底，16a 真机校准前不宣称生产可用。
// 商家后台同京麦：匹配必须同时满足"会话列表 + 输入框"，否则把搜索框当聊天框。
export const SEL = {
  CHAT_LIST: '.chat-list, [class*="chat-portrait" i], [class*="session-list" i], [class*="conversation-list" i]',
  // A 源：会话入口是 .chat-portrait（头像块），子 i 是未读角标；点击父元素打开会话。
  CONV_ITEM: '.chat-portrait, [class*="session-item" i], [class*="chat-item" i], li[class*="item" i]',
  UNREAD: '.chat-portrait > i, [class*="unread" i], [class*="red-dot" i]',
  // A 源：消息容器 .msg-list，气泡 .onemsg。
  MSG_LIST: '.msg-list, [class*="msg-list" i], [class*="message-list" i], [class*="chat-content" i]',
  MSG_ITEM: '.onemsg, [class*="msg-item" i], [class*="message-item" i], [class*="bubble" i]',
  // A 源：内容在 .msg-content（图片/商品卡/订单卡/lego 卡分型），兜底 .msg-content-box。
  TEXT: '.msg-content, .msg-content-box, [class*="msg-content" i], [class*="message-text" i]',
  // A 源：输入框是 id=replyTextarea 的 textarea。
  INPUT: '#replyTextarea, textarea[class*="reply" i], textarea[class*="input" i], div[contenteditable="true"]',
  // A 源：发送按钮 .reply-footer .send-btn，兜底回车。
  SEND: '.reply-footer .send-btn, button[class*="send" i], [class*="send-btn" i]',
  // A 源：会话身份在右侧 .base-info .remark-row span（商家对买家的备注名）。
  PEER_NAME: '.base-info .remark-row span, [class*="base-info" i] [class*="remark" i] span, [class*="chat-title" i], [class*="header" i] [class*="name" i]',
  SYSTEM: '.system-msg, [class*="system" i], [class*="notice" i]',
  // A 源：客服自己发的消息 .cs-item（买家是 .buyer-item）。自/他判定按既有口径移交
  // 后端（前端统一 customer 占位，回环防护在后端），这里仅用于排除时间线噪音。
  SELF_ITEM: '.cs-item, [class*="cs-item" i]',
  BUYER_ITEM: '.buyer-item, [class*="buyer-item" i]',
};

const PDD_HOSTS = ['pinduoduo.com'];

function isPddHost() {
  try {
    const host = (location.hostname || '').toLowerCase();
    return PDD_HOSTS.some((h) => host === h || host.endsWith('.' + h));
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

// 商家后台有大量输入框（搜索、筛选、备注），必须同时看到"会话列表 + 输入框"才算聊天页。
function looksLikeChatPage() {
  return !!(findInputEl() && hasConversationList());
}

function getAccountId() {
  const metas = qsa('meta[name*="mall" i], meta[name*="shop" i], meta[name*="user" i], meta[name*="uin" i]');
  for (const meta of metas) {
    const content = (meta.getAttribute('content') || '').trim();
    if (content) {
      try { localStorage.setItem(`hivebridge:account:${CHANNELS.PDD}`, content); } catch (_) {  }
      return content;
    }
  }
  const el = qs('[data-mall-id], [data-mallid], [data-shop-id], [data-user-id]');
  if (el) {
    const id = el.getAttribute('data-mall-id') || el.getAttribute('data-mallid') ||
      el.getAttribute('data-shop-id') || el.getAttribute('data-user-id');
    if (id) return String(id).trim();
  }
  try {
    const cached = localStorage.getItem(`hivebridge:account:${CHANNELS.PDD}`);
    if (cached) return cached;
  } catch (_) {  }
  return 'pdd-unknown';
}

function convElements() {
  const items = qsa(SEL.CONV_ITEM);
  if (items.length) return items;
  // 兜底：容器本身没有 item 类名时，取容器的直接子元素（A 源形态是头像块列表）。
  const out = [];
  for (const box of qsa(SEL.CHAT_LIST)) {
    for (const child of box.children) out.push(child);
  }
  return out;
}

function itemName(el) {
  const nameEl = el.querySelector('[class*="name" i], [class*="title" i], [class*="remark" i], [class*="nick" i]');
  return sanitizePeerName(nameEl ? cleanText(nameEl) : cleanText(el));
}

function itemId(el) {
  const raw = el.getAttribute('data-session-id') || el.getAttribute('data-uid') ||
    el.getAttribute('data-mall-id') || el.getAttribute('data-id') ||
    el.getAttribute('data-key') || el.id;
  return raw ? String(raw).trim() : '';
}

function getConversationId() {
  const active = convElements().find((el) =>
    el.classList && (el.classList.contains('active') || el.classList.contains('selected') ||
      el.classList.contains('current') || el.getAttribute('aria-selected') === 'true'));
  if (active) {
    const id = itemId(active);
    if (id) return id;
  }
  try {
    const q = new URLSearchParams(location.search);
    const fromQuery = q.get('sessionId') || q.get('session_id') || q.get('uid') ||
      q.get('mallid') || q.get('buyer_id');
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

function isSystemMessage(item) {
  try {
    if (item.matches(SEL.SYSTEM)) return true;
    if (item.querySelector && item.querySelector(SEL.SYSTEM)) return true;
  } catch (_) {  }
  return false;
}

function extractContent(item) {
  // A 源分型：.msg-content 内按 class 分图片/商品卡/订单卡/lego 卡；先给文本兜底，
  // 媒体/卡片消息统一占位（与既有渠道同一占位词表：占位文案 + msg_type 标注，
  // 卡片/订单上下文本体不拼进 content——16d 专属实现前降级不可发送）。
  const img = item.querySelector('.msg-content img[src], img[src]');
  const mediaUrl = img ? (img.getAttribute('src') || '') : '';
  if (item.querySelector('.image-msg, [class*="image-msg" i]')) {
    return { text: mediaPlaceholder('image'), mediaUrl, msgType: 'image' };
  }
  if (item.querySelector('.good-card, .order-card, .lego-card, [class*="good-card" i], [class*="order-card" i], [class*="lego-card" i]')) {
    // 未知卡片降级：占位 + 类型标注，卡片文本（商品/订单数据）不冒充买家发言。
    return { text: mediaPlaceholder('card'), mediaUrl, msgType: 'card' };
  }
  let textEl = null;
  try {
    textEl = item.querySelector(SEL.TEXT);
  } catch (_) {  }
  const text = cleanText(textEl || item);
  return { text, mediaUrl, msgType: 'text' };
}

function parseMessageItem(item) {
  if (!item) return null;
  if (isSystemMessage(item)) {
    const sysText = cleanText(item);
    if (!sysText) return null;
    return {
      message_id: item.getAttribute('data-id') || item.id || `sys:${sysText}`,
      sender_type: SENDER.SYSTEM,
      text: sysText,
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
  const { text, mediaUrl, msgType } = extractContent(item);
  if (!text && !mediaUrl) return null;
  // 自/他判定按既有口径移交后端（统一 customer 占位，回环防护由后端承担）。
  const sender_type = FRONTEND_DEFAULT_SENDER_TYPE;
  const mid =
    item.getAttribute('data-id') ||
    item.getAttribute('data-msg-id') ||
    item.id ||
    `c:${text || mediaUrl}`;
  return {
    message_id: mid,
    sender_type,
    text,
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
      const items = Array.from(root.querySelectorAll(sel));
      if (items.length) return items;
    } catch (_) {  }
  }
  if (root && root !== document) return Array.from(root.children).filter((el) => cleanText(el));
  return [];
}

const hooks = {
  match() {
    if (!isPddHost()) return false;
    if (looksLikeChatPage()) return true;
    if (findInputEl() && looksLikeMessagePage() && hasConversationList()) {
      log.warn('match() 走 fallback 模式：拼多多选择器未命中，使用通用 DOM 扫描');
      return true;
    }
    return false;
  },
  matchMode() {
    if (!isPddHost()) return null;
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
    if (!input) throw new Error('pdd input not found');
    const isEditable = input.isContentEditable || input.getAttribute('contenteditable') === 'true';
    if (isEditable) {
      await fillContentEditableHumanized(input, text, { clearBefore: true });
    } else {
      setValue(input, '');
      setValue(input, text);
    }
    await humanDelay('click', { channel: CHANNELS.PDD, account: getAccountId() });
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
      log.warn('未找到拼多多发送按钮，改用回车发送（A 源即回车兜底）');
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

export { getAccountId, getConversationId, getConversationList, getPeerName, isPddHost };

export function buildPddAdapter() {
  return new BaseAdapter({
    name: 'pdd',
    channel: CHANNELS.PDD,
    SEL,
    hooks,
    rateLimiter: undefined,
  });
}
