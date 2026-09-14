# LICENSE 合规自检（AGPL-3.0 第 13 条）

> **v3 审计结论 [P0-S2]**：项目 LICENSE 是 AGPL-3.0（含网络 copyleft 第 13 条）。
> 任何 fork 后**通过网络对外提供服务**（SaaS / 托管 / API）必须开源修改。

## 自检工具

```bash
# 判定当前仓库是否违反 AGPL-3.0
./scripts/license-compliance-scan.sh

# 指定 URL 校验
./scripts/license-compliance-scan.sh --public-url https://your-domain.com

# 仅判定不探活
./scripts/license-compliance-scan.sh --dry-run
```

## 判定矩阵

| 维度 | PASS | WARN | FAIL |
|---|---|---|---|
| git remote | 上游为官方 | 非官方 fork | — |
| PUBLIC_BASE_URL | 私网 IP | 公网 IP | — |
| HTTP 探活 | 不可达 | 200 | — |
| 整体 | 全部 PASS | 任一 WARN | 任一 FAIL |

## AGPL-3.0 第 13 条原文（核心）

> 如果你修改本程序并通过网络提供服务，使得服务对象能够通过计算机网络
> 与本程序进行交互，你必须向服务对象提供你修改后的对应源代码。

## 私域豁免

- **内部使用**（员工/团队内部 SaaS）：豁免
- **私有部署**（客户内网运行）：豁免
- **对外提供网络服务**：必须开源修改

## 定期自检

`.github/workflows/lint.yml` 的 `license-compliance` job 每月 1 日
（cron `0 3 1 * *` UTC）自动跑一次 `license-compliance-scan.sh`，
支持 `workflow_dispatch` 手动触发，WARN 不阻断、FAIL 使 workflow 失败。
