# Fix: web-token-expired-no-redirect

**日期**：2026-10-29 ｜ **依据**：[assessment.md](./assessment.md)

## Changes

### hivemtk / user-web

1. `src/router/index.js`（守卫 beforeEach）
   - **入口同步闸**：每次导航先对齐登录态单一真相——localStorage 无 token 时把
     Pinia 残留的僵尸 `token` ref 清掉（`setToken('')`）。这是主修复：修前探针显示
     401 清空 localStorage 后，僵尸 `isLoggedIn` 让守卫反复放行失效会话。
   - **两道 401 竞态闸**（`ensureBootstrap` 后、`ensureMenus` 后各一道）：guard 开始时
     持有 token、走到该点已被清掉的 ⇒ 立即 `next('/login')` 返回，防止 guard 收尾的
     `next({replace:true})` 把 `/login` 导航覆盖回原页面（assess 根因 ①）。
   - 判据直读 localStorage（401 清的正是它），不用 Pinia（僵尸值靠不住）。

### hivemtk-platform / platform-web

2. `src/router/index.js`
   - 守卫登录态改以 localStorage 为准（`syncAuthState`：LS 为空时同步清掉
     Pinia 残留 ref），`/login` 与受保护路由两个分支都用对齐后的 `hasToken` 判定
     （assess 根因 ②）。
3. `src/utils/request.js`
   - 401 分支升级为双清：Pinia（动态 import，try/catch 包裹防 pinia 未激活）+
     localStorage；外层 rejection handler 改 async 以承载动态导入。
     `window.location.href = '/#/login'` 兜底保留。

### 测试

4. `tests/unit/router_query_preserve.test.js`：store mock 补 `setToken: () => {}`
   （守卫同步闸会调用它，mock 需与真 store 形状一致）。

## Deviations from Assessment

- assessment 只把「僵尸 Pinia 登录态」归给 platform-web；实测 user-web 存在**同样问题**
  （同样的 `token = ref('')` + 单次 initAuth 模式），且它才是 user-web 6/6 复现的主因
  —— 仅加两道竞态闸时回归仍 5/6 失败，加入口同步后才 6/6 通过。故 user-web 的改动
  比评估多了入口同步这一处（同一文件内）。
- `router_query_preserve.test.js` 是评估未列出的改动文件（mock 形状跟随守卫新行为，
  属测试同步，非生产行为变更）。

## Local Verification

- `npx vitest run`（user-web）：71 文件 / 705 用例全绿（修前 router_query_preserve 7 例
  因 mock 缺 setToken 失败，补齐后通过）。
- `npx vite build`（user-web / platform-web）：均编译通过。
- 浏览器回归见 [test.md](./test.md)：verdict **verified**。
