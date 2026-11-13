# skills/ — 可移植 Skill 源（任何编辑器可用）

本目录放**权威源** SKILL.md，格式是跨编辑器事实标准（YAML frontmatter `name` + `description` +
Markdown 正文）：OpenCode / Claude Code / Trae / Cursor 等 agent  harness 都能直接读。

## 安装到本机（改完 SKILL.md 后重跑一次）

```bash
ROOT=$(git rev-parse --show-toplevel)
for d in "$HOME/.config/opencode/skill" "$HOME/.agents/skills"; do
  mkdir -p "$d"
  cp -R "$ROOT/hivemtk/skills/." "$d/"
done
```

## 现有 Skill

| 名称 | 用途 | 触发词 |
|---|---|---|
| `hivemtk-release` | HiveMtk 三条发布链（API 上线 / 谷歌扩展 / 平台上链）的固定流程 | 发布线上、部署、上线、发版、更新扩展、装进 Chrome、重载扩展、上传安装包、上链平台、release/deploy/publish |

## 新增 Skill 的约定

1. 在 `skills/<name>/SKILL.md` 写**权威源**，`description` 必须同时写清「做什么」和「何时触发」，
   并尽量埋全中英文触发词（模型靠 description 决定是否加载）。
2. 正文命令**不要写死个人绝对路径**，用 `ROOT=$(git rev-parse --show-toplevel)` 定位仓库。
3. 改完按上面命令安装到本机，再 commit + push 双远端。
4. 深度细节指向仓内文档（如 `docs/operations/RELEASE-PIPELINE.md`），SKILL.md 只放**可执行的口径与命令**。
