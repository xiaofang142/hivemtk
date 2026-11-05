
import { SECURITY } from './constants.js';

const HTML_ESCAPE_MAP = {
  '&': '&amp;',
  '<': '&lt;',
  '>': '&gt;',
  '"': '&quot;',
  "'": '&#39;',
  '/': '&#x2F;',
  '`': '&#x60;',
  '=': '&#x3D;',
};

export function escapeHTML(s) {
  if (s == null) return '';
  return String(s).replace(/[&<>"'`=/]/g, (ch) => HTML_ESCAPE_MAP[ch] || ch);
}

// 净化用户控制文本：去掉可能的 script/iframe 注入、控制字符、超长截断
// 用于限制单条 AI 回复最大字节数（防止恶意 prompt 注入 + 平台显示限制）
// 文档源：handler.go: maxReplyContentBytes = 4 * 1024；前端 constants.SECURITY.maxReplyContentBytes
//        测试：test/constants.test.js "maxReplyContentBytes 必须与服务端 handler.go ... 严格对齐"
export const MAX_BODY_BYTES = SECURITY.maxReplyContentBytes;

export function sanitizeForDisplay(text, maxBytes = MAX_BODY_BYTES) {
  if (text == null) return '';
  let s = String(text);
  // 有意移除 C0 控制字符（防注入 + 平台显示限制），此处控制字符属安全净化的目标而非误用
  // eslint-disable-next-line no-control-regex
  s = s.replace(/[\u0000-\u0008\u000B\u000C\u000E-\u001F\u007F]/g, '');
  if (s.length > maxBytes) s = s.slice(0, maxBytes);
  return s;
}

// ---------------------------------------------------------------------------
// 非 BMP 字符剥离（借鉴 ChatGPT-On-CS v1.4.5 pinduoduo 驱动 sendReply，2026-11 穷尽调研）：
//   原作写法 content.replace(/[^\u0000-\uFFFF]/g, '') —— 但 JS 正则**无 u 标志时按
//   UTF-16 码元工作**，代理对（emoji 等）的每个码元都落在 \u0000-\uFFFF 内，这条
//   剥离在原作里实际失效（复刻校正：必须加 u 标志按码点匹配才生效）。
//   目的：剥 >U+FFFF 的星形面字符（部分 emoji、生僻 CJK 扩展区），它们在某些平台
//   网页输入框会触发渲染异常或静默丢弃。
//   对方仅在拼多多驱动启用；hivemtk 现有桥接渠道暂无该实证需求，故只导出备用
//   （渠道面板可按需启用），不默认挂入出站链——剥 emoji 是内容损失，须有渠道证据。
// ---------------------------------------------------------------------------
export function stripNonBmpChars(text) {
  if (text == null) return '';
  // u 标志=按码点匹配（原作缺此标志导致失效，此处校正）；\u0000 是范围端点写法，
  // 有意剥星形面字符属平台兼容净化目标而非误伤
  // eslint-disable-next-line no-control-regex
  return String(text).replace(/[^\u0000-\uFFFF]/gu, '');
}

// ---------------------------------------------------------------------------
// 私聊 DM 出站的 Markdown 剥除（与服务端 bridge_outbound_markdown.go 同口径，
// 双保险：服务端入队前剥一次，扩展发送前再剥一次——服务端漏改版本/离线重放
// 旧信封时仍有兜底）。
//
// 防误清洗约定（与服务端 StripMarkdownForDM 一致）：
//   - 只剥成对语法标记与行首标记，内容本体保留；
//   - 链接 [text](url) → text（url），URL 永远保留；
//   - 不动不成对星号（3*4）与下划线（snake_case）；
//   - 表格不处理。
// ---------------------------------------------------------------------------
export function stripMarkdownForDM(text) {
  if (text == null) return '';
  let s = String(text);
  if (!/[*_`~#[\]\n-]/.test(s)) return s;
  s = s.replace(/^```[a-zA-Z0-9]*[ \t]*\r?\n?/gm, '').replace(/```/g, '');
  s = s.replace(/\[([^\]\n]*)\]\(([^)\s]+)\)/g, (_m, text2, url) => {
    const t = String(text2).trim();
    return t === '' || t === url ? url : `${t}（${url}）`;
  });
  s = s.replace(/\*\*([^*\n]+)\*\*/g, '$1');
  s = s.replace(/__([^_\n]+)__/g, '$1');
  s = s.replace(/~~([^~\n]+)~~/g, '$1');
  s = s.replace(/`([^`\n]+)`/g, '$1');
  s = s.replace(/\*([^*\n]+)\*/g, '$1');
  s = s.replace(/^[ \t]{0,3}#{1,6}[ \t]+/gm, '');
  s = s.replace(/^[ \t]{0,3}>[ \t]?/gm, '');
  s = s.replace(/^[ \t]{0,3}-[ \t]+/gm, '· ');
  s = s.replace(/^[ \t]{0,3}\*[ \t]+/gm, '· ');
  return s.replace(/\n+$/, '');
}

// 安全设值到 contenteditable 容器：使用 textContent 而非 innerHTML
//   - 防止 AI 回复中携带 <script> 之类 XSS payload 触发
//   - 平台 IM 容器通常用 contenteditable 渲染，textContent 即可保留换行
export function safeSetContent(node, text) {
  if (!node) return;
  const safe = sanitizeForDisplay(text);
  node.textContent = safe;
  node.dispatchEvent(new Event('input', { bubbles: true }));
}

// 安全设值到 textarea / input 元素
export function safeSetValue(el, text) {
  if (!el) return;
  const safe = sanitizeForDisplay(text);
  const proto =
    el.tagName === 'TEXTAREA'
      ? window.HTMLTextAreaElement.prototype
      : el.tagName === 'INPUT'
        ? window.HTMLInputElement.prototype
        : null;
  if (proto) {
    const setter = Object.getOwnPropertyDescriptor(proto, 'value')?.set;
    if (setter) setter.call(el, safe);
    else el.value = safe;
  } else {
    el.value = safe;
  }
  el.dispatchEvent(new Event('input', { bubbles: true }));
  el.dispatchEvent(new Event('change', { bubbles: true }));
}

