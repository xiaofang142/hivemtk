// primitives.js — 原语实现（MV3 executeScript func+args 形态）
// 硬约束：executeScript 的 func 会被序列化注入，闭包变量全部丢失，
// 所有外部值必须经 args 传入（官方 scripting API 规范）。

const WAIT_SELECTOR_INTERVAL_MS = 200;

// ---- 页面上下文函数（序列化注入，禁止引用外部闭包）----

function injClick(target) {
  const el = document.querySelector(target);
  if (!el) return { ok: false, error: 'element_not_found: ' + target };
  try { el.scrollIntoView?.({ block: 'center' }); } catch { /* jsdom/不可滚动时忽略 */ }
  el.click();
  // SPA（React/Vue 合成事件）常忽略程序化 el.click()：补发真实指针事件序列。
  const opts = { bubbles: true, cancelable: true, view: window, pointerId: 1, isPrimary: true };
  try {
    el.dispatchEvent(new PointerEvent('pointerdown', opts));
    el.dispatchEvent(new MouseEvent('mousedown', opts));
    el.dispatchEvent(new PointerEvent('pointerup', opts));
    el.dispatchEvent(new MouseEvent('mouseup', opts));
    el.dispatchEvent(new MouseEvent('click', opts));
  } catch { /* PointerEvent 不可用时忽略（el.click() 已发过一次） */ }
  // 链接兜底：上述仍不触发导航时直接跳 href（新标签链接也改为当前页打开，
  // 保持自动化会话 tab 稳定）。但 target=@e ref 的非链接元素不动。
  let navigated = false;
  try {
    const u = new URL(el.href, location.href);
    if (u.href !== location.href && (el.tagName === 'A' && el.href)) {
      // el.click() 已可能已触发路由；仅在确实是外链/新标签时接管
      const wantsNewTab = el.target === '_blank';
      if (wantsNewTab || u.origin !== location.origin) {
        location.href = u.href;
        navigated = true;
      }
    }
  } catch { /* 无 href 或非法 URL */ }
  return { ok: true, navigated };
}

function injType(target, value, clearFirst, submitOnEnter) {
  const el = document.querySelector(target);
  if (!el) return { ok: false, error: 'element_not_found: ' + target };
  el.focus();
  // contenteditable（小红书/微博等富文本评论框）：不能用 el.value，只能走输入管线
  if (el.isContentEditable) {
    if (clearFirst || el.textContent) {
      // 选中全部内容后删除（React 受控组件对 textContent 赋值不响应）
      const sel = window.getSelection();
      const range = document.createRange();
      range.selectNodeContents(el);
      sel.removeAllRanges();
      sel.addRange(range);
      try { document.execCommand('delete', false, null); } catch { /* noop */ }
      el.dispatchEvent(new InputEvent('beforeinput', { bubbles: true, cancelable: true, inputType: 'deleteContentBackward', data: null }));
      el.dispatchEvent(new Event('input', { bubbles: true }));
    }
    let inserted = false;
    try {
      // execCommand('insertText') 在 contenteditable 上走浏览器输入管线，触发真实 input/beforeinput
      inserted = document.execCommand('insertText', false, value);
    } catch { inserted = false; }
    if (!inserted) {
      // 兜底：逐字符 InputEvent（draft.js/ProseMirror 类编辑器监听 beforeinput）
      for (const ch of value) {
        el.dispatchEvent(new InputEvent('beforeinput', { bubbles: true, cancelable: true, inputType: 'insertText', data: ch }));
        const textNode = document.createTextNode(ch);
        el.appendChild(textNode);
        const sel2 = window.getSelection();
        const r2 = document.createRange();
        r2.selectNodeContents(el);
        r2.collapse(false);
        sel2.removeAllRanges();
        sel2.addRange(r2);
        el.dispatchEvent(new Event('input', { bubbles: true }));
      }
    }
    el.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', code: 'Enter', bubbles: true }));
    return { ok: true, editable: true };
  }
  if (clearFirst || el.value) {
    el.value = '';
    el.dispatchEvent(new Event('input', { bubbles: true }));
  }
  // insertText 走浏览器输入管线，触发 input 事件（比直接赋值更接近真实键入）
  try {
    document.execCommand('insertText', false, value);
  } catch {
    el.value = value;
    el.dispatchEvent(new Event('input', { bubbles: true }));
  }
  if (submitOnEnter) {
    el.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', code: 'Enter', bubbles: true }));
    el.form?.requestSubmit?.();
  }
  return { ok: true };
}

