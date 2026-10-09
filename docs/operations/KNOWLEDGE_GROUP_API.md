# 智能体知识库隔离 - API 参考

> API 风格: RESTful + JSON  
> 基础路径: `/api/knowledge-bases`  
> 鉴权: Bearer Token (JWT)  
> 文档版本: 1.0 (2026-07-31)  
> **2026-10-09 逐条对代码复核并订正**（本节下列口径以 controller 现读为准）：
> 路由没有 `/v1` 这一级——`user-server/internal/controller/knowledge_base.go:30` 的
> `Group("/knowledge-bases")` 挂在 `internal/router/router.go:342` 的 `auth := r.Group("/api")` 下；
> 更新是 `PUT /:id`（不是 `PATCH`，`knowledge_base.go:38`）；
> 绑定/解绑是 `POST /:id/bind` / `POST /:id/unbind`（`:40-41`），另有 `/api/agent-kb-bindings` 一族
> （`controller/agent_kb_binding.go:32-42`）；代码里**不存在** `/api/agents/{agent_id}/knowledge-bases*` 嵌套路由；
> 错误码是 `INVALID_PARAM_1001` 式字符串（`internal/pkg/utils/error_code.go:11-45`），不是 4 位数字；
> user-server 监听 **8204**（`docs/PORT_REGISTRY.md`）。

---

## 1. 通用约定

### 1.1 通用响应格式

```json
{
  "code": 0,
  "message": "ok",
  "data": { ... }
}
```

错误响应:
```json
{
  "code": "INVALID_PARAM_1001",
  "message": "无效的请求参数"
}
```

> `code` 成功时是整数 `0`，失败时是下面表里的字符串错误码（`internal/pkg/utils/response/response.go:40-44`
> 的 `Response.Code` 为 `any`、`Data` 带 `omitempty` ⇒ 出错时**没有** `data` 键，
> 而不是 `"data": null`）。

### 1.2 错误码

错误码定义在 `user-server/internal/pkg/utils/error_code.go:11-45`，HTTP 映射在同文件
`errorCodeRegistry`（`:77-123`），由 `response.errorCodeFromHTTPCode`（`response.go:191-211`）按 HTTP
状态码折算：

| 错误码 | 含义 | HTTP |
|--------|------|------|
| 0 | 成功 | 200 |
| `INVALID_PARAM_1001` | 参数错误 | 400 |
| `NOT_FOUND_1002` | 资源不存在 | 404 |
| `ALREADY_EXISTS_1003` | 资源已存在 | 409 |
| `UNAUTHORIZED_2001` | 未授权 | 401 |
| `FORBIDDEN_2002` | 越权访问 | 403 |
| `VALIDATION_4001` / `REQUIRED_FIELD_4002` | 业务校验失败（走 `ErrorFromDB` 时按消息文本折算成 400） | 400 |
| `INTERNAL_ERROR_6002` | 服务器内部错误 | 500 |

### 1.3 通用字段

| 字段 | 类型 | 说明 |
|------|------|------|
| id | uint | 知识库 ID |
| kb_code | string | 业务唯一码 (KB-FAQ-001) |
| type | enum | faq / rag / sop |
| name | string | 知识库名称 |
| description | string | 描述 |
| owner_type | enum | private / shared |
| owner_agent_id | uint \| null | 当 owner_type=private 时必填 |
| enabled | bool | 是否启用 |
| member_count | int | 成员数 (冗余) |
| doc_count | int | 文档数 (冗余) |
| created_at | string (RFC3339) | 创建时间 |
| updated_at | string (RFC3339) | 更新时间 |

---

## 2. 知识库 CRUD

### 2.1 创建知识库

```http
POST /api/knowledge-bases
Content-Type: application/json
Authorization: Bearer <token>

{
  "kb_code": "KB-FAQ-001",
  "type": "faq",
  "name": "客服常见问题",
  "description": "电商平台 FAQ",
  "owner_type": "private",
  "owner_agent_id": 1001,
  "enabled": true
}
```

