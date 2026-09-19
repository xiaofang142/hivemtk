import { DEFAULT_USER_SERVER, isExperimentalChannel } from '../core/constants.js';
import { createLogger } from '../core/logger.js';
import { hydrateSelectors, SELECTOR_UPDATE_MSG } from '../core/selector-ai.js';
import { Uplink } from '../core/uplink.js';
import { PollingLoop } from '../core/polling-loop.js';

const log = createLogger('content', 'bridge');

// 下行限速告警去抖：被全局最小间隔拦截时每轮轮询都会触发，去抖避免刷屏。
const _rateLimitWarnAt = new Map(); 
const RATE_LIMIT_WARN_INTERVAL_MS = 15000;

// B6 修复：popup -> content 的 selfcheck 用 chrome.tabs.sendMessage 触发，
// 监听器必须在事件循环 turn 内同步调用 sendResponse，否则 chrome 会报
// "message port closed" 且 popup 永远收不到响应。
// 若需异步，必须在监听器内 return true 且最终在 .then 内调用 sendResponse；
// 此处同步处理即可。
//
// 关键修复：原版用 adapter.snapshotMeta() 取 accountId/conversationId，
// 但三渠道 adapter 都没实现 snapshotMeta hook（仅实现 getAccountId/getConversationId），
// 导致自检结果账号/会话永远空，无法定位"哪条会话/哪个账号"在跑。
// 改为走 getter 拿到真实值。
function handleSelfcheck(adapter, sendResponse, diag) {
  try {
    const items = adapter.getMessageItems();
    const parsed = items.slice(-5).map((it) => adapter.parseMessageItem(it)).filter(Boolean);
    sendResponse({
      channel: adapter.channel,
      matched: adapter.match(),
      matchMode: adapter.matchMode(), 
      accountId: adapter.getAccountId() || '',
      conversationId: adapter.getConversationId() || null,
      msgItemCount: items.length,
      selectors: adapter.SEL || null,
      sample: parsed.map((p) => ({ sender: p.sender_type, text: (p.text || '').slice(0, 60) })),
      bridgeActive: diag ? diag.active : null,
      stats: diag ? diag.stats : null,
    });
  } catch (e) {
    sendResponse({ error: String(e) });
  }
}

// 深度自检：即便 match() 返回 false 也能扫描 DOM，给出真实页面快照
// 用于「打开私信页但平台改版导致 match() 失败」的诊断场景
// 不读 adapter 状态，纯 DOM 扫描，IO 轻量
//
// 关键修复：早期版本用 `return false` 同步模式，但 scanDomSnapshot 在抖音/小红书/
// TikTok 等复杂 SPA 上做 4 次重量级 querySelectorAll，同步执行可能超过 Chrome
// 端口有效响应窗口（约 5s），导致 "The message port closed before a response
// was received" 错误，popup 永远收不到响应。
// 改为异步模式：return true 保持 port 打开，把 scan 放进 microtask 让出事件循环，
// sendResponse 包 try/catch 防止 port 已关闭时二次抛错。
function handleDeepSelfcheck(sendResponse) {
  Promise.resolve()
    .then(() => scanDomSnapshot())
    .then((snapshot) => {
      try {
        sendResponse({ ok: true, ...snapshot });
      } catch (e) {
        log.warn('deepSelfcheck sendResponse 失败（port 已关闭？）', e);
      }
    })
    .catch((e) => {
      try {
        sendResponse({ ok: false, error: String(e && e.message || e) });
      } catch (sendErr) {
        log.warn('deepSelfcheck 错误回传失败（port 已关闭）', sendErr);
      }
    });
}