function injScroll(direction, amount) {
  const dx = direction === 'left' ? -amount : direction === 'right' ? amount : 0;
  const dy = direction === 'up' ? -amount : direction === 'down' ? amount : 0;
  window.scrollBy(dx, dy);
  return { ok: true, x: window.scrollX, y: window.scrollY };
}

// 以锚元素为基准点击「同容器内的 button」——应对发送/提交按钮无稳定 class、
// 且 @e ref 每次快照重排导致硬编码 ref 不可靠的场景（如小红书评论发送按钮）
function injClickNear(anchorSelector, buttonText) {
  const anchor = document.querySelector(anchorSelector);
  if (!anchor) return { ok: false, error: 'anchor_not_found: ' + anchorSelector };
  let root = anchor.parentElement;
  for (let depth = 0; depth < 4 && root; depth++) {
    const btns = Array.from(root.querySelectorAll('button'));
    const hit = buttonText
      ? btns.find((b) => (b.innerText || '').trim().includes(buttonText))
      : btns[0];
    if (hit) {
      try { hit.scrollIntoView?.({ block: 'center' }); } catch { /* noop */ }
      hit.click();
      const opts = { bubbles: true, cancelable: true, view: window, pointerId: 1, isPrimary: true };
      try {
        hit.dispatchEvent(new PointerEvent('pointerdown', opts));
        hit.dispatchEvent(new MouseEvent('mousedown', opts));
        hit.dispatchEvent(new PointerEvent('pointerup', opts));
        hit.dispatchEvent(new MouseEvent('mouseup', opts));
        hit.dispatchEvent(new MouseEvent('click', opts));
      } catch { /* PointerEvent 不可用时忽略 */ }
      return { ok: true, clicked: (hit.innerText || 'button').trim() };
    }
    root = root.parentElement;
  }
  return { ok: false, error: 'button_not_found_near: ' + anchorSelector };
}

function injExtract(selectors) {
  const out = {};
  for (const [key, sel] of Object.entries(selectors || {})) {
    const nodes = document.querySelectorAll(sel);
    if (nodes.length === 0) { out[key] = []; continue; }
    const MAX = 100;
    out[key] = Array.from(nodes).slice(0, MAX).map((el) =>
      (el.innerText || el.value || el.getAttribute('href') || '').trim()
    ).filter((t) => t !== '');
  }
  return { ok: true, data: out };
}

function injWaitForSelector(selector, timeoutMs) {
  return new Promise((resolve) => {
    const start = Date.now();
    const tick = () => {
      if (document.querySelector(selector)) return resolve({ ok: true });
      if (Date.now() - start >= timeoutMs) return resolve({ ok: false, error: 'selector_timeout: ' + selector });
      setTimeout(tick, 200);
    };
    tick();
  });
}

function injMarkdown() {
  const pick = (root, sel) => root.querySelector(sel);
  const title = (pick(document, 'h1')?.innerText || document.title || '').trim();
  const lines = [`# ${title}`, ''];
  document.querySelectorAll('h1,h2,h3,p,li').forEach((el) => {
    const t = (el.innerText || el.textContent || '').trim();
    if (!t) return;
    const tag = el.tagName.toLowerCase();
    if (tag === 'h1') lines.push(`# ${t}`, '');
    else if (tag === 'h2') lines.push(`## ${t}`, '');
    else if (tag === 'h3') lines.push(`### ${t}`, '');
    else if (tag === 'li') lines.push(`- ${t}`);
    else lines.push(t, '');
  });
  return { ok: true, markdown: lines.join('\n').slice(0, 64 * 1024) };
}

// ---- 页面上下文函数（序列化注入，禁止引用外部闭包）----

/**
 * injPostCommentPrep 阶段一：定位评论输入框 + 聚焦 + 合成注入文字。
 * 选择器由平台适配器（服务端 L3）下发，缺省小红书（R17 真机实测）。
 * 返回 { ok, input_found, needs_trusted, input_text }。
 * needs_trusted=true 表示目标是 contenteditable（React 受控），合成注入后需 trusted 键入兜底。
 */
