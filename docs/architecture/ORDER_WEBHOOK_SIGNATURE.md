# 订单回调签名协议（order-webhook）

> 适用范围：外部电商平台向本系统推送订单状态变更。
> 对应实现：`user-server/internal/middleware/webhook_signature.go`（验签）、
> `user-server/internal/router/business_routes.go`（注册）、
> `user-server/internal/controller/integration.go`（落库入口 + 错误分档）、
> `user-server/internal/service/payment_webhook.go`（载荷里那一笔钱的解析，§8）。
> 来源：R-3 / T-P2-02；§8 那一份钱契约为 T-P7-02 新增。历史背景见 `docs/replan-2026-09/本项目调研.md` 的 V-06 条目。

## 1. 为什么要换契约

旧端点 `POST /api/integration/order-webhook/:platform` 挂在**会话鉴权组**下：调用方必须先
在本系统登录、拿到一个 JWT 才能推订单。外部电商平台不会替我们保管会话凭证，实践中只有
"给它开一个长期账号"这种更糟的对接法。对外回调的惯例是**公开路径 + 每平台共享密钥的
HMAC 签名**，本系统据此提供新端点，旧端点保留一个大版本。

## 2. 端点

| 项 | 新契约（推荐） | 旧契约（deprecation） |
|----|----------------|----------------------|
| 方法 + 路径 | `POST /api/integration/webhook/order/{platform}` | `POST /api/integration/order-webhook/{platform}` |
| 鉴权方式 | HMAC-SHA256 签名（本节§3、§4） | 会话 JWT（登录态） |
| `{platform}` | `^[a-z0-9][a-z0-9_-]{0,31}$`（1–32 位） | 同 |
| 请求体 | `{"order_id":"...", "status":"...", ...}`，`order_id` 必填；可选带一格 `payment`（§8） | 同 |
| 体积上限 | 256 KiB（`ORDER_WEBHOOK_MAX_BODY_BYTES` 可调，1 KiB–4 MiB） | 全局 10 MiB |

两条路径进的是**同一个处理函数**（`IntegrationController.ReceiveOrderWebhook`）：
差异只在鉴权与那三个响应头，载荷语义（含§8 那一笔钱）完全一致。

旧路径每次命中都回 `Deprecation: true` 与
`Link: </api/integration/webhook/order/{platform}>; rel="successor-version"`，
并按 60s 限速记一行 WARN 日志（含平台、来源 IP、UA）——**那条日志就是旧路径下线前的调用方名单**。

## 3. 请求头

| 头 | 必填 | 含义 |
|----|------|------|
| `X-Webhook-Signature` | 是 | 签名串。裸 hex 或 `sha256=<hex>` 前缀写法都接受，大小写不敏感 |
| `X-Webhook-Timestamp` | 是 | **Unix 秒**（10 位十进制）。不是毫秒、不是 RFC3339 |
| `X-Webhook-Nonce` | 是 | 一次性随机串，`^[A-Za-z0-9_-]{8,64}$`。每条回调都要换新的 |

缺任一头 → 400 并点名缺哪个；格式不对 → 400。

## 4. 签名算法

```
canonical = platform + "\n" + timestamp + "\n" + nonce + "\n" + raw_body
signature = hex( HMAC_SHA256( secret, canonical ) )
```

- `platform` 取路径参数原值（如 `taobao`），**参与签名**：不带它，A 平台的一份合法报文可以
  原封不动地推成 B 平台的订单（两家共用同一密钥时无人拦得住）。
- `raw_body` 是**未解析的原始字节**，与发送的字节完全一致（不要重新序列化 JSON；
  键序、空格、转义任何差异都会改变签名）。
- `timestamp`/`nonce` 用请求头里的原值（已去首尾空白）。

## 5. 密钥配置

每平台一条密钥，存 `system_config_kv`（运行时可改，不需重启，不进代码仓库）：

| 键 | 用途 |
|----|------|
| `order_webhook_secret_<platform>` | 当前密钥 |
| `order_webhook_secret_<platform>_prev` | 轮换灰度位；签名比对时与当前密钥**并列**尝试 |

轮换姿势：把旧值挪到 `_prev`、新值写进主键 ⇒ 过渡期两套签名都收，对接方切完再删 `_prev`。
主键为空/缺行时会退而接受 `_prev`（"半配置"状态不该把对接方打死）。

**没有密钥就没有可信回调**：某平台未配置密钥时，它的回调一律 503（fail-closed）。
新端点**不存在"先公开、后验签"的中间态**，也没有测试模式豁免——在签名之前把路由公开出去，
本身就是本次要修的那个洞。灰度发生在对接方一侧：谁准备好了谁切路径。

## 6. 时效与重放

