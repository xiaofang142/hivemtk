# Git 协作规则（hivemtk）

> 本文件为仓库 git 规则的唯一说明。配套 `.githooks/commit-msg` 钩子会自动校验提交信息，
> `.gitattributes` 统一换行符，`.gitignore` 已覆盖依赖 / 密钥 / 构建产物 / IDE。

## 1. 分支模型

- `master` 为主干（可直接部署），**禁止**未经评审直接推送破坏性提交。
- 功能开发：`feature/<名称>`，如 `feature/system-user-unify`。
- 缺陷修复：`fix/<名称>`。
- 合并采用 **Squash Merge** 或 **Rebase**，保持主干线性、提交信息规范。

## 2. 提交信息规范（Conventional Commits）

格式：

```
<type>(<scope>): <subject>
```

- `type`（必填）：`feat` `fix` `docs` `style` `refactor` `perf` `test` `build` `ci` `chore` `revert`
- `scope`（可选）：受影响的模块，如 `user-server` `bridge` `platform-web`
- `subject`（必填）：中文简洁描述，**不以句号结尾**
- 破坏性变更用 `!`：`feat(api)!: 移除弃用字段`
- 正文 / 脚注可选，用空行分隔

示例：

```
feat(user-server): 新增抖音私信桥接渠道
fix(bridge): 修复红点未读数解析错误
chore: 完善 git 规则与提交校验钩子
```

> 钩子仅校验首行；`merge` / `squash` / `revert` 自动生成的提交自动跳过。

## 3. 启用本地钩子

```bash
git config core.hooksPath .githooks
```

克隆新仓库后执行一次即可，提交时自动校验提交信息。

## 4. 忽略策略（.gitignore）

- 依赖：`node_modules/` `vendor/` `go.sum` 之外由包管理器管理
- 密钥 / 证书：`*.env`（仅 `.env-example` 入库）、`*.pem` `*.key` `*.crt` **禁止提交**
- 构建产物：`dist/` `build/` Go 二进制、`*.zip` 发布包等
- 运行时数据：`logs/` `data/` `*.db` `pg_data/`
- IDE / OS：`.vscode/` `.idea/` `.DS_Store`

> 例外：`scripts/inference-host/models.env` 为非敏感模型定义，已在 `.gitignore` 显式保留。

## 5. 协作模式（最高指示：禁用 PR，成员直推）

- **只使用两个仓库**：`gitee.com/xhpmayun/hivemtk` 与 `github.com/xiaofang142/hivemtk`，其他 forks 一律不用。
- **禁止 PR 流程**：不发起 PR、不走评审合并。
- 每次开发完成后将 `master` 直推上述两个 remote；推送前 `git fetch` 确认 fast-forward，分叉先 merge 再推。
- 早期版本的 PR 条款已废止，仅保留 `.github/PULL_REQUEST_TEMPLATE.md` 文件本身以兼容历史。

## 6. 换行与编码

- 所有文本文件统一 **LF** 换行（见 `.gitattributes`），禁止 CRLF 提交。
- 源码与文档统一 UTF-8。
