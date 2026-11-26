// T10 统一收件箱展示纯函数定向测试（自他 / 群聊归并 / 高亮 / 筛选参数）
import { describe, it, expect } from 'vitest'
import {
  isPlatformMessage,
  isGroupConversation,
  groupNameFor,
  groupKeyFor,
  partitionInboxConversations,
  isHighlightedMember,
  defaultMemberHighlight,
  buildInboxListParams
} from '../../src/utils/inboxView.js'

describe('T10 isPlatformMessage 自他判据', () => {
  it('服务端 is_self 是权威，显式 false 不被 sender_type 翻案', () => {
    expect(isPlatformMessage({ is_self: true, sender_type: 'contact', direction: 'inbound' })).toBe(true)
    expect(isPlatformMessage({ is_self: false, sender_type: 'staff', direction: 'outbound' })).toBe(false)
  })

  it('缺 is_self 时按 direction / is_ai_reply / sender_type 保守回退', () => {
    expect(isPlatformMessage({ direction: 'outbound' })).toBe(true)
    expect(isPlatformMessage({ is_ai_reply: true })).toBe(true)
    expect(isPlatformMessage({ sender_type: ' AI ' })).toBe(true)
    expect(isPlatformMessage({ sender_type: 'contact' })).toBe(false)
    expect(isPlatformMessage({ sender_type: 'customer', direction: 'inbound' })).toBe(false)
  })

  it('昵称不参与判 self', () => {
    expect(isPlatformMessage({ sender_name: '客服小姐姐', direction: 'inbound' })).toBe(false)
    expect(isPlatformMessage(null)).toBe(false)
  })
})

describe('T10 群聊检测与归并键', () => {
  it('显式 conversation_type 优先，旧数据信号兜底', () => {
    expect(isGroupConversation({ conversation_type: 'group' })).toBe(true)
    expect(isGroupConversation({ conversation_type: 'single' })).toBe(false)
    expect(isGroupConversation({ is_group: true })).toBe(true)
    expect(isGroupConversation({ group_id: 'gid', conversation_type: undefined })).toBe(true)
    expect(isGroupConversation({ extra: { group_name: '相侵相碍一家人' } })).toBe(true)
    expect(isGroupConversation({})).toBe(false)
  })

  it('群名称取值顺序 group_name → extra.group_name → group_id → conversation_id，空值 fail-closed', () => {
    expect(groupNameFor({ group_name: ' 新群 ', extra: { group_name: '旧群' }, group_id: 'gid', conversation_id: 'cid' })).toBe('新群')
    expect(groupNameFor({ extra: { group_name: ' 旧群 ' }, group_id: 'gid' })).toBe('旧群')
    expect(groupNameFor({ group_id: ' gid ', conversation_id: 'cid' })).toBe('gid')
    expect(groupNameFor({ conversation_id: ' cid ' })).toBe('cid')
    expect(groupNameFor({ group_name: '   ', extra: { group_name: '' } })).toBe('')
  })

  it('归并键含渠道与账号，跨渠道/账号不合并；空群名返回空串', () => {
    expect(groupKeyFor({ platform: 'wecom', account_id: 'acc-1', group_name: '群A' })).toBe('wecom|acc-1|群A')
    expect(groupKeyFor({ platform: 'wecom', account_id: 'acc-2', group_name: '群A' })).toBe('wecom|acc-2|群A')
    expect(groupKeyFor({ platform: 'wecom', account_id: 'acc-1', conversation_id: '   ' })).toBe('')
  })
})

