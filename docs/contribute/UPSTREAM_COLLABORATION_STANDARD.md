# 上下游协作标准模式(全网调研定案版)

> 版本:2026-09-05 v1.0 · 性质:**AI 硬约束 + 团队标准**。本文件是对《COLLABORATION_CHARTER.md》的最终定案,两者冲突时以本文件为准。
> 调研来源:GitHub 官方文档、Gitee 帮助中心、Conventional Comments、Conventional Commits、Kubernetes/Rust/Linux/Ant Design/Vue 贡献指南、Google 工程实践、release-please/changesets/git-cliff 官方文档(每节末标注)。

## 0. AI 硬约束(每次会话必须遵守,违反即停)

> 本节是给 AI 执行者的最高优先级指令。任何会话中涉及 hivemtk 仓库操作时:

1. **通道唯一**:代码进上游只能走 fork→PR/MR。禁止直推 upstream(即使有成员权限);禁止绕过 PR 用其他方式改上游。
2. **先同步后干活**:任何新分支必须基于刚同步的上游 master;基线过期禁止开 PR。
3. **自测硬门槛**:commit 前必须跑完 §5 checklist 中与变更相关的项;测试不过禁止 commit。
4. **PR 标题即历史**:采用 squash merge,PR 标题=master 历史的 commit message,必须符合 Conventional Commits(`type(scope): 中文描述`)。
5. **推送与发帖前过目**:push fork、开 Issue/PR、评论上游,内容必须先给 owner 展示摘要(B 级节奏);owner 明确授权某类低风险操作(C 级)后除外。
6. **凭证红线**:私钥/token 不进仓库、不进聊天、不写死在脚本;`.env`/`*.key` 永不入库。
7. **闭环记录**:每次上游动作(Issue/PR/评审)完成后,把 URL 记入 §7 状态表;每轮会话结束前更新。
8. **SLA 纪律**:上游 7 天未响应才 ping 一次,之后每 7-14 天一次,只 ping 团队账号;PR 30 天无活动标记 stale、stale 后 7 天可关闭(关闭说明可重开)。
9. **评审语言**:给上游的评审评论遵循 Conventional Comments 标签格式;默认 non-blocking,只有 `(blocking)` 阻止合并。
10. **单一事实源**:本文件是流程唯一权威;流程问题先改本文件再执行,不允许"口头约定"。

## 1. 模式选型结论(为什么是 fork+PR)

| 候选模式 | 业界采用 | 结论 |
|----------|----------|------|
| **fork + PR + squash**(GitHub/Gitee 官方标准) | GitHub 全站标准、Gitee 官方文档、K8s/Rust/Vue/AntD | ✅ **定案采用**。隔离干净、评审留痕、贡献图谱自动累计,是上游 GIT_RULES.md 的合规路径 |
| 成员直推分支 | 多数项目禁止,own fast path | ❌ 仅备用。与上游"评审合并"规则冲突;权限生效后也不用 |
| 邮件补丁(kernel 模式) | Linux kernel | ❌ 上游已用平台 PR,不引入第二通道 |
| bors/合并队列 | Rust | ❌ 依赖上游基础设施,贡献者侧不可控 |

依据:GitHub fork 同步官方流程 https://docs.github.com/en/pull-requests/collaborating-with-pull-requests/working-with-forks/syncing-a-fork ;Gitee Fork+PR 模式 https://help.gitee.com/base/开发协作/Fork+PullRequest模式

## 2. 标准生命周期(每次贡献必经的 10 步)