**响应 (200)**:
```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "id": 123,
    "kb_code": "KB-FAQ-001",
    "type": "faq",
    "name": "客服常见问题",
    "owner_type": "private",
    "owner_agent_id": 1001,
    "enabled": true,
    "created_at": "2026-07-31T16:00:00Z",
    "updated_at": "2026-07-31T16:00:00Z"
  }
}
```

**业务校验失败 (400)**:
```json
{
  "code": "INVALID_PARAM_1001",
  "message": "owner_type=shared 时 owner_agent_id 必为空"
}
```

> 创建走 `response.ErrorFromDB`（`response.go:140-189`）：错误消息命中「不能为空 / 参数 / 校验」等
> 词族时折 400，命中「不存在」折 404，其余落 500（`INTERNAL_ERROR_6002`）——
> 也就是说 **400 的具体码取决于折算结果**，`VALIDATION_4001` 那族只在 controller 直接传
> `utils.ErrorCode` 时才会原样出现。

---

### 2.2 查询知识库

```http
GET /api/knowledge-bases/{id}
Authorization: Bearer <token>
```

**响应 (200)**:
```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "id": 123,
    "kb_code": "KB-FAQ-001",
    "type": "faq",
    ...
  }
}
```

**资源不存在 (404)**:
```json
{
  "code": "NOT_FOUND_1002",
  "message": "知识库不存在"
}
```

---

### 2.3 更新知识库

```http
PUT /api/knowledge-bases/{id}
Content-Type: application/json
Authorization: Bearer <token>

{
  "name": "更新后的名称",
  "description": "新描述",
  "enabled": false
}
```

**响应 (200)**:
```json
{
  "code": 0,
  "message": "更新成功",
  "data": {
    "id": 123
  }
}
```

> 方法在 `knowledge_base.go:38` 注册为 `PUT`，代码里没有 `PATCH`；
> 但 `UpdateKB`（`service/knowledge_base.go:225-230`）是"先加载现有记录、只覆盖请求里显式提供的字段"，
> 所以**语义上是部分更新**——`knowledgeBaseUpdateReq`（`controller/knowledge_base.go:202-209`）
> 不含 `kb_code`，改不了业务码。
> 返回体只有 `{"id": 123}`（`controller/knowledge_base.go:235`），不回整份对象。

---

### 2.4 删除知识库

```http
DELETE /api/knowledge-bases/{id}
Authorization: Bearer <token>
```

**响应 (200)**:
```json
{
  "code": 0,
  "message": "删除成功",
  "data": {
    "id": 123
  }
}
```

**业务说明**: 删除 KB 时, 同步级联删除所有 `agent_kb_bindings` 引用
（`service/knowledge_base.go:321-329`：先 `bindingRepo.DeleteByKB`，再 `repo.Delete`）。

---

## 3. 知识库查询

### 3.1 列表查询 (管理端)

```http
GET /api/knowledge-bases?type=faq&owner_type=private&agent_id=1001&keyword=客服
Authorization: Bearer <token>
```

**Query 参数**（`controller/knowledge_base.go:63-77` 的 `List` 只读这四个）:

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| type | string | 否 | faq / rag / sop |
| owner_type | string | 否 | private / shared |
| agent_id | uint | 否 | 折算成 `owner_agent_id = ?`（`service/knowledge_base.go:168-171`），即"该智能体**私有**的 KB"，不含它绑定的共享 KB |
| keyword | string | 否 | 按名称/描述模糊，**取回后才在内存里过滤**（`service/knowledge_base.go:180-188`），此时 `total` 是过滤后的条数 |

