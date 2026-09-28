import { describe, it, expect, vi, beforeEach } from 'vitest';
import { BaseAdapter } from '../src/core/channel-adapter.js';
import { SENDER, DIRECTION, CHANNELS } from '../src/core/types.js';
import { getConversationList } from '../src/channels/douyin.js';

beforeEach(() => {
  document.body.innerHTML = '';
});

// 让 jsdom 元素有可见性（真实浏览器 offsetParent 由布局决定；jsdom 恒为 null）
function makeVisible(el) {
  Object.defineProperty(el, 'offsetParent', { configurable: true, get: () => ({}) });
  return el;
}

describe('douyin.getConversationList 枚举会话列表', () => {
  it('从会话列表 DOM 提取 [{ id, name, el }]，id 取自 /user/<id>', () => {
    const wrapper = document.createElement('div');
    wrapper.className = 'conversationConversationListwrapper';
    function addConv(id, name) {
      const item = makeVisible(document.createElement('div'));
      item.setAttribute('data-e2e', 'conversation-item');
      const a = document.createElement('a');
      a.href = `/user/${id}`;
      item.appendChild(a);
      const nameEl = document.createElement('div');
      nameEl.className = 'name';
      nameEl.textContent = name;
      item.appendChild(nameEl);
      wrapper.appendChild(item);
    }
    addConv('MS4wA1', '张三');
    addConv('MS4wB2', '李四');
    document.body.appendChild(wrapper);

    const list = getConversationList();
    expect(list.length).toBe(2);
    expect(list[0].id).toBe('MS4wA1');
    expect(list[0].name).toBe('张三');
    expect(list[0].el).toBeTruthy();
    expect(list[1].id).toBe('MS4wB2');
    expect(list[1].name).toBe('李四');
  });

  it('无会话列表容器时返回空数组', () => {
    expect(getConversationList()).toEqual([]);
  });

  it('self 链接与无 id 节点被过滤', () => {
    const wrapper = document.createElement('div');
    wrapper.className = 'conversationConversationListwrapper';
    const self = makeVisible(document.createElement('div'));
    self.setAttribute('data-e2e', 'conversation-item');
    const a = document.createElement('a');
    a.href = '/user/self'; 
    self.appendChild(a);
    wrapper.appendChild(self);
    document.body.appendChild(wrapper);
    expect(getConversationList()).toEqual([]);
  });
});

