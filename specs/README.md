# HiveMtk 规格施工源索引（specs/）

> 本页登记 `hivemtk/specs/` 下的全部 spec 项目。口径：**spec 是唯一施工源**——功能实现
> 以对应 spec 的 spec/plan/tasks/delivery 为准，交付时把结论回写到文档索引（见
> [`../docs/INDEX.md`](../docs/INDEX.md) §九）。
> 基线：复核 2026-10-24。

## spec 项目清单

| # | 目录 | 主题 | 状态 |
|---|---|---|---|
| 001 | [001-saas-multi-tenant/](001-saas-multi-tenant/) | SaaS 多租户与离线单机双形态一体化（TENANT_MODE 双形态、tenancy、契约） | 已收口 |
| 002 | [002-multi-type-messages/](002-multi-type-messages/) | 多类型消息 | 已收口 |
| 005 | [005-reach-compliance-policy/](005-reach-compliance-policy/) | 主动触达合规策略（E1） | 已交付 |
| 006 | [006-wave2-gates-and-selector-health/](006-wave2-gates-and-selector-health/) | Wave2 门禁与选择器健康（五道判据门 + 桥接选择器失效检测） | 已交付 |
| 007 | [007-observability-and-channel-health/](007-observability-and-channel-health/) | D2 AI 全链路可观测性 + B2 渠道健康监控仪表盘（运营看板） | 已交付 |
| 008 | [008-deploy-and-inference-ops/](008-deploy-and-inference-ops/) | C1 部署向导 + C2 推理降级档 | 已交付 |
| 009 | [009-docs-and-i18n-closeout/](009-docs-and-i18n-closeout/) | F1 文档分层 + F3 国际化收口 + C4 多租户文档口径 | 已交付 |
| 010 | [010-executor-split/](010-executor-split/) | A1 巨型文件分层重构——executor.go 拆分（纯重构，不改可观察行为） | 已交付 |
| 011 | [011-lead-to-cash-closure/](011-lead-to-cash-closure/) | D1 Agentic Lead-to-Cash 全流程闭环实挂（AI 报价草稿 → 改价确认 → 自动转订单/回款） | 已交付 |
| 012 | [012-module-contract-registry/](012-module-contract-registry/) | 可插拔模块边界（模块契约 + 进程内注册表 + 内建示例模块） | 已交付 |
| 013 | [013-template-skill-market/](013-template-skill-market/) | 模板/技能市场（F2 最小可用版：只读浏览 + 元数据 + 分类） | 已交付 |
| 014 | [014-egolite-blocking-confirm-gate/](014-egolite-blocking-confirm-gate/) | ego-lite 腿收口：事前阻塞写确认闸（decision 文件握手）+ runner 接线杂项（server-name 透传/键匹配/分型解帧） | 已交付 |
| 015 | [015-egolite-runtime-supervision/](015-egolite-runtime-supervision/) | ego-lite 运行时监督：域白名单三层（静扫/守卫/事件审计）+ 官方升级指令 + macOS 安装器 + 前端契约提示 | 已交付 |
| 016 | [016-egolite-scheduling-and-selfupdate/](016-egolite-scheduling-and-selfupdate/) | ego-lite 调度闭环：流程 cron 触发（纯读特权）+ egolite_run AI 工具 + runner 自更新（handoff 真机销账） | 已交付 |
| 017 | [017-bridge-turn-observability/](017-bridge-turn-observability/) | Bridge 轮次可观测性：追踪查询上下文边界 + TraceMonitor 页面 | 主体已交付；收敛报告见 converge.md（T011/T012 真页面验证未完成，不宣称 Converged） |
| 018 | [018-bridge-lead-deferred/](018-bridge-lead-deferred/) | Bridge 线索延迟处理（deferred 队列 + worker） | 主体已交付；未发布新表/worker，T009 保留（不宣称 Converged） |
| 019 | [019-bridge-conversation-evidence/](019-bridge-conversation-evidence/) | Bridge 会话证据（隔离构建 + 扩展测试） | 主体已交付；T006/T007 ego 只读证据与收敛报告未完成（不宣称 Converged） |
| 020 | [020-retrieval-observation/](020-retrieval-observation/) | 检索观测（召回/重排打点） | 主体已交付；T005 全包回归与线上收益未验证（不宣称 Converged） |
| 021 | [021-answer-cache-scope/](021-answer-cache-scope/) | 答案缓存作用域（租户/会话隔离） | 主体已交付；T005 线上命中率与速度收益未验证（不宣称 Converged） |
| 022 | [022-bridge-outbox-wake/](022-bridge-outbox-wake/) | Bridge outbox 唤醒（扩展侧等待型收口） | 主体已交付；T005 完整 Go/扩展回归与真实证据待补（不宣称 Converged） |
| 023 | [023-visitor-privacy-notice/](023-visitor-privacy-notice/) | 访客隐私告知同意闸（embed-sdk 侧，iframe 加载前拦截） | 已交付（verdict Converged，见 converge.md） |
| 024 | [024-im-seam-alignment/](024-im-seam-alignment/) | IM 接入接缝：出站发送器注册表 + 事件类型（含交互回调上下文）+ 路由 + 渲染 + 身份双层键 + 生命周期读侧 + 门禁与文档收口 | 已交付（B1–B6 全批，2026-11-18；见 delivery.md） |
| 025 | [025-im-platforms/](025-im-platforms/) | 新平台接入：企业微信客服 wecomcs / OneBot v11 / Slack / wecombot / 通用 custom 入口 | 已交付（2026-11-18；19 任务全落地，T707/T806/T904 真实账号项用模拟信号验收，见 delivery.md） |

> 注：005–016 各目录均已有 `delivery.md`（已交付收口）；001/002 早于 delivery 机制，
> 以既有收口结论为准。（原"007/012 并发 lane 在途（未提交）"注记已过期，2026-10-28 补录订正。）

## 与文档索引的关系

- 各 spec 的 `delivery.md` 写清「改动文件、检查命令与结果、遗留项」，是回写索引的依据。
- 001 覆盖关系说明见 [`../docs/INDEX.md`](../docs/INDEX.md) §九（003 显式覆盖 002 两处口径）。
- 用户端子索引的「规格与施工文档」节以本目录为唯一施工源入口。
