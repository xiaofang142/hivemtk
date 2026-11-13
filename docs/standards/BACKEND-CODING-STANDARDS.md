# 后端编码规范（Go / user-server）

> 权威源：`docs/architecture/GO_FIVE_LAYER_ARCHITECTURE.md`（⭐⭐⭐最高规则）+ 可执行门禁
> `scripts/check-architecture.sh`（10 组检查，违例阻断合并）。
> 本文件是「写代码前要知道什么」的浓缩版，**每条规则都标注了实证来源**；
> 与本文件冲突时以架构文档与门禁脚本为准，并回来修订本文件。
>
> 触发条件：任何改动落在 `user-server/**`（含 `cmd/`、`internal/`）之前必须读本文件。

## 1. 分层与目录（不可协商）

```
cmd/api/main.go          # L1 装配：中间件装载 + 路由注册 + 依赖注入
internal/router/         # L1  路由表（112 文件）——只做 URL → Controller 映射
internal/middleware/     # L1+ 鉴权 / JWT / 限流 / TraceID / metrics
internal/controller/     # L2  Controller（324 文件）
internal/service/        # L3  Service（893 文件）
internal/repository/     # L4  Repository（394 文件）
internal/model/          # L5  Model（258 文件）
internal/dto/            # L5  DTO
internal/pkg/            # 横向能力包（logger/metrics/trace/response…），不依赖任何业务层
```

**实证事实（2026-11-13 清点）**：本仓**没有 `internal/handler/` 包**，`XxxHandler` 命名 0 命中；
165 个控制器类型全叫 `XxxController`。写 `handler.go` / `XxxHandler` 就是错的。

### 1.1 各层职责与禁止（来自架构文档 §二 + 门禁 [1/10]~[4/10]）

| 层 | 必须做 | 禁止（CI 阻断） |
|---|---|---|
| Router | URL→Controller 映射；装载全局中间件 | 业务代码；**内联闭包处理器** `func(x *gin.Context)`（非 `_test.go`） |
| Middleware | 鉴权/限流/TraceID；不依赖业务 service | 持有业务 service 做判断 |
| Controller | 参数绑定 `c.ShouldBindJSON`；调 Service 编排；返回响应 | ① import repository ② `db.GetDB()` ③ import `gorm.io/gorm` ④ 直接调 `c.JSON`/`AbortWithStatusJSON`/`PureJSON`/… ⑤ 错误文案夹带 `err.Error()` ⑥ 业务判断 |
| Service | 业务逻辑/事务/校验；组合多个 Repo；跨领域调用；异步触发 | ① 引用 controller/router ② 直连 db（`db.GetDB()` / `x.db.Raw/Exec/Create/…`）③ SQL 字符串拼接 `.Raw(.*+.*)` ④ 返回 ORM 原始 model（应返回 DTO） |
| Repository | 封装 GORM CRUD；**唯一允许用 `*gorm.DB` 的层**；返回 model | ① 引用 service ② 引用 dto ③ 业务判断 ④ 跨表 join 业务 ⑤ 导出方法首参缺 `ctx context.Context` |
| Model | gorm tag + 字段 + `TableName` | 任何业务方法（只允许 GORM Hook：`TableName`/`Before*`/`After*`/`Value`/`Scan`） |
| DTO | Controller 入参出参 | 引用 service/repository；调 db；业务方法 |

### 1.2 命名规范（架构文档 §四 + 门禁 [6/10]~[8/10]）

- **文件命名**：统一 `<domain>.go`。**禁止** `*_v[0-9]*.go` / `*_stub*.go` / `*_ext*.go` /
  `*_2026-*.go` / `*_new*.go` / `*_old*.go` / `*_bak*.go` / `*_copy*.go`；
  **禁止冗余后缀** `*_controller.go` / `*_service.go` / `*_repository.go` / `*_dto.go`
  （后缀与包名重复）；WARN `utils.go` / `common.go` / `helpers.go`。
- **类型命名**：Controller `XxxController`（**不加 Handler 后缀**）；Service 导出 interface
  `XxxService` + 小写 struct `xxxService` 实现（**不用 I 前缀**）；Repository 导出 interface
  `XxxRepository` + 小写 struct；DTO `XxxRequest` / `XxxResponse`。
  （门禁 [7/10]/[8/10]：有 struct 就必须有对应导出 interface，缺了判 WARN。）
- **方法命名**：Service `Xxx`（动词+名词）；Repository `FindXxx` / `XxxByXxx` / `CreateXxx`。
- **测试文件**：`<domain>_test.go`（禁止把源文件误命名成 `_test.go`）。

### 1.3 Controller 样板（`internal/controller/bridge_extension.go` 实证）