function injPostCommentPrep(text, inputSelector) {
  const CANDIDATE_INPUT = inputSelector || '.content-textarea, p.content-input, [contenteditable="true"], div[contenteditable], .comments-container textarea, textarea[placeholder]';
  const el = (() => {
    const nodes = document.querySelectorAll(CANDIDATE_INPUT);
    for (const n of nodes) {
      const style = window.getComputedStyle(n);
      if (style.display !== 'none' && style.visibility !== 'hidden' && n.offsetParent !== null) return n;
    }
    return null;
  })();
  if (!el) return { ok: false, input_found: false };

  el.focus();
  if (el.isContentEditable) {
    // contenteditable + React 受控：合成注入无效（untrusted），只聚焦并把光标放进去，
    // 文字由 CDP trusted 键入（cdpTypeText）负责
    const sel = window.getSelection();
    const range = document.createRange();
    range.selectNodeContents(el);
    range.collapse(false);
    sel.removeAllRanges();
    sel.addRange(range);
    return { ok: true, input_found: true, needs_trusted: true, input_text: (el.innerText || '').trim() };
  }
  el.value = text;
  el.dispatchEvent(new Event('input', { bubbles: true }));
  return { ok: true, input_found: true, needs_trusted: false, input_text: (el.value || '').trim() };
}

/**
 * injPostCommentSend 阶段二：定位 发送/发布 button 的**视口坐标**（供 CDP trusted 点击），
 * 并返回输入框引用供后续验证。按钮文本由平台适配器下发。
 */
function injPostCommentSend(inputSelector, sendButtonText) {
  const CANDIDATE_INPUT = inputSelector || '.content-textarea, p.content-input, [contenteditable="true"], div[contenteditable], .comments-container textarea, textarea[placeholder]';
  const el = (() => {
    const nodes = document.querySelectorAll(CANDIDATE_INPUT);
    for (const n of nodes) {
      const style = window.getComputedStyle(n);
      if (style.display !== 'none' && style.visibility !== 'hidden' && n.offsetParent !== null) return n;
    }
    return null;
  })();
  if (!el) return { ok: false, error: 'comment_input_not_found' };

  const WANT = sendButtonText ? [sendButtonText] : ['发送', '发布', '评论'];
  let btn = null;
  let root = el.parentElement;
  for (let depth = 0; depth < 4 && root && !btn; depth++) {
    const btns = Array.from(root.querySelectorAll('button'));
    for (const b of btns) {
      const bt = (b.innerText || '').trim();
      if (WANT.some((w) => bt.includes(w))) {
        const style = window.getComputedStyle(b);
        if (style.display === 'none' || style.visibility === 'hidden') continue;
        btn = b;
        break;
      }
    }
    root = root.parentElement;
  }
  if (!btn) return { ok: false, error: 'send_button_not_found' };
  const rect = btn.getBoundingClientRect();
  // 触发前先滚到按钮可见处（CDP 坐标是视口坐标）
  btn.scrollIntoView?.({ block: 'center' });
  const rect2 = btn.getBoundingClientRect();
  return {
    ok: true,
    x: Math.round(rect2.left + rect2.width / 2),
    y: Math.round(rect2.top + rect2.height / 2),
    legacy_xy: [Math.round(rect.left + rect.width / 2), Math.round(rect.top + rect.height / 2)],
    input_ref_marker: true,
  };
}

/**
 * injPostCommentVerify 阶段三：验证评论已渲染进评论区（xiaohongshu-mcp waitCommentRendered 模式）。
 * MutationObserver 等「评论区出现目标文本」或超时；配平台选择器参数（来自适配器，缺省用小红书）。
 */
function injPostCommentVerify(targetText, opts) {
  const o = opts || {};
  const containerSel = o.containerSelector || '.comments-container, .comments-el, [class*=comment-list], [class*=comments]';
  const deadline = Date.now() + (o.timeoutMs || 5000);
  const norm = (s) => (s || '').replace(/\s+/g, '');
  const want = norm(targetText);

  // 立即查一次（评论可能已渲染）
  const checkNow = () => {
    const containers = document.querySelectorAll(containerSel);
    for (const c of containers) {
      if (norm(c.innerText).includes(want)) return true;
    }
    // 兜底：全文搜（评论区容器类名可能变）
    return norm(document.body.innerText).includes(want);
  };
  if (checkNow()) return { ok: true, posted: true, verified: true };

  return new Promise((resolve) => {
    const done = (result) => {
      try { observer.disconnect(); } catch { /* noop */ }
      resolve(result);
    };
    const observer = new MutationObserver(() => {
      if (checkNow()) done({ ok: true, posted: true, verified: true });
    });
    try {
      observer.observe(document.body, { childList: true, subtree: true, characterData: true });
    } catch { /* 容器不可观察时退化为轮询 */ }
    const poll = setInterval(() => {
      if (checkNow()) { clearInterval(poll); done({ ok: true, posted: true, verified: true }); return; }
      if (Date.now() > deadline) {
        clearInterval(poll);
        done({ ok: true, posted: false, verified: false, reason: 'comment_not_rendered' });
      }
    }, 500);
  });
}

