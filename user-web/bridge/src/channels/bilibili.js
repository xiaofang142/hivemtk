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
// M-6 新用户（非关注好友）识别：tab 归属（未关注人消息）打标 + followUser hook。
// UNFOLLOWED_TAB 选择器此前为死定义（无引用），本刀接线为消息打标的归属判据。
// 激活态判定 fixture 口径、真机未校准：判不出激活态 → 不打标（宁缺勿错）。
import { applyFollowSignal, followSignalFromSystemText, FOLLOW_SIGNAL_SOURCES } from '../core/follow-signal.js';
import { createFollowUserHook } from '../core/follow-action.js';

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
// M-4（2026-11-11）：IMAGE_UPLOAD（.image-upload-btn, input[type="file"]）删除——
//   2026-11-10 审计登记的死选择器（全仓零引用）：它是发送侧入口（点开文件选择器），
//   解析侧无消费者；下行 sendMedia 归 M-3 工厂（media-inject.js）且本轮纪律冻结不
//   得动，接线无落点；实证证据已保留在上方文件头注释（图片 .image-upload-btn）。
//   论证同 M-1 offline-cache 死代码清除模式（接线无消费者 / 删除无损 / 保留只留
//   死定义），故三选一取「删除」。
export const SEL = {
  // 2026-11-12 校准（真机 message.bilibili.com/#/whisper 官方赛事会话实证）：
  // 真实类名带 CSS Modules 哈希后缀（_SessionItem_t04g4_1 / _MessageList_1vip7_1 /
  // _MessageSendBox__Textarea_1izxa_42 + msb-textarea / brt-editor），旧选择器
  // 字面量全不命中 → looksLikeChatPage=false → match()=false → 无 loop 无上报。
  // 加性扩展，不动既有候选。
  CHAT_LIST: ".list-container div[class='list-item'], .message-sidebar__item, [class*=\"session-item\" i], [class*=\"_SessionItem_\" i]",
  MSG_LIST: ".message-list, .message-content__inner, [class*=\"message-list\" i], [class*=\"_MessageList_\" i]",
  // A1（2026-11-13 真机缺口修复，P0-1）：现网消息气泡类名为 CSS Modules 哈希后缀
  //   _Msg_o7f0t_1（外层）/ _Msg__Wrap_ / _Msg__Main_ / _Msg__Content_o7f0t_12（正文），
  //   旧字面量 .msg-item / [class*="message-item"] 全不命中 → getMessageItems=0 零上行。
  //   注意 [class*="_Msg_"] 会同时命中嵌套子节点（见 A8 outermost 过滤）。
  MSG_ITEM: '.msg-item, .interaction-item__content, [class*="message-item" i], [class*="_Msg_" i]',
  // A2：正文文本候选（_Msg__Content_o7f0t_12），与 SEL.TEXT 旧字面量并集。
  TEXT: '.message-content, .message-text-nodes, [class*="msg-content" i], [class*="_Msg__Content" i]',
  INPUT: '#editor, .message-reply-box__input, textarea[class*="reply" i], .msb-textarea, .brt-editor, [class*="_MessageSendBox__Textarea" i]',
  SEND: "[class*='btn-box send-btn'], .message-reply-box__pub, button[class*=\"send\" i]",
  PEER_NAME: ".dialog div[class='title'] > span, .message-header__title, [class*=\"message-header\" i] [class*=\"title\" i]",
  UNREAD: '.notify.notify-number, .message-sidebar__item-num, [class*="unread" i]',
  UNFOLLOWED_TAB: "[title='未关注人消息']",
  MY_MSG_TAB: "[title='我的消息']",
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

// —— A3 outermost 过滤（2026-11-13 真机缺口修复，P0-1/P1-2）——
// 现网 B站私信 DOM 用 CSS Modules 哈希类名，一个逻辑节点是一棵嵌套树：
//   会话行：_SessionItem_t04g4_1（根，带 data-id=contact_<mid>）
//             └ _SessionItem__Avatar / __Main / __Title / __Name / __Info / __Message …
//   消息泡：_Msg_o7f0t_1（根，textContent 含时间前缀）
//             └ _Msg__Time_o7f0t_25（时间分隔）/ _Msg__Wrap_ / _Msg__Main_ / _Msg__Content
// [class*="_SessionItem_"] 命中 54 个（6 根行 × 9 子元素），[class*="_Msg_"] 同理。
// 只保留「祖先链上没有同类命中」的最外层节点：6 根行 / 每泡 1 个。
// 口径与 douyin SelectorEngine.expandMultiBubbleItems 的 outermost 语义一致。
function keepOutermost(nodes) {
  const arr = Array.from(nodes || []);
  const set = new Set(arr);
  return arr.filter((el) => {
    let p = el.parentElement;
    while (p) {
      if (set.has(p)) return false;
      p = p.parentElement;
    }
    return true;
  });
}

// —— A9 系统消息文案漏斗（2026-11-13 真机实证）——
// B站私信没有稳定的系统消息 class（SEL.SYSTEM 的 .message-divider 在现网不存在），
// 官方账号（社区中心/哔哩哔哩智能机/英雄联盟赛事…）的通知以普通气泡形态下发：
//   「今天 09:13 登录操作通知 账号在新设备或平台登录成功…」——不拦即被当客户消息
//   上行触发 AI 回复官方 bot。口径同 douyin：词表 + 时间分隔文案，宁缺勿错。
const SYSTEM_TEXT_PATTERNS = [
  /登录操作通知/,
  /系统通知/,
  /账号在新设备或平台登录成功/,
  /你(已)?添加.*为好友/,
  /已添加.*为好友/,
  /撤回了?一条消息/,
  /recalled a message/i,
  /对方正在输入/,
  /typing/i,
  /互相关注/,
  /以下为新消息/,
  /^={3,}$/,
  /^-{3,}$/,
  /对方已开启.*验证/,
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
  return SYSTEM_TEXT_PATTERNS.some((re) => re.test(text.trim()));
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
  const links = qsa('header a[href*="/u/"], header a[href*="space.bilibili.com/"], nav a[href*="/u/"], nav a[href*="space.bilibili.com/"], .bili-header a[href*="space.bilibili.com/"], a[href*="space.bilibili.com/"]');
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
  // 稳定占位（口径依据 2026-11-12 收口补记）：空串会让服务端 400（ingest/outbox 都
  // 硬性要求非空 account_id，见 handler_http.go「account_id required」），历史/实时全断；
  // 且下行订阅身份=本函数返回值，空串经 http-ingest 的 `accountId || 'default'` 回退
  // 会轮询一个永不有行的账号。占位与 weibo 同款（douyin 的「兜底失败返回空串」fail-loud
  // 口径是另一种有意取舍，代价是多标签页解析失败会归并进同一占位账号行）。
  return 'bilibili-unknown';
}

// 会话列表：.message-sidebar__item 首先是左侧导航（回复我的/@我/赞），
// 只有带 data-uid/data-mid 或 /u/<id> 链接的才算私信会话。
// A4（2026-11-13 真机缺口修复，P1-2）：[class*="_SessionItem_"] 在现网命中 54 个节点
//   （6 个根行 _SessionItem_t04g4_1 + 48 个子元素 _SessionItem__Avatar/__Main/__Name…），
//   靠 name 兜底去重虽能收敛成 6 项，但 el 是子 span 非根行 → patrol simulateRealClick
//   点错目标（子元素里恰好有 _SessionItem__RemoveBtn 删除按钮，无 name 侥幸跳过）。
//   故先做 outermost 过滤只留根行，再走原有导航/链接过滤。
function conversationItems() {
  const all = keepOutermost(qsa(SEL.CHAT_LIST));
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
  // A5（2026-11-13 真机实证）：现网会话根行 data-id="contact_844424930131964"，
  //   其中 844424930131964 即对端 mid（与激活时 URL hash #/whisper/mid844424930131964
  //   一致）。此前裸返回 "contact_844424930131964"，与 hash 路由层/上行 conversation_id
  //   口径不一致 → 去 contact_ 前缀归一为 mid。
  const raw = el.getAttribute('data-uid') || el.getAttribute('data-mid') ||
    el.getAttribute('data-id') || el.getAttribute('data-key') || el.id;
  if (raw) return String(raw).trim().replace(/^contact_/, '');
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
  //    A6a（2026-11-13 真机实证）：现网激活行 class=
  //    "_SessionItem_t04g4_1 _SessionItemIsActive_t04g4_12"，classList.contains('active')
  //    精确匹配必失（类名带 IsActive 中缀与哈希后缀）；is-active 亦不存在。
  //    改 /active/i 子串判定，并显式排除 inactive（防误判）。
  const active = qsa(SEL.CHAT_LIST).find((el) => {
    if (!el.classList) return false;
    const cls = String(el.className || '');
    if (/\binactive\b/i.test(cls)) return false;
    return /\bactive\b/i.test(cls) || /IsActive/i.test(cls) ||
      el.getAttribute('aria-selected') === 'true';
  });
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
  // 3.2) A6b：hash 路由层（真机实证 https://message.bilibili.com/#/whisper/mid844424930131964）
  //    B站消息中心是 SPA hash 路由，会话 id 在 hash 不在 query。
  try {
    const hm = String(location.hash || '').match(/whisper\/mid(\d+)/i);
    if (hm && hm[1]) return hm[1];
  } catch (_) {  }
  // 3.5) 会话面板（.dialog）标题昵称（实证：ChatGPT-On-CS 从 .title > span 取名）
  //    A7：getPeerName 已排除页面级通用标题（"我的消息"等），不会返回常数串台 id。
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
    // M-5：未读徽章（SEL.UNREAD 口径）。供巡检「未读优先」调度使用；
    //   B 站徽章 DOM 未经真机校准，未读优先开关默认不对本渠道开启。
    const unread = detectUnread(el);
    out.push({ id, name, el, unread });
  }
  return out;
}

