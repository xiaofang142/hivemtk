
const ERROR_CATALOG = {
  'http_400': {
    level: 'error',
    title_zh: '请求格式错误',
    title_en: 'Bad Request',
    body_zh: '发送到 user-server 的消息格式不合法（缺 channel、account_id、conversation_id 等必填字段）。',
    body_en: 'The request sent to user-server is malformed (missing required fields like channel/account_id/conversation_id).',
    action: '请检查扩展版本是否最新；若仍出现，请把自检输出发到 issue。',
    docUrl: 'https://github.com/hivemtk/hivemtk/blob/master/hivemtk/user-web/bridge/bridge.md',
  },
  'http_401': {
    level: 'error',
    title_zh: '通道凭证被拒',
    title_en: 'Unauthorized',
    body_zh: 'user-server 的桥接闸门（fail-closed）拒绝了当前通道凭证：为空、拼错，或已在后台轮换过期。',
    body_en: 'The user-server bridge gate (fail-closed) rejected the channel token: empty, mistyped, or rotated out.',
    action: '网页端「桥接凭证」页点「生成新凭证」→ 复制明文 → 粘贴到本扩展的「通道凭证 Token」→ 保存并重连（服务端只读 X-Bridge-Token，不是登录 JWT）。',
    docUrl: 'https://github.com/hivemtk/hivemtk/blob/master/hivemtk/user-web/bridge/bridge.md#token',
  },
  'http_403': {
    level: 'error',
    title_zh: '权限不足',
    title_en: 'Forbidden',
    body_zh: '服务端账号归属校验未通过：这个 platform 账号已绑定到另一个 HiveMTK 用户。',
    body_en: 'Server-side account ownership check failed: this platform account is already bound to another HiveMTK user.',
    action: '确认扩展里的 account_id 属于当前登录用户；若是换人接管，先在后台解绑该账号再重连。',
    docUrl: null,
  },
  'http_404': {
    level: 'warn',
    title_zh: '接口不存在',
    title_en: 'Not Found',
    body_zh: 'user-server 端点 404。请检查 user-server 版本是否支持桥接（8204 端口）。',
    body_en: 'user-server endpoint not found. Check user-server version supports bridge (port 8204).',
    action: '执行「测试连接」确认服务端可达；如可达仍报 404，请升级 user-server。',
    docUrl: null,
  },
  'http_413': {
    level: 'warn',
    title_zh: '请求体过大',
    title_en: 'Payload Too Large',
    body_zh: '单次上行消息数超过 user-server 上限（HTTPIngestMaxMessages=200），被截断。',
    body_en: 'Single ingest exceeded user-server limit (HTTPIngestMaxMessages=200), truncated.',
    action: '已自动按 firstRunMaxBatch=20 限速；如仍出现，请缩短巡检间隔。',
    docUrl: null,
  },
  'http_429': {
    level: 'warn',
    title_zh: '请求过于频繁',
    title_en: 'Too Many Requests',
    body_zh: 'user-server 限流中（同一账号短时间内上行过多）。',
    body_en: 'user-server is rate-limiting (too many requests from this account in a short window).',
    action: '已自动放慢节奏；如持续出现，请增大 PATROL_DEFAULTS.intervalMs。',
    docUrl: null,
  },

  'http_500': {
    level: 'error',
    title_zh: '服务端内部错误',
    title_en: 'Internal Server Error',
    body_zh: 'user-server 内部异常（PG/Redis/LLM 依赖故障）。',
    body_en: 'user-server internal error (PG/Redis/LLM dependency failure).',
    action: '请检查 user-server 日志；如持续，联系运维。',
    docUrl: 'https://github.com/hivemtk/hivemtk/blob/master/hivemtk/user-server/docs/operations/AI_AGENT_PERF_DEPLOY.md',
  },
  'http_502': {
    level: 'error',
    title_zh: '网关错误',
    title_en: 'Bad Gateway',
    body_zh: 'user-server 上游（反向代理层 /网关）未响应。',
    body_en: 'user-server upstream (反向代理层 /gateway) did not respond.',
    action: '检查 反向代理层 /网关日志与 user-server 进程是否存活。',
    docUrl: null,
  },
  'http_503': {
    level: 'error',
    title_zh: '服务暂不可用',
    title_en: 'Service Unavailable',
    body_zh: 'user-server 已启动但依赖（PG/Redis/LLM）暂未就绪。',
    body_en: 'user-server is up but dependencies (PG/Redis/LLM) are not ready yet.',
    action: '等 1-2 分钟自愈；如持续，执行「测试连接」查看健康度。',
    docUrl: null,
  },
  'http_504': {
    level: 'error',
    title_zh: '网关超时',
    title_en: 'Gateway Timeout',
    body_zh: 'user-server 处理超时（PG/LLM 慢查询）。',
    body_en: 'user-server timed out (slow PG/LLM query).',
    action: '检查 LLM 容器（llama.cpp 8207）是否过载；考虑降低并发。',
    docUrl: null,
  },

  'net_abort': {
    level: 'info',
    title_zh: '请求已取消',
    title_en: 'Request Aborted',
    body_zh: '网络请求被取消（用户重新点击 / popup 关闭）。',
    body_en: 'Request aborted (user re-clicked / popup closed).',
    action: '无需处理，正常行为。',
    docUrl: null,
  },
  'net_unreachable': {
    level: 'error',
    title_zh: '无法连接服务端',
    title_en: 'Service Unreachable',
    body_zh: 'user-server 不可达（端口未开 / 防火墙 / 域名解析失败）。',
    body_en: 'user-server unreachable (port not open / firewall / DNS failed).',
    action: '检查 user-server 是否运行、端口是否正确、Docker 端口映射。',
    docUrl: 'https://github.com/hivemtk/hivemtk/blob/master/hivemtk/user-server/docs/dev/DEVELOPMENT.md',
  },
  'net_timeout': {
    level: 'warn',
    title_zh: '连接超时',
    title_en: 'Connection Timeout',
    body_zh: '请求 user-server 超时（3.5s 未响应）。',
    body_en: 'user-server request timed out (no response in 3.5s).',
    action: '检查网络；如服务端在远程，尝试调大 healthCheckTimeoutMs。',
    docUrl: null,
  },
  'net_cors': {
    level: 'error',
    title_zh: 'CORS 跨域拦截',
    title_en: 'CORS Blocked',
    body_zh: '浏览器拦截了跨域请求（user-server 未配置 CORS 允许 chrome-extension://）。',
    body_en: 'Browser blocked cross-origin request (user-server CORS not configured for chrome-extension://).',
    action: '在 user-server config.yaml 配置 allow_origins 包含 chrome-extension://*。',
    docUrl: 'https://github.com/hivemtk/hivemtk/blob/master/hivemtk/user-server/docs/dev/DEVELOPMENT.md#cors',
  },

  'ack_failed': {
    level: 'warn',
    title_zh: '下行状态确认失败',
    title_en: 'Outbox Ack Failed',
    body_zh: '下行消息已发送给用户，但 ack 上报服务端失败。消息已入 _pendingAck 队列，会自动重试。',
    body_en: 'Outbound sent to user, but ack to server failed. Message queued in _pendingAck for retry.',
    action: '无需操作；超 10 次重试后会自动放弃。',
    docUrl: null,
  },
  'pending_ack_exceeded': {
    level: 'error',
    title_zh: '待重试队列堆积',
    title_en: 'Pending Ack Queue Overflow',
    body_zh: '_pendingAck 队列长度超过 1000 上限，可能服务端长时间不可达。',
    body_en: '_pendingAck queue exceeded 1000 cap; server may be unreachable for a long time.',
    action: '检查 user-server 健康度；手动重启扩展可清空队列。',
    docUrl: null,
  },
  'dead_letter': {
    level: 'warn',
    title_zh: '下行死信',
    title_en: 'Dead Letter',
    body_zh: '某条消息 ack 失败次数超 10，已放弃重试。用户已收到该消息，但服务端状态未同步。',
    body_en: 'A message exceeded 10 ack retries; given up. User already received the message, but server state is desynced.',
    action: '无需操作；如需精确状态，重启扩展可触发 reconciliation。',
    docUrl: null,
  },
  'circuit_open': {
    level: 'error',
    title_zh: '熔断器已开启',
    title_en: 'Circuit Breaker Open',
    body_zh: '连续 5 次失败，桥接已熔断 30 秒，避免继续加重服务端负担。',
    body_en: '5 consecutive failures, bridge opened for 30s to avoid burdening server.',
    action: '等待 30s 自动探测恢复；可点击「健康度」面板查看失败原因。',
    docUrl: null,
  },
  'rate_limited': {
    level: 'warn',
    title_zh: '本地下行限流',
    title_en: 'Local Rate Limited',
    body_zh: '本端令牌桶已空（每分钟 12 条上限）。',
    body_en: 'Local token bucket empty (12 per minute cap).',
    action: '无需操作；下一分钟自动补充。',
    docUrl: null,
  },
  'uninjected': {
    level: 'error',
    title_zh: '扩展未注入',
    title_en: 'Content Script Not Injected',
    body_zh: '当前页未注入桥接内容脚本（扩展被禁用 / URL 不匹配 / 旧标签页）。',
    body_en: 'Content script not injected (extension disabled / URL not in matches / stale tab).',
    action: 'chrome://extensions 检查开关；按 Ctrl+Shift+R 刷新页面；或在「自检」中触发自动注入。',
    docUrl: null,
  },
  'unknown': {
    level: 'warn',
    title_zh: '未知错误',
    title_en: 'Unknown Error',
    body_zh: '发生了预期外的错误，请把自检输出发到 issue。',
    body_en: 'Unexpected error occurred. Please send self-check output to issue.',
    action: '点击「自检当前私信页」获取详细诊断。',
    docUrl: 'https://github.com/hivemtk/hivemtk/issues',
  },
};

