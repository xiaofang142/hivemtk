// primitives.js — 原语实现（MV3 executeScript func+args 形态）
// 硬约束：executeScript 的 func 会被序列化注入，闭包变量全部丢失，
// 所有外部值必须经 args 传入（官方 scripting API 规范）。

const WAIT_SELECTOR_INTERVAL_MS = 200;

// ---- 页面上下文函数（序列化注入，禁止引用外部闭包）----
// 铁律（批14 真机实证，闸门见 test/inject-sandbox.js）：chrome.scripting.executeScript
// 只把 func.toString() 送进页面，模块作用域里的任何自由变量在页面侧都是 ReferenceError，
// 且 Chrome 回包 result:null —— 调用方会把它误读成「注入没返回/CDP 不可用」。
// 下面的 actionability 检查因此在 injClick / injClickNear / injPostCommentSend 里各内联一份
//（三处必须同步改）；语义对齐 Playwright _retryPointerAction 的 visible/stable/enabled/hit-target/box 五项。
// stable（批17 §8.2-1a）也在注入函数内部实现：rAF 双帧比盒，直到连续两帧同 box 才交坐标。
// 单帧模型不是障碍——Chrome 会等注入函数返回的 Promise 结算（injWaitForSelector 早就靠这条），
// 所以「等落位」仍是一次 evaluate、零额外往返。结算窗上限（500ms）与帧间隔（16ms）随检查逻辑
// 一起内联在各份 probe 里：注入函数引用不到模块作用域，改数值同样是三处一起改。
// 点后身份复核的位移容差（视口像素）：抖动落点与 1px 布局误差不算移动，真挪位算。
// 这个值在 SW 侧作为参数下发，所以只有一份。
const IDENTITY_RECHECK_TOLERANCE_PX = 5;

