import { BaseAdapter } from '../core/channel-adapter.js';
import { CHANNELS, SENDER } from '../core/types.js';
import { mergeSelectors, customConversationListSelectors } from '../core/selector-ai.js';
import { SelectorEngine } from '../core/selector-engine.js';
import { qs, qsa, cleanText, setValue, fillContentEditableHumanized, enhancedClick, createLogger, findAnyMessageInput, looksLikeMessagePage, sanitizePeerName } from '../core/dom.js';
import { humanDelay } from '../core/humanize.js';
import { FRONTEND_DEFAULT_SENDER_TYPE } from '../core/fallback.js';

const log = createLogger('xhs', CHANNELS.XHS);

// 合并选择器：用户配置优先（chrome.storage），SEL 默认兜底。
function mergedSelectors() {
  const fb = {
    itemSelectors: SEL.MSG_ITEM.split(',').map((s) => s.trim()).filter(Boolean),
    listSelectors: SEL.MSG_LIST.split(',').map((s) => s.trim()).filter(Boolean),
    textSelectors: SEL.TEXT.split(',').map((s) => s.trim()).filter(Boolean),
    inputSelectors: SEL.INPUT.split(',').map((s) => s.trim()).filter(Boolean),
    sendSelectors: SEL.SEND.split(',').map((s) => s.trim()).filter(Boolean),
  };
  return mergeSelectors(CHANNELS.XHS, fb);
}

// —— 选择器定义（2026-08 验证可用）——
// ⚠️ [class*="..."] 均带 i 标志（大小写不敏感），否则 CSS module 大写类名全失配。
export const SEL = {
  CHAT_LIST: '.xhs-im-conv-item, .sx-contact-item, [class*="xhs-im-conv-list" i] [class*="conv-item" i]',
  MSG_LIST: '.xhs-im-msg-list-wrap, .vue-recycle-scroller.ready, [class*="chat-content" i], [class*="msg-list" i]',
  MSG_ITEM: '.chat-item, .im-msg-item, [class*="msg-item" i], [class*="bubble" i]',
  TEXT: '.xhs-im-bubble__text, .text-message, [class*="text-message" i], [class*="msg-content" i]',
  INPUT: '#jarvis-reply-textarea, div.xhs-im-input-bar-editor[contenteditable="true"]',
  SEND: '.send_btn, [class*="send-btn" i], [aria-label*="发送"]',
  MSG_TYPE: '[data-msg-type], [data-content-type]',
  CARD: '.card_container, [class*="card-container" i]',
  CARD_TITLE: '.card_bottom_title, [class*="card-title" i]',
  CARD_INFO: '.card_bottom_info, [class*="card-info" i]',
  SPOTLIGHT: '.source-tip',
  PEER_NAME: '.xhs-im-chat-title, [class*="chat-header" i] [class*="title" i], [class*="chat-window" i] [class*="header" i] [class*="name" i]',
  UNREAD: '.xhs-im-conv-item__badge, [class*="unread" i], [class*="red-dot" i], [class*="reddot" i], [data-unread="1"], [class*="new-msg" i], [class*="newmessage" i]',
  UNREAD_BADGE: '.xhs-im-conv-item__badge, [class*="badge" i], [class*="count" i], [class*="num" i], [class*="dot" i]',
};

