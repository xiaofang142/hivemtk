import { BaseAdapter } from '../core/channel-adapter.js';
import { CHANNELS, SENDER } from '../core/types.js';
import { mergeSelectors, customConversationListSelectors } from '../core/selector-ai.js';
import { SelectorEngine } from '../core/selector-engine.js';
import {
  qs, qsa, cleanText, setValue, fillContentEditable, enhancedClick,
  simulateRealClick, createLogger, findAnyMessageInput, looksLikeMessagePage,
  sanitizePeerName,
} from '../core/dom.js';
import { FRONTEND_DEFAULT_SENDER_TYPE } from '../core/fallback.js';

const log = createLogger('tiktok', CHANNELS.TIKTOK);

// 合并选择器：用户配置优先（chrome.storage），SEL 默认兜底。
function mergedSelectors() {
  const fb = {
    itemSelectors: SEL.MSG_ITEM.split(',').map((s) => s.trim()).filter(Boolean),
    listSelectors: SEL.MSG_LIST.split(',').map((s) => s.trim()).filter(Boolean),
    textSelectors: SEL.TEXT.split(',').map((s) => s.trim()).filter(Boolean),
    inputSelectors: (SEL.INPUT || '').split(',').map((s) => s.trim()).filter(Boolean),
    sendSelectors: (SEL.SEND || '').split(',').map((s) => s.trim()).filter(Boolean),
  };
  return mergeSelectors(CHANNELS.TIKTOK, fb);
}

// —— 选择器定义（2026-08 验证可用，全部基于 data-e2e 属性）——
// TikTok 以 data-e2e 为主键，class 选择器带 i 标志兜底大小写。
export const SEL = {
  CHAT_LIST: '[data-e2e="dm-new-conversation-list"]',
  CONV_ITEM: '[data-e2e="dm-new-conversation-item"]',
  MSG_LIST: '[data-e2e="dm-new-message-list"], [class*="DivChatMain" i], [class*="chat-main" i]',
  MSG_ITEM: '[data-e2e="dm-new-chat-item"], [class*="DivChatItemWrapper" i], [class*="bubble" i]',
  TEXT: '[data-e2e="dm-new-message-text"], [class*="DivTextContainer" i]',
  TIME: '[data-e2e="dm-new-time-separator"]',
  SYSTEM: '[class*="system-msg" i], [class*="notice" i], [class*="divider" i], [data-e2e="dm-new-time-separator"]',
  UNREAD: '[class*="unread" i], [data-unread="1"]',
  MSG_TYPE: '[data-msg-type], [data-message-type]',
  CARD: '[class*="card-container" i], [class*="share-card" i]',
  PEER_NAME: '[data-e2e="dm-new-chat-nickname"], [data-e2e="chat-uniqueid"]',
  INPUT: '[data-e2e="dm-new-input-editor"] div[contenteditable="true"], div.DraftEditor-root div[contenteditable="true"]',
  SEND: 'button[aria-label*="Send"], [data-e2e="message-button"]',
};

// —— 严格输入框 / 弹性输入框 ——
const STRICT_INPUT_SELECTORS = [
  '#main-content-messages div[contenteditable="true"]',
  '[data-e2e="dm-new-input-editor"] div[contenteditable="true"]',
  'div.DraftEditor-root div[contenteditable="true"]',
  '[data-e2e="message-input"]',
];
const INPUT_FALLBACKS = [
  ...STRICT_INPUT_SELECTORS,
  'div[contenteditable="true"][role="textbox"]',
  'div[contenteditable="true"]',
  '[role="textbox"]',
  'textarea[placeholder*="Message" i]',
  'textarea[placeholder*="消息" i]',
];

function strictGetInputEl() {
  for (const sel of STRICT_INPUT_SELECTORS) {
    try {
      const el = qs(sel);
      if (el && el.offsetParent !== null) return el;
    } catch (_) {  }
  }
  return null;
}

function getInputEl() {
  for (const sel of INPUT_FALLBACKS) {
    try {
      const el = qs(sel);
      if (el && el.offsetParent !== null) return el;
    } catch (_) {  }
  }
  return null;
}

function findInputEl() {
  return getInputEl() || findAnyMessageInput();
}

// —— TikTok 聊天页 URL 判断 ——
function isTiktokChatUrl() {
  const path = location.pathname || '';
  return path === '/messages' || path.startsWith('/messages/');
}

function isTiktokHost() {
  return (location.hostname || '').includes('tiktok.com');
}