> 设计稿里的 `enabled` / `page` / `page_size` 这个端点**没有实现**：
> `KBListFilter` 有 `Enabled/Limit/Offset` 字段（`repository/knowledge_base.go:67-74`），
> 但 service 从不赋值；单次最多返回 200 行、按 `id DESC`
> （`repository/knowledge_base.go:96-100`）。要分页得走服务端尚未接线的过滤。

**响应 (200)**:
```json
{
  "code": 0,
  "message": "查询成功",
  "data": {
    "total": 42,
    "list": [
      {
        "id": 123,
        "kb_code": "KB-FAQ-001",
        ...
      }
    ]
  }
}
```

---

### 3.2 智能体可见知识库 (业务端)

```http
GET /api/knowledge-bases/by-agent/{agent_id}
Authorization: Bearer <token>
```

**路由事实**：注册的是 `g.GET("/by-agent/:aid", ...)`（`controller/knowledge_base.go:33`），
路径参数名是 **`aid`**；代码里**不存在** `/api/agents/{agent_id}/knowledge-bases` 这种挂在 agents 下的
嵌套路由（`grep -rn 'Group("/agents' user-server/internal/` 零命中；`/api/agents*` 一族实际注册在
`internal/router/service_routes.go:45-53`，全集是 `/agents`、`/agents/me`、`/agents/all`、`/agents/online`、
`/agents/:id`、`/agents/:id/status`、`/agents/:id/online`、`/agents/:id/offline`、`/agents/:id/sessions`
——其中没有 `knowledge-bases` 子路径）。

**业务逻辑**: 返回 `agent_id` 可见的 KB = 私有 ∪ 显式绑定的共享
（`repository/knowledge_base.go:137-142`：`owner_agent_id = ? OR (owner_type='shared' AND id IN (绑定子查询))`，
且两侧都要求 `enabled`）

**响应 (200)**:
```json
{
  "code": 0,
  "message": "ok",
  "data": [
    {
      "id": 123,
      "kb_code": "KB-FAQ-001",
      "type": "faq",
      "owner_type": "private",
      "owner_agent_id": 1001,
      ...
    },
    {
      "id": 456,
      "kb_code": "KB-SOP-PLATFORM",
      "type": "sop",
      "owner_type": "shared",
      "owner_agent_id": null,
      ...
    }
  ]
}
```

---

### 3.3 按类型 / 共享知识库列表

```http
GET /api/knowledge-bases/by-type/sop
Authorization: Bearer <token>
```

**路由事实**：只有 `g.GET("/by-type/:type", ...)`（`controller/knowledge_base.go:34`）这一个按类型端点，
`:type` 必须是 `faq/rag/sop` 之一，否则 `IsValidKBType` 直接回 400
（`controller/knowledge_base.go:155-160`）；没有 `/knowledge-bases/shared` 这个路径。

"共享列表"走通用列表端点加过滤：

```http
GET /api/knowledge-bases?owner_type=shared&type=sop
```

**响应 (200)**:
```json
{
  "code": 0,
  "message": "ok",
  "data": [
    {
      "id": 456,
      "kb_code": "KB-SOP-PLATFORM",
      "type": "sop",
      "owner_type": "shared",
      ...
    }
  ]
}
```

---

## 4. 智能体绑定

### 4.1 单个绑定

有两个真实端点，形状不同，别混用：

```http
POST /api/knowledge-bases/{kb_id}/bind
Content-Type: application/json
Authorization: Bearer <token>

{
  "agent_id": 1001
}
```

**响应 (200)**（`controller/knowledge_base.go:40` 注册，handler `BindToAgent`，`:257-278`）:
```json
{
  "code": 0,
  "message": "绑定成功",
  "data": {
    "kb_id": 456,
    "agent_id": 1001
  }
}
```

