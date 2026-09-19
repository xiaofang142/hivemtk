import { createLogger } from './logger.js';
// B7（批2）：逐段键入计划器来自 @hivemtk/browser-core（段长/停顿分布与 A 链路共源）；
// 本模块只做 DOM 注入执行 + 末段校验兜底。
import { planTypingBursts, shouldInterjectMouse, sleep } from '../../../../packages/browser-core/index.js';

export { createLogger };

const log = createLogger('dom', 'bridge');

export const qs = (sel, root = document) => root.querySelector(sel);
export const qsa = (sel, root = document) => Array.from(root.querySelectorAll(sel));

/** 等待某个 selector 出现（抖音自动回复脚本用的 waitForElement 同款思路） */
export function waitFor(selector, { timeout = 8000, root = document } = {}) {
  return new Promise((resolve) => {
    const el = qs(selector, root);
    if (el) return resolve(el);
    const start = Date.now();
    const timer = setInterval(() => {
      const found = qs(selector, root);
      if (found || Date.now() - start > timeout) {
        clearInterval(timer);
        resolve(found || null);
      }
    }, 200);
  });
}

/** 真实点击：合成完整 Pointer + Mouse 事件序列（DY-auto / tiktok 同款，绕过点击拦截） */
export function simulateRealClick(element) {
  if (!element) return;
  const rect = element.getBoundingClientRect();
  const x = rect.left + rect.width / 2;
  const y = rect.top + rect.height / 2;
  const opts = (type) => ({
    view: window,
    bubbles: true,
    cancelable: true,
    clientX: x,
    clientY: y,
    screenX: x,
    screenY: y,
    button: 0,
    buttons: 1,
    composed: true,
    pointerId: 1,
    pointerType: 'mouse',
    isPrimary: true,
  });
  element.dispatchEvent(new PointerEvent('pointerdown', opts('pointerdown')));
  element.dispatchEvent(new MouseEvent('mousedown', opts('mousedown')));
  element.dispatchEvent(new PointerEvent('pointerup', opts('pointerup')));
  element.dispatchEvent(new MouseEvent('mouseup', opts('mouseup')));
  element.dispatchEvent(new MouseEvent('click', opts('click')));
}

/**
 * 小红书同款点击：先原生 click，再补一层 mousedown/up/click 事件。
 * 用于 .send_btn 这类需要真实事件触发的按钮。
 */
export function enhancedClick(element) {
  if (!element) return;
  try {
    element.click();
  } catch (e) {
    log.debug('native click failed, fallback', e);
  }
  const ev = (type) =>
    new MouseEvent(type, { bubbles: true, cancelable: true, view: window });
  element.dispatchEvent(ev('mousedown'));
  element.dispatchEvent(ev('mouseup'));
  element.dispatchEvent(ev('click'));
}

/** 小红书 setValue：textarea 设值 + 触发 input（domUtils.js 同款） */
export function setValue(el, value) {
  if (!el) return;
  const proto = el.tagName === 'TEXTAREA' ? window.HTMLTextAreaElement.prototype : window.HTMLInputElement.prototype;
  const setter = Object.getOwnPropertyDescriptor(proto, 'value')?.set;
  if (setter) setter.call(el, value);
  else el.value = value;
  el.dispatchEvent(new Event('input', { bubbles: true }));
  el.dispatchEvent(new Event('change', { bubbles: true }));
}

/**
 * 抖音 fillInputViaPaste：contenteditable 填值（粘贴事件优先，保证框架识别输入）。
 * 参考 DY-auto fillInputViaPaste：触发 paste + 写入 innerText + 派发 input。
 *
 * 2026-08-07 修复（用户诉求③）：增加 clearBefore 选项，下发场景必须先清空输入框旧内容，
 *   避免「用户正在打字 + extension 同时下发」导致旧内容+新内容拼接发出。
 *   - clearBefore: true  → 先清空（innerText=''/value=''）再 insertText（默认 false 兼容历史行为）
 *   - 历史回填场景保持原行为（不清空，仅 append）
 */