// 通用 DOM 快照：扫描页面上所有可能的「消息输入/输出/会话」元素
// 不依赖任何平台特定选择器，给出真实可观测数据
export function scanDomSnapshot() {
  // 1) 可能的输入框：contenteditable / textarea / role=textbox
  const inputs = collectElements((root) => {
    const sels = [
      'div[contenteditable="true"]',
      'textarea',
      '[role="textbox"]',
      '[contenteditable=""]',
      '.sendbox textarea',
      'textarea.ant-input',
    ];
    return uniqueQueryAll(root, sels);
  });
  const visibleInputs = inputs.filter((el) => isVisible(el));

  // 2) 可能的发送按钮：button / [role=button] / aria-label 含"发" / 含 Send
  //    抖音真实发送按钮为 svg.messageMsgInputpublishBtn.e2e-send-msg-btn，需显式纳入
  const sendBtns = collectElements((root) => {
    const sels = [
      'button[type="button"]',
      'button[type="submit"]',
      '[role="button"]',
      '[class*="e2e-send-msg-btn"]',
      '[class*="send-msg"]',
      'svg[class*="send"]',
      '.sendbox button',
      '[class*="sendbox" i] button',
    ];
    return uniqueQueryAll(root, sels);
  });
  const visibleSendBtns = sendBtns
    .filter((el) => isVisible(el))
    .filter((el) => isLikelySendButton(el))
    .slice(0, 10);

  // 3) 可能的会话列表：含 list / chat / message / scroll 类的容器
  const listRoots = collectElements((root) => {
    const sels = [
      '[class*="chat-"]',
      '[class*="message-"]',
      '[class*="msg-"]',
      '[class*="conversation"]',
      '[class*="contact"]',
      '[class*="list"]',
      '[class*="recycle"]',
      '[class*="sidebar"]',
      '[class*="conv-list"]',
      '[id*="conv-list"]',
      '[id*="message-list"]',
      '[data-e2e*="message"]',
      '[data-e2e*="chat"]',
    ];
    return uniqueQueryAll(root, sels);
  });
  // 优先：conversation-list / conv-list / sider 排除 chat-main（右侧面板）
  const chatMainRegex = /chat-main|chat-main|ChatMain|ant-layout-content/i;
  const prioritized = listRoots.filter((el) => {
    const cls = el.className || '';
    return /conversation-list|conv-list|sider|chat-list/i.test(cls) && !chatMainRegex.test(cls);
  });
  const visibleListRoots = (prioritized.length ? prioritized : listRoots)
    .filter((el) => isVisible(el))
    .slice(0, 8);

  // 4) 截图 href（个人主页链接 → 推断账号 id）
  const accountLinks = uniqueQueryAll(document, ['a[href*="/user/"]', 'a[href*="/@"]', 'a[href*="profile"]', 'a[href*="personal?userId="]'])
    .filter((el) => isVisible(el))
    .slice(0, 5);
  const accountHints = accountLinks.map((a) => ({
    text: (a.textContent || '').trim().slice(0, 30),
    href: a.getAttribute('href') || '',
  }));

  // 5) 消息气泡候选：覆盖多版 DOM 变体，用于定位「为何没捕获到聊天消息」
  const msgItemCandidates = [
    'div[data-e2e="msg-item-content"]',
    '[data-e2e*="msg-item"]',
    '[class*="msg-item-content"]',
    '[class*="msg-content"]',
    '[class*="bubble"]',
    '[class*="Bubble"]',
    '[class*="messageText"]',
    '[class*="MessageText"]',
    '[class*="chatMsgItem"]',
    '[class*="MessageItem"]',
    '[class*="messageItem"]',
    '[class*="msgBubble"]',
    '[class*="MsgBubble"]',
    '[class*="imMessage"]',
    '[class*="ImMessage"]',
    '[class*="dialogItem"]',
    '[class*="chatItem"]',
    '[class*="ChatItem"]',
    '[class*="messageBubble"]',
    '[class*="message-row"]',
    '[class*="MessageRow"]',
    '[class*="message-text"]',
  ];
  const msgItems = uniqueQueryAll(document, msgItemCandidates).filter((el) => isVisible(el));
  const msgItemSample = msgItems.slice(0, 5).map((el) => elementSummary(el));
  // 命中哪个候选选择器（用于推荐精确 SEL）
  const hitMsgSelector = msgItemCandidates.find((s) => document.querySelector(s)) || null;

  // 6) 消息线程容器深度结构扫描（关键诊断）：
  //    新版 /chat 页消息气泡 class 未知时，预置候选全 miss → 从已知容器
  //    （im-chat-window / xhs-im-chat-window / chat-window 等）向下钻取，
  //    列出每层可见子节点的 class + 文本，精确定位真实气泡 class。
  const threadRoot = document.querySelector(
    '#message-list-scrollable, .im-chat-window, [class*="xhs-im-chat-window"], [class*="chat-window"], [class*="ChatWindow"], [class*="chat-content"], [class*="ChatContent"], [class*="im-chat"], [class*="ImChat"], [class*="chat-main"], [class*="ChatMain"], [class*="message-list-reverse"]'
  );
  let threadTree = [];
  if (threadRoot) {
    const walk = (el, depth) => {
      if (!el || depth > 4 || threadTree.length >= 12) return;
      const cls = (el.className && typeof el.className === 'string') ? el.className.trim().slice(0, 60) : '';
      const txt = (el.textContent || '').trim().slice(0, 24);
      if (cls && txt) {
        threadTree.push({ depth, cls, txt });
      }
      for (const child of el.children) walk(child, depth + 1);
    };
    for (const child of threadRoot.children) walk(child, 1);
  }

  return {
    url: location.href,
    hostname: location.hostname,
    title: document.title,
    inputCount: visibleInputs.length,
    inputSample: visibleInputs.slice(0, 5).map((el) => elementSummary(el)),
    sendBtnCount: visibleSendBtns.length,
    sendBtnSample: visibleSendBtns.map((el) => elementSummary(el)),
    listRootCount: visibleListRoots.length,
    listRootSample: visibleListRoots.map((el) => elementSummary(el)),
    accountHints,
    recommendedSelector: pickRecommendedSelector(visibleInputs),
    msgItemCount: msgItems.length,
    msgItemSample,
    recommendedMsgSelector: hitMsgSelector || '（未匹配到消息气泡，请把本快照 msgItemSample 发到 issue 校准 SEL.MSG_ITEM）',
    threadRootFound: !!threadRoot,
    threadTree,
  };
}

