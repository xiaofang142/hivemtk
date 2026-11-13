# skills/ — 可移植 Skill 源（任何编辑器可用）

本目录放**权威源** SKILL.md，格式是跨编辑器事实标准（YAML frontmatter `name` + `description` +
Markdown 正文，agentskills.io 开放规范）：Claude Code / OpenCode / ZCode 等采纳该标准的
agent harness 能直接读；Cursor / Trae 等不读 SKILL.md 的编辑器则靠根目录 `AGENTS.md`
（其中已内联发布链等关键口径）兜底。

## 安装到本机（改完 SKILL.md 后重跑一次）

```bash
bash scripts/bootstrap-agent-env.sh   # 新机器第一步（AGENTS.md §0 第零步）：钩子 + skills + 自检，幂等
```

上节脚本即本节安装命令的超集（多做了钩子配置与一致性自检）；日常改完 SKILL.md 重跑它即可。

> 2026-11-14 两处修正：① 旧命令写死 `$ROOT/hivemtk/skills`，只在外层仓执行才成立，
> 从内层仓跑指向不存在的路径——改为按 `skills/` 是否存在自适应；② OpenCode 全局技能目录
> 是复数 `skills`（官方文档），旧的单数 `skill` 目录它不读。

## 现有 Skill

| 名称 | 用途 | 触发词 |
|---|---|---|
| `hivemtk-coding` | 编码约束编排器——写码前/中/后的统一动作序列，保证全员产出同一标准代码（规则本体指向权威文档，零复述） | 写代码、编码、开发、新增功能、加接口、修 bug、改 user-server/user-web |
| `hivemtk-release` | HiveMtk 三条发布链（API 上线 / 谷歌扩展 / 平台上链）的固定流程 | 发布线上、部署、上线、发版、更新扩展、装进 Chrome、重载扩展、上传安装包、上链平台、release/deploy/publish |
| `hivemtk` | HiveMTK/WeGEO 私域营销中台的 MCP 工具入口（查客户 / 知识库 / 跨渠道发消息 / 触达管线） | 查客户、发消息、触达、知识库、MCP |
| `browser-debug-rule` | 浏览器调试/自动化全局强制规则（主方案 ego lite，复用登录态，禁独立 profile/重登录） | 浏览器调试、页面验证、截图取证、console/network 排查 |

## 跨编辑器分发（业界通用路径，可选）

业界主流分发是 Vercel 的 `npx skills` CLI（自动探测本机已装的 70+ 编辑器，装到各自目录，
skills.sh 为其排行榜）：`npx skills add <owner>/<repo> --skill <name>`。本仓 skills/ 与
agentskills.io 开放规范兼容，天然可用该通道；日常用上节自带安装命令即可（零依赖、不联网）。

## 新增 Skill 的约定

1. 在 `skills/<name>/SKILL.md` 写**权威源**，`description` 必须同时写清「做什么」和「何时触发」，
   并尽量埋全中英文触发词（模型靠 description 决定是否加载）；有反触发边界也写进 description。
2. 正文命令**不要写死个人绝对路径**（确属个人机配置的内容须显式标注「本机个人配置示例」）。
3. 改完按上面命令安装到本机，再 commit + push 双远端。**只改部署副本（`~/.agents/skills` 等）
   不改权威源 = 违规**，下次安装会被覆盖。
4. 深度细节指向仓内文档（如 `docs/operations/RELEASE-PIPELINE.md`），SKILL.md 只放**可执行的口径与命令**。