export function fillContentEditable(el, text, { clearBefore = false } = {}) {
  if (!el) return;
  el.focus();
  if (clearBefore) clearEditable(el);
  try {
    document.execCommand('insertText', false, text);
  } catch (e) {
  }
  if ((el.innerText || el.value || '').toString().trim() !== text.trim()) {
    if (el.tagName === 'TEXTAREA' || el.tagName === 'INPUT') {
      el.value = text;
    } else {
      el.innerText = text;
    }
    el.dispatchEvent(new InputEvent('input', { bubbles: true }));
    el.dispatchEvent(new Event('input', { bubbles: true }));
  }
}

/** fillContentEditable 的清空段：textarea/input 清 value，contenteditable 摘空子节点并派发 input */
function clearEditable(el) {
  try {
    if (el.tagName === 'TEXTAREA' || el.tagName === 'INPUT') {
      el.value = '';
    } else {
      while (el.firstChild) el.removeChild(el.firstChild);
      el.dispatchEvent(new InputEvent('input', { bubbles: true }));
    }
  } catch (_) {  }
}

/** 单段注入失败时的兜底追加（textarea/input 走 value 拼接，contenteditable 走 innerText） */
function appendEditable(el, chunk) {
  if (el.tagName === 'TEXTAREA' || el.tagName === 'INPUT') {
    el.value = (el.value || '') + chunk;
  } else {
    el.innerText = (el.innerText || '') + chunk;
  }
  el.dispatchEvent(new InputEvent('input', { inputType: 'insertText', data: chunk, bubbles: true }));
}

/**
 * fillContentEditableHumanized（B7）：按 browser-core 计划逐段拟人键入。
 *
 * 动机：fillContentEditable 一次性整段 insertText——无节奏、无停顿、无鼠标活动，
 * 是内容脚本链路最典型的机器人特征。本函数保持同一注入通道（execCommand insertText，
 * 合成事件 isTrusted=false 的现状不在本批范围），只把"节奏"做实：
 *   - 1–4 码点分段、段后高斯停顿、标点加重停顿、5% 偶发思考停顿（计划器纯函数可测）；
 *   - 18% 概率穿插一次 mousemove（真人打字时鼠标不会钉死）；
 *   - 末段全量校验，不一致回落到 fillContentEditable 语义（保证发送内容正确优先于拟人）。
 */
export async function fillContentEditableHumanized(el, text, { clearBefore = false } = {}) {
  if (!el || !text) return;
  el.focus();
  if (clearBefore) clearEditable(el);
  for (const burst of planTypingBursts(text)) {
    let ok = false;
    try {
      ok = document.execCommand('insertText', false, burst.text);
    } catch (e) {
      ok = false;
    }
    if (!ok) appendEditable(el, burst.text);
    if (shouldInterjectMouse()) {
      try {
        const r = el.getBoundingClientRect();
        el.dispatchEvent(new MouseEvent('mousemove', {
          bubbles: true, view: typeof window !== 'undefined' ? window : undefined,
          clientX: r.left + Math.random() * r.width, clientY: r.top + Math.random() * r.height,
        }));
      } catch (_) {  }
    }
    if (burst.delayMs > 0) await sleep(Math.round(burst.delayMs));
  }
  const expect = String(text).trim();
  if ((el.innerText || el.value || '').toString().trim() !== expect) {
    fillContentEditable(el, text, { clearBefore: true });
  }
}


/** 抖音 humanType：逐字符 execCommand('insertText')（更拟人，适合作者打字节奏） */
export function humanType(el, text, { interval = 20 } = {}) {
  return new Promise((resolve) => {
    if (!el) return resolve();
    el.focus();
    let i = 0;
    const tick = () => {
      if (i >= text.length) return resolve();
      try {
        document.execCommand('insertText', false, text[i]);
      } catch (e) {
        el.innerText = (el.innerText || '') + text[i];
      }
      i += 1;
      setTimeout(tick, interval);
    };
    tick();
  });
}

/**
 * TikTok simulateTyping：先聚焦，再逐字符写值并派发 input（适配 Draft.js 受控输入）。
 * 对 textarea / contenteditable 均兼容。
 */