function uniqueQueryAll(root, sels) {
  const out = new Set();
  for (const sel of sels) {
    try {
      const list = root.querySelectorAll(sel);
      for (const n of list) out.add(n);
    } catch (_) {
    }
  }
  return Array.from(out);
}

function collectElements(producer) {
  return producer(document.body || document);
}

export function isVisible(el) {
  if (!el) return false;
  const style = getComputedStyle(el);
  if (style.display === 'none' || style.visibility === 'hidden') return false;
  // SVG 元素的 offsetParent 恒为 null（不在 HTML offset 链里），
  // 不能据此判隐藏；改为仅用 getBoundingClientRect 判断尺寸。
  // 早期版本 `el.offsetParent === null && position !== 'fixed'` 会把抖音
  // svg.messageMsgInputpublishBtn 发送按钮一律判成不可见，导致自检报「发送按钮 0 个」。
  const isSvg = typeof SVGElement !== 'undefined' && el instanceof SVGElement;
  if (!isSvg && el.offsetParent === null && style.position !== 'fixed') return false;
  const rect = el.getBoundingClientRect();
  if (rect.width === 0 || rect.height === 0) return false;
  return true;
}

function isLikelySendButton(el) {
  const cls = (el.getAttribute('class') || '').toLowerCase();
  if (/e2e-send-msg-btn|send-msg|sendmsg/.test(cls)) return true; 
  const aria = (el.getAttribute('aria-label') || '').toLowerCase();
  const text = (el.textContent || '').trim().toLowerCase();
  if (/发送|send|发 送|發送/.test(aria) || /发送|send|发 送/.test(text)) return true;
  if (el.querySelector('svg[fill*="FE2C55"], svg[fill*="fe2c55"]')) return true;
  return false;
}

function elementSummary(el) {
  const tag = el.tagName.toLowerCase();
  const id = el.id ? `#${el.id}` : '';
  const cls = (el.getAttribute('class') || '').trim().slice(0, 80);
  const clsPart = cls ? `.${cls.split(/\s+/).slice(0, 3).join('.')}` : '';
  const dataE2E = el.getAttribute('data-e2e');
  const dataE2EPart = dataE2E ? `[data-e2e="${dataE2E}"]` : '';
  const role = el.getAttribute('role');
  const rolePart = role ? `[role="${role}"]` : '';
  const placeholder = el.getAttribute('placeholder') || '';
  const ce = el.getAttribute('contenteditable');
  const cePart = ce !== null ? `[contenteditable="${ce || 'true'}"]` : '';
  const text = (el.textContent || '').trim().slice(0, 40);
  return {
    tag,
    selectorHint: `${tag}${id}${clsPart}${dataE2EPart}${rolePart}${cePart}`.slice(0, 200),
    placeholder: placeholder.slice(0, 50),
    text: text,
  };
}

