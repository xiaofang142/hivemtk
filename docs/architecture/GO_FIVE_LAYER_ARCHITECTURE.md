# Go 五层架构编码规范（最高约束）

> **版本**：v1.0（2026-08-16）
> **范围**：所有 Go 后端服务（user-server、platform-server）
> **CI 检查**：`scripts/check-architecture.sh` 静态扫描，`golangci-lint` depguard 规则阻断
> **状态**：⭐⭐⭐ 最高规则

---

## 一、为什么需要五层架构

HiveMtk 后端采用**单体应用 + 模块化**设计，不引入微服务架构。为了在单体代码库内保持代码可维护性、可测试性、可演进性，**强制五层依赖方向**，禁止跨层调用。

| 优势 | 说明 |
|------|------|
| **职责清晰** | 每层只关心自己的事，HTTP 解析、业务逻辑、数据访问严格分离 |
| **可测试性** | Service 层可独立 Mock Repository，Repository 可独立测试 SQL |
| **可演进性** | 换 ORM（GORM → sqlx）只动 Repository，换框架（Gin → Echo）只动 Controller |
| **防止腐烂** | CI 静态扫描 + depguard 规则阻断跨层引用，避免"屎山" |

---

## 二、五层定义（自上而下）

```
┌──────────────────────────────────────────┐
│ L1 Router                                │  路由注册
│   - 注册 URL → Controller 映射           │  middleware 装配
│   - 装载全局中间件（日志、TraceID、CORS） │  无业务逻辑
│   - **禁止**写业务代码 / inline 闭包      │
└─────────────────┬────────────────────────┘
                  ▼
┌──────────────────────────────────────────┐
│ L1+ Middleware                           │  HTTP 中间件
│   - 鉴权、JWT 解析、限流、TraceID、metrics│  跨切面逻辑
│   - 不依赖业务 service（注入仅在 Controller）│
└─────────────────┬────────────────────────┘
                  ▼
┌──────────────────────────────────────────┐
│ L2 Controller                            │  HTTP 接口层
│   - 参数绑定（c.ShouldBindJSON）          │  调 Service
│   - 调 Service 编排业务                   │  返回响应
│   - **禁止**写业务判断 / 直访 db / 直访   │  
│   - 外部 service（cmds、util）             │
└─────────────────┬────────────────────────┘
                  ▼
┌──────────────────────────────────────────┐
│ L3 Service                               │  业务编排层
│   - 业务逻辑、事务控制、业务校验          │  业务规则
│   - 组合多个 Repository 完成用例          │  缓存策略
│   - 跨领域服务调用                        │  异步任务触发
│   - **禁止**直访 db（*gorm.DB）            │  
│   - **禁止**返回 ORM 原始 model           │  （改用 DTO）
└─────────────────┬────────────────────────┘
                  ▼
┌──────────────────────────────────────────┐
│ L4 Repository                            │  数据访问层
│   - 封装 GORM CRUD、复杂查询              │  唯一允许使用
│   - 接收 ctx context.Context              │  *gorm.DB 的
│   - 返回 GORM model 或简单包装            │  层
│   - **禁止**业务判断 / 跨表 join 业务     │
│   - **禁止**调用其他 service              │
└─────────────────┬────────────────────────┘
                  ▼
┌──────────────────────────────────────────┐
│ L5 Model + DTO                           │  数据结构
│   Model: GORM 实体（gorm.Model 嵌入）     │  
│   - 仅含 GORM tag + 字段 + TableName 方法 │  
│   - **禁止**外部依赖 / 业务方法           │  
│   DTO: 传输对象                           │  
│   - Controller 入参/出参                  │  
│   - **禁止**反向引用 service / repository │
└──────────────────────────────────────────┘
```

---

## 三、依赖方向硬约束

### 3.1 允许的依赖