export function simulateTyping(el, text, { interval = 25 } = {}) {
  return new Promise((resolve) => {
    if (!el) return resolve();
    el.focus();
    let acc = '';
    let i = 0;
    const tick = () => {
      if (i >= text.length) {
        el.dispatchEvent(new Event('input', { bubbles: true }));
        return resolve();
      }
      acc += text[i];
      if (el.tagName === 'TEXTAREA' || el.tagName === 'INPUT') {
        const setter = Object.getOwnPropertyDescriptor(window.HTMLTextAreaElement.prototype, 'value')?.set;
        if (setter) setter.call(el, acc);
        else el.value = acc;
      } else {
        try {
          document.execCommand('insertText', false, text[i]);
        } catch (e) {
          el.innerText = acc;
        }
      }
      el.dispatchEvent(new InputEvent('input', { bubbles: true }));
      i += 1;
      setTimeout(tick, interval);
    };
    tick();
  });
}

/** TikTok simulateEnterKey：在输入框派发回车（Draft.js 靠 Enter 发送） */
export function simulateEnterKey(el) {
  if (!el) return;
  const ev = (type) =>
    new KeyboardEvent(type, { key: 'Enter', code: 'Enter', keyCode: 13, which: 13, bubbles: true, cancelable: true });
  el.dispatchEvent(ev('keydown'));
  el.dispatchEvent(ev('keypress'));
  el.dispatchEvent(ev('keyup'));
}

/** 取纯文本并压缩多余空白 */
export function cleanText(el) {
  if (!el) return '';
  return (el.innerText || el.textContent || '').replace(/\s+/g, ' ').trim();
}

/**
 * 净化会话列表项文本为稳定昵称，剥离时间戳/状态徽章等易变后缀。
 *
 * 背景（2026-08-07 第十一轮修复）：兜底用 cleanText(activeItem) 派生 conversation_id 时，
 * 元素文本常含会话项内的时间戳、状态徽章、相对时间等易变文本（如
 *   "好吃嘴辰辰 12:31"、"AI 修炼场 5 昨天 18:20"、"淘淘达人软件商城 交易成功"）。
 * patrol 每次扫描这些文本都在变 → conversation_id 每次不同 →
 * outbound 入库 conversation_id 与前端下次 getConversationId() 不匹配 →
 * 下行永远找不到目标会话 → 大量 pending 永久堆积（实测 287 条）。
 *
 * 本函数反复剥离已知易变后缀，仅保留稳定的昵称部分。
 * 返回空串表示输入全是易变文本（如仅 "12:31"），调用方应据此放弃派生。
 */
export function sanitizePeerName(text) {
  if (!text) return '';
  let s = String(text).replace(/\s+/g, ' ').trim();
  if (isVolatileToken(s)) return '';
  // 反复剥离后缀，直到稳定（防多层后缀如 "昨天 18:20"：先剥 HH:MM 再剥 昨天）
  let prev;
  let iter = 0;
  do {
    prev = s;
    s = s.replace(/\s+(刚刚|\d+分钟前|\d+小时前|\d+天前)$/i, '');
    s = s.replace(/\s+(昨天|前天|今天)(\s+\d{1,2}:\d{2})?$/i, '');
    s = s.replace(/\s+\d{1,2}:\d{2}$/, '');
    s = s.replace(/\s+\d{4}[/年\-]\d{1,2}[/月\-]\d{1,2}日?$/, '');
    s = s.replace(/\s+\d{1,2}月\d{1,2}日$/, '');
    s = s.replace(/\s+(交易成功|有新交易评价|已发货|待发货|待付款|等待买家付款|交易关闭|退款成功|退款中|已退款|已读|未读)$/i, '');
    s = s.replace(/\s+\[\d+\]$/, '');
    s = s.replace(/\s+$/, '');
    if (s && isVolatileToken(s)) s = '';
    iter++;
  } while (s !== prev && iter < 8 && s.length > 0);
  return s.trim();
}

// 判断整串是否为易变 token（纯时间/相对时间/订单状态/日期）。
// 用作 sanitizePeerName 的"清空触发器"：若剩余文本全是易变信息则放弃派生会话 id。
function isVolatileToken(s) {
  if (!s) return false;
  if (/^\d{1,2}:\d{2}$/.test(s)) return true;
  if (/^(刚刚|\d+分钟前|\d+小时前|\d+天前)$/i.test(s)) return true;
  if (/^(昨天|前天|今天)(\s+\d{1,2}:\d{2})?$/i.test(s)) return true;
  if (/^\d{4}[/年\-]\d{1,2}[/月\-]\d{1,2}日?$/.test(s)) return true;
  if (/^\d{1,2}月\d{1,2}日$/.test(s)) return true;
  if (/^(交易成功|有新交易评价|已发货|待发货|待付款|等待买家付款|交易关闭|退款成功|退款中|已退款|已读|未读)$/i.test(s)) return true;
  return false;
}

