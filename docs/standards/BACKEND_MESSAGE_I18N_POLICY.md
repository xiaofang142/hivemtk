# 后端文案国际化：口径与清单（BACKEND_MESSAGE_I18N_POLICY）

> 定位：定义「后端哪些返回文案需要 i18n、哪些不需要」，并给出当前覆盖清单与缺口。
> 本文件是 F3（国际化收口）的**口径文档 + 施工清单**，供后续 lane 按清单补齐词典，不改代码结构。
> 配套：前端 9 语种见 `user-web/src/i18n/`，翻译协作规范见 [`TRANSLATOR_GUIDE.md`](../../user-web/src/i18n/TRANSLATOR_GUIDE.md)。
> 基线：复核 2026-10-24，统计为源码实算。

---

## 一、现状（已存在的基础设施，勿重建）

后端**已经**有一套局部 i18n 设施，收口时只补词典、不改框架：

| 组件 | 位置 | 作用 |
|---|---|---|
| 语言解析中间件 | `internal/middleware/locale.go` | `X-Locale` > `Accept-Language` > 中文；把语言注入 gin ctx |
| 响应层翻译出口 | `internal/pkg/utils/response/response.go` | `Success/Error/Accepted` 输出前统一 `i18n.Localize(localeOf(c), message)` |
| 错误码四语词典 | `internal/pkg/i18n/messages.go` → `ErrorMessages` | 30 个错误码 × zh/en/ja/ar |
| 语义 key 词典 | `internal/pkg/i18n/messages.go` → `Messages` + `T()` | 9 个语义 key（`success`、`operation_failed`…），支持 `{0}` 占位 |
| 中文字面词典 | `internal/pkg/i18n/backend_phrases.go` → `BackendPhrases` + `Localize()` | 100 条「中文原文 → 四语」，响应层自动替换 |
| 语言解析工具 | `internal/pkg/i18n/lang_ctx.go` / `lang_detect.go` / `locale.go` | Locale 类型与解析 |

**语言集合现状（决策点）**：后端只有 **zh / en / ja / ar 四语**，前端是 **9 语**（+ es / fr / de / ru / pt）。
本 lane 判定：后端补齐词典时**先维持四语口径**（与既有词典一致），扩到 9 语是独立决策，
避免「词典四语、新词条九语」的混合态（见 §五 决策点）。

---

## 二、口径：哪些需要 i18n，哪些不需要

### 2.1 必须 i18n —— API 响应 `message`（用户可见文案）

| 出口 | 判据 | 处理方式 |
|---|---|---|
| `response.Success(c, data, "登录成功")` | 第三参是给用户看的结果提示 | 进 `BackendPhrases` 词典（中文原文为键），或改调 `i18n.T(loc, "operation_success")` 语义 key |
| `response.Error(c, code, "当前站点未开放注册")` | 第三参是给用户看的失败原因 | 同上；若走错误码注册表，`message=""` 让 `ErrorCode` 的 `LocalizedMessage` 兜底 |
| `response.ErrorWithBusinessCode(...)` | 业务码 + 文案双通道 | 文案同上；机器码（`code` 字段）**不翻译** |

### 2.2 不 i18n —— 协议 / 机器值 / 日志 / 模型生成

| 类别 | 判据 | 理由 |
|---|---|---|
| **协议枚举与机器错误码** | 响应体 `code` 字段（`SUCCESS` / `INVALID_PARAM_1001` / `4104`…） | 前端按 `code` 分支渲染，翻译 `code` 会破坏契约 |
| **错误码值本身** | `internal/domain/errors/codes.go` 的 `ErrorCode` 常量名 | 机器标识，非文案 |
| **开发日志** | `logger.Infof/Warnf/Errorf(...)`、`log.Printf(...)` | 日志是给开发/运维看的，统一中文（仓库既有口径）；生产排查靠 grep 中文关键字 |
| **LLM 生成内容** | 提示词、AI 回复、知识库问答 | 大模型自带多语言能力；由前端把用户语言透传给对话接口（见 `locale.go` 注释） |
| **测试断言 / 内部调试串** | `*_test.go` 内的中文、含 `%v` 的拼装串 | 不进响应体，不影响用户体验 |
| **WS 内部事件名 / Redis 键 / 表名** | `message.new`、`t1:jwt_blacklist`、`tenant_id` | 协议与内部标识，翻译即故障 |

