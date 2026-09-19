import { BaseAdapter } from '../core/channel-adapter.js';
import { CHANNELS, SENDER } from '../core/types.js';
import { mergeSelectors, customConversationListSelectors } from '../core/selector-ai.js';
import { SelectorEngine } from '../core/selector-engine.js';
import {
  qs, qsa, cleanText, setValue, fillContentEditableHumanized, enhancedClick,
  simulateRealClick, createLogger, findAnyMessageInput, looksLikeMessagePage,
  sanitizePeerName,
} from '../core/dom.js';
import { humanDelay } from '../core/humanize.js';
import { FRONTEND_DEFAULT_SENDER_TYPE } from '../core/fallback.js';

const log = createLogger('xianyu', CHANNELS.XIANYU);

// 合并选择器：用户配置优先（chrome.storage），SEL 默认兜底。
function mergedSelectors() {
  const fb = {
    itemSelectors: SEL.MSG_ITEM.split(',').map((s) => s.trim()).filter(Boolean),
    listSelectors: SEL.MSG_LIST.split(',').map((s) => s.trim()).filter(Boolean),
    textSelectors: SEL.TEXT.split(',').map((s) => s.trim()).filter(Boolean),
    inputSelectors: SEL.INPUT.split(',').map((s) => s.trim()).filter(Boolean),
    sendSelectors: SEL.SEND.split(',').map((s) => s.trim()).filter(Boolean),
  };
  return mergeSelectors(CHANNELS.XIANYU, fb);
}

// —— 选择器定义（2026-08 验证可用）——
// ⚠️ [class*="..."] 均带 i 标志（大小写不敏感），否则 CSS module 大写类名全失配。
export const SEL = {
  CHAT_LIST: '#conv-list-scrollable, .ant-layout-sider',
  CONV_ITEM: '[class*="conversation-item" i], [class*="conv-item" i]',
  MSG_LIST: '#message-list-scrollable, [class*="chat-main" i]',
  MSG_ITEM: 'li.ant-list-item .message-row, li.ant-list-item, [class*="msg-item" i], [class*="bubble" i]',
  SYSTEM: '[class*="msg-tips" i], [class*="divider" i], [class*="notice" i], [class*="system-msg" i]',
  UNREAD: '[class*="unread" i], [data-unread="1"], .ant-badge-count',
  TEXT: '[class*="message-text" i], [class*="msg-content" i]',
  MSG_TYPE: '[data-msg-type], [data-message-type]',
  CARD: '[class*="msg-text-card" i], [class*="card-container" i]',
  INPUT: '.sendbox textarea.ant-input, textarea[placeholder*="请输入消息"]',
  SEND: '.sendbox button.ant-btn, [class*="send-btn" i]',
  PEER_NAME: '[class*="message-topbar" i] [class*="text1" i], [class*="chat-header" i] [class*="title" i]',
};

// —— 严格输入框 / 弹性输入框（解耦 matchMode 判定 vs 发送路径）——
const STRICT_INPUT_SELECTORS = [
  '.sendbox textarea.ant-input',
  'textarea.ant-input',
  'textarea[placeholder*="请输入消息" i]',
  'textarea[placeholder*="发送" i]',
];
const INPUT_FALLBACKS = [
  ...STRICT_INPUT_SELECTORS,
  '.sendbox textarea',
  '[class*="sendbox" i] textarea',
  'div[contenteditable="true"]',
  '[role="textbox"]',
];

function strictInputEl() {
  for (const sel of STRICT_INPUT_SELECTORS) {
    try {
      const el = qs(sel);
      if (el) return el;
    } catch (_) {  }
  }
  return null;
}

// 闲鱼聊天页 URL：https://www.goofish.com/im 或 https://www.goofish.com/im?conversationId=xxx
function isXianyuChatUrl() {
  const path = (location.pathname || '');
  return path === '/im' || path.startsWith('/im/');
}

function isXianyuHost() {
  const h = location.hostname || '';
  return h.includes('goofish.com') || h.includes('xianyu.com');
}