> `knowledgeBaseBindReq` 只有 `agent_id` 一个字段 ⇒ 这个端点**传不了 `priority`**。
> 要带优先级用另一个：
>
> ```http
> POST /api/agent-kb-bindings
> { "agent_id": 1001, "knowledge_base_id": 456, "priority": 10 }
> ```
>
> （`controller/agent_kb_binding.go:40` 注册 + `agentKBBindReq`，`:142-146`；
> 响应 `data` 为 `{agent_id, knowledge_base_id, priority}`。）
> 绑定对象是 `model.AgentKBBinding`（表 `agent_kb_bindings`，`model/agent_kb_binding.go:29-45`），
> `role` 只有 `primary` / `reference` 两值，默认 `primary` ⇒ `role` 不由这两个端点写入。

**业务说明**:
- 重复绑定 (agent, kb) 自动覆盖（表上有 `UNIQUE(agent_id, kb_id)`，`model/agent_kb_binding.go:31-32`）
- kb 必须存在

---

### 4.2 单个解绑

```http
POST /api/knowledge-bases/{kb_id}/unbind
Content-Type: application/json
Authorization: Bearer <token>

{
  "agent_id": 1001
}
```

**响应 (200)**:
```json
{
  "code": 0,
  "message": "解绑成功",
  "data": {
    "kb_id": 456,
    "agent_id": 1001
  }
}
```

> 解绑有三个口子，全部 **不是** `DELETE /agents/{aid}/knowledge-bases/{kb_id}`：
> `POST /api/knowledge-bases/:id/unbind`（`controller/knowledge_base.go:41`，body `agent_id`）、
> `DELETE /api/agent-kb-bindings/{agentId}/{kbId}`（`controller/agent_kb_binding.go:39`，
> 返回 `data` 为 `{agent_id, knowledge_base_id}`）、
> `DELETE /api/agent-kb-bindings`（`:42`，body `{agent_id, knowledge_base_id}`）。

---

### 4.3 智能体的所有绑定

```http
GET /api/agent-kb-bindings/by-agent/{agent_id}
Authorization: Bearer <token>
```

**路由事实**：`controller/agent_kb_binding.go:34-35` 同时注册了
`/by-agent/:agentId` 与旧别名 `/agent/:aid`，两个都能用；另有
`PUT /api/agent-kb-bindings/by-agent/{agent_id}`（`:38` 的 `ReplaceByAgent`，
body `{"kb_ids": ["456","789"]}`，**字符串数组**，全量替换该智能体的挂载）。

**Query 参数**: 无——handler 只读路径参数（`controller/agent_kb_binding.go:54-69`），
设计稿里的 `kb_type` 过滤在这个端点上没有实现。

**响应 (200)**:
```json
{
  "code": 0,
  "message": "查询成功",
  "data": [
    {
      "id": 789,
      "agent_id": 1001,
      "kb_id": 456,
      "kb_type": "sop",
      "role": "primary",
      "priority": 10,
      "enabled": true
    }
  ]
}
```

---

### 4.4 知识库的引用智能体

```http
GET /api/agent-kb-bindings/by-kb/{kb_id}
Authorization: Bearer <token>
```

**路由事实**：`controller/agent_kb_binding.go:36-37` 注册 `/by-kb/:kbId` 与旧别名 `/kb/:kid`；
没有 `GET /api/knowledge-bases/{kb_id}/agents` 这个路径。

**响应 (200)**:
```json
{
  "code": 0,
  "message": "查询成功",
  "data": [
    {
      "id": 789,
      "agent_id": 1001,
      "kb_id": 456,
      "role": "primary",
      "priority": 10,
      "enabled": true
    }
  ]
}
```

---

### 4.5 批量绑定

```http
POST /api/agent-kb-bindings/batch
Content-Type: application/json
Authorization: Bearer <token>

{
  "items": [
    {"agent_id": 1001, "knowledge_base_id": 456, "priority": 1},
    {"agent_id": 1001, "knowledge_base_id": 789, "priority": 2},
    {"agent_id": 1002, "knowledge_base_id": 456, "priority": 1}
  ]
}
```

