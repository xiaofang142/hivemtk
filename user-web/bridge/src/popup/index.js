import {
  DEFAULT_USER_SERVER,
  PLATFORM_ENTRY_URLS,
  UI_DEFAULTS,
  PATROL_DEFAULTS,
  CHANNEL_DISPLAY,
} from '../core/constants.js';
import {
  saveSelectors,
  getCustomSelectors,
  SELECTOR_FIELDS,
  SELECTOR_CONFIG_KEY,
} from '../core/selector-ai.js';
import { SEL as DOUYIN_SEL } from '../channels/douyin.js';
import { SEL as XHS_SEL } from '../channels/xhs.js';
import { SEL as XIANYU_SEL } from '../channels/xianyu.js';
import { SEL as TIKTOK_SEL } from '../channels/tiktok.js';
import {
  renderHealthPanel,
  startHealthPanelPolling,
  stopHealthPanelPolling,
  escapeHtml,
} from './health.js';
import { startAlertPolling, stopAlertPolling } from './alert-banner.js';
import {
  isEmergencyStop,
  triggerEmergencyStop,
  resumeBridge,
} from './emergency-stop.js';
import { explainError, formatErrorBanner } from './error-messages.js';

const $ = (id) => document.getElementById(id);

// ---- 默认值（全部来自 constants.js，禁止就地写死） ----
// 文档源：user-server/docs/dev/DEVELOPMENT.md 端口对照表 + user-server/cmd/api/main.go 读 env `PORT`
//         + user-server/cmd/api/main.go listenAddr 兜底 :8204
// 调整流程：见 docs/bridge/DEFAULTS.md §3
const DEFAULT_PORT_HINT = DEFAULT_USER_SERVER.port;
const DEFAULT_PLACEHOLDER = DEFAULT_USER_SERVER.baseUrl;
const HEALTH_PATHS = DEFAULT_USER_SERVER.healthPaths;
// popup「测试连接」单次 fetch 超时（与 constants.UI_DEFAULTS.healthCheckTimeoutMs 同源）
const POPUP_HEALTH_CHECK_TIMEOUT_MS = UI_DEFAULTS.healthCheckTimeoutMs;

// ---- 工具：chrome API 错误兜底 ----
// 返回 lastError.message 字符串（已解包）；调用方直接用即可，不要再 .message。
// 修复历史：早期版本返回整个 lastError 对象，调用方又取 .message，导致 undefined 显示。
function lastError() {
  try {
    if (!chrome.runtime.lastError) return null;
    return chrome.runtime.lastError.message || '(无错误详情)';
  } catch (_) { return null; }
}

// 按错误内容推断可能原因（用于自检失败时的引导文案）
// lastError.message 典型值：
//   - "Could not establish connection. Receiving end does not exist." → content script 未注入
//   - "The message port closed before a response was received."        → receiver 异步丢失
//   - "Message manager disconnected"                                   → MV3 通道关闭
function diagnoseUninjected(errMsg, tabUrl) {
  const onSupportedHost = /douyin\.com|xiaohongshu\.com|tiktok\.com|goofish\.com|xianyu\.com/i.test(tabUrl || '');
  const lines = [];
  if (errMsg && /Could not establish connection|Receiving end does not exist|port closed|disconnected/i.test(errMsg)) {
    if (!onSupportedHost) {
      lines.push('  1. 当前 URL 不在 manifest matches 范围内（仅抖音/小红书/TikTok/闲鱼网页版生效）');
    } else {
      lines.push('  1. 扩展未加载 / 被禁用（chrome://extensions 检查开关）');
      lines.push('  2. content script 尚未执行（请等待页面加载完成，或按 Ctrl+Shift+R 强制刷新一次）');
      lines.push('  3. 此标签页在扩展加载前已打开，需要重新加载页面');
      lines.push('  4. 页面在 iframe 中（content script 默认不注入 iframe）');
    }
  } else {
    lines.push('  1. 扩展未加载 / 被禁用');
    lines.push('  2. content script 尚未执行（请等待页面加载完成）');
    lines.push('  3. 页面已登录但 URL 不在 manifest matches 范围内');
  }
  return lines.join('\n');
}

function showBanner(kind, title, body) {
  const el = $('banner');
  el.className = `banner show ${kind}`;
  el.innerHTML = '';
  if (title) {
    const t = document.createElement('div');
    t.className = 'title';
    t.textContent = title;
    el.appendChild(t);
  }
  if (body) {
    const b = document.createElement('div');
    b.textContent = body;
    el.appendChild(b);
  }
}

function clearBanner() {
  $('banner').className = 'banner';
  $('banner').textContent = '';
}