```go
// 包注释用中文说明「本 controller 只回答什么问题」
type XxxController struct { svc *service.XxxService }        // 私有字段 + 构造注入
func NewXxxController(svc *service.XxxService) *XxxController { ... }
func (c *XxxController) RegisterRoutes(auth *gin.RouterGroup) { ... }  // 挂路由

// handler 方法注释必须写状态码口径（200 / 404 / 503 各自语义与文案差异）
// @Summary 中文标题
// @Tags Xxx
// @Router /api/xxx [get]
func (c *XxxController) GetXxx(ctx *gin.Context) {
    pkg, err := c.svc.Xxx(ctx.Request.Context(), id)
    if err != nil {
        if errors.Is(err, service.ErrXxxNotFound) {
            response.Error(ctx, http.StatusNotFound, "扩展包不存在")  // 中文文案
            return
        }
        response.Error(ctx, http.StatusInternalServerError, errors.ErrSystemError)
        return
    }
    response.Success(ctx, pkg, "ok")
}
```

**铁律：Controller 一律禁止 `c.JSON`**（门禁注释明确指向 check-architecture.sh 的 [L3] 条）。
所有响应必须走 `internal/pkg/utils/response` 出口。

### 1.4 响应与错误（`internal/pkg/utils/response/response.go` + `errors.go` 实证）

| 函数 | 语义 |
|---|---|
| `Success(c, data, msg)` | 200 `{code:0, message: i18n.Localize(localeOf(c), msg), data}` |
| `Accepted(c, data, msg)` | 202 同构。**适用场景**：报价发送等「已受理、结论未定」——回 200 会被渲染成已发送、回 409 会被读成被拒，两种都错 |
| `SuccessWithList(c, data, total)` | `data:{list,total}` |
| `SuccessWithPage(c, data, page, size, total)` | `data:{list,total,page,page_size}` |
| `Error(c, code, msg, data...)` | 统一错误出口；内部按 `internalDetailMarkers` 剥离内部细节，兜底文案 `"操作失败，请稍后重试"`，`leakWindowRunes=16`（rune 滑窗防中文被字节切断漏判） |

⇒ **错误响应禁止把内部错误原文透传前端**。中文错误消息常量集中在 `errors.go`
（`ErrInvalidParams`="无效的请求参数" / `ErrResourceNotFound`="资源不存在" /
`ErrUnauthorized` / `ErrPermissionDenied` / `ErrIDMismatch` …），**新文案加到这里，不要散着写**。

### 1.5 多租户与上下文

- 一切租户相关读取走 `TenantCodeFromContext(ctx)` 兜底；**空租户 fail-closed**
  （实证：0b6007e3 selector-health 空租户 400 替代 500）。
- Repository 导出方法首参必须 `ctx context.Context`（门禁 [9/10]；豁免纯内存访问器
  `Available`/`IsNil`/`GetDB`/`SetDB`/`DialectName` 与 `Error`/`Is`/`Unwrap` 与谓词 `Bound`/`Connected`/`DBReady`）。

### 1.6 并发与数据访问（两道专项门禁）

- **禁止裸 `go func(`**（`user-server/scripts/check_naked_goroutine.sh`）：动机=最高标准审计发现
  55/76 处 `go func` 无 recover，任一 panic 击穿 `gin.Recovery` 击垮整个进程。
  统一收口 `utils.SafeGo` / `SafeGoDetached` / `SafeGoWithRetry`。
- **数据访问必须走 GORM API**（`user-server/scripts/check-gorm-only.sh`）：禁 `.Exec(`/`.Raw(` 直连。
  豁免清单（每项都写了理由）：`internal/migration/`、`internal/pkg/db/migrate.go`、
  `tenant_id_normalize.go`、`internal/pkg/tenancyidx/`、`internal/pkg/testutil/`、
  `pgvector_*.go`、`moduleidx.go`、`*_test.go`。**新增豁免必须写理由，不能静默加白名单。**

### 1.7 写完后必须跑

```bash
cd <仓库根>/hivemtk   # 嵌套仓库，真实代码在内层
bash scripts/check-architecture.sh        # 10 组架构门禁，违例即红
bash user-server/scripts/check_naked_goroutine.sh
bash user-server/scripts/check-gorm-only.sh
cd user-server && go build ./... && go vet ./... && go test ./internal/service/ ./internal/bridge/...
```

> `go test` 报 `test password is missing; set POSTGRES_TEST_PASSWORD or POSTGRES_PASSWORD`
> 是**环境缺变量触发测试库安全门 fail-closed**，不是代码问题（未发起任何连接）。

## 2. 约束（给 AI 编辑器的可判定条目）

1. 新增文件前先想清楚落在哪一层；**不要新建 `internal/handler/`**。
2. Controller 不 import repository、不 import gorm、不调 `c.JSON`。
3. Service 不碰 `*gorm.DB`、不拼 SQL；需要新查询就下到 Repository。
4. Repository 方法第一个参数是 `ctx`，返回 model 不返回 DTO。
5. Model 只放字段 + `TableName` + Hook；业务逻辑一律上 Service。
6. 错误走 `response.Error` + `errors.go` 常量，不把 `err.Error()` 写给前端。
7. 并发用 `SafeGo` 三兄弟，不写裸 `go func`。
8. 文件命名不带 `_v1`/`_new`/`_bak`/`_controller` 等后缀。
9. 改完必须跑 §1.7 四条命令，全绿才算完成。