// 未读检测（M-5）：会话项自身带未读 class，或其内部含未读徽章/红点，即视为有新消息。
// 口径 = 本渠道既有 SEL.UNREAD 选择器（与 douyin/xianyu 的 detectUnread 同一模式）。
// 真机校准缺口：B 站徽章结构样本不足，检出率未验证（见 M-5 报告）。
function detectUnread(item) {
  if (!item) return false;
  try {
    if (item.matches && item.matches(SEL.UNREAD)) return true;
    if (item.querySelector && item.querySelector(SEL.UNREAD)) return true;
  } catch (_) {  }
  return false;
}

// A7（2026-11-13 真机实证）：现网 headerTitle 选择器
//   [class*="message-header" i] [class*="title" i] 命中的是**页面级**标题
//   「我的消息」（消息中心总标题，非对端昵称）。此前它直接透传 → getPeerName 返回
//   "我的消息" → getConversationId 3.5 层返回常数 'conv:我的消息'，所有会话串台。
//   对端昵称改由激活会话行 _SessionItem__Name_* 提供（itemName 的 [class*="name" i]）。
//   通用标题词表与 douyin.js getPeerName 的排除口径同源。
const PAGE_LEVEL_TITLES = /^(我的消息|消息中心|消息|私信|聊天|全部消息|消息列表)$/;
function getPeerName() {
  const el = qs(SEL.PEER_NAME);
  if (!el) return '';
  const t = sanitizePeerName(cleanText(el));
  if (t && PAGE_LEVEL_TITLES.test(t)) return '';
  return t;
}