describe('T10 partitionInboxConversations 单聊/群聊分区', () => {
  const base = (id, convID, customer, isGroup, at) => ({
    id,
    platform: 'wecom',
    account_id: 'acc-1',
    conversation_id: convID,
    conversation_type: isGroup ? 'group' : 'single',
    is_group: isGroup,
    group_name: isGroup ? convID : undefined,
    customer_id: customer,
    customer_name: customer,
    member_count: isGroup ? 3 : 0,
    last_message_preview: `msg-${id}`,
    last_message_at: at
  })

  it('共享 conversation_id 的多个会话折叠成一张群卡，代表取最近活跃者', () => {
    const list = [
      base(1, '相侵相碍一家人', 'u-A', true, '2026-01-01T10:00:00Z'),
      base(2, '相侵相碍一家人', 'u-B', true, '2026-01-02T10:00:00Z'),
      base(3, 'single-C', 'u-C', false, '2026-01-03T10:00:00Z')
    ]
    const { singles, groups } = partitionInboxConversations(list)
    expect(singles.map((c) => c.id)).toEqual([3])
    expect(groups).toHaveLength(1)
    expect(groups[0].group_key).toBe('wecom|acc-1|相侵相碍一家人')
    expect(groups[0].member_count).toBe(3)
    expect(groups[0].representative || groups[0].id).toBe(2)
    expect(groups[0].last_message_preview).toBe('msg-2')
    expect(groups[0].conversations.map((c) => c.id)).toEqual([1, 2])
    // 客户绑定不丢失，仍可按成员回溯原会话
    expect(groups[0].conversations[0].customer_id).toBe('u-A')
  })

  it('同名群跨账号不合并；不能命名的群退化为单卡', () => {
    const list = [
      base(1, '群A', 'u-A', true, '2026-01-01T10:00:00Z'),
      { ...base(2, '群A', 'u-B', true, '2026-01-01T10:00:00Z'), account_id: 'acc-2' },
      { ...base(3, '', 'u-C', true, '2026-01-01T10:00:00Z'), group_name: '', conversation_id: '' }
    ]
    const { groups } = partitionInboxConversations(list)
    expect(groups).toHaveLength(3)
    expect(new Set(groups.map((g) => g.group_key)).size).toBe(3)
  })
})

describe('T10 当前成员高亮', () => {
  const msg = { sender_id: 'u-1', sender_name: '甲', direction: 'inbound' }

  it('按 sender_id / sender_name 命中，平台消息永不高亮', () => {
    expect(isHighlightedMember(msg, { sender_id: 'u-1' })).toBe(true)
    expect(isHighlightedMember(msg, { sender_name: '甲' })).toBe(true)
    expect(isHighlightedMember(msg, { sender_id: 'u-2' })).toBe(false)
    expect(isHighlightedMember({ ...msg, is_self: true }, { sender_id: 'u-1' })).toBe(false)
    expect(isHighlightedMember(msg, null)).toBe(false)
  })

  it('默认高亮 CRM 绑定人；成员列表匹配不到时用绑定值构造', () => {
    const conv = { customer_id: 'u-1', customer_name: '甲' }
    const members = [{ sender_id: 'u-1', sender_name: '甲' }, { sender_id: 'u-2', sender_name: '乙' }]
    expect(defaultMemberHighlight(conv, members)).toEqual({ sender_id: 'u-1', sender_name: '甲' })
    expect(defaultMemberHighlight(conv, [])).toEqual({ sender_id: 'u-1', sender_name: '甲' })
    expect(defaultMemberHighlight({}, members)).toBe(null)
  })
})

describe('T10 筛选参数映射', () => {
  it('渠道/账号/类型/昵称全部下推服务端，空值不下推', () => {
    expect(buildInboxListParams({
      platform: 'wecom', account_id: 'acc-1', conversation_type: 'group',
      nickname: '张三', status: 'open', filter: '', pinned: '', keyword: ''
    }, 2, 20)).toEqual({
      page: 2,
      page_size: 20,
      platform: 'wecom',
      account_id: 'acc-1',
      conversation_type: 'group',
      nickname: '张三',
      status: 'open'
    })
    expect(buildInboxListParams({}, 1, 10)).toEqual({ page: 1, page_size: 10 })
  })
})