describe('BaseAdapter.syncAllConversations 遍历回填', () => {
  function makeConvItem(id, name) {
    const el = makeVisible(document.createElement('a'));
    el.href = `/user/${id}`;
    return { id, name, el };
  }

  // 模拟「点击会话项 → 活动会话变为该会话」的 DOM 行为
  function buildAdapter(convList, clickSpy) {
    let currentActive = null; 
    const hooks = {
      match: () => true,
      getAccountId: () => 'acc1',
      getConversationId: () => currentActive,
      getMessageRoot: () => null,
      getMessageItems: () => [makeVisible(document.createElement('div'))],
      parseMessageItem: () => ({
        sender_type: SENDER.CUSTOMER,
        text: '历史消息',
        message_id: 'm-' + Math.random(),
        timestamp: Date.now(),
      }),
      extractMessages: () => null, 
      getConversationList: () => convList,
    };
    const adapter = new BaseAdapter({ name: 'test', channel: 'douyin_web', SEL: {}, hooks });
    convList.forEach((conv) => {
      conv.el.addEventListener('click', () => {
        currentActive = conv.id;
        if (clickSpy) clickSpy(conv.id);
      });
    });
    return { adapter, getActive: () => currentActive };
  }

  it('遍历每个会话并各回填一次历史（统一收信中心各一条记录）', async () => {
    const convList = [makeConvItem('MS4wA1', '张三'), makeConvItem('MS4wB2', '李四'), makeConvItem('MS4wC3', '王五')];
    const clickSpy = vi.fn();
    const { adapter } = buildAdapter(convList, clickSpy);
    const onMessage = vi.fn();
    adapter.callbacks.onMessage = onMessage;

    const result = await adapter.syncAllConversations({ throttleMs: 1 });

    expect(result.synced).toBe(3);
    expect(result.failures).toBe(0);
    expect(clickSpy).toHaveBeenCalledTimes(3);
    expect(onMessage).toHaveBeenCalledTimes(3);
    const ids = onMessage.mock.calls.map((c) => c[0].conversation_id);
    expect(ids).toEqual(expect.arrayContaining(['MS4wA1', 'MS4wB2', 'MS4wC3']));
    expect(onMessage.mock.calls[0][0].direction).toBe(DIRECTION.INBOUND);
  });

  it('已同步集合生效：二次调用不再重复回填', async () => {
    const convList = [makeConvItem('MS4wA1', '张三'), makeConvItem('MS4wB2', '李四')];
    const { adapter } = buildAdapter(convList);
    const onMessage = vi.fn();
    adapter.callbacks.onMessage = onMessage;

    const r1 = await adapter.syncAllConversations({ throttleMs: 1 });
    expect(r1.synced).toBe(2);
    expect(onMessage).toHaveBeenCalledTimes(2);

    // 二次调用：列表未变，全部已同步 → 跳过，不重复回填
    const r2 = await adapter.syncAllConversations({ throttleMs: 1 });
    expect(r2.synced).toBe(0);
    expect(onMessage).toHaveBeenCalledTimes(2);
  });

  it('点击未打开线程（活动会话未变）→ 该会话跳过且不标记已同步', async () => {
    // 用 div 作为会话项（避免 jsdom 对 a[href] 的导航告警）；点击不切换活动会话
    const convList = [{ id: 'MS4wA1', name: '张三', el: makeVisible(document.createElement('div')) }];
    const hooks = {
      match: () => true,
      getAccountId: () => 'acc1',
      getConversationId: () => 'SAME', 
      getMessageRoot: () => null,
      getMessageItems: () => [],
      parseMessageItem: () => ({ sender_type: SENDER.CUSTOMER, text: 'x', message_id: 'm', timestamp: 1 }),
      extractMessages: () => null,
      getConversationList: () => convList,
    };
    const adapter = new BaseAdapter({ name: 'test', channel: 'douyin_web', SEL: {}, hooks });
    const onMessage = vi.fn();
    adapter.callbacks.onMessage = onMessage;

    const r = await adapter.syncAllConversations({ throttleMs: 1, waitActiveMs: 50 });
    expect(r.synced).toBe(0);
    expect(r.failures).toBe(1);
    expect(onMessage).not.toHaveBeenCalled();
  });

  it('列表首项即当前打开会话（活动会话未变但=目标）→ 计为成功并回填，不误报失败', async () => {
    // 场景：用户已打开会话 MS4wA1（getConversationId 返回它），遍历列表首项正是它。
    // 点击不会改变活动会话，但 realCid === conv.id，应直接回填成功。
    const convList = [{ id: 'MS4wA1', name: '张三', el: makeVisible(document.createElement('div')) }];
    const hooks = {
      match: () => true,
      getAccountId: () => 'acc1',
      getConversationId: () => 'MS4wA1', 
      getMessageRoot: () => null,
      getMessageItems: () => [makeVisible(document.createElement('div'))],
      parseMessageItem: () => ({
        sender_type: SENDER.CUSTOMER,
        text: '首项历史',
        message_id: 'm-active',
        timestamp: Date.now(),
      }),
      extractMessages: () => null,
      getConversationList: () => convList,
    };
    const adapter = new BaseAdapter({ name: 'test', channel: 'douyin_web', SEL: {}, hooks });
    const onMessage = vi.fn();
    adapter.callbacks.onMessage = onMessage;

    const r = await adapter.syncAllConversations({ throttleMs: 1 });
    expect(r.synced).toBe(1); 
    expect(r.failures).toBe(0);
    expect(onMessage).toHaveBeenCalledTimes(1);
    expect(onMessage.mock.calls[0][0].conversation_id).toBe('MS4wA1');
  });

  it('列表 id 与活动会话 id 表述不一致（/user/链接 vs data-*）→ 两者都记为已同步，二次调用不重复', async () => {
    // 场景：conv.id 取自 /user/<id> 链接（MS4wA1），但点击后 getConversationId 返回
    // data-conversation-id（total-99）。二者不同名，若只记 realCid，下次 rescan 按
    // conv.id 过滤仍会命中 → 永不续传。应把两个 id 都加入已同步集合。
    const convList = [
      { id: 'MS4wA1', name: '张三', el: makeVisible(document.createElement('button')) },
      { id: 'MS4wB2', name: '李四', el: makeVisible(document.createElement('button')) },
    ];
    let current = null;
    const hooks = {
      match: () => true,
      getAccountId: () => 'acc1',
      getConversationId: () => current, 
      getMessageRoot: () => null,
      getMessageItems: () => [makeVisible(document.createElement('div'))],
      parseMessageItem: () => ({
        sender_type: SENDER.CUSTOMER,
        text: '历史',
        message_id: 'm-' + Math.random(),
        timestamp: Date.now(),
      }),
      extractMessages: () => null,
      getConversationList: () => convList,
    };
    const adapter = new BaseAdapter({ name: 'test', channel: 'douyin_web', SEL: {}, hooks });
    convList.forEach((conv) => {
      conv.el.addEventListener('click', () => { current = 'total-' + conv.id.replace('MS4w', ''); });
    });
    const onMessage = vi.fn();
    adapter.callbacks.onMessage = onMessage;

    const r1 = await adapter.syncAllConversations({ throttleMs: 1 });
    expect(r1.synced).toBe(2);
    expect(r1.failures).toBe(0);
    expect(adapter._syncedConvIds.has('MS4wA1')).toBe(true); 
    expect(adapter._syncedConvIds.has('total-A1')).toBe(true); 

    // 二次调用：无论按 conv.id 还是 realCid 过滤都不重复回填
    const r2 = await adapter.syncAllConversations({ throttleMs: 1 });
    expect(r2.synced).toBe(0);
    expect(onMessage).toHaveBeenCalledTimes(2);
  });

  it('无 getConversationList 钩子 → 安全回退（skipped）', async () => {
    const adapter = new BaseAdapter({ name: 'test', channel: 'douyin_web', SEL: {}, hooks: { match: () => true } });
    const r = await adapter.syncAllConversations();
    expect(r.skipped).toBe(true);
    expect(r.reason).toBe('no-hook');
  });
});

