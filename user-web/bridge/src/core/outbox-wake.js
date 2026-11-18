// 唤醒只携带注册身份；配置与凭证只在当前isolated world中比对，不发送给页面。
export function subscribeOutboxWake({ channels, getConfig, getMeta, onReady }) {
  const runtime = typeof chrome !== 'undefined' ? chrome.runtime : null;
  if (!runtime?.id || !runtime.onMessage?.addListener || !runtime.sendMessage) return () => {};
  const registrations = new Map();
  let stopped = false;
  let renewing = false;
  const scopeOf = (cfg) => JSON.stringify([cfg?.serverUrl || '', cfg?.token || '', cfg?.tenantCode || '']);
  const accountOf = (cfg) => getMeta?.()?.accountId || cfg?.active?.accountId || '';
  const validAccount = (account) => typeof account === 'string' && !!account && account !== 'default'
    && account.length <= 100 && !/[\s\p{Cc}]/u.test(account);
  const send = (message) => {
    try { return Promise.resolve(runtime.sendMessage(message)).catch(() => {}); }
    catch (_) { return Promise.resolve(); }
  };
  const unregister = (registration) => send({
    type: 'BRIDGE_WAKE_UNREGISTER', channel: registration.channel,
    accountId: registration.accountId, registrationId: registration.registrationId,
  });
  async function current(registration) {
    if (stopped || registrations.get(registration.channel) !== registration) return false;
    try {
      const cfg = await getConfig();
      return !stopped && registrations.get(registration.channel) === registration
        && scopeOf(cfg) === registration.scope && accountOf(cfg) === registration.accountId;
    } catch (_) { return false; }
  }
  async function renew() {
    if (stopped || renewing) return;
    renewing = true;
    try {
      const cfg = await getConfig();
      if (stopped) return;
      const accountId = accountOf(cfg);
      for (const channel of channels) {
        let registration = registrations.get(channel);
        const scope = scopeOf(cfg);
        const usable = validAccount(accountId) && !!cfg?.serverUrl && typeof cfg.token === 'string' && !!cfg.token.trim();
        if (registration && (!usable || registration.accountId !== accountId || registration.scope !== scope)) {
          registrations.delete(channel);
          void unregister(registration);
          registration = null;
        }
        if (!usable) continue;
        if (!registration) {
          registration = { channel, accountId, scope, registrationId: crypto.randomUUID() };
          registrations.set(channel, registration);
        }
        // 周期登记恢复被终止的MV3 Worker；后台相同账号仍共享一条连接。
        await send({ type: 'BRIDGE_WAKE_REGISTER', channel, accountId, registrationId: registration.registrationId });
        if (stopped || registrations.get(channel) !== registration) await unregister(registration);
        if (stopped) return;
      }
    } catch (_) { /* 登记失败不影响原轮询。 */ }
    finally { renewing = false; }
  }
  const listener = (message, sender) => {
    if (stopped || sender?.id !== runtime.id || sender?.tab || message?.type !== 'BRIDGE_OUTBOX_READY') return false;
    const registration = registrations.get(message.channel);
    if (!registration || registration.accountId !== message.accountId || registration.registrationId !== message.registrationId) return false;
    void current(registration).then((confirmed) => {
      if (confirmed) onReady({ isCurrent: () => current(registration) });
    }).catch(() => {});
    return false;
  };
  runtime.onMessage.addListener(listener);
  const timer = setInterval(() => { void renew(); }, 30000);
  void renew();
  return () => {
    if (stopped) return;
    stopped = true;
    clearInterval(timer);
    runtime.onMessage.removeListener(listener);
    for (const registration of registrations.values()) void unregister(registration);
    registrations.clear();
  };
}
