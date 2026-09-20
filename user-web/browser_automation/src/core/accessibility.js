// accessibility.js — snapshot 生成 @e{N} refs
// 快照格式（供 LLM 吃）：
//   button "登录" @e1
//   textbox "搜索" @e2
//   *button "发送" @e3          ← 相对上一快照新出现的元素（browser-use *[index] 语义）
// refs 映射缓存在扩展 SW 内存（Map<ref, selector>）；页面导航即失效。

// ref 分配器（SW 单例内存，按 tab 分桶）
// 批9：旧实现是**全局单桶 + 每次快照清空**，多 tab 编排时两条错路都会发生——
// ① tab B 拍一帧就把 tab A 的 @eN 全清了，A 的后续定位凭空失效；
// ② 计数器每帧归零，A 的 @e3 与 B 的 @e3 同号，后写覆盖前写 → A 的 ref 解析到 B 的元素。
// 分桶后桶键 = String(tabKey)，与调用方传数字还是字符串无关（Go 侧 tab_id 是 JSON number）。
const refBuckets = new Map(); // tabKey -> { counter: number, map: Map<ref, cssPath> }

const bucketKey = (tabKey) => (tabKey === undefined || tabKey === null || tabKey === '' ? 'default' : String(tabKey));

function refBucket(tabKey) {
  const key = bucketKey(tabKey);
  let b = refBuckets.get(key);
  if (!b) {
    b = { counter: 0, map: new Map() };
    refBuckets.set(key, b);
  }
  return b;
}

// resetRefs 清空 ref 桶：无参清全部，带 tabKey 只清该 tab（导航/新快照前调用）。
export function resetRefs(tabKey) {
  if (tabKey === undefined) {
    refBuckets.clear();
    return;
  }
  refBuckets.delete(bucketKey(tabKey));
}

export function getRefSelector(ref, tabKey) {
  const b = refBuckets.get(bucketKey(tabKey));
  return (b && b.map.get(ref)) || null;
}

/**
 * buildSnapshot 遍历可交互 DOM 生成快照文本 + refs。
 * 在页面上下文（executeScript）中执行后把 nodes/paths 传回 SW 组装。
 */
