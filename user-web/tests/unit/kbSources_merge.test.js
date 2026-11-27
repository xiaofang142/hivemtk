import { describe, it, expect } from 'vitest'
import { backendMenuItems } from './menuRegistry-helper'
import { getNavigationPaths, projectNavigation, findNavigationTrail } from '@/utils/menuNavigation'

// 2026-11-27 菜单审计：外部知识源入口收敛。
//
// 背景：`/knowledge/connectors`（Notion/飞书/钉钉/CRM 凭据与连通测试）与
// `/kb/connectors`（IMA/HTTP 导入连接器，拉取落库）是**互补的两段能力**，
// 此前在「知识中心 → 知识库工作台」下并列成两个语义相同的菜单项，用户无法
// 判断该进哪个。现在前端收敛成单个入口（key=kbSources）+ 两个 tab。
//
// 为什么后端两个 key 都要保留：menu_capability_routes.go 用它们分别守着
// /api/knowledge/connectors/* 与 /api/kb-connectors/* 的接口授权，删 key 会
// 让接口鉴权断链。所以约束是「后端保留两行、前端只出一个入口」——
// 这两条断言一起把该约束钉死。

describe('外部知识源入口收敛（kbSources）', () => {
  /** 把后端注册表还原成前端拿到的原始树形状（只保留 key/title/path/roles/children）。 */
  const rawTree = () => {
    // backendMenuItems 是扁平列表（含 key/path），需要重建层级。
    // 这里直接用扁平列表断言「两个 path 都在注册表里」，层级断言交给下一条。
    return backendMenuItems()
  }

  it('后端注册表必须同时保留两个连接器节点（接口授权依赖这两个 key）', () => {
    const items = rawTree()
    const byKey = new Map(items.map((it) => [it.key, it]))
    expect(byKey.has('knowledgeConnectors'), 'knowledgeConnectors 缺失会让 /api/knowledge/connectors/* 失去授权锚点').toBe(true)
    expect(byKey.has('kbImportConnectors'), 'kbImportConnectors 缺失会让 /api/kb-connectors/* 失去授权锚点').toBe(true)
    expect(byKey.get('knowledgeConnectors').path).toBe('/knowledge/connectors')
    expect(byKey.get('kbImportConnectors').path).toBe('/kb/connectors')
  })

  // 用一个与后端同形的最小树验证投影：两个连接器叶子 → 一个 kbSources 入口。
  const buildTree = () => ([
    {
      key: 'knowledge', title: '知识中心', path: '', roles: [],
      children: [
        {
          key: 'knowledgeWorkspace', title: '知识库工作台', path: '', roles: [],
          children: [
            { key: 'knowledgeOpenAPI', title: 'OpenAPI 集成', path: '/knowledge/openapi', roles: [], children: [] },
            { key: 'knowledgeConnectors', title: '外部知识源', path: '/knowledge/connectors', roles: [], children: [] },
            { key: 'kbImportConnectors', title: '外部知识源', path: '/kb/connectors', roles: [], children: [] }
          ]
        }
      ]
    },
    { key: 'analytics', title: '数据分析', path: '', roles: [], children: [
      { key: 'dashboardScreen', title: '数据大屏', path: '/dashboardScreen/list', roles: [], children: [] }
    ] }
  ])

  const findNode = (nodes, key, out = []) => {
    for (const n of nodes) {
      if (n.key === key) out.push(n)
      if (n.children?.length) findNode(n.children, key, out)
    }
    return out
  }

  it('投影后两个连接器收敛成唯一入口，且不再作为两个独立叶子出现', () => {
    const nav = projectNavigation(buildTree())
    const merged = findNode(nav, 'kbSources')
    expect(merged.length, 'kbSources 入口应唯一').toBe(1)
    expect(merged[0].connectorPaths).toEqual(['/knowledge/connectors', '/kb/connectors'])

    // 合并入口自身带 path（= 首个已授权页，点击可直达），这是与 channels 一致的口径；
    // 关键是原始两个菜单节点不能各自再出现一次，否则侧栏仍是两项。
    const leaves = getNavigationPaths(nav)
    expect(merged[0].path, '入口默认落在首个已授权页').toBe('/knowledge/connectors')
    expect(leaves.filter((p) => p === '/knowledge/connectors').length, '该 path 只应来自合并入口这一次').toBe(1)
    expect(leaves.filter((p) => p === '/kb/connectors').length, '第二个 tab 不应再作为独立菜单叶子').toBe(0)
    expect(leaves, '同一层其它菜单不受影响').toContain('/knowledge/openapi')
  })

  it('合并入口挂回原分组（知识库工作台），不跑到知识中心顶层', () => {
    const nav = projectNavigation(buildTree())
    const knowledge = nav.find((n) => n.key === 'knowledge')
    const workspace = knowledge.children.find((c) => c.key === 'knowledgeWorkspace')
    expect(workspace.children.some((c) => c.key === 'kbSources'), 'kbSources 必须在 knowledgeWorkspace 下').toBe(true)
    expect(knowledge.children.some((c) => c.key === 'kbSources'), 'kbSources 不应上浮到 knowledge 顶层').toBe(false)
  })

  it('两个 tab 的深链都能解析出完整面包屑归属链（否则侧栏整块消失）', () => {
    const nav = projectNavigation(buildTree())
    for (const path of ['/knowledge/connectors', '/kb/connectors']) {
      const trail = findNavigationTrail(nav, { path, matched: [{ path }] })
      expect(trail.length, `${path} 应能解析出归属链`).toBeGreaterThan(0)
      expect(trail[0].key, `${path} 的归属链必须以 knowledge 开头`).toBe('knowledge')
      expect(trail.at(-1).key, `${path} 的归属链叶子应为 kbSources`).toBe('kbSources')
    }
  })

  it('只授权其中一个时，入口只带被授权的那个 path（不越权）', () => {
    const tree = buildTree()
    // 只留 /kb/connectors，模拟只授权了导入连接器
    tree[0].children[0].children = tree[0].children[0].children.filter((c) => c.path !== '/knowledge/connectors')
    const nav = projectNavigation(tree)
    const merged = findNode(nav, 'kbSources')
    expect(merged.length).toBe(1)
    expect(merged[0].path).toBe('/kb/connectors')
    expect(merged[0].connectorPaths).toEqual(['/kb/connectors'])
  })
})