// 把任意错误归类到字典 key
function classifyError(err) {
  if (!err) return 'unknown';
  if (typeof err === 'string') {
    if (/abort|cancel/i.test(err)) return 'net_abort';
    if (/CORS|cors/i.test(err)) return 'net_cors';
    if (/timeout|timed out/i.test(err)) return 'net_timeout';
    if (/Failed to fetch|NetworkError|net::|unreachable/i.test(err)) return 'net_unreachable';
    if (/circuit.*open/i.test(err)) return 'circuit_open';
    if (/rate.*limit/i.test(err)) return 'rate_limited';
    if (/HTTP 4\d\d/i.test(err)) {
      const m = err.match(/HTTP (\d{3})/);
      return m ? 'http_' + m[1] : 'http_400';
    }
    if (/HTTP 5\d\d/i.test(err)) {
      const m = err.match(/HTTP (\d{3})/);
      return m ? 'http_' + m[1] : 'http_500';
    }
    return 'unknown';
  }
  if (err.status) {
    const s = String(err.status);
    if (/^4\d\d$/.test(s)) return 'http_' + s;
    if (/^5\d\d$/.test(s)) return 'http_' + s;
  }
  if (err.code === 'ack_failed' || err.code === 'ack-failed') return 'ack_failed';
  if (err.code === 'pending_ack_exceeded') return 'pending_ack_exceeded';
  if (err.code === 'dead_letter') return 'dead_letter';
  if (err.code === 'circuit_open') return 'circuit_open';
  if (err.code === 'rate_limited') return 'rate_limited';
  if (err.code === 'uninjected') return 'uninjected';
  if (err.name === 'AbortError') return 'net_abort';
  if (err.message) return classifyError(err.message);
  return 'unknown';
}