// —— 输入框候选 ——
// STRICT_INPUT_SELECTORS：仅平台严格特征（用于 matchMode strict 判定 / 页面识别）。
// INPUT_FALLBACKS：发送路径的弹性候选（改版/换肤兜底），不参与 matchMode 判定，
// 避免「任意 textarea 命中就误判为 strict」导致 fallback 降级失效。
// 新版小红书输入框为 div.xhs-im-input-bar-editor[contenteditable]（非 textarea）。
const STRICT_INPUT_SELECTORS = [
  '#jarvis-reply-textarea',
  'div.xhs-im-input-bar-editor[contenteditable="true"]',
  '[class*="xhs-im-input-bar-editor"]',
];
const INPUT_FALLBACKS = [
  '#jarvis-reply-textarea',
  'div.xhs-im-input-bar-editor[contenteditable="true"]',
  '[class*="xhs-im-input-bar-editor"]',
  'textarea[placeholder*="发消息"]',
  'textarea[placeholder*="回复"]',
  'textarea[placeholder*="消息"]',
  'textarea[aria-label*="回复"], textarea[aria-label*="发消息"]',
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

// 小红书聊天页 URL：https://www.xiaohongshu.com/chat（用户确认的真实私信页入口）。
// SPA 面板未渲染时结构命中可能为 false，URL 命中最先判定，避免等待轮询。
function isXhsChatUrl() {
  const path = (location.pathname || '');
  return path === '/chat' || path.startsWith('/chat/');
}

// 小红书联系人 id 规范化：去除常见前缀、清理非法字符（XHS-YYDS normalizeContactId 同款）。
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

// 当前登录的小红书账号 id。
// 多层兜底：侧边栏/header「我的」主页链接 → 任意 profile 链接 → 任意 /user/ 链接 →
// localStorage 缓存（同一账号浮层/改版取不到也可恢复）→ 稳定 unknown（绝不空串）。
// 空串会导致 WS 握手持空 account_id → 服务端 401 → 历史/实时全不上行。
function getAccountId() {
  const candidates = [];
  qsa('aside a[href*="/user/profile/"], header a[href*="/user/profile/"], nav a[href*="/user/profile/"]').forEach((a) => {
    const m = a.getAttribute('href')?.match(/\/user\/profile\/([\w.-]+)/);
    if (m && m[1] && m[1] !== 'self') candidates.push(m[1]);
  });
  qsa('a[href*="/user/profile/"]').forEach((a) => {
    const m = a.getAttribute('href')?.match(/\/user\/profile\/([\w.-]+)/);
    if (m && m[1] && m[1] !== 'self') candidates.push(m[1]);
  });
  qsa('a[href*="/user/"]').forEach((a) => {
    const m = a.getAttribute('href')?.match(/\/user\/(?:profile\/)?([\w.-]+)/);
    if (m && m[1] && m[1] !== 'self') candidates.push(m[1]);
  });
  for (const c of candidates) {
    if (c && c.trim()) {
      try { localStorage.setItem(`hivebridge:account:${CHANNELS.XHS}`, c.trim()); } catch (e) {  }
      return c.trim();
    }
  }
  try {
    const ls = localStorage.getItem(`hivebridge:account:${CHANNELS.XHS}`);
    if (ls) return ls;
  } catch (e) {  }
  // 2026-08-17 治本：DOM 兜底失败时返回空串（不返回 xiaohongshu-unknown 占位值）。
  // types.js buildMessage 在 account_id 为空时 delete 该字段，后端层0 改用
  // (platform+sender_name+content) 三元组命中 outbound，避免占位值污染入库链路。
  return '';
}

// 当前会话 id（小红书的会话 id 多为联系人的 user id）。
// 兜底链：活动会话项 data-conv-id（新版最权威）→ URL /chat/{id} 路径 →
//        ?conversation_id → 旧版活动项 data-* → header 对方链接（排除自己）→ 昵称派生。
function getConversationId() {
  // 1) 活动会话项：新版 .xhs-im-conv-item--active[data-conv-id]（真实 DOM 实测），
  //    旧版 .active / [aria-selected="true"]。data-conv-id 是会话聚合键，最权威。
  const active =
    qsa(SEL.CHAT_LIST).find(
      (el) =>
        el.classList.contains('xhs-im-conv-item--active') ||
        el.classList.contains('active') ||
        el.getAttribute('aria-selected') === 'true'
    );
  if (active) {
    const convId =
      active.getAttribute('data-conv-id') ||
      active.getAttribute('data-key') ||
      active.getAttribute('data-id') ||
      active.getAttribute('data-contactusemid') ||
      active.id ||
      active.getAttribute('data-uid');
    const norm = normalizeContactId(convId);
    if (norm) return norm;
  }
  // 2) 新版小红书聊天页 URL 形如 https://www.xiaohongshu.com/chat/{conversationId}
  //    （实测：https://www.xiaohongshu.com/chat/5e4f75e30000000001001cd8）。
  //    会话 id 直接编码在路径里——不解析它 getConversationId() 返回 null，适配器守卫
  //    会拦截全部消息（表现：会话(空) + 消息零上行）。故路径解析兜底必须存在。
  const pathMatch = (location.pathname || '').match(/\/chat\/([^/?#]+)/);
  if (pathMatch && pathMatch[1]) {
    const norm = normalizeContactId(pathMatch[1]);
    if (norm) return norm;
  }
  if (/[?&]conversation_id=/.test(location.href)) {
    const raw = new URLSearchParams(location.search).get('conversation_id');
    const norm = normalizeContactId(raw);
    if (norm) return norm;
  }
  // 聊天 header 用户链接（对方主页链接即会话标识）。
  // 关键：新版 /chat 页 header 第一个 /user/profile/ 链接常是「我」自己（账号线索），
  // 不能当会话 id；须跳过与当前登录账号相同的链接，取「对方」链接。
  const myAccount = getAccountId();
  const headerLinks = qsa('[class*="chat-window"] [class*="header"] a[href*="/user/"], [class*="chat-content"] [class*="header"] a[href*="/user/"], .im-chat-window [class*="header"] a[href*="/user/"], [class*="chat-header"] a[href*="/user/"]');
  for (const headerLink of headerLinks) {
    const href = headerLink.getAttribute('href') || '';
    const m = href.match(/\/user\/profile\/([\w.-]+)/) || href.match(/\/user\/([\w.-]+)/);
    if (!m || !m[1]) continue;
    if (myAccount && m[1] === myAccount) continue; 
    return m[1];
  }
  // 消息容器 data-id
  const container = qs('.im-chat-window [data-id], .im-chat-container [data-id], [class*="chat-content"] [data-id]');
  if (container) {
    const norm = normalizeContactId(container.getAttribute('data-id'));
    if (norm) return norm;
  }
  if (active) {
    const nameEl = active.querySelector(
      '[class*="nickname" i], [class*="nick-name" i], [class*="name" i], [class*="title" i], [class*="Title" i]'
    );
    const raw = nameEl ? cleanText(nameEl) : cleanText(active);
    const name = sanitizePeerName(raw);
    if (name) return 'conv:' + name.slice(0, 80);
  }
  return null;
}

// 小红书输入框（容错版）：先 INPUT_FALLBACKS 弹性候选 → findAnyMessageInput 通用扫描
// 用途：sendText 在小红书改版后仍能找到输入框（matchMode 判定用 strictInputEl，与此区分）
function findInputEl() {
  for (const sel of INPUT_FALLBACKS) {
    try {
      const el = qs(sel);
      if (el) return el;
    } catch (_) {  }
  }
  return findAnyMessageInput();
}

// 小红书发送按钮：优先 merged selectors → SEL.SEND → 通用扫描
function findSendButton() {
  const m = mergedSelectors();
  for (const sel of m.sendSelectors) {
    try {
      const el = qs(sel);
      if (el) return el;
    } catch (_) {  }
  }
  return qs(SEL.SEND) || qs('button[class*="send"], [class*="send"], [aria-label*="发送"]') || null;
}

// —— 非文字消息提取 ——
// 小红书气泡类型：text | card(笔记卡片) | image | voice | video | link | recall | system(聚光进线)
// data-msg-type 属性为主信号；缺失时按 DOM 结构兜底（头像图绝不当作消息图片）。
function extractMessageContent(item) {
  const typeAttr = (item.getAttribute('data-msg-type') || item.querySelector(SEL.MSG_TYPE)?.getAttribute('data-msg-type') || '').toUpperCase();
  const text = cleanText(item.querySelector(SEL.TEXT) || item);
  if (text && /^(\d{1,2}:\d{2}|(昨天|前天)?\s*\d{1,2}月\d{1,2}日?\s*\d{1,2}:\d{2}|\d{4}-\d{2}-\d{2})$/.test(text)) {
    return { msgType: 'text', mediaUrl: '', text: '' };
  }
  // 笔记卡片消息（data-msg-type=CARD 或含 card_container）
  const card = item.querySelector(SEL.CARD);
  if (card || typeAttr === 'CARD') {
    const title = cleanText(card?.querySelector(SEL.CARD_TITLE));
    const info = cleanText(card?.querySelector(SEL.CARD_INFO));
    const img = card?.querySelector('img');
    return { msgType: 'card', mediaUrl: img?.getAttribute('src') || '', text: info || title || '笔记卡片' };
  }
  if (item.querySelector(SEL.SPOTLIGHT) || typeAttr === 'SPOTLIGHT') {
    return { msgType: 'system', mediaUrl: '', text };
  }
  if (/撤回了?一条消息|撤回了一条|recalled a message/i.test(text)) {
    return { msgType: 'recall', mediaUrl: '', text };
  }
  // 媒体：排除头像图（[class*="avatar"]）——私信每条消息都带发送者头像，不能当消息图片
  const imgs = Array.from(item.querySelectorAll('img')).filter(
    (img) => !(img.closest && img.closest('[class*="avatar"], [class*="Avatar"]'))
  );
  const vids = item.querySelectorAll('video, [class*="video"], [class*="Video"]');
  const audio = item.querySelectorAll('[class*="voice"], [class*="Voice"], [class*="audio"], [class*="Audio"]');
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

// —— 聊天对象昵称（发件人 / 对方昵称）——
// 需求③/修复：1v1 私信「一个会话=一条消息」需把对方昵称作为系统客户名称/发件人。
// 从右侧聊天页上方 header 内的标题元素（含对方昵称或群名）抽取文本，多层兜底。
// 排除「小红书 Web IM」等通用标题词、排除仅是产品名（如「私信」），保证拿到真正昵称或群名。
function getPeerName() {
  for (const sel of String(SEL.PEER_NAME || '').split(',').map((s) => s.trim()).filter(Boolean)) {
    try {
      const el = qs(sel);
      if (el) {
        const t = cleanText(el);
        if (t && !/私信|消息|小红书|聊天|contact|会话/i.test(t)) return t;
      }
    } catch (_) {  }
  }
  // 2) 聊天 header 内任意带 /user/profile/ 的链接（对方主页链接的可见文字=昵称），排除「我」
  const myAccount = getAccountId();
  const headerLinks = qsa(
    '[class*="chat-window"] [class*="header"] a[href*="/user/"], [class*="chat-content"] [class*="header"] a[href*="/user/"], .im-chat-window [class*="header"] a[href*="/user/"], [class*="chat-header"] a[href*="/user/"]'
  );
  for (const link of headerLinks) {
    const href = link.getAttribute('href') || '';
    const m = href.match(/\/user\/(?:profile\/)?([\w.-]+)/);
    if (!m || !m[1]) continue;
    if (myAccount && m[1] === myAccount) continue; 
    const t = cleanText(link);
    if (t && !/私信|消息|小红书|聊天/i.test(t)) return t;
  }
  // 3) 活动会话列表项里的昵称元素（与 getConversationId 兜底同源，适合 header 取不到场景）
  const active =
    qsa(SEL.CHAT_LIST).find(
      (el) =>
        el.classList.contains('xhs-im-conv-item--active') ||
        el.classList.contains('active') ||
        el.getAttribute('aria-selected') === 'true'
    );
  if (active) {
    const nameEl = active.querySelector(
      '[class*="nickname" i], [class*="nick-name" i], [class*="name" i], [class*="title" i], [class*="Title" i]'
    );
    // sanitizePeerName 剥离时间戳/未读徽章等易变后缀（与 getConversationId 兜底一致）
    const raw = nameEl ? cleanText(nameEl) : cleanText(active);
    const t = sanitizePeerName(raw);
    if (t && !/私信|消息|小红书|聊天/i.test(t)) return t;
  }
  return '';
}

// —— 群聊识别 ——
// 小红书群聊特征：① 聊天 header 标题含「群」；② 消息内「@昵称」前缀；③ URL 群 id。
function detectGroup(item) {
  let isGroup = false;
  let groupId = '';
  let groupName = '';
  let senderName = '';
  // 【修复】chat header 文本优先用 getPeerName()（已抽好 header 标题），避免重复合并 selector
  const headerText = getPeerName() || '';
  if (headerText && /群|团队|家族|小组|team|group/i.test(headerText)) {
    isGroup = true;
    groupName = headerText;
  }
  const inner = cleanText(item);
  const atMatch = inner.match(/^@([^\s,，：:]+)[\s,，：:]/);
  if (atMatch) {
    isGroup = true;
    senderName = atMatch[1];
  }
  const gid = (location.href.match(/[?&](group|conversation)_id=([^&]+)/) || [])[2];
  if (gid) {
    groupId = gid;
    isGroup = true;
  }
  return { isGroup, groupId, groupName, senderName };
}

// —— 噪音过滤 ——
// 会话列表项（新版 .xhs-im-conv-item / 旧版 .sx-contact-item）及其内部节点、
// 聊天侧栏卡片绝不应当作消息内容上行。
function isListNoise(item) {
  if (!item || !item.closest) return false;
  if (item.closest('.xhs-im-conv-item')) return true;   
  if (item.closest('.sx-contact-item')) return true;     
  if (item.closest('.xhs-im-conv-list')) return true;    
  if (item.closest('.chat-list-box')) return true;
  if (item.closest(SEL.CHAT_LIST)) {
    if (item.matches('[class*="conv-item"], [class*="contact"]') || item.querySelector('[class*="conv-item"], [class*="contact"]')) return true;
  }
  return false;
}

// 小红书 IM 结构特征（新版 .chat-item 气泡 / 旧版 .im-msg-item）：
//   - 输入框 .xhs-im-input-bar-editor / #jarvis-reply-textarea
//   - 消息项 .chat-item / .im-msg-item；会话项 .xhs-im-conv-item / .sx-contact-item
// 私信常为 SPA 面板/浮层，URL 可能不切 /message，故以结构命中为主。
function isXhsMessagePage() {
  const hasInput = !!strictInputEl();
  const hasMsg = !!document.querySelector(
    '.chat-item, .im-msg-item, .im-chat-window, [class*="msg-list"], [class*="chat-content"], [class*="message-list"], [class*="chat-list"]'
  );
  if (hasInput && hasMsg) return true;
  const hasContact = !!document.querySelector('.sx-contact-item, .xhs-im-conv-item');
  if (hasContact && hasInput) return true;
  return false;
}

// 会话列表枚举（供适配器「遍历所有私信」全量同步）：
// 返回 [{ id, name, el }]，id 取自会话项的 data-key/data-id/data-contactusemid/id/data-uid（规范化后），
// el 为可点击打开该会话的列表项 DOM。遍历器逐个点击 el → 打开线程 → 回填历史。
function getConversationList() {
  // 用户自定义「会话列表项」选择器优先（人工识别 HTML 填写即生效）；无则用默认 SEL.CHAT_LIST
  const customSels = customConversationListSelectors(CHANNELS.XHS);
  let items = [];
  if (customSels.length) {
    for (const sel of customSels) {
      try {
        items = items.concat(qsa(sel));
      } catch (_) {  }
    }
  }
  if (!items.length) items = qsa(SEL.CHAT_LIST);
  if (!items.length) return [];
  const out = [];
  const ids = new Set();
  for (const item of items) {
    if (!item || !item.offsetParent) continue; 
    // 新版 .xhs-im-conv-item 用 data-conv-id（真实 DOM 实测）；旧版 .sx-contact-item 用 data-key/data-id
    const raw =
      item.getAttribute('data-conv-id') ||
      item.getAttribute('data-key') ||
      item.getAttribute('data-id') ||
      item.getAttribute('data-contactusemid') ||
      item.id ||
      item.getAttribute('data-uid');
    let id = normalizeContactId(raw);
    // 昵称：会话项内昵称/标题元素（sanitizePeerName 剥离时间戳/未读徽章等易变后缀）
    const nameEl = item.querySelector(
      '[class*="nickname" i], [class*="nick-name" i], [class*="name" i], [class*="title" i], [class*="Title" i]'
    );
    const name = sanitizePeerName(nameEl ? cleanText(nameEl) : '');
    if (!id) {
      if (name) id = 'conv:' + name.slice(0, 80);
      else continue; 
    }
    if (ids.has(id)) continue;
    ids.add(id);
    // 巡检用：是否标记为有未读新消息（红点/未读 badge）。用于 patrol 优先巡检有新消息的会话。
    const unread = detectUnread(item);
    out.push({ id, name, el: item, unread });
  }
  return out;
}

// 未读检测：会话项自身带未读 class，或其内部含未读红点/badge，即视为有新消息。
//
// 2026-08-05 修复：原版仅靠 SEL.UNREAD 选择器匹配，但小红书会话列表的未读徽章
//   class 名多变（badge/count/num/dot 等），且徽章可能是纯数字文本元素无明确 class。
//   实测 detectUnread 全返回 false → 巡检"未读 0" → 新消息不上报。
//
//   修复策略（多层兜底）：
//   1) SEL.UNREAD 选择器匹配（扩展后覆盖更多 class）
//   2) 数字徽章检测：遍历候选 badge 元素，文本是纯数字 > 0 即视为未读
//   3) data 属性检测：data-unread / data-count / data-msg-count / data-unread-count
//   4) 粗体检测：会话项 name 元素 font-weight >= 600（未读会话标题加粗显示）
//   注意排除"当前激活会话"（active/selected 类）——激活会话本身不算未读（用户正在看）。
function detectUnread(item) {
  if (!item) return false;
  try {
    // 当前激活会话不算未读（用户正在查看）
    const isActive = item.matches && (
      item.matches('[class*="active" i]') ||
      item.matches('[class*="selected" i]') ||
      item.matches('[class*="current" i]')
    );
    if (item.matches && item.matches(SEL.UNREAD) && !isActive) return true;
    if (item.querySelector && item.querySelector(SEL.UNREAD)) {
      if (!isActive) return true;
    }
    if (item.querySelectorAll) {
      const badges = item.querySelectorAll(SEL.UNREAD_BADGE);
      for (const b of badges) {
        const txt = (b.textContent || '').trim();
        if (/^\d+$/.test(txt) && parseInt(txt, 10) > 0) return true;
      }
    }
    // 3) data 属性检测
    const dataKeys = ['data-unread', 'data-count', 'data-msg-count', 'data-unread-count', 'data-new'];
    for (const k of dataKeys) {
      const v = item.getAttribute(k);
      if (v === '1' || v === 'true' || (/^\d+$/.test(v) && parseInt(v, 10) > 0)) return true;
    }
    if (!isActive) {
      const nameEl = item.querySelector(
        '[class*="nickname" i], [class*="nick-name" i], [class*="name" i], [class*="title" i]'
      );
      if (nameEl) {
        const fw = window.getComputedStyle(nameEl).fontWeight;
        const fwNum = parseInt(fw, 10);
        if (!isNaN(fwNum) && fwNum >= 600) return true;
        if (fw === 'bold' || fw === '600' || fw === '700' || fw === '800' || fw === '900') return true;
      }
    }
  } catch (_) {  }
  return false;
}


const hooks = {
  match() {
    if (!location.hostname.includes('xiaohongshu.com')) return false;
    if (isXhsChatUrl()) return true;         
    if (isXhsMessagePage()) return true;     
    if (qs(SEL.INPUT)) return true;          
    if (findAnyMessageInput() && looksLikeMessagePage()) {
      log.warn('match() 走 fallback 模式：小红书 IM 严格选择器已失效，使用通用 DOM 扫描');
      return true;
    }
    return false;
  },
  matchMode() {
    if (!location.hostname.includes('xiaohongshu.com')) return null;
    if (isXhsChatUrl() || isXhsMessagePage() || qs(SEL.INPUT)) return 'strict';
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
    return qs(SEL.MSG_LIST) || qs('.im-chat-window') || qs('[class*="chat-content"]') || qs('[class*="message-list"]') || null;
  },
  getMessageItems() {
    // 主路径：用户配置选择器优先 → SEL 默认候选 → SelectorEngine 结构启发式兜底。
    const m = mergedSelectors();
    const { items } = SelectorEngine.locateMessages({
      root: document,
      itemSelectors: m.itemSelectors,
      listSelectors: m.listSelectors,
    });
    const filtered = items.filter((it) => !isListNoise(it));
    if (filtered.length) return filtered;
    // 兜底：小红书消息气泡 class 多变时，用「消息线程容器内的文本叶子」推断气泡
    const thread = this.getMessageListRoot();
    if (!thread) return [];
    const leafText = [];
    const walk = (el, depth) => {
      if (depth > 3) return;
      for (const child of el.children) {
        const txt = cleanText(child);
        if (txt && child.offsetParent !== null && !child.querySelector('.sx-contact-item') && !child.querySelector(SEL.INPUT)) {
          leafText.push(child);
        } else {
          walk(child, depth + 1);
        }
      }
    };
    walk(thread, 0);
    return leafText.filter((el) => !isListNoise(el));
  },
  parseMessageItem(item, ctx) {
    if (isListNoise(item)) return null;
    // 先判定消息类型（卡片/图片/语音/视频/撤回/系统）
    const { msgType, mediaUrl, text } = extractMessageContent(item);
    if (!text && msgType === 'text') return null; 
    if (msgType === 'system' || msgType === 'recall') {
      const sysText = text || (msgType === 'recall' ? '撤回了一条消息' : '系统消息');
      return {
        message_id:
          item.getAttribute('data-message-id') ||
          item.getAttribute('data-id') ||
          item.getAttribute('data-msg-id') ||
          item.id ||
          `sys:${sysText}`,
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

    // 自/他判定已移交后端（统一 customer 占位，回环防护由后端承担）。
    const sender_type = FRONTEND_DEFAULT_SENDER_TYPE;
    // 群聊识别
    const groupInfo = detectGroup(item);
    // 发件人/对方昵称（需求③，修复 1v1 私信发件人为空/字符串 hash）：
    //   - 群聊里有 @  前缀的成员昵称 → 用它（groupInfo.senderName）
    //   - 群聊无 @ 时 → sender_name 留空（成员名通常显示在每条气泡上方，本字段不强行抓取）
    //   - 1v1 私信客户消息 → 对方昵称（getPeerName() 抓自聊天 header class）
    //   - 1v1 私信自己消息 → 不需要 sender_name（AI/客服）→ 留空（服务端按 account_id 落地）
    let senderName = groupInfo.senderName || '';
    if (!senderName && !groupInfo.isGroup && sender_type === SENDER.CUSTOMER) {
      senderName = getPeerName();
    }
    // 消息 id：新版 .chat-item 用 data-message-id（唯一幂等键）；旧版 data-id/data-msg-id
    //
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
      sender_id: getConversationId() || '', 
      conversation_id: getConversationId() || '', 
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
      log.error('未找到小红书输入框（merged + strict + fallback 均失败）');
      throw new Error('xhs input not found');
    }
    // 2026-08-07 修复（用户诉求③）：下发场景必须先清空输入框旧内容再写入新内容，
    //   避免「用户正在打字时 extension 同时下发」导致「旧内容+新内容」拼接发出。
    //   - contenteditable：fillContentEditableHumanized({ clearBefore: true }) 先清空子节点再逐段拟人 insertText
    //   - textarea：setValue(el, '') 先清空再 setValue(el, text) 覆盖
    //   顺序：清空 → 拟人键入 → 拟人点击间隔（humanDelay click 档案）→ 发送 → 立即通知调用方（ack 由 downlin k 队列统一处理）
    const isContentEditable = input.isContentEditable || input.getAttribute('contenteditable') === 'true' || input.tagName !== 'TEXTAREA';
    if (isContentEditable) {
      await fillContentEditableHumanized(input, text, { clearBefore: true });
    } else {
      setValue(input, '');
      setValue(input, text);
    }
    await humanDelay('click', { channel: CHANNELS.XHS, account: getAccountId() });
    const sendBtn = findSendButton();
    if (sendBtn) {
      enhancedClick(sendBtn);
      try {
        if (isContentEditable) {
          while (input.firstChild) input.removeChild(input.firstChild);
          input.dispatchEvent(new InputEvent('input', { bubbles: true }));
        } else {
          setValue(input, '');
        }
      } catch (_) {  }
      return;
    }
    log.warn('未找到小红书发送按钮，改用回车发送');
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', code: 'Enter', keyCode: 13, which: 13, bubbles: true, cancelable: true }));
    input.dispatchEvent(new KeyboardEvent('keyup', { key: 'Enter', code: 'Enter', keyCode: 13, which: 13, bubbles: true, cancelable: true }));
    try {
      if (isContentEditable) {
        while (input.firstChild) input.removeChild(input.firstChild);
        input.dispatchEvent(new InputEvent('input', { bubbles: true }));
      } else {
        setValue(input, '');
      }
    } catch (_) {  }
  },
};

export { getAccountId, getConversationId, getConversationList, normalizeContactId, isXhsMessagePage, isXhsChatUrl, getPeerName };

export function buildXhsAdapter() {
  return new BaseAdapter({
    name: 'xhs',
    channel: CHANNELS.XHS,
    SEL,
    hooks,
    rateLimiter: undefined,
  });
}