### 2.3 改造顺序（优先级）

1. **P0 错误码兜底**：所有走 `ErrorCode` 的失败路径，`message=""` 交给 `LocalizedMessage` 兜底（已具备）。
2. **P1 高频字面量进词典**：把 §四清单里的**高频**消息（出现 ≥ 5 次）补进 `BackendPhrases`。
3. **P2 迁移到语义 key**：`创建失败`/`参数错误` 这类可参数化的，逐步改调 `i18n.T(loc, "operation_failed", "创建")`，
   消除「同义多条、键形带冒号」的词典膨胀（见 §四注）。
4. **P3 语言集合对齐**：词典扩 9 语（与前端对齐），需配套 CI 门禁（防新增四语-only 词条）。

---

## 三、当前覆盖（实算读数）

| 指标 | 数值 | 统计口径 |
|---|---|---|
| `response.Success/Error/ErrorWithBusinessCode` 中文字面量消息 | **184 种** | `user-server/internal/**/*.go`（非测试），精确字面匹配 |
| 其中已被 `BackendPhrases` 收录 | **100 种** | `backend_phrases.go` 的 map 键 |
| **未收录（按原文回退中文）** | **127 种** | 见 §四清单 |

> 说明：`ErrorMessages`（30 错误码）与 `Messages`（9 语义 key）不计入上表，它们走另一条通道。
> 未收录消息经 `Localize` 时**回退为中文原文**——功能不坏，但非中文用户看到的是中文，这正是 F3 的缺口。

---

## 四、未覆盖清单（127 种，供后续 lane 施工）

> 键 = 中文字面量原文（精确匹配，含标点/空格）。分组按业务域；`次数` 为全仓出现次数。
> 迁移建议：带 `:` 后缀的是「前缀 + 动态详情」的写法，建议改造为语义 key（如 `operation_failed` + 参数），
> 而不是逐条登记进 `BackendPhrases`。

### 认证 / 令牌 / 账号（31）

| 消息 | 建议 |
|---|---|
| 未提供认证令牌 | 进词典 |
| 认证令牌格式错误 | 进词典 |
| 认证令牌已失效，请重新登录 | 进词典 |
| 令牌缺少有效租户归属，请重新登录 | 进词典 |
| 缺少租户归属，请重新登录 | 进词典 |
| 无效的认证令牌 | 进词典 |
| 无效的 Token ID | 进词典 |
| 未授权，请先登录 | 进词典 |
| 未找到用户信息 | 进词典 |
| 密码重置成功 | 进词典 |
| 新密码不能为空 | 进词典 |
| 尝试次数过多，请稍后再试 | 进词典 |
| 此操作要求启用 MFA | 进词典 |
| 需要二次验证（MFA）才能执行此操作 | 进词典 |
| 查询 MFA 状态失败 | 进词典 |
| 账号状态已变更，请重新登录 | 进词典 |
| 仅管理员可访问 | 进词典 |
| 权限不足，需要超管权限 | 进词典 |
| 无权限执行此操作 | 进词典 |
| 无权访问该渠道账号 | 进词典 |
| user_id 类型错误 | 进词典 |
| user_id 非法 | 进词典 |
| 用户 ID 类型错误 | 进词典 |
| 非法 ID | 语义 key |
| 非法成员 ID | 语义 key |
| 无效的 ID | 语义 key |
| account_id 非法 | 语义 key |
| 产品ID不能为空 | 语义 key |
| 配置不完整 | 进词典 |
| 已吊销 | 进词典 |
| 切换成功 | 进词典 |

### 渠道 / 访客 / 会话（12）

