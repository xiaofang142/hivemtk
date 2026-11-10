import { BaseAdapter } from '../core/channel-adapter.js';
import { CHANNELS, SENDER } from '../core/types.js';
import {
  qs, qsa, cleanText, setValue, fillContentEditableHumanized,
  createLogger, findAnyMessageInput, looksLikeMessagePage, sanitizePeerName,
  assertConvOrAbort, absoluteMediaUrl, avatarImgUrl,
} from '../core/dom.js';
import { humanDelay } from '../core/humanize.js';
import { mergeSelectors } from '../core/selector-ai.js';
import { FRONTEND_DEFAULT_SENDER_TYPE } from '../core/fallback.js';

const log = createLogger('weibo', CHANNELS.WEIBO);

function mergedSelectors() {
  const fb = {
    itemSelectors: SEL.MSG_ITEM.split(',').map((s) => s.trim()).filter(Boolean),
    listSelectors: SEL.MSG_LIST.split(',').map((s) => s.trim()).filter(Boolean),
    textSelectors: SEL.TEXT.split(',').map((s) => s.trim()).filter(Boolean),
    inputSelectors: SEL.INPUT.split(',').map((s) => s.trim()).filter(Boolean),
    sendSelectors: SEL.SEND.split(',').map((s) => s.trim()).filter(Boolean),
  };
  return mergeSelectors(CHANNELS.WEIBO, fb);
}

// —— 选择器定义 ——
// 2026-11-06 升级为「实证」：来源 ChatGPT-On-CS v1.4.5 官方安装包
// （GitHub Releases，SHA512 已验）内嵌 sourcemap 还原的
// src/main/platforms/weibo/index.ts（文件尾注释原文即下列选择器）。
// 方向判定：气泡 .bubble_cont.basi 的 left=客户 / right=自己；
// 正文 p.puretext，图片/视频走 div.card。#webchat-textarea + Enter 发送
// 与公开 bundle 证据互相印证。
export const SEL = {
  CHAT_LIST: '.chatlist-box .sessionlist, [class*="sessionlist" i]',
  MSG_LIST: 'ul.chat-content, [class*="chat-content" i]',
  MSG_ITEM: '.message-item, [class*="message-item" i]',
  TEXT: 'div>p[class*="puretext" i], .bubble_cont .text, .message-item .text',
  INPUT: '#webchat-textarea, textarea[class*="textarea" i]',
  SEND: '.sendbox_bar button, [class*="send-btn" i], [aria-label*="发送"]',
  PEER_NAME: 'div[class*="friendname" i]>div>span, .chat-header .name, [class*="chat"] [class*="name" i]',
  UNREAD: '[class*="num fff font" i], .chatlist-box [class*="unread" i], [class*="red-dot" i]',
  BUBBLE: '[class*="bubble_cont" i]',
  CARD_IMG: '[class*="card" i]>div>img',
  CARD_VIDEO: '[class*="card" i]>div>video',
};

function isWeiboHost() {
  try {
    return /(^|\.)weibo\.com$/i.test(location.hostname || '');
  } catch (_) {
    return false;
  }
}

