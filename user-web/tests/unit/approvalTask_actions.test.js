import { describe, it, expect } from 'vitest'
import {
  rowActions,
  hasRejectReason,
  slaField,
  verdictBlockReason,
  verdictButtons,
  KIND_APPROVAL,
  KIND_HANDOFF,
  KIND_COLLECTION
} from '@/views/approvalTask/actions'

// 本文件守的是待办中心（N-9）唯一几条"点错就出业务事故"的判据：
// 哪一行能出现哪个按钮、驳回要不要理由、读哪一列 SLA。
// 判据写在 actions.js 而不是模板里，是为了让它能被跑出来 —— 模板里的 v-if
// 只能靠人眼 review，而这里第一条用例就是人眼最容易看漏的那条。

const task = (over) => ({ id: 'ht_1', kind: KIND_HANDOFF, status: 'pending', ...over })

describe('rowActions：审批类待办只有一颗按钮（裁决）', () => {
  it('pending 的审批待办只给"裁决"，complete 与 cancel 都不给', () => {
    // 整串逐字比，不用 toContain：这条判据要拦的是"多给一颗"，
    // toContain 在 ['decide','cancel'] 上照样绿（那正是要拦的形状）。
    expect(rowActions(task({ kind: KIND_APPROVAL }), '7')).toEqual(['decide'])
  })

  it('claimed 的审批待办同样只有"裁决"（认领不改变裁决入口）', () => {
    const acts = rowActions(task({ kind: KIND_APPROVAL, status: 'claimed', assignee_user_id: '7' }), '7')
    expect(acts).toEqual(['decide'])
  })

  it('审批类不给"认领"：审批不可抢占，只能裁决', () => {
    expect(rowActions(task({ kind: KIND_APPROVAL }), '7')).not.toContain('claim')
  })

  it('前后端同口径：待办侧的两颗关闭按钮服务端现在也都拒（见 model/human_task_test.go）', () => {
    // 这里钉的是"前端不许比服务端宽"：服务端已经把 approval 类的 complete/cancel 从
    // 对外动作表里摘掉，前端若哪天又放出这两颗，坐席就会点到 409（而不是像以前那样
    // 拿到一个"处理完了"的假成功）。
    for (const status of ['pending', 'claimed']) {
      const acts = rowActions(task({ kind: KIND_APPROVAL, status }), '7')
      expect(acts).not.toContain('complete')
      expect(acts).not.toContain('cancel')
    }
  })
})

describe('rowActions：会话类', () => {
  it('pending 可认领、可完成、可撤销', () => {
    expect(rowActions(task({ status: 'pending' }), '7')).toEqual(['claim', 'complete', 'cancel'])
  })

  it('claimed 且是自己认领的 ⇒ 出现"释放"', () => {
    const acts = rowActions(task({ status: 'claimed', assignee_user_id: '7' }), '7')
    expect(acts).toEqual(['release', 'complete', 'cancel'])
  })

  it('claimed 且是别人认领的 ⇒ 不给"释放"（旁人替放等于悄悄转走 A 正在处理的会话）', () => {
    const acts = rowActions(task({ status: 'claimed', assignee_user_id: '8' }), '7')
    expect(acts).not.toContain('release')
    expect(acts).toEqual(['complete', 'cancel'])
  })
})

describe('rowActions：催收类与边界', () => {
  it('催收升级不给认领/释放，只给完成与撤销', () => {
    expect(rowActions(task({ kind: KIND_COLLECTION }), '7')).toEqual(['complete', 'cancel'])
  })

  it('终态一行都不给（处理完的待办不许原地复活）', () => {
    for (const status of ['done', 'cancelled']) {
      expect(rowActions(task({ status }), '7')).toEqual([])
      expect(rowActions(task({ kind: KIND_APPROVAL, status }), '7')).toEqual([])
    }
  })

  it('未知 kind / 未知 status / 没有行 ⇒ 一律不给按钮（与服务层 fail-closed 同口径）', () => {
    expect(rowActions(task({ kind: 'mystery' }), '7')).toEqual([])
    expect(rowActions(task({ status: 'mystery' }), '7')).toEqual([])
    expect(rowActions(null, '7')).toEqual([])
    expect(rowActions(task(), '')).toEqual([])
  })

  it('没有登录身份时不给任何动作（动作类端点全部要 operator）', () => {
    expect(rowActions(task({ status: 'pending' }), '')).toEqual([])
    expect(rowActions(task({ kind: KIND_APPROVAL }), '')).toEqual([])
  })
})