- 时间戳窗口默认 ±300s（`ORDER_WEBHOOK_MAX_SKEW_SECONDS`，10s–15min，越界按默认并 WARN）。
  超前与滞后**同权重**判定（对端时钟快不得也一样被拒），出窗 → 401。
- 窗口之内才验签，验签通过才登记 nonce；`nonce` 用 `SetNX` 写入缓存，
  TTL = **2×窗口**（所以任何一条 nonce 被逐出时，它的时间戳必然已出窗，不留空隙）。
  窗口内重复出现 → 409。
- 缓存故障时**默认 fail-open**（签名与时间戳都已验证，最坏是同一订单状态被重复 upsert 一次），
  并计数 + 限速 WARN；把 `ORDER_WEBHOOK_NONCE_STRICT=on` 打开则改为 fail-closed（拒绝）。
  多副本部署要共享 nonce，需要 Redis 后端；仅内存缓存时每个进程各自记一份。

## 7. 响应码

分档只有一把尺子：**渠道这一侧该怎么办**。4xx ⇒ 重投同一份载荷永远修不好；5xx ⇒ 我们这边的事，
重投正是想要的。

| 码 | 含义 | 谁的问题 |
|----|------|---------|
| 200 | 已接收并处理（`data` 回显 platform/order_id/status，带钱时另有两格，见§8.6） | — |
| 400 | 缺头、nonce/时间戳格式错、`platform` 非法、`order_id` 为空、体解析失败；**或 `payment` 格子里的内容不合法**（不认识的字段、`bill_id`/`channel_ref` 空、金额/币种/状态/时间形状不对） | 对接方 |
| 401 | 签名不符、时间戳出窗 | 对接方 |
| 403 | 请求到了公开入口却没有验签凭据（路由装配出错，防御性核对） | 本侧 |
| 404 | `payment.bill_id` 指向的应收不存在 | 对接方（它推了一张我们没开过的单） |
| 409 | 两种来源：**`nonce` 重复**（重放）；或载荷没坏而我们库里的数据对不上（应收已作废、同一 `channel_ref` 被拿去记另一笔钱、币种与账单不符、对已冲销那笔普通重投、冲一笔从没到账过的钱） | 前者是对接方，后者要两边一起看数据 |
| 413 | 体积越界 | 对接方 |
| 500 | 事件留痕写失败、订单镜像读/写失败、`ErrPaymentStatusStuck`（钱已入账而账单状态没折算成）—— 最后这一条**必须人工看那一笔**，绝不能被压成 4xx 让渠道以为不用重投了 | 本侧 |
| 503 | 该平台未配密钥 / 密钥读不到；**或回款腿未装配**（`payment` 格子进不去，见§8.3） | **本侧**，回 401 会让对方去改自己没错的代码 |

失败响应不回显"期望签名"，也不泄露密钥；但会说清缺什么、偏差多少秒。
带钱那一腿的错误文案里固定写着两件事：**钱记没记上、镜像动没动** —— 后者决定重推是否安全。

## 8. `payment` 格子：一条回调里可以带着一笔钱（T-P7-02）

对应实现：`user-server/internal/service/payment_webhook.go`（解析）、
`user-server/internal/service/payment.go`（入账与结算）、
`user-server/internal/controller/integration.go` 的 `orderWebhookFailure`（分档成 §7）。

### 8.1 为什么钱走订单回调

订单状态本来就是渠道推送的（`paid` 那一格就是它先说的）。回款如果另开一条通道，就有两个入口
在写"这单付了没有"，而两边的到达顺序不由我们决定。所以钱是订单载荷里的**一个可选对象**：
没有它 = §7 的旧形状一切照旧（这条判据由用例钉着，T-P7-02 不许把存量对接打挂）。

### 8.2 字段（白名单，多一格就报错）

| 字段 | 必填 | 含义 |
|------|------|------|
| `bill_id` | 是 | 这笔钱冲的是哪张应收（`bills.id`，由报价发送时带给渠道） |
| `channel_ref` | 是 | 渠道流水号，**本层唯一的幂等键** |
| `amount` | 是 | 正数，且 ≤ 9999999999.99（`numeric(14,2)` 量程）；符号不许进金额列，方向由 `status` 表达 |
| `currency` | 否 | ISO 4217 三位码；不给就随账单币种，给了就必须与账单一致 |
| `paid_at` | 否 | 到账时间。接受 RFC3339 / `2006-01-02 15:04:05` / `2006-01-02T15:04:05` / `2006-01-02`；不给则填接收时刻（并在响应里承认是填的） |
| `status` | 否 | 只接受 `confirmed`（缺省即此）与 `reversed` 两个词 |

**不认识的字段一律 400**，而不是"认识的就取、其余忽略"：多出来一格说明渠道在按另一份契约推送，
而那多半是同一笔钱的另一种写法（例如 `bill_amount` 与 `amount` 并存且不相等）。
忽略它 = 按半份契约把钱记进去，而这一格记的是钱。

