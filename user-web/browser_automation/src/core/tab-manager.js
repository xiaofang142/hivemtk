// tab-manager.js — 后台 tab 管理（寄生式：active:false 不抢焦点）

export function createTabManager(chromeAPI = chrome) {
  async function openTab(url, active = false) {
    // 寄生式约定：后台打开，不抢焦点
    return await chromeAPI.tabs.create({ url, active });
  }

  // openTab 只是把 tab 建出来，页面还在加载。真机实证（session=432）：
  // open_tab → markdown 紧跟着跑，读到的是空气泡 DOM（"# \n"），三步全绿 = 假绿。
  // 所以「打开」必须以「这一帧能读」为完成，且把没等够如实回成 loaded:false：
  // 永不 complete 的重页（长轮询/流式）是现实，硬失败会把可用页判死，
  // 静默成功则把空读伪装成成果——两害相权取「如实回标记 + 读步骤自己判红」。
  async function waitForLoad(tabId, timeoutMs = 10000) {
    const budget = Math.min(Math.max(Number(timeoutMs) || 10000, 500), 30000);
    const deadline = Date.now() + budget;
    let title;
    for (;;) {
      let tab;
      try {
        tab = await chromeAPI.tabs.get(tabId);
      } catch {
        // tab 被用户关掉：不在这儿报错，交给后续步骤的 tab_not_found
        return { loaded: false, title: '', wait_ms: budget };
      }
      const waited = budget - Math.max(deadline - Date.now(), 0);
      title = tab.title || '';
      if (tab.status === 'complete') return { loaded: true, title, wait_ms: waited };
      if (Date.now() >= deadline) return { loaded: false, title, wait_ms: waited };
      await new Promise((r) => setTimeout(r, 150));
    }
  }

  async function closeTab(tabId) {
    if (!tabId || tabId <= 0) return;
    try {
      await chromeAPI.tabs.remove(tabId);
    } catch {
      // tab 已被用户关闭，忽略
    }
  }

  async function activateTab(tabId) {
    try {
      const tab = await chromeAPI.tabs.get(tabId);
      await chromeAPI.tabs.update(tabId, { active: true });
      if (tab.windowId) await chromeAPI.windows.update(tab.windowId, { focused: true });
    } catch {
      // tab 消失，忽略（后续截图会失败并被上层捕获）
    }
  }

  // 激活后让渲染进程出一帧再发 Input 事件所需的时间。
  // 与 screenshot 路径的 150ms 同源（那边是等视口稳定），这里等的是「开始消费输入」。
  const ACTIVATE_SETTLE_MS = 200;

  // ensureActiveForInput —— CDP trusted 输入（Input.dispatchMouseEvent / dispatchKeyEvent /
  // insertText）的前置条件，与 activateTab（截图用，会抢窗口焦点）是两件事。
  //
  // 为什么必须有：openTab 恒以 active:false 寄生式后台打开（见上），而**后台 tab 的渲染进程
  // 不产帧**；CDP 输入事件的 ack 是渲染进程消费完才回的，帧不出 → ack 压栈。真机实测同一 tab：
  //   后台态：连续 insertText 213 / 72 / 1367 / 68ms，下一批 3 条合计 9403ms（≈3.1s/条）
  //   激活后：1 / 346 / 53 / 81ms
  // 逐字键入 N 个字 = 2N 条事件，最坏 2N × 3.1s；N=6 的评论就吃光服务端 30s 闸，
  // 而扩展一个帧都不回 → 服务端只见「host 命令超时」，连卡在哪一步都无从归因。
  // （cdp/input.js 的 F11 注释记录的是同一现象的极端形态：mouseMoved 逐条 5003/5004/5005ms。）
  //
  // 为什么只切 tab 激活态、不抢窗口焦点：抢焦点（windows.update focused）会打断用户手头
  // 的工作，而后台 tab 不出帧才是这里的真凶——切到激活态即可出帧。
  //
  // 已知边界（如实记录，不假装解决）：整个窗口被最小化时，Chrome 仍可能对渲染做节流，
  // 此时 ack 依旧会慢；此处不擅自 un-minimize 用户的窗口（那比慢更打扰），退化行为由
  // cdp/input.js 的各层 deadline 兜住（慢 → 提前收手并如实回报，不黑盒）。
  async function ensureActiveForInput(tabId) {
    let tab;
    try {
      tab = await chromeAPI.tabs.get(tabId);
    } catch {
      return; // tab 已消失，交给上层 tab_not_found 归因
    }
    if (tab.active) return; // 已是激活态：零开销，也完全不打断用户
    try {
      await chromeAPI.tabs.update(tabId, { active: true });
    } catch {
      return; // 切不过去（窗口已关等），上层 deadline 兜底
    }
    await new Promise((r) => setTimeout(r, ACTIVATE_SETTLE_MS));
  }

  async function tabExists(tabId) {
    if (!tabId || tabId <= 0) return false;
    try {
      await chromeAPI.tabs.get(tabId);
      return true;
    } catch {
      return false;
    }
  }

  return { openTab, waitForLoad, closeTab, activateTab, ensureActiveForInput, tabExists, ACTIVATE_SETTLE_MS };
}
