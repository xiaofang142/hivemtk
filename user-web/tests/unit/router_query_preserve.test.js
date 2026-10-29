import { describe, it, expect, vi } from 'vitest'

// 路由守卫在"懒模块刚装好"那一次重定向里必须把 query（和 hash）带上。
//
// 这条判据是被真实例打出来的：新开页 /#/customerSession/list?session_id=… 时，
// 落地地址变成 #/customerSession/list ——参数没了。CSAT 看板「查看会话」用的正是
// window.open 深链，每次都是冷启动第一趟 ⇒ 参数必然被抹掉，页面里再怎么去读
// route.query 都读不到。同一会话里第二次进这一页反而正常（那时模块已登记，不再重定向），
// 所以这个坑只在"从别处点过来"的那一下出现，走查里极难归因到守卫这一行。
//
// 由此推出两条装架规矩：
//  1) 判据必须打在**冷模块**上——模块一旦登记，守卫根本不走那一支，断言就成了真空通过
//     （第一版把 hash 腿放在 customerSession 已热之后，去掉 hash: to.hash 也照样绿，就是这个错）；
//  2) 每个冷腿用各自的模块，一条腿吃掉的"冷"不会传给下一条。
// 为什么不用源码文本当判据：那只能证明这一行写了 query: to.query，
// 证不了 vue-router 真的把它带到了 currentRoute 上。这里跑的是真 push、真守卫、真懒登记。

vi.mock('@/utils/initHelper', () => ({ isInitialized: () => true }))
// 路由守卫在非公开路由上要 await ensureBootstrap()（真发 /api/system/init-status）。
// 不桩它的话，这条腿的成败取决于本机有没有一个「已初始化」的后端在跑：请求失败时
// 回落 localStorage（本用例里是空的）⇒ initialized=false ⇒ 首次 push 就被推进 /setup，
// 被测的「懒模块重定向带不带 query」根本走不到。这里把引导状态钉成「已初始化」，
// 与下面三个桩同一条隔离思路：本用例测的是守卫怎么带 query，不是引导状态怎么拉。
vi.mock('@/utils/bootstrapState', () => ({
  ensureBootstrap: () => Promise.resolve({ initialized: true, tenantMode: false }),
  getBootstrap: () => ({ initialized: true, tenantMode: false })
}))
vi.mock('@/stores/user', () => ({
  // setToken：守卫的 localStorage→Pinia 同步闸（401 僵尸登录态修复）会调用它，
  // mock 必须与真 store 形状一致，否则守卫在空 localStorage 下直接抛错。
  useUserStore: () => ({ isLoggedIn: true, role: 'admin', userInfo: { id: 1, username: 'probe' }, setToken: () => {} })
}))
// Layout.vue 在 setup 顶层就调 usePermissionStore()，而它是 pinia setup store，
// 内部 useUserStore() 要求有 active Pinia —— 本用例只装真路由（router.push），
// 没有 app.use(pinia)，于是 Layout 被懒加载进来时直接抛
// "getActivePinia() was called but there was no active Pinia"。
//
// 这里 mock 掉它（与上面 mock @/stores/user 同一条隔离思路：本用例的判据是路由守卫
// 怎么带 query，不是菜单怎么从后端拉），而不是给测试装一个真 Pinia ——
// 装上 pinia 会让守卫之外多出一整条菜单加载链路，而那条链路另有它的用例。
// stub 的两个成员是 Layout.vue 真正用到的那两个：menus（computed 读）与 ensureMenus（挂载时调）。
vi.mock('@/stores/permission', () => ({
  usePermissionStore: () => ({ menus: [], ensureMenus: () => Promise.resolve() })
}))

import router from '@/router'

describe('冷启动进入懒模块：深链参数不许在重定向里丢失', () => {
  it('第一趟（触发懒登记那一趟）就把 session_id 带到 currentRoute 上', async () => {
    // /customerSession/list 在这一腿之前从未导航过 ⇒ 这一趟真的走 routeLoaded 分支
    await router.push('/customerSession/list?session_id=sess_cold_1')
    expect(router.currentRoute.value.path).toBe('/customerSession/list')
    expect(router.currentRoute.value.name).toBe('CustomerSessionList')
    expect(router.currentRoute.value.query).toEqual({ session_id: 'sess_cold_1' })
    // 解析到了真路由，而不是兜底页
    expect(router.currentRoute.value.matched.map((m) => m.name)).toContain('CustomerSessionList')
  })

  it('冷模块同时保住 query 与 hash（两个都得跟着重定向走）', async () => {
    await router.push('/approvalTask/list?tab=mine&status=pending#row-7')
    expect(router.currentRoute.value.path).toBe('/approvalTask/list')
    expect(router.currentRoute.value.query).toEqual({ tab: 'mine', status: 'pending' })
    expect(router.currentRoute.value.hash).toBe('#row-7')
    expect(router.currentRoute.value.name).not.toBe('NotFound')
  })

  it('带编码的参数原样到位（深链里的会话号真的含冒号）', async () => {
    const raw = 'sess_telegram_9_group:-1004496876892'
    await router.push(`/customerSession/list?session_id=${encodeURIComponent(raw)}&from=csat`)
    expect(router.currentRoute.value.query.session_id).toBe(raw)
    expect(router.currentRoute.value.query.from).toBe('csat')
  })

  it('另一个懒模块同样受益（证明修的是守卫，不是给某一页开的后门）', async () => {
    await router.push('/badCase/list?status=pending')
    expect(router.currentRoute.value.path).toBe('/badCase/list')
    expect(router.currentRoute.value.query).toEqual({ status: 'pending' })
    expect(router.currentRoute.value.name).not.toBe('NotFound')
  })

  it('模块已登记之后的第二次进入同样保住参数', async () => {
    await router.push('/customerSession/list?session_id=sess_warm_2')
    expect(router.currentRoute.value.query).toEqual({ session_id: 'sess_warm_2' })
    expect(router.currentRoute.value.name).toBe('CustomerSessionList')
  })

  it('不带参数的日常导航不被改写地址', async () => {
    await router.push('/glossary')
    expect(router.currentRoute.value.fullPath).toBe('/glossary')
    expect(router.currentRoute.value.query).toEqual({})
    expect(router.currentRoute.value.name).toBe('GlossaryList')
  })

  it('冷模块只多走一趟：带参数的重定向不多于一次', async () => {
    // 这一腿断的是"这次修复的代价"：冷模块一趟导航只多一次重定向，且两次同址。
    // 说清它接不住什么：把守卫的"已登记就短路"那行摘掉（真无限重定向）后，
    // 实测不是这一腿变红，而是 vitest worker 直接 OOM（FATAL: heap out of memory，
    // 59s 后 rc=1、零行 --- FAIL）——装架红不是用例红，所以不拿它当这一腿的杀手；
    // 那种退化会以整份套件崩掉的形式出声，比任何单条断言都响。
    const passes = []
    const off = router.beforeEach((to) => { passes.push(to.fullPath) })
    let err = null
    try {
      await router.push('/backup/list?tab=db')
    } catch (e) {
      err = e
    } finally {
      off()
    }
    expect(err, err ? `push 被拒：${err?.message}` : '').toBeNull()
    const backupPasses = passes.filter((p) => p.startsWith('/backup/list'))
    expect(backupPasses.length).toBeLessThanOrEqual(2)
    expect(new Set(backupPasses).size).toBe(1)
    expect(router.currentRoute.value.query).toEqual({ tab: 'db' })
    expect(router.currentRoute.value.name).toBe('BackupList')
  })
})
