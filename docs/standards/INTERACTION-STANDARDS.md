# HiveMtk 交互规范（API 契约 / 响应信封 / toast / i18n / 新手引导）

> **触发条件**：新增或修改任何 HTTP 接口、改 `user-web/src/utils/request.js` /
> `tokenRefresh.js`、改错误码口径、改 i18n 文案、加路由/引导，**之前必读**。
> 前后端任一侧改动都可能破坏另一侧，本文是两侧的共同契约。
>
> **权威源**（全部仓内实证）：
> - `user-server/internal/pkg/utils/response/response.go` + `errors.go`（响应信封与错误常量）
> - `user-server/internal/controller/bridge_extension.go`（controller 样板）
> - `user-web/src/utils/request.js` + `tokenRefresh.js`（拦截器与续期）
> - `user-web/src/i18n/` + `TRANSLATOR_GUIDE.md`（10 语言）
> - `docs/standards/BACKEND_MESSAGE_I18N_POLICY.md`（后端消息 i18n 策略）
> - `docs/standards/BACKEND-CODING-STANDARDS.md` / `FRONTEND-CODING-STANDARDS.md`
>
> 版本：v1.0 · 2026-11-13

---

## 1. API 路径与响应信封

### 1.1 路径

- 管理端接口一律 `/api/v1/<domain>/<resource>`（小写中划线）。
- 桥接专用通道：`/api/bridge/*`（ingest / outbox / outbox/ack / outbox/sse / capabilities），
  见 `docs/bridge/BRIDGE-ARCHITECTURE-2026-08-14.md`。
- WebSocket 备选：`/api/ws/channel`。

### 1.2 信封（唯一格式）

```json
{ "code": 0, "message": "ok", "data": {} }
```

- `code: 0` = 成功（**唯一**成功码）；其余 code 一律走错误分支。
- `message` = 面向用户的中文文案（后端已过 `i18n.Localize`，前端不要再包一层）。
- `data` 承载业务体；列表类用 `data:{list,total}`，分页用 `data:{list,total,page,page_size}`。

### 1.3 后端出口（禁止 `c.JSON`）

`internal/pkg/utils/response` 是唯一出口，架构门禁 `check-architecture.sh` [1/10] 强制：

| 场景 | 函数 | HTTP |
|---|---|---|
| 普通成功 | `response.Success(c, data, "ok")` | 200 |
| **已受理、结论未定** | `response.Accepted(c, data, msg)` | **202** |
| 列表 | `response.SuccessWithList(c, data, total)` | 200 |
| 分页 | `response.SuccessWithPage(c, data, page, size, total)` | 200 |
| 业务错误 | `response.Error(c, http.StatusXxx, "中文文案")` | 4xx/5xx |

**为什么有 `Accepted`（202）**：源码注释实证——报价发送等「已受理、结论未定」场景，
回 200 会被前端渲染成「已发送」，回 409 会被读成「被拒」，两种都错。新增这类接口必须用 202。

**错误文案禁止透传**：`response.Error` 内部按 `internalDetailMarkers` 剥离内部细节，
`maskedInternalMessage = "操作失败，请稍后重试"`，`leakWindowRunes = 16`（rune 滑窗防中文被字节切断漏判）。
⇒ controller 里**不许** `response.Error(ctx, 500, err.Error())`（架构门禁 S-36 + check-error-leak.py 双重拦）。

错误消息常量集中在 `internal/pkg/utils/response/errors.go`：
`ErrInvalidParams="无效的请求参数"` / `ErrInvalidIDFormat` / `ErrIDMismatch` /
`ErrResourceNotFound="资源不存在"` / `ErrUnauthorized="未授权访问"` /
`ErrPermissionDenied="权限不足"` / `ErrBusinessError="业务处理失败"` /
`ErrSystemAlreadyInitialized="系统已初始化，禁止重复创建超管"` 等。新错误先查这里有没有。

### 1.4 controller 判错口径

```go
if errors.Is(err, service.ErrXxx) {
    response.Error(ctx, http.StatusNotFound, "中文文案")
    return
}
response.Error(ctx, http.StatusInternalServerError, errors.ErrSystemError)
```

哨兵错误用 `errors.Is` 判，不要在 controller 里字符串匹配 err。

---

## 2. 前端请求层（`request.js` / `tokenRefresh.js`）

### 2.1 成功与失败分派

- `code === 0` ⇒ 成功，resolve `data`。
- 其余 code ⇒ 通用分支只弹一句 toast，不 resolve 不 reject 成异常。

### 2.2 HTTP 状态码分档