async function injClick(target, mode) {
  const el = document.querySelector(target);
  if (!el) return { ok: false, error: 'element_not_found: ' + target };
  if (mode === 'probe') {
    // trusted 主通道（F1）：只做可点性检查 + 滚入视口 + 返回中心坐标，真实事件由 CDP 注入
    const check = (node) => {
      const st = window.getComputedStyle(node);
      if (st.display === 'none' || st.visibility === 'hidden' || st.opacity === '0') return 'not_visible';
      const box = node.getBoundingClientRect();
      if (box.width === 0 || box.height === 0) return 'zero_box';
      if (node.disabled || node.getAttribute('aria-disabled') === 'true') return 'disabled';
      // hit-target：中心点被什么接管（浮层遮挡检测，browser-use occlusion check 同构）
      try {
        const hit = document.elementFromPoint(box.left + box.width / 2, box.top + box.height / 2);
        if (hit && hit !== node && !node.contains(hit) && !hit.contains(node)) return 'covered';
      } catch { /* 无 elementFromPoint 的环境：跳过该项 */ }
      return null;
    };
    // stable（批17 §8.2-1a，三份内联同步改）：连续两帧同 box 才算落位；一直动就一帧坐标都不下发。
    // 节拍用 rAF，但真机后台 tab（open_tab active=false）不出帧，所以并挂 setTimeout 兜底——
    // 只等 rAF 的版本会在隐藏页里永挂。先比盒后查 deadline：被节流的静止元素照样一次通过。
    // 500ms 上限远小于 comment_send 的 15s 注入竞速窗：超窗会被切成 *_inject_timeout，
    // 那是「点击从未发生」的归因，被 stable 借用就是假归因。
    const settleBox = async (node) => {
      const same = (a, b) => a.left === b.left && a.top === b.top && a.width === b.width && a.height === b.height;
      const frame = () => new Promise((res) => {
        let done = false;
        const fin = () => { if (!done) { done = true; res(); } };
        try { requestAnimationFrame(fin); } catch { /* 无 rAF 的引擎：只靠定时器 */ }
        setTimeout(fin, 50);
      });
      const deadline = Date.now() + 500;
      let prev = node.getBoundingClientRect();
      for (;;) {
        await frame();
        const cur = node.getBoundingClientRect();
        if (same(prev, cur)) return { box: cur };
        prev = cur;
        if (Date.now() >= deadline) return { error: 'unstable' };
      }
    };
    let err = check(el);
    if (err === 'covered' || err === 'zero_box') {
      try { el.scrollIntoView?.({ block: 'center', inline: 'center' }); } catch { /* noop */ }
      err = check(el);
    }
    if (err) return { ok: false, error: 'element_not_interactable: ' + err };
    const settled = await settleBox(el);
    if (settled.error) return { ok: false, error: 'element_not_interactable: ' + settled.error };
    // 落位后重查一次可点性：等待期间浮层可能才渲染完，用旧那帧的遮挡结论点新位置的坐标
    // 等于把 hit-target 检查作废——而中心点判据用的是结算后的 box。
    err = check(el);
    if (err) return { ok: false, error: 'element_not_interactable: ' + err };
    const r = settled.box;
    return {
      ok: true,
      x: Math.round(r.left + r.width / 2),
      y: Math.round(r.top + r.height / 2),
      jitter_radius: Math.min(r.width, r.height) / 2,
      href: el.tagName === 'A' ? (el.href || '') : '',
      page_url: location.href,
    };
  }
  // DOM 兜底路径保持宽松（旧语义）：jsdom/无几何环境也能走通；仅 disabled 硬失败。
  // 可见性/遮挡一项不在这里重判——那是 probe 的职责，且 dispatch 层已保证
  // probe 不通过就不会走到这条路径（批14：闸门不得被兜底绕过）。
  if (el.disabled) return { ok: false, error: 'element_not_interactable: disabled' };
  try { el.scrollIntoView?.({ block: 'center', inline: 'center' }); } catch { /* jsdom/不可滚动时忽略 */ }
  // SPA（React/Vue 合成事件）监听的是指针序列，所以一次动作 = 一轮指针事件 + 一个 click。
  // click 由 el.click() 收尾：它既是唯一的那个 click 事件，又带浏览器激活行为
  //（表单提交 / 链接跳转 / 勾选切换）。批14 真机证据（session 536/537）：原先在
  // el.click() 之外又 dispatchEvent(new MouseEvent('click'))，页面收到两个 click，
  // 「发送」按钮的 handler 跑了两遍 = 公开内容双发且不可撤回。
  const opts = { bubbles: true, cancelable: true, view: window, pointerId: 1, isPrimary: true };
  try {
    el.dispatchEvent(new PointerEvent('pointerdown', opts));
    el.dispatchEvent(new MouseEvent('mousedown', opts));
    el.dispatchEvent(new PointerEvent('pointerup', opts));
    el.dispatchEvent(new MouseEvent('mouseup', opts));
  } catch { /* 无 PointerEvent 的内核：指针层整体跳过，click 仍由 el.click() 给出 */ }
  el.click();
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
    // Enter 只在被要求时发：富文本评论框上「键入」和「提交」是两件事，
    // 无条件派发等于让一个 type 步骤带上不可撤回的提交语义（批14）。
    if (submitOnEnter) {
      el.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', code: 'Enter', bubbles: true }));
    }
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

// injClickIdentityCheck 批17 §8.2-1b：trusted 点击**之后**的身份复核（只有写步下发这条）。
// 探测与真点之间隔着拟人贝塞尔轨迹的飞行时间（可达数百 ms），这期间轮播/懒加载/toast 挪动页面，
// 就会点到一个从未被探测过的元素，而旧回包仍是 {ok:true, channel:'cdp'}——静默假绿。
// 只复核「页面侧此刻还观测得到」的三件事：selector 仍可解析、中心点未挪出容差、
// 该点 hit-target 仍是这个元素。观测不到的那一类如实记在 spec §8.2-1(b)：
// 同位置被换成同 selector 的另一个节点，除非页内埋身份令牌，否则无从分辨。
function injClickIdentityCheck(target, probeX, probeY, tolerancePx) {
  const el = document.querySelector(target);
  if (!el) return { ok: false, error: 'element_moved: 点后目标已不可解析（被移除或被改版换掉定位）' };
  const r = el.getBoundingClientRect();
  const cx = r.left + r.width / 2;
  const cy = r.top + r.height / 2;
  if (Math.abs(cx - probeX) > tolerancePx || Math.abs(cy - probeY) > tolerancePx) {
    return { ok: false, error: 'element_moved: 中心点从 ' + probeX + ',' + probeY + ' 挪到 ' + Math.round(cx) + ',' + Math.round(cy) };
  }
  try {
    const hit = document.elementFromPoint(probeX, probeY);
    if (hit && hit !== el && !el.contains(hit) && !hit.contains(el)) {
      return { ok: false, error: 'element_moved: 落点已被 ' + (hit.id || hit.tagName) + ' 接管' };
    }
  } catch { /* 无 elementFromPoint 的环境：该项按现状跳过（与 probe 同口径） */ }
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
async function injClickNear(anchorSelector, buttonText, mode) {
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
        // 与 injClick 同一份可点性检查，内联第二次：注入函数不得引用模块作用域的自由变量
        const check = (node) => {
          const st = window.getComputedStyle(node);
          if (st.display === 'none' || st.visibility === 'hidden' || st.opacity === '0') return 'not_visible';
          const box = node.getBoundingClientRect();
          if (box.width === 0 || box.height === 0) return 'zero_box';
          if (node.disabled || node.getAttribute('aria-disabled') === 'true') return 'disabled';
          try {
            const taken = document.elementFromPoint(box.left + box.width / 2, box.top + box.height / 2);
            if (taken && taken !== node && !node.contains(taken) && !taken.contains(node)) return 'covered';
          } catch { /* 无 elementFromPoint 的环境：跳过该项 */ }
          return null;
        };
        // stable：与 injClick 同一份，内联（口径与预算的理由见彼处注释）
        const settleBox = async (node) => {
          const same = (a, b) => a.left === b.left && a.top === b.top && a.width === b.width && a.height === b.height;
          const frame = () => new Promise((res) => {
            let done = false;
            const fin = () => { if (!done) { done = true; res(); } };
            try { requestAnimationFrame(fin); } catch { /* 无 rAF 的引擎：只靠定时器 */ }
            setTimeout(fin, 50);
          });
          const deadline = Date.now() + 500;
          let prev = node.getBoundingClientRect();
          for (;;) {
            await frame();
            const cur = node.getBoundingClientRect();
            if (same(prev, cur)) return { box: cur };
            prev = cur;
            if (Date.now() >= deadline) return { error: 'unstable' };
          }
        };
        // 一条能再解析回同一个节点的路径：有 id 用 id，否则逐层 tag + nth-of-type。
        const pathOf = (node) => {
          if (node.id) return '#' + node.id;
          const parts = [];
          let cur = node;
          while (cur && cur.nodeType === 1 && cur.tagName !== 'HTML') {
            const parent = cur.parentElement;
            if (!parent) break;
            const sameTag = Array.from(parent.children).filter((c) => c.tagName === cur.tagName);
            const suffix = sameTag.length > 1 ? ':nth-of-type(' + (sameTag.indexOf(cur) + 1) + ')' : '';
            parts.unshift(cur.tagName.toLowerCase() + suffix);
            cur = parent;
          }
          return parts.join(' > ');
        };
        let err = check(hit);
        if (err === 'covered' || err === 'zero_box') {
          try { hit.scrollIntoView?.({ block: 'center', inline: 'center' }); } catch { /* noop */ }
          err = check(hit);
        }
        if (err) return { ok: false, error: 'element_not_interactable: ' + err };
        const settled = await settleBox(hit);
        if (settled.error) return { ok: false, error: 'element_not_interactable: ' + settled.error };
        err = check(hit);
        if (err) return { ok: false, error: 'element_not_interactable: ' + err };
        const r = settled.box;
        return {
          ok: true,
          x: Math.round(r.left + r.width / 2),
          y: Math.round(r.top + r.height / 2),
          jitter_radius: Math.min(r.width, r.height) / 2,
          clicked: (hit.innerText || 'button').trim(),
          // 点后身份复核要把「这次点的是哪个元素」再解析一遍，而 click_near 的定位本来就是
          // 锚点+文本现算的（没有调用方持有的 selector）——回传一条可再解析的路径，
          // 复核才有对象。没有它就只能「无从复核却照样 ok」，那正是本批要堵的静默绿。
          selector: pathOf(hit),
        };
      }
      try { hit.scrollIntoView?.({ block: 'center' }); } catch { /* noop */ }
      // 一次动作一轮指针事件 + 一个 click（el.click() 收尾，理由见 injClick 同处注释）
      const opts = { bubbles: true, cancelable: true, view: window, pointerId: 1, isPrimary: true };
      try {
        hit.dispatchEvent(new PointerEvent('pointerdown', opts));
        hit.dispatchEvent(new MouseEvent('mousedown', opts));
        hit.dispatchEvent(new PointerEvent('pointerup', opts));
        hit.dispatchEvent(new MouseEvent('mouseup', opts));
      } catch { /* 无 PointerEvent 的内核：指针层整体跳过，click 仍由 hit.click() 给出 */ }
      hit.click();
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
  let bodyLines = 0;
  document.querySelectorAll('h1,h2,h3,p,li').forEach((el) => {
    const t = (el.innerText || el.textContent || '').trim();
    if (!t) return;
    bodyLines++;
    const tag = el.tagName.toLowerCase();
    if (tag === 'h1') lines.push(`# ${t}`, '');
    else if (tag === 'h2') lines.push(`## ${t}`, '');
    else if (tag === 'h3') lines.push(`### ${t}`, '');
    else if (tag === 'li') lines.push(`- ${t}`);
    else lines.push(t, '');
  });
  // 零可读内容必须判红，而不是回一句空标题壳子：真机 session=432 实测
  // open_tab 未等加载时读到 "# \n" 却整轮判成功（假绿）。这个特征与「页面确实没字」
  // 无法区分，但对自动化而言两者都意味着「这一步没拿到东西」，交上层重试/换路径。
  // 有标题就仍算读到东西——正文全在 div 里的 SPA 是既有抽取边界，不在这儿判死。
  if (!title && bodyLines === 0) {
    return { ok: false, error: 'empty_document: 页面无可读文本（未加载完成，或正文不在可读标签里）' };
  }
  const full = lines.join('\n');
  const markdown = full.slice(0, 64 * 1024);
  // 截断必须如实上报：只回截断后的长度，消费方会把 64KiB 当成整页内容。
  return {
    ok: true,
    markdown,
    markdown_chars: markdown.length,
    full_chars: full.length,
    truncated: markdown.length < full.length,
    content_empty: bodyLines === 0,
  };
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
async function injPostCommentSend(inputSelector, sendButtonText) {
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
  // 可点性检查与 injClick probe 同规格，内联第三份（注入函数不得引用模块作用域的自由变量）。
  // 提交点比一次普通 click 更该严：往浮层上落一次坐标 = 未知副作用还被记成「提交已跨越」。
  const check = (node) => {
    const st = window.getComputedStyle(node);
    if (st.display === 'none' || st.visibility === 'hidden' || st.opacity === '0') return 'not_visible';
    const box = node.getBoundingClientRect();
    if (box.width === 0 || box.height === 0) return 'zero_box';
    if (node.disabled || node.getAttribute('aria-disabled') === 'true') return 'disabled';
    try {
      const hit = document.elementFromPoint(box.left + box.width / 2, box.top + box.height / 2);
      if (hit && hit !== node && !node.contains(hit) && !hit.contains(node)) return 'covered';
    } catch { /* 无 elementFromPoint 的环境：跳过该项 */ }
    return null;
  };
  // CDP 坐标是视口坐标：先滚进视口再判，遮挡/零框可能只是滚动前的假象
  btn.scrollIntoView?.({ block: 'center' });
  let blocked = check(btn);
  if (blocked === 'covered' || blocked === 'zero_box') {
    btn.scrollIntoView?.({ block: 'center', inline: 'center' });
    blocked = check(btn);
  }
  if (blocked) return { ok: false, error: 'send_button_not_interactable: ' + blocked };
  // stable（内联第三份）：发送按钮最常在提交瞬间被禁用/重排/被确认弹层接管，
  // 落位前拿到的坐标点下去 = 提交动作落在一个从未被探测过的元素上，且回包仍写 sent=true。
  const settleBox = async (node) => {
    const same = (a, b) => a.left === b.left && a.top === b.top && a.width === b.width && a.height === b.height;
    const frame = () => new Promise((res) => {
      let done = false;
      const fin = () => { if (!done) { done = true; res(); } };
      try { requestAnimationFrame(fin); } catch { /* 无 rAF 的引擎：只靠定时器 */ }
      setTimeout(fin, 50);
    });
    const deadline = Date.now() + 500;
    let prev = node.getBoundingClientRect();
    for (;;) {
      await frame();
      const cur = node.getBoundingClientRect();
      if (same(prev, cur)) return { box: cur };
      prev = cur;
      if (Date.now() >= deadline) return { error: 'unstable' };
    }
  };
  const settled = await settleBox(btn);
  if (settled.error) return { ok: false, error: 'send_button_not_interactable: ' + settled.error };
  // 落位后重判一次：等待期间才挂上来的浮层/按钮 disabled 都必须拦下这次提交
  blocked = check(btn);
  if (blocked) return { ok: false, error: 'send_button_not_interactable: ' + blocked };
  const r = settled.box;
  return { ok: true, x: Math.round(r.left + r.width / 2), y: Math.round(r.top + r.height / 2) };
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
  // 输入区节点判据（三条缺一不可）：表单控件 tagName / isContentEditable 主判据 /
  // closest 兜底（jsdom 等引擎不实现该属性，缺它就会出现「草稿仍留在输入框 → 零提交假绿」形态）。
  const inInputNode = (p) => {
    if (!p) return false;
    if (/^(INPUT|TEXTAREA|SELECT|OPTION)$/.test(p.tagName)) return true;
    if (p.isContentEditable) return true;
    try {
      return !!p.closest('[contenteditable]:not([contenteditable="false"])');
    } catch {
      return false; // 无 closest 的旧引擎：按可见文本继续判
    }
  };
  // 跨节点累积（评论正文可能被拆进多个文本节点），限窗避免长页 O(n²)
  const textOutsideInputsIncludes = (needle) => {
    const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
    let node;
    let acc = '';
    while ((node = walker.nextNode())) {
      if (inInputNode(node.parentElement)) continue;
      acc += norm(node.nodeValue);
      if (acc.length > 6000) acc = acc.slice(-3000);
      if (needle && acc.includes(needle)) return true;
    }
    return false;
  };
  // 子树版（F11b 批6）：容器/条目级取文同样必须剔除输入子树。
  // 真机 Leg X 实测形态——小红书评论输入框**就在** .comments-container 里面，
  // 旧实现整容器 textOf(c) 直接命中那条未提交草稿 → 零提交也报 verified=true。
  // 不可逆动作的自检假绿是所有假里最坏的一类（用户据此认为评论已发出）。
  const textExcludingInputs = (root) => {
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
    let node;
    let out = '';
    while ((node = walker.nextNode())) {
      if (inInputNode(node.parentElement)) continue;
      out += norm(node.nodeValue);
    }
    return out;
  };

  const evidenceOf = () => {
    const containers = document.querySelectorAll(containerSel);
    let ev = { containers: containers.length };
    if (o.itemSelector) {
      // R25 证据语义修正（session192 实测）：优先取**全文恰等于目标文本**的条目=我们刚发的那条；
      // 「包含目标」会误命中他人引用了同样文字的历史评论（如「今天真好吃」⊂「今天真好吃吗」）。
      // 无精确命中时退化为含目标文本的条目（verified 判定仍是 contains 语义，此处只关乎证据归属）。
      // 条目文本走 textExcludingInputs：verified 与 evidence 必须来自同一套「剔除输入节点」的
      // 文本，否则会产出「verified=false 但证据里有这条」的自相矛盾审计包。
      let containsHit = '';
      for (const c of containers) {
        const items = c.querySelectorAll(o.itemSelector);
        for (const item of items) {
          const t = textExcludingInputs(item);
          if (!t) continue;
          if (t === want) { ev.item_text = (textOf(item).trim()).slice(0, 200); ev.own = true; return ev; }
          if (!containsHit && t.includes(want)) containsHit = textOf(item).trim();
        }
      }
      if (containsHit) { ev.item_text = containsHit.slice(0, 200); ev.matched = true; }
    }
    return ev;
  };

  // 立即查一次（评论可能已渲染）
  const checkNow = () => {
    if (!want) return false;
    const containers = document.querySelectorAll(containerSel);
    for (const c of containers) {
      if (textExcludingInputs(c).includes(want)) return true;
    }
    // 兜底：全文搜（评论区容器类名可能变）——但必须先排除输入节点自身。
    return textOutsideInputsIncludes(want);
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
    // r 为空与 r.ok=false 是两种成因：后者是注入跑了并给出理由（走 r.error），
    // 前者是 Chrome 在该帧于注入期间被导航/销毁时返回 result=undefined
    //（真机 xhs 未登录重定向实测：旧文案把两者混成 'executeScript failed'，无法归因）
    throw new Error(r?.error || 'inject_no_result(该帧注入未返回，通常是页面正在导航/帧已销毁)');
  }
  return r;
}

// isUnackedClick CDP 可信点击的「结局未知」态：mousePressed/mouseReleased 已下发进渲染进程
// 队列，只是 ack 没回来（后台 tab 不出帧时实测可达 5s+/条）。
// 批9：这种态**绝不**走 DOM el.click() 兜底——兜底等于在「可能已经点中」之上再点一次，
// 对提交/发送按钮就是双发（公开评论不可撤回）。宁可把未知态原样抛给上层，
// 由服务端 finalize 用只读验证裁决（写步骤 retries=0，见 Go 侧台账）。
// 其余 CDP 失败（attach 被拒、调试器被占）事件从未下发，兜底仍然保留。
function isUnackedClick(msg) {
  return String(msg || '').includes('click_unacked');
}

// asIdentityVerdict 批17(b) 点后复核的归因：复核自己给出 element_moved 就原样上抛（那是结论）；
// 复核跑不动（注入没回包/该帧被销毁）必须换名成 identity_recheck_failed——未知态既不能顺着
// 「没抛错就是 ok」变成静默绿，也不能冒充「元素挪位」这个已经查明成因的具体结论。
function asIdentityVerdict(e) {
  const msg = String(e?.message || e);
  if (msg.includes('element_moved')) return new Error(msg);
  return new Error('identity_recheck_failed(点后复核未能给出结论): ' + msg);
}

/**
 * dispatch 执行一条命令帧（server → Host → 扩展）
 * @param {Map<string,Function>} deps 依赖注入（tab-manager / accessibility），便于测试
 * @returns {Promise<Object>} 回包 data
 */
export async function dispatch(cmd, deps) {
  const { openTab, waitForLoad, closeTab, activateTab, tabExists } = deps.tabManager;
  const { getRefSelector } = deps.accessibility;
  // F6 基线联动：导航即清该 tab 的新元素基线（下一帧重新建立，不把整页标成新元素）。
  // resetBaseline 为可选依赖（旧测试 deps 未提供时静默跳过）。
  const resetBaseline = deps.accessibility.resetBaseline || (() => {});

  switch (cmd.action) {
    case 'resolve_ref': {
      // A1 selector 自愈回路：LLM 依据快照行选 @eN ref，Go 用本命令换回真实 CSS
      // （ref→cssPath 映射只在 SW 内存、按 tab 分桶，页面导航/新快照会重置——调用方须紧邻快照使用）。
      const ref = String(cmd.ref || '');
      return { selector: ref.startsWith('@e') ? getRefSelector(ref, cmd.tab_id) || '' : ref };
    }
    case 'open_tab': {
      if (!cmd.url || !/^https?:\/\//i.test(cmd.url)) {
        throw new Error('open_tab 需要合法 http(s) URL，收到: ' + (cmd.url || '(空)'));
      }
      const tab = await openTab(cmd.url, cmd.active === true);
      resetBaseline(tab.id);
      // 等页面这一帧真的加载出来再回包（假绿收口，见 tab-manager.waitForLoad）。
      // 命令预算 30s，缺省等待 10s，上限 30s 由 waitForLoad 自身夹紧，永不吃掉命令超时。
      const load = await waitForLoad(tab.id, cmd.load_timeout_ms);
      return { chrome_tab_id: tab.id, title: load.title || tab.title || '', loaded: load.loaded, load_wait_ms: load.wait_ms };
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
      // refs → CSS selector 映射（@eN 引用在 SW 内存、按 tab 分桶，批9）
      // 解析不出来的 @eN=快照已失效（导航/新帧重置），绝不能原样塞进 querySelector：
      // 那会变成 DOMException「'@e77' is not a valid selector」，把「元素未命中」伪装成语法错误，
      // 而 A1 自愈只认 *_not_found 结构化 token（executor_selfheal.go isSelectorMiss）——
      // 正是它最该接住的「LLM 选的 ref 过期了」这一案被错误类型吃掉，自愈回路永不触发。
      // missToken 由调用点给出该听的错误名，保持归因准确。
      const resolveTarget = (t, missToken) => {
        if (!t || !t.startsWith('@e')) return t;
        const sel = getRefSelector(t, tabId);
        if (sel) return sel;
        throw new Error((missToken || 'element_not_found') + ': ref 已失效（快照被重置）' + t);
      };
      switch (cmd.action) {
        case 'click': {
          // F1 铁律 2 收口：写操作主通道=CDP trusted（probe 定位坐标→贝塞尔轨迹点击）；
          // CDP 不可用（调试器被占/扩展受限）才降级 DOM 合成兜底——兜底结果标注 channel。
          // 批14：probe 必须在 try 外面。原实现把 probe 和 CDP 点击放同一个 try，
          // probe 报「元素不可交互（被浮层遮挡/不可见/零尺寸）」时被 catch 当成
          // 「CDP 不可用」而降级 DOM 兜底——兜底不重查可见性，闸门恰好在它最该
          // 生效的那一刻被自己绕过（浮层还压着，按钮已经被点掉了）。
          const sel = resolveTarget(cmd.target);
          const probe = await executeInTab(tabId, injClick, [sel, 'probe']);
          let navigated = false;
          try {
            await cdpInput.clickAt(tabId, probe.x, probe.y, { jitterRadius: probe.jitter_radius });
            // R25-Q1：点击生效帧后短暂等路由，再纯读 location 对比判定同页导航；
            // 检测注入失败通常=页面正在导航中，按已导航处理。不再主动接管 href。
            await new Promise((r) => setTimeout(r, 300));
            try {
              const nav = await executeInTab(tabId, injNavigatedCheck, [probe.page_url]);
              navigated = !!nav.navigated;
            } catch {
              navigated = true;
            }
            if (navigated) resetBaseline(tabId);
          } catch (e) {
            const msg = String(e?.message || e);
            if (isUnackedClick(msg)) throw e; // 可能已点中：兜底=双发，直接上抛交裁决
            // 走到这里 = probe 已通过、坐标已拿到、CDP 命令本身失败（事件从未下发）：
            // DOM 兜底是这次动作的第一次下发，安全。
            const r = await executeInTab(tabId, injClick, [sel, 'fallback']).catch(() => null);
            if (r?.ok) {
              if (r.navigated) resetBaseline(tabId);
              return { ...r, channel: 'dom_fallback' };
            }
            throw e;
          }
          // 批17(b)：写步的点后身份复核。**必须落在上面那个 try 之外**——复核失败若在 try 内
          // 抛出，会被 catch 当成「CDP 不可用」而走 DOM 兜底再点一次：那是「已经发生的动作」
          // 之上再动一次（双发），正是本批要消灭的形状。DOM 兜底不做复核：兜底点的是元素本身，
          // 复核只会把真动作误判成移动。导航已发生同样跳过：那是跳转，不是元素挪位。
          if (cmd.verify_identity && !navigated) {
            try {
              await executeInTab(tabId, injClickIdentityCheck, [sel, probe.x, probe.y, IDENTITY_RECHECK_TOLERANCE_PX]);
            } catch (e) {
              throw asIdentityVerdict(e);
            }
            return { ok: true, navigated, channel: 'cdp', identity_checked: true };
          }
          return { ok: true, navigated, channel: 'cdp' };
        }
        case 'click_near': {
          const anchorSel = resolveTarget(cmd.anchor);
          const probe = await executeInTab(tabId, injClickNear, [anchorSel, cmd.button_text || '', 'probe']);
          try {
            await cdpInput.clickAt(tabId, probe.x, probe.y, { jitterRadius: probe.jitter_radius });
          } catch (e) {
            const msg = String(e?.message || e);
            if (isUnackedClick(msg)) throw e;
            const r = await executeInTab(tabId, injClickNear, [anchorSel, cmd.button_text || '', 'fallback']).catch(() => null);
            if (r?.ok) return { ...r, channel: 'dom_fallback' };
            throw e;
          }
          // 同 click：复核在 try 之外，落点身份由 probe 回传的 selector 再解析一次
          if (cmd.verify_identity) {
            try {
              await executeInTab(tabId, injClickIdentityCheck, [probe.selector, probe.x, probe.y, IDENTITY_RECHECK_TOLERANCE_PX]);
            } catch (e) {
              throw asIdentityVerdict(e);
            }
            return { ok: true, clicked: probe.clicked, channel: 'cdp', identity_checked: true };
          }
          return { ok: true, clicked: probe.clicked, channel: 'cdp' };
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
          // 平台选择器由服务端 L3 适配器下发（无则缺省小红书——R17 真机实测）。
          // A1 自愈回路：input_selector 允许是 @eN 引用（LLM 重定位产物），此处同 click/type 走 ref 解析。
          // ref 失效必须报 comment_input_not_found（Go 侧 healCommentInput 只认这个名字，
          // 报错名不对=自愈不触发=一次改版把整条写链路打死），故显式给 missToken。
          const inputSel = resolveTarget(cmd.input_selector || '', 'comment_input_not_found');
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
