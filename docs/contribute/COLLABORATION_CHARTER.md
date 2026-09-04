# HiveMtk 协作章程(Workspace ↔ 上游)

> 版本:2026-09-04 v1.0 · 生效范围:本机 workspace 对 hivemtk / hivemtk-platform 的一切贡献活动
> 上游规则 precedence:`GIT_RULES.md` > `CONTRIBUTING.md` > `CLAUDE.md` > 本章程;冲突时以上游为准。

## 1. 身份与凭证

| 项 | 值 | 用途 |
|----|----|------|
| GitHub 账号 | **hivemtkbot** | fork、PR、Issue |
| Gitee 账号 | **jungle-hero**(HiveMtkBot) | fork、MR(Gitee 为上游主仓库) |
| SSH 密钥 | `~/.ssh/id_ed25519`(单密钥双平台) | 推送/拉取认证 |
| SSH 路由 | GitHub 强制走 `ssh.github.com:443`(本机 22 端口被网络拦截),已固化 `~/.ssh/config` | — |

**remote 布局**(每个克隆工作区统一):
```
upstream  https://github.com/xiaofang142/hivemtk.git   # 只读,只 fetch 不 push
github    git@github.com:hivemtkbot/hivemtk.git        # 推送 fork
gitee     git@gitee.com:jungle-hero/hivemtk.git        # 推送 fork
origin    = github(如需,可 git remote rename github origin)
```

**铁则**:私钥不落仓库、不进聊天记录、不做多机拷贝;`.env`/`*.key`/`*.pem` 永不提交(上游 .gitignore 已覆盖)。

## 2. 贡献通道与顺序(每次都按这个来)

> **通道决策(2026-09-04 实测后定案)**:
> - **GitHub = 成员直推(fast path)**:hivemtkbot 已被加为 `xiaofang142/hivemtk` 成员(dry-run 推送验证通过),分支可直接推上游同名分支,再开 PR 评审合并;同时 push 到 fork 备份。
> - **Gitee = fork + MR**:jungle-hero 未被加为上游成员(实测 `Access denied`),fork 也未创建;待 Gitee 网页 fork 后走标准 fork→MR 流程。
> - 简言之:**日常走 GitHub 成员直推,代码备份与 Gitee 互动走 fork**。

```
①上游同步     git fetch upstream && git checkout master && git merge --ff-only upstream/master
②分支         git checkout -b <type>/<topic>     # type ∈ fix|feature|docs|perf|test
③开发+自测    改动 → 构建双平台 → vet → go test ./...(-count=1)
④提交         Conventional Commits 中文 subject,钩子自动校验
⑤推送         GitHub:直推 upstream 同名分支(成员权限,实测已生效);同时 push github 备份
              Gitee:push gitee(fork)后开 MR
⑥上游动作     先 Issue 后 PR(见 §4)
⑦评审         意见 → 原分支追加 commit → 通知评审者
⑧合并后清理   删分支 → 同步 master(回到①)
```

## 3. 分支与 Commit 规范(固化)

- 分支名:`<type>/<topic>`,如 `fix/user-server-windows-build`、`docs/collaboration-charter`。**禁止** `master`、`dev`、个人名分支。
- Commit:`<type>(<scope>): <中文 subject>`;type ∈ feat fix docs style refactor perf test build ci chore revert;subject 不以句号结尾;破坏性加 `!`。
- 一个分支只做一件事;diff 超 ~800 行必须拆。
- 每次 commit 前自测三件套是硬门槛(Windows 修复那次已验证流程)。

## 4. 上游交互策略(冷启动期专用)

上游现状:0 Issue / 641 commits / 单主干开发 / Gitee 主仓库+GitHub 镜像。

| 动作 | 规则 |
|------|------|
| Issue 优先 | 任何 PR 之前,先开 Issue 描述问题+复现+方案,等维护者反馈方向。首条 Issue 同时询问:**PR 收 GitHub 还是 Gitee MR** |
| PR 双投 | 维护者未明确前,PR 发 GitHub(流程标准、留痕好),并在 Issue 里附 Gitee MR 链接 |
| 安全漏洞 | 永远不走公开渠道,按 SECURITY.md 私下邮件 |
| CLA | 每个 PR 描述首行注明:"已阅读并同意 CLA.md,贡献按 AGPL-3.0 分发" |
| PR 模板 | 使用 `.github/PULL_REQUEST_TEMPLATE.md` |
| 邮件礼仪 | Issue/PR 全程中文(上游习惯),引用上游文档条目作为共同语言(如"符合 CLAUDE.md 五层铁律第 3 条") |
| 沉淀 | 所有讨论在 Issue/PR 内,不私信、不另开群;每周一对未回应的 Issue 礼貌 ping 一次,超过两周无回应则标注 stale 自行收尾 |

## 5. 评审与质量门(合并前 checklist)

- [ ] 双平台编译:`CGO_ENABLED=0 go build`(windows + GOOS=linux)
- [ ] `go vet ./...` 零告警
- [ ] `go test ./... -count=1` 全绿(flaky 用例须注明并单独重跑验证)
- [ ] `make lint`(golangci-lint 架构护栏)通过
- [ ] 五层架构不违规(Router→Handler→Service→Repository→Model,禁止跨层)
- [ ] 涉及推理栈:推理栈 smoke test 通过
- [ ] 无密钥/隐私/大文件(>1MB 二进制)入库
- [ ] 前端:纯 JS(禁 TS),Element Plus 风格

## 6. Workspace 内部分工(你是 owner,我是执行者)

| 角色 | 职责 |
|------|------|
| 你(owner) | 账号与凭证所有权;fork/PR 页面最终点击;评审意见转达;是否推送的最终决定 |
| 我(执行) | 代码/文档产出、自测闭环、commit、准备 PR 文案;**推送与上游发帖前必须向你展示内容摘要并获得确认**(§7 节奏表为准) |

## 7. 操作节奏(三级,默认 Level B)

| 级别 | 行为 | 适用 |
|------|------|------|
| A 全手动 | 每个 diff 先展示,你点头才 commit | 敏感改动(涉及密钥、删数据、上游 README/LICENSE) |
| **B 默认** | 本地 commit 自动,推送/开 PR/发 Issue 前展示摘要等确认 | 日常开发(当前) |
| C 全自动 | commit+push+PR 全自动,你只看通知 | 你明确授权某类低风险变更(如纯文档 typo)后可对同类永久生效 |

## 8. 已有资产索引(勿重复劳动)

| 资产 | 位置 | 状态 |
|------|------|------|
| Windows 编译修复 | 分支 `fix/user-server-windows-build` commit 082bad8 | 自测通过,待发 PR |
| 代码目录清单 | `docs/contribute/CODEBASE_MAP.md` | 已 commit(2656bfa),建议拆独立 docs 分支 |
| 协作方案初稿 | `docs/contribute/COLLABORATION_PROPOSAL.md` | 同上 |
| 本章程 | `docs/contribute/COLLABORATION_CHARTER.md` | 本文件 |
| 本地全栈 | hivemtk-deploy/(PG+pgvector+Redis+llama.cpp embedding/rerank) | health 全绿 |
| 已知上游缺陷线索 | ① Windows 编译(已修)② failover 健康检查 404 误熔断 ③ service 包 2 个 flaky 用例 | ②③ 待 Issue |

## 9. 记忆与延续

每次会话结束前:更新本文件 §8 资产索引;PR/Issue 的 URL 追加到 §8。新会话从本文件恢复上下文,不依赖对话记忆。