| 状态 | 行为 |
|---|---|
| 401 | 走 `expireSession(sentWith)`：**只有「仍是同一把令牌」才清凭据跳登录，且不 toast**；并发 401 只处理第一条 |
| 403 / 404 | **保留服务端文案**（常带「怎么才能用」的可执行信息），不要吞成通用文案 |
| 429 | 提示限流，引导稍后重试 |
| 500 | 通用失败提示 |

### 2.3 登录/注册端点裸调（O 轮线上修复）

`UNAUTHENTICATED_AUTH_PATHS = ['/api/auth/login', '/api/auth/register']`：
这两个端点请求拦截器**不带 Authorization**（它们自己就是凭据校验入口），
响应侧 401 **不清会话、不弹「登录已过期」**——否则已登录用户打错口令会把自己有效的会话弄死
（双 toast + 会话被清，实测复现过）。

### 2.4 令牌续期（`tokenRefresh.js`）

- **单飞锁**：并发 401 只发起一次 refresh。
- 刷新请求用**裸 axios 实例**（不挂业务拦截器，避免刷新自身再触发续期/401 清会话）。
- 不覆盖并发放回来的新令牌。
- 根因注释：两个入口各自 refresh 会让后端轮换两次，后一次必 401 ⇒ 登录成功后被踢回登录页。

### 2.5 toast 去重

`msg === lastToastMsg && now - lastToastTs < 2500` ⇒ 丢弃（2500ms 内同文案不重复弹）。
type 分派：`warning` → `ElMessage.warning`、`info` → `ElMessage.info`、其余 → `ElMessage.error`。

---

## 3. i18n

- 10 语言：`zh`（基准）/ `en` / `ja` / `de` / `ru` / `pt` / `fr` / `es` / `ar`，
  目录 `src/i18n/locales/`，翻译规则见同目录 `TRANSLATOR_GUIDE.md`。
- **中文是基准源**：新增文案先加 zh，再同步其余语言（缺失键回落到 zh，不许留空串）。
- 后端消息也走 i18n：`i18n.Localize(localeOf(c), message)`，策略见
  `docs/standards/BACKEND_MESSAGE_I18N_POLICY.md`。
- 用户可见文案（toast/表格空态/按钮）不许硬编码在模板里，统一走 i18n key。

---

## 4. 路由台账与新手引导

### 4.1 路由台账（改路由必同步）

- `router-list.md`（仓根）+ `user-web/router-list.md` 同源，逐路由记录状态/路径/组件/回归结论。
- 新增路由必须补台账行；UI 回归按台账逐页跑（见 AGENTS.md 规则-1）。

### 4.2 新手引导三层体系

新页面/新功能要配引导，三层（存量约定，改引导时保持三层齐）：
1. **首登弹层**：首次进入时的一次性说明。
2. **空态引导**：表格/列表无数据时告诉用户下一步做什么（不要只写「暂无数据」）。
3. **字段级提示**：表单字段的 tooltip / placeholder 说明填写要求。

---

## 5. 给 AI 编辑器的可判定约束

1. 新接口路径必须 `/api/v1/` 前缀，响应必须走 `response.Success/Accepted/SuccessWithList/SuccessWithPage/Error`。
2. **禁止** `c.JSON` / `c.PureJSON` / `AbortWithStatusJSON`（架构门禁直接判红）。
3. **禁止**把 `err.Error()` 透传给前端（内部细节泄漏，门禁 S-36）。
4. 「已受理、结论未定」类接口必须回 **202 Accepted**，不许用 200/409 凑。
5. 新错误先查 `response/errors.go` 有没有现成常量，不许随手 new 错误字符串。
6. 改 `request.js` 401 逻辑不许破坏「同一把令牌才清会话 + 并发只处理第一条 + 登录端点裸调」三条既有口径。
7. 新增用户可见文案必须进 i18n 10 语言，中文为基准。
8. 新增/修改路由必须同步 `router-list.md` 两份。
9. 403/404 的服务端文案不许在前端吞掉。

---

## 6. 深度参考

| 主题 | 文档 |
|---|---|
| 后端分层与响应出口 | `docs/standards/BACKEND-CODING-STANDARDS.md` |
| 五层架构最高规则 | `docs/architecture/GO_FIVE_LAYER_ARCHITECTURE.md` |
| 后端消息 i18n 策略 | `docs/standards/BACKEND_MESSAGE_I18N_POLICY.md` |
| 前端请求层写法 | `docs/standards/FRONTEND-CODING-STANDARDS.md` |
| 桥接 HTTP 三通道 | `docs/bridge/BRIDGE-ARCHITECTURE-2026-08-14.md` |