/**
 * injAssert 断言原语（洞察层，Playwright expect 语义）：
 *  - contains_text: 页面（或 selector 命中的元素）文本包含 value
 *  - selector_exists: selector 存在（轮询至 timeout）
 * 失败 resolve {ok:false,...}，SW 侧据此抛错（保持 executeInTab 错误路径统一）。
 */
function injAssert(kind, value, selector, timeoutMs) {
  const norm = (s) => (s || '').replace(/\s+/g, '');
  const want = norm(value);
  const deadline = Date.now() + (timeoutMs || 5000);
  const check = () => {
    if (kind === 'selector_exists') {
      return !!document.querySelector(selector || value);
    }
    // contains_text
    if (selector) {
      const el = document.querySelector(selector);
      return !!el && norm(el.innerText).includes(want);
    }
    return norm(document.body.innerText).includes(want);
  };
  const describe = () => (kind === 'selector_exists'
    ? `selector 未出现: ${selector || value}`
    : `文本未命中: "${value}"${selector ? ' @ ' + selector : ' @ 全文'}`);
  if (check()) return { ok: true, asserted: kind };
  return new Promise((resolve) => {
    const poll = setInterval(() => {
      if (check()) { clearInterval(poll); resolve({ ok: true, asserted: kind }); return; }
      if (Date.now() > deadline) {
        clearInterval(poll);
        resolve({ ok: false, error: `assert_failed(${kind}): ${describe()}` });
      }
    }, 300);
  });
}

/**
 * injQuery 只读洞察原语（Midscene 洞察类语义，返回数据不抛错）：
 *  - text: selector 首个元素 innerText
 *  - exists: 是否存在
 *  - count: 命中数量
 *  - attr: 指定属性值（attribute 参数）
 */
function injQuery(kind, selector, attribute) {
  if (kind === 'exists') return { ok: true, exists: !!document.querySelector(selector) };
  if (kind === 'count') return { ok: true, count: document.querySelectorAll(selector).length };
  const el = document.querySelector(selector);
  if (kind === 'attr') return { ok: true, value: el ? (el.getAttribute(attribute) || '') : '' };
  // text（缺省）
  return { ok: true, value: el ? (el.innerText || el.value || '').trim() : '' };
}

// ---- SW 侧原语分发 ----
// cdp trusted 输入模块（码表/事件序列工厂/attach 生命周期/infobar 等待）——
// 实现级规范来源见 cdp/input.js 头注释
import * as cdpInput from './cdp/input.js';

async function executeInTab(tabId, func, args = []) {
  const [res] = await chrome.scripting.executeScript({ target: { tabId }, func, args });
  const r = res?.result;
  if (!r || r.ok === false) {
    throw new Error(r?.error || 'executeScript failed');
  }
  return r;
}

/**
 * dispatch 执行一条命令帧（server → Host → 扩展）
 * @param {Map<string,Function>} deps 依赖注入（tab-manager / accessibility），便于测试
 * @returns {Promise<Object>} 回包 data
 */