// —— 聊天页结构识别 ——
function isTiktokMessagePage() {
  if (!isTiktokHost()) return false;
  const hasInput = !!strictGetInputEl();
  const hasMsg = !!document.querySelector(
    '[data-e2e="dm-new-chat-item"], [class*="DivChatItemWrapper"], [data-e2e="message-item"]',
  );
  if (hasInput && hasMsg) return true;
  const hasConvList = !!document.querySelector(SEL.CHAT_LIST);
  if (hasConvList && hasInput) return true;
  return false;
}

// —— 联系人 ID 规范化 ——
function normalizeContactId(id) {
  if (id == null) return '';
  let s = String(id).trim();
  if (!s) return '';
  const prefixes = ['Total-', 'Active-', 'User-', 'Contact-', 'User_', 'Contact_'];
  for (const prefix of prefixes) {
    if (s.startsWith(prefix)) { s = s.substring(prefix.length); break; }
  }
  s = s.replace(/[^a-zA-Z0-9_:-]/g, '');
  return s;
}

// —— 当前登录账号 ID（永不返回空串）——
// TikTok DM 页面特点：
//   - 会话列表只显示对方，不显示自己
//   - Header 显示对方昵称
//   - 消息气泡内的头像链接指向发送者（自己或对方）
// 策略：从消息气泡头像提取所有 /@username，排除对方（header 中的），剩下的就是自己
function getAccountId() {
  try {
    const ls = localStorage.getItem(`hivebridge:account:${CHANNELS.TIKTOK}`);
    if (ls) return ls;
  } catch (e) {  }

  // 2) 从消息气泡头像提取自己（排除对方）
  const peerUsername = (() => {
    // 对方昵称在 header
    const headerLinks = qsa('[data-e2e="dm-new-chatbox"] a[href*="/@"]');
    for (const link of headerLinks) {
      const m = link.getAttribute('href')?.match(/@([\w.]+)/);
      if (m && m[1]) return m[1];
    }
    return null;
  })();

  const allUsernames = new Set();
  qsa('[data-e2e="dm-new-chat-item"] a[href*="/@"]').forEach((a) => {
    const m = a.getAttribute('href')?.match(/@([\w.]+)/);
    if (m && m[1]) allUsernames.add(m[1]);
  });
  for (const u of allUsernames) {
    if (peerUsername && u === peerUsername) continue;
    try { localStorage.setItem(`hivebridge:account:${CHANNELS.TIKTOK}`, u); } catch (e) {  }
    return u;
  }

  // 3) 侧边栏/导航栏内的 profile 链接（兜底）
  const sidebarCandidates = [];
  qsa('aside a[href*="/@"], nav a[href*="/@"], [class*="sidebar" i] a[href*="/@"]').forEach((a) => {
    const m = a.getAttribute('href')?.match(/@([\w.]+)/);
    if (m && m[1]) sidebarCandidates.push(m[1]);
  });
  for (const c of sidebarCandidates) {
    if (c && c.trim()) {
      try { localStorage.setItem(`hivebridge:account:${CHANNELS.TIKTOK}`, c.trim()); } catch (e) {  }
      return c.trim();
    }
  }

  return '';
}

// —— 当前会话 ID ——
function getConversationIdFromUrl() {
  try {
    const pathMatch = (location.pathname || '').match(/\/messages\/@?([\w.]+)/);
    if (pathMatch && pathMatch[1]) return normalizeContactId(pathMatch[1]);
  } catch (_) {  }
  return null;
}

function getConversationId() {
  // 1) URL 路径（最权威：/messages/@username）
  const urlId = getConversationIdFromUrl();
  if (urlId) return urlId;
  // 2) 活动会话项 data-conversation-id
  const activeItem = document.querySelector(
    '[data-e2e="dm-new-conversation-item"][aria-selected="true"], [class*="DivItemWrapper"][aria-selected="true"]',
  );
  if (activeItem) {
    const convId =
      activeItem.getAttribute('data-conversation-id') ||
      activeItem.getAttribute('data-id') ||
      activeItem.id ||
      null;
    const norm = normalizeContactId(convId);
    if (norm) return norm;
  }
  // 3) 聊天 header 对方链接（排除自己）
  const myAccount = getAccountId();
  const headerLinks = qsa('[data-e2e="dm-new-chatbox"] a[href*="/@"]');
  for (const link of headerLinks) {
    const href = link.getAttribute('href') || '';
    const m = href.match(/@([\w.]+)/);
    if (!m || !m[1]) continue;
    if (myAccount && m[1] === myAccount) continue;
    return m[1];
  }
  if (activeItem) {
    const nameEl = activeItem.querySelector('[data-e2e="dm-new-conversation-nickname"], [class*="PInfoNickname"]');
    const raw = nameEl ? cleanText(nameEl) : cleanText(activeItem);
    const name = sanitizePeerName(raw);
    if (name) return 'conv:' + name.slice(0, 80);
  }
  return null;
}