载荷里**没有** `platform` / `order_id` 的位置：来路两格取的是回调路径上的 `:platform` 与
顶层 `order_id`（与订单镜像同一口径 —— 载荷自述"我是哪一单"不作数）。

### 8.3 幂等、冲销，以及"钱进不去"的四种说法

`channel_ref` 是幂等键：同一个流水号再次到达，不是"再记一笔"，而是"这一笔的又一次通知"。

| 情形 | 结果 |
|------|------|
| 新流水号 + `confirmed` | 记一行 `confirmed`，随后把那张应收折算一次（§8.5） |
| 同流水号、且 `bill_id`/`amount`/`currency` 三格**都相同** | 不写任何行，`reused=true`，账单再折算一次（顺带修好上次折算失败留下的缺口） |
| 同流水号、三格任一不同 | **409**（`ErrPaymentRefReuse`）—— 渠道在拿旧流水号发新钱，那是另一笔钱，该给一个新 `channel_ref` |
| 同流水号 + `status=reversed` | 把那一行改成 `reversed`（只写 status），应收重新折算 —— 账单可能从 `paid` 退回 `partial`/`open` |
| 新流水号 + `status=reversed` | **409**（`ErrPaymentNothingToReverse`）：不"顺手补一行 reversed"，那会造出一笔从没到账过的钱 |
| 已 `reversed` 的那一行又来一次普通推送 | **409**（`ErrPaymentAlreadyReversed`）：冲掉的格子不许被重投复活，那一格钱在渠道那边并不存在 |
| 回款腿未装配（这台服务没接回款） | **503**，装配修好后重推同一份载荷即可补上 |
| 应收不存在 / 已作废 / 币种不符 | **404** / **409** / **409** |

两条共同点：**都不静默少记**。少记一笔钱的代价是"应收挂着、客户说付过了"，那种账只能在
报错的当天查。三条分支都保留订单镜像已写入的事实，重推只补回款这一腿。

### 8.4 形状细节

- `amount` 接受 JSON 数字、各宽度整数、以及**十进制字符串**（`"369.99"` 是渠道常态）；
  文本要么完整认下、要么明确报错，不许"看起来认了、其实丢了小数"。`NaN`/`Inf`/`1e999` 在解析层就出局。
- 金额落库前统一按两位小数（`money2`）取整，与 `numeric(14,2)` 同口径。
- `paid_at` 缺省填接收时刻，响应里的 `payment.filled_paid_at=true` 就是这件事的存证 ——
  运营按 `paid_at` 排账龄，这一格不是渠道说的就必须能看出来。

### 8.5 账单状态怎么被折算

结清金额 = 该应收名下**计入结清的那些状态**之和（今天只有 `confirmed`），求和发生在 SQL 侧，
全系统只有这一处做这笔算术。折算结果：`settled ≥ amount` ⇒ `paid`；`settled > 0` ⇒ `partial`；
否则 ⇒ `open`。比较用半分（0.005）而不是裸 `==`：一分的缺口既不该读成"收清了"，
也不该把"还差一分"读成"已结清"。

- 收的比主张的多 ⇒ **不拒**，账单照常 `paid`，响应里 `oversettled=true` 标出来（多收的钱是真收到的）。
- `bills` 上**没有**"已收金额"这一列：那是 `payments` 求和读出来的事实，存一份就有第二套答案。
- 跃迁表里没有那条边时（库里状态与名下钱对不上）报一句要人看的话并**不写库**，回 500。

### 8.6 响应里的两格

```json
{"code":0,"data":{"platform":"taobao","order_id":"...","status":"paid",
  "payment_present":true,
  "payment":{"payment":{"id":"...","bill_id":"...","amount":369.99,"currency":"CNY",
                         "paid_at":"...","channel_ref":"...","status":"confirmed",
                         "platform":"taobao","order_id":"..."},
             "reused":false,
             "settlement":{"bill_id":"...","status":"partial","amount":500.00,
                           "settled":369.99,"outstanding":130.01,
                           "oversettled":false,"transited":true}}}}
```

- `payment_present` 区分"这条回调压根不带钱"（旧载荷，正常）与"带着钱但没进账"（要看的一句话）；
  没有它两者会被压成同一个 200。
- `payment.reused` 与 `settlement.transited` 一起给，渠道才能把"重复通知"与"这一笔把账单结掉了"分开。

### 8.7 本卡**不**具备的（勿当作已具备）

- **部分退款**：两种写法都被拒 —— 用新流水号推 `reversed` 是 409（那笔钱从没到账过），
  用原流水号推一个更小的金额也是 409（同一个键对应了另一笔钱）。今天只有"整笔冲销"这一档。
  要支持部分退款，得让它成为**一行新记录**（退款行自己带 `channel_ref` 与方向），
  而不是改那一格的金额 —— 排在这条链有真实退款单之后。
