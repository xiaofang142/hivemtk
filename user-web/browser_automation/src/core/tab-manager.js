// tab-manager.js — 后台 tab 管理（寄生式：active:false 不抢焦点）

export function createTabManager(chromeAPI = chrome) {
  async function openTab(url, active = false) {
    // 寄生式约定：后台打开，不抢焦点
    return await chromeAPI.tabs.create({ url, active });
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

  return { openTab, closeTab, activateTab, tabExists };
}