export function collectInteractiveNodes() {
  // 上限刻意声明在函数体内：本函数以 func.toString() 的形态送进页面，
  // 引用模块顶层 const 今天能跑只是因为 esbuild 把它折成了字面量——
  // 一旦它变成可配置值或关掉折叠，页面侧就是 ReferenceError + result:null，
  // 上层读成 inject_no_result，快照静默变空（批14 静态闸门把这类都拦下）。
  const MAX_NODES = 400;
  const cssEscape = (s) => (typeof CSS !== 'undefined' && CSS.escape ? CSS.escape(s) : String(s).replace(/([^\w-])/g, '\\$1'));
  const selectorOf = (el) => {
    if (el.id) return `#${cssEscape(el.id)}`;
    const parts = [];
    let cur = el;
    let depth = 0;
    while (cur && cur !== document.body && depth < 6) {
      let seg = cur.tagName.toLowerCase();
      if (cur.id) {
        seg = `#${cssEscape(cur.id)}`;
        parts.unshift(seg);
        break;
      }
      const parent = cur.parentElement;
      if (parent) {
        const same = Array.from(parent.children).filter((c) => c.tagName === cur.tagName);
        if (same.length > 1) seg += `:nth-of-type(${same.indexOf(cur) + 1})`;
      }
      parts.unshift(seg);
      cur = parent;
      depth++;
    }
    return parts.join('>');
  };

  const roleOf = (el) => {
    const tag = el.tagName.toLowerCase();
    if (tag === 'a' && el.href) return 'link';
    if (tag === 'button' || (tag === 'input' && ['button', 'submit'].includes(el.type))) return 'button';
    if (tag === 'input' || tag === 'textarea') return 'textbox';
    if (tag === 'select') return 'combobox';
    if (el.getAttribute('role')) return el.getAttribute('role');
    return tag;
  };

  const nameOf = (el) => {
    const aria = el.getAttribute('aria-label');
    if (aria) return aria;
    const text = (el.innerText ?? el.textContent ?? el.value ?? '').trim().replace(/\s+/g, ' ');
    if (text) return text;
    if (el.placeholder) return el.placeholder;
    return el.id || '';
  };

  const selector = [
    'a[href]', 'button', 'input', 'textarea', 'select',
    '[contenteditable="true"], [contenteditable=""]',
    '[role="button"]', '[role="link"]', '[role="tab"]', '[onclick]',
  ].join(',');

  // 静态文本节点（h1-h6/p/li/label/th/td）：不可交互，但 LLM 判断目标是否达成依赖这些内容
  const textSelector = 'h1,h2,h3,h4,h5,h6,p,li,label,th,td';

  const nodes = [];
  const paths = [];
  const pushNode = (el, role) => {
    if (nodes.length >= MAX_NODES) return;
    const style = window.getComputedStyle(el);
    if (style.display === 'none' || style.visibility === 'hidden') return;
    // jsdom 的 computed style 不级联：逐级上溯检查祖先链内联 display:none
    let anc = el.parentElement;
    let hidden = false;
    while (anc && anc !== document.documentElement) {
      if (anc.style && anc.style.display === 'none') { hidden = true; break; }
      anc = anc.parentElement;
    }
    if (hidden) return;
    nodes.push({ role: role || roleOf(el), name: nameOf(el) });
    paths.push(selectorOf(el));
  };

  document.querySelectorAll(selector).forEach((el) => pushNode(el));
  // 文本节点去重：父元素已是交互节点（如 <a><p>…</p></a>）时 innerText 会被 nameOf 吸收，
  // 这里只补页面正文语义；已登记的元素跳过
  const seen = new Set(nodes.map((n) => n.name));
  document.querySelectorAll(textSelector).forEach((el) => {
    const text = (el.innerText ?? el.textContent ?? '').trim().replace(/\s+/g, ' ');
    if (!text || seen.has(text)) return;
    seen.add(text);
    pushNode(el, 'text');
  });
  // A2（批2）：快照携带页面 URL——拦截判据的 URL 层依赖（旧实现快照不含 URL，
  // website-login/error 等重定向判据永不命中）
  return { nodes, paths, url: location.href };
}

/**
 * assembleSnapshot SW 侧组装快照文本并登记 refs。
 * F6 新元素标记（browser-use *[index] 语义）：与上一快照（同 tab）的 role+name 指纹集
 * 做 diff——新出现元素行首加 `*`。首帧/导航重建基线前不打标（防"全是新元素"噪声）。
 * @param {{nodes:Array,paths:Array}} collected
 * @param {number|string} [tabKey] 基线归属 tab（缺省 'default'，单 tab 场景）
 * @returns {{ text: string, count: number, new_count: number, url: string }}
 */
const baselines = new Map(); // tabKey -> Set<role + '|' + name>

export function assembleSnapshot({ nodes, paths, url }, tabKey = 'default') {
  const b = refBucket(tabKey);
  b.map.clear();
  b.counter = 0;
  const key = bucketKey(tabKey);
  const prev = baselines.get(key) || null;
  const now = new Set();
  const lines = [];
  let newCount = 0;
  nodes.forEach((n, i) => {
    b.counter += 1;
    const ref = `@e${b.counter}`;
    b.map.set(ref, paths[i]);
    const k = `${n.role}|${n.name}`;
    const isNew = prev && !prev.has(k) && !now.has(k);
    if (isNew) newCount += 1;
    now.add(k);
    lines.push(`${isNew ? '*' : ''}${n.role} "${n.name}" ${ref}`);
  });
  baselines.set(key, now);
  return { text: lines.join('\n'), count: lines.length, new_count: newCount, url: url || '' };
}

// resetSnapshotBaseline 清某 tab（或全部）的对比基线——页面导航/open_tab 后调用，
// 使下一帧重新建立基线而不把整页标成新元素。
// 批9：refs 与基线同生命周期（导航后 cssPath 必失效），一并清掉，避免旧 @eN 解析到
// 新页面的同序元素——那是「定位看似成功、其实点错」的温床。
export function resetSnapshotBaseline(tabKey) {
  if (tabKey === undefined) {
    baselines.clear();
    resetRefs();
    return;
  }
  baselines.delete(bucketKey(tabKey));
  resetRefs(tabKey);
}
