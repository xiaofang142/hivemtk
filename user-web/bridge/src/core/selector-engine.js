
const _log = (() => {
  try {
    const { createLogger } = require('./logger.js');
    return createLogger('selector', 'bridge');
  } catch (_) {
    return { info: () => {}, warn: () => {}, debug: () => {}, error: () => {} };
  }
})();

// 结构启发式：判断一个元素是否「看起来像一条消息气泡」
// 命中条件：有可见文本 或 内含 <img>/<video>/音频/链接 等媒体；且不是纯容器骨架。
function looksLikeMessageBubble(el) {
  if (!el || el.nodeType !== 1) return false;
  const hasMedia = !!el.querySelector('img, video, audio, [class*="image"], [class*="Image"], [class*="picture"], [class*="Picture"], a[href*="http"]');
  const text = (el.textContent || '').trim();
  if (hasMedia) return true;
  return text.length > 0;
}

// 在 root 下用候选选择器并集收集候选节点（去重）
export function collectCandidates(root, selectors) {
  const out = [];
  const seen = new Set();
  for (const sel of selectors) {
    let nodes;
    try {
      nodes = root.querySelectorAll(sel);
    } catch (_) {
      continue; 
    }
    nodes.forEach((n) => {
      if (seen.has(n)) return;
      seen.add(n);
      out.push(n);
    });
  }
  return out;
}

// 评分：给定一组候选消息项，找「消息列表容器」。
// 启发式：最优容器应满足
//   - 包含最多「像消息气泡」的直接或间接子节点
//   - 自身不是单条气泡（否则会是叶子）
//   - 在视口内可见
// 返回评分最高的容器元素；若找不到则返回 root。
export function scoreMessageListContainer(root, itemCandidates) {
  if (!root) return null;
  const bubbles = itemCandidates.filter(looksLikeMessageBubble);
  if (bubbles.length === 0) return root;

  // 候选容器 = 所有气泡的祖先（最多上溯 6 层）+ root 自身
  const containers = new Map();
  const bump = (c, score) => {
    if (!c) return;
    containers.set(c, (containers.get(c) || 0) + score);
  };
  for (const b of bubbles) {
    let cur = b.parentElement;
    let depth = 0;
    while (cur && cur !== root && depth < 6) {
      bump(cur, 1);
      cur = cur.parentElement;
      depth++;
    }
  }
  bump(root, 0.5);

  let best = null;
  let bestScore = -1;
  for (const [c, score] of containers) {
    // 容器内部气泡数量越多越优；同时惩罚「本身就是单条气泡」的容器
    const inner = c.querySelectorAll('*').length;
    const adjusted = score + (looksLikeMessageBubble(c) ? -5 : 0) + Math.min(inner / 200, 2);
    if (adjusted > bestScore) {
      bestScore = adjusted;
      best = c;
    }
  }
  return best || root;
}

// 线程容器识别：单条气泡再长也有限，整段线程会随着聊天不断变长。超过这条线就当作
// 「可能是容器」，向下拆分而不是当成一条消息上报——否则整段线程会被反复当新消息抓回。
export const BUBBLE_SPLIT_MIN_RUNES = 1200;
// 拆不动又超过这个长度的项不是可信气泡（正常私信远达不到），直接丢弃。
export const BUBBLE_HARD_MAX_RUNES = 4000;

export function bubbleTextLength(el) {
  const text = (el && el.textContent) || '';
  return Array.from(text).length;
}

export function isOversizedBubble(el, hardMax = BUBBLE_HARD_MAX_RUNES) {
  return bubbleTextLength(el) > hardMax;
}

function splitBubbleContainer(el, max, depth) {
  if (!el || depth > 8) return [el];
  if (bubbleTextLength(el) <= max) return [el];
  const kids = Array.from(el.children || []).filter((k) => bubbleTextLength(k) > 0);
  if (kids.length === 0) return [el];
  // 唯一子节点说明这是一条消息内部的包装层，继续向下找正文根节点，不会产生碎片。
  if (kids.length === 1) return splitBubbleContainer(kids[0], max, depth + 1);
  return kids.flatMap((k) => splitBubbleContainer(k, max, depth + 1));
}

// 把「命中选择器但其实是线程容器」的候选拆成多条气泡；拆不出多个子项时保留原元素
// （单条长消息不允许被拆成碎片）。
export function expandMultiBubbleItems(items, { maxRunes = BUBBLE_SPLIT_MIN_RUNES } = {}) {
  const out = [];
  for (const el of items || []) {
    const parts = splitBubbleContainer(el, maxRunes, 0);
    if (parts.length > 1) out.push(...parts);
    else out.push(el);
  }
  return out;
}

// 主入口（同步）：定位消息项列表
//   root         : 搜索根（通常 document）
//   itemSelectors: 渠道特定的消息项候选选择器
//   listSelectors: 可选，渠道特定的「消息列表容器」候选选择器（优先尝试）
// 返回 { container, items } —— items 为去重后的消息项元素数组（按 DOM 顺序）
// 规则全失效时自动回退到结构启发式（scoreMessageListContainer）。
export function locateMessages({ root, itemSelectors, listSelectors }) {
  if (!root) return { container: null, items: [] };
  let container = null;
  if (listSelectors && listSelectors.length) {
    for (const sel of listSelectors) {
      try {
        const el = root.querySelector(sel);
        if (el && el.querySelectorAll('*').length > 3) {
          container = el;
          break;
        }
      } catch (_) {  }
    }
  }
  const candidates = collectCandidates(root, itemSelectors);
  if (!container) {
    container = scoreMessageListContainer(root, candidates);
  }
  // 容器内再补一轮候选，避免容器选择偏差漏掉
  let items = candidates;
  if (container && container !== root) {
    const inside = collectCandidates(container, itemSelectors);
    const mergeSeen = new Set(items);
    for (const n of inside) {
      if (!mergeSeen.has(n)) {
        mergeSeen.add(n);
        items.push(n);
      }
    }
  }
  items = items.filter((el) => {
    // 若某候选内部还包含其它候选（它是容器而非单条），剔除，避免重复计数
    const containsAnother = items.some((other) => other !== el && el.contains(other));
    if (containsAnother) return false;
    return looksLikeMessageBubble(el);
  });
  items = expandMultiBubbleItems(items).filter((el) => !isOversizedBubble(el));
  return { container, items };
}

// 通用输入框定位（多渠道同构）：contenteditable / textarea / [role=textbox]
export function locateInput(root) {
  if (!root) return null;
  const sels = [
    'div[contenteditable="true"]',
    'textarea',
    '[role="textbox"]',
    '[contenteditable=""]',
    '[contenteditable]',
  ];
  for (const sel of sels) {
    const list = root.querySelectorAll(sel);
    for (const el of list) {
      const rect = el.getBoundingClientRect();
      // 真实浏览器有尺寸；jsdom/headless 下 getBoundingClientRect 返回 0，
      // 此时退化为「元素已挂载(非 display:none)」判定，避免漏选输入框。
      const hasSize = rect && (rect.width > 0 || rect.height > 0);
      const mounted = el.offsetParent !== null || el.getClientRects?.().length > 0;
      const visible = hasSize || mounted;
      const editable = el.getAttribute('contenteditable') !== 'false';
      if ((visible || !hasSize) && editable) return el;
    }
  }
  return null;
}

export const SelectorEngine = {
  collectCandidates,
  scoreMessageListContainer,
  locateMessages,
  locateInput,
  looksLikeMessageBubble,
  expandMultiBubbleItems,
  isOversizedBubble,
};
