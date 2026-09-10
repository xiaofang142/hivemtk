// accessibility.js — snapshot 生成 @e{N} refs
// 快照格式（供 LLM 吃）：
//   button "登录" @e1
//   textbox "搜索" @e2
// refs 映射缓存在扩展 SW 内存（Map<ref, selector>）；页面导航即失效。

const MAX_NODES = 400;

// ref 分配器（SW 单例内存）
const refMap = new Map(); // ref -> cssPath
let refCounter = 0;

export function resetRefs() {
  refMap.clear();
  refCounter = 0;
}

export function getRefSelector(ref) {
  return refMap.get(ref) || null;
}

/**
 * buildSnapshot 遍历可交互 DOM 生成快照文本 + refs。
 * 在页面上下文（executeScript）中执行后把 nodes/paths 传回 SW 组装。
 */
export function collectInteractiveNodes() {
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
  return { nodes, paths };
}

/**
 * assembleSnapshot SW 侧组装快照文本并登记 refs
 * @returns {{ text: string, count: number }}
 */
export function assembleSnapshot({ nodes, paths }) {
  resetRefs();
  const lines = [];
  nodes.forEach((n, i) => {
    refCounter += 1;
    const ref = `@e${refCounter}`;
    refMap.set(ref, paths[i]);
    lines.push(`${n.role} "${n.name}" ${ref}`);
  });
  return { text: lines.join('\n'), count: lines.length };
}