| 层 | 可依赖的层 | 可依赖的外部包 |
|---|---|---|
| Router | Middleware、Controller、pkg/* | gin、router 自身 |
| Middleware | pkg/*、model | gin、jwt、redis |
| Controller | Service、DTO、pkg/* | gin、c.ShouldBindJSON |
| Service | Repository、其他 Service（解耦）、pkg/*、model、DTO | gorm（仅事务）、redis |
| Repository | model、pkg/* | **gorm**（唯一允许）、database |
| Model | （无依赖） | gorm tag |

### 3.2 禁止的依赖（CI 阻断）

| 违规 | 典型反例 | 修复 |
|------|----------|------|
| **Controller 直访 Repository** | `c.dictRepo.FindByID(...)` | 改走 `c.dictSvc.FindByID(...)` |
| **Service 直访 db（*gorm.DB）** | `s.db.Where(...).Find(...)` | 封装为 Repository 方法 |
| **Repository 调 Service** | `r.userSvc.SendNotification(...)` | 触发业务用 Service 编排 Repos |
| **Model 含业务方法** | `func (c *Customer) Save()` | 业务逻辑放 Service |
| **DTO 反向引用 Service** | `type DTO struct { Svc *Service }` | DTO 仅含纯数据结构 |
| **跨层循环依赖** | A → B → A | 拆分为新 service 或抽象接口 |

### 3.3 横向能力包（pkg/）

| 包 | 职责 | 可被谁引用 | 实际路径 |
|---|---|---|---|
| `internal/cache` | Redis 缓存抽象（含内存/Redis 双实现） | Service、Middleware | `internal/cache/` |
| `internal/pkg/utils/logger` | zerolog 封装（唯一日志实现） | 所有层 | `internal/pkg/utils/logger/` |
| `internal/pkg/metrics` | 轻量级指标采集（应用层巡检用） | Middleware、Service | `internal/pkg/metrics/` |
| `internal/pkg/trace` | TraceID 注入（与 traceparent 配套） | Middleware、Service | `internal/pkg/trace/` |
| `internal/pkg/utils/urlguard` | URL 校验（含 SSRF 防护） | Controller、Service | `internal/pkg/utils/urlguard/` |
| `internal/pkg/tgbot` | Telegram bot SDK 封装 | 仅 Middleware / Service | `internal/pkg/tgbot/` |

横向包**不依赖**任何业务层（Controller/Service/Repository），保证低耦合。

---

## 四、命名与文件组织规范

### 4.1 目录约定

```
hivemtk/user-server/
├── cmd/
│   └── api/
│       └── main.go              # 进程入口（L1 Router 装配）
├── internal/
│   ├── controller/              # L2 Controller（HTTP 接口层）
│   ├── service/                 # L3 Service
│   ├── repository/              # L4 Repository
│   ├── model/                   # L5 Model（GORM 实体）
│   ├── dto/                     # L5 DTO
│   ├── middleware/              # L1+ Middleware
│   ├── pkg/                     # 横向能力包
│   ├── integration/             # 集成（独立子模块）
│   ├── migration/               # 数据库迁移
│   └── app/                     # 应用层（启动配置、DI 装配）
└── scripts/
    └── check-architecture.sh    # 架构合规 CI 检查
```

### 4.2 文件命名禁用清单

| 后缀 | 禁用原因 | 替代方案 |
|------|----------|----------|
| `_utils` | 散乱难定位 | 归入 `pkg/utils/` |
| `_common` | 同上 | 归入 `pkg/common/` |
| `_v1` / `_v2` | API 版本控制应在 URL 层 | 路由 `/api/v1/` 区分 |
| `_stub` / `_mock` | 测试代码应在 `_test.go` | `xxx_test.go` |
| `_2026-*` | 时间戳命名易过期 | 用语义命名 |

### 4.3 函数命名

| 层 | 函数名风格 | 示例 |
|---|---|---|
| Controller | `Xxx`（动词+名词，**不加 `Handler` 后缀**） | `CreateCustomer` |
| Service | `Xxx` / `XxxService` | `CreateCustomer` |
| Repository | `XxxByXxx` / `FindXxx` | `FindByPhone` |
| Model | （仅 GORM hook + TableName）| `TableName()` |
| DTO | `XxxRequest` / `XxxResponse` | `CreateCustomerRequest` |

> 实测依据：`internal/controller/` 下 165 个控制器类型全为 `XxxController`、969 个方法全为「动词+名词」
> （`ListCustomers` / `CreateCustomer` / `MergeCustomers`）。仓库**不存在 `internal/handler/` 包**，
> `XxxHandler` 类型 0 命中；唯一含 Handler 字样的是 `CustomerSessionController.SwitchHandler`，语义为"转接坐席"，非命名约定。

---

## 五、CI 静态检查脚本

`scripts/check-architecture.sh` 阻断式检查：

| 检查项 | 阻断级别 | 说明 |
|--------|----------|------|
| Controller → Repository 直访 | ❌ 阻断 | 强制走 Service |
| Service → *gorm.DB 直访 | ❌ 阻断 | 必须封装 Repository |
| Repository → Service 调用 | ❌ 阻断 | 禁止业务渗入数据层 |
| 跨层循环依赖 | ❌ 阻断 | `goimports` + 静态扫描 |
| 文件命名禁用后缀 | ⚠️ 警告 | 强烈建议整改 |
| Middleware → Service 直访 | ❌ 阻断 | Middleware 仅做横切面 |

**违规处理**：CI 阻断合并，必须修复后重新提交。

---

## 六、典型反例与正例

### ❌ 反例 1：Controller 直访 Repository

```go
// controller/customer.go
func (c *CustomerController) GetByID(ctx *gin.Context) {
    id := ctx.Param("id")
    var customer model.Customer
    c.db.Where("id = ?", id).First(&customer)  // ❌ 直访 db
    ctx.JSON(200, customer)
}
```

### ✅ 正例 1：Controller → Service → Repository

```go
// controller/customer.go
func (c *CustomerController) GetByID(ctx *gin.Context) {
    id := ctx.Param("id")
    customer, err := c.customerSvc.FindByID(ctx, id)  // ✅ 走 Service
    if err != nil { /* error handling */ }
    ctx.JSON(200, dto.ToCustomerResponse(customer))
}