describe('openConversation / sendOutbound 目标会话切换（左侧找用户→点击进入右侧→发送）', () => {
  function makeConvItem(id, name) {
    const el = makeVisible(document.createElement('button'));
    el.setAttribute('data-e2e', 'conversation-item');
    return { id, name, el };
  }

  function buildCtx(convList, initialActive) {
    let current = initialActive;
    const hooks = {
      match: () => true,
      getAccountId: () => 'acc1',
      getConversationId: () => current,
      getMessageRoot: () => null,
      getConversationList: () => convList,
      sendText: vi.fn(async () => {}),
      extractMessages: () => null,
    };
    const adapter = new BaseAdapter({ name: 'test', channel: 'douyin_web', SEL: {}, hooks });
    convList.forEach((conv) => {
      conv.el.addEventListener('click', () => { current = conv.id; });
    });
    return { adapter, setActive: (v) => { current = v; }, getActive: () => current };
  }

  it('openConversation：目标会话≠当前时，点击左侧列表项并切到目标', async () => {
    const convList = [makeConvItem('a1', '张三'), makeConvItem('b2', '李四')];
    const { adapter } = buildCtx(convList, 'a1'); 
    const opened = await adapter.openConversation('b2', { waitActiveMs: 100 });
    expect(opened).toBe('b2');
  });

  it('openConversation：目标即当前会话 → 直接返回，不点击', async () => {
    const convList = [makeConvItem('a1', '张三')];
    const { adapter } = buildCtx(convList, 'a1');
    const opened = await adapter.openConversation('a1', { waitActiveMs: 100 });
    expect(opened).toBe('a1');
  });

  it('openConversation：左侧列表找不到目标 → 返回 null', async () => {
    const convList = [makeConvItem('a1', '张三')];
    const { adapter } = buildCtx(convList, 'a1');
    const opened = await adapter.openConversation('not-exist', { waitActiveMs: 50 });
    expect(opened).toBeNull();
  });

  // 这三条腿只测「切会话再发」的路由，夹具里的假 DOM 不会长出气泡 ⇒ 不显式给 sendVerifyMs:0
  // 就要白等一整个回查预算（2.5s），会把 5s 的用例超时吃掉。回查本身的判据在
  // adapter-b24-send-verify.test.js 里逐条钉。
  it('sendOutbound：目标会话≠当前 → 先切到目标会话再发送（左侧找用户→点击进入→发送）', async () => {
    const convList = [makeConvItem('a1', '张三'), makeConvItem('b2', '李四')];
    const { adapter } = buildCtx(convList, 'a1');
    const ok = await adapter.sendOutbound('您好，请问有什么可以帮您？', 'b2', { sendVerifyMs: 0 });
    expect(ok.ok).toBe(true);
    expect(adapter.getConversationId()).toBe('b2'); 
    expect(adapter.hooks.sendText).toHaveBeenCalledWith('您好，请问有什么可以帮您？');
  });

  it('sendOutbound：目标会话未找到 → 放弃发送（防串台）', async () => {
    const convList = [makeConvItem('a1', '张三')];
    const { adapter } = buildCtx(convList, 'a1');
    const ok = await adapter.sendOutbound('你好', 'ghost-user');
    expect(ok.ok).toBe(false);
    expect(adapter.hooks.sendText).not.toHaveBeenCalled();
  });

  it('sendOutbound：目标会话=当前 → 直接发送，不切换', async () => {
    const convList = [makeConvItem('a1', '张三')];
    const { adapter } = buildCtx(convList, 'a1');
    const ok = await adapter.sendOutbound('在的', 'a1', { sendVerifyMs: 0 });
    expect(ok.ok).toBe(true);
    expect(adapter.hooks.sendText).toHaveBeenCalledWith('在的');
  });
});