```
① 同步     git fetch upstream && git merge --ff-only upstream/master
② 分支     git checkout -b <type>/<topic>(type ∈ fix|feature|docs|perf|test)
③ 开发     五层架构约束内改代码(CLAUDE.md 铁律)
④ 自测     §5 checklist(与变更相关的全部项)
⑤ 提交     Conventional Commits 中文 subject,钩子校验
⑥ 双推     git push github <branch> + git push gitee <branch>(双平台备份)
⑦ PR 双投  GitHub 主投 + Gitee MR(互相引用链接);使用上游 PR 模板;首行注明 CLA
⑧ 评审     Conventional Comments 回应;追加 commit 更新 PR(禁 force-push 已评审历史)
⑨ 合并     上游 squash merge;PR 标题即 master 历史
⑩ 清理     删双平台分支 → 回到①;URL 记入 §7 状态表
```

## 3. PR 标题与 commit 规范(squash 语义)

- squash 后 PR 标题成为上游 master 的 commit message → **PR 标题必须**:`<type>(<scope>): <中文 subject>`。
- 单 commit PR 的 commit message 同样合规(GitHub 默认取该 commit 标题,防止漏网)。
- 破坏性变更:`feat(api)!: ...` 或正文含 `BREAKING CHANGE:`。
- 类型→发版影响(供 release-please 类工具):`fix`→patch、`feat`→minor、`!`→major、`chore/build/docs` 不触发。
依据:https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/configuring-pull-request-merges/configuring-commit-squashing-for-pull-requests ;https://www.conventionalcommits.org

## 4. 双平台差异速查(GitHub vs Gitee)

| 事项 | GitHub | Gitee | 我们的执行 |
|------|--------|-------|-----------|
| PR 来源 | 自由 | **必须存在 fork 关系** | fork 已建,合规 |
| 合并方式 | merge/squash/rebase 三选 | merge/扁平化(squash) | 统一请求 squash |
| 评审门槛 | 分支保护可强制 N approve | CodeOwners 指派+门禁检查 | 目标:≥1 approve + CI 绿 |
| CLA | 第三方 bot | 原生 CLA 管理 | 每 PR 描述首行声明已同意 CLA.md |
| 回退 | revert PR | PR 一键回退(反向 PR) | 上游选择即可 |

## 5. 合并前质量门(commit/PR 前逐项过)

- [ ] 双平台编译:windows 原生 + `GOOS=linux CGO_ENABLED=0 go build`
- [ ] `go vet ./...` 零告警
- [ ] `go test ./... -count=1` 全绿(flaky 单独重跑 3 次验证)
- [ ] `make lint`(golangci-lint 五层架构护栏)
- [ ] 无跨层调用/内联 handler(CLAUDE.md 铁律)
- [ ] 涉及推理栈:smoke test 通过
- [ ] 无密钥/无 >1MB 二进制/无隐私数据
- [ ] 前端:纯 JS、Element Plus 风格

## 6. 评审与 SLA(双向)

**对上游(我们求人)**:Google 官方指南"1 个工作日是评审响应上限"仅限其内部;开源惯例是维护者 2-3 个工作日首次响应。我们的纪律:**7 天静默才 ping,每 7-14 天一次,ping 团队不 ping 个人;30 天无活动接受 stale;被关闭可礼貌请求重开**。

**对下游(将来别人给我们提 PR 时)**:响应目标 ≤2 个工作日(哪怕"已排期");评审用 Conventional Comments 标签:`praise`/`nitpick`/`suggestion`/`issue`/`question`/`todo`/`thought`/`chore`/`note`,默认 non-blocking,`(blocking)` 才阻塞;每次评审至少一条 `praise`。

依据:https://google.github.io/eng-practices/review/reviewer/speed.html ;https://conventionalcomments.org ;https://github.com/actions/stale

## 7. 状态台账(每次上游动作后 AI 必须更新此表)