// service/customer.go
func (s *CustomerService) FindByID(ctx context.Context, id string) (*model.Customer, error) {
    return s.repo.FindByID(ctx, id)  // ✅ Service 调 Repository
}

// repository/customer.go
func (r *CustomerRepository) FindByID(ctx context.Context, id string) (*model.Customer, error) {
    var customer model.Customer
    err := r.db.WithContext(ctx).Where("id = ?", id).First(&customer).Error
    return &customer, err
}
```

### ❌ 反例 2：Model 含业务方法

```go
// model/customer.go
func (c *Customer) SendNotification() error {
    // ❌ Model 包含业务方法，违反 L5 职责
    return notification.Send(c.Phone, "Hi")
}
```

### ✅ 正例 2：业务逻辑放 Service

```go
// model/customer.go
type Customer struct {
    gorm.Model
    Phone string
    // 仅有字段 + GORM tag，无业务方法
}

// service/customer.go
func (s *CustomerService) SendGreeting(ctx context.Context, customerID uint) error {
    customer, err := s.repo.FindByID(ctx, customerID)
    if err != nil { return err }
    return s.notifier.Send(customer.Phone, "Hi")  // ✅ 业务在 Service
}
```

---

## 七、与 ADR 的关系

| ADR | 与本规范的关系 |
|-----|----------------|
| ADR-001 | **本规范**（五层架构的 ADR 化版本） |
| ADR-002 | AGPL-3.0 许可证（与代码组织无关） |
| ADR-005 | 数据库设计（Repository 层遵循的 schema 约定） |
| ADR-009 | 错误处理（Service 层错误包装与码映射） |
| ADR-012 | 配置包迁移（pkg/config 归属横向能力） |
| ADR-013 | 模块重命名（controller/service/repository 命名规范） |

---

## 八、修订历史

| 版本 | 日期 | 修订人 | 内容 |
|------|------|--------|------|
| v1.0 | 2026-08-16 | @maintainer-team | 初版五层架构编码规范（合并散落文档） |
