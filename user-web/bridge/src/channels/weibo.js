import { BaseAdapter } from '../core/channel-adapter.js';
import { CHANNELS, SENDER, mediaPlaceholder } from '../core/types.js';
import {
  qs, qsa, cleanText, setValue, fillContentEditableHumanized,
  createLogger, findAnyMessageInput, looksLikeMessagePage, sanitizePeerName,
  assertConvOrAbort, absoluteMediaUrl, avatarImgUrl, extractVoiceSrc, extractEmojiSrc,
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

// 隐藏 uid 读取（2026-11-13 真机实证，api.weibo.com/chat）：微博把 uid 放在
// display:none 的 .hidden 节点里（自我盒 .user .hidden=7911766916；会话项
// .name 盒内嵌 <div class="hidden">2671109275</div>）。真机样例 uid 位数不一：
// 新手指南=2671109275（10 位）、活动通知=4771252927990411（16 位），故放宽到 6-20 位。
// 真浏览器 innerText 对隐藏节点返回空串，故这里直读 textContent（jsdom 亦走同一路径）。
function hiddenUid(scope) {
  if (!scope || !scope.querySelectorAll) return '';
  const nodes = scope.querySelectorAll('.hidden, [class*="hidden" i]');
  for (const el of nodes) {
    const t = String(el.textContent || '').replace(/\s+/g, '');
    if (/^\d{6,20}$/.test(t)) return t;
  }
  return '';
}

function getAccountId() {
  // ① 自我盒 .user 的隐藏 uid（2026-11-13 真机实证，优先级高于 $CONFIG）：
  //    该页 $CONFIG 只剩 setting_open、无 uid，全页零 /u/ 链接，localStorage 无
  //    缓存 → 旧链路只能返回 'weibo-unknown' 占位（P2-7 多账号归并失真）。
  //    .user 盒在 chat 页唯一（document.querySelectorAll('.user').length=1）。
  try {
    const uid = hiddenUid(qs('.user') || qs('[class*="user" i]'));
    if (uid) {
      try { localStorage.setItem(`hivebridge:account:${CHANNELS.WEIBO}`, uid); } catch (_) {  }
      return uid;
    }
  } catch (_) {  }
  // ①.5 可见昵称兜底：<span class="name">用户7911766916</span> → 数字段即 uid
  try {
    const nameEl = qs('.user .name, .user [class*="name" i]');
    const t = nameEl ? cleanText(nameEl) : '';
    const m = t.match(/^用户(\d{6,15})$/);
    if (m && m[1]) {
      try { localStorage.setItem(`hivebridge:account:${CHANNELS.WEIBO}`, m[1]); } catch (_) {  }
      return m[1];
    }
  } catch (_) {  }
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
  // 稳定占位（2026-11-12 口径收口）：与 douyin 的「兜底失败返回空串」是两种**有意不同**
  // 的取舍，此处保留占位。依据：服务端契约对 account_id 硬性非空——
  //   ① ingest：handler_http.go「account_id required」400（fix_2026_08_14_test 钉住）；
  //   ② outbox/SSE 订阅：同样要求非空 account_id，且下行订阅身份=本函数返回值，
  //      空串会让 http-ingest 的 `accountId || 'default'` 回退轮询一个永不有行的账号；
  // 占位值保证「解析失败时传输链路仍活着」（历史/实时/AI 回复下发同键可达）。
  // 代价：多标签页解析失败会归并进 'weibo-unknown' 账号行（多账号归并失真）——
  // 真机校准轮若发现更高频的解析失败，需改走 douyin 的 fail-loud 口径或补 DOM 兜底层。
  return 'weibo-unknown';
}

function convElements() {
  return qsa(SEL.CHAT_LIST);
}

// 会话项昵称剥离尾部隐藏 uid：真机 .name 盒内嵌 display:none 的 <div class="hidden">
// 2671109275</div>，innerText 环境本无该串，但 textContent/jsdom 兜底环境会出现
// 「新手指南 2671109275」。只按**盒内真实存在的隐藏 uid** 精确剥离尾部串，不按
// 「纯数字尾巴」泛化剥离（避免误伤「客服 1234567」这类真昵称）。
function stripTrailingHiddenUid(text, item) {
  const uid = hiddenUid(item);
  if (!uid || !text) return text;
  const stripped = String(text).replace(new RegExp(`\\s*${uid}\\s*$`), '').trim();
  return stripped || text;
}

function itemName(el) {
  const nameEl = el.querySelector('[class*="name" i], .nick, .remark');
  const raw = nameEl ? cleanText(nameEl) : cleanText(el);
  return sanitizePeerName(stripTrailingHiddenUid(raw, el));
}

function itemId(el) {
  const raw = el.getAttribute('data-uid') || el.getAttribute('data-id') ||
    el.getAttribute('data-userid') || el.getAttribute('data-key') || el.id;
  if (raw) return String(raw).trim();
  // 隐藏 uid（真机实证，2026-11-13）：会话项 .name 盒内嵌 .hidden 纯数字 uid。
  // 优先级高于 /u/ 链接（chat 页全页零 /u/ 链接，旧链路在此断掉 → 昵称派生）。
  const uid = hiddenUid(el);
  if (uid) return uid;
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
    const name = sanitizePeerName(stripTrailingHiddenUid(cleanText(header), header));
    if (name) {
      // 2026-11-13 真机缺口修复（P2-8）：header 只有昵称、无 /u/ 链接时，旧链路直接
      // 派生 conv:<昵称>（改昵/同名即串台）。改为按昵称反查会话列表项的隐藏 uid
      // （稳定 id），查不到才回落 conv:<昵称>。
      // 注意 qs(SEL.PEER_NAME) 在文档序上可能先命中会话列表里的 .name 节点
      // （候选含 [class*="chat"] [class*="name" i]），其 cleanText 在 textContent
      // 环境会粘上隐藏 uid 串，故上面先做同一套 uid 剥离再比较。
      const hit = convElements().find((el) => itemName(el) === name);
      const hid = hit ? itemId(hit) : '';
      if (hid && !hid.startsWith('conv:')) return hid;
      return 'conv:' + name.slice(0, 80);
    }
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

// —— 系统消息识别（2026-11-12 收口：微博此前无过滤，「撤回了一条消息」等系统文案
// 会以 customer 方向上行并触发 AI 误回复——douyin/kuaishou/bilibili 均有漏斗，微博补齐）——
// 保守 fixture 口径（真机校准缺口见 CHANNEL-WEIBO.md §9）：
//   ① 时间分隔文案（聊天流中的 "14:30" / "昨天 10:20" 等居中分隔）
//   ② 居中对齐（通用信号，与 douyin isCenterAligned 同源）
//   ③ 内容词表（撤回/加好友/正在输入/发送失败等系统文案，微博高频形态）
// 与 douyin 的差异：**不带**「短文本且无头像无气泡即判系统」的激进启发层（⑤）——
// 该层依赖 douyin 真机校准过的气泡/头像结构假设，微博结构样本不足，宁缺勿错；
// 词表校准后如有专属 class/结构信号（SEL.SYSTEM），真机轮再补。
const WEIBO_SYSTEM_TEXT_PATTERNS = [
  /撤回了?一条消息/,
  /recalled a message/i,
  /你(已)?添加.*为好友/,
  /已添加.*为好友/,
  /对方正在输入/,
  /typing/i,
  /以下为新消息/,
  /消息发送失败/,
  /消息已发出.*被对方拒收/,
];
function isTimeText(text) {
  if (!text) return false;
  const t = text.trim();
  if (/^\d{1,2}:\d{2}$/.test(t)) return true;
  if (/^(今天|昨天|前天)\s*\d{1,2}:\d{2}$/.test(t)) return true;
  if (/^\d{1,2}月\d{1,2}日(\s+\d{1,2}:\d{2})?$/.test(t)) return true;
  if (/^\d{4}[-/]\d{1,2}[-/]\d{1,2}$/.test(t)) return true;
  if (/^(上午|下午|凌晨)\s*\d{1,2}:\d{2}$/.test(t)) return true;
  return false;
}
function isSystemText(text) {
  if (!text) return false;
  const t = text.trim();
  return WEIBO_SYSTEM_TEXT_PATTERNS.some((re) => re.test(t));
}
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
function isSystemMessage(item) {
  if (!item) return false;
  // ①/② 通用信号：时间分隔 / 居中对齐（系统提示的常见版式）
  const text = cleanText(item);
  if (isTimeText(text)) return true;
  if (isCenterAligned(item) && text && text.length < 60) return true;
  // ③ 内容词表
  if (isSystemText(text)) return true;
  return false;
}

function parseMessageItem(item) {
  if (!item) return null;
  // 系统消息不入上行（douyin:648 / kuaishou:337 同款守卫）：避免撤回/时间分隔触发 AI
  if (isSystemMessage(item)) return null;
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
  // M-4 媒体分支补齐（审计登记缺口 voice/card/link；fixture 口径，真机未校准）。
  // card 提前：div.card 含标题/摘要文本时按 card 上报（链接卡/商品卡），纯图/视频卡
  // （无标题摘要）仍走既有 image/video 实证口径；voice/emoji URL 尽力提取、提取不
  // 到保持类型标注；纯媒体门禁由「!text && !mediaUrl」放宽为「!text && msgType==='text'」
  // ——严格加法，纯语音/表情此前在渠道侧整条被丢弃。
  const card = item.querySelector('[class*="card" i]');
  const cardTitle = cleanText(card?.querySelector('[class*="title" i], [class*="Title" i]') || '');
  const cardInfo = cleanText(card?.querySelector('[class*="info" i], [class*="desc" i]') || '');
  const audio = item.querySelectorAll('[class*="voice" i], [class*="Voice" i], [class*="audio" i], [class*="Audio" i]');
  const emoji = item.querySelectorAll('[class*="emoji" i], [class*="Emoji" i], [draggable="false"][alt]');
  const links = item.querySelectorAll('a[href*="http"]');
  let mediaUrl = absoluteMediaUrl((img && img.getAttribute('src')) || (video && video.getAttribute('src')) || '');
  let msgType = 'text';
  if (card && (cardTitle || cardInfo)) {
    msgType = 'card';
    text = cardInfo || cardTitle;
  } else if (mediaUrl && !text) {
    msgType = video ? 'video' : 'image';
  } else if (audio.length) {
    msgType = 'voice';
    mediaUrl = mediaUrl || absoluteMediaUrl(extractVoiceSrc(item, audio));
  } else if (emoji.length && !text) {
    msgType = 'emoji';
    mediaUrl = mediaUrl || absoluteMediaUrl(extractEmojiSrc(emoji));
  } else if (links.length && !text) {
    msgType = 'link';
    mediaUrl = mediaUrl || absoluteMediaUrl(links[0].getAttribute('href') || '');
  }
  if (!text && msgType === 'text') return null;
  const senderType = directionOf(item);
  return {
    message_id: item.getAttribute('data-id') || item.id || `c:${text || mediaUrl}`,
    sender_type: senderType,
    text: text || mediaPlaceholder(msgType),
    media_url: mediaUrl,
    msg_type: msgType,
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
      if (items.length) return items.filter((el) => !isSystemMessage(el));
    } catch (_) {  }
  }
  if (root && root !== document) {
    return Array.from(root.children).filter((el) => cleanText(el) && !isSystemMessage(el));
  }
  return [];
}

const hooks = {
  match() {
    if (!isWeiboHost()) return false;
    if (looksLikeChatPage()) return true;
    // 2026-11-13 真机缺口修复（P1-5 鸡生蛋）：未开会话时 #webchat-textarea 尚未
    // 渲染（输入框随会话挂载）→ looksLikeChatPage()=false 且 findAnyMessageInput()=0
    // → match()=false → 巡检永远无法自主点开第一个会话。chat URL + 会话列表已在 DOM
    // 即视为聊天页（start() 后由 patrol 自主 openConversation 开会话再收上行）。
    if (isChatUrl() && qs(SEL.CHAT_LIST)) return true;
    if (isChatUrl() && findAnyMessageInput() && looksLikeMessagePage()) {
      log.warn('match() 走 fallback 模式：微博选择器未命中，使用通用 DOM 扫描');
      return true;
    }
    return false;
  },
  matchMode() {
    if (!isWeiboHost()) return null;
    if (looksLikeChatPage()) return 'strict';
    // 与 match() 新增分支同步：会话列表命中即 strict（CHAT_LIST 严格选择器生效）
    if (isChatUrl() && qs(SEL.CHAT_LIST)) return 'strict';
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

export { getAccountId, getConversationId, getConversationList, getPeerName, getPeerAvatar, isWeiboHost, isChatUrl, detectUnread, isSystemMessage, isSystemText, isTimeText };

export function buildWeiboAdapter() {
  return new BaseAdapter({
    name: 'weibo',
    channel: CHANNELS.WEIBO,
    SEL,
    hooks,
    rateLimiter: undefined,
  });
}
