// ── M-3 下行媒体原生发送（2026-11-11）──────────────────────────────────────
//
// 背景：下行协议已带 msg_type/media_url（服务端出站三口径齐备），adapter 层预留了
// hooks.sendMedia 分派口（channel-adapter.js：限速通过后 → 原生发送 → 失败降级
// 「[占位] URL」文本），但渠道侧此前零实现 —— 客户收到的是 URL 字符串不是图片。
//
// 本模块是各渠道 sendMedia hook 的共享实现面：
//   1) 下载：mediaUrl → File。content script 直取（同源/带 CORS 的平台 CDN）
//      失败（CORS/防盗链）→ chrome.runtime 走 background `bridge:fetchMedia`
//      代理（host_permissions 免 CORS）。字节只在扩展内存里搬运（base64 过消息
//      通道），不落盘、不外发任何第三方。
//   2) 注入：会话面板内 input[type=file]（工具栏「图片/文件」按钮的隐藏输入）
//      优先 —— DataTransfer 塞 files + change 事件，走平台自己的上传管线；
//      兜底合成 paste（DataTransfer 携带 File）。合成事件 isTrusted=false /
//      框架不响应，由 DOM 校验兜住，不做盲发。
//   3) 校验：注入后轮询编辑器媒体节点数（img/video/attachment 类），较注入前
//      基线增长才判「媒体已落地」——没落地不点发送，宁可降级文本。
//   4) 发送：可选文案走既有拟人键入（不清空，保留已注入媒体）→ 批25 会话归属
//      复核（assertConv）→ 渠道既有发送按钮 / 回车路径。
//
// 失败语义（与 adapter 降级约定对齐，不丢行）：
//   - 任一步失败：hook 抛错 → adapter 记 via=text-fallback:* → 文本投递，
//     行 ack 语义不变（发出去才 ack，不丢行）。
//   - 失败清场：catch 里清空编辑器（防文本降级把残留媒体一起点出去）。
//   - 超时弃用：adapter 对 sendMedia 有整体超时（withTimeout）；被放弃的在途
//     hook 通过 opts.abortToken 在每个不可逆动作（点发送）前自检，命中即退出
//     —— 杜绝「降级文本已发出，被弃的 hook 迟到又点一次发送」的双发。
//
// 风控：本模块不另设通道。adapter sendOutbound 先过 rateLimiter.tryAcquire
// （拟人最小间隔/账号桶/会话桶/冷却/同文案去重）+ 全局最小间隔闸，才分派
// sendMedia；成功后 markSent 记账 —— sendMedia 与纯文本共用同一套风控。
//
// 平台 CSP/焦点注意：DataTransfer/合成事件均为 DOM 级注入，不 eval、不碰
// 页面 JS 世界，CSP 不构成障碍；粘贴/注入前 focus 编辑器，焦点丢失由 DOM
// 校验兜底（未落地即降级，不盲点发送）。

import { clearMessageInput, fillContentEditableHumanized, assertConvOrAbort, enhancedClick } from './dom.js';
import { humanDelay } from './humanize.js';

// 媒体大小上限（base64 后 ~34MB，chrome.runtime 消息通道 64MB 上限内）
export const MAX_MEDIA_BYTES = 25 * 1024 * 1024;
// 下行整体预算 20s（BRIDGE_THREE_CHANNEL.sendOutboundTimeoutMs）：下载 ≤8s + 注入校验 ≤4s
// + 拟人点按间隔，长文案键入可能超预算 —— 超了走 abortToken 弃用路径，不双发。
export const MEDIA_FETCH_TIMEOUT_MS = 8000;
export const MEDIA_VERIFY_TIMEOUT_MS = 4000;
export const MEDIA_VERIFY_POLL_MS = 250;

const MIME_BY_EXT = {
  png: 'image/png',
  jpg: 'image/jpeg',
  jpeg: 'image/jpeg',
  gif: 'image/gif',
  webp: 'image/webp',
  bmp: 'image/bmp',
  svg: 'image/svg+xml',
  heic: 'image/heic',
  mp4: 'video/mp4',
  webm: 'video/webm',
  mov: 'video/quicktime',
  mp3: 'audio/mpeg',
  m4a: 'audio/mp4',
  wav: 'audio/wav',
  pdf: 'application/pdf',
};