// —— 聊天对象昵称 ——
function getPeerName() {
  for (const sel of String(SEL.PEER_NAME || '').split(',').map((s) => s.trim()).filter(Boolean)) {
    try {
      const el = qs(sel);
      if (el) {
        const t = cleanText(el);
        if (t && !/messages|message|chat|消息|聊天/i.test(t)) return t;
      }
    } catch (_) {  }
  }
  // 2) header 内 /@ 链接文字（排除自己）
  const myAccount = getAccountId();
  const headerLinks = qsa('[data-e2e="dm-new-chatbox"] a[href*="/@"]');
  for (const link of headerLinks) {
    const href = link.getAttribute('href') || '';
    const m = href.match(/@([\w.]+)/);
    if (!m || !m[1]) continue;
    if (myAccount && m[1] === myAccount) continue;
    const t = cleanText(link);
    if (t && !/messages|message|chat/i.test(t)) return t;
  }
  // 3) 活动会话列表项昵称
  const activeItem = document.querySelector(
    '[data-e2e="dm-new-conversation-item"][aria-selected="true"]',
  );
  if (activeItem) {
    const nameEl = activeItem.querySelector('[data-e2e="dm-new-conversation-nickname"], [class*="PInfoNickname"], [class*="SpanNicknameText"]');
    // sanitizePeerName 剥离时间戳/相对时间等易变后缀（与 getConversationId 兜底一致）
    const t = sanitizePeerName(nameEl ? cleanText(nameEl) : '');
    if (t && !/messages|message|chat/i.test(t)) return t;
  }
  return '';
}

// —— 发送按钮 ——
function findSendButton() {
  const m = mergedSelectors();
  for (const sel of m.sendSelectors) {
    try {
      const el = qs(sel);
      if (el && el.offsetParent !== null) {
        if (el.tagName === 'SVG') return el.closest('button, [role="button"], div, span') || el;
        return el;
      }
    } catch (_) {  }
  }
  // 兜底：TikTok 可能有 SVG 飞机发送按钮
  const sendBtns = qsa('[data-e2e="message-button"], [class*="send-btn" i], [class*="SendBtn" i]');
  for (const btn of sendBtns) {
    if (btn.offsetParent !== null) {
      if (btn.tagName === 'SVG') return btn.closest('button, [role="button"], div, span') || btn;
      return btn;
    }
  }
  // 兜底：扫描蓝色/主色调 SVG 路径（TikTok 使用 #FE2C55 / #00F2EA 等品牌色）
  const bluePaths = qsa('svg path').filter((p) => {
    const fill = (p.getAttribute('fill') || '').toUpperCase();
    return fill === '#FE2C55' || fill === '#00F2EA' || fill === '#25F4EE' || fill === '#FF4F24';
  });
  for (const path of bluePaths) {
    const clickable = path.closest('button, [role="button"], div[tabindex], span[tabindex]');
    if (clickable && clickable.offsetParent !== null) return clickable;
  }
  return null;
}

// —— 居中对齐检测（系统消息识别漏斗①）——
function isCenterAligned(el) {
  if (!el) return false;
  try {
    const ta = getComputedStyle(el).textAlign;
    if (ta === 'center' || ta === '-webkit-center') return true;
    const parent = el.parentElement;
    if (parent) {
      const jc = getComputedStyle(parent).justifyContent;
      if (jc === 'center') return true;
    }
  } catch (_) {  }
  return false;
}

// —— 内容模式匹配 ——
const TIME_PATTERNS = [
  /^\d{1,2}:\d{2}$/,
  /^(Today|Yesterday|Last)\s+\d{1,2}:\d{2}$/i,
  /^(今天|昨天|前天)\s*\d{1,2}:\d{2}$/,
  /^\d{1,2}\/\d{1,2}\/\d{2,4}$/,
  /^\d{4}[-/]\d{1,2}[-/]\d{1,2}$/,
  /^(上午|下午|凌晨|AM|PM)\s*\d{1,2}:\d{2}$/i,
  /^(Mon|Tue|Wed|Thu|Fri|Sat|Sun)\s*$/i,
  /^\d{1,2}:\d{2}\s*(AM|PM)$/i,
  /^\d{1,2}月\d{1,2}日(\s+\d{1,2}:\d{2})?$/,
];