/** 包裹 MutationObserver 的便捷 API */
export function observe(root, cb, options = { childList: true, subtree: true }) {
  if (!root) return null;
  const obs = new MutationObserver(cb);
  obs.observe(root, options);
  return obs;
}


/** 元素是否在视觉上可见（offsetParent + 尺寸 + 非 display:none） */
export function isLikelyVisible(el) {
  if (!el) return false;
  if (el.offsetParent === null && getComputedStyle(el).position !== 'fixed') return false;
  const rect = el.getBoundingClientRect();
  if (rect.width === 0 || rect.height === 0) return false;
  return true;
}

/**
 * 通用回退：扫描页面上所有「可能是消息输入框」的元素并按可信度打分。
 * 命中条件（任一即可）：
 *   1) contenteditable="true" / ""  且 尺寸 ≥ 20×10（聊天输入框通常不小于此）
 *   2) [role="textbox"]  且 尺寸 ≥ 20×10
 *   3) textarea 且 placeholder / aria-label 含「消息|留言|回复|reply|message|input|comment|chat」
 *   4) [data-e2e*="message-input" / "input" / "editor" / "chat-input"]
 *   5) [data-testid*="message" / "input" / "editor"]
 *   6) [aria-label*="消息" / "留言" / "回复" / "Send a message" / "Type a message"]
 * 排除条件（任何一条命中即跳过）：
 *   - className 含 comment / Comment / commentEditor （评论框）
 *   - className 含 editor-kit / editorContainer （视频评论编辑器）
 *   - className 含 search / Search （搜索框）
 *   - 父链上有 className 含 comment / editor-kit / video-comment （继承排除）
 * 启发式排序：
 *   - 优先 contenteditable / role=textbox
 *   - 在视口下半部分（top > 40% 视口高度）的元素更像是聊天输入框
 *   - 尺寸过小（评论框、搜索框）不计入
 * @returns {Element|null} 最佳候选元素；无候选时返回 null
 */
export function findAnyMessageInput() {
  const all = (sel) => {
    try { return Array.from(document.querySelectorAll(sel)); }
    catch (_) { return []; }
  };
  const candidates = [];
  const vh = window.innerHeight || 800;

  // 反例关键词（评论框 / 视频评论 / 搜索框）
  // jingxuan 页面就是误中：messageEditorinputArea 在 editor-kit-container 内
  const EXCLUDE_KEYWORDS = /comment|Comment|editor-kit|editorContainer|searchInput|searchBar|searchBox|SearchInput/i;

  /** 元素或其父链是否含反例 class（继承排除：editor-kit-container 套着 messageEditorinputArea） */
  const hasExcludedAncestor = (el) => {
    let cur = el;
    let depth = 0;
    while (cur && depth < 6) {
      const cls = (cur.className && typeof cur.className === 'string') ? cur.className : '';
      if (EXCLUDE_KEYWORDS.test(cls)) return true;
      cur = cur.parentElement;
      depth += 1;
    }
    return false;
  };

  const score = (el) => {
    if (!isLikelyVisible(el)) return -1;
    if (hasExcludedAncestor(el)) return -1; 
    const r = el.getBoundingClientRect();
    if (r.width < 20 || r.height < 10) return -1;
    if (el.tagName === 'TEXTAREA' && r.width < 60) return -1;
    // 在视口下半部分加分
    const lowerHalfBonus = r.top > vh * 0.4 ? 2 : 0;
    return lowerHalfBonus;
  };

  const push = (el, base) => {
    const s = score(el);
    if (s < 0) return;
    candidates.push({ el, score: base + s });
  };

  for (const el of all('div[contenteditable="true"], div[contenteditable=""], [contenteditable="true"], [contenteditable=""]')) {
    push(el, 10);
  }
  for (const el of all('[role="textbox"]')) {
    push(el, 8);
  }
  for (const el of all('textarea')) {
    const ph = (el.getAttribute('placeholder') || '').toLowerCase();
    const aria = (el.getAttribute('aria-label') || '').toLowerCase();
    if (/消息|留言|回复|reply|message|input|comment|chat|say|send|content/.test(ph + ' ' + aria)) {
      push(el, 7);
    }
  }
  for (const sel of [
    '[data-e2e*="message-input"]',
    '[data-e2e*="chat-input"]',
    '[data-e2e*="input"]',
    '[data-e2e*="editor"]',
  ]) {
    for (const el of all(sel)) push(el, 6);
  }
  for (const sel of [
    '[data-testid*="message"]',
    '[data-testid*="input"]',
    '[data-testid*="editor"]',
  ]) {
    for (const el of all(sel)) push(el, 5);
  }
  for (const el of all('[aria-label]')) {
    const aria = (el.getAttribute('aria-label') || '');
    if (/消息|留言|回复|send a message|type a message|new message/.test(aria.toLowerCase() + ' ' + aria)) {
      push(el, 4);
    }
  }

  if (!candidates.length) return null;
  candidates.sort((a, b) => {
    if (b.score !== a.score) return b.score - a.score;
    const ar = a.el.getBoundingClientRect();
    const br = b.el.getBoundingClientRect();
    return (br.width * br.height) - (ar.width * ar.height);
  });
  return candidates[0].el;
}