describe('hasRejectReason：驳回理由是前端唯一闸门', () => {
  it('空串与纯空白都不算理由', () => {
    expect(hasRejectReason('')).toBe(false)
    expect(hasRejectReason('   \n ')).toBe(false)
    expect(hasRejectReason(undefined)).toBe(false)
  })

  it('给了内容才算（服务层不强制驳回理由，这里松一次就没有第二道）', () => {
    expect(hasRejectReason('金额不对')).toBe(true)
    expect(hasRejectReason('  金额不对  ')).toBe(true)
  })
})

describe('verdictButtons：人能点的裁决只有两个方向', () => {
  it('pending 且时限内 ⇒ 给 approved / rejected，不给 expired', () => {
    const acts = verdictButtons({
      status: 'pending',
      decidable_by_human: true,
      allowed_transitions: ['approved', 'rejected', 'expired']
    })
    expect(acts).toEqual(['approved', 'rejected'])
    expect(acts).not.toContain('expired')
  })

  it('已落定的审批不再给任何裁决口（后端对同一行第二次裁决回 409）', () => {
    expect(verdictButtons({ status: 'approved', allowed_transitions: [] })).toEqual([])
    expect(verdictButtons({ status: 'expired', allowed_transitions: [] })).toEqual([])
  })

  it('详情还没读回来 / 读失败了 ⇒ 不给按钮（不能凭"大概还没批"就让人点）', () => {
    expect(verdictButtons(null)).toEqual([])
    expect(verdictButtons({})).toEqual([])
    expect(verdictButtons({ status: 'pending' })).toEqual([])
  })

  // TTL 到期到清扫器跑上来之间最多一整轮，那段时间里状态照旧是 pending、
  // allowed_transitions 照旧给三个目标，而服务端已经不收这次裁决了。
  // 只看状态画按钮，端出来的就是「按钮亮着、点下去永远 409」那颗。
  it('时限已过的 pending：状态机还给目标态，按钮也不许给', () => {
    const lapsed = {
      status: 'pending',
      decidable_by_human: false,
      allowed_transitions: ['approved', 'rejected', 'expired']
    }
    // 前置：这一格里两份读数是**分开**的（后端把它们做成两个字段的全部理由）。
    expect(lapsed.allowed_transitions).toContain('approved')
    expect(verdictButtons(lapsed)).toEqual([])
  })

  it('decidable_by_human 缺失或不是字面 true ⇒ 不给按钮（缺读数按关闸处理）', () => {
    const base = { status: 'pending', allowed_transitions: ['approved', 'rejected'] }
    for (const v of [undefined, null, false, 0, '', 'true', {}]) {
      expect(verdictButtons({ ...base, decidable_by_human: v })).toEqual([])
    }
    // 真值那一格必须给按钮，否则上面那圈 not-to-equal 是白过的（判据没牙）。
    expect(verdictButtons({ ...base, decidable_by_human: true })).toEqual(['approved', 'rejected'])
  })
})

describe('verdictBlockReason：按钮不见了要说得出为什么', () => {
  it('时限已过的 pending ⇒ 一句话指向 TTL，而不是含糊的「不需要人裁决」', () => {
    const reason = verdictBlockReason({
      status: 'pending',
      decidable_by_human: false,
      allowed_transitions: ['approved', 'rejected', 'expired']
    })
    expect(reason).toContain('挂起时限已过')
    // 这句不许在已落定的行上开火：那时按钮不在是正常形状，冒出来一句"时限已过"是假话。
    expect(verdictBlockReason({ status: 'approved', decidable_by_human: false })).toBe('')
    expect(verdictBlockReason({ status: 'pending', decidable_by_human: true })).toBe('')
    expect(verdictBlockReason(null)).toBe('')
  })

  it('读数缺失 ⇒ 说「没读到」，不许替后端宣布「时限已过」', () => {
    // 并进上一句就等于把一次版本不一致报成一个业务事实，而人照着那句去查的是 TTL。
    const reason = verdictBlockReason({ status: 'pending', allowed_transitions: ['approved', 'rejected'] })
    expect(reason).toContain('没有')
    expect(reason).not.toContain('挂起时限已过')
  })
})

describe('slaField：每类读自己那一列', () => {
  it('三类各读一列，未知类读空', () => {
    expect(slaField(KIND_HANDOFF)).toBe('sla_first_response_at')
    expect(slaField(KIND_APPROVAL)).toBe('sla_decide_at')
    expect(slaField(KIND_COLLECTION)).toBe('sla_escalate_at')
    expect(slaField('mystery')).toBe('')
    expect(slaField(undefined)).toBe('')
  })
})