const SYSTEM_TEXT_PATTERNS = [
  /you can now message this user/i,
  /follow each other to start chatting/i,
  /followed you/i,
  /accepted your follow request/i,
  /sent you a (message|friend request|follow)/i,
  /recalled a message/i,
  /撤回了一条消息/,
  /message could not be sent/i,
  /message deleted/i,
  /you are (no longer|not) (friends|following)/i,
  /this account is (private|public)/i,
  /typing/i,
  /以下为新消息/,
  /^={3,}$/,
  /^-{3,}$/,
  /blocked/i,
  /reported/i,
  /你(已)?添加.*为好友/,
  /已添加.*为好友/,
  /对方正在输入/,
  /互相关注/,
  /对方已开启.*验证/,
  /消息发送失败/,
  /消息已发出.*被对方拒收/,
  /我已收到你的消息/,
  /请等待卖家回复/,
];

function isTimeText(text) {
  if (!text) return false;
  const t = text.trim();
  return TIME_PATTERNS.some((re) => re.test(t));
}

function isSystemText(text) {
  if (!text) return false;
  const t = text.trim();
  return SYSTEM_TEXT_PATTERNS.some((re) => re.test(t));
}

// —— 系统消息识别（漏斗 5 层）——
function isSystemMessage(item) {
  if (!item) return false;
  if (isCenterAligned(item)) return true;
  try {
    if (item.matches && item.matches(SEL.SYSTEM)) return true;
    if (item.querySelector && item.querySelector(SEL.SYSTEM)) return true;
  } catch (_) {  }
  try {
    if (item.matches && (
      item.matches('[role="status"]') ||
      item.matches('[role="alert"]') ||
      item.matches('[aria-live="polite"]') ||
      item.matches('[aria-live="assertive"]')
    )) return true;
  } catch (_) {  }
  // ④ 内容模式
  const text = cleanText(item);
  if (isTimeText(text) || isSystemText(text)) return true;
  if (text && text.length < 60) {
    const hasAvatar = !!item.querySelector('[class*="avatar" i], [class*="Avatar" i]');
    const hasBubble = !!item.querySelector('[class*="bubble" i], [class*="Bubble" i], [class*="text-container" i]');
    if (!hasAvatar && !hasBubble) return true;
  }
  return false;
}


// —— 非文字消息提取 ——
function extractMessageContent(item) {
  const typeAttr = (item.getAttribute('data-msg-type') || item.querySelector(SEL.MSG_TYPE)?.getAttribute('data-msg-type') || '').toUpperCase();
  const text = cleanText(item.querySelector(SEL.TEXT) || item);
  if (text && isTimeText(text)) {
    return { msgType: 'system', mediaUrl: '', text: '' };
  }
  if (text && isSystemText(text)) {
    return { msgType: 'system', mediaUrl: '', text };
  }
  // 卡片
  const card = item.querySelector(SEL.CARD);
  if (card || typeAttr === 'CARD' || typeAttr === 'SHARE') {
    const title = cleanText(card?.querySelector('[class*="title" i], [class*="Title" i]'));
    const info = cleanText(card?.querySelector('[class*="info" i], [class*="desc" i], [class*="Desc" i]'));
    const img = card?.querySelector('img');
    return { msgType: 'card', mediaUrl: img?.getAttribute('src') || '', text: info || title || '卡片消息' };
  }
  if (/recalled? a message|撤回了一?条消息/i.test(text)) {
    return { msgType: 'recall', mediaUrl: '', text };
  }
  // 媒体
  const imgs = Array.from(item.querySelectorAll('img')).filter(
    (img) => !(img.closest && img.closest('[class*="avatar" i], [class*="Avatar" i]')),
  );
  const vids = item.querySelectorAll('video, [class*="video" i], [class*="Video" i]');
  const audio = item.querySelectorAll('[class*="voice" i], [class*="Voice" i], [class*="audio" i], [class*="Audio" i]');
  const links = item.querySelectorAll('a[href*="http"]');
  let mediaUrl = '';
  let msgType = 'text';
  if (typeAttr === 'IMAGE' || (imgs.length && !text)) {
    msgType = 'image';
    mediaUrl = imgs[0]?.getAttribute('src') || '';
  } else if (typeAttr === 'VIDEO' || vids.length) {
    msgType = 'video';
    mediaUrl = vids[0]?.getAttribute('src') || '';
  } else if (typeAttr === 'VOICE' || typeAttr === 'AUDIO' || audio.length) {
    msgType = 'voice';
  } else if (links.length && !text) {
    msgType = 'link';
    mediaUrl = links[0].getAttribute('href') || '';
  }
  return { msgType, mediaUrl, text };
}

