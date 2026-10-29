# Assessment: web-token-expired-no-redirect

**症状（用户报告）**：user-web 与 platform-web 直接访问页面时提示 token 过期，但没有跳转登录页。
**日期**：2026-10-29 ｜ **复现环境**：本地 dev（user-web :8211 / platform-web :8212，后端 :8204/:8205）
**复现方式**：localStorage 塞一把 exp 已过期的 JWT（签名任意）→ 打开/F5 业务页 → 观察终态。

## 复现结果（ego-browser 实测）

| 场景 | user-web (:8211) | platform-web (:8212) |
|---|---|---|
| 无效签名 + 未过期 exp，goto | ✅ 跳 #/login | ✅ 跳 #/login |
| 过期 exp，**goto**（store 晚于塞 token 创建） | ✅ 跳 #/login | ✅ 跳 #/login |
| 过期 exp，**reload/直接访问**（store 创建时 token 已在） | ❌ **6/6 停在原页**，token 被清、toast 出现过 | ❌ **停在 #/dashboard**，token 被清、toast「登录已失效」 |

CDP `addScriptToEvaluateOnNewDocument` 导航日志（user-web，reload 场景）：

```
replace->#/login            ← request.js clearAuthAndGoLogin 的 push('/login') 生效
replace->#/messageHub/list  ← 随后被拉回原页面（覆盖了 /login）
```

## 根因（两仓库各一个，症状相同）

### user-web —— 路由守卫 pending next() 覆盖 401 跳转

`user-web/src/router/index.js` 的 `beforeEach`（非公开路由分支）：

1. guard 检查 `userStore.isLoggedIn`（token 在 localStorage，**已过期但存在** → true）；
2. `await permissionStore.ensureMenus()` —— 守卫挂起等待；
3. ensureMenus 的 API 请求 401 → `request.js` 的 `expireSession` → `clearAuthAndGoLogin()`
   → 清 token + `router.push('/login')` → **`replace->#/login` 发生**；
4. guard 从 await 恢复，**不再复查登录态**，继续走到末尾
   `next({ path: to.path, query, hash, replace: true })`（routeLoaded 分支）→
   **`replace->#/messageHub/list` 把 /login 覆盖掉**。

vue-router 4 中，guard 自身的 pending `next()` 在导航被外部 push 打断后仍会作为该导航的
最终决策应用 —— 于是 401 跳转被守卫自己的收尾覆盖。toast 已弹过（「认证令牌已失效」），
终态却是留在原页 = 用户症状。

### platform-web —— 401 处理只清 localStorage，不清 Pinia，守卫读到僵尸登录态

`platform-web/src/utils/request.js` 401 分支：`localStorage.removeItem('token')` +
`window.location.href = '/#/login'`（**同文档 hash 跳转，不重载页面**）。

但登录态的另一份真相在 Pinia：`stores/user.js` 的 `token = ref('')` 只在
`initAuth()`（store 创建时）从 localStorage 读一次。401 处理不清 ref →
`isLoggedIn` 仍为 true → 守卫 `if (to.path === '/login') { if (userStore.isLoggedIn) next('/dashboard') }`
**把刚要到的 /login 弹回 /dashboard** → 页面请求再次 401 → 循环若干轮后停在
#/dashboard（数据全空、只有过期 toast）。登录表单永远不出现。

## Files likely to change

| 仓库 | 文件 | 改动 |
|---|---|---|
| hivemtk | `user-web/src/router/index.js` | 守卫在 `ensureMenus()` 恢复后、任何 `next()` 决策前，复查「guard 开始时持有 token 但现在已清」→ 直接 `next('/login')`（一次性闸，不影响正常路径） |
| hivemtk-platform | `platform-web/src/router/index.js` | 守卫登录态改以 localStorage 为准（读时同步清掉残留的 Pinia ref），消除僵尸 isLoggedIn |
| hivemtk-platform | `platform-web/src/utils/request.js` | 401 分支同时清 Pinia 登录态（经 store，防 pinia 未激活用 try/catch 包裹），保持 location.href 兜底 |

## Severity

高（P1）：过期会话的用户被卡在空数据页，无法自助回到登录页；两前端同症。

## Unverified / 边界

- 生产 nginx 若把前端挂在子路径，platform-web 的 `location.href = '/#/login'` 会跳出子路径 ——
  本次不动它（dev/生产均挂根路径，见 deploy/），只修「跳了又被弹回」的根因。
- 复现用假 JWT（签名无效）只影响服务端判定路径，不影响前端跳转链路结论。