| 消息 | 建议 |
|---|---|
| 缺少 X-API-Key | 进词典 |
| 缺少 X-Bridge-Token | 进词典 |
| 缺少 X-MCP-Token | 进词典 |
| 缺少 X-MCP-Token（本入口未配置专用凭证时也可用 X-Bridge-Token） | 进词典 |
| 缺少 session_id 参数 | 进词典 |
| 缺少 visitor_id 参数 | 进词典 |
| 缺少 visitor_token，请先调用 OpenSession 获取 | 进词典 |
| visitor_token 无效或已过期 | 进词典 |
| 会话不存在或无权访问 | 进词典 |
| 无效的会话凭证 | 进词典 |
| 消息内容过长，最多 5000 字符 | 进词典 |
| 服务暂时不可用，请稍后重试 | 进词典（或错误码） |

### Bridge / Webhook（10）

| 消息 | 建议 |
|---|---|
| Bot Token 格式错误 | 进词典 |
| BotToken 未配置 | 进词典 |
| bot_token 必填 | 进词典 |
| bridge token 无效 | 进词典 |
| 桥接通道需配置 BRIDGE_INGEST_TOKEN（或显式 BRIDGE_INGEST_AUTH=off 以接受无鉴权模式） | 进词典 |
| Webhook 注册成功 | 进词典 |
| WebhookURL 无法推导（请配置 external.public_base_url 或在请求 body 中显式传 webhook_url） | 进词典 |
| WebhookURL 格式不合法 | 进词典 |
| AppKey 已轮换 | 进词典 |
| AppSecret 已重置（仅返回一次） | 进词典 |
| Client ID 或 API Key 无效（或已停用） | 进词典 |

### MCP（3）

| 消息 | 建议 |
|---|---|
| MCP token 无效 | 进词典 |
| MCP 入口需要配置 MCP_TOKEN 或桥接凭证（本入口不跟随 BRIDGE_INGEST_AUTH=off） | 进词典 |
| MCP 单值凭证需要明确的租户线索: | 语义 key |

### 知识库 / 文档 / 数据源（15）

| 消息 | 建议 |
|---|---|
| KB ID 不能为空 | 语义 key |
| rag_product_id 不能为空 | 语义 key |
| term_id 不能为空 | 语义 key |
| product_id 不能为空 | 语义 key |
| source 不能为空 | 语义 key |
| query 不能为空 | 语义 key |
| 需要 url 或 content 字段（ | 语义 key |
| 无效的文档ID | 语义 key |
| 无效的数据源ID | 语义 key |
| 无效的分段ID | 语义 key |
| 文档已接收，切片/向量化/入库处理已启动 | 进词典 |
| 文件已接收,处理已启动 | 进词典 |
| 文本导入已启动 | 进词典 |
| URL 抓取已启动 | 进词典 |
| 重建任务已启动 | 进词典 |
| OpenAPI 数据源服务未装配 | 进词典 |

### 资产 / 卡片（3）

| 消息 | 建议 |
|---|---|
| asset_id 与 agent_id 必填 | 语义 key |
| 无效的卡片ID | 语义 key |
| Token 已创建，请妥善保存明文（仅显示一次） | 进词典 |

### 角色 / 权限菜单（9）

| 消息 | 建议 |
|---|---|
| 未找到角色信息 | 进词典 |
| 未获取到角色信息 | 进词典 |
| 角色 code 不能为空 | 语义 key |
| 角色类型异常 | 进词典 |
| 保存角色菜单权限成功 | 进词典 |
| 获取角色列表成功 | 进词典 |
| 获取角色菜单权限成功 | 进词典 |
| 获取角色详情成功 | 进词典 |
| 获取菜单注册表成功 | 进词典 |
| 获取状态成功 | 进词典 |

### 订阅 / 模块 / 平台集成（4）

| 消息 | 建议 |
|---|---|
| 订阅已到期，业务功能已暂停，请续费后继续使用 | 进词典 |
| 该功能模块未开通，请先在「功能模块市场」订阅开通 | 进词典 |
| 平台集成未启用（设 PLATFORM_ENABLED=true 并启动本地 platform-server 后可用） | 进词典 |
| 平台未初始化，返回空数据 | 进词典 |

### LTC / 审批（4）

| 消息 | 建议 |
|---|---|
| ltc.config 已更新 | 进词典 |
| ltc.config 未保存： | 语义 key |
| ltc.config 未写入（存储不可用）： | 语义 key |
| ltc.config 超过 8KB 上限：整份策略是一个原子文档，不该被拆着写 | 进词典 |
| 审批门未接线（先设 | 语义 key |

