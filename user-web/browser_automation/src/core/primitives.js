// primitives.js — 原语实现（MV3 executeScript func+args 形态）
// 硬约束：executeScript 的 func 会被序列化注入，闭包变量全部丢失，
// 所有外部值必须经 args 传入（官方 scripting API 规范）。

const WAIT_SELECTOR_INTERVAL_MS = 200;

// ---- 页面上下文函数（序列化注入，禁止引用外部闭包）----

// actionability 五项检查（F4/G13，对齐 Playwright 语义：visible/stable/enabled/hit-target/box）。
// mode='probe'：只做可点性检查+返回中心视口坐标（trusted 路径用）；
// 非 probe 路径（DOM 兜底）失败=element_not_interactable（服务端 ClassifyError=retry 类）。
function actionabilityCheck(el) {
  const style = window.getComputedStyle(el);
  if (style.display === 'none' || style.visibility === 'hidden' || style.opacity === '0') return 'not_visible';
  const r = el.getBoundingClientRect();
  if (r.width === 0 || r.height === 0) return 'zero_box';
  if (el.disabled || el.getAttribute('aria-disabled') === 'true') return 'disabled';
  // hit-target：中心点被什么接管（浮层遮挡检测，browser-use occlusion check 同构）
  try {
    const hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
    if (hit && hit !== el && !el.contains(hit) && !hit.contains(el)) return 'covered';
  } catch { /* jsdom 等无 elementFromPoint 环境：跳过该项 */ }
  // 注：stable（两帧同 box）在注入函数单帧执行模型里无法低成本实现；
  // trusted 路径的 infobar 500ms 等待 + CDP press 前 pointerSettle 时序已覆盖主要动画窗口。
  return null;
}