| 日期 | 类型 | 平台 | 标题/编号 | URL | 状态 |
|------|------|------|-----------|-----|------|
| 2026-09-05 | 分支备份 | GitHub | fix/user-server-windows-build | https://github.com/hivemtkbot/hivemtk/tree/fix/user-server-windows-build | 已推 |
| 2026-09-05 | 分支备份 | GitHub | docs/collaboration-guides | https://github.com/hivemtkbot/hivemtk/tree/docs/collaboration-guides | 已推 |
| 2026-09-05 | 分支备份 | Gitee | 同上两分支 | https://gitee.com/jungle-hero/hivemtk/branches | 已推 |
| 2026-09-05 | 直推上游 | GitHub | 两分支推入 xiaofang142/hivemtk(hivemtkbot write 权限已生效) | https://github.com/xiaofang142/hivemtk/tree/fix/user-server-windows-build | 已推 |
| 2026-09-05 | Issue | GitHub | #11 Windows 编译修复报告 + 收流方向询问 | https://github.com/xiaofang142/hivemtk/issues/11 | 开放 |
| 2026-09-05 | PR | GitHub | #12 fix(user-server): Windows 编译修复(Closes #11) | https://github.com/xiaofang142/hivemtk/pull/12 | 待评审 |
| 2026-09-05 | PR | GitHub | #13 docs(contribute): 贡献者文档三份 | https://github.com/xiaofang142/hivemtk/pull/13 | 待评审 |
| 2026-09-05 | MR | Gitee | !1 fix(user-server): Windows 编译修复(首评承载 bug 报告+收流询问) | https://gitee.com/xhpmayun/hivemtk/pulls/1 | ~~open~~ 已关闭(源分支清理触发,教训见下) |
| 2026-09-05 | MR | Gitee | !2 docs(contribute): 贡献者文档三份 | https://gitee.com/xhpmayun/hivemtk/pulls/2 | ~~open~~ 已关闭(同上) |
| 2026-09-05 | **PR 合并** | GitHub | **#12 / #13 已 squash 合入 master(61b0d3c / 7771ecb),#11 自动关闭** | https://github.com/xiaofang142/hivemtk/pull/12 | ✅ 已合并 |
| 2026-09-05 | MR 重建 | Gitee | !3(=!1 内容)/ !4(=!2 内容),基于合并后 master 重建 | https://gitee.com/xhpmayun/hivemtk/pulls/3 · /pulls/4 | open,审查已过,卡"测试"标记 |

> 2026-09-05 平台能力备注:Gitee 个人版 API 无法创建上游 Issue(`POST /repos/{owner}/issues` 报 "project or enterprise",该端点仅企业版可用)——**Gitee 侧 bug 报告以 MR 首条评论承载**(见 !1 评论);MR 创建/更新/评论 API 一切正常(jungle-hero token,最小权限 issues/pull_requests/projects)。Gitee MR 必须以 `head: fork用户:分支` 跨库形式创建。
>
> **⚠️ 流程教训(已固化)**:
> 1. **删已合并分支必须等双平台 MR/PR 都合并后**,或删除前先检查 Gitee 侧是否还有 open MR——源分支删除会**自动关闭**依赖它的 Gitee MR(!1/!2 因此被关,重建为 !3/!4)。
> 2. **Gitee 合并门槛链**:owner 审查(POST /pulls/{n}/review,204 即过)→ 测试标记("未通过设置的测试",**API 无法标记,必须网页端由测试员点头**)。GitHub 侧无此门槛。
> 3. 双平台合并顺序定案:**先 GitHub PR 合并 → 同步 Gitee master → Gitee 侧若只有镜像同步需求则直接同步 master、不再重复发 MR**(避免双份历史)。

## 8. 合并后同步(上游→下游)

- 网页:fork 页 `Sync fork` → `Update branch`(最推荐,零风险)
- CLI:`git fetch upstream && git merge --ff-only upstream/master`(`--ff-only` 防脏提交混入)
- 合并后旧分支立即删除(squash 改写历史,旧分支不可复用)
- 发版:上游采用 Conventional Commits 后,可启用 release-please/git-cliff 类工具由合并动作自动产出 Release PR/CHANGELOG;贡献者侧不手动改 CHANGELOG