function isChatUrl() {
  try {
    return /\/chat/i.test(location.pathname || '') || /\/chat/i.test(location.hash || '');
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

function looksLikeChatPage() {
  return !!(findInputEl() && (chatRoot() || qs(SEL.CHAT_LIST)));
}

function getAccountId() {
  try {
    const cfg = typeof window !== 'undefined' ? window.$CONFIG : null;
    const uid = (cfg && (cfg.uid || (cfg.user && cfg.user.id))) || null;
    if (uid) return String(uid);
  } catch (_) {  }
  const links = qsa('a[href*="/u/"], a[href*="weibo.com/u/"]');
  for (const a of links) {
    const m = (a.getAttribute('href') || '').match(/\/u\/(\d+)/);
    if (m && m[1]) {
      try { localStorage.setItem(`hivebridge:account:${CHANNELS.WEIBO}`, m[1]); } catch (_) {  }
      return m[1];
    }
  }
  try {
    const cached = localStorage.getItem(`hivebridge:account:${CHANNELS.WEIBO}`);
    if (cached) return cached;
  } catch (_) {  }
  return 'weibo-unknown';
}

function convElements() {
  return qsa(SEL.CHAT_LIST);
}

function itemName(el) {
  const nameEl = el.querySelector('[class*="name" i], .nick, .remark');
  return sanitizePeerName(nameEl ? cleanText(nameEl) : cleanText(el));
}

function itemId(el) {
  const raw = el.getAttribute('data-uid') || el.getAttribute('data-id') ||
    el.getAttribute('data-userid') || el.getAttribute('data-key') || el.id;
  if (raw) return String(raw).trim();
  const link = el.querySelector('a[href*="/u/"]');
  const m = link ? (link.getAttribute('href') || '').match(/\/u\/(\d+)/) : null;
  return m ? m[1] : '';
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
    const fromQuery = q.get('uid') || q.get('id') || q.get('talker');
    if (fromQuery) return String(fromQuery);
  } catch (_) {  }
  const header = qs(SEL.PEER_NAME);
  if (header) {
    const link = header.querySelector('a[href*="/u/"]');
    const m = link ? (link.getAttribute('href') || '').match(/\/u\/(\d+)/) : null;
    if (m && m[1]) return m[1];
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
    // M-5：未读徽章（SEL.UNREAD 口径）。供巡检「未读优先」调度使用；
    //   微博徽章 DOM 未经真机校准，未读优先开关默认不对本渠道开启。
    const unread = detectUnread(el);
    out.push({ id, name, el, unread });
  }
  return out;
}

// 未读检测（M-5）：会话项自身带未读 class/红点，或其内部含未读徽章，即视为有新消息。
// 口径 = 本渠道既有 SEL.UNREAD 选择器（与 douyin/xianyu 的 detectUnread 同一模式）。
// 真机校准缺口：微博徽章结构样本不足，检出率未验证（见 M-5 报告）。
function detectUnread(item) {
  if (!item) return false;
  try {
    if (item.matches && item.matches(SEL.UNREAD)) return true;
    if (item.querySelector && item.querySelector(SEL.UNREAD)) return true;
  } catch (_) {  }
  return false;
}

function getPeerName() {
  const el = qs(SEL.PEER_NAME);
  return el ? sanitizePeerName(cleanText(el)) : '';
}

// —— 对方头像（M-2 客户头像链路，2026-11-11，尽力而为渠道）——
// 微博私信结构样本不足，仅抓聊天 header（.chat-header / friendname 同容器口径）头像 img；
// 抓不到保持为空，不报错（真机校准缺口见 M-2 报告）。
function getPeerAvatar() {
  for (const sel of ['[class*="chat-header" i] img', 'div[class*="friendname" i] img', '.chat-header img']) {
    try {
      const u = avatarImgUrl(qs(sel));
      if (u) return u;
    } catch (_) {  }
  }
  return '';
}

// 方向判定（实证）：消息项内气泡 .bubble_cont.basi 的 class 含
// left=对方（客户）/ right=自己（ChatGPT-On-CS v1.4.5 platforms/weibo 实证）。
// 判不出时回落 customer（服务端入站回环判定会兜底）。
function directionOf(item) {
  try {
    const bubble = item.querySelector(SEL.BUBBLE);
    const cls = bubble ? String(bubble.className || '') : '';
    if (/\bleft\b/i.test(cls)) return SENDER.CUSTOMER;
    if (/\bright\b/i.test(cls)) return SENDER.SELF;
  } catch (_) {  }
  return FRONTEND_DEFAULT_SENDER_TYPE;
}

function parseMessageItem(item) {
  if (!item) return null;
  const textEl = item.querySelector(SEL.TEXT);
  let text = textEl ? cleanText(textEl) : '';
  if (!text) text = cleanText(item);
  // 图片/视频优先（实证：div.card > div > img|video），有媒体时文本只作占位。
  // 头像过滤（2026-11-10 审计 F4，与 xhs/xianyu/tiktok 同口径）+ URL 绝对化（F5）：
  // 通用 `img[src]` 兜底会命中发送者头像，不能当消息图片。
  const isAvatarImg = (el) => !!(el.closest && el.closest('[class*="avatar" i], [class*="Avatar" i]'));
  const img = [item.querySelector(SEL.CARD_IMG), ...Array.from(item.querySelectorAll('img[src]'))]
    .find((el) => el && !isAvatarImg(el)) || null;
  const video = item.querySelector(SEL.CARD_VIDEO) || item.querySelector('video[src]');
  const mediaUrl = absoluteMediaUrl((img && img.getAttribute('src')) || (video && video.getAttribute('src')) || '');
  if (!text && !mediaUrl) return null;
  const senderType = directionOf(item);
  return {
    message_id: item.getAttribute('data-id') || item.id || `c:${text || mediaUrl}`,
    sender_type: senderType,
    text: text || '[图片]',
    media_url: mediaUrl,
    msg_type: mediaUrl && !text ? (video ? 'video' : 'image') : 'text',
    is_group: false,
    group_id: '',
    group_name: '',
    sender_name: senderType === SENDER.CUSTOMER ? getPeerName() : '',
    // M-2 客户头像链路：仅客户方向带对方头像（directionOf 判定，与 sender_name 同口径）。
    sender_avatar: senderType === SENDER.CUSTOMER ? getPeerAvatar() : '',
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
    if (!isWeiboHost()) return false;
    if (looksLikeChatPage()) return true;
    if (isChatUrl() && findAnyMessageInput() && looksLikeMessagePage()) {
      log.warn('match() 走 fallback 模式：微博选择器未命中，使用通用 DOM 扫描');
      return true;
    }
    return false;
  },
  matchMode() {
    if (!isWeiboHost()) return null;
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
    if (!input) throw new Error('weibo input not found');
    const isEditable = input.isContentEditable || input.getAttribute('contenteditable') === 'true';
    if (isEditable) {
      await fillContentEditableHumanized(input, text, { clearBefore: true });
    } else {
      setValue(input, '');
      setValue(input, text);
    }
    await humanDelay('click', { channel: CHANNELS.WEIBO, account: getAccountId() });
    assertConvOrAbort(input, opts.assertConv);

    // 微博聊天页以 Enter 发送（页面提示：Enter 发送 / Ctrl+Enter 换行）。
    // 先找按钮（部分版本有），找不到再派发原生键盘事件。
    const m = mergedSelectors();
    let btn = null;
    for (const sel of m.sendSelectors) {
      try { btn = qs(sel); if (btn) break; } catch (_) {  }
    }
    if (btn) {
      try { btn.click(); } catch (_) {  }
    } else {
      for (const type of ['keydown', 'keyup']) {
        input.dispatchEvent(new KeyboardEvent(type, {
          key: 'Enter', code: 'Enter', keyCode: 13, which: 13,
          bubbles: true, cancelable: true, isComposing: false,
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

export { getAccountId, getConversationId, getConversationList, getPeerName, getPeerAvatar, isWeiboHost, isChatUrl, detectUnread };

export function buildWeiboAdapter() {
  return new BaseAdapter({
    name: 'weibo',
    channel: CHANNELS.WEIBO,
    SEL,
    hooks,
    rateLimiter: undefined,
  });
}