// —— 对方头像（M-2 客户头像链路，2026-11-11，尽力而为渠道）——
// B 站私信结构样本不足，仅抓会话窗口（.dialog / .message-header，与 PEER_NAME 同容器口径）
// 内头像 img；抓不到保持为空，不报错（真机校准缺口见 M-2 报告）。
// A11（2026-11-13 真机实证补齐）：现网 header 无头像 img（headerTitle 命中页面级
//   「我的消息」），对方头像实际挂在激活会话行 _SessionItem__Avatar_*。补一层
//   激活行头像（与 getConversationId 的 active 判定同源：/active/i + IsActive）。
function getPeerAvatar() {
  for (const sel of ['.dialog img', '.message-header img', '[class*="message-header" i] img']) {
    try {
      const u = avatarImgUrl(qs(sel));
      if (u) return u;
    } catch (_) {  }
  }
  try {
    const active = qsa(SEL.CHAT_LIST).find((el) => {
      if (!el.classList) return false;
      const cls = String(el.className || '');
      if (/\binactive\b/i.test(cls)) return false;
      return /\bactive\b/i.test(cls) || /IsActive/i.test(cls) ||
        el.getAttribute('aria-selected') === 'true';
    });
    const u = avatarImgUrl(active && active.querySelector('img'));
    if (u) return u;
  } catch (_) {  }
  return '';
}