**业务说明**:
- 事务性: 任一失败, 全部回滚（`service/agent_kb_binding.go:229-234` 的行为注释 + `:254` 的 `tx.Transaction`）
- 重复 binding 自动覆盖
- `items` 为空**不算错**，直接返回 nil、照样回 200（`service/agent_kb_binding.go:236-238`）

**响应 (200)**（`controller/agent_kb_binding.go:189-191` 只回条数，没有成功/失败分列）:
```json
{
  "code": 0,
  "message": "批量绑定成功",
  "data": {
    "count": 3
  }
}
```

**失败 (404)**（消息含"不存在"，`response.ErrorFromDB` 折成 404；整体已回滚，所以没有部分成功的计数）:
```json
{
  "code": "NOT_FOUND_1002",
  "message": "items[agent=1001 kb=99999]: 知识库不存在"
}
```

---

## 5. 业务规则示例

### 5.1 创建共享 KB (跨智能体可见)

```bash
# 1. 创建 shared KB
curl -X POST http://localhost:8204/api/knowledge-bases \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <admin_token>" \
  -d '{
    "kb_code": "KB-SOP-PLATFORM",
    "type": "sop",
    "name": "平台通用 SOP",
    "owner_type": "shared"
  }'

# 2. 给 agent1 绑定（kb_id 在路径上，agent_id 在 body 里）
curl -X POST http://localhost:8204/api/knowledge-bases/456/bind \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <admin_token>" \
  -d '{"agent_id": 1001}'

# 3. agent1 可见此 shared KB
curl http://localhost:8204/api/knowledge-bases/by-agent/1001 \
  -H "Authorization: Bearer <agent1_token>"
```

### 5.2 升级 private → shared

```bash
# 1. 起初是 private
curl -X POST http://localhost:8204/api/knowledge-bases \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <admin_token>" \
  -d '{
    "kb_code": "KB-FAQ-TEAM",
    "type": "faq",
    "name": "团队 FAQ",
    "owner_type": "private",
    "owner_agent_id": 1001
  }'

# 2. 升级为 shared (清空 owner)：方法是 PUT，代码里没有 PATCH
curl -X PUT http://localhost:8204/api/knowledge-bases/123 \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <admin_token>" \
  -d '{
    "owner_type": "shared",
    "owner_agent_id": null
  }'

# 3. 现在需要 binding 才能访问
curl -X POST http://localhost:8204/api/knowledge-bases/123/bind \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <admin_token>" \
  -d '{"agent_id": 1002}'
```

### 5.3 删除 KB (级联清理)

```bash
# 1. 删除 KB
curl -X DELETE http://localhost:8204/api/knowledge-bases/123 \
  -H "Authorization: Bearer <admin_token>"

# 2. 自动级联: 所有 (agent_id, 123) binding 消失
# 3. 验证——没有 /knowledge-bases/{id}/bindings 这个路径，用下面两条
curl -H "Authorization: Bearer <admin_token>" \
  http://localhost:8204/api/agent-kb-bindings/by-kb/123
# 期望: {"code":0,"message":"查询成功","data":[]}
curl -H "Authorization: Bearer <admin_token>" \
  http://localhost:8204/api/knowledge-bases/123
# 期望: HTTP 404 + {"code":"NOT_FOUND_1002","message":"知识库不存在"}
```

---

## 6. 错误处理最佳实践

### 6.1 客户端处理

```javascript
async function createKB(data) {
  const resp = await fetch('/api/knowledge-bases', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'Authorization': `Bearer ${token}`
    },
    body: JSON.stringify(data)
  });
  const json = await resp.json();
  // code 成功是数字 0，失败是字符串错误码（error_code.go:11-45），用 HTTP 状态码判语义更稳
  if (resp.status !== 200) {
    if (resp.status === 400) {
      throw new ValidationError(json.message);
    }
    throw new ApiError(json.message);
  }
  return json.data;
}
```

### 6.2 重试策略