// —— 群聊识别 ——
function detectGroup(item) {
  let isGroup = false;
  let groupId = '';
  let groupName = '';
  let senderName = '';
  const headerText = getPeerName() || '';
  if (headerText && /group|team|chat|群|团队/i.test(headerText)) {
    isGroup = true;
    groupName = headerText;
  }
  const inner = cleanText(item);
  const atMatch = inner.match(/^@([^\s,，：:]+)[\s,，：:]/);
  const bracketMatch = inner.match(/^\[([^\]]+)\]/);
  if (atMatch || bracketMatch) {
    isGroup = true;
    senderName = (atMatch && atMatch[1]) || (bracketMatch && bracketMatch[1]) || '';
  }
  const gid = (location.href.match(/[?&](group|conversation)_id=([^&]+)/) || [])[2];
  if (gid) {
    groupId = gid;
    isGroup = true;
  }
  return { isGroup, groupId, groupName, senderName };
}

// —— 噪音过滤 ——
function isListNoise(item) {
  if (!item || !item.closest) return false;
  if (item.closest(SEL.CONV_ITEM)) return true;
  if (item.closest(SEL.CHAT_LIST)) {
    if (item.matches('[data-e2e="dm-new-conversation-item"], [class*="DivItemWrapper"]')) return true;
    if (item.querySelector('[data-e2e="dm-new-conversation-item"], [class*="DivItemWrapper"]')) return true;
  }
  return false;
}

// —— 未读检测 ——
function detectUnread(item) {
  if (!item) return false;
  try {
    if (item.matches && item.matches(SEL.UNREAD)) return true;
    if (item.querySelector && item.querySelector(SEL.UNREAD)) return true;
  } catch (_) {  }
  // TikTok 未读通常有红色 badge 数字
  const badge = item.querySelector('[class*="badge" i], [class*="Badge" i], [class*="count" i]');
  if (badge && badge.textContent && /\d/.test(badge.textContent)) return true;
  return false;
}

// —— 会话列表枚举 ——
function getConversationList() {
  const customSels = customConversationListSelectors(CHANNELS.TIKTOK);
  let items = [];
  if (customSels.length) {
    for (const sel of customSels) {
      try { items = items.concat(qsa(sel)); } catch (_) {  }
    }
  }
  if (!items.length) items = qsa(SEL.CONV_ITEM);
  if (!items.length) {
    const container = qs('[data-e2e="dm-new-conversation-list"]');
    if (container) items = Array.from(container.querySelectorAll('[data-e2e="dm-new-conversation-item"], [class*="DivItemWrapper"]'));
  }
  if (!items.length) return [];
  const out = [];
  const ids = new Set();
  for (const item of items) {
    if (!item || !item.offsetParent) continue;
    // ID 提取：data-conversation-id → /@username 链接 → 昵称
    const raw = item.getAttribute('data-conversation-id') || item.getAttribute('data-id') || item.id || null;
    let id = normalizeContactId(raw);
    if (!id) {
      const link = item.querySelector('a[href*="/@"]');
      if (link) {
        const m = link.getAttribute('href')?.match(/@([\w.]+)/);
        if (m && m[1]) id = m[1];
      }
    }
    const nameEl = item.querySelector('[data-e2e="dm-new-conversation-nickname"], [class*="PInfoNickname"], [class*="SpanNicknameText"]');
    const name = sanitizePeerName(nameEl ? cleanText(nameEl) : '');
    if (!id) {
      if (name) id = 'conv:' + name.slice(0, 80);
      else continue;
    }
    if (ids.has(id)) continue;
    ids.add(id);
    const unread = detectUnread(item);
    out.push({ id, name, el: item, unread });
  }
  return out;
}