// A9 补齐（2026-11-13）：在 class 信号之外增加「时间分隔文案 + 系统文案词表」两层。
// 现网官方账号通知（登录操作通知…）走普通气泡 class，无 .message-divider 包裹，
// 只靠 SEL.SYSTEM class 判不出来 → 必须以文案兜底（真机实证）。
function isSystemMessage(item) {
  if (!item) return false;
  try {
    if (item.matches && item.matches(SEL.SYSTEM)) return true;
    if (item.querySelector && item.querySelector(SEL.SYSTEM)) return true;
  } catch (_) {  }
  const text = cleanText(item);
  if (isTimeText(text) || isSystemText(text)) return true;
  return false;
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

// activeTabIsUnfollowed 判定「未关注人消息」tab 是否处于激活态（M-6）。
// UNFOLLOWED_TAB 选择器（[title='未关注人消息']）2026-11-10 审计登记为死定义，
// 本刀接线：激活态 = tab 自身/最近容器 class 命中 active/selected/current/checked，
// 或 aria-selected=true。fixture 口径（真机未校准）：判不出激活态一律返回 false
// ——打标 fail-safe，宁缺勿错（不激活误标 = 误伤；这里不可能误标）。
function activeTabIsUnfollowed() {
  try {
    const tab = qs(SEL.UNFOLLOWED_TAB);
    if (!tab) return false;
    const host = (tab.closest && tab.closest('[class*="tab" i], [class*="item" i], li')) || tab;
    const cls = `${(host && host.className) || ''} ${(tab && tab.className) || ''}`;
    if (/\b(active|selected|current|checked)\b/i.test(cls)) return true;
    if (host && host.getAttribute && String(host.getAttribute('aria-selected') || '') === 'true') return true;
    if (tab.getAttribute && String(tab.getAttribute('aria-selected') || '') === 'true') return true;
    return false;
  } catch (_) {
    return false;
  }
}

function parseMessageItem(item) {
  if (!item) return null;
  // M-6：当前激活 tab 为「未关注人消息」⇒ 该会话归属未关注用户，全部消息打标
  // （来源 unfollowed_tab；只加 extra 字段，sender_type/msg_type 语义不变）。
  const tabSignal = activeTabIsUnfollowed() ? FOLLOW_SIGNAL_SOURCES.TAB : null;
  if (isSystemMessage(item)) {
    const sysParsed = {
      message_id: item.getAttribute('data-id') || item.id || `sys:${cleanText(item).slice(0, 40)}`,
      sender_type: SENDER.SYSTEM,
      text: cleanText(item),
      media_url: '',
      msg_type: 'system',
      is_group: false,
      group_id: '',
      group_name: '',
      sender_name: '',
      sender_avatar: '',
      timestamp: Date.now(),
      raw: item.outerHTML ? item.outerHTML.slice(0, 500) : '',
    };
    applyFollowSignal(sysParsed, tabSignal || followSignalFromSystemText(CHANNELS.BILIBILI, sysParsed.text));
    return sysParsed;
  }
  const textEl = item.querySelector(SEL.TEXT) || item;
  let text = cleanText(textEl);
  // 头像过滤（2026-11-10 审计 F4）+ URL 绝对化（F5），与 xhs/xianyu/tiktok 同口径
  const img = Array.from(item.querySelectorAll('img[src]')).find(
    (el) => !(el.closest && el.closest('[class*="avatar" i], [class*="Avatar" i]'))
  ) || null;
  // M-4 媒体分支补齐（审计登记：bilibili 仅 image；补 video/voice/emoji/link/card。
  // fixture 口径，真机未校准）。模板同 douyin/xhs：card 结构提前（标题/摘要进 content，
  // 缩略图沿用首个非头像 img），voice/emoji URL 尽力提取、提取不到保持类型标注；
  // image 语义保持既有口径（无文本才归 image），纯媒体门禁由「!text && !mediaUrl」
  // 放宽为「!text && msgType==='text'」——严格加法，纯语音/表情此前在渠道侧整条被丢弃。
  const card = item.querySelector('[class*="card" i]');
  const cardTitle = cleanText(card?.querySelector('[class*="title" i], [class*="Title" i]') || '');
  const cardInfo = cleanText(card?.querySelector('[class*="info" i], [class*="desc" i]') || '');
  const vids = item.querySelectorAll('video, [class*="video" i], [class*="Video" i]');
  const audio = item.querySelectorAll('[class*="voice" i], [class*="Voice" i], [class*="audio" i], [class*="Audio" i]');
  const emoji = item.querySelectorAll('[class*="emoji" i], [class*="Emoji" i], [draggable="false"][alt]');
  const links = item.querySelectorAll('a[href*="http"]');
  let mediaUrl = absoluteMediaUrl(img ? (img.getAttribute('src') || '') : '');
  let msgType = 'text';
  if (card && (cardTitle || cardInfo)) {
    msgType = 'card';
    text = cardInfo || cardTitle;
  } else if (vids.length) {
    msgType = 'video';
    mediaUrl = absoluteMediaUrl(vids[0].getAttribute('src') || vids[0].querySelector('video')?.getAttribute('src') || '');
  } else if (img && !text) {
    msgType = 'image';
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
  const parsed = {
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
  return applyFollowSignal(parsed, tabSignal);
}

function getMessageItems() {
  const root = chatRoot() || document;
  const m = mergedSelectors();
  for (const sel of m.itemSelectors) {
    try {
      // A8（2026-11-13 真机缺口修复）：[class*="_Msg_"] 同时命中外层气泡
      // _Msg_o7f0t_1 与嵌套的 _Msg__Time/_Msg__Wrap/_Msg__Main/_Msg__Content
      // （一层泡 6+ 节点）→ 不过滤会把一条消息上报 6 次（去重层虽能挡，但
      // msg_id/raw/方向判定都会被嵌套节点带偏）。outermost 只留气泡根。
      const items = Array.from(root.querySelectorAll(sel)).filter((el) => el.offsetParent !== null || true);
      const filtered = keepOutermost(items).filter((it) => !isSystemMessage(it));
      if (filtered.length) return filtered;
    } catch (_) {  }
  }
  // 兜底：线程容器下的直接子元素
  if (root && root !== document) {
    return Array.from(root.children).filter((el) => cleanText(el) && !isSystemMessage(el));
  }
  return [];
}

const hooks = {
  match() {
    if (!isBiliHost()) return false;
    if (looksLikeChatPage()) return true;
    // A10（2026-11-13 真机缺口修复，鸡生蛋）：官方 bot 会话无输入框（B站设计如此，
    //   inputPresent=false）→ looksLikeChatPage()=false 且 findAnyMessageInput()=0
    //   → match()=false → bot-only 账号适配器永不启动，收不到任何官方通知。
    //   消息中心 URL + 会话列表或消息列表已在 DOM 即视为命中（patrol 可自主开会话）。
    if (isMessageCenterUrl() && (qs(SEL.CHAT_LIST) || qs(SEL.MSG_LIST))) return true;
    if (isMessageCenterUrl() && findAnyMessageInput() && looksLikeMessagePage()) {
      log.warn('match() 走 fallback 模式：B站选择器未命中，使用通用 DOM 扫描');
      return true;
    }
    return false;
  },
  matchMode() {
    if (!isBiliHost()) return null;
    if (looksLikeChatPage()) return 'strict';
    // 与 match() 新增分支同步：CHAT_LIST/MSG_LIST 严格选择器命中即 strict
    if (isMessageCenterUrl() && (qs(SEL.CHAT_LIST) || qs(SEL.MSG_LIST))) return 'strict';
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
    if (!input) throw new Error('bilibili input not found');
    const isEditable = input.isContentEditable || input.getAttribute('contenteditable') === 'true';
    const isField = input.tagName === 'TEXTAREA' || input.tagName === 'INPUT';
    // 2026-11-13：.msb-textarea 等 DIV 容器在未开会话时是占位壳（contenteditable=false），
    // 对其走原生 value setter 会抛 Illegal invocation（prod 实证 mh:a825b262 回写失败 20 次 exhaust）；
    // 非编辑态一律视为输入框不可用，明确失败交下行重投，不冒用占位壳。
    if (!isEditable && !isField) throw new Error('bilibili input not editable');
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
  // M-6 followUser（模拟点击关注）：服务端策略触发（默认关）。与渠道 sendText
  // 同一点击原语（el.click()）；已关注态词表命中绝不点击。fixture 口径，真机未校准。
  followUser: createFollowUserHook({
    channelKey: CHANNELS.BILIBILI,
    channelLabel: 'B站',
    findCandidates: () => {
      const scope = qs('.dialog, [class*="dialog" i], [class*="message-header" i]');
      const root = scope || document;
      return qsa('button, [role="button"], [class*="follow" i], [class*="Follow" i]', root);
    },
    clickFn: (el) => { try { el.click(); } catch (_) {  } },
    log,
  }),
};

export { getAccountId, getConversationId, getConversationList, getPeerName, getPeerAvatar, isBiliHost, isMessageCenterUrl, detectUnread, activeTabIsUnfollowed };

export function buildBilibiliAdapter() {
  return new BaseAdapter({
    name: 'bilibili',
    channel: CHANNELS.BILIBILI,
    SEL,
    hooks,
    rateLimiter: undefined,
  });
}
