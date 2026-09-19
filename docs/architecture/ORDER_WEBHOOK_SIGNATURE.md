# 订单回调签名协议（order-webhook）

> 适用范围：外部电商平台向本系统推送订单状态变更。
> 对应实现：`user-server/internal/middleware/webhook_signature.go`（验签）、
> `user-server/internal/router/business_routes.go`（注册）、
> `user-server/internal/controller/integration.go`（落库入口）。
> 来源：R-3 / T-P2-02。历史背景见 `docs/replan-2026-09/本项目调研.md` 的 V-06 条目。

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
| 请求体 | `{"order_id":"...", "status":"...", ...}`，`order_id` 必填 | 同 |
| 体积上限 | 256 KiB（`ORDER_WEBHOOK_MAX_BODY_BYTES` 可调，1 KiB–4 MiB） | 全局 10 MiB |

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

* `platform` 取路径参数原值（如 `taobao`），**参与签名**：不带它，A 平台的一份合法报文可以
  原封不动地推成 B 平台的订单（两家共用同一密钥时无人拦得住）。
* `raw_body` 是**未解析的原始字节**，与发送的字节完全一致（不要重新序列化 JSON；
  键序、空格、转义任何差异都会改变签名）。
* `timestamp`/`nonce` 用请求头里的原值（已去首尾空白）。

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

* 时间戳窗口默认 ±300s（`ORDER_WEBHOOK_MAX_SKEW_SECONDS`，10s–15min，越界按默认并 WARN）。
  超前与滞后**同权重**判定（对端时钟快不得也一样被拒），出窗 → 401。
* 窗口之内才验签，验签通过才登记 nonce；`nonce` 用 `SetNX` 写入缓存，
  TTL = **2×窗口**（所以任何一条 nonce 被逐出时，它的时间戳必然已出窗，不留空隙）。
  窗口内重复出现 → 409。
* 缓存故障时**默认 fail-open**（签名与时间戳都已验证，最坏是同一订单状态被重复 upsert 一次），
  并计数 + 限速 WARN；把 `ORDER_WEBHOOK_NONCE_STRICT=on` 打开则改为 fail-closed（拒绝）。
  多副本部署要共享 nonce，需要 Redis 后端；仅内存缓存时每个进程各自记一份。

## 7. 响应码

| 码 | 含义 | 谁的问题 |
|----|------|---------|
| 200 | 已接收并处理（`data` 回显 platform/order_id/status） | — |
| 400 | 缺头、nonce/时间戳格式错、`platform` 非法、`order_id` 为空、体解析失败 | 对接方 |
| 401 | 签名不符、时间戳出窗 | 对接方 |
| 403 | 请求到了公开入口却没有验签凭据（路由装配出错，防御性核对） | 本侧 |
| 409 | nonce 重复（重放） | 对接方（或它在重试） |
| 413 | 体积越界 | 对接方 |
| 503 | 该平台未配密钥 / 密钥读不到 | **本侧**，回 401 会让对方去改自己没错的代码 |

失败响应不回显"期望签名"，也不泄露密钥；但会说清缺什么、偏差多少秒。

## 8. 已知边界（本次未修，勿当作已具备）

* 回调入库路径 `IntegrationService.UpsertOrderFromWebhook` 仍然：`webhook_events` 每次新建
  （`EventID` 含 `UnixNano`，**不去重**）、创建错误被丢弃（`_ =`）、对订单状态无回退保护。
  边缘的 nonce/签名校验挡住了重复推送，但**服务层自身仍不幂等** —— 幂等入账排在
  `docs/replan-2026-09/新规划任务清单.md` 的 T-P7-02（R-3 的残留项，见同文件 T-P2-02 执行结果第 6 条）。
* 旧路径的 `Sunset` 日期**故意不写**：下线时点取决于"还有谁在用"，那份名单正由§2 的限速日志收集。

## 9. 对接自检清单

1. 平台密钥是否已在 `system_config_kv` 配好（键名见§5）？没有 → 全部 503。
2. 时间戳是 Unix **秒**、且与标准时间源同步（NTP）？偏差 > 300s → 401。
3. 每条回调的 nonce 是否唯一？重复 → 409（重放会被当作攻击）。
4. 签名用的是**发送前的原始字节**，不是重新序列化后的 JSON？
5. 路径里的 `{platform}` 与签名里的 platform 是否同一个值（大小写敏感）？
