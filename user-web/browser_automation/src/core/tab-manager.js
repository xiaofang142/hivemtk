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

  async function tabExists(tabId) {
    if (!tabId || tabId <= 0) return false;
    try {
      await chromeAPI.tabs.get(tabId);
      return true;
    } catch {
      return false;
    }
  }

  return { openTab, waitForLoad, closeTab, activateTab, tabExists };
}
