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
| 006 | [006-wave2-gates-and-selector-health/](006-wave2-gates-and-selector-health/) | Wave2 门禁与选择器健康 | lane 在途 |
| 009 | [009-docs-and-i18n-closeout/](009-docs-and-i18n-closeout/) | F1 文档分层 + F3 国际化收口 + C4 多租户文档口径 | 本 lane |
| 014 | [014-egolite-blocking-confirm-gate/](014-egolite-blocking-confirm-gate/) | ego-lite 腿收口：事前阻塞写确认闸（decision 文件握手）+ runner 接线杂项（server-name 透传/键匹配/分型解帧） | 已交付 |
| 015 | [015-egolite-runtime-supervision/](015-egolite-runtime-supervision/) | ego-lite 运行时监督：域白名单三层（静扫/守卫/事件审计）+ 官方升级指令 + macOS 安装器 + 前端契约提示 | 已交付 |

> 注：目录下另有 `007-observability-and-channel-health/`、`012-module-contract-registry/` 为并发 lane
> 在途（未提交），不在本索引登记范围，待其 delivery 收口后自行补录。

## 与文档索引的关系

- 各 spec 的 `delivery.md` 写清「改动文件、检查命令与结果、遗留项」，是回写索引的依据。
- 001 覆盖关系说明见 [`../docs/INDEX.md`](../docs/INDEX.md) §九（003 显式覆盖 002 两处口径）。
- 用户端子索引的「规格与施工文档」节以本目录为唯一施工源入口。
