// background/index.js — MV3 Service Worker：connectNative + 原语分发
// SW 保活：Chrome 105+ connectNative 保活 SW，114+ port 收发消息也保活 → port 不断则 SW 不死。
// port 断开（浏览器退出/扩展重载）→ Host 进程被 Chrome 连带杀掉 → server 侧断连钩子清理会话。

import { createNativePort } from '../core/native-messaging.js';
import { createTabManager } from '../core/tab-manager.js';
import { dispatch } from '../core/primitives.js';
import { collectInteractiveNodes, assembleSnapshot, getRefSelector } from '../core/accessibility.js';

const tabManager = createTabManager();

// accessibility 的页面侧函数经 executeScript 注入，SW 侧只做组装
const accessibility = {
  collectInPage: collectInteractiveNodes,
  assemble: (collected) => {
    const { text } = assembleSnapshot(collected);
    return { snapshot: text };
  },
  getRefSelector,
};

const deps = { tabManager, accessibility };

let lastStatus = 'offline';

const nativePort = createNativePort(
  chrome,
  async (cmd) => {
    // Host → 扩展命令帧：{req_id, action, ...}
    const { req_id: reqId } = cmd;
    try {
      const data = await dispatch(cmd, deps);
      nativePort.post({ req_id: reqId, ok: true, data: data || {} });
    } catch (e) {
      nativePort.post({ req_id: reqId, ok: false, error: String(e?.message || e) });
    }
  },
  (status, info) => {
    lastStatus = status;
    if (status === 'offline' || status === 'giveup') {
      console.warn('[browser_automation] Host 通道状态:', status, info);
    }
  },
);

// popup 查询状态
chrome.runtime.onMessage.addListener((msg, _sender, sendResponse) => {
  if (msg?.type === 'status') {
    sendResponse({ status: lastStatus, connected: nativePort.isConnected() });
  }
  return false;
});

// MV3 SW 事件驱动：没有持久事件监听器时浏览器重启不会拉起 SW，
// 顶层 connectNative 也就无从执行 → Host 永远不连。注册 onStartup/onInstalled
// 作为持久事件锚点（SW 冷启动时顶层 connectNative 才会运行）。
chrome.runtime.onStartup.addListener(() => {});
chrome.runtime.onInstalled.addListener(() => {});