const defaultSleep = (ms) => new Promise((r) => setTimeout(r, ms));

// URL 扩展名 → MIME（Content-Type 缺失时的兜底推断）
export function mediaMimeFromUrl(url) {
  try {
    const p = new URL(url).pathname.toLowerCase();
    const m = p.match(/\.([a-z0-9]+)$/);
    return (m && MIME_BY_EXT[m[1]]) || '';
  } catch (_) {
    return '';
  }
}

// 从 URL 派生安全的文件名（平台 CDN 常带 query 签名/超长路径段，须截断清洗）
export function mediaFileName(url, mime = '') {
  let base = '';
  try {
    base = decodeURIComponent(new URL(url).pathname.split('/').pop() || '');
  } catch (_) { /* URL 非法时走兜底名 */ }
  base = String(base || '').replace(/[\\/:*?"<>|\s]+/g, '_').slice(0, 80);
  let ext = '';
  const extFromUrl = base.match(/\.([a-z0-9]{1,8})$/i);
  if (extFromUrl) {
    ext = extFromUrl[1].toLowerCase();
  } else if (mime) {
    const sub = String(mime).split(';')[0].split('/')[1] || '';
    ext = sub.replace('quicktime', 'mov').replace('svg+xml', 'svg');
  }
  const stem = base.replace(/\.[a-z0-9]{1,8}$/i, '') || `media-${Date.now()}`;
  return `${stem || 'media'}${ext ? `.${ext}` : ''}`;
}

function base64ToBytes(b64) {
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

// 字节 → File（名字/类型供平台上传管线识别）
export function mediaBytesToFile(bytes, mime = '', url = '') {
  const view = bytes instanceof Uint8Array ? bytes : new Uint8Array(bytes);
  const mimeClean = String(mime || '').split(';')[0].trim() || mediaMimeFromUrl(url) || 'application/octet-stream';
  const name = mediaFileName(url, mimeClean);
  const copy = new Uint8Array(view.byteLength);
  copy.set(view);
  return new File([copy.buffer], name, { type: mimeClean });
}

// content script 直取：同源或 CORS 放行的 CDN 命中；no-cors 的 opaque 响应读不到体，
// 不尝试（读不到字节就等于失败，交给代理）。
async function directFetch(fetchFn, url, maxBytes, timeoutMs) {
  const ctrl = typeof AbortController === 'function' ? new AbortController() : null;
  const timer = ctrl ? setTimeout(() => ctrl.abort(), timeoutMs) : null;
  try {
    const resp = await fetchFn(url, ctrl ? { signal: ctrl.signal } : undefined);
    if (!resp || !resp.ok) throw new Error(`HTTP ${(resp && resp.status) || 'no-response'}`);
    const lenHeader = resp.headers && typeof resp.headers.get === 'function'
      ? Number(resp.headers.get('content-length') || 0)
      : 0;
    if (lenHeader > maxBytes) throw new Error(`too_large(content-length ${lenHeader})`);
    const buf = await resp.arrayBuffer();
    if (buf.byteLength > maxBytes) throw new Error(`too_large(${buf.byteLength} > ${maxBytes})`);
    const mime = resp.headers && typeof resp.headers.get === 'function'
      ? (resp.headers.get('content-type') || '')
      : '';
    return { bytes: new Uint8Array(buf), mime };
  } finally {
    if (timer) clearTimeout(timer);
  }
}

// background 代理消息（callback / promise 双形态兼容；超时自兜）
function sendRuntimeMessage(runtime, payload, timeoutMs) {
  return new Promise((resolve, reject) => {
    let settled = false;
    const finish = (fn, arg) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      fn(arg);
    };
    const timer = setTimeout(() => finish(reject, new Error('bridge:fetchMedia 响应超时')), timeoutMs);
    try {
      const maybe = runtime.sendMessage(payload, (res) => {
        let err = null;
        try {
          err = (typeof chrome !== 'undefined' && chrome.runtime && chrome.runtime.lastError)
            ? chrome.runtime.lastError.message
            : null;
        } catch (_) { /* 测试环境无 chrome 全局 */ }
        if (err) finish(reject, new Error(err));
        else finish(resolve, res);
      });
      if (maybe && typeof maybe.then === 'function') {
        maybe.then((res) => finish(resolve, res), (e) => finish(reject, e));
      }
    } catch (e) {
      finish(reject, e);
    }
  });
}

// mediaUrl → { file, bytes, mime, source }。
// 两级兜底：content 直取 → background 代理。都失败抛错（调用方降级文本）。
export async function fetchMediaFile(mediaUrl, {
  maxBytes = MAX_MEDIA_BYTES,
  timeoutMs = MEDIA_FETCH_TIMEOUT_MS,
  fetchFn,
  runtime,
} = {}) {
  if (!mediaUrl || !/^https?:\/\//i.test(mediaUrl)) {
    throw new Error(`media url 非法: ${String(mediaUrl || '').slice(0, 120)}`);
  }
  const cap = Math.min(Math.max(1, maxBytes), MAX_MEDIA_BYTES);
  const errors = [];
  const f = fetchFn || (typeof fetch === 'function' ? fetch : null);
  if (f) {
    try {
      const { bytes, mime } = await directFetch(f, mediaUrl, cap, timeoutMs);
      return { file: mediaBytesToFile(bytes, mime, mediaUrl), bytes: bytes.byteLength, mime, source: 'direct' };
    } catch (e) {
      errors.push(`direct: ${(e && e.message) || e}`);
    }
  }
  const rt = runtime || (typeof chrome !== 'undefined' && chrome.runtime ? chrome.runtime : null);
  if (rt && typeof rt.sendMessage === 'function') {
    try {
      const res = await sendRuntimeMessage(rt, { type: 'bridge:fetchMedia', url: mediaUrl, maxBytes: cap }, timeoutMs);
      if (res && res.ok && res.base64) {
        const bytes = base64ToBytes(res.base64);
        if (bytes.byteLength > cap) throw new Error(`too_large(${bytes.byteLength} > ${cap})`);
        const mime = res.mime || mediaMimeFromUrl(mediaUrl);
        return { file: mediaBytesToFile(bytes, mime, mediaUrl), bytes: bytes.byteLength, mime, source: 'proxy' };
      }
      errors.push(`proxy: ${(res && res.error) || 'no_response'}`);
    } catch (e) {
      errors.push(`proxy: ${(e && e.message) || e}`);
    }
  }
  throw new Error(`媒体下载失败(${errors.join(' | ') || '无可用下载通道'})`);
}

// accept 属性是否放行该 mime/文件名。accept 为空/缺失一律不放行：
// 裸 file input（无 accept）常是头像上传等无关域，误注入会传错内容，宁缺勿滥；
// 该类平台改走合成 paste 兜底。
export function acceptAllows(accept, mime, fileName = '') {
  if (accept == null) return false;
  const tokens = String(accept).split(',').map((t) => t.trim().toLowerCase()).filter(Boolean);
  if (!tokens.length) return false;
  const m = String(mime || '').toLowerCase();
  const name = String(fileName || '').toLowerCase();
  for (const t of tokens) {
    if (t === '*/*') return true;
    if (t.startsWith('.') && name.endsWith(t)) return true;
    if (t.endsWith('/*') && m.startsWith(t.slice(0, -1))) return true;
    if (m && t === m) return true;
  }
  return false;
}

// 会话面板内的 input[type=file]（工具栏「图片/文件」按钮的隐藏输入）。
// 从编辑器向上最多 8 层找近邻命中；doc 级兜底同样只认「显式 accept 且允许该类型」
// 的输入（无 accept 的裸 input 一律不碰）。
export function findUploadInput(editor, mime, fileName = '', { doc = document } = {}) {
  let root = editor;
  for (let up = 0; root && root.querySelectorAll && up < 8; up++) {
    let inputs = [];
    try {
      inputs = Array.from(root.querySelectorAll('input[type="file"]'));
    } catch (_) { /* DOM 异常时跳过该层 */ }
    const hit = inputs.find((inp) => acceptAllows(
      inp.getAttribute ? inp.getAttribute('accept') : null,
      mime,
      fileName
    ));
    if (hit) return hit;
    root = root.parentElement;
  }
  try {
    const inputs = Array.from(doc.querySelectorAll('input[type="file"]'));
    return inputs.find((inp) => acceptAllows(
      inp.getAttribute ? inp.getAttribute('accept') : null,
      mime,
      fileName
    )) || null;
  } catch (_) {
    return null;
  }
}

// 文件输入注入：DataTransfer 塞 files + input/change。
// （Chrome 允许 input.files = DataTransfer.files 赋值；不支持的环境抛错 → 返回 false → paste 兜底）
export function injectViaFileInput(input, file, { DataTransferCtor } = {}) {
  try {
    const dtc = DataTransferCtor || (typeof DataTransfer === 'function' ? DataTransfer : null);
    if (!dtc) return false;
    const dt = new dtc();
    dt.items.add(file);
    input.files = dt.files;
    input.dispatchEvent(new Event('input', { bubbles: true }));
    input.dispatchEvent(new Event('change', { bubbles: true }));
    return true;
  } catch (_) {
    return false;
  }
}

// 合成 paste 注入：DataTransfer 携带 File，Event + defineProperty 挂 clipboardData
// （比 ClipboardEvent 构造器兼容面更宽；defineProperty 失败再退 ClipboardEvent）。
// 编辑器不响应合成 paste（isTrusted=false / 框架不处理）由上层 DOM 校验兜住。
export function injectViaPaste(editor, file, { DataTransferCtor } = {}) {
  try {
    const dtc = DataTransferCtor || (typeof DataTransfer === 'function' ? DataTransfer : null);
    if (!dtc) return false;
    const dt = new dtc();
    dt.items.add(file);
    if (typeof editor.focus === 'function') {
      try { editor.focus(); } catch (_) { /* 隐藏/不可聚焦时由校验兜底 */ }
    }
    let evt = null;
    try {
      evt = new Event('paste', { bubbles: true, cancelable: true });
      Object.defineProperty(evt, 'clipboardData', { value: dt, configurable: true });
    } catch (_) {
      evt = null;
    }
    if (!evt && typeof ClipboardEvent === 'function') {
      evt = new ClipboardEvent('paste', { bubbles: true, cancelable: true, clipboardData: dt });
    }
    if (!evt) return false;
    editor.dispatchEvent(evt);
    return true;
  } catch (_) {
    return false;
  }
}

// 编辑器内媒体节点计数（img/video/附件类 class）——作注入前后差分基线
export function countMediaNodes(root) {
  if (!root || !root.querySelectorAll) return 0;
  try {
    return root.querySelectorAll(
      'img, video, [class*="attachment" i], [class*="upload" i], [class*="thumb" i], [class*="preview" i]'
    ).length;
  } catch (_) {
    return 0;
  }
}

// 注入后轮询：媒体节点数较基线增长才判「落地」。超时 false（宁可降级不盲发）。
export async function waitForMediaInEditor(editor, {
  baselineCount = 0,
  timeoutMs = MEDIA_VERIFY_TIMEOUT_MS,
  pollMs = MEDIA_VERIFY_POLL_MS,
  sleepFn = defaultSleep,
} = {}) {
  const deadline = Date.now() + Math.max(0, timeoutMs);
  while (Date.now() < deadline) {
    if (countMediaNodes(editor) > baselineCount) return true;
    await sleepFn(Math.max(1, pollMs));
  }
  return countMediaNodes(editor) > baselineCount;
}

// 注入编排：file-input 优先（走平台自带上传管线）→ paste 兜底。
// 已触发平台上传但校验未落地时不再试 paste（防双注入），直接判失败。
export async function injectMediaToEditor(editor, file, {
  doc = document,
  verifyTimeoutMs = MEDIA_VERIFY_TIMEOUT_MS,
  pollMs = MEDIA_VERIFY_POLL_MS,
  DataTransferCtor,
  sleepFn,
} = {}) {
  const baseline = countMediaNodes(editor);
  const input = findUploadInput(editor, file.type, file.name, { doc });
  if (input && injectViaFileInput(input, file, { DataTransferCtor })) {
    const landed = await waitForMediaInEditor(editor, { baselineCount: baseline, timeoutMs: verifyTimeoutMs, pollMs, sleepFn });
    return landed ? { ok: true, method: 'file-input' } : { ok: false, method: 'file-input' };
  }
  if (injectViaPaste(editor, file, { DataTransferCtor })) {
    const landed = await waitForMediaInEditor(editor, { baselineCount: baseline, timeoutMs: verifyTimeoutMs, pollMs, sleepFn });
    return landed ? { ok: true, method: 'paste' } : { ok: false, method: 'paste' };
  }
  return { ok: false, method: '' };
}

// 文案拟人键入（不清空 —— 保留已注入媒体；与 sendText 的 clearBefore 语义相反）
const defaultTypeCaption = (editor, text) => fillContentEditableHumanized(editor, text);

function abortError() {
  const e = new Error('sendMedia aborted（下发已被降级路径接管，hook 弃用退出）');
  e.code = 'send-media-aborted';
  return e;
}

// 渠道 sendMedia hook 工厂：下载 → 注入 → 校验 →（可选文案）→ 会话复核 → 发送。
// 参数均为渠道本地既有函数（findInput/findSend/getAccount），不重构渠道既有逻辑。
export function makeSendMediaHook({
  channelKey,
  channelLabel,
  findInput,
  findSend,
  getAccount,
  log,
  clickFn = enhancedClick,
  fetchFile = fetchMediaFile,
  injectToEditor = injectMediaToEditor,
  typeCaption = defaultTypeCaption,
  clearEditor = clearMessageInput,
  fetchTimeoutMs = MEDIA_FETCH_TIMEOUT_MS,
  verifyTimeoutMs = MEDIA_VERIFY_TIMEOUT_MS,
} = {}) {
  if (!channelKey || !findInput || !log) {
    throw new Error('makeSendMediaHook 需要 channelKey/findInput/log');
  }
  return async function sendMedia(msgType, mediaUrl, _convId, opts = {}) {
    const abortToken = opts.abortToken || null;
    const aborted = () => !!(abortToken && abortToken.aborted);
    const editor = findInput();
    if (!editor) throw new Error(`${channelLabel} input not found (sendMedia)`);
    try {
      if (aborted()) throw abortError();
      // 注入前清空：防与用户半截草稿 / 上一条残留拼接（media 是本条下发的唯一载荷）
      clearEditor(editor);
      const { file } = await fetchFile(mediaUrl, { timeoutMs: fetchTimeoutMs });
      if (aborted()) throw abortError();
      const res = await injectToEditor(editor, file, {
        doc: editor.ownerDocument || document,
        verifyTimeoutMs,
      });
      if (!res || !res.ok) throw new Error(`媒体注入未落地(method=${(res && res.method) || 'none'})`);
      if (aborted()) throw abortError();
      if (opts.text) await typeCaption(editor, String(opts.text));
      await humanDelay('click', { channel: channelKey, account: getAccount ? getAccount() : '' });
      // 批25 同款：点发送前复核会话归属（漂移 ⇒ 清空输入框并抛 conv-drift，不 ack 留 pending 重投）
      assertConvOrAbort(editor, opts.assertConv);
      // 最后一个不可逆动作（点发送）前的弃用自检：降级路径已接管即退出
      if (aborted()) throw abortError();
      const btn = findSend ? findSend() : null;
      if (btn) {
        clickFn(btn);
        return { ok: true, method: res.method };
      }
      log.warn(`${channelLabel} 未找到发送按钮，改用回车发送媒体`);
      editor.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', code: 'Enter', keyCode: 13, which: 13, bubbles: true, cancelable: true }));
      editor.dispatchEvent(new KeyboardEvent('keyup', { key: 'Enter', code: 'Enter', keyCode: 13, which: 13, bubbles: true, cancelable: true }));
      return { ok: true, method: res.method };
    } catch (e) {
      // 失败清场：防文本降级把输入框残留媒体一起点出去。
      // 已弃用（abort）时不清场 —— 输入框已归降级文本路径所有，抢清场会与 rawSendText 键入竞争；
      // （降级键入自身的 B7 末段校验会修复中途被清掉的半截文案。）
      if (!aborted()) {
        try { clearEditor(editor); } catch (_) { /* 清场失败不影响降级路径 */ }
      }
      throw e;
    }
  };
}
