import { BaseAdapter } from '../core/channel-adapter.js';
import { CHANNELS, SENDER } from '../core/types.js';
import { qs, qsa, cleanText, fillContentEditableHumanized, simulateRealClick, createLogger, findAnyMessageInput, looksLikeMessagePage, sanitizePeerName } from '../core/dom.js';
import { humanDelay } from '../core/humanize.js';
import { SelectorEngine } from '../core/selector-engine.js';
import { mergeSelectors } from '../core/selector-ai.js';
import { FRONTEND_DEFAULT_SENDER_TYPE } from '../core/fallback.js';

const log = createLogger('kuaishou', CHANNELS.KUAISHOU);

// 合并选择器：用户配置优先，SEL 默认兜底。
function mergedSelectors() {
  const fb = {
    itemSelectors: SEL.MSG_ITEM.split(',').map((s) => s.trim()).filter(Boolean),
    listSelectors: SEL.MSG_LIST.split(',').map((s) => s.trim()).filter(Boolean),
    textSelectors: SEL.TEXT.split(',').map((s) => s.trim()).filter(Boolean),
    inputSelectors: SEL.INPUT.split(',').map((s) => s.trim()).filter(Boolean),
    sendSelectors: SEL.SEND.split(',').map((s) => s.trim()).filter(Boolean),
  };
  return mergeSelectors(CHANNELS.KUAISHOU, fb);
}

// —— 选择器定义（快手网页 IM 基础选择器）——
// 快手私信页 URL 模式: https://www.kuaishou.com/new-reco 或 /message/**
// DOM 结构基于 2026-08 观察，平台改版后可通过 UI 配置面板覆盖。
export const SEL = {
  CHAT_LIST: '[class*="conversation-list" i], [class*="ConvList" i], [class*="chat-list" i], [class*="dialog-list" i]',
  MSG_LIST: '[class*="message-list" i], [class*="msg-list" i], [class*="chat-content" i], [class*="im-content" i]',
  MSG_ITEM: '[class*="message-item" i], [class*="msg-item" i], [class*="bubble" i], [class*="chat-msg" i]',
  TEXT: '[class*="text-content" i], [class*="msg-text" i], [class*="bubble-text" i], [class*="content-text" i]',
  INPUT: 'div[contenteditable="true"][class*="input" i], div[contenteditable="true"][class*="editor" i], textarea[class*="input" i]',
  SEND: '[class*="send-btn" i], [class*="send-button" i], [aria-label*="发送" i], button[class*="send" i]',
  MSG_TYPE: '[data-msg-type], [data-type]',
  CARD: '[class*="card" i]',
  CONV_ITEM: '[class*="conversation-item" i], [class*="conv-item" i], [class*="dialog-item" i]',
  SYSTEM: '[class*="system-msg" i], [class*="notice" i], [class*="time" i], [class*="divider" i]',
  PEER_NAME: '[class*="chat-header" i] [class*="name" i], [class*="header-name" i], [class*="dialog-name" i]',
  UNREAD: '[class*="unread" i], [class*="badge" i], [data-unread]',
  EDITOR: 'div[contenteditable="true"][class*="input" i], div[contenteditable="true"][class*="editor" i]',
};

// 输入框
function editorBox() {
  const m = mergedSelectors();
  for (const sel of m.inputSelectors) {
    try {
      const el = qs(sel);
      if (el) return el;
    } catch (_) {}
  }
  return qs(SEL.INPUT) || qs(SEL.EDITOR);
}

function findInputEl() {
  return editorBox() || findAnyMessageInput();
}

// 发送按钮
function getRealSendButton() {
  const m = mergedSelectors();
  for (const sel of m.sendSelectors) {
    try {
      const el = qs(sel);
      if (el) {
        if (el.tagName === 'SVG') return el.closest('button, [role="button"], div, span') || el;
        return el;
      }
    } catch (_) {}
  }
  return qs('[class*="send"], [aria-label*="发送"]') || null;
}