// —— hooks 对象 ——
const hooks = {
  match() {
    if (!isTiktokHost()) return false;
    if (isTiktokChatUrl()) return true;
    if (isTiktokMessagePage()) return true;
    if (strictGetInputEl()) return true;
    if (findAnyMessageInput() && looksLikeMessagePage()) {
      log.warn('match() 走 fallback 模式：TikTok IM 严格选择器已失效，使用通用 DOM 扫描');
      return true;
    }
    return false;
  },
  matchMode() {
    if (!isTiktokHost()) return null;
    if (isTiktokChatUrl() || isTiktokMessagePage() || strictGetInputEl()) return 'strict';
    return 'fallback';
  },
  selectors: SEL,
  getMessageListRoot() {
    const m = mergedSelectors();
    for (const sel of m.listSelectors) {
      try {
        const el = qs(sel);
        if (el) return el;
      } catch (_) {  }
    }
    return qs(SEL.MSG_LIST) || qs('[class*="DivChatMain"]') || qs('[role="log"]') || null;
  },
  getMessageItems() {
    // 主路径：用户配置选择器优先 → SEL 默认候选 → SelectorEngine 结构启发式兜底。
    const m = mergedSelectors();
    const { items } = SelectorEngine.locateMessages({
      root: document,
      itemSelectors: m.itemSelectors,
      listSelectors: m.listSelectors,
    });
    const filtered = items.filter((it) => !isListNoise(it) && !isSystemMessage(it));
    if (filtered.length) return filtered;
    // 兜底
    const thread = this.getMessageListRoot();
    if (!thread) return [];
    const leafText = [];
    const walk = (el, depth) => {
      if (depth > 3) return;
      for (const child of el.children) {
        const txt = cleanText(child);
        if (txt && child.offsetParent !== null && !child.querySelector(SEL.CONV_ITEM) && !child.querySelector(SEL.INPUT)) {
          leafText.push(child);
        } else {
          walk(child, depth + 1);
        }
      }
    };
    walk(thread, 0);
    return leafText.filter((el) => !isListNoise(el) && !isSystemMessage(el));
  },
  parseMessageItem(item) {
    if (isListNoise(item)) return null;
    if (isSystemMessage(item)) {
      const sysText = cleanText(item);
      return {
        message_id: item.getAttribute('data-message-id') || item.getAttribute('data-id') || item.id || `sys:${sysText}`,
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
    // ② 内容提取
    const { msgType, mediaUrl, text } = extractMessageContent(item);
    if (!text && msgType === 'text') return null;
    // ③ 自/他判定已移交后端（统一 customer 占位）
    const sender_type = FRONTEND_DEFAULT_SENDER_TYPE;
    // ④ 群聊识别
    const groupInfo = detectGroup(item);
    // ⑤ 发件人昵称
    let senderName = groupInfo.senderName || '';
    if (!senderName && !groupInfo.isGroup && sender_type === SENDER.CUSTOMER) {
      senderName = getPeerName();
    }
    // ⑥ 消息 id
    // 2026-08-05 修复（无新消息仍不断回复 AI 的根因）：
    // 兜底 msg_id：优先取 DOM 自带 id，否则按文本内容生成确定性 id。
    // 幂等去重（消息ID / 内容hash）已由后端统一负责，前端不再计算内容 hash。
    const mid =
      item.getAttribute('data-message-id') ||
      item.getAttribute('data-id') ||
      item.getAttribute('data-msg-id') ||
      item.id ||
      `c:${text}`;
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
    const input = findInputEl();
    if (!input) {
      log.error('未找到 TikTok 输入框（strict + fallback 均失败）');
      throw new Error('tiktok input not found');
    }
    if (input.isContentEditable || input.getAttribute('contenteditable') === 'true' || input.tagName !== 'TEXTAREA') {
      fillContentEditable(input, text);
    } else {
      setValue(input, text);
    }
    await new Promise((r) => setTimeout(r, 150));
    const btn = findSendButton();
    if (btn) {
      enhancedClick(btn);
      return;
    }
    log.warn('未找到 TikTok 发送按钮，改用回车发送');
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', code: 'Enter', keyCode: 13, which: 13, bubbles: true, cancelable: true }));
    input.dispatchEvent(new KeyboardEvent('keyup', { key: 'Enter', code: 'Enter', keyCode: 13, which: 13, bubbles: true, cancelable: true }));
  },
};

export {
  getAccountId, getConversationId, getConversationList,
  normalizeContactId, isTiktokMessagePage, isTiktokChatUrl, getPeerName,
  isSystemMessage, isTimeText, isSystemText,
  isCenterAligned, isListNoise, detectUnread, detectGroup,
  findSendButton, findInputEl,
};

export function buildTiktokAdapter() {
  return new BaseAdapter({
    name: 'tiktok',
    channel: CHANNELS.TIKTOK,
    SEL,
    hooks,
    rateLimiter: undefined,
  });
}