| 错误码 | 重试策略 |
|--------|----------|
| 0 | 成功, 无需重试 |
| `INVALID_PARAM_1001` / `VALIDATION_4001` | 不重试 (业务校验, HTTP 400) |
| `NOT_FOUND_1002` | 不重试 (资源不存在, 404) |
| `FORBIDDEN_2002` / `PERMISSION_DENIED_2007` | 不重试 (越权, 403) |
| `INTERNAL_ERROR_6002` | 重试 3 次, 指数退避 (500) |

---

## 7. 性能与限制

### 7.1 速率限制

| 端点 | 每分钟限制 |
|------|-----------|
| GET (查询) | 600 req/min |
| POST (创建) | 60 req/min |
| PUT (更新) | 120 req/min |
| DELETE (删除) | 60 req/min |
| 批量绑定 | 10 req/min |

> 上表是设计值。代码里落地的只有全局 per-IP 令牌桶 `RPS:1000 / BucketSize:20000`
> （`internal/router/router.go:212-215`），没有按端点/按方法分档的限流实现。

### 7.2 数据规模

| 项 | 上限 |
|----|------|
| 单智能体 KB 数 | 1000 |
| 单 KB binding 数 | 5000 |
| 批量绑定单次 | 200 |

---

## 8. 客户端调用示例

> 设计稿这一节引用的两个 SDK **仓内都不存在**，不要按它写代码：
> `marketing/internal/client`（仓库里没有 `marketing/` 目录）、`@hivemtk/sdk`
> （全仓 `package.json` 里没有这个包名，前端只有 `embed-sdk/`）。
> go.mod 的模块名是 `hivemtk-user`（`user-server/go.mod:1`），也不是 `hivemtk/sdk`。
> 目前调用这两组接口的方式就是裸 HTTP。

### 8.1 Go（标准库直连）

```go
package kbclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

type KB struct {
	ID        uint   `json:"id"`
	KBCode    string `json:"kb_code"`
	Type      string `json:"type"`
	Name      string `json:"name"`
	OwnerType string `json:"owner_type"`
}

type envelope struct {
	Code    any             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func CreateKB(ctx context.Context, baseURL, token string, in KB) (*KB, error) {
	body, _ := json.Marshal(in)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/api/knowledge-bases", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var env envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK { // 失败时 code 是字符串错误码
		return nil, errors.New(env.Message)
	}
	var out KB
	if err := json.Unmarshal(env.Data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
```

> `baseURL` 用 `http://127.0.0.1:8204`（user-server 监听口，`docs/PORT_REGISTRY.md`）。
> 信封结构见 `internal/pkg/utils/response/response.go:40-44`。

### 8.2 JavaScript（fetch 直连）

```javascript
const BASE = 'http://127.0.0.1:8204';

async function listVisibleKBs(agentId, token) {
  const resp = await fetch(`${BASE}/api/knowledge-bases/by-agent/${agentId}`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  const json = await resp.json();
  return resp.ok ? json.data : Promise.reject(new Error(json.message));
}
```

---

## 9. OpenAPI / 路由表

设计稿写的 `api/openapi/knowledge_group.yaml` **不存在**（`user-server/api` 是编译产物二进制，
不是目录；`find . -name "knowledge_group.yaml"` 零命中）。真实的规范来源是 swag 生成物：

- 规格文件：`user-server/docs/swagger.yaml` / `swagger.json`（`/api/knowledge-bases` 在
  `swagger.yaml:3043`、`/api/knowledge-bases/{id}` 在 `:3077`）
- 在线文档：`http://127.0.0.1:8204/swagger/index.html`（仅本机可访问，
  `internal/router/swagger.go:41-46`），JSON 同口 `GET /api/swagger.json`（`:49-54`）
- 现查现用：`GET /__debug__/routes`（`router.go:207-210`，默认本地/开发环境开放）

