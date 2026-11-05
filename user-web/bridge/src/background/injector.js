
// URL → content script 文件 的路由
// 顺序：先匹配具体平台域，最后 fallback 到 'common'（仅 ping/selfcheck）
// 调整需同步 manifest.json content_scripts[*].js 与 build.mjs entries
//
// H5 域名白名单须含全部支持平台（漏了某域名 → 注入永远走 unsupported_host 失败路径）。
//   xianyu.com / goofish.com（咸鱼两个域名共用 content-xianyu.js）。
//   集中在 SUPPORTED_HOSTS 常量，供各处运行时校验复用（manifest matches + popup 域名判断）。
export const SUPPORTED_HOSTS = [
  'douyin.com',
  'xiaohongshu.com',
  'tiktok.com',
  'goofish.com',
  'xianyu.com',
  'kuaishou.com',
  'jdm.jd.com',
  'dongdong.jd.com',
  'chat.jd.com',
  'im.jd.com',
  'message.bilibili.com',
  'weibo.com',
];

// isSupportedHost：运行时域名校验。manifest.matches 是入口校验，
// 这里是代码层兜底（programmatic 注入 / popup 自检 / background 路由）。
// 双校验确保即使 manifest 误配也不会注入到非支持站点。
export function isSupportedHost(url) {
  if (!url) return false;
  try {
    const u = new URL(url);
    const host = u.hostname.toLowerCase();
    return SUPPORTED_HOSTS.some((d) => host === d || host.endsWith('.' + d));
  } catch (_) {
    return false;
  }
}

function pickContentScriptFile(url) {
  if (!url) return null;
  try {
    const u = new URL(url);
    const host = u.hostname.toLowerCase();
    if (/(^|\.)douyin\.com$/.test(host)) return 'content-douyin.js';
    if (/(^|\.)xiaohongshu\.com$/.test(host)) return 'content-xhs.js';
    if (/(^|\.)tiktok\.com$/.test(host)) return 'content-tiktok.js';
    if (/(^|\.)goofish\.com$/.test(host)) return 'content-xianyu.js';
    if (/(^|\.)xianyu\.com$/.test(host)) return 'content-xianyu.js';
    if (/(^|\.)kuaishou\.com$/.test(host)) return 'content-kuaishou.js';
    if (/(^|\.)(jdm|dongdong|chat|im)\.jd\.com$/.test(host)) return 'content-jd.js';
    if (/(^|\.)message\.bilibili\.com$/.test(host)) return 'content-bilibili.js';
    if (/(^|\.)weibo\.com$/.test(host)) return 'content-weibo.js';
    return null; 
  } catch (_) {
    return null;
  }
}

// 注入幂等保护：同 (tabId, file) 在 1500ms 内只注入一次
const RECENT_INJECTS = new Map(); 
const INJECT_DEDUP_MS = 1500;

// 测试专用：清空 dedup 状态，避免用例间状态污染。
// 不在生产路径调用，仅供 vitest 用。
export function __resetInjectState() {
  RECENT_INJECTS.clear();
}

function _key(tabId, file) {
  return `${tabId}:${file}`;
}

function _isRecent(tabId, file) {
  const k = _key(tabId, file);
  const t = RECENT_INJECTS.get(k);
  if (!t) return false;
  if (Date.now() - t > INJECT_DEDUP_MS) {
    RECENT_INJECTS.delete(k);
    return false;
  }
  return true;
}

function _markRecent(tabId, file) {
  RECENT_INJECTS.set(_key(tabId, file), Date.now());
  if (RECENT_INJECTS.size > 500) {
    const now = Date.now();
    for (const [k, t] of RECENT_INJECTS.entries()) {
      if (now - t > INJECT_DEDUP_MS) RECENT_INJECTS.delete(k);
    }
  }
}

// ---- chrome API 兜底 ----
function lastError() {
  try {
    if (!chrome.runtime.lastError) return null;
    return chrome.runtime.lastError.message || '(无错误详情)';
  } catch (_) {
    return null;
  }
}

// 检查当前环境是否支持 chrome.scripting
export function scriptingAvailable() {
  return Boolean(
    typeof chrome !== 'undefined' &&
      chrome &&
      chrome.scripting &&
      typeof chrome.scripting.executeScript === 'function'
  );
}

// 主动注入 content script
//
// 参数：
//   tabId: 目标标签页
//   url:   标签页 URL（用于选择文件）
//   opts:  { allFrames?: boolean } 可选
//   cb:    (result) => void  回调
//     result = { ok, file?, reason?, error? }
export function injectContentScript(tabId, url, opts, cb) {
  if (typeof opts === 'function') {
    cb = opts;
    opts = {};
  }
  opts = opts || {};
  cb = cb || (() => {});

  if (!scriptingAvailable()) {
    cb({ ok: false, reason: 'scripting_api_unavailable' });
    return;
  }
  const file = pickContentScriptFile(url);
  if (!file) {
    cb({ ok: false, reason: 'unsupported_host' });
    return;
  }
  if (_isRecent(tabId, file)) {
    cb({ ok: true, file, dedup: true });
    return;
  }
  try {
    chrome.scripting.executeScript(
      {
        target: { tabId, allFrames: !!opts.allFrames },
        files: [file],
      },
      () => {
        const err = lastError();
        if (err) {
          cb({ ok: false, reason: 'execute_failed', error: err, file });
          return;
        }
        _markRecent(tabId, file);
        cb({ ok: true, file });
      }
    );
  } catch (e) {
    cb({ ok: false, reason: 'execute_threw', error: String(e), file });
  }
}

// 给 background 的 webNavigation.onCommitted 用的注入
// 与 popup 用同一份逻辑；自动注入后 content script 自身会建立端口
export function autoInjectOnCommit(details) {
  if (!details || details.frameId !== 0) return;
  if (details.tabId < 0) return;
  if (!details.url) return;
  if (!pickContentScriptFile(details.url)) return;
  injectContentScript(details.tabId, details.url, { allFrames: false }, (r) => {
    if (!r.ok && r.reason !== 'unsupported_host') {
      console.warn('[auto-inject]', r.reason, r.error || '');
    }
  });
}

export { pickContentScriptFile };