function pickRecommendedSelector(visibleInputs) {
  if (!visibleInputs.length) return null;
  for (const el of visibleInputs) {
    if (el.tagName.toLowerCase() === 'textarea') {
      const ph = el.getAttribute('placeholder');
      if (ph) return `textarea[placeholder*="${ph.slice(0, 12)}"]`;
    }
  }
  for (const el of visibleInputs) {
    const ce = el.getAttribute('contenteditable');
    if (ce === 'true') {
      const dataE2E = el.getAttribute('data-e2e');
      if (dataE2E) return `[data-e2e="${dataE2E}"]`;
    }
  }
  for (const el of visibleInputs) {
    const role = el.getAttribute('role');
    if (role === 'textbox') {
      const cls = (el.getAttribute('class') || '').split(/\s+/).filter(Boolean).slice(0, 2).join('.');
      if (cls) return `.${cls}`;
    }
  }
  for (const el of visibleInputs) {
    if (el.getAttribute('contenteditable') === 'true') {
      return 'div[contenteditable="true"]';
    }
  }
  return null;
}

export function startBridge(channel, buildAdapter) {
  const adapter = buildAdapter();
  // 上报链路诊断状态（自检时返回给 popup，定位「解析到了但不上报」）。
  // 2026-08-05 HTTP-only 重构：移除 portAlive/connectError（无 port 概念），
  // 移除 register 计数（HTTP 模式无 REGISTER 帧）。
  const diag = { active: false, stats: { inbound: 0, history: 0, outbound: 0, dropped: 0 } };
  try { log.info('已注入 content script', channel, location.host, 'match=' + adapter.match()); } catch (_) {  }

  try { hydrateSelectors(); } catch (_) {  }

  // 配置读取：每次 submitIngest 提交前调一次，从 chrome.storage 拿 serverUrl/token。
  // chrome.storage 在 content script 不可用（需要 background 转发）— 但实测可用，
  // 与 background 共享同一 storage.local 命名空间。失败兜底走 DEFAULT_USER_SERVER。
  const getConfig = () => new Promise((resolve) => {
    if (!chrome || !chrome.storage || !chrome.storage.local) {
      resolve({ serverUrl: DEFAULT_USER_SERVER.baseUrl, token: '' });
      return;
    }
    try {
      chrome.storage.local.get('bridgeConfig', (res) => {
        const err = (chrome.runtime && chrome.runtime.lastError) ? chrome.runtime.lastError : null;
        if (err) {
          log.warn('getConfig 失败（fallback 默认）', err.message);
          resolve({ serverUrl: DEFAULT_USER_SERVER.baseUrl, token: '' });
          return;
        }
        const cfg = (res && res.bridgeConfig) || {};
        resolve({
          serverUrl: cfg.serverUrl || DEFAULT_USER_SERVER.baseUrl,
          token: cfg.token || '',
        });
      });
    } catch (e) {
      log.warn('getConfig 异常（fallback 默认）', e && e.message);
      resolve({ serverUrl: DEFAULT_USER_SERVER.baseUrl, token: '' });
    }
  });

  chrome.runtime.onMessage.addListener((msg, _sender, sendResponse) => {
    if (!msg || !msg.type) return false;
    // match() 安全包装：任何异常都视为「未匹配」并带错误信息返回，不中断监听器
    const safeMatch = () => {
      try { return adapter.match(); } catch (e) { return { error: String(e && e.message || e) }; }
    };
    if (msg.type === 'ping') {
      // ping 始终应答（探活用），让 popup 能区分"未注入" vs "未匹配页"
      let m = safeMatch();
      const isObj = m && typeof m === 'object' && !Array.isArray(m);
      sendResponse({ pong: true, channel, matched: m === true, matchError: isObj ? m.error : null, matchMode: adapter.matchMode() });
      return false;
    }
    if (msg.type === 'selfcheck') {
      let m = safeMatch();
      const isObj = m && typeof m === 'object' && !Array.isArray(m);
      if (m !== true) {
        sendResponse({
          channel,
          matched: false,
          matchMode: null,
          matchError: isObj ? m.error : null,
          accountId: '',
          conversationId: null,
          msgItemCount: 0,
          selectors: adapter.SEL || null,
          sample: [],
          hint: '当前页面不是私信/消息页；请打开目标会话的【私信/聊天】界面（不是首页/feed）后再次校准',
        });
        return false;
      }
      handleSelfcheck(adapter, sendResponse, diag);
      return false;
    }
    if (msg.type === 'deepSelfcheck') {
      handleDeepSelfcheck(sendResponse);
      return true;
    }
    if (msg.type === 'syncAllConversations') {
      if (!adapter.match()) {
        sendResponse({ ok: false, error: '当前页面不是私信页，无法遍历' });
        return false;
      }
      adapter.syncAllConversations({ throttleMs: 1000 }).then((r) => {
        try { sendResponse({ ok: true, ...r }); } catch (_) {  }
      }).catch((e) => {
        try { sendResponse({ ok: false, error: String((e && e.message) || e) }); } catch (_) {  }
      });
      return true;
    }
    if (msg.type === SELECTOR_UPDATE_MSG) {
      hydrateSelectors().then(() => { try { sendResponse({ ok: true }); } catch (_) {  } });
      return true;
    }
    if (msg.type === 'patrolStart') {
      if (!adapter.startPatrol) { sendResponse({ ok: false, error: '适配器不支持巡检' }); return false; }
      const r = adapter.startPatrol({ intervalMs: msg.intervalMs });
      sendResponse(r);
      return false;
    }
    if (msg.type === 'patrolStop') {
      if (!adapter.stopPatrol) { sendResponse({ ok: false, error: '适配器不支持巡检' }); return false; }
      sendResponse(adapter.stopPatrol());
      return false;
    }
    if (msg.type === 'patrolNow') {
      if (!adapter.patrol) { sendResponse({ ok: false, error: '适配器不支持巡检' }); return false; }
      adapter.patrol().then((r) => {
        try { sendResponse({ ok: true, ...r }); } catch (_) {  }
      }).catch((e) => {
        try { sendResponse({ ok: false, error: String((e && e.message) || e) }); } catch (_) {  }
      });
      return true;
    }
    if (msg.type === 'patrolStatus') {
      sendResponse(adapter.patrolStatus ? adapter.patrolStatus() : { ok: false, error: '适配器不支持巡检' });
      return false;
    }
    return false;
  });

  // K2 桥接统计（跨重连累计，便于排查「通通没数据」类问题）：
  // inbound=实时上行、history=历史上行、outbound=下行回写、dropped=端口断开丢弃、register=注册帧次数。
  const stats = diag.stats;

  // 抖音/小红书/TikTok 多为 SPA：content script 注入时私信面板（常作浮层/overlay）
  // 未必已打开，match() 此刻为 false；用户后续点开私信，面板才渲染。
  // 早期版本在 match() 为 false 时直接 return，导致「打开私信页但桥接从未启动」。
  // 改为：先立即尝试激活；不匹配则轮询，待私信面板出现再激活；面板关闭则自动停用。
  let active = false;
  let deactivateBridge = null;
  let pollTimer = null;

  // 2026-08-05 HTTP-only 重构：移除所有 WebSocket 相关状态机。
  //   原来「sync 限速 + onDisconnect 退避 + 5 次连续失败」全是 WS 时代的产物，
  //   HTTP 长轮询无状态、每次请求独立，无需重连计数。保留 sync() 限速用于
  //   「SPA 浮层打开/关闭」时频繁调用 activateBridge 的去抖。
  let lastSyncAt = 0;
  const SYNC_THROTTLE_MS = 3000;      

  const activateBridge = () => {
    let closed = false;       


    // 通过 postIngest 批量上报消息（纯桥接：不设 expect_reply，后端根据 sender_type 判断）。
    //
    // 关键设计：URL + 完整 query + body 预览由 http-ingest._logRequest 统一打印
    // （所有渠道 douyin/xhs/tiktok/xianyu 共用），用户可从 console 直接看到上行地址。
    //
    // 2026-08-06 架构重构：上行统一收口到 Uplink（父层封装，三通道相互独立）。
    // 4 个渠道的聊天内容都经 uplink.enqueue 推入上报队列；Uplink 负责短窗口合并 + POST /api/bridge/ingest。
    // 消息 hash（event_id）在前端完成：渠道已给则沿用，否则 Uplink 兜底补全（后端按 event_id 去重）。
    // AI 回复不再随 ingest 响应返回，改由 downlink 独立轮询 GET /api/bridge/outbox 拉取下发。
    // account_id 必须每次实时从 DOM 抓取：bridge 初始化时 SPA 可能尚未渲染账号链接，
    // 若一次性固化（旧实现 const accountId = ...），后续 outbox 拉取恒为 'default'，
    // 与 inbound 入库的真实 account_id 不匹配 → ClaimPendingOutbound 精确查询返回空 → 下行卡 inflight。
    const resolveAccountId = () => (typeof adapter.getAccountId === 'function' && adapter.getAccountId()) || 'default';
    const uplink = new Uplink({ channel, getConfig });
    const submitIngest = (message) => {
      if (!message) return;
      if (!message.account_id) message.account_id = resolveAccountId();
      uplink.enqueue(message);
    };

    adapter.start({
      onMessage: (message) => {
        stats.inbound++;
        const len = (message && message.content ? message.content.length : 0);
        log.info('[上行 message] #' + stats.inbound, {
          conv: message && message.conversation_id,
          sender: message && message.sender_type,
          len,
        });
        submitIngest(message);
      },
      onRateLimited: (decision) => {
        const now = Date.now();
        const last = _rateLimitWarnAt.get(decision.reason) || 0;
        if (now - last >= RATE_LIMIT_WARN_INTERVAL_MS) {
          _rateLimitWarnAt.set(decision.reason, now);
          log.warn('下行被风控拦截:', decision.reason);
        }
      },
    });

    // 2026-08-06 架构重构：启动桥接巡检 + 下发轮询（三通道相互独立）。
    // PollingLoop 内含：
    //   - 通道A·上报巡检：每 3s 遍历会话列表、逐会话切换随机 1-2s、抓消息推入 Uplink
    //   - 通道C·下发轮询：每 1.5s 拉取 GET /api/bridge/outbox，转发网页后经通道B·ack 确认
    const pollingLoop = new PollingLoop({
      getAdapter: () => adapter,
      getConfig,
      getMeta: () => ({ accountId: resolveAccountId() }),
      channels: [channel],
    });
    pollingLoop.start();

    // 页面卸载 / SPA 路由切换时清理
    // B5（批3）：原实现每次 activate 都 addEventListener 且从不移除——面板反复开关使
    // 监听器随激活次数累积（每层闭包持有旧 pollingLoop/adapter 引用=内存泄漏）。
    // 修复：cleanup 执行时自我摘除；deactivate（sync 的 unmatch 分支）即回收监听。
    const cleanup = () => {
      closed = true;
      try { pollingLoop.stop(); } catch (_) {  }
      try { adapter.stop(); } catch (_) {  }
      window.removeEventListener('beforeunload', cleanup);
      window.removeEventListener('pagehide', cleanup);
    };
    window.addEventListener('beforeunload', cleanup, { once: true });
    window.addEventListener('pagehide', cleanup, { once: true });

    window.__bridgeAdapter = adapter;
    // B8：实验渠道如实告警（kuaishou 选择器未经真机校准）——不拦截功能，只标成熟度。
    if (isExperimentalChannel(channel)) {
      log.warn(`实验渠道 ${channel}：选择器未经真机校准，读漏/发偏时请在配置面板覆盖选择器`);
    }
    log.info('桥接已启动（HTTP-only）', channel);
    return cleanup;
  };

  const sync = () => {
    // 2026-08-05 修复：sync 节流，防止 SPA 浮层打开/关闭切换时被频繁激活。
    //   至少 3 秒间隔，上次 sync 不足 3 秒则跳过。
    const now = Date.now();
    if (now - lastSyncAt < SYNC_THROTTLE_MS) return;
    lastSyncAt = now;
    // match() 若抛异常（平台早期 DOM 结构不完整 / 某选择器报错），不得中断整个桥接。
    let matched = false;
    try { matched = !!adapter.match(); } catch (e) { log.warn('match() 异常，跳过本轮', e && e.message); }
    if (!active && matched) {
      log.info('私信面板已匹配', channel, '，启动桥接');
      deactivateBridge = activateBridge();
      active = true;
      diag.active = true;
    } else if (active && !matched) {
      log.info('私信面板已关闭', channel, '，停止桥接');
      if (deactivateBridge) { deactivateBridge(); deactivateBridge = null; }
      active = false;
      diag.active = false;
    }
  };

  sync();
  if (!active) {
    log.info('当前页面不匹配', channel, '，启动轮询等待私信面板打开…');
  }
  pollTimer = setInterval(sync, 1500);

  // K2 统计汇总日志：每 15s 打印一次累计条数，便于现场排查数据是否上行/下行/丢弃。
  // 2026-08-05 HTTP-only 重构：移除 register 字段（HTTP 模式无 REGISTER 帧）。
  let statsTimer = setInterval(() => {
    log.info('桥接统计', {
      channel,
      active,
      inbound: stats.inbound,
      history: stats.history,
      outbound: stats.outbound,
      dropped: stats.dropped,
    });
  }, 15000);

  const stopPoll = () => {
    if (pollTimer) { clearInterval(pollTimer); pollTimer = null; }
    if (statsTimer) { clearInterval(statsTimer); statsTimer = null; }
  };
  window.addEventListener('beforeunload', stopPoll, { once: true });
  window.addEventListener('pagehide', stopPoll, { once: true });
}