function injClick(target, mode) {
  const el = document.querySelector(target);
  if (!el) return { ok: false, error: 'element_not_found: ' + target };
  if (mode === 'probe') {
    // trusted 主通道（F1）：只做可点性检查 + 滚入视口 + 返回中心坐标，真实事件由 CDP 注入
    const err = actionabilityCheck(el);
    if (err === 'covered' || err === 'zero_box') {
      try { el.scrollIntoView?.({ block: 'center', inline: 'center' }); } catch { /* noop */ }
    }
    const retry = actionabilityCheck(el);
    if (retry) return { ok: false, error: 'element_not_interactable: ' + retry };
    const r = el.getBoundingClientRect();
    return {
      ok: true,
      x: Math.round(r.left + r.width / 2),
      y: Math.round(r.top + r.height / 2),
      jitter_radius: Math.min(r.width, r.height) / 2,
      href: el.tagName === 'A' ? (el.href || '') : '',
      page_url: location.href,
    };
  }
  // DOM 兜底路径保持宽松（旧语义）：jsdom/无几何环境也能走通；仅 disabled 硬失败
  if (el.disabled) return { ok: false, error: 'element_not_interactable: disabled' };
  try { el.scrollIntoView?.({ block: 'center', inline: 'center' }); } catch { /* jsdom/不可滚动时忽略 */ }
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

// mode='probe'（F1 trusted 主通道）：可编辑性检查 + 清空 + 聚焦放光标，键入交 CDP insertText；
// mode='fallback'（trusted 失败后兜底）：可编辑性检查 + DOM 输入管线注入。
function injType(target, value, clearFirst, submitOnEnter, mode) {
  const el = document.querySelector(target);
  if (!el) return { ok: false, error: 'element_not_found: ' + target };
  const style = window.getComputedStyle(el);
  if (style.display === 'none' || style.visibility === 'hidden') return { ok: false, error: 'element_not_interactable: not_visible' };
  if (el.disabled || el.getAttribute('aria-readonly') === 'true' || el.readOnly) return { ok: false, error: 'element_not_interactable: not_editable' };
  try { el.scrollIntoView?.({ block: 'center' }); } catch { /* noop */ }
  el.focus();
  if (mode === 'probe') {
    // 清空（CDP 键入语义=替换，先删净）
    if (clearFirst || el.value || el.textContent) {
      const sel = window.getSelection();
      if (el.isContentEditable) {
        const range = document.createRange();
        range.selectNodeContents(el);
        sel.removeAllRanges();
        sel.addRange(range);
        try { document.execCommand('delete', false, null); } catch { /* noop */ }
        el.dispatchEvent(new InputEvent('beforeinput', { bubbles: true, cancelable: true, inputType: 'deleteContentBackward', data: null }));
        el.dispatchEvent(new Event('input', { bubbles: true }));
        const r2 = document.createRange();
        r2.selectNodeContents(el);
        r2.collapse(false);
        sel.removeAllRanges();
        sel.addRange(r2);
      } else {
        el.value = '';
        el.dispatchEvent(new Event('input', { bubbles: true }));
      }
    } else if (el.isContentEditable) {
      const sel = window.getSelection();
      const range = document.createRange();
      range.selectNodeContents(el);
      range.collapse(false);
      sel.removeAllRanges();
      sel.addRange(range);
    }
    return { ok: true, editable: true };
  }
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
    let inserted;
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

// injNavigatedCheck trusted 点击后的纯观察检测（R25-Q1 修正）：
// 对比 probe 记录的 page_url——CDP 真实点击对 A 元素的导航由浏览器自己完成，
// trusted 路径禁止再 location.href= 接管（_blank 链接会双跳：新标签已由浏览器打开，
// 当前自动化 tab 又被多余带走）。href 接管仅保留给 DOM 兜底路径（injClick 内联逻辑）。
function injNavigatedCheck(probePageUrl) {
  try {
    return { ok: true, navigated: !!probePageUrl && location.href !== probePageUrl };
  } catch { return { ok: true, navigated: false }; }
}

function injScroll(direction, amount) {
  const dx = direction === 'left' ? -amount : direction === 'right' ? amount : 0;
  const dy = direction === 'up' ? -amount : direction === 'down' ? amount : 0;
  window.scrollBy(dx, dy);
  return { ok: true, x: window.scrollX, y: window.scrollY };
}

// 以锚元素为基准点击「同容器内的 button」——应对发送/提交按钮无稳定 class、
// 且 @e ref 每次快照重排导致硬编码 ref 不可靠的场景（如小红书评论发送按钮）
function injClickNear(anchorSelector, buttonText, mode) {
  const anchor = document.querySelector(anchorSelector);
  if (!anchor) return { ok: false, error: 'anchor_not_found: ' + anchorSelector };
  let root = anchor.parentElement;
  for (let depth = 0; depth < 4 && root; depth++) {
    const btns = Array.from(root.querySelectorAll('button'));
    const hit = buttonText
      ? btns.find((b) => (b.innerText || '').trim().includes(buttonText))
      : btns[0];
    if (hit) {
      if (mode === 'probe') {
        const err = actionabilityCheck(hit);
        if (err) {
          try { hit.scrollIntoView?.({ block: 'center', inline: 'center' }); } catch { /* noop */ }
        }
        const retry = actionabilityCheck(hit);
        if (retry) return { ok: false, error: 'element_not_interactable: ' + retry };
        const r = hit.getBoundingClientRect();
        return {
          ok: true,
          x: Math.round(r.left + r.width / 2),
          y: Math.round(r.top + r.height / 2),
          jitter_radius: Math.min(r.width, r.height) / 2,
          clicked: (hit.innerText || 'button').trim(),
        };
      }
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
  if (!el) return { ok: false, input_found: false, error: 'comment_input_not_found' };

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
 * F2②：回包带 evidence（命中容器数 + 首个评论条目文本节选）——finalize 落库审计用。
 */
function injPostCommentVerify(targetText, opts) {
  const o = opts || {};
  const containerSel = o.containerSelector || '.comments-container, .comments-el, [class*=comment-list], [class*=comments]';
  const deadline = Date.now() + (o.timeoutMs || 5000);
  const norm = (s) => (s || '').replace(/\s+/g, '');
  const want = norm(targetText);
  // innerText 在非渲染上下文（jsdom/后台 tab 未布局）为空——textContent 兜底
  const textOf = (n) => (n ? (n.innerText || n.textContent || '') : '');

  const evidenceOf = () => {
    const containers = document.querySelectorAll(containerSel);
    let ev = { containers: containers.length };
    if (o.itemSelector) {
      // R25 证据语义修正（session192 实测）：优先取**全文恰等于目标文本**的条目=我们刚发的那条；
      // 「包含目标」会误命中他人引用了同样文字的历史评论（如「今天真好吃」⊂「今天真好吃吗」）。
      // 无精确命中时退化为含目标文本的条目（verified 判定仍是 contains 语义，此处只关乎证据归属）。
      let containsHit = '';
      for (const c of containers) {
        const items = c.querySelectorAll(o.itemSelector);
        for (const item of items) {
          const t = textOf(item).trim();
          if (!t) continue;
          const nt = norm(t);
          if (nt === want) { ev.item_text = t.slice(0, 200); ev.own = true; return ev; }
          if (!containsHit && nt.includes(want)) containsHit = t;
        }
      }
      if (containsHit) { ev.item_text = containsHit.slice(0, 200); ev.matched = true; }
    }
    return ev;
  };

  // 立即查一次（评论可能已渲染）
  const checkNow = () => {
    const containers = document.querySelectorAll(containerSel);
    for (const c of containers) {
      if (norm(textOf(c)).includes(want)) return true;
    }
    // 兜底：全文搜（评论区容器类名可能变）
    return norm(textOf(document.body)).includes(want);
  };
  if (checkNow()) return { ok: true, posted: true, verified: true, evidence: evidenceOf() };

  return new Promise((resolve) => {
    const done = (result) => {
      try { observer.disconnect(); } catch { /* noop */ }
      resolve(result);
    };
    const observer = new MutationObserver(() => {
      if (checkNow()) done({ ok: true, posted: true, verified: true, evidence: evidenceOf() });
    });
    try {
      observer.observe(document.body, { childList: true, subtree: true, characterData: true });
    } catch { /* 容器不可观察时退化为轮询 */ }
    const poll = setInterval(() => {
      if (checkNow()) { clearInterval(poll); done({ ok: true, posted: true, verified: true, evidence: evidenceOf() }); return; }
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

/**
 * racedExecuteInTab R26-2：executeScript 竞速超时——重页（小红书评论区渲染/主线程拥堵）
 * 会把注入队列堵到数十秒，NM 回包赶不上服务端超时产生「假失败真提交」灰态。
 * 只读定位类调用加本地 deadline：超时=注入从未执行（页面主线程没轮到它），
 * 返回显式 timeout 错误，与「注入执行了但失败」区分——提交前灰态归 Go finalize 处置。
 */
function raceTimeout(promise, ms, label) {
  let timer;
  const guard = new Promise((_, rej) => {
    timer = setTimeout(() => rej(new Error(`${label}_inject_timeout_${ms}ms`)), ms);
  });
  return Promise.race([promise, guard]).finally(() => clearTimeout(timer));
}

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
  // F6 基线联动：导航即清该 tab 的新元素基线（下一帧重新建立，不把整页标成新元素）。
  // resetBaseline 为可选依赖（旧测试 deps 未提供时静默跳过）。
  const resetBaseline = deps.accessibility.resetBaseline || (() => {});

  switch (cmd.action) {
    case 'open_tab': {
      if (!cmd.url || !/^https?:\/\//i.test(cmd.url)) {
        throw new Error('open_tab 需要合法 http(s) URL，收到: ' + (cmd.url || '(空)'));
      }
      const tab = await openTab(cmd.url, cmd.active === true);
      resetBaseline(tab.id);
      return { chrome_tab_id: tab.id, title: tab.title || '' };
    }
    case 'click':
    case 'type':
    case 'click_near':
    case 'comment_prep':
    case 'comment_send':
    case 'comment_verify':
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
        case 'click': {
          // F1 铁律 2 收口：写操作主通道=CDP trusted（probe 定位坐标→贝塞尔轨迹点击）；
          // CDP 不可用（调试器被占/扩展受限）才降级 DOM 合成兜底——兜底结果标注 channel。
          try {
            const probe = await executeInTab(tabId, injClick, [resolveTarget(cmd.target), 'probe']);
            await cdpInput.clickAt(tabId, probe.x, probe.y, { jitterRadius: probe.jitter_radius });
            // R25-Q1：点击生效帧后短暂等路由，再纯读 location 对比判定同页导航；
            // 检测注入失败通常=页面正在导航中，按已导航处理。不再主动接管 href。
            await new Promise((r) => setTimeout(r, 300));
            let navigated = false;
            try {
              const nav = await executeInTab(tabId, injNavigatedCheck, [probe.page_url]);
              navigated = !!nav.navigated;
            } catch {
              navigated = true;
            }
            if (navigated) resetBaseline(tabId);
            return { ok: true, navigated, channel: 'cdp' };
          } catch (e) {
            if (!String(e?.message || e).includes('element_not_found')) {
              // 元素在但 CDP 失败（attach 被拒/调试器占用）：DOM 兜底
              const r = await executeInTab(tabId, injClick, [resolveTarget(cmd.target), 'fallback']).catch(() => null);
              if (r?.ok) {
                if (r.navigated) resetBaseline(tabId);
                return { ...r, channel: 'dom_fallback' };
              }
            }
            throw e;
          }
        }
        case 'click_near': {
          try {
            const probe = await executeInTab(tabId, injClickNear, [resolveTarget(cmd.anchor), cmd.button_text || '', 'probe']);
            await cdpInput.clickAt(tabId, probe.x, probe.y, { jitterRadius: probe.jitter_radius });
            return { ok: true, clicked: probe.clicked, channel: 'cdp' };
          } catch (e) {
            if (!String(e?.message || e).includes('anchor_not_found') && !String(e?.message || e).includes('button_not_found')) {
              const r = await executeInTab(tabId, injClickNear, [resolveTarget(cmd.anchor), cmd.button_text || '', 'fallback']).catch(() => null);
              if (r?.ok) return { ...r, channel: 'dom_fallback' };
            }
            throw e;
          }
        }
        case 'type': {
          const sel = resolveTarget(cmd.target);
          try {
            await executeInTab(tabId, injType, [sel, cmd.value || '', !!cmd.clear_first, !!cmd.submit_on_enter, 'probe']);
            await cdpInput.typeText(tabId, cmd.value || '');
            if (cmd.submit_on_enter) {
              await cdpInput.pressEnter(tabId).catch(() => {});
            }
            return { ok: true, editable: true, channel: 'cdp' };
          } catch (e) {
            if (!String(e?.message || e).includes('element_not_found')) {
              const r = await executeInTab(tabId, injType, [sel, cmd.value || '', !!cmd.clear_first, !!cmd.submit_on_enter, 'fallback']).catch(() => null);
              if (r?.ok) return { ...r, channel: 'dom_fallback' };
            }
            throw e;
          }
        }
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
          // F6 新元素标记：基线按 tab 归属（两 tab 快照互不污染 diff 集）
          return deps.accessibility.assemble(collected, tabId);
        }
        case 'markdown':
          return await executeInTab(tabId, injMarkdown, []);
        case 'comment_prep':
        case 'comment_send': {
          // F2②（G11 正确版）：三段式编排收口 Go——扩展只暴露无状态子命令，
          // 提交（comment_send）与验证（comment_verify 轮询）分离，可中断可归因。
          // 旧一站式 post_comment 兼容路径已删（服务端 v3.41.0 起只发子命令，单一路径防分叉）。
          // 平台选择器由服务端 L3 适配器下发（无则缺省小红书——R17 真机实测）
          const inputSel = cmd.input_selector || '';
          const sendText = cmd.send_button_text || '';
          if (cmd.action === 'comment_prep') {
            // 阶段一：定位输入框 + 聚焦（contenteditable 交给 CDP trusted 键入）。
            // R26-2：定位注入加 15s 竞速 deadline——重页注入队列拥堵时早返明确错误
            // （注入未执行，无副作用），不再陪跑到服务端超时产生灰态。
            const pre = await raceTimeout(executeInTab(tabId, injPostCommentPrep, [cmd.value || '', inputSel]), cmd.inject_timeout_ms || 15000, 'comment_prep');
            if (!pre.input_found) throw new Error('comment_input_not_found');
            if (pre.needs_trusted) {
              // CJK 走逐字 insertText、ASCII 走 keyDown/keyUp（码表对齐 Puppeteer），
              // 含 humanize 时序与 infobar 稳定等待 —— 见 cdp/input.js
              await cdpInput.typeText(tabId, cmd.value || '');
            }
            return { ok: true, input_found: true, needs_trusted: !!pre.needs_trusted, input_text: pre.input_text || '' };
          }
          // comment_send 阶段二：拿发送按钮坐标，CDP trusted 坐标点击（mouseMoved 轨迹前置）。
          // 提交不可逆：本命令绝不含 verify——verify 超时态由 Go 侧 finalize 轮询处置（禁双发）。
          // R26-2：按钮定位注入同样加 15s 竞速——**注入未执行=点击从未发生=无副作用**，
          // 错误名 comment_send_inject_timeout 供服务端归类（pre-click 灰态≠post-click 未知态）。
          const btn = await raceTimeout(executeInTab(tabId, injPostCommentSend, [inputSel, sendText]), cmd.inject_timeout_ms || 15000, 'comment_send');
          if (!btn.ok) throw new Error(btn.error || 'send_button_not_found');
          await cdpInput.clickAt(tabId, btn.x, btn.y);
          return { ok: true, sent: true };
        }
        case 'comment_verify': {
          // 只读验证（可重试/可中断）：评论是否渲染进评论区。Go finalize 轮询调用。
          return await executeInTab(tabId, injPostCommentVerify, [
            cmd.value || '',
            { timeoutMs: cmd.timeout_ms || 3000, containerSelector: cmd.comment_container || '', itemSelector: cmd.comment_item_text || '' },
          ]);
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
      // M3 定稿：captureVisibleTab 仅支持当前激活 tab —— 先激活再截（见设计文档 §6）。
      // G9 勘误：第一参数是 windowId 不是 tabId；本扩展单窗常态，恒传 undefined（当前聚焦窗）。
      const tabId = cmd.tab_id;
      if (cmd.activate_first) await activateTab(tabId);
      // 激活后让渲染一帧
      await new Promise((r) => setTimeout(r, 150));
      const dataUrl = await chrome.tabs.captureVisibleTab(undefined, { format: 'png' });
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