export async function dispatch(cmd, deps) {
  const { openTab, closeTab, activateTab, tabExists } = deps.tabManager;
  const { getRefSelector } = deps.accessibility;

  switch (cmd.action) {
    case 'open_tab': {
      if (!cmd.url || !/^https?:\/\//i.test(cmd.url)) {
        throw new Error('open_tab 需要合法 http(s) URL，收到: ' + (cmd.url || '(空)'));
      }
      const tab = await openTab(cmd.url, cmd.active === true);
      return { chrome_tab_id: tab.id, title: tab.title || '' };
    }
    case 'click':
    case 'type':
    case 'click_near':
    case 'post_comment':
    case 'wait_for_selector':
    case 'assert':
    case 'query':
    case 'scroll':
    case 'extract':
    case 'snapshot':
    case 'markdown': {
      const tabId = cmd.tab_id;
      const exists = await tabExists(tabId);
      if (!exists) throw new Error('tab_not_found: ' + tabId);
      // refs → CSS selector 映射（@eN 引用在 SW 内存）
      const resolveTarget = (t) => (t && t.startsWith('@e') ? getRefSelector(t) || t : t);
      switch (cmd.action) {
        case 'click':
          return await executeInTab(tabId, injClick, [resolveTarget(cmd.target)]);
        case 'click_near':
          return await executeInTab(tabId, injClickNear, [resolveTarget(cmd.anchor), cmd.button_text || '']);
        case 'type':
          return await executeInTab(tabId, injType, [
            resolveTarget(cmd.target), cmd.value || '', !!cmd.clear_first, !!cmd.submit_on_enter,
          ]);
        case 'wait_for_selector': {
          const timeout = Math.min(Math.max(cmd.timeout_ms || 10000, 1000), 60000);
          return await executeInTab(tabId, injWaitForSelector, [cmd.selector, timeout]);
        }
        case 'scroll': {
          const amount = Math.min(Math.max(cmd.amount || 400, 0), 20000);
          return await executeInTab(tabId, injScroll, [cmd.direction || 'down', amount]);
        }
        case 'extract': {
          const selectors = cmd.selectors || {};
          const r = await executeInTab(tabId, injExtract, [selectors]);
          return { data: r.data };
        }
        case 'snapshot': {
          const collected = await executeInTab(tabId, deps.accessibility.collectInPage, []);
          return deps.accessibility.assemble(collected);
        }
        case 'markdown':
          return await executeInTab(tabId, injMarkdown, []);
        case 'post_comment': {
          // 平台选择器由服务端 L3 适配器下发（无则缺省小红书——R17 真机实测）
          const inputSel = cmd.input_selector || '';
          const sendText = cmd.send_button_text || '';
          // 阶段一：定位输入框 + 聚焦（contenteditable 交给 CDP trusted 键入）
          const pre = await executeInTab(tabId, injPostCommentPrep, [cmd.value || '', inputSel]);
          if (!pre.input_found) throw new Error('comment_input_not_found');
          if (pre.needs_trusted) {
            // CJK 走逐字 insertText、ASCII 走 keyDown/keyUp（码表对齐 Puppeteer），
            // 含 humanize 时序与 infobar 稳定等待 —— 见 cdp/input.js
            await cdpInput.typeText(tabId, cmd.value || '');
          }
          // 阶段二：拿发送按钮坐标，CDP trusted 坐标点击（mouseMoved 轨迹前置）
          const btn = await executeInTab(tabId, injPostCommentSend, [inputSel, sendText]);
          if (!btn.ok) throw new Error(btn.error || 'send_button_not_found');
          await cdpInput.clickAt(tabId, btn.x, btn.y);
          // 阶段三：就地验证——评论渲染进评论区（MutationObserver+轮询，超时兜底）
          const vr = await executeInTab(tabId, injPostCommentVerify, [
            cmd.value || '', { timeoutMs: 6000, containerSelector: cmd.comment_container || '' },
          ]);
          if (!vr.verified) {
            throw new Error('post_comment 未生效: 输入框已清但评论区未见评论（可能被平台拦截/审核中）');
          }
          return { ok: true, posted: true, verified: true };
        }
        case 'assert': {
          const kind = cmd.assert || 'contains_text';
          const timeout = Math.min(Math.max(cmd.timeout_ms || 5000, 500), 60000);
          return await executeInTab(tabId, injAssert, [kind, cmd.value || cmd.target || '', cmd.selector || '', timeout]);
        }
        case 'query': {
          const kind = cmd.query || 'text';
          return await executeInTab(tabId, injQuery, [kind, cmd.selector || cmd.target || '', cmd.attribute || '']);
        }
      }
      break;
    }
    case 'screenshot': {
      // M3 定稿：captureVisibleTab 仅支持当前激活 tab —— 先激活再截（见设计文档 §6）
      const tabId = cmd.tab_id;
      if (cmd.activate_first) await activateTab(tabId);
      // 激活后让渲染一帧
      await new Promise((r) => setTimeout(r, 150));
      const dataUrl = await chrome.tabs.captureVisibleTab(tabId === null ? undefined : undefined, { format: 'png' });
      return { base64: dataUrl };
    }
    case 'wait': {
      const ms = Math.min(Math.max(cmd.ms || 0, 0), 60000);
      await new Promise((r) => setTimeout(r, ms));
      return {};
    }
    case 'close_tab':
      await closeTab(cmd.tab_id);
      return {};
    case 'tab_exists':
      return { exists: await tabExists(cmd.tab_id) };
    default:
      throw new Error('unknown_action_v4: ' + cmd.action);
  }
  throw new Error('unreachable: ' + cmd.action);
}

export const WAIT_SELECTOR_INTERVAL = WAIT_SELECTOR_INTERVAL_MS;
