import { describe, it, expect } from 'vitest'
import { readFileSync, existsSync } from 'node:fs'
import { dirname, join } from 'node:path'

// 线索类型标签表的前后端对账（src/constants/cardPlatform.js ↔ user-server clue.go）。
//
// 后端 internal/service/clue.go 的 defaultClueTypes 是这张表的唯一事实源
// （GET /api/clues/type 原样返回它）。前端原先只抄了 4 条，而且抄错位：
// 前端 1=小红书/2=视频号/3=抖音/4=快手，后端 1=QQ/2=微信/3=电话/4=Telegram。
// 后果两条，都是用户看得见的：
//   · 线索列表的「线索类型」筛选只有 4 项，选「抖音」筛的是 type=3=电话；
//   · 统计页对 type 9~14 直接印裸数字（库里 277 条线索中 186 条＝67% 如此）。
// 所以这里不写死一份"期望值"，而是每次跑都去读那个 .go 文件现算 —— 后端加一档、
// 前端没跟，这一格就红。

// jsdom 环境里 import.meta.url 不是 file: 协议，所以用"从当前目录向上找最近的
// user-server/internal/service/clue.go"来定位 —— 在真实仓库和验证克隆里都命中各自那棵。
function findGoFile() {
  let dir = process.cwd()
  for (let depth = 0; depth < 8 && dir !== dirname(dir); depth++) {
    const candidate = join(dir, 'user-server', 'internal', 'service', 'clue.go')
    if (existsSync(candidate)) return candidate
    dir = dirname(dir)
  }
  return null
}

const GO_FILE = findGoFile()

import {
  CLUE_TYPE_OPTIONS,
  CLUE_TYPE_LABEL_MAP,
  getClueTypeLabel
} from '@/constants/cardPlatform'

function backendTypes() {
  if (!GO_FILE) throw new Error('向上找不到 user-server/internal/service/clue.go，对账口径已失效')
  const src = readFileSync(GO_FILE, 'utf8')
  const start = src.indexOf('var defaultClueTypes')
  if (start < 0) throw new Error('clue.go 里找不到 defaultClueTypes，对账口径已失效')
  const block = src.slice(start, src.indexOf('\n}', start))
  return [...block.matchAll(/\{Value:\s*"(\d+)",\s*Label:\s*"([^"]+)"\}/g)]
    .map((m) => ({ value: m[1], label: m[2] }))
}

describe('线索类型标签与后端一致', () => {
  it('后端 clue.go 在预期位置，对账不是空跑', () => {
    expect(GO_FILE, `从 ${process.cwd()} 向上找不到 user-server/internal/service/clue.go`).not.toBe(null)
    expect(backendTypes().length, `现读出后端档数（正控制，须 ≥18）`).toBeGreaterThanOrEqual(18)
  })

  it('前端表与后端逐条同值同标签、同条数', () => {
    const be = backendTypes()
    const fe = CLUE_TYPE_OPTIONS.map((o) => ({ value: String(o.value), label: o.label }))
    expect(fe.length, '前端条数与后端不一致').toBe(be.length)
    const diff = be
      .map((b, i) => ({ be: b, fe: fe[i] }))
      .filter((p, _i) => !p.fe || p.fe.value !== p.be.value || p.fe.label !== p.be.label)
    expect(diff, `错位项：${JSON.stringify(diff)}`).toEqual([])
  })

  it('后端登记的每一档都能取到真标签，不会退化成裸数字或"未知类型"', () => {
    for (const { value, label } of backendTypes()) {
      expect(getClueTypeLabel(value), `type=${value}`).toBe(label)
      expect(getClueTypeLabel(Number(value)), `type=${value} 以数字传入`).toBe(label)
    }
  })

  it('库里真实出现过的类型各有其名', () => {
    // 这七档是 clues 表 GROUP BY type 的全部取值（277 条）
    expect(getClueTypeLabel(1)).toBe('QQ')
    expect(getClueTypeLabel(4)).toBe('Telegram')
    expect(getClueTypeLabel(9)).toBe('抖音')
    expect(getClueTypeLabel(10)).toBe('快手')
    expect(getClueTypeLabel(11)).toBe('小红书')
    expect(getClueTypeLabel(12)).toBe('闲鱼')
    expect(getClueTypeLabel(14)).toBe('TikTok')
  })

  it('LEGACY 那 6 档不再盖住权威写法', () => {
    expect(getClueTypeLabel('5')).toBe('WhatsApp')
    expect(getClueTypeLabel('6')).toBe('Twitter/X')
    expect(CLUE_TYPE_LABEL_MAP['5']).toBe('WhatsApp')
  })

  it('没登记过的档位读出"未知类型 N"，而不是一枚像渲染坏了的裸数字', () => {
    expect(getClueTypeLabel(88)).toBe('未知类型 88')
    expect(getClueTypeLabel('')).toBe('-')
    expect(getClueTypeLabel(null)).toBe('-')
  })
})
