---
name: hivemtk
description: HiveMTK/WeGEO 私域营销中台的 MCP 工具入口——查客户、查知识库、跨渠道发消息、跑触达管线。当用户要求操作 HiveMTK/WeGEO（查客户资料、发抖音/企微/短信消息、检索知识库、看商机）时使用。
---

# HiveMTK MCP

通过 MCP（JSON-RPC over HTTP）操作 HiveMTK 私域营销中台。

## 接入

1. 管理员在 HiveMTK 后台签发凭证：`POST /api/mcp/credentials {"name":"..."}`，
   得到 `client_id`（`mcp-` 开头）与 `api_key`（`wgk_` 开头，**只显示一次**）。
2. 保存到 `~/.hivemtk/skill.json`：
   ```json
   {"client_id":"mcp-xxxx","api_key":"wgk_xxxx","server":"https://api.xjznpt.com"}
   ```

## 调用

```bash
node scripts/mcp.mjs tools-list                          # 列出全部工具
node scripts/mcp.mjs tools-call customer.search '{"keyword":"张"}'
node scripts/mcp.mjs tools-call rag.search '{"query":"全包多少钱"}'
```

## 常用工作流

| 用户意图 | 步骤 |
|---|---|
| 查客户 | `customer.search` → 需要详情再 `customer.get`（以 tools-list 实际名称为准） |
| 知识库问答 | `rag.search` 检索 → 引用来源回答 |
| 触达客户 | `customer.search` 圈人 → `reach.*` 对应渠道发送（**高风险**：先向用户确认收件人与文案） |
| 录入知识 | `knowledge.add_doc`（注明来源） |

## 红线

- `reach.*`（真实外发消息）与 `customer.update/merge`（写客户数据）属于不可撤回操作：
  执行前必须把「目标 + 内容」展示给用户确认。
- 工具报错时先 `tools-list` 确认参数形状，不要盲目重试；
  涉及风控/配额类错误（如 429）直接停下询问用户。

详细参数见 `reference/tools.md`（由 tools-list 生成）。