// 解释一个错误：返回 { title, body, level, action, docUrl }
// opts: { lang?: 'zh'|'en', fallbackTitle?, fallbackBody? }
export function explainError(err, opts) {
  const key = classifyError(err);
  const entry = ERROR_CATALOG[key] || ERROR_CATALOG.unknown;
  const lang = (opts && opts.lang) || 'zh';
  return {
    key,
    level: entry.level,
    title: lang === 'en' ? entry.title_en : entry.title_zh,
    body: lang === 'en' ? entry.body_en : entry.body_zh,
    action: entry.action,
    docUrl: entry.docUrl,
  };
}

// 格式化为 popup banner 用 HTML 片段（含操作建议 + 文档链接）
// opts: { lang?: 'zh'|'en', includeAction?: boolean, includeDoc?: boolean }
export function formatErrorBanner(err, opts) {
  const o = opts || {};
  const lang = o.lang || 'zh';
  const e = explainError(err, { lang });
  const lines = [];
  lines.push(`<div class="title">${escapeHtml(e.title)}</div>`);
  lines.push(`<div>${escapeHtml(e.body)}</div>`);
  if (o.includeAction !== false && e.action) {
    lines.push(`<div style="margin-top:4px;color:#374151;"><strong>建议：</strong>${escapeHtml(e.action)}</div>`);
  }
  if (o.includeDoc !== false && e.docUrl) {
    lines.push(`<div style="margin-top:4px;"><a href="${e.docUrl}" target="_blank" style="color:#4f8cff;text-decoration:underline;">📖 查看文档</a></div>`);
  }
  return { level: e.level, html: lines.join('\n') };
}

// HTML 转义（防 XSS）
function escapeHtml(s) {
  if (s == null) return '';
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

// 错误码 → banner HTML 直接渲染（用于 showBanner 替代）
// 等价于 showBanner(explainError(err).level, title, body+action)
export function showErrorBanner(showBannerFn, err) {
  const r = formatErrorBanner(err, { includeAction: true, includeDoc: true });
  showBannerFn(r.level, '', r.html);
}

if (typeof window !== 'undefined') {
  window.__errorMessages = {
    explainError,
    classifyError,
    formatErrorBanner,
    showErrorBanner,
    ERROR_CATALOG,
  };
}

