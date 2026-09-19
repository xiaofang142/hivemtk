import { describe, it, expect } from 'vitest'
import {
  rowActions,
  hasRejectReason,
  slaField,
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

describe('rowActions：审批类待办没有裸"完成"', () => {
  it('pending 的审批待办只给"裁决"和"撤销"', () => {
    const acts = rowActions(task({ kind: KIND_APPROVAL }), '7')
    expect(acts).toContain('decide')
    expect(acts).toContain('cancel')
    expect(acts).not.toContain('complete')
  })

  it('claimed 的审批待办同样不给"完成"（认领不改变裁决入口）', () => {
    const acts = rowActions(task({ kind: KIND_APPROVAL, status: 'claimed', assignee_user_id: '7' }), '7')
    expect(acts).not.toContain('complete')
    expect(acts).toContain('decide')
  })

  it('审批类不给"认领"：审批不可抢占，只能裁决', () => {
    expect(rowActions(task({ kind: KIND_APPROVAL }), '7')).not.toContain('claim')
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
  it('pending 审批给 approved / rejected，不给 expired', () => {
    const acts = verdictButtons({
      status: 'pending',
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
