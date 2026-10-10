# ADR-018 catbus 借鉴评估（只读参考，不复制代码）

- **状态**：✅ Accepted（评估结论）；借鉴项落地另行排期（见各项标注）
- **决策日期**：2026-10-11
- **评估对象**：`C:\myproject\catbus`（catbus-cli：驱动 10 个平台 Web API 的统一命令行，为 AI Agent 设计——统一命令语法 + 统一 JSON 输出 + 稳定退出码 + 错误带"下一步"提示）
- **与 hivemtk 的关系**：catbus 的 core 设计（词汇表+注册表+错误信封+凭证存储纪律）正是本项目 ADR-017（统一平台词汇表与注册表）的成熟参考实现；评估为只读，未复制任何代码。

## 值得借鉴（按价值排序）

### B1. "一份词汇表 + 每渠道声明式注册" ⭐ 与 ADR-017 直接对接
- catbus：`src/core/vocab.ts`（通用命令词汇，208 行）+ `src/core/registry.ts`（`definePlatform()` 声明式注册，`resolveCommands()` 启动期全量校验）+ `src/core/capabilities.ts`（能力矩阵由注册表生成，`docs/capabilities.md` 有测试钉新鲜度）。
- 平台只声明差异：支持哪些词汇（full/partial/none）、缺哪些标准参数（`supports`）、自己的叫法。其余——解析、四层帮助、UNSUPPORTED 判定、能力矩阵——全部派生。
- **hivemtk 落点**：ADR-017 迁移计划第 1-3 步；本仓 `internal/capregistry` + `docs/registry/registry.yaml` 已有第一层。
- **状态**：🔴 排期中（ADR-017 迁移计划）。

### B2. 错误分类码 + hint（下一步动作）
- catbus：`src/core/errors.ts` 10 个错误码映射稳定退出码；每个错误带可选 `hint`（可直接执行的纠正命令）。`AUTH_EXPIRED` 与 `AUTH_REQUIRED` 分开。
- **hivemtk 落点**：API 错误信封统一 `code/message/hint`；与 MCP guard 2026-10-11 的失败原因分级（key_disabled/key_revoked/expired/credential_disabled/invalid）同口径，缺的是把 hint 语义写进 `response.Error` 规范（ADR-009 的增补）。
- **状态**：🟡 P1（随 RequirePermission 落地）。

### B3. 统一响应信封的 meta 块 = 审计字段集
- catbus：`src/core/output.ts` 每个结果 `{ok, platform, endpoint, resource, action, account, data, page, error}`，键序固定。
- **hivemtk 落点**：谁/哪个渠道/哪个动作/哪个账户——恰是审计行字段集（2026-10-11 审计增强已按此集齐 team_id/api_key_id/request_id/UA）。**已兑现**。
- **状态**：✅ 已兑现（审计增强）。

### B4. 凭证存储纪律
- catbus：`src/core/auth-store.ts` + `fsutil.ts`——身份三元组（platform×endpoint×account）一文件；schema 版本字段+读取时校验（坏文件带修复提示，不在运行期炸）；**原子写**（temp 0600 → rename，Windows EPERM/EBUSY 重试）；"只有 core 层持久化凭证"（handler 永不摸盘，dispatch 快照 diff 后仅变更时写）。
- **hivemtk 落点**：API Key 明文双轨（sha256+AES-GCM）已达标；**Windows 原子写重试**值得抄进任何"写敏感文件"的新代码（install.lock、商户密钥——后者已有 0600 纪律）。
- **状态**：🟡 模式沉淀（无独立排期，写入编码规约即可）。

### B5. 日志/审计里的密钥脱敏正则
- catbus：`src/core/log.ts` `redact()`——按键名正则（cookie|token|authorization|password|secret|ticket|sessdata|ct0）、整行 Cookie 头、`key=value`/`key: value`（含 Bearer/Basic 前缀）、URL userinfo（`//***@`）。实战打磨过的集合。
- **hivemtk 落点**：middleware/audit.go 已有键名脱敏；对齐 catbus 的头行/URL userinfo 两类（audit 中间件的入参序列化处）。
- **状态**：🟡 P2 增量（半天工作量）。

### B6. 危险操作 confirm 语义
- catbus：`CommandSpec.confirm` 声明式；非交互环境拿不到确认 → `CONFIRM_REQUIRED` 错误（exit 2）+ hint "加 -y 重跑"；绝不静默重试风控类错误。
- **hivemtk 落点**：前端已有 ElMessageBox 二次确认（key 删除/吊销等）；服务端补一个声明式 `confirm: true` 请求体字段（删除类 API 在非 UI 调用方场景强制），审计事件单列。
- **状态**：🟡 P2。

### B7. 游标分页协议（可续传 + 防死循环）
- catbus：`dispatch.ts:379-424`——`{cursor, has_more}`；`--limit` 截半页生成复合游标 `"<cursor>#skip=N"`，续传不丢不重；**同一游标出现两次告警并停**（防上游死循环）；页间隔默认 1000ms。
- **hivemtk 落点**：审计/日志/客户列表的 keyset 游标（operation_logs 已有 GetAllKeyset）——`#skip=` 技巧与 same-cursor 防环值得直接抄。
- **状态**：🟡 待遇到具体分页 bug 时引入（预防性引入优先级低）。

### B8. 文档一致性测试（防文档烂）
- catbus：`tests/skill.test.ts` 解析 agent skill 文档，断言每条提到的命令都在注册表里、每平台别名行正确——文档提到不存在的东西直接红。
- **hivemtk 落点**：skills/hivemtk/SKILL.md 提到的工具名 ↔ tools-list 实际注册工具（tools.md 生成已有，差一致性测试）；docs-consistency workflow 已有骨架。
- **状态**：🟡 P2。

### B9. out-of-process Provider 协议（语言无关渠道）
- catbus：AGENTS.md §7.8——子进程 manifest（provider.json）+ stdio 行分 JSON；请求带 credential，凭 `credential_update` 返还，**不可信组件永不接触凭证库**。
- **hivemtk 落点**：未来 MCP 插件/外部 Skill 宿主的边界蓝图（"untrusted 组件不摸凭证库"是现成铁律）。
- **状态**：📋 远期。

## 明确不借鉴

- 平台爬取机械（TLS 指纹、签名 JS 沙箱、ONNX 验证码、protobuf/jsdom）——领域特定且脆。
- 金样本录制的上游 Python 工具链——离了 catbus 的上游仓库无意义（**模式**已在 B8 吸收）。
- CLI argv 解析与四层 help——Web 平台用不上（注册表派生帮助的思想已在 B1）。
- 中文硬编码文案无 i18n 框架——本仓 9 locale 体系更完整。

## 结论

catbus 的 core 是"一份规格生成一切"的活样例，与本仓 ADR-017 方向完全同构；
最有价值的迁移是 B1（对接既有 capregistry/registry.yaml）与 B2（错误 hint），
B3/B5/B6 是低成本高确定性的补强，B9 是远期边界蓝图。
