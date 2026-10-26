# 跨端线上契约清单（`cross-repo-wire.json`）

本目录里那份清单是**生成物**，不是手写文档：

```bash
cd user-server && UPDATE_WIRE_MANIFEST=1 go test ./internal/pkg/wirecontract/   # 重新生成
cd user-server && go test ./internal/pkg/wirecontract/ -count=1                # 判据：struct ⇄ 清单
python3 user-server/scripts/check-wire-contract.py                             # 判据：本仓 ⇄ 平台仓
```

## 它挡的是什么

平台端（`hivemtk-platform/platform-server`）与租户端（本仓）是两个 git 仓、两套 Go 类型，
描述同一批**跨端传递的结构**：

| 通道 | 平台侧类型 | 租户侧类型 |
|---|---|---|
| 开户登记-注册 | `controller.RegisterReq` | `platform.RegisterMerchantReq` |
| 开户登记-安装 | `controller.InstallReportRequest` | `platform.ReportInstallReq` |
| 心跳 | `service.HeartbeatRequest` | `platform.ReportHeartbeatReq` |
| 订单-请求 | `service.CreateOrderInput` | `platform.CreateOrderInput` |
| 订单-响应 | `service.CreateOrderResult` | `platform.ModuleOrderResult` |
| 订单-条目 | `model.OrderItem` | `platform.ModuleOrderItem` |
| 订单-支付参数 | `service.EpayPayParams` | `platform.OrderPayment` |
| 模块-下发 | `service.ModuleSyncSnapshot` | `platform.ModuleSnapshot` |
| 模块-视图 | `service.MerchantModuleView` | `platform.ModuleCatalogRow` |
| 模块-选项 | `model.ModuleOption` | `platform.ModuleOptionRow` |
| 模块-订阅 | `service.SubscriptionView` | `platform.SubscriptionSnapshot` |

Go 编译器看不见另一侧，所以**改一边的 json tag，两边都能编译、两边用例都能绿**。
线上的表现是：对面的键被静默忽略，字段永远是零值 —— 不报错、不进日志，只在几个月后
有人问"为什么这个开关改了没反应"时才被翻出来。

## 三条判据

1. **类型集相等**：两侧清单的键集合必须一致。两侧的 Go 名字本来就不同（上表的左右列），
   但清单是生成物，**租户侧生成时就把嵌套类型名改写成平台侧的名字**（见
   `internal/pkg/wirecontract/manifest_test.go` 的 `tenantRenames`）。于是"谁对谁"退化成
   键相等 —— 不存在一张会漏行的映射表，也就没有"表漏一行 ⇒ 那条子树静默不被判据覆盖"。
2. **字段方向性子集**：租户发/读的每个键，平台侧必须有。方向是子集而不是相等，因为平台
   合法地比租户多（下面那张表就是全部差额，逐条有理由）。
3. **必填反向**：平台 `binding:"required"` 的键，租户必须发。漏了是**整条通道 400**，
   而且只在升级那一刻才炸。
4. **口径版本相等**：两份清单的 `version` 必须相同。两个仓各有一份 `wirecontract` 反射器
   （不共代码），一边把 `time.Time` 算成 string、另一边算成 integer 的那种分叉不会以"某个
   字段不一致"的形式出现，而会以一堆莫名其妙的一致性告警出现。

## 合法差额（子集方向的那一半）

| 类型 | 平台多出的键 | 为什么合法 |
|---|---|---|
| `CreateOrderInput` | `asset_ids` | 资产订单只在平台前端下单，租户端没有这条路径（全仓无 `asset_ids` 写入点） |
| `CreateOrderInput` | `merchant_key` | 平台从**验签后的 ctx** 取商户身份（A3/A19），租户端发什么都不作数，因此不发 |
| `ModuleOption` | `id`、`module_key`、`sort_order` | 平台库表行投影，租户只消费 `option_key`/`option_name`/`price_delta_cents`/`enabled` |
| `OrderItem` | `id`、`order_id`、`created_at` | 同上：平台库表行，租户不用主键与创建时间 |

平台的必填键（反向约束）：`RegisterReq.name`、`InstallReportRequest.install_id`、
`HeartbeatRequest.install_id`、`HeartbeatRequest.version` —— 租户侧全都发。

## 两处「看起来不一致但不该改」的地方

- **`ModuleSyncSnapshot.SyncedAt`**：平台是 `time.Time`（线上 RFC3339 字符串），租户是
  `string`。清单记的是 **wire 口径**（都算 `string`），不记 Go 类型 —— 按 Go 类型比会造出
  永久红，而永久红的判据等于没有判据。
- **`json.RawMessage` 字段**（`host_info`/`metrics`/`params`/`snapshot`）：线上形状由调用方
  决定，清单记 `any` 并跳过比较。它们仍然被"键必须在对面存在"约束着。

## 为什么不是"共一份代码"

两端是两个独立库、两条演进节奏。`ConfigParam` 这类**各自库内**的同名模型共代码并不划算
（审计 P2-6 已明确只对**跨端传递**的结构设事实源）。而且"共一份包"会逼两个仓的发布节奏
绑在一起 —— 那正是双形态（云端运营版 + 本地独立部署）最不想要的东西。所以取**形状清单**：
权威方（平台）吐形状，消费方（租户）吐形状，一条方向性规则比对。

## 这道门的边界（诚实登记）

- **单仓 CI 证不了跨仓那一半**。`.github/workflows/wire-contract.yml` 里跑的是
  `--allow-missing-platform`，它会**响亮打印**"跨仓比对被显式跳过"。发布前请在两个仓都在的
  机器上跑一次 `make wire-contract`。
- **只覆盖四条通道**（开户登记/心跳/订单/模块下发）。其它同名模型不在判据里，也不打算在。
- **`any` 与 `map[string]any` 内部不比对**：`params` / `options` 这类开放结构只能保证"键在"。

---

## 本目录下的另一份合同（手写，不参与生成与判据）

[`message-hub-recall.md`](message-hub-recall.md)：消息撤回 `POST /api/message-hub/:id/recall`
的请求/响应/幂等/错误码口径（OPEN_ITEMS B2）。它与上面这份清单是两件事：

- `cross-repo-wire.json` 管**跨仓数据结构**，生成 + 判据（改一边另一边的键静默丢失）；
- `message-hub-recall.md` 管**一条链的行为契约**（同进程 HTTP 契约 + 渠道外部 API 契约），
  手写，由 `internal/service/message_hub_recall_test.go` 等用例复算。

把撤回登记在这里的理由是它常被误读成"平台端要反向调用租户端"——事实上这条链两端
都不经过 `hivemtk-platform`，它只与渠道（TG/飞书/企微）打交道。详见该文件 §1。
