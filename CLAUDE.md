# CLAUDE.md - 最高规则

> 基础协作规则（身份、提交信息规范、推送前置约束等）遵循全局 `~/.claude/CLAUDE.md`，
> 以下为本项目专属补充规则；与本规则冲突时，项目规则优先。

## 🏆 规则1：每次任务完成自动提交并推送
- 推送目标**唯一**：`gitea`（http://192.168.199.239:8418/Alan/hivemtk）
- **永不推送 GitHub**（upstream/fork），**不发起/不同步 PR** 到原作者仓库
- 任务/轮次完成后，必须自动 `git add` 变更文件
- 自动 `git commit` 含清晰 message（轮次/功能/测试状态）
- 自动 `git push` 到 gitea，并同步部署到阿里云服务器
- 推送前必须 `git fetch` 确认是 fast-forward；若分叉，先 merge 再推
- 不等待确认，不询问，作为工作流固化步骤执行（本规则为全局"不自动推送"的项目级豁免）

---

## 🏆 规则2：发现问题必须立即修复（禁止记录待办）
- 任何检查/测试/审查/日志中发现的问题，**发现当轮立即修复**，闭环验证
- **禁止**"记录待办、下轮处理、专项重构轮"等任何形式的延迟——延迟=逃避
- 修复后必须回归验证（构建+测试+lint），确认归零后才允许提交推送
- 问题只允许两种状态：已修复 / 修复中。不存在"已记录"状态

---

## 🏆 Go 后端五层架构（本项目最高编码准则）

所有 Go 代码必须严格遵循五层架构：

```
Router → Controller → Service → Repository → Model
```

> 目录即层名：`internal/{router,controller,service,repository,model}`。**本仓库无 `handler` 包**：
> `internal/controller/` 下实测 157 个非测试文件、165 个控制器类型全为 `XxxController`（如 `CustomerController`），`XxxHandler` 零命中。
> 新增业务域一律按 `controller` 落地，不引入 `handler` 同义词（避免双命名）。

### 各层职责
| 层 | 职责 | 禁止 |
|----|------|------|
| Router | URL→Controller映射 | 写业务逻辑、内联闭包 |
| Controller | 参数绑定+调Service+返回响应 | 写SQL、业务判断 |
| Service | 业务逻辑 | 直接操作DB |
| Repository | GORM数据访问 | 包含业务判断 |
| Model | 纯数据结构 | 外部依赖 |

### 铁律
1. **禁止跨层调用** — Controller → Repository = 违规
2. **禁止内联闭包**（旧文档亦称"内联 handler"，指同一件事）— `func(c *gin.Context) { c.JSON(...) }` 不允许在 router.go 中出现
3. **每域四层齐全** — 一个业务域 Controller/Service/Repository/Model 四文件必须完整
4. **Router 只做映射** — DI 装配在 main.go 完成

---

## 🏆 API 规范

### 路径
```
/api/{domain}/{resource}           # 用户端
/api/manage/{resource}             # 管理端
```

### 响应格式
```json
{"code": 0, "data": {...}, "message": "ok"}           // 成功
{"code": 400, "message": "参数错误"}                   // 失败
{"code": 0, "data": {"list":[], "total":N}}           // 列表
```

### 错误码
0=成功 400=参数 401=认证 403=权限 404=不存在 409=冲突 500=服务端

---

## 🏆 前端规范

| 项目 | 框架 | API出口 | 状态管理 |
|------|------|---------|---------|
| `user-web/`（唯一业务 Web 工程） | Vue 3.5 + Element Plus 2.9 | `src/api/{module}.js`（实测 98 个模块文件，统一 `import { http } from '@/utils/request'`） | Pinia 3 |
| `embed-sdk/`（客服 Widget 嵌入 SDK，ADR-011） | 原生 JS + Vite，运行时零依赖 | 不直连业务 API，iframe 嵌 `user-web` 的 `/chat/embed/:channel_ref` | 无 |

---

## 🏆 API 测试验收标准（三端验证）

每次修改后必须运行 `scripts/api_verify_full.py`，每个端点验证：
```
【入参】前端发送字段
【返回】API实际响应
【预期】code=0 + 字段完整
【DB】  写操作后SQL确认
```

---

## 🏆 工程文档索引

- 工程开发文档总索引见 `docs/DEV_DOCS_INDEX.md`;架构决策记录见 `docs/architecture/adr/`
