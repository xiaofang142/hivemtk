# HiveMtk 协作与 Git 管理方案论证(贡献者视角)

> 2026-09-04 · 基于 CONTRIBUTING.md / GIT_RULES.md / CLA.md / .github/ 实测 + 本地部署实践

## 一、项目现状与协作入口

| 事实 | 影响 |
|------|------|
| 双仓库:Gitee 为主仓库,GitHub 为镜像;平台端 hivemtk-platform 独立仓库 | PR 提交目标需确认:官方贡献指南写"创建 Merge Request"(Gitee 术语),但 GitHub 也有 9 个 PR;**建议先开 Issue 声明贡献意图,询问维护者收 Gitee MR 还是 GitHub PR** |
| 仓库有 CLA.md(贡献即授予版权持有人使用权) | 提 PR 即视为接受 CLA,首次贡献可在 PR 描述中声明"已阅读并同意 CLA.md 与 AGPL-3.0" |
| Issues 数为 0,无 good first issue | 社区冷启动阶段,贡献者需自行发现切入点;这正是本方案 §四 的价值 |
| 641 commits,核心作者主导,9 个 PR 待处理 | 有持续维护能力,但评审带宽可能有限;PR 应小而聚焦 |

## 二、Git 管理:官方规则(GIT_RULES.md,必须遵守)

```
分支模型:master 主干(线性历史)+ feature/<名称> + fix/<名称>
合并方式:Squash Merge / Rebase,禁止 merge commit 堆积
提交格式:<type>(<scope>): <中文 subject,不以句号结尾>
  type ∈ feat fix docs style refactor perf test build ci chore revert
  scope ∈ user-server bridge platform-web ...(受影响模块)
  破坏性:feat(api)!: 移除弃用字段
钩子:.githooks/commit-msg 自动校验,克隆后执行 git config core.hooksPath .githooks
换行:全部 LF(.gitattributes 强制),禁止 CRLF;UTF-8
密钥:*.env/*.pem/*.key/*.crt 禁止入库(.env 仅 .env-example)
评审:至少 1 个 approve + CI 通过 + 无密钥泄露才可合并
```

**论证:为什么这套规则合理**——master 直可部署 + squash 线性历史适合中小团队快速迭代;中文 subject 降低双语贡献者成本;钩子校验把规范左移到本地,减少 CI 往返。对贡献者而言无额外负担,照做即可。

**补充建议(可向维护者提案)**:
1. 分支命名已够用,但建议再加 `docs/<topic>`(纯文档)与 `perf/<topic>`,与 type 对齐,PR 评审一眼知变更性质;
2. 建议 CI 增加 `gofmt`/`golangci-lint` 结果作为 PR 必过项(本地有 `make lint`,CI 侧落实);
3. 大 PR 拆分:每 PR 聚焦一个域,超过 ~800 行 diff 建议拆。

## 三、本地 Git 工作流(贡献者实操 SOP)

```bash
# 0. 一次性配置
git clone https://github.com/xiaofang142/hivemtk.git
cd hivemtk
git config core.hooksPath .githooks        # 启用 commit-msg 校验
git config core.autocrlf false             # 强制 LF,配合 .gitattributes

# 1. 同步主干
git checkout master && git pull origin master

# 2. 开分支(命名遵循 GIT_RULES.md)
git checkout -b fix/user-server-windows-build

# 3. 开发自测闭环(提交前必跑,CLAUDE.md 铁律)
make lint          # golangci-lint 架构护栏
make vet           # go vet
make test-go       # go test ./... -count=1
# 涉及推理栈:make inference-host-test

# 4. 提交(钩子校验格式)
git add <files>
git commit -m "fix(user-server): 修复 Windows 平台无法编译(信号/rusage/statfs 平台拆分)"

# 5. 推送并开 PR(GitHub)/ MR(Gitee),用 .github/PULL_REQUEST_TEMPLATE.md

# 6. 评审意见 → 同分支追加 commit → squash 后由维护者合并
```

## 四、沟通机制设计(项目尚无 Issue 文化,需从 0 建立)

### 4.1 问题与提案分层

| 层 | 载体 | 场景 |
|----|------|------|
| Bug 报告 | GitHub/Gitee Issue,打 `bug` 标签 | 复现步骤+期望/实际+环境(GOOS/版本) |
| 功能提案 | Issue,打 `feature` 标签,先论证再动手 | 对照 README 路线图,避免与 Q3/Q4 计划冲突 |
| 安全漏洞 | **不走公开渠道**,按 SECURITY.md 私下邮件 | 项目有安全护栏(secrets/护栏代码),维护者明确要求私密 |
| 实现讨论 | PR 描述 + 代码行评论 | 架构争议引用 CLAUDE.md 五层铁律作为共同语言 |
| 日常答疑 | Issue 或仓库社区入口(README 指定) | 避免私信,沉淀为可检索知识 |

### 4.2 Issue 模板建议(项目还没有,可贡献给上游)

贡献一个 `ISSUE_TEMPLATE/bug_report.md` + `feature_request.md` 本身就是很好的首个 PR——冷启动项目最缺的就是协作基础设施,且零代码风险、通过率最高。

### 4.3 首次贡献路径(由易到难)

1. **文档/模板**:Issue 模板、本方案两份文档(CODEBASE_MAP.md + 本文件)——建立信任;
2. **环境兼容**:本次已完成的 Windows 编译修复(fix/user-server-windows-build 分支)——项目 Makefile/脚本全是 macOS/Linux 向,Windows 开发者会持续受益;
3. **测试补强**:service 包存在时序脆弱用例(TestAIDebounce_MediaExempt / TestAlertDispatcher_ConcurrentExec 并发下偶发 FAIL,重跑即过)——贡献 flaky 修复或 CI 稳定性;
4. **路线图功能**:对照 README 2026 Q3/Q4 计划,开 Issue 认领。

## 五、本次部署验证记录(方案可行性证据)

- **便携化全栈**在无 Docker/WSL 的 Windows 上跑通:PG 15.13 原生二进制 + pgvector 0.8.6(社区预编译,自编译 MinGW 版因 MSVC↔MinGW ABI 冲突崩溃)+ Redis 5.0.14(Windows 移植版)+ Go 1.25.0 便携版;
- user-server 编译修复后 `8204/health` 返回 database/redis 均 ok;`POST /api/system/init-admin` 建超管 → `/api/auth/login` 拿到 JWT → 前端 dist 由 user-server 同源托管,登录页可访问;
- LLM/Embedding/Rerank(8207-8209)未部署,health 显示 inference/embedding down 属预期(本地无 8GB+ 模型资源),不影响 API 层联调;
- 全量 `go test` 通过;发现 2 个上游 flaky 用例(时序敏感,非平台相关,单独重跑 PASS)。

**给上游的启示(可写进 PR/Issue)**:官方部署文档假设 macOS/Linux + Docker;建议 CONTRIBUTING.md 补一节 Windows 原生开发指引(便携 PG+Redis+pgvector 预编译包路径),降低 Windows 贡献者门槛。
