# API Contract: CRM 主数据（第一部分）

> 路由口径（2026-11-24 核对）：权威源为 `user-server/internal/router/crm_routes.go`（`auth.Group("/manage/crm")`）。
> 契约早期草稿给客户资源挂了 `/customers` 中间段，实际路由在部分端点上没有该段，已按下表修正；
> 未在路由中找到的端点一律标「待核对」，不臆造。响应信封 `{"code":0,"data":...,"message":"ok"}` 不变。

## 客户/联系人/公司/供应商/服务商 CRUD
- `GET /api/manage/crm/customers` — 列表，按数据权限过滤；query: account_id, industry, owner_id, keyword
- `POST /api/manage/crm/customers` — 新建
- `GET /api/manage/crm/customers/:id` — 详情
- `PUT /api/manage/crm/customers/:id` — 修改
- `DELETE /api/manage/crm/customers/:id` — 删除（写审计）
- 同类资源实际只找到 `POST /api/manage/crm/contacts`、`POST /api/manage/crm/suppliers`、
  `POST /api/manage/crm/service-providers`；`/companies` 及各资源的 GET/PUT/DELETE **待核对**（路由未找到）。

## 去重/合并
- `POST /api/manage/crm/dedupe/scan` — 按手机号/邮箱/外部 ID/名称相似度识别重复（返回候选组）
  （契约原写 `/api/manage/crm/customers/dedupe`，与实际路径形状不同，语义是否同一端点**待核对**）
- `POST /api/manage/crm/merge` — 合并，body 含 sourceIds/targetId；写合并审计与 OneID（实际另可 `GET /api/manage/crm/merge/audit`）

## 导入导出
- `POST /api/manage/crm/import`
- `GET /api/manage/crm/export` — 按权限过滤字段与范围；写导出审计

## 360 与交叉检索
- `GET /api/manage/crm/360/:id` — 单账号全链路视图
- `GET /api/manage/crm/cross-industry` — 跨账号行业资源交叉检索（契约原写 `/resources/search`，映射语义**待核对**）

## AI
- `POST /api/manage/crm/ai/analyze` — 画像/分层/商机识别/跟进建议；每条建议附 evidence
  （契约原写 `GET /api/manage/crm/customers/:id/ai-profile`，动词与路径均不同，映射**待核对**）

## 审计
- `GET /api/manage/crm/operation-logs` — 按对象/操作人/时间筛选（**待核对**：路由未找到）

## 归档
- `POST /api/manage/crm/clue/archive` — 线索归档（契约未列，实际路由见上）