// ---- 工具：URL 规范化 ----
function normalizeServerUrl(raw) {
  if (!raw) return '';
  let s = String(raw).trim();
  if (!s) return '';
  if (!/^https?:\/\//i.test(s)) s = 'http://' + s;
  s = s.replace(/\/+$/, '');
  return s;
}

// ---- 配置存取 ----
function loadConfig(cb) {
  try {
    chrome.storage.local.get('bridgeConfig', (res) => {
      const cfg = res && res.bridgeConfig;
      cb(cfg || { serverUrl: '', token: '', autoConnect: true });
    });
  } catch (e) {
    showBanner('error', '读取配置失败', String(e));
    cb({ serverUrl: '', token: '', autoConnect: true });
  }
}

function saveConfig(cfg, cb) {
  try {
    chrome.storage.local.set({ bridgeConfig: cfg }, () => {
      const err = lastError();
      if (err) {
        showBanner('error', '保存失败', err);
        cb && cb(false);
        return;
      }
      try {
        chrome.runtime.sendMessage({ type: 'setConfig', config: cfg }, (resp) => {
          const e = lastError();
          if (e) {
            showBanner('warn', '已写入 storage，但 background 未响应', e + '（请检查扩展是否被禁用）');
            cb && cb(false);
            return;
          }
          cb && cb(resp && resp.ok);
        });
      } catch (e) {
        showBanner('warn', '已写入 storage，但 background 通信失败', String(e));
        cb && cb(false);
      }
    });
  } catch (e) {
    showBanner('error', '保存失败', String(e));
    cb && cb(false);
  }
}

// ---- 测试连接：fetch 健康检查端点 ----
// 策略：依次尝试 HEALTH_PATHS 多个候选路径
//   - 2xx：服务端可达；**再读响应体**判降级（user-server 把依赖故障写在 200 的体里，
//     见 readHealthVerdict——只看 HTTP 码会把"数据库挂了"的部署报成完全正常）
//   - 5xx：服务端可达但进程/反代层故障，仍记为"可连 + 已降级"
//   - 404：当前路径不存在，继续试下一个（避免误报）
//   - 其他 4xx：认证/权限问题，立即返回
//   - 网络错误/超时/拒绝：保留 last error，作为 unreachable 返回
//
// 这一路只回答"地址通不通、服务端自身怎么样"，**不含凭证**：能不能收发由
// probeBridgeCredential（打在挂同一道闸门的 capabilities 口上）单独回答，两条判词在
// 点击处理里合成一条 banner。
//
// 共享 AbortController（2026-08-05 审计 P1 修复）：
//   原实现每次 fetch 各建独立 AbortController 仅用于超时；存在两类泄漏：
//     1) 用户连点"测试连接"：旧请求未取消，先发后到的响应会覆盖最新 banner（race condition）
//     2) 用户关闭 popup：未取消的 fetch 继续占用 socket 直至超时（30s），MV3 popup 关闭后
//        promise resolved 也无处展示，纯浪费资源。
//   改为 popup 单例 AbortController：
//     - abortInFlight()：再次点击「测试连接」时调用，立即终止上一轮 in-flight fetch
//     - pagehide/beforeunload 监听器：popup 关闭时 abort，所有未完成 fetch 抛 AbortError
//     - 单次超时仍保留（每条路径单独 setTimeout + 子 controller），保证单个慢路径不拖整体
let _healthAbortCtl = null;
function abortInFlightHealth() {
  if (_healthAbortCtl) {
    try { _healthAbortCtl.abort(); } catch (_) {  }
    _healthAbortCtl = null;
  }
  // 凭证探测口同样是 in-flight fetch：用户连点或关掉 popup 时一起取消，
  // 否则旧一轮的凭证判词会盖掉新一版（与健康口那两条是同一条故障形态）。
  if (_credAbortCtl) {
    try { _credAbortCtl.abort(); } catch (_) {  }
    _credAbortCtl = null;
  }
}
async function testConnection(serverUrl) {
  const base = normalizeServerUrl(serverUrl);
  if (!base) return { ok: false, reason: 'empty' };
  abortInFlightHealth();
  // 本轮共享 controller：所有候选路径 fetch 共用此 signal
  // 任何路径超时只 abort 自己的子 controller；用户重新点击 / popup 关闭时 abort 父 controller
  const parentCtl = new AbortController();
  _healthAbortCtl = parentCtl;
  let lastNetErr = null;
  try {
    for (const p of HEALTH_PATHS) {
      const url = base + p;
      // 子 controller：单路径超时用，不影响其他候选路径
      const childCtl = new AbortController();
      const t = setTimeout(() => childCtl.abort(), POPUP_HEALTH_CHECK_TIMEOUT_MS);
      // 父 controller abort 时也要 abort 子 controller（避免泄漏 + 立即取消）
      const onParentAbort = () => { try { childCtl.abort(); } catch (_) {} };
      parentCtl.signal.addEventListener('abort', onParentAbort, { once: true });
      try {
        const res = await fetch(url, {
          method: 'GET',
          signal: childCtl.signal,
          cache: 'no-store',
        });
        clearTimeout(t);
        if (res.ok) {
          // 2xx 之后还要读体：见 readHealthVerdict 的说明（降级时服务端仍回 HTTP 200）。
          const verdict = await readHealthVerdict(res);
          return {
            ok: true,
            url,
            status: res.status,
            degraded: verdict.degraded,
            degradedByServer: verdict.declaredByServer,
            healthStatus: verdict.overall,
            degradedChecks: verdict.checks,
          };
        }
        if (res.status === 404) continue; 
        if (res.status >= 500 && res.status < 600) return { ok: true, url, status: res.status, degraded: true, degradedByServer: true, degradedChecks: [`HTTP ${res.status}`] };
        if (res.status >= 400 && res.status < 500) return { ok: false, url, status: res.status, reason: 'http_' + res.status };
        return { ok: false, url, status: res.status, reason: 'http_' + res.status };
      } catch (e) {
        clearTimeout(t);
        if (parentCtl.signal.aborted) {
          return { ok: false, url: base, reason: 'aborted' };
        }
        lastNetErr = e && e.message ? e.message : String(e);
      } finally {
        parentCtl.signal.removeEventListener('abort', onParentAbort);
      }
    }
    return { ok: false, url: base, reason: 'unreachable', detail: lastNetErr };
  } finally {
    if (_healthAbortCtl === parentCtl) _healthAbortCtl = null;
  }
}

// 读 /health 响应体里的依赖判词。
//
// 为什么必须读体：`internal/router/health.go` 在 overallOK 为假时走
// `response.ErrorWithBusinessCode(c, 50301, "service degraded", data)`，而那个 helper 用的状态码是
// `http.StatusOK` ⇒ 数据库挂了、或没有可用 LLM provider 时，HTTP 仍是 200，真话只在体里的
// `code:50301` 与 `data.status:"degraded"`。只看 res.ok 会把这种部署显示成「✓ 服务端可达」。
//
// 只认这两个红信号，别的状态值一律不算红：
//   - `inference` 正常时写的是 "up"、`/healthz` 写 "alive"、`/readyz` 写 "ready"、
//     redis 未配置写 "not_configured"（服务端自己的总判在这种情形仍是 ok）；
//     按 `status !== 'ok'` 一刀切会把这些正常态报成降级。
//   - 单格故障看 `checks.<name>.status === 'down'`，服务端只在真失败时才写这个值。
//
// 两类信号必须分开报，因为服务端只把**它认为关键的**依赖计入总判：`health.go` 里
// embedding 仅在 `HEALTH_EMBEDDING_CRITICAL=true` 时才把 `overallOK` 置假 ⇒ 活实测
// 「data.status:"ok" + checks.embedding.status:"down"（8208 连不上）」是能同时出现的。
// 这时扩展不能替服务端宣布"已降级"（那是它没作过的结论），但也不能不提——
// 向量召回挂了会走兜底话术，对桥接用户是看得见的事。所以 `declaredByServer` 单独回传，
// 合成文案那一路据此选标题。
// 体读不出来（不是 JSON、或旧版没有 data 格）就退回"不改变可达结论"，宁可不判。
const HEALTH_DEGRADED_BUSINESS_CODE = 50301;
async function readHealthVerdict(res) {
  const none = { degraded: false, declaredByServer: false, overall: '', checks: [] };
  if (!res || typeof res.json !== 'function') return none;
  let body;
  try { body = await res.json(); } catch (_) { return none; }
  if (!body || typeof body !== 'object') return none;
  const data = body.data;
  if (!data || typeof data !== 'object') return none;
  const checks = [];
  const items = data.checks && typeof data.checks === 'object' ? Object.entries(data.checks) : [];
  for (const [name, v] of items) {
    if (v && v.status === 'down') {
      checks.push(`${name}: ${String(v.error || '服务端未给原因').slice(0, 160)}`);
    }
  }
  const topDegraded = data.status === 'degraded' || body.code === HEALTH_DEGRADED_BUSINESS_CODE;
  if (topDegraded && checks.length === 0) checks.push(`总判 ${data.status || 'degraded'}`);
  return {
    degraded: topDegraded || checks.length > 0,
    declaredByServer: topDegraded,
    overall: String(data.status || ''),
    checks,
  };
}

// 凭证探测：GET /api/bridge/capabilities，带 X-Bridge-Token。
//
// 为什么原本要补这一格：「测试连接」只打无鉴权的健康口，所以**凭证填错也照样报「✓ 服务端可达」**，
// 用户下一步看到的就是"扩展连着、一条客户消息也不上来、一条回复也不下发"。
// 选这个口是因为它是 bridge 五条路由里唯一"只读、无副作用、又与 ingest/outbox/ack/SSE 挂同一道
// `BridgeIngressGuard`"的（`internal/router/router.go` 的 bridge group）——活实测三态互不混淆：
//   不带凭证 ⇒ 401 `缺少 X-Bridge-Token`；带错 ⇒ 401 `bridge token 无效`；带对 ⇒ 200 + 能力读数。
// 反过来说 ingest/outbox/ack 会真写队列，不能拿来"测试"。
// 返回体绝不回显凭证；服务端那句 message 是闸门自己写的固定文案，直接抄给用户比任何猜测准。
const BRIDGE_CREDENTIAL_PROBE_PATH = '/api/bridge/capabilities';
let _credAbortCtl = null;
async function probeBridgeCredential(serverUrl, token) {
  const base = normalizeServerUrl(serverUrl);
  if (!base) return { state: 'invalid_url' };
  const url = base + BRIDGE_CREDENTIAL_PROBE_PATH;
  const creds = String(token || '').trim();
  const ctl = new AbortController();
  _credAbortCtl = ctl;
  const t = setTimeout(() => ctl.abort(), POPUP_HEALTH_CHECK_TIMEOUT_MS);
  const headers = {};
  // 空凭证时不伪造空头：不发与发空串，服务端给的都是同一句 401，
  // 但"你压根没填"只有不发才判得出来（state==='missing'）。
  if (creds) headers['X-Bridge-Token'] = creds;
  try {
    const res = await fetch(url, { method: 'GET', headers, signal: ctl.signal, cache: 'no-store' });
    clearTimeout(t);
    if (res.ok) {
      let caps = null;
      try { caps = await res.json(); } catch (_) { caps = null; }
      return {
        state: 'verified',
        url,
        status: res.status,
        sseEnabled: !!(caps && caps.sse_enabled),
        pollIntervalMs: caps && caps.poll_interval_ms,
      };
    }
    if (res.status === 404) return { state: 'no_probe_endpoint', url, status: 404 };
    if (res.status === 401 || res.status === 403) {
      let msg = '';
      try {
        const b = await res.json();
        msg = (b && (b.message || b.msg)) || '';
      } catch (_) { msg = ''; }
      // 防御：任何情况下都不把用户刚填的凭证回显进判词。
      if (creds && String(msg).includes(creds)) msg = '';
      return { state: creds ? 'rejected' : 'missing', url, status: res.status, serverMessage: String(msg).slice(0, 160) };
    }
    return { state: 'unexpected_status', url, status: res.status };
  } catch (e) {
    clearTimeout(t);
    if (ctl.signal.aborted) return { state: 'timeout', url };
    return { state: 'unreachable', url, detail: e && e.message ? e.message : String(e) };
  } finally {
    if (_credAbortCtl === ctl) _credAbortCtl = null;
  }
}

// 「测试连接」的判词合成：可达性 + 依赖降级 + 凭证，三条读数出**一条** banner。
// 单独抽成纯函数是因为这条 banner 就是用户看到的全部东西——"通不通"必须由它说了算，
// 而不是由 fetch 的返回值说了算；抽出来之后每条用户可见文案都能直接用例钉住。
// 严重度取最差：凭证被拒 ⇒ 红（链路一条也走不通），仅依赖降级 ⇒ 黄。
function buildConnectionVerdict(reach, cred) {
  const r = reach || {};
  if (!r.ok) {
    if (r.reason === 'empty') {
      return { kind: 'error', title: '请输入服务端地址', body: '例如 ' + DEFAULT_PLACEHOLDER };
    }
    if (r.reason && /^http_/.test(r.reason)) {
      return {
        kind: 'warn',
        title: '⚠ 服务端响应 4xx',
        body: `${r.url}  ${r.reason}\n\n检查：\n  1. user-server 是否在运行\n  2. 健康检查路径是否被中间件拦截\n  3. URL 是否正确`,
      };
    }
    const detail = r.detail ? `\n详细: ${r.detail}` : '';
    return {
      kind: 'error',
      title: '✗ 无法连接',
      body: `${r.url}${detail}\n\n可能原因：\n  1. user-server 未启动（默认端口 ${DEFAULT_PORT_HINT}）\n  2. 端口不正确：检查 cmd/api/main.go 或 PORT 环境变量\n  3. 防火墙/CORS 拦截\n  4. URL 写错\n  5. manifest.json host_permissions 未覆盖此域名`,
    };
  }

  const lines = [`${r.url}  HTTP ${r.status}`];
  let kind = 'success';
  let title = '✓ 服务端可达';

  if (r.degraded) {
    kind = 'warn';
    const why = (r.degradedChecks && r.degradedChecks.length) ? r.degradedChecks.join('\n  ') : `HTTP ${r.status}`;
    if (r.degradedByServer) {
      title = '⚠ 服务端可达但已降级';
      lines.push(`服务端健康检查点名的问题：\n  ${why}\n桥接收发本身不受影响，但 AI 回复会变慢或失败。`);
    } else {
      // 服务端总判仍是 ok（它只把关键依赖计入降级，embedding 要 HEALTH_EMBEDDING_CRITICAL=true 才算），
      // 标题就不许替它宣布"已降级"；但这格实测报 down，对桥接用户是可感知的，必须点名。
      title = '⚠ 可达，有一项依赖实测故障';
      lines.push(`服务端总判为「${r.healthStatus || 'ok'}」，未把它计为降级，而这一格报的是 down：\n  ${why}\n`
        + '桥接收发不受影响；向量召回打不中时回复会走兜底话术。');
    }
  }

  const c = cred || {};
  switch (c.state) {
    case 'verified':
      // 标题不许盖掉健康口那一路已经判出的降级：颜色与文案必须同一条口径，
      // 否则「黄底绿字」会让用户以为一切正常，依赖故障这一条就白报了。
      if (kind === 'success') title = '✓ 可达，凭证可用';
      lines.push(c.sseEnabled
        ? '凭证可用；下行形态 SSE 长连接（服务端 capabilities 实报 sse_enabled=true）。'
        : `凭证可用；下行形态轮询（服务端实报 sse_enabled=false${c.pollIntervalMs ? `，间隔 ${c.pollIntervalMs}ms` : ''}）。`);
      break;
    case 'rejected':
      kind = 'error';
      title = '✗ 凭证被服务端拒绝';
      lines.push(`capabilities 返回 ${c.status}${c.serverMessage ? `：${c.serverMessage}` : ''}\n`
        + '上报/拉取/ack/SSE 五条路由挂的是同一道闸门，全都会被拦下 ⇒ 扩展会"连着"却一条也不同步。\n'
        + '请核对这里填的凭证与服务端配置（环境变量 BRIDGE_INGEST_TOKEN，或系统配置项 bridge_ingest_token）是否一致。');
      break;
    case 'missing':
      kind = 'error';
      title = '✗ 未填桥接凭证';
      lines.push(`服务端要求 X-Bridge-Token（capabilities 返回 ${c.status}${c.serverMessage ? `：${c.serverMessage}` : ''}），而凭证栏是空的。\n不填的话每一条桥接请求都会 401。`);
      break;
    case 'no_probe_endpoint':
      if (kind === 'success') { kind = 'warn'; title = '⚠ 可达，但凭证没能校验'; }
      lines.push('该地址没有 capabilities 探测口（HTTP 404）：user-server 版本可能偏旧，凭证要等真正收发时才会被检验。');
      break;
    case 'timeout':
    case 'unreachable':
    case 'unexpected_status':
    case 'invalid_url':
      if (kind === 'success') { kind = 'warn'; title = '⚠ 可达，但凭证没能校验'; }
      lines.push(c.state === 'unexpected_status'
        ? `凭证探测返回了非预期状态（HTTP ${c.status}），这条判词不作数。`
        : '凭证探测请求没通（超时或网络错误），这条判词不作数——健康口通不代表桥接口通。');
      break;
    default:
      break;
  }

  if (kind === 'success') lines.push('请打开 抖音/小红书/TikTok/闲鱼 私信页启动桥接。');
  return { kind, title, body: lines.join('\n\n') };
}

// 渠道展示名：统一只显示「抖音 / 小红书 / TikTok / 闲鱼」，不出现 douyin_web/xhs_web 这类内部编码
// （需求④：只有一个渠道名称、列表渲染/搜索同理）。内部协议码仍用 *_web，仅展示层归一化。
function channelDisplayName(channel) {
  if (!channel) return '';
  return CHANNEL_DISPLAY[channel] || channel;
}

// 把 metaStore key 形如 "douyin_web:acc1" 拆出 channel/account 用于展示
function parseStatusKey(k) {
  const idx = String(k || '').indexOf(':');
  if (idx < 0) return { channel: k, account: '' };
  return { channel: k.slice(0, idx), account: k.slice(idx + 1) };
}

// ---- 连接状态 ----
function refreshStatus() {
  try {
    chrome.runtime.sendMessage({ type: 'getStatus' }, (res) => {
      const err = lastError();
      if (err) {
        $('statusOut').textContent = 'background 无响应：' + err;
        return;
      }
      if (!res) {
        $('statusOut').textContent = '无响应';
        return;
      }
      const lines = Object.entries(res.statuses || {}).map(([k, v]) => {
        const { channel, account } = parseStatusKey(k);
        const disp = channelDisplayName(channel) || channel || '-';
        const tag = v.online ? '🟢 在线' : '🔴 离线';
        return `${tag}  ${disp}${account ? ' / ' + account : ''}\n   会话: ${v.conversationId || '-'}\n   渠道: ${disp}`;
      });
      $('statusOut').textContent = lines.length ? lines.join('\n\n') : '当前无已连接账号';
    });
  } catch (e) {
    $('statusOut').textContent = '查询失败：' + e;
  }
}

// ---- 自检：向当前标签页的 content script 发 ping → selfcheck 两步走 ----
// 协议设计（与 content/common.js 对齐）：
//   1) 先 ping：内容脚本在不匹配页面也会应答，用于"是否注入"探活
//   2) ping 成功后再 selfcheck：未匹配页返回 matched=false + 提示，不报错
// 这样能区分三种情况：
//   A. ping 都失败 → content script 根本未注入（扩展禁用 / URL 不匹配 / 旧标签页）
//   B. ping 成功 + selfcheck 返回 matched=false → 内容脚本在，但当前不是私信/消息页
//   C. ping 成功 + selfcheck 返回完整数据 → 桥接健康
//
// 全自动修复：ping 失败时自动请求 background 执行 programmatic 注入，
// 解决"标签页在扩展加载/更新前已打开"场景（接收端不存在）。
// 用户无需手动刷新页面。
function selfCheck() {
  try {
    chrome.tabs.query({ active: true, currentWindow: true }, (tabs) => {
      const tab = tabs && tabs[0];
      if (!tab) {
        $('selfOut').textContent = '无活动标签页';
        return;
      }
      if (!tab.url) {
        $('selfOut').textContent = '当前标签页 URL 不可读（请打开非 chrome:// 页面）';
        return;
      }
      // 第一道闸：域名白名单
      const onSupportedHost = /douyin\.com|xiaohongshu\.com|tiktok\.com|goofish\.com|xianyu\.com/i.test(tab.url);
      if (!onSupportedHost) {
        $('selfOut').textContent = `当前标签页不是抖音/小红书/TikTok/闲鱼：\n${tab.url}\n\n请在对应平台网页（已登录）上点击。`;
        return;
      }
      pingContentScript(tab, (pingErr) => {
        if (pingErr) {
          $('selfOut').textContent = `未检测到桥接，正在自动注入…`;
          tryAutoInject(tab, (injectResult) => {
            if (!injectResult.ok) {
              $('selfOut').textContent = `该页面未注入桥接：${pingErr.msg}\n\n自动注入失败：${injectResult.reason}${injectResult.error ? '（' + injectResult.error + '）' : ''}\n\n可能原因：\n${pingErr.hints}`;
              return;
            }
            setTimeout(() => {
              pingContentScript(tab, (retryErr) => {
                if (retryErr) {
                  $('selfOut').textContent = `自动注入 ${injectResult.file} 成功但 ping 仍失败：${retryErr.msg}\n\n请手动按 Ctrl+Shift+R 刷新一次。\n（注入后 content script 需页面重新加载才能完全生效）`;
                  return;
                }
                $('selfOut').textContent = `✅ 已自动注入 ${injectResult.file}，桥接已恢复。\n正在拉取详细数据…`;
                runSelfcheck(tab);
              });
            }, 200);
          });
          return;
        }
        runSelfcheck(tab);
      });
    });
  } catch (e) {
    $('selfOut').textContent = '查询标签页失败：' + e;
  }
}

// 请求 background 执行 programmatic 注入
function tryAutoInject(tab, cb) {
  try {
    chrome.runtime.sendMessage(
      { type: 'injectContentScript', tabId: tab.id, url: tab.url, allFrames: false },
      (resp) => {
        const e = lastError();
        if (e) {
          cb({ ok: false, reason: 'background_no_response', error: e });
          return;
        }
        if (!resp) {
          cb({ ok: false, reason: 'empty_response' });
          return;
        }
        cb(resp);
      }
    );
  } catch (e) {
    cb({ ok: false, reason: 'send_threw', error: String(e) });
  }
}

function pingContentScript(tab, cb) {
  try {
    chrome.tabs.sendMessage(tab.id, { type: 'ping' }, (resp) => {
      const err = lastError();
      if (err) {
        cb({ msg: err, hints: diagnoseUninjected(err, tab.url) });
        return;
      }
      if (!resp || !resp.pong) {
        cb({ msg: 'content script 未响应 ping', hints: diagnoseUninjected(null, tab.url) });
        return;
      }
      cb(null);
    });
  } catch (e) {
    cb({ msg: '发送 ping 失败：' + e, hints: diagnoseUninjected(null, tab.url) });
  }
}

function runSelfcheck(tab) {
  try {
    chrome.tabs.sendMessage(tab.id, { type: 'selfcheck' }, (resp) => {
      const err = lastError();
      if (err) {
        $('selfOut').textContent = 'ping 已通过但 selfcheck 失败：' + err + '\n（content script 已注入但响应中断，请刷新页面重试）';
        return;
      }
      if (!resp) {
        $('selfOut').textContent = 'selfcheck 未返回数据';
        return;
      }
      if (resp.matched === false) {
        // matched=false 通常说明 match() 选择器过期 / 用户在首页/feed
        // 自动降级到 deepSelfcheck 拿真实 DOM 快照，便于用户定位问题
        const header = `内容脚本已注入，但 adapter.match() 返回 false。\n\nURL: ${tab.url}\n频道: ${resp.channel}\n提示: ${resp.hint || '请打开目标会话的【私信/聊天】界面'}\n\n正在拉取真实 DOM 快照…`;
        $('selfOut').textContent = header;
        runDeepSelfcheck(tab, header);
        return;
      }
      // 匹配成功：区分 strict / fallback 模式
      // fallback 模式说明平台严格选择器已失效、走通用 DOM 扫描
      // 提示用户桥接功能正常但请把 DOM 快照反馈到 issue 让维护者更新 SEL
      const modeLine = resp.matchMode === 'fallback'
        ? '\n匹配模式: ⚠ fallback（严格选择器已失效，走通用 DOM 扫描）\n请把【深度自检】输出发到 issue，维护者会更新 src/channels/<platform>.js 的 SEL'
        : '\n匹配模式: ✓ strict（严格选择器命中）';
      const sel = resp.selectors ? '\n选择器:\n' + JSON.stringify(resp.selectors, null, 2) : '';
      const sample = (resp.sample || []).map((s) => `  [${s.sender}] ${s.text}`).join('\n');
      $('selfOut').textContent = `频道: ${resp.channel}\n匹配: ${resp.matched}${modeLine}\n账号: ${resp.accountId || '(空)'}\n会话: ${resp.conversationId || '(空)'}\n消息条目: ${resp.msgItemCount}\n样本:\n${sample}${sel}`;
    });
  } catch (e) {
    $('selfOut').textContent = '发送 selfcheck 失败：' + e;
  }
}

// 深度自检：直接向 content script 请求 DOM 快照
// 触发条件：自检返回 matched=false / msgItemCount=0
// 输出：可见的输入框 / 发送按钮 / 列表容器 / 账号链接 / 推荐选择器
// header: 可选，selfcheck 上下文（matched=false 时给出），失败时与 deep 错误合并展示
//
// 重要：content/common.js 的 deepSelfcheck 监听器已改为异步（return true +
// Promise 微任务）以避免 "port closed before response"。但极端情况下
// （页面长时间无响应 / 标签页被冻结 / 浏览器内存压力）port 仍可能在
// 异步 sendResponse 之前关闭。错误处理中针对此场景给出明确引导。
function runDeepSelfcheck(tab, header) {
  try {
    chrome.tabs.sendMessage(tab.id, { type: 'deepSelfcheck' }, (resp) => {
      const err = lastError();
      if (err) {
        const prefix = header ? header + '\n\n---\n\n' : '';
        // 区分"port 在异步 sendResponse 前被关闭"vs"接收端根本不存在"
        // 后者通常意味着 content script 未注入（已在前序 ping 阶段拦截）
        const isPortClosed = /port closed|disconnected/i.test(err);
        const guide = isPortClosed
          ? '\n\n可能原因：\n  1. 当前页 DOM 极重（消息列表很长 / 大量虚拟滚动节点），扫描耗时超长\n  2. 标签页处于后台，Chrome 限流 / 冻结了 content script\n  3. 页面在扫描过程中发生了 SPA 路由切换\n\n建议：\n  · 关闭其他抖音/小红书/TikTok/闲鱼标签，仅保留当前私信页\n  · 滚动到顶部让虚拟列表回收\n  · 按 Ctrl+Shift+R 强制刷新一次后再试'
          : '\n\n可能原因：\n  · content script 未注入（请先点「自检当前私信页」执行 ping 探活）\n  · 扩展被禁用 / URL 不在 manifest matches 范围';
        $('selfOut').textContent = prefix + 'deepSelfcheck 失败：' + err + guide;
        return;
      }
      if (!resp || !resp.ok) {
        const prefix = header ? header + '\n\n---\n\n' : '';
        $('selfOut').textContent = prefix + 'deepSelfcheck 未返回数据：' + (resp && resp.error || '(空)');
        return;
      }
      $('selfOut').textContent = formatDeepSnapshot(resp);
    });
  } catch (e) {
    const prefix = header ? header + '\n\n---\n\n' : '';
    $('selfOut').textContent = prefix + '发送 deepSelfcheck 失败：' + e;
  }
}

// 把 DOM 快照渲染成易读文本
function formatDeepSnapshot(s) {
  const lines = [];
  lines.push('=== 深度自检（DOM 快照）===');
  lines.push(`URL: ${s.url}`);
  lines.push(`域名: ${s.hostname}`);
  lines.push(`标题: ${s.title}`);
  lines.push('');
  lines.push(`【输入框】共 ${s.inputCount} 个可见：`);
  for (const it of s.inputSample || []) {
    lines.push(`  - <${it.tag}>  ${it.selectorHint}`);
    if (it.placeholder) lines.push(`      placeholder="${String(it.placeholder).slice(0, 50)}"`);
    if (it.text) lines.push(`      text="${String(it.text).slice(0, 50)}"`);
  }
  lines.push('');
  lines.push(`【发送按钮】共 ${s.sendBtnCount} 个疑似：`);
  for (const it of s.sendBtnSample || []) {
    lines.push(`  - <${it.tag}>  ${it.selectorHint}`);
  }
  lines.push('');
  lines.push(`【消息列表根】共 ${s.listRootCount} 个疑似：`);
  for (const it of s.listRootSample || []) {
    lines.push(`  - <${it.tag}>  ${it.selectorHint}`);
  }
  lines.push('');
  lines.push(`【账号线索】共 ${(s.accountHints || []).length} 个链接：`);
  for (const a of s.accountHints || []) {
    lines.push(`  - ${a.text || '(无文本)'}  →  ${a.href}`);
  }
  lines.push('');
  if (s.recommendedSelector) {
    lines.push(`【推荐选择器】${s.recommendedSelector}`);
    lines.push('（若上述内容看着像私信页但 adapter.match() 返回 false，');
    lines.push('  说明平台已改版/选择器过期。请把【推荐选择器】发到 issue，');
    lines.push('  维护者会更新 src/channels/<platform>.js 的 SEL.INPUT/EDITOR。');
    lines.push('  临时方案：在 DevTools Console 跑 `window.__bridgeAdapter.SEL.INPUT=...` 覆盖。）');
  } else {
    lines.push('【推荐选择器】未找到 — 当前页可能不是私信/消息页，');
    lines.push('  请打开目标会话的【私信/聊天】界面后再次点击「自检」。');
  }
  return lines.join('\n');
}

// ---- 深度自检入口（用户主动触发） ----
// 跳过 ping/自检/域白名单，直接尝试拉 DOM 快照
// ping 失败时尝试自动注入，注入成功后再拉快照
function deepSelfCheck() {
  try {
    chrome.tabs.query({ active: true, currentWindow: true }, (tabs) => {
      const tab = tabs && tabs[0];
      if (!tab) {
        $('selfOut').textContent = '无活动标签页';
        return;
      }
      if (!tab.url) {
        $('selfOut').textContent = '当前标签页 URL 不可读（请打开非 chrome:// 页面）';
        return;
      }
      pingContentScript(tab, (pingErr) => {
        if (pingErr) {
          $('selfOut').textContent = '未检测到桥接，正在自动注入…';
          tryAutoInject(tab, (injectResult) => {
            if (!injectResult.ok) {
              $('selfOut').textContent = `该页面未注入桥接：${pingErr.msg}\n自动注入失败：${injectResult.reason}${injectResult.error ? '（' + injectResult.error + '）' : ''}\n\n可能原因：\n${pingErr.hints}`;
              return;
            }
            setTimeout(() => {
              pingContentScript(tab, (retryErr) => {
                if (retryErr) {
                  $('selfOut').textContent = `已注入 ${injectResult.file} 但 ping 仍失败：${retryErr.msg}\n请手动 Ctrl+Shift+R 后再试。`;
                  return;
                }
                runDeepSelfcheck(tab);
              });
            }, 200);
          });
          return;
        }
        runDeepSelfcheck(tab);
      });
    });
  } catch (e) {
    $('selfOut').textContent = '深度自检失败：' + e;
  }
}

// 复制 selfOut 文本到剪贴板
function copySelfOut() {
  const txt = $('selfOut').textContent || '';
  if (!txt) {
    showBanner('info', '无内容', 'selfOut 区域为空');
    return;
  }
  if (navigator.clipboard?.writeText) {
    navigator.clipboard.writeText(txt).then(
      () => showBanner('success', '已复制', '已复制到剪贴板，可粘贴到 issue / 文档'),
      () => showBanner('error', '复制失败', '请检查剪贴板权限')
    );
  } else {
    showBanner('error', '复制失败', '当前环境不支持 navigator.clipboard');
  }
}

// ---- 打开私信页 ----
function openUrl(url) {
  try {
    chrome.tabs.create({ url });
  } catch (e) {
    showBanner('error', '打开失败', String(e));
  }
}

// ---- 全量同步私信（需求①③） ----
// 向当前标签页 content script 发送 syncAllConversations：
// 遍历所有私信会话，逐个回填多轮历史（一个会话=一条记录）。仅私信页有效。
function syncAllConversations() {
  try {
    chrome.tabs.query({ active: true, currentWindow: true }, (tabs) => {
      const tab = tabs && tabs[0];
      if (!tab) {
        $('selfOut').textContent = '无活动标签页';
        return;
      }
      const onSupportedHost = /douyin\.com|xiaohongshu\.com|tiktok\.com|goofish\.com|xianyu\.com/i.test(tab.url || '');
      if (!onSupportedHost) {
        $('selfOut').textContent = '当前标签页不是抖音/小红书/TikTok/闲鱼：\n' + (tab.url || '');
        return;
      }
      $('selfOut').textContent = '正在全量同步私信会话…（可能需要数分钟，请勿切换标签页）';
      chrome.tabs.sendMessage(tab.id, { type: 'syncAllConversations' }, (resp) => {
        const err = lastError();
        if (err) {
          $('selfOut').textContent = '触发全量同步失败：' + err + '\n\n可能原因：\n  1. 当前页面未注入桥接（请先点「自检当前私信页」）\n  2. 当前不是私信页（需打开抖音/小红书/闲鱼聊天页）';
          return;
        }
        if (!resp) {
          $('selfOut').textContent = 'content script 未返回结果';
          return;
        }
        if (resp.ok === false) {
          $('selfOut').textContent = '同步失败：' + (resp.error || '未知');
          return;
        }
        const waitHint = resp.skipped
          ? '\n（另一轮同步正在进行，已忽略本次重复触发）'
          : '';
        $('selfOut').textContent = `✅ 全量同步完成${waitHint}\n成功会话: ${resp.synced}\n遍历会话: ${resp.total}\n失败: ${resp.failures}\n\n说明：已同步的会话不会重复回填（持久化续传）；实时新私信仍会即时上行。`;
      });
    });
  } catch (e) {
    $('selfOut').textContent = '触发全量同步失败：' + e;
  }
}

// ---- 选择器配置：抖音/小红书/闲鱼 切换页 分别配置（需求①）----
// 字段 <-> 输入框 id 映射（与 SELECTOR_FIELDS 顺序一致）
const SEL_FIELD_IDS = {
  conversationList: 'selConversationList',
  messageList: 'selMessageList',
  messageItem: 'selMessageItem',
  text: 'selText',
  input: 'selInput',
  send: 'selSend',
};

// 字段 -> 渠道 SEL 属性名映射（用于「当前生效值」展示：用户未配置时，placeholder 直接显示 SEL 默认，
// 非空即有效值/当前使用值，可让用户一眼看到当前到底在用什么选择器，无需翻源码）
const SEL_FIELD_TO_PROP = {
  conversationList: 'CHAT_LIST',
  messageList: 'MSG_LIST',
  messageItem: 'MSG_ITEM',
  text: 'TEXT',
  input: 'INPUT',  
  send: 'SEND',
};

// 各渠道 SEL 默认表（静态导入，避免触发 DOM 探测）
// 2026-08-05 渠道编码统一：key 改为平台全名（与后端 model.Channel* 对齐）。
const CHANNEL_SEL_MAP = {
  douyin: DOUYIN_SEL,
  xiaohongshu: XHS_SEL,
  xianyu: XIANYU_SEL,
  tiktok: TIKTOK_SEL,
};

// 取某渠道某字段的当前生效默认选择器（来自 SEL；抖音 input 字段兼容 EDITOR/INPUT 双命名）
function channelSelDefault(channel, field) {
  const sel = CHANNEL_SEL_MAP[channel];
  if (!sel) return '';
  const prop = SEL_FIELD_TO_PROP[field];
  if (!prop) return '';
  // 抖音输入框为 EDITOR，小红书为 INPUT；优先按 prop 取，缺失时 input 字段兼容 EDITOR
  let v = sel[prop];
  if (!v && field === 'input') v = sel.EDITOR || sel.INPUT;
  return v || '';
}

// 同步读取全部选择器配置（popup localStorage 镜像，与 saveSelectors 写入侧一致）
function readAllSelectors() {
  try {
    const raw = localStorage.getItem(SELECTOR_CONFIG_KEY);
    if (raw) return JSON.parse(raw) || {};
  } catch (_) {  }
  return {};
}

// 加载某渠道选择器配置到表单。
// 需求②：自定义值为空时，placeholder 直接显示当前生效值（SEL 默认，非空即有效值/当前使用值），
//         不再是泛化的「如 .xhs-im-conv-item」示例文案。
function loadSelectorConfig(channel) {
  const all = readAllSelectors();
  const cfg = all[channel] || {};
  for (const field of SELECTOR_FIELDS) {
    const el = $(SEL_FIELD_IDS[field]);
    if (!el) continue;
    el.value = cfg[field] || '';
    // 当前生效值（SEL 默认）作为 placeholder：非空即展示，让用户知道未配置时正在用什么
    const def = channelSelDefault(channel, field);
    if (def) el.placeholder = def;
    else el.placeholder = '';
  }
}

function collectSelectorConfig() {
  const cfg = {};
  for (const field of SELECTOR_FIELDS) {
    const el = $(SEL_FIELD_IDS[field]);
    const v = el ? String(el.value || '').trim() : '';
    if (v) cfg[field] = v;
  }
  return cfg;
}

// 广播选择器更新到所有抖音/小红书/TikTok/闲鱼标签页，触发 content script 重新水合并立即生效
function broadcastSelectorsUpdated() {
  try {
    chrome.tabs.query({}, (tabs) => {
      const list = Array.isArray(tabs) ? tabs : [];
      for (const t of list) {
        if (!t || !t.id || !t.url) continue;
        if (!/douyin\.com|xiaohongshu\.com|tiktok\.com|goofish\.com|xianyu\.com/i.test(t.url)) continue;
        try {
          chrome.tabs.sendMessage(t.id, { type: 'selectorsUpdated' }, () => {  try { void chrome.runtime.lastError; } catch (_) {} });
        } catch (_) {  }
      }
    });
  } catch (_) {  }
}

let selActiveChannel = 'douyin';

function wireSelectorConfig() {
  const toggle = $('selToggle');
  const panel = $('selPanel');
  if (toggle && panel) {
    toggle.addEventListener('click', () => {
      panel.style.display = panel.style.display === 'none' ? 'block' : 'none';
      if (panel.style.display !== 'none') loadSelectorConfig(selActiveChannel);
    });
  }
  // Tab 切换：点击抖音/小红书/TikTok/闲鱼标签 → 切到对应渠道并回填其已存配置
  const tabs = document.querySelectorAll ? document.querySelectorAll('.sel-tab') : [];
  for (const tab of Array.from(tabs)) {
    tab.addEventListener('click', () => {
      const ch = tab.getAttribute('data-channel');
      if (!ch) return;
      selActiveChannel = ch;
      for (const t of Array.from(tabs)) t.classList && t.classList.remove('active');
      tab.classList && tab.classList.add('active');
      loadSelectorConfig(ch);
    });
  }
  const saveBtn = $('selSave');
  if (saveBtn) {
    saveBtn.addEventListener('click', async () => {
      const ch = selActiveChannel;
      const cfg = collectSelectorConfig();
      const all = readAllSelectors();
      const merged = { ...all, [ch]: cfg };
      try {
        await saveSelectors(merged);
        broadcastSelectorsUpdated();
        showBanner('success', '✓ 选择器已保存', `${channelDisplayName(ch)} 配置已生效（已同步到已打开的私信页）`);
      } catch (e) {
        showBanner('error', '保存失败', String(e));
      }
    });
  }
  const clearBtn = $('selClear');
  if (clearBtn) {
    clearBtn.addEventListener('click', async () => {
      const ch = selActiveChannel;
      const all = readAllSelectors();
      if (all[ch]) delete all[ch];
      try {
        await saveSelectors(all);
        broadcastSelectorsUpdated();
        loadSelectorConfig(ch);
        showBanner('info', '已清除', `${channelDisplayName(ch)} 选择器配置已清除，恢复内置默认`);
      } catch (e) {
        showBanner('error', '清除失败', String(e));
      }
    });
  }
}

// ---- 巡检制度（需求上行②）----
// popup -> 当前活动私信页 content script：启动/停止/立即巡检/查状态。
function sendToActiveTab(msg, cb) {
  try {
    chrome.tabs.query({ active: true, currentWindow: true }, (tabs) => {
      const tab = tabs && tabs[0];
      if (!tab || !tab.id) { cb && cb({ ok: false, reason: 'no-active-tab' }); return; }
      if (!tab.url || !/douyin\.com|xiaohongshu\.com|tiktok\.com|goofish\.com|xianyu\.com/i.test(tab.url)) {
        cb && cb({ ok: false, reason: 'not-supported-host', url: tab.url });
        return;
      }
      try {
        chrome.tabs.sendMessage(tab.id, msg, (resp) => {
          const err = lastError();
          if (err) { cb && cb({ ok: false, reason: 'send-failed', error: err }); return; }
          cb && cb(resp);
        });
      } catch (e) { cb && cb({ ok: false, reason: 'send-threw', error: String(e) }); }
    });
  } catch (e) { cb && cb({ ok: false, reason: 'query-threw', error: String(e) }); }
}

function fmtPatrolStatus(s) {
  if (!s) return '无状态返回';
  if (s.ok === false) return '巡检不可用：' + (s.error || s.reason || '未知');
  const running = s.running ? '🟢 运行中' : '⚪ 未运行';
  const lines = [
    running + (s.inRound ? '（进行一轮中）' : ''),
    `轮间隔: ${s.intervalMs ? Math.round(s.intervalMs / 1000) + 's' : '-'}`,
    `累计轮数: ${s.rounds || 0}`,
    `累计访问会话: ${s.visited || 0}`,
    `有新消息会话: ${s.withNew || 0}`,
    `捕获新消息: ${s.captured || 0}`,
    `失败: ${s.failures || 0}`,
    s.lastRoundAt ? `上一轮: ${new Date(s.lastRoundAt).toLocaleTimeString()}（用时 ${s.lastDurationMs || 0}ms）` : '',
  ].filter(Boolean);
  return lines.join('\n');
}

function refreshPatrolStatus() {
  sendToActiveTab({ type: 'patrolStatus' }, (resp) => {
    const out = $('patrolOut');
    if (out) out.textContent = fmtPatrolStatus(resp);
  });
}

let patrolStatusTimer = null;
function startPatrolStatusPolling() {
  if (patrolStatusTimer) clearInterval(patrolStatusTimer);
  refreshPatrolStatus();
  patrolStatusTimer = setInterval(refreshPatrolStatus, UI_DEFAULTS.metaReportIntervalMs);
}

function wirePatrol() {
  const toggle = $('patrolToggle');
  const panel = $('patrolPanel');
  if (toggle && panel) {
    toggle.addEventListener('click', () => {
      panel.style.display = panel.style.display === 'none' ? 'block' : 'none';
      if (panel.style.display !== 'none') { refreshPatrolStatus(); startPatrolStatusPolling(); }
    });
  }
  const startBtn = $('patrolStart');
  if (startBtn) {
    startBtn.addEventListener('click', () => {
      const inp = $('patrolInterval');
      let sec = parseInt((inp && inp.value) || '', 10);
      if (!Number.isFinite(sec) || sec < 3) sec = Math.round(PATROL_DEFAULTS.intervalMs / 1000);
      const intervalMs = sec * 1000;
      sendToActiveTab({ type: 'patrolStart', intervalMs }, (resp) => {
        const out = $('patrolOut');
        if (resp && resp.ok) {
          if (out) out.textContent = `✓ 巡检已启动：每 ${sec}s 一轮，自动遍历左侧新消息会话并上报`;
          showBanner('success', '✓ 巡检已启动', `每 ${sec}s 一轮，自动遍历左侧新消息会话并上报`);
          startPatrolStatusPolling();
        } else {
          if (out) out.textContent = '启动失败：' + ((resp && (resp.error || resp.reason)) || '未知');
        }
      });
    });
  }
  const stopBtn = $('patrolStop');
  if (stopBtn) {
    stopBtn.addEventListener('click', () => {
      sendToActiveTab({ type: 'patrolStop' }, (resp) => {
        const out = $('patrolOut');
        if (out) out.textContent = resp && resp.ok ? '✓ 巡检已停止' : '停止失败：' + (resp && (resp.error || resp.reason) || '未知');
        refreshPatrolStatus();
      });
    });
  }
  const nowBtn = $('patrolNow');
  if (nowBtn) {
    nowBtn.addEventListener('click', () => {
      const out = $('patrolOut');
      if (out) out.textContent = '正在巡检一轮…';
      sendToActiveTab({ type: 'patrolNow' }, (resp) => {
        if (out) out.textContent = resp && resp.ok
          ? `✓ 巡检一轮完成：访问 ${resp.visited}，有新消息 ${resp.withNew}，捕获 ${resp.captured}，失败 ${resp.failures}`
          : '巡检失败：' + (resp && (resp.error || resp.reason) || '未知');
        refreshPatrolStatus();
      });
    });
  }
}

document.addEventListener('DOMContentLoaded', () => {
  $('serverUrl').placeholder = DEFAULT_PLACEHOLDER;
  $('token').placeholder = '服务端配置了桥接凭证时必填，留空会被全部拒绝（401）';
  loadConfig((cfg) => {
    $('serverUrl').value = cfg.serverUrl || '';
    $('token').value = cfg.token || '';
  });

  $('save').addEventListener('click', async () => {
    const btn = $('save');
    const serverUrl = normalizeServerUrl($('serverUrl').value);
    if (!serverUrl) {
      showBanner('error', '请输入服务端地址', '例如 ' + DEFAULT_PLACEHOLDER);
      $('serverUrl').focus();
      return;
    }
    btn.disabled = true;
    const cfg = { serverUrl, token: $('token').value.trim(), autoConnect: true };
    saveConfig(cfg, (ok) => {
      btn.disabled = false;
      if (ok) {
        showBanner('success', '✓ 已保存', '配置已写入 Chrome storage，background 收到。新打开抖音/小红书/TikTok/闲鱼私信页即生效。');
      }
      refreshStatus();
    });
  });

  $('test').addEventListener('click', async () => {
    const btn = $('test');
    const serverUrl = normalizeServerUrl($('serverUrl').value);
    const token = String($('token').value || '').trim();
    if (!serverUrl) {
      showBanner('error', '请输入服务端地址', '例如 ' + DEFAULT_PLACEHOLDER);
      return;
    }
    btn.disabled = true;
    // 占位文案不写死某个路径：命中哪一条由 HEALTH_PATHS 的顺序决定（本机实测停在 /health），
    // 原来那句"… /api/health"与实际打的那一口不一致，照着它排查会走偏。
    showBanner('info', '⏳ 正在测试…', `${serverUrl}\n健康口依次试：${HEALTH_PATHS.join(' → ')}\n凭证口：${BRIDGE_CREDENTIAL_PROBE_PATH}`);
    const r = await testConnection(serverUrl);
    // 健康口都没通时不再打凭证探测：那时它必然也失败，只会把"地址不通"淹在第二条红里。
    const cred = r.ok ? await probeBridgeCredential(serverUrl, token) : null;
    btn.disabled = false;
    const v = buildConnectionVerdict(r, cred);
    showBanner(v.kind, v.title, v.body);
  });

  $('serverUrl').addEventListener('keydown', (e) => { if (e.key === 'Enter') $('save').click(); });
  $('token').addEventListener('keydown', (e) => { if (e.key === 'Enter') $('save').click(); });

  $('status').addEventListener('click', refreshStatus);
  $('selfcheck').addEventListener('click', selfCheck);
  const deepBtn = $('deepSelfcheck');
  if (deepBtn) deepBtn.addEventListener('click', deepSelfCheck);
  const syncBtn = $('syncAll');
  if (syncBtn) syncBtn.addEventListener('click', syncAllConversations);
  const copyBtn = $('copySelfOut');
  if (copyBtn) copyBtn.addEventListener('click', copySelfOut);

  wireSelectorConfig();

  wirePatrol();

  $('openDouyin').addEventListener('click', () => openUrl(PLATFORM_ENTRY_URLS.douyin));
  $('openXhs').addEventListener('click', () => openUrl(PLATFORM_ENTRY_URLS.xiaohongshu));
  $('openTiktok').addEventListener('click', () => openUrl(PLATFORM_ENTRY_URLS.tiktok));
  $('openXianyu').addEventListener('click', () => openUrl(PLATFORM_ENTRY_URLS.xianyu));

  refreshStatus();

  // ---- 2026-08-15 M2-P1-产品1：健康度面板（实时轮询）----
  let _healthStop = null;
  const healthToggle = $('healthToggle');
  const healthPanel = $('healthPanel');
  if (healthToggle && healthPanel) {
    healthToggle.addEventListener('click', () => {
      const isOpen = healthPanel.style.display !== 'none';
      healthPanel.style.display = isOpen ? 'none' : 'block';
      if (!isOpen) {
        if (_healthStop) { try { _healthStop(); } catch (_) {} _healthStop = null; }
        _healthStop = startHealthPanelPolling({ containerId: 'healthOut', intervalMs: UI_DEFAULTS.popupHealthPanelPollMs });
      } else {
        if (_healthStop) { stopHealthPanelPolling(_healthStop); _healthStop = null; }
      }
    });
  }

  // ---- 2026-08-15 M2-P1-产品3：紧急停止（toggle）----
  const emergencyBtn = $('emergencyStop');
  if (emergencyBtn) {
    const refreshEmergencyLabel = async () => {
      const stopped = await isEmergencyStop();
      if (stopped) {
        emergencyBtn.textContent = '▶ 恢复桥接（解除紧急停止）';
        emergencyBtn.classList.remove('danger');
        emergencyBtn.classList.add('primary');
        showBanner('warn', '⛔ 桥接已紧急停止', '所有 content script 收到停止信号，不再发送任何 HTTP 请求。点击按钮恢复。');
      } else {
        emergencyBtn.textContent = '⛔ 紧急停止（停所有桥接）';
        emergencyBtn.classList.remove('primary');
        emergencyBtn.classList.add('danger');
      }
    };
    refreshEmergencyLabel();
    emergencyBtn.addEventListener('click', async () => {
      const stopped = await isEmergencyStop();
      if (stopped) {
        const r = await resumeBridge();
        if (r.ok) {
          showBanner('success', '✓ 已恢复', '桥接已恢复，content script 重新开始上行 / 下行。');
          refreshEmergencyLabel();
        } else {
          showBanner('error', '恢复失败', r.error || '未知');
        }
      } else {
        if (!window.confirm('确认紧急停止所有桥接？\n\n这将停止所有抖音/小红书/TikTok/闲鱼 私信页的：\n  · 自动巡检\n  · 下行回复\n  · ack 重试\n\n用户已收到的消息不受影响（已 cache），但服务端不会收到 ack。')) {
          return;
        }
        const r = await triggerEmergencyStop('user_manual');
        if (r.ok) {
          showBanner('warn', '⛔ 已紧急停止', '所有桥接活动已停止。点击按钮恢复。');
          refreshEmergencyLabel();
        } else {
          showBanner('error', '停止失败', r.error || '未知');
        }
      }
    });
  }

  // ---- 2026-08-15 M2-P1-产品4：多账号管理面板 ----
  const accountsToggle = $('accountsToggle');
  const accountsPanel = $('accountsPanel');
  const accountsList = $('accountsList');
  if (accountsToggle && accountsPanel && accountsList) {
    const refreshAccounts = async () => {
      try {
        const data = await new Promise((resolve) => {
          try {
            chrome.runtime.sendMessage({ type: 'getAccounts' }, (resp) => {
              try { if (chrome.runtime.lastError) { resolve({ accounts: {}, health: {} }); return; } } catch (_) {  }
              if (!resp) { resolve({ accounts: {}, health: {} }); return; }
              resolve(resp);
            });
          } catch (_) { resolve({ accounts: {}, health: {} }); }
        });
        const accounts = data.accounts || {};
        const health = data.health || {};
        const channels = Object.keys(accounts);
        if (channels.length === 0) {
          accountsList.innerHTML = '<div class="hint" style="padding:8px;">当前无活跃账号（content script 未启动）</div>';
          return;
        }
        const order = ['douyin', 'xiaohongshu', 'tiktok', 'xianyu', 'kuaishou'];
        const sorted = channels.sort((a, b) => {
          const ai = order.indexOf(a); const bi = order.indexOf(b);
          if (ai === -1 && bi === -1) return a.localeCompare(b);
          if (ai === -1) return 1; if (bi === -1) return -1;
          return ai - bi;
        });
        const rows = sorted.map((ch) => {
          const a = accounts[ch] || {};
          const h = health[ch] || {};
          const online = !!a.accountId;
          const healthy = h.healthy !== false;
          const dotClass = !online ? 'offline' : (healthy ? 'online' : 'unhealthy');
          // popup XSS 收口：accountId/currentConvId 抓自第三方页面 DOM，
          // h.state 回传自后端——一律转义后入模板（escapeHtml 与本文件
          // accounts.js/health.js 同款约定）
          const meta = [];
          if (a.accountId) meta.push(escapeHtml(a.accountId));
          if (a.currentConvId) meta.push('会话 ' + escapeHtml(a.currentConvId));
          if (typeof a.capturedCount === 'number') meta.push('已捕获 ' + a.capturedCount);
          if (h.state) meta.push('熔断 ' + escapeHtml(h.state));
          return `<div class="account-row" data-channel="${escapeHtml(ch)}">
            <div class="dot ${dotClass}"></div>
            <div class="name">${escapeHtml(channelDisplayName(ch))}</div>
            <div class="meta">${meta.join(' / ') || '无数据'}</div>
          </div>`;
        });
        accountsList.innerHTML = rows.join('\n');
      } catch (e) {
        accountsList.innerHTML = `<div class="hint" style="color:#dc2626;padding:8px;">加载失败：${escapeHtml(e && e.message ? e.message : String(e))}</div>`;
      }
    };
    accountsToggle.addEventListener('click', () => {
      const isOpen = accountsPanel.style.display !== 'none';
      accountsPanel.style.display = isOpen ? 'none' : 'block';
      if (!isOpen) refreshAccounts();
    });
    const pauseAllBtn = $('accountsPauseAll');
    const resumeAllBtn = $('accountsResumeAll');
    if (pauseAllBtn) {
      pauseAllBtn.addEventListener('click', async () => {
        if (!window.confirm('确认暂停所有渠道？\n\n这等价于点击"紧急停止"。')) return;
        const r = await triggerEmergencyStop('user_pause_all');
        if (r.ok) {
          showBanner('warn', '⏸ 已全部暂停', '所有桥接已暂停。');
        } else {
          showBanner('error', '暂停失败', r.error || '未知');
        }
      });
    }
    if (resumeAllBtn) {
      resumeAllBtn.addEventListener('click', async () => {
        const r = await resumeBridge();
        if (r.ok) {
          showBanner('success', '▶ 已恢复', '所有桥接已恢复。');
        } else {
          showBanner('error', '恢复失败', r.error || '未知');
        }
      });
    }
  }

  // ---- 2026-08-15 M2-P1-产品5：告警横幅自动弹出（健康度异常时）----
  // 任何时刻只要有渠道熔断/无响应，就在顶部自动弹红色横幅
  const _alertStop = startAlertPolling({
    onAlert: (alert) => {
      showBanner(alert.level, alert.title, alert.body);
    },
    onClear: () => {
      clearBanner();
    },
  });

  // ---- popup 卸载时 abort 未完成的 in-flight fetch（2026-08-05 审计 P1）----
  // MV3 popup 关闭后，promise resolved 也无处展示；继续占用 socket 直至超时纯属浪费。
  // 监听 pagehide（覆盖移动端 + 桌面）+ beforeunload（兜底），任一触发即 abort。
  const _abortOnUnload = () => {
    abortInFlightHealth();
    if (_healthStop) { try { _healthStop(); } catch (_) {} _healthStop = null; }
    if (_alertStop) { try { _alertStop(); } catch (_) {} }
  };
  window.addEventListener('pagehide', _abortOnUnload, { once: true });
  window.addEventListener('beforeunload', _abortOnUnload, { once: true });
});

if (typeof window !== 'undefined') {
  window.__popup = {
    normalizeServerUrl,
    testConnection,
    probeBridgeCredential,
    buildConnectionVerdict,
    saveConfig,
    loadConfig,
    showBanner,
    clearBanner,
    lastError,
    diagnoseUninjected,
    selfCheck,
    deepSelfCheck,
    copySelfOut,
    pingContentScript,
    runSelfcheck,
    runDeepSelfcheck,
    formatDeepSnapshot,
    tryAutoInject,
    syncAllConversations,
    abortInFlightHealth,
    _getHealthAbortCtl: () => _healthAbortCtl,
    health: { renderHealthPanel, startHealthPanelPolling, stopHealthPanelPolling },
    alert: { startAlertPolling, stopAlertPolling },
    emergency: { isEmergencyStop, triggerEmergencyStop, resumeBridge },
    errors: { explainError, formatErrorBanner },
  };
}

