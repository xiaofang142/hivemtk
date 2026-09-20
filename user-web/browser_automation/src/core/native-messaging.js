// native-messaging.js — Chrome Native Messaging port 封装（connectNative 长连接）
// 关键事实（官方文档）：
//  - 必须声明 "nativeMessaging" 权限
//  - sendNativeMessage 每次新起 Host 进程且只认第一条回包 → 不可用，必须 connectNative
//  - Chrome 105+ connectNative 保活 SW；114+ port 收发消息保活

export const NM_HOST_NAME = 'com.hivemtk.browser';

// 证活窗口：端口建立后活满这段时间才算「连上了」。connectNative 是同步返回 port 对象的，
// Host 进程起不来/秒退（清单缺失、二进制没装、服务端没起）都表现为返回成功后的异步
// onDisconnect——所以「返回了」不等于「连上了」，先报 online 就是假绿。
const PROVE_ONLINE_MS = 1500;
const RECONNECT_BASE_MS = 2000;
const RECONNECT_MAX_MS = 30000;
// giveup 之后并不真的停：本扩展没有 alarms 权限，SW 一旦没有这个重试循环就再也没人
// 拉起 Host（实测：服务端重启窗口超过退避预算后设备永久离线，只能重启浏览器）。
// 所以到上限只播报一次 giveup 给人看，重试降到长间隔继续跑。
const GIVEUP_RETRY_MS = 300000;
const MAX_RECONNECT = 5;

export function createNativePort(chromeAPI = chrome, onCommand, onStatusChange) {
  let port = null;
  let reconnectAttempts = 0;
  let closedByUs = false;
  let gaveUp = false;

  function connect() {
    closedByUs = false;
    let p;
    try {
      p = chromeAPI.runtime.connectNative(NM_HOST_NAME);
    } catch (e) {
      onStatusChange?.('offline', String(e));
      scheduleReconnect();
      return;
    }
    port = p;
    const proveTimer = setTimeout(() => {
      reconnectAttempts = 0; // 计数只在「确实连上了」时清零
      gaveUp = false;
      onStatusChange?.('online', '');
    }, PROVE_ONLINE_MS);

    p.onMessage.addListener((msg) => {
      // Host 回包/命令帧都走这里；命令帧带 action 字段
      if (msg && msg.action) {
        onCommand?.(msg);
      }
    });

    p.onDisconnect.addListener(() => {
      const err = chromeAPI.runtime.lastError ? String(chromeAPI.runtime.lastError.message || '') : '';
      clearTimeout(proveTimer);
      if (port === p) port = null;
      onStatusChange?.('offline', err);
      if (!closedByUs) scheduleReconnect();
    });
  }

  function scheduleReconnect() {
    if (reconnectAttempts >= MAX_RECONNECT) {
      if (!gaveUp) {
        gaveUp = true;
        onStatusChange?.('giveup', '重连次数用尽，请检查 Host 安装（仍以长间隔自动重试）');
      }
      reconnectAttempts += 1;
      setTimeout(() => connect(), GIVEUP_RETRY_MS);
      return;
    }
    reconnectAttempts += 1;
    const delay = Math.min(RECONNECT_BASE_MS * 2 ** (reconnectAttempts - 1), RECONNECT_MAX_MS);
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
