import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { BaseAdapter } from '../src/core/channel-adapter.js';
import { SENDER } from '../src/core/types.js';

// 会话锚点校验（批：Bridge 重复消息根治——扩展侧）：
// 线上实证（tz3rmdj 2026-10-22）：多窗口 DOM 采集把 A 会话消息错归 B 会话上行，
// 同 msg_id（内容哈希）跨会话二次入库触发重复应答。服务端已加错归去重闸（2min 窗），
// 扩展侧这道锚点校验把「sender_name 与会话头昵称错配」的残留气泡挡在上报之前。

function makeFixture(state) {
  const root = document.createElement('div');
  document.body.appendChild(root);
  let items = [];
  const hooks = {
    match: () => true,
    getMessageItems: () => items,
    parseMessageItem: (node) => ({
      message_id: node.__text,
      sender_type: node.__sender,
      sender_name: node.__senderName || '',
      text: node.__text,
      media_url: '',
      msg_type: 'text',
      timestamp: Date.now(),
      raw: '',
    }),
    getConversationId: () => state.convId,
    getPeerName: () => state.peerName || '',
    getMessageRoot: () => root,
  };
  const adapter = new BaseAdapter({ name: 't', channel: 'douyin_web', SEL: {}, hooks });
  return {
    adapter,
    arrive: (text, senderType = SENDER.CUSTOMER, senderName = '') => {
      const el = document.createElement('div');
      el.__text = text;
      el.__sender = senderType;
      el.__senderName = senderName;
      root.appendChild(el);
      items.push(el);
      return el;
    },
    clear: () => { root.innerHTML = ''; items = []; },
  };
}

let current = null;
beforeEach(() => { document.body.innerHTML = ''; });
afterEach(() => { if (current) { current.stop(); current = null; } });

describe('会话锚点校验（错归防线）', () => {
  let inbound;
  beforeEach(() => { inbound = []; });

  it('sender_name 与锚点昵称错配 ⇒ 增量拒收（跨会话残留不上行）', () => {
    const state = { convId: 'conv-示例会话', peerName: '示例会话' };
    const { adapter, arrive } = makeFixture(state);
    current = adapter;
    adapter.start({ onMessage: (m) => inbound.push(m) });
    adapter._refreshConvAnchor();

    // 锚点昵称=示例会话；气泡抓到的 sender_name=做点吃的 ⇒ 错配，拒收
    arrive('你是不是弄错了', SENDER.CUSTOMER, '做点吃的也不是很难啊');
    adapter._scanIncremental();
    expect(inbound).toHaveLength(0);

    // sender_name 与锚点一致 ⇒ 正常上行
    arrive('我弄错了', SENDER.CUSTOMER, '示例会话');
    adapter._scanIncremental();
    expect(inbound).toHaveLength(1);
    expect(inbound[0].content).toBe('我弄错了');
  });

  it('锚点或 sender 任一侧名字缺失 ⇒ 放行（抓取能力不足宁可不拦）', () => {
    const state = { convId: 'conv-A', peerName: '' }; // 锚点无昵称
    const { adapter, arrive } = makeFixture(state);
    current = adapter;
    adapter.start({ onMessage: (m) => inbound.push(m) });
    adapter._refreshConvAnchor();

    arrive('没有锚点名', SENDER.CUSTOMER, '随便谁');
    adapter._scanIncremental();
    expect(inbound).toHaveLength(1);

    // 消息无 sender_name（如群聊系统态/self）同样放行
    arrive('气泡无名', SENDER.CUSTOMER, '');
    adapter._scanIncremental();
    expect(inbound).toHaveLength(2);
  });

  it('群聊消息不走 sender 绑定校验（sender_name 是成员昵称非会话名）', () => {
    const state = { convId: 'conv-group', peerName: '某群' };
    const { adapter, arrive } = makeFixture(state);
    current = adapter;
    adapter.start({ onMessage: (m) => inbound.push(m) });
    adapter._refreshConvAnchor();

    // is_group=true，sender_name=成员昵称（≠锚点群名）⇒ 放行
    arrive('群里发言', SENDER.CUSTOMER, '成员甲');
    const el = document.querySelectorAll('[__text]') || [];
    // arrive 的节点要标记 is_group：通过 parseMessageItem 不便注入，直接改最后一条
    adapter._scanIncremental();
    // 即便 sender 错配，但 is_group 默认 false —— 此用例仅验证非群路径不受群名影响。
    // 群路径在 _senderMatchesAnchor 有 parsed.is_group 短路，由单元覆盖：
    expect(adapter._senderMatchesAnchor({ is_group: true, sender_name: '成员甲', sender_type: 'customer' })).toBe(true);
    expect(adapter._senderMatchesAnchor({ is_group: false, sender_name: '成员甲', sender_type: 'customer' })).toBe(false);
  });
});
