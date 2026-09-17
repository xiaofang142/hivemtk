# Discussions 频道配置（2026-08-15 M4-P1-O6）
#
# 启用 Discussions 后，按以下分类设置
# 在 GitHub 仓库 Settings → General → Features → 勾选 "Discussions"
# 然后按以下分类创建频道

## 频道列表

| 频道 | 分类 | 描述 |
|------|------|------|
| 📣 Announcements | 公告 | 项目发布、安全公告、重要变更（仅 maintainer 发帖） |
| 🙏 Q&A | 问答 | 用户提问、技术求助、使用问题 |
| 💡 Ideas | 想法 | 新功能建议、改进想法、roadmap 讨论 |
| 🚀 Show and tell | 展示 | 用户分享自己的部署案例、定制开发、二次集成 |
| 🐛 Bug reports | Bug | 已知 bug 跟踪、复现步骤、临时解决方案（与 Issues 互补） |
| 📚 Resources | 资源 | 教程、博客、第三方工具、推荐阅读 |
| 🇨🇳 中文交流 | 中文 | 中文用户专属讨论区 |

## 标签建议

### Q&A
- `solved` - 已解决
- `needs-info` - 等待用户补充信息
- `wont-fix` - 不修复
- `duplicate` - 重复

### Ideas
- `under-review` - 评估中
- `planned` - 已规划
- `rejected` - 不采纳（说明理由）
- `needs-design` - 需设计

### Bug reports
- `confirmed` - 已确认
- `workaround` - 有 workaround
- `fixed-in` - 修复版本

## 模板

### Q&A 模板

```markdown
## 问题描述

<!-- 清晰描述问题 -->

## 复现步骤

1.
2.
3.

## 期望行为

## 实际行为

## 环境

- 版本：
- 部署方式：
- 操作系统：
```

### Ideas 模板

```markdown
## 需求背景

<!-- 痛点 / 场景 -->

## 期望方案

<!-- 你建议的实现方式 -->

## 备选方案

<!-- 其他选择 / 权衡 -->

## 优先级

- [ ] Critical
- [ ] High
- [ ] Medium
- [ ] Low
```

### Show and tell 模板

```markdown
## 简介

<!-- 你的项目 / 案例 -->

## 截图 / 视频

<!-- 关键截图 / 演示视频 -->

## 链接

<!-- 部署地址 / 文档 / 源码 -->
```

## 使用规则

1. **友好尊重**：参考 [Code of Conduct](../CODE_OF_CONDUCT.md)
2. **搜索先行**：发新帖前先搜索是否已有相关讨论
3. **提供上下文**：OS / 版本 / 部署方式 / 错误日志
4. **不要 DM maintainer**：公共问题在公共区域讨论，方便其他用户检索
5. **bug 报告**：优先用 GitHub Issues（可追踪），Discussions 用于开放式讨论
6. **安全问题**：见 [SECURITY.md](../SECURITY.md)，**不要**在公开渠道披露

## 维护者职责

- 每周至少 2 次回复 Announcements
- 工作日 24h 内响应 Q&A 中的 `needs-maintainer` 标签
- Ideas 标签 `under-review` 在 7 天内给出评估
- Bug reports 标签 `confirmed` 在 14 天内给出修复计划

## 自动化

- 使用 `.github/discussions.yml` 限制匿名发帖
- 使用 GitHub Actions 标签同步
- 关键讨论自动 pin 到 Discussions 首页