下面是 `RegisterRoutes` 的**全量实测端点表**（逐行读
`internal/controller/knowledge_base.go:30-47` 与 `internal/controller/agent_kb_binding.go:32-42`，
前缀 `/api` 来自 `internal/router/router.go:342`）：

| Method | Path | Handler | 注册行 |
|--------|------|---------|--------|
| GET | `/api/knowledge-bases` | `List` | knowledge_base.go:32 |
| GET | `/api/knowledge-bases/by-agent/:aid` | `ListByAgent` | :33 |
| GET | `/api/knowledge-bases/by-type/:type` | `ListByType` | :34 |
| GET | `/api/knowledge-bases/:id` | `Get` | :35 |
| GET | `/api/knowledge-bases/:id/stats` | `Stats` | :36 |
| POST | `/api/knowledge-bases` | `Create` | :37 |
| PUT | `/api/knowledge-bases/:id` | `Update` | :38 |
| DELETE | `/api/knowledge-bases/:id` | `Delete` | :39 |
| POST | `/api/knowledge-bases/:id/bind` | `BindToAgent` | :40 |
| POST | `/api/knowledge-bases/:id/unbind` | `UnbindFromAgent` | :41 |
| GET | `/api/knowledge-bases/:id/versions` | `VersionInfo` | :44 |
| POST | `/api/knowledge-bases/:id/version` | `SwitchVersion` | :45 |
| PUT | `/api/knowledge-bases/:id/canary` | `PutCanary` | :46 |
| GET | `/api/agent-kb-bindings/by-agent/:agentId` | `ListByAgent` | agent_kb_binding.go:34 |
| GET | `/api/agent-kb-bindings/agent/:aid` | `ListByAgent`（旧别名） | :35 |
| GET | `/api/agent-kb-bindings/by-kb/:kbId` | `ListByKB` | :36 |
| GET | `/api/agent-kb-bindings/kb/:kid` | `ListByKB`（旧别名） | :37 |
| PUT | `/api/agent-kb-bindings/by-agent/:agentId` | `ReplaceByAgent` | :38 |
| DELETE | `/api/agent-kb-bindings/:agentId/:kbId` | `UnbindByPath` | :39 |
| POST | `/api/agent-kb-bindings` | `Bind` | :40 |
| POST | `/api/agent-kb-bindings/batch` | `BatchBind` | :41 |
| DELETE | `/api/agent-kb-bindings` | `Unbind` | :42 |

> 版本/灰度那三条（`/:id/versions`、`/:id/version`、`/:id/canary`）是设计稿没列的：
> 它们只动 `knowledge_bases` 的版本三列与 `rag_answer_cache` 命名空间
> （`knowledge_base.go:42-43` 的注释），口径见 `KNOWLEDGE_GROUP_DEPLOY.md` 的版本一节。
> `PUT /:id/canary` 的 body 是 `{"enabled":true,"percent":0}`，两个字段都 `binding:"required"`，
> `percent` 超出 `[0,100]` 直接 400（`controller/knowledge_base.go:357-369`）。

---

## 10. 相关文档

- `docs/architecture/adr/ADR-014-knowledge-group-isolation.md` - ADR（设计依据）
- `docs/operations/KNOWLEDGE_GROUP_DEPLOY.md` - 部署
- `user-server/docs/swagger.yaml` - 由 swag 注释生成的接口规范

> 设计稿这里还列了 `docs/architecture/KNOWLEDGE_GROUP_DESIGN.md` 与
> `docs/operations/KNOWLEDGE_GROUP_MONITORING.md`，两个文件**仓内都不存在**
> （`ls docs/architecture/ | grep -i knowledge`、`ls docs/operations/ | grep -i monitoring` 均空），
> 设计依据只在 ADR-014 里。

---

**最后更新**: 2026-10-09（按 controller 现读路由表逐条订正；原稿 2026-07-31）  
**作者**: HiveMTK API 团队