- **逾期与催收**：`bills.due_at` 那一列在，但派生腿永远写 `NULL`（今天没有任何一处定义过付款条件），
  对账视图里的 `due_at` 因此恒 `null`；逾期扫描与催收收口是 T-P7-03。
- **回款的读侧入口**只有账单那两条 GET（`/api/bill/:bill_id`、`/api/bill/of-quote/:quote_id`）：
  没有"按渠道流水号查这笔钱"的端点，渠道侧要问一笔钱，走它自己那个流水号对应的账单。

## 9. 已知边界（勿当作已具备）

**本节第一条在 T-P7-02 已被推翻，逐条写明现在的形状**（原文三条"仍然"全部失效，留在这里是为了
让读过旧版的人不必重新猜）：

- ~~`webhook_events` 每次新建（`EventID` 含 `UnixNano`，不去重）~~ ⇒ `EventID` 改由**内容**派生
  （`orderWebhookEventKey`：平台 + 订单号 + 状态 + 载荷摘要）。同一件事重复推送落一行，
  状态或载荷变了才落第二行。**这个键只用于留痕，绝不当处理的闸门** —— 拿它当闸门会造出一种更坏的坏法：
  第一次把镜像写坏了、渠道重推时被"这个事件我见过了"挡在门外，那一单永远修不好。
- ~~创建错误被丢弃（`_ =`）~~ ⇒ 留痕写失败**中止整条回调**（回 500，文案写着"镜像一行都没改，重推可修"）。
- ~~对订单状态无回退保护~~ ⇒ `model.ExternalOrderStatusRegresses` 拦住"迟到的 `created` 把已付的擦回待付"。
  两侧任一**不认识**那个状态词时一律放行（状态词由平台给，把不认识的值冻成"不许覆盖"会让镜像
  永远停在第一条收到的状态上，那比偶尔被回退一次更坏）。
- 服务层的幂等**只在回款这一腿**：判据是 `uq_payments_channel_ref`，作用域是**全表**而不是那张账单 ——
  同一个流水号被拿去记第二笔钱，会撞上"这是另一笔钱"那条 409（§8.3），而不是各记各的。
- 钱进不进来取决于渠道**真的推 `payment`**：本系统不会去渠道侧捞账，也没有补录端点。
  一条从未带过钱的回调，事后只能由渠道重推补上（§8.3 那两支幂等通路正是为此留的）。
- `external_orders` 上那把**旧的全局唯一键** `uni_external_orders_order_id` 的删除属存量迁移
  （`AutoMigrate` 只加不删）。启动钩子失败时**只 WARN 不 panic**：那台实例上"不同平台同 `order_id`"
  仍会被旧键挡下，而新代码期望的是 `(platform, order_id)` 复合唯一 —— 这一格的取舍是"存量缺陷没修完"
  换"整站起不来"，所以它写成决定、也判成了决定。判据与形状见
  `user-server/internal/pkg/db/external_order_key_startup_test.go` 三条：旧索引真被摘掉且复跑不动数据、
  `AutoMigrate` 里那句调用已接线且排在其余 post-migrate 之后（`TestAutoMigrateWiresExternalOrderLegacyKeyDrop`）、
  失败处置不 panic（`TestLegacyExternalOrderKeyHookFailsLoudlyNotFatally`，两臂 `recover`；
  它的牙由 `scripts/mut_startup_hook_p702.py` 两刀证明）。
- 旧路径的 `Sunset` 日期**故意不写**：下线时点取决于"还有谁在用"，那份名单正由§2 的限速日志收集。

## 10. 对接自检清单

1. 平台密钥是否已在 `system_config_kv` 配好（键名见§5）？没有 → 全部 503。
2. 时间戳是 Unix **秒**、且与标准时间源同步（NTP）？偏差 > 300s → 401。
3. 每条回调的 nonce 是否唯一？重复 → 409（重放会被当作攻击）。
4. 签名用的是**发送前的原始字节**，不是重新序列化后的 JSON？
5. 路径里的 `{platform}` 与签名里的 platform 是否同一个值（大小写敏感）？
6. 带钱那格时：`channel_ref` 用的是**渠道自己那一笔的流水号**、而不是"这张账单的号"？
   拿账单号当流水号，同一张单的第二笔回款会被当成第一笔的重投 —— 金额相同就**静默吞掉**（`reused=true`），
   金额不同就撞 409（§8.3）。两种都是把钱记错，而前一种不会有任何报错。
7. 退款按**整笔冲销**推（同 `channel_ref` + `status=reversed`）？部分退款今天会被拒（§8.7）。
8. `bill_id` 用的是报价发送时我们给出去的那个号？自造一份 → 404，且钱不会进任何一张单的账。