/**
 * 页面上下文是否看起来像「私信/消息/聊天」页。
 *
 * 判定策略（按可靠性排序）：
 *   1) URL 必须包含私信/IM 关键词：message / chat / im / inbox / direct / dm / private / msg
 *   2) URL 不能包含反例关键词：jingxuan / discover / explore / search / hot / follow / feed / recommend
 *   3) DOM 启发式只作为辅助，需「消息列表」+「会话容器」类名同时存在（多特征投票）
 *      单个 class 命中不算（避免 jingxuan 页面有 conversationConversation* 元素就误判）
 *
 * 设计原则：宁可漏判让 match() 返回 false 由用户报告，也不要误判让桥接在评论区乱跑。
 *
 * @returns {boolean}
 */
export function looksLikeMessagePage() {
  try {
    const url = (location.href || '').toLowerCase();

    // ---- 1. URL 反例黑名单（首页/feed/精选/搜索/个人主页/帖子详情 等） ----
    // 即便 URL 包含聊天关键词，若同时包含反例词，仍按反例处理
    const EXCLUDE_URL_PATTERNS = [
      /\/jingxuan\b/i,           
      /\/discover\b/i,           
      /\/explore\b/i,            
      /\/search\b/i,             
      /\/hot\b/i,                
      /\/follow\b/i,             
      /\/recommend\b/i,          
      /\/feed\b/i,               
      /\/trending\b/i,           
      /\/user\/[^/?#]+/,         
      /\/video\/\d+/,            
      /\/note\//,                
      /\/explore\?/,             
    ];
    for (const re of EXCLUDE_URL_PATTERNS) {
      if (re.test(url)) return false;
    }

    if (/\/(messages?|chats?|msg|direct|im|inbox|conversation|private|dm)(\/|\?|$|#)/.test(url)) return true;
    if (/[?&](conversation_id|message_id|session_id|chat_id|user_id)=/.test(url)) return true;
    if (/\/messages\/@?[\w.]+/.test(url)) return true;            
    if (/\/im\b/.test(url)) return true;                          
    if (/\/im\/chat\b/.test(url)) return true;                    

    // ---- 3. DOM 启发式（URL 不匹配时的兜底，必须多特征同时命中） ----
    // 要求「消息列表根」+「会话容器」类名同时存在
    // 单凭 message- / conversation 这种宽泛匹配会误中 jingxuan 推荐列表
    const hasMessageList = !!document.querySelector(
      '[class*="MessageList"], [class*="message-list"], [class*="messageList"], ' +
      '[class*="MessageContainer"], [class*="message-container"]'
    );
    const hasChatContainer = !!document.querySelector(
      '[class*="ChatWindow"], [class*="chat-window"], [class*="chatWindow"], ' +
      '[class*="ImChat"], [class*="im-chat"], [class*="IMChat"]'
    );
    if (hasMessageList && hasChatContainer) return true;

    // 强特征：data-e2e 含 chat-item / message-item 列表项（不算 comment）
    const hasChatItems = document.querySelectorAll(
      '[data-e2e*="chat-item"], [data-e2e*="message-item"]'
    ).length >= 2;
    if (hasChatItems) return true;

    return false;
  } catch (_) {
    return false;
  }
}

