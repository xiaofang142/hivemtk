// native-messaging.js — Chrome Native Messaging port 封装（connectNative 长连接）
// 关键事实（官方文档）：
//  - 必须声明 "nativeMessaging" 权限
//  - sendNativeMessage 每次新起 Host 进程且只认第一条回包 → 不可用，必须 connectNative
//  - Chrome 105+ connectNative 保活 SW；114+ port 收发消息保活

export const NM_HOST_NAME = 'com.hivemtk.browser';

export function createNativePort(chromeAPI = chrome, onCommand, onStatusChange) {
  let port = null;
  let reconnectAttempts = 0;
  const MAX_RECONNECT = 5;
  let closedByUs = false;

  function connect() {
    closedByUs = false;
    try {
      port = chromeAPI.runtime.connectNative(NM_HOST_NAME);
    } catch (e) {
      onStatusChange?.('offline', String(e));
      scheduleReconnect();
      return;
    }

    port.onMessage.addListener((msg) => {
      // Host 回包/命令帧都走这里；命令帧带 action 字段
      if (msg && msg.action) {
        onCommand?.(msg);
      }
    });

    port.onDisconnect.addListener(() => {
      const err = chromeAPI.runtime.lastError ? String(chromeAPI.runtime.lastError.message || '') : '';
      port = null;
      onStatusChange?.('offline', err);
      if (!closedByUs) scheduleReconnect();
    });

    reconnectAttempts = 0;
    onStatusChange?.('online', '');
  }

  function scheduleReconnect() {
    if (reconnectAttempts >= MAX_RECONNECT) {
      onStatusChange?.('giveup', '重连次数用尽，请检查 Host 安装');
      return;
    }
    reconnectAttempts += 1;
    const delay = Math.min(2000 * 2 ** (reconnectAttempts - 1), 30000);
    setTimeout(() => connect(), delay);
  }

  function post(msg) {
    if (!port) throw new Error('native port offline');
    port.postMessage(msg);
  }

  function close() {
    closedByUs = true;
    try { port?.disconnect(); } catch { /* noop */ }
    port = null;
  }

  connect();
  return { post, close, isConnected: () => !!port };
}