// 联系人 id 规范化（与 XHS normalizeContactId 同款，去前缀 + 非法字符）
function normalizeContactId(id) {
  if (id == null) return '';
  let s = String(id).trim();
  if (!s) return '';
  const prefixes = ['Total-', 'Active-', 'User-', 'Contact-', 'User_', 'Contact_'];
  for (const prefix of prefixes) {
    if (s.startsWith(prefix)) {
      s = s.substring(prefix.length);
      break;
    }
  }
  s = s.replace(/[^a-zA-Z0-9_-]/g, '');
  return s;
}

// 解析 URL 中的 conversationId（闲鱼常用 ?conversationId= 或 /im/{id} 路径）
function getConversationIdFromUrl() {
  try {
    // 1) query 参数 ?conversationId=
    const qsParams = new URLSearchParams(location.search);
    const qid = qsParams.get('conversationId') || qsParams.get('conversation_id') || qsParams.get('cid');
    if (qid) {
      const norm = normalizeContactId(qid);
      if (norm) return norm;
    }
    // 2) 路径 /im/{id}
    const pathMatch = (location.pathname || '').match(/\/im\/([^/?#]+)/);
    if (pathMatch && pathMatch[1]) {
      const norm = normalizeContactId(pathMatch[1]);
      if (norm) return norm;
    }
  } catch (_) {  }
  return null;
}

// 当前登录的闲鱼账号 id
// 多层兜底：会话列表项内 /user/ 链接（最可靠，含真实 token id） → 左导航/header「我的」主页链接 → 任意 /user/ 链接 → localStorage 缓存 → 稳定 unknown
function getAccountId() {
  const candidates = [];
  // 1) 会话列表项里的真实用户链接（最可靠，含 token 形式 id）
  //    参考 douyin.js：会话列表内 a[href*="/user/"] 常含真实用户 ID（非 'self' 占位）
  const convLinkSelectors = [
    '[class*="conversation-list"] a[href*="/user/"]',
    '[class*="ConversationList"] a[href*="/user/"]',
    '[class*="conversation-item"] a[href*="/user/"]',
    '[class*="ConversationItem"] a[href*="/user/"]',
    '.ant-layout-sider a[href*="/user/"]',
  ];
  for (const sel of convLinkSelectors) {
    try {
      qsa(sel).forEach((a) => {
        const m = a.getAttribute('href')?.match(/\/user\/([\w.-]+)/);
        if (m && m[1] && m[1] !== 'self') candidates.push(m[1]);
      });
    } catch (_) {  }
  }
  qsa('aside a[href*="/user/"], header a[href*="/user/"], nav a[href*="/user/"]').forEach((a) => {
    const m = a.getAttribute('href')?.match(/\/user\/([\w.-]+)/);
    if (m && m[1] && m[1] !== 'self') candidates.push(m[1]);
  });
  qsa('a[href*="personal?userId="], a[href*="personal?userId="]').forEach((a) => {
    const m = a.getAttribute('href')?.match(/userId=([\w.-]+)/);
    if (m && m[1] && m[1] !== 'self') candidates.push(m[1]);
  });
  qsa('a[href*="/user/"]').forEach((a) => {
    const m = a.getAttribute('href')?.match(/\/user\/([\w.-]+)/);
    if (m && m[1] && m[1] !== 'self') candidates.push(m[1]);
  });
  for (const c of candidates) {
    if (c && c.trim()) {
      try { localStorage.setItem(`hivebridge:account:${CHANNELS.XIANYU}`, c.trim()); } catch (e) {  }
      return c.trim();
    }
  }
  try {
    const ls = localStorage.getItem(`hivebridge:account:${CHANNELS.XIANYU}`);
    if (ls) return ls;
  } catch (e) {  }
  return '';
}

// 当前会话 id（闲鱼）
// 兜底链：URL ?conversationId= → 路径 /im/{id} → 活动会话项 data-* → 消息容器 data-id → header 对方链接（排除自己）→ 昵称派生
function getConversationId() {
  // 1) URL 解析（最权威：闲鱼 SPA 切会话时常通过 query 携带 conversationId）
  const urlId = getConversationIdFromUrl();
  if (urlId) return urlId;
  // 2) 活动会话项（闲鱼: conversation-item-active--xxx class）
  const activeItem = document.querySelector(
    '[class*="conversation-item" i][class*="active" i], [class*="conversation-item-active" i]',
  );
  if (activeItem) {
    const convId =
      activeItem.getAttribute('data-conversation-id') ||
      activeItem.getAttribute('data-cid') ||
      activeItem.getAttribute('data-id') ||
      activeItem.getAttribute('data-uid') ||
      activeItem.id ||
      null;
    const norm = normalizeContactId(convId);
    if (norm) return norm;
  }
  // 3) 消息线程容器的 data-id
  const msgContainer = qs(
    '[class*="chat-content" i] [data-id], [class*="im-message-container" i] [data-id], [class*="chat-main" i] [data-id]',
  );
  if (msgContainer) {
    const containerId = msgContainer.getAttribute('data-id') || msgContainer.getAttribute('data-conversation-id');
    const normC = normalizeContactId(containerId);
    if (normC) return normC;
  }
  // 4) 聊天 header 对方链接（排除自己）
  const myAccount = getAccountId();
  const headerLinks = qsa(
    '[class*="message-topbar" i] a[href*="/user/"], [class*="message-topbar" i] a[href*="personal?userId="], [class*="chat-header" i] a[href*="/user/"]',
  );
  for (const link of headerLinks) {
    const href = link.getAttribute('href') || '';
    const m = href.match(/\/user\/([\w.-]+)/) || href.match(/userId=([\w.-]+)/);
    if (!m || !m[1]) continue;
    if (myAccount && m[1] === myAccount) continue;
    return m[1];
  }
  if (activeItem) {
    const nameEl = activeItem.querySelector(
      '[class*="nickname" i], [class*="nick-name" i], div[style*="font-weight: 500"]',
    );
    const raw = nameEl ? cleanText(nameEl) : cleanText(activeItem);
    const name = sanitizePeerName(raw);
    if (name) return 'conv:' + name.slice(0, 80);
  }
  return null;
}

// 输入框（容错版）：先 STRICT → FALLBACK → 通用扫描
function findInputEl() {
  for (const sel of INPUT_FALLBACKS) {
    try {
      const el = qs(sel);
      if (el) return el;
    } catch (_) {  }
  }
  return findAnyMessageInput();
}

// 发送按钮（参考 douyin.js getRealSendButton 多层兜底）
function findSendButton() {
  const m = mergedSelectors();
  for (const sel of m.sendSelectors) {
    try {
      const el = qs(sel);
      if (el) {
        if (el.tagName === 'SVG') return el.closest('button, [role="button"], div, span') || el;
        return el;
      }
    } catch (_) {  }
  }
  // 兜底1：ant-design 风格发送按钮（闲鱼使用 ant-input-search-button / ant-btn-primary 等）
  const antBtn = qs('.ant-input-search-button, .ant-btn-primary[class*="send"], .ant-btn-primary[class*="Send"]');
  if (antBtn) return antBtn;
  // 兜底2：通用 class/aria-label 匹配
  const generic = qs('[class*="send-btn"], [class*="sendBtn"], [class*="send-button"], [class*="SendButton"], [aria-label*="发送"], [aria-label*="Send"]');
  if (generic) {
    if (generic.tagName === 'SVG') return generic.closest('button, [role="button"], div, span') || generic;
    return generic;
  }
  // 兜底3：扫描红色/主色调 SVG 路径（与 douyin 同款策略，适配闲鱼发送 icon）
  const redPaths = qsa('svg path').filter((p) => {
    const fill = (p.getAttribute('fill') || '').toUpperCase();
    return fill === '#FF4F24' || fill === '#FF5000' || fill === '#FE2C55' || fill === '#FF4444';
  });
  for (const path of redPaths) {
    const clickable = path.closest('button, [role="button"], div, span, svg');
    if (clickable && clickable.offsetParent !== null) return clickable;
  }
  return null;
}

// —— 居中对齐检测（系统消息识别漏斗①）——
// getComputedStyle 在 jsdom 中可能为 'start'/'center'/'end'
function isCenterAligned(el) {
  if (!el) return false;
  try {
    // 直接 textAlign
    const ta = getComputedStyle(el).textAlign;
    if (ta === 'center' || ta === '-webkit-center') return true;
    // 父容器 justifyContent（flex）
    const parent = el.parentElement;
    if (parent) {
      const jc = getComputedStyle(parent).justifyContent;
      if (jc === 'center') return true;
    }
  } catch (_) {  }
  return false;
}

// —— 内容模式匹配（系统消息识别漏斗④）——
const TIME_PATTERNS = [
  /^\d{1,2}:\d{2}$/,
  /^(今天|昨天|前天)\s*\d{1,2}:\d{2}$/,
  /^\d{1,2}月\d{1,2}日(\s+\d{1,2}:\d{2})?$/,
  /^\d{4}[-/]\d{1,2}[-/]\d{1,2}$/,
  /^(上午|下午|凌晨)\s*\d{1,2}:\d{2}$/,
  /^(Mon|Tue|Wed|Thu|Fri|Sat|Sun)\s*$/i,
];

const SYSTEM_TEXT_PATTERNS = [
  /你还不是对方好友/,
  /互相关注后/,
  /请勿向陌生人/,
  /请勿轻信.*转账/,
  /消息已发出.*被对方拒收/,
  /对方已开启.*验证/,
  /消息发送失败/,
  /撤回了一条消息/,
  /recalled a message/i,
  /对方正在输入/,
  /typing/i,
  /以下为新消息/,
  /^={3,}$/,
  /^-{3,}$/,
  /我已收到你的消息/,
  /请等待卖家回复/,
  /订单已创建/,
  /已下单/,
  /已发货/,
  /我已拍下.*待付款/,
  /我已付款.*等待.*发货/,
  /记得及时发货/,
  /卖家已发货/,
  /确认收货.*交易成功/,
  /订单即将自动确认/,
  /卖家交易风险较高/,
  /建议申请退款/,
  /快给.*评价/,
  /交易体验还满意/,
  /购买获.*蚂蚁森林/,
  /闲鱼币待领/,
  /含运费/,
  /交易成功/,
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
// 命中任一层即视为系统消息，返回 true
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
  // ④ 内容模式（纯时间 / 系统文本）
  const text = cleanText(item);
  if (isTimeText(text) || isSystemText(text)) return true;
  if (text && text.length < 60) {
    const hasAvatar = !!item.querySelector('[class*="avatar" i]');
    const hasBubble = !!item.querySelector('[class*="bubble" i], [class*="Bubble" i], [class*="msg-content" i]');
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
  // 商品卡片
  const card = item.querySelector(SEL.CARD);
  if (card || typeAttr === 'CARD' || typeAttr === 'GOODS') {
    const title = cleanText(card?.querySelector('[class*="title" i]'));
    const info = cleanText(card?.querySelector('[class*="info" i], [class*="desc" i]'));
    const img = card?.querySelector('img');
    return { msgType: 'card', mediaUrl: img?.getAttribute('src') || '', text: info || title || '商品卡片' };
  }
  if (/撤回了?一条消息|撤回了一条|recalled a message/i.test(text)) {
    return { msgType: 'recall', mediaUrl: '', text };
  }
  // 媒体：排除头像图
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

// 聊天对象昵称（1v1 私信时作为 sender_name）
function getPeerName() {
  for (const sel of String(SEL.PEER_NAME || '').split(',').map((s) => s.trim()).filter(Boolean)) {
    try {
      const el = qs(sel);
      if (el) {
        const t = cleanText(el);
        if (t && !/闲鱼|消息|聊天|contact|会话/i.test(t)) return t;
      }
    } catch (_) {  }
  }
  // 2) 聊天 header 内任意 /user/ 链接的可见文字
  const myAccount = getAccountId();
  const headerLinks = qsa(
    '[class*="chat-header"] a[href*="/user/"], [class*="ChatHeader"] a[href*="/user/"], [class*="im-chat-header"] a[href*="/user/"]',
  );
  for (const link of headerLinks) {
    const href = link.getAttribute('href') || '';
    const m = href.match(/\/user\/([\w.-]+)/);
    if (!m || !m[1]) continue;
    if (myAccount && m[1] === myAccount) continue;
    const t = cleanText(link);
    if (t && !/闲鱼|消息|聊天/i.test(t)) return t;
  }
  // 3) 活动会话列表项里的昵称元素
  const active =
    qsa(SEL.CHAT_LIST).find(
      (el) =>
        el.classList.contains('active') ||
        el.classList.contains('selected') ||
        el.classList.contains('conversation-item--active') ||
        el.getAttribute('aria-selected') === 'true',
    );
  if (active) {
    const nameEl = active.querySelector(
      '[class*="nickname" i], [class*="nick-name" i], [class*="name" i], [class*="title" i], [class*="Title" i]',
    );
    // sanitizePeerName 剥离会话项里的订单状态徽章/时间戳（与 getConversationId 兜底一致）
    const raw = nameEl ? cleanText(nameEl) : cleanText(active);
    const t = sanitizePeerName(raw);
    if (t && !/闲鱼|消息|聊天/i.test(t)) return t;
  }
  return '';
}

// 群聊识别：① 聊天 header 标题含「群」/「team」/「group」；② 消息内「@昵称」前缀
function detectGroup(item) {
  let isGroup = false;
  let groupId = '';
  let groupName = '';
  let senderName = '';
  const headerText = getPeerName() || '';
  if (headerText && /群|团队|家族|小组|team|group/i.test(headerText)) {
    isGroup = true;
    groupName = headerText;
  }
  const inner = cleanText(item);
  const atMatch = inner.match(/^@([^\s,，：:]+)[\s,，：:]/);
  // [群成员昵称] 结构（参考 douyin.js bracketMatch，闲鱼群聊也可能出现）
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

// 噪音过滤：会话列表项不当作消息内容
function isListNoise(item) {
  if (!item || !item.closest) return false;
  if (item.closest(SEL.CONV_ITEM)) return true;
  if (item.closest(SEL.CHAT_LIST)) {
    if (item.matches('[class*="conversation-item" i], [class*="conv-item" i]')) return true;
    if (item.querySelector('[class*="conversation-item" i], [class*="conv-item" i]')) return true;
  }
  return false;
}

// 闲鱼聊天页结构识别
function isXianyuMessagePage() {
  if (!isXianyuHost()) return false;
  const hasInput = !!strictInputEl();
  const hasMsg = !!document.querySelector(
    '[class*="message-row" i], [class*="msg-item" i], [class*="message-item" i], [class*="chat-bubble" i]',
  );
  if (hasInput && hasMsg) return true;
  const hasConvList = !!document.querySelector(SEL.CHAT_LIST);
  if (hasConvList && hasInput) return true;
  return false;
}

// 会话列表枚举（供适配器「遍历所有私信」全量同步）
function getConversationList() {
  const customSels = customConversationListSelectors(CHANNELS.XIANYU);
  let items = [];
  if (customSels.length) {
    for (const sel of customSels) {
      try {
        items = items.concat(qsa(sel));
      } catch (_) {  }
    }
  }
  if (!items.length) items = qsa(SEL.CONV_ITEM);
  if (!items.length) {
    const container = document.getElementById('conv-list-scrollable');
    if (container) items = Array.from(container.querySelectorAll('[class*="conversation-item" i]'));
  }
  if (!items.length) return [];
  const out = [];
  const ids = new Set();
  for (const item of items) {
    if (!item || !item.offsetParent) continue;
    // 闲鱼会话项内没有 /user/ 链接，用 data-* 或名称兜底
    let id;
    // 1) data 属性
    const raw =
      item.getAttribute('data-conversation-id') ||
      item.getAttribute('data-cid') ||
      item.getAttribute('data-id') ||
      item.getAttribute('data-uid') ||
      item.id ||
      null;
    id = normalizeContactId(raw);
    if (!id) {
      const link = item.querySelector('a[href*="/user/"]');
      if (link) {
        const m = link.getAttribute('href')?.match(/\/user\/([\w.-]+)/);
        if (m && m[1] && m[1] !== 'self') id = m[1];
      }
    }
    // 3) 名称兜底（闲鱼会话项含昵称 div；sanitizePeerName 剥离订单状态/时间戳）
    const nameEl = item.querySelector(
      '[class*="nickname" i], [class*="nick-name" i], [class*="name" i], [class*="title" i], [class*="Title" i], div[style*="font-weight: 500"]',
    );
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

// 未读检测
function detectUnread(item) {
  if (!item) return false;
  try {
    if (item.matches && item.matches(SEL.UNREAD)) return true;
    if (item.querySelector && item.querySelector(SEL.UNREAD)) return true;
  } catch (_) {  }
  return false;
}

const hooks = {
  match() {
    if (!isXianyuHost()) return false;
    if (isXianyuChatUrl()) return true;
    if (isXianyuMessagePage()) return true;
    if (qs(SEL.INPUT)) return true;
    if (findAnyMessageInput() && looksLikeMessagePage()) {
      log.warn('match() 走 fallback 模式：闲鱼 IM 严格选择器已失效，使用通用 DOM 扫描');
      return true;
    }
    return false;
  },
  matchMode() {
    if (!isXianyuHost()) return null;
    if (isXianyuChatUrl() || isXianyuMessagePage() || qs(SEL.INPUT)) return 'strict';
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
    return qs(SEL.MSG_LIST) || qs('[class*="chat-content" i]') || qs('[class*="message-list" i]') || null;
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
    // ② 内容提取（卡片/图片/语音/视频/链接/撤回/系统）
    const { msgType, mediaUrl, text } = extractMessageContent(item);
    if (!text && msgType === 'text') return null;
    // ③ 自/他判定已移交后端（统一 customer 占位）
    const sender_type = FRONTEND_DEFAULT_SENDER_TYPE;
    // ④ 群聊识别
    const groupInfo = detectGroup(item);
    // ⑤ 发件人昵称（1v1 客户消息 → 对方昵称）
    let senderName = groupInfo.senderName || '';
    if (!senderName && !groupInfo.isGroup && sender_type === SENDER.CUSTOMER) {
      senderName = getPeerName();
    }
    // ⑥ 消息 id
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
      log.error('未找到闲鱼输入框（merged + strict + fallback 均失败）');
      throw new Error('xianyu input not found');
    }
    if (input.isContentEditable || input.getAttribute('contenteditable') === 'true' || input.tagName !== 'TEXTAREA') {
      await fillContentEditableHumanized(input, text);
    } else {
      setValue(input, text);
    }
    await humanDelay('click', { channel: CHANNELS.XIANYU, account: getAccountId() });
    const sendBtn = findSendButton();
    if (sendBtn) {
      enhancedClick(sendBtn);
      return;
    }
    log.warn('未找到闲鱼发送按钮，改用回车发送');
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', code: 'Enter', keyCode: 13, which: 13, bubbles: true, cancelable: true }));
    input.dispatchEvent(new KeyboardEvent('keyup', { key: 'Enter', code: 'Enter', keyCode: 13, which: 13, bubbles: true, cancelable: true }));
  },
};

export {
  getAccountId, getConversationId, getConversationList,
  normalizeContactId, isXianyuMessagePage, isXianyuChatUrl, getPeerName,
  isSystemMessage, isTimeText, isSystemText,
  isCenterAligned, isListNoise, detectUnread, detectGroup,
  findSendButton, findInputEl,
};

export function buildXianyuAdapter() {
  return new BaseAdapter({
    name: 'xianyu',
    channel: CHANNELS.XIANYU,
    SEL,
    hooks,
    rateLimiter: undefined,
  });
}

