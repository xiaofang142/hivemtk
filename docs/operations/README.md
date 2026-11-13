# HiveMtk 运维层导航（docs/operations/）

> 本页是 `hivemtk/docs/operations/` 的分层索引，补齐运维文档散落、无层内导航的缺口。
> 权威入口仍是顶层 [`docs/INDEX.md`](../INDEX.md)。基线：复核 2026-10-24。

## 一、部署与初始化

| 文档 | 说明 |
|---|---|
| [MERCHANT_DEPLOYMENT.md](MERCHANT_DEPLOYMENT.md) | 商户部署手册 |
| [RELEASE-PIPELINE.md](RELEASE-PIPELINE.md) | 发布链总入口：API 线上 / 蜂桥扩展（`npm run publish`）/ 安装包上链平台 |
| [MERCHANT_INITIALIZATION_FLOW.md](MERCHANT_INITIALIZATION_FLOW.md) | 首次启动初始化流程（/setup 开户字段、静默合规采集、开户赠送） |
| [HA_DEPLOYMENT.md](HA_DEPLOYMENT.md) | 高可用部署（单机 + 备份） |
| [DR_RECOVERY.md](DR_RECOVERY.md) | 灾难恢复方案 |
| [EPAY_LIVE_CHECKLIST.md](EPAY_LIVE_CHECKLIST.md) | 易支付上线前真人支付验收清单（003 · T032） |

## 二、运行保障

| 文档 | 说明 |
|---|---|
| [SLA_SLO.md](SLA_SLO.md) | 服务等级承诺 |
| [secret_rotation.md](secret_rotation.md) | 密钥轮换指南 |
| [INSPECTION_SQL_LAYER_DECISION_AUDIT.md](INSPECTION_SQL_LAYER_DECISION_AUDIT.md) | 巡检 SQL 手册：layer_decision_logs 与审计操作日志（RM-1⑦；audit_logs 名不副实，真实表 operation_logs） |
| [LICENSE_COMPLIANCE.md](LICENSE_COMPLIANCE.md) | LICENSE 合规自检（AGPL-3.0 第 13 条） |
| [PERFORMANCE_BENCHMARK.md](PERFORMANCE_BENCHMARK.md) | 性能基准与 SLA 阐明（报告） |

## 三、功能专题

| 文档 | 说明 |
|---|---|
| [Bridge_Runbook.md](Bridge_Runbook.md) | Bridge 桥接系统企业运维手册 |
| [CHAT_WIDGET_EMBED.md](CHAT_WIDGET_EMBED.md) | 嵌入式 Chat Widget 集成指南 |
| [AI_AGENT_PERF_DEPLOY.md](AI_AGENT_PERF_DEPLOY.md) | AI 销冠部署 |
| [AI_AGENT_PERF_API.md](AI_AGENT_PERF_API.md) | AI 销冠 API |
| [KNOWLEDGE_GROUP_DEPLOY.md](KNOWLEDGE_GROUP_DEPLOY.md) | 智能体知识库隔离部署指南 |
| [KNOWLEDGE_GROUP_API.md](KNOWLEDGE_GROUP_API.md) | 智能体知识库隔离 API 参考 |

## 四、反向代理模板

| 文档 | 说明 |
|---|---|
| [reverse-proxy/README.md](reverse-proxy/README.md) | 反代配置模板（v3 审计 P1-A1） |
| [reverse-proxy/nginx.conf.template](reverse-proxy/nginx.conf.template) | nginx 模板 |
| [reverse-proxy/caddy.Caddyfile.template](reverse-proxy/caddy.Caddyfile.template) | Caddy 模板 |
| [reverse-proxy/traefik.yml.template](reverse-proxy/traefik.yml.template) | Traefik 模板 |
| [reverse-proxy/frpc.toml.template](reverse-proxy/frpc.toml.template) | frpc 客户端模板 |

## 相关索引

| 索引 | 内容 |
|---|---|
| [`../INDEX.md`](../INDEX.md) | 用户端子索引 |
| [`../DEPLOYMENT_GUIDE.md`](../DEPLOYMENT_GUIDE.md) | 部署运维手册（三份主文档之一） |
| [`../TROUBLESHOOTING.md`](../TROUBLESHOOTING.md) | 故障排查手册 |
| [`../handbook/README.md`](../handbook/README.md) | 功能文档 / 操作手册四册入口 |
