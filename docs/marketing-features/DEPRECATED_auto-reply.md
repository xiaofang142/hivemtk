# 已下线：自动回复（auto-reply）功能与文档

> **状态**：❌ 功能已移除，文档已下线
> **移除时间**：2026-08-13
> **移除提交**：`8212b5ca` — `chore: 提交全部剩余改动（删除 CDP 自动回复 + 测试脚手架 + 验证脚本）`
> **对应任务**：OPT-DOC-01

---

## 一、为什么有这份文档

原任务 OPT-DOC-01 要求「补 3 份 `auto-reply-{universal,xianyu,tiktok}.md`」。
但这三份文档**并非缺失，而是被有意删除**——其对应的功能已经整体下线。
因此正确的处置不是「补写」，而是留下一份**下线说明**，避免后来者误以为文档丢失而重新补写。

> ⚠️ 本文件是补做的下线说明。原任务在清单中被标记为「已实施：DEPRECATED 下线说明 ✅」，
> 但该说明文件此前**从未创建**（`git log --all -- "*DEPRECATED_auto-reply*"` 零命中），
> 属假性完成，2026-09-15 复核时发现并补齐。

---

## 二、被删除的内容

`8212b5ca` 一次性删除了以下资产：

### 文档

| 文件 | 删除行数 |
|------|----------|
| `docs/marketing-features/auto-reply-universal.md` | -253 |
| `docs/marketing-features/auto-reply-xianyu.md` | -179 |
| `docs/marketing-features/auto-reply-tiktok.md` | -163 |
| `docs/feature-architecture/03-auto-reply-rag.md` | -148 |

### 代码（同一提交）

- 后端：`xianyu_auto_reply.go`、`xiaohongshu_auto_reply.go`、
  `internal/aiagent/agent/auto_reply/auto_reply_integration.go`
- 前端：`AutoReply.vue`、`autoReply.js`、`tiktokAutoReply.js`、`xianyuAutoReply.js`
- 脚本：`autoreply_api_test.sh`
- 同时清理了 `router` 模块与 i18n 中的残留引用

---

## 三、为什么下线

被删除的是**基于 CDP 无头浏览器的平台自动回复**方案，覆盖抖音 / 快手 / 咸鱼 / 小红书 / TikTok。

该方案的核心问题是**依赖浏览器自动化模拟真人操作**，因而：

1. **脆弱**：平台前端改版即失效，需持续跟进维护；
2. **风控风险**：模拟操作易触发平台风控，存在账号受限风险；
3. **不可控**：无法保证消息送达与幂等，失败场景难以恢复。

因此项目改用**官方 API / 桥接（bridge）通道**承接触达能力，
即当前的 `user-web/bridge/` 与渠道网关方案，不再依赖浏览器模拟。

---

## 四、现在的替代方案

| 能力 | 当前实现 |
|------|----------|
| 多渠道消息接入 | `user-server/internal/channelgw/`、`internal/bridge/` |
| 渠道账号与智能体绑定 | `channel_agent_bindings` 表 + `model.ChannelTypeXxx` |
| 统一消息存储 | `message_hub`（platform 维度） |
| 自动回复 / 跟单 | AI 销售引擎（ReAct Agent + SOP 流程），非浏览器模拟 |

> 相关文档：`docs/architecture/AI_CORE_FEATURE_INVENTORY.md`、
> `docs/marketing-features/README.md`。

---

## 五、不要做的事

- ❌ **不要**依据旧任务描述重新补写 `auto-reply-{universal,xianyu,tiktok}.md`；
  这些文档描述的是已废弃的 CDP 方案。
- ❌ **不要**恢复 `xianyu_auto_reply.go` / `AutoReply.vue` 等文件；
  它们已在 `8212b5ca` 中被有意移除。
- ✅ 如需新增渠道自动回复能力，请走 `internal/channelgw/` 的官方 API 通道。