### 群发 / 触达（3）

| 消息 | 建议 |
|---|---|
| 已有群发队列在处理中，请稍后再试 | 进词典 |
| 目标群体为空，无可发送对象 | 进词典 |
| 请选择目标分群 | 进词典 |

### 通用 / 运行时（30+，按需取舍）

| 消息 | 建议 |
|---|---|
| JSON 解析失败 | 进词典 |
| 参数错误: | 语义 key（参数错误 已在词典，冒号版是拼装变体） |
| 参数错误: card_id 必填 | 语义 key |
| 参数错误：menu_keys 必填（清空请传空数组） | 语义 key |
| 参数错误：请求体应为 {\"menu_keys\": [...]} | 语义 key |
| 请求参数错误: | 语义 key |
| 请求体过大 | 进词典 |
| 读取请求体失败: | 语义 key |
| size 参数非法 | 语义 key |
| 校验完成 | 进词典 |
| 测试完成 | 进词典 |
| 同步完成 | 进词典 |
| 拆分成功 | 进词典 |
| 批量导入完成 | 进词典 |
| 刷新成功 | 进词典 |
| 反馈已记录 | 进词典 |
| 自检失败 | 进词典 |
| 文件上传失败: | 语义 key |
| 文件类型不被允许: | 语义 key（无冒号版已在词典） |
| 文件错误: | 语义 key |
| 草稿计数读取失败（运行时在位但底座读不动）: | 进词典 |
| 读取持久化审计失败: | 语义 key |
| 读取持久化计费聚合失败: | 语义 key |
| 创建成功（Bot Token 验证 + Webhook/Polling 配置正在后台自动处理中） | 进词典 |
| expires_at 必须晚于当下（收到 | 语义 key |
| expires_at 需为 RFC3339，如 2026-10-01T00:00:00Z | 进词典 |
| tool registry not initialized：分级报告需要已装配的工具注册中心 | 进词典 |
| agent lifecycle runtime not assembled（双模式运行时未装配） | 进词典 |
| 账号 | 语义 key（疑似前缀拼装） |
| 卡片不存在: | 语义 key（无冒号版已在词典） |

> **清单生成方式**：`user-server/internal/**/*.go`（排除 `_test.go`）中 `response.Success/Error/ErrorWithBusinessCode`
> 第三参为中文字面量的精确字面扫描（2026-10-24）。带动态参数的消息（`fmt.Sprintf` 拼装）不在本清单，
> 建议一并迁移到 `i18n.T(loc, key, args...)`。

---

## 五、决策点与建议

| # | 决策点 | 本 lane 建议 | 拍板依据 |
|---|---|---|---|
| D1 | 后端语言集合 4 语 vs 前端 9 语 | 词典先维持 4 语，扩 9 语单独立项 | 避免词典混合态；扩 9 语要配套 CI 门禁 |
| D2 | 「中文字面量」键 vs 「语义 key」 | 新代码一律 `i18n.T()` 语义 key；存量高频字面量先进词典止损 | 字面量键带标点/拼装变体，词典会膨胀 |
| D3 | 前端 9 语种 JSON | 本 lane 不改（门禁已 100% 绿，且在途） | 取证见 delivery.md |
| D4 | 127 项清单谁补 | 后续 lane 按 §四分域批量补 `BackendPhrases` + 语义 key 迁移 | 本 lane 铁律不写 Go |

## 六、前端门禁现状（证据，不改）

| 检查 | 结果 | 命令 |
|---|---|---|
| 覆盖率门禁 | ✅ 9 语种 1978 key 全 100%，0 缺失 0 空值，rc=0 | `node user-web/scripts/check-i18n-coverage.cjs` |
| 翻译完整性（bash） | ✅ rc=0 | `bash user-web/scripts/check-i18n-coverage.sh` |
| 硬编码棘轮 | ✅ 10460 行 / 220 文件，与基线一致，rc=0 | `node user-web/scripts/check-i18n-hardcode.cjs` |
| `$t()` 引用 vs zh key | ✅ 730 个去重 key，used-but-missing = 0 | 源码扫描 |