// 当前登录账号 id
function getAccountId() {
  // B8（2026-09-19）：旧实现取路径末段任意 ≥6 字符串——私信默认路径 /new-reco
  // 本身就是 8 字符，渠道名被永久误判成账号 id 并进 localStorage。快手网页携带
  // 用户 id 的 URL 形态只有 /profile/<userId>，其余路径不可信、不猜。
  const pathMatch = location.pathname.match(/\/profile\/([^/?#]+)/);
  if (pathMatch && pathMatch[1]) {
    try { localStorage.setItem(`hivebridge:account:${CHANNELS.KUAISHOU}`, pathMatch[1]); } catch (_) {}
    return pathMatch[1];
  }
  // 尝试从 localStorage 缓存恢复
  try {
    const cached = localStorage.getItem(`hivebridge:account:${CHANNELS.KUAISHOU}`);
    if (cached) return cached;
  } catch (_) {}
  // 2026-08-17 治本：DOM 兜底失败时返回空串（不返回 kuaishou-unknown 占位值）。
  // types.js buildMessage 在 account_id 为空时 delete 该字段，后端层0 改用
  // (platform+sender_name+content) 三元组命中 outbound，避免占位值污染入库链路。
  return '';
}

// 消息内容提取
function extractMessageContent(item) {
  const text = cleanText(item.querySelector(SEL.TEXT) || item);
  // 纯时间戳过滤
  if (text && /^\d{1,2}:\d{2}$/.test(text)) {
    return { msgType: 'text', mediaUrl: '', text: '' };
  }
  const imgs = item.querySelectorAll('img');
  const vids = item.querySelectorAll('video, [class*="video"]');
  const sysTxt = text || '';
  if (/撤回了?一条消息|recalled/i.test(sysTxt)) {
    return { msgType: 'recall', mediaUrl: '', text: sysTxt };
  }
  let mediaUrl = '';
  let msgType = 'text';
  if (vids.length) {
    msgType = 'video';
    mediaUrl = vids[0].getAttribute('src') || '';
  } else if (imgs.length) {
    msgType = 'image';
    mediaUrl = imgs[0].getAttribute('src') || '';
  }
  return { msgType, mediaUrl, text };
}

// 系统消息识别
const SYSTEM_TEXT_PATTERNS = [
  /你(已)?添加.*为好友/,
  /已添加.*为好友/,
  /撤回了?一条消息/,
  /recalled a message/i,
  /对方正在输入/,
  /typing/i,
  /以下为新消息/,
  /消息发送失败/,
];

function isSystemText(text) {
  if (!text) return false;
  return SYSTEM_TEXT_PATTERNS.some((re) => re.test(text));
}

function isSystemMessage(item) {
  if (!item) return false;
  try {
    if (item.matches && item.matches(SEL.SYSTEM)) return true;
    if (item.querySelector && item.querySelector(SEL.SYSTEM)) return true;
  } catch (_) {}
  const text = cleanText(item);
  if (isSystemText(text)) return true;
  return false;
}

// 聊天对象昵称
function getPeerName() {
  for (const sel of String(SEL.PEER_NAME || '').split(',').map((s) => s.trim()).filter(Boolean)) {
    try {
      const el = qs(sel);
      if (el) {
        const t = cleanText(el);
        if (t && !/私信|消息|聊天|快手/i.test(t)) return t;
      }
    } catch (_) {}
  }
  return '';
}

// 群聊识别
function detectGroup(item) {
  let isGroup = false;
  let groupId = '';
  let groupName = '';
  let senderName = '';
  const headerText = getPeerName() || '';
  if (headerText && /群|组|team|group/i.test(headerText)) {
    isGroup = true;
    groupName = headerText;
  }
  const inner = cleanText(item);
  const atMatch = inner.match(/^@([^\s,，：:]+)[\s,，：:]/);
  if (atMatch) {
    isGroup = true;
    senderName = atMatch[1] || '';
  }
  return { isGroup, groupId, groupName, senderName };
}

// 当前会话 id
function getConversationId() {
  const active = qsa('[class*="conversation-item"][class*="active"], [class*="conv-item"][class*="active"], [class*="dialog-item"][class*="active"], [aria-selected="true"]')
    .find((el) => el.offsetParent !== null);
  if (active) {
    const dataConv = active.getAttribute('data-conversation-id') || active.getAttribute('data-id') || active.getAttribute('data-conv-id');
    if (dataConv) return dataConv;
    const nameEl = active.querySelector('[class*="name" i], [class*="title" i]');
    const raw = nameEl ? cleanText(nameEl) : cleanText(active);
    const name = sanitizePeerName(raw);
    if (name && name !== 'self') return 'conv:' + name.slice(0, 80);
  }
  return null;
}

// 会话列表枚举
function getConversationList() {
  const items = qsa('[class*="conversation-item" i], [class*="conv-item" i], [class*="dialog-item" i]');
  if (!items.length) return [];
  const out = [];
  const ids = new Set();
  for (const item of items) {
    if (!item || !item.offsetParent) continue;
    let id = item.getAttribute('data-conversation-id') || item.getAttribute('data-id') || null;
    const nameEl = item.querySelector('[class*="name" i], [class*="title" i], [class*="nickname" i]');
    const name = sanitizePeerName(nameEl ? cleanText(nameEl) : '');
    if (!id) {
      if (name && name !== 'self') id = 'conv:' + name.slice(0, 80);
      else continue;
    }
    if (ids.has(id)) continue;
    ids.add(id);
    const unread = !!item.querySelector('[class*="unread" i], [class*="badge" i]');
    out.push({ id, name, el: item, unread });
  }
  return out;
}

// 快手 IM 页面检测
function isKuaishouMessagePage() {
  // 快手私信在 /new-reco 等页面以浮层形式存在
  if (!location.hostname.includes('kuaishou.com')) return false;
  const hasEditor = !!document.querySelector('div[contenteditable="true"][class*="input"], div[contenteditable="true"][class*="editor"], textarea[class*="input"]');
  const hasConvList = !!document.querySelector('[class*="conversation-item"], [class*="dialog-item"]');
  const hasSend = !!document.querySelector('[class*="send"], [aria-label*="发送"]');
  if (hasEditor && hasConvList) return true;
  if (hasEditor && hasSend) return true;
  return false;
}

const hooks = {
  match() {
    if (!location.hostname.includes('kuaishou.com')) return false;
    if (isKuaishouMessagePage()) return true;
    if (editorBox()) return true;
    if (findAnyMessageInput() && looksLikeMessagePage()) {
      log.warn('match() 走 fallback 模式：快手选择器失效，使用通用 DOM 扫描');
      return true;
    }
    return false;
  },
  matchMode() {
    if (!location.hostname.includes('kuaishou.com')) return null;
    if (isKuaishouMessagePage() || editorBox()) return 'strict';
    return 'fallback';
  },
  selectors: SEL,
  getMessageListRoot() {
    const root = qs(SEL.MSG_LIST);
    if (root) return root;
    // 兜底：输入框的最近可滚动祖先
    const anchor = editorBox() || qs(SEL.CHAT_LIST);
    if (anchor) {
      let cur = anchor;
      while (cur && cur !== document.body) {
        const style = getComputedStyle(cur);
        if (style.overflowY === 'auto' || style.overflowY === 'scroll' || cur.scrollHeight > cur.clientHeight + 50) return cur;
        cur = cur.parentElement;
      }
    }
    return null;
  },
  getMessageItems() {
    const m = mergedSelectors();
    const { items } = SelectorEngine.locateMessages({
      root: document,
      itemSelectors: m.itemSelectors,
      listSelectors: m.listSelectors,
    });
    const filtered = items.filter((it) => !isSystemMessage(it));
    if (filtered.length) return filtered;
    // 兜底
    const thread = this.getMessageListRoot();
    if (!thread) return [];
    const leafText = [];
    const walk = (el, depth) => {
      if (depth > 3) return;
      for (const child of el.children) {
        const txt = cleanText(child);
        if (txt && child.offsetParent !== null) {
          leafText.push(child);
        } else {
          walk(child, depth + 1);
        }
      }
    };
    walk(thread, 0);
    return leafText.filter((el) => !isSystemMessage(el));
  },
  parseMessageItem(item) {
    if (isSystemMessage(item)) {
      const sysText = cleanText(item);
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
        raw: item.outerHTML?.slice(0, 500),
      };
    }
    const { msgType, mediaUrl, text } = extractMessageContent(item);
    if (!text && msgType === 'text') return null;
    const sender_type = FRONTEND_DEFAULT_SENDER_TYPE;
    const groupInfo = detectGroup(item);
    let senderName = groupInfo.senderName || '';
    if (!senderName && !groupInfo.isGroup && sender_type === SENDER.CUSTOMER) {
      senderName = getPeerName();
    }
    const mid = item.getAttribute('data-id') || item.id || `c:${text}`;
    return {
      message_id: mid,
      sender_type,
      text,
      media_url: mediaUrl,
      msg_type: msgType,
      is_group: groupInfo.isGroup,
      group_id: groupInfo.groupId,
      group_name: groupInfo.groupName,
      sender_name: senderName,
      timestamp: Date.now(),
      raw: item.outerHTML?.slice(0, 500),
    };
  },
  getAccountId,
  getConversationId,
  getConversationList,
  mergedSelectors,
  getPeerName,
  async sendText(text) {
    const editor = findInputEl();
    if (!editor) {
      log.error('未找到快手输入框');
      throw new Error('kuaishou input not found');
    }
    await fillContentEditableHumanized(editor, text);
    await humanDelay('click', { channel: CHANNELS.KUAISHOU, account: getAccountId() });
    const btn = getRealSendButton();
    if (!btn) {
      log.error('未找到快手发送按钮');
      throw new Error('kuaishou send button not found');
    }
    simulateRealClick(btn);
  },
};

export { getAccountId, getConversationId, getConversationList, getPeerName, isSystemMessage, isKuaishouMessagePage };

export function buildKuaishouAdapter() {
  return new BaseAdapter({
    name: 'kuaishou',
    channel: CHANNELS.KUAISHOU,
    SEL,
    hooks,
    rateLimiter: undefined,
  });
}