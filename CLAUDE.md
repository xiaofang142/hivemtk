# CLAUDE.md - 最高规则

## 🏆 规则0：Git 仓库与协作模式(最高指示,优先级最高)

- **只允许使用这两个仓库**,其他仓库一律不用(jungle-hero/hivemtkbot 等 forks 已弃用,勿推送):
  - `gitee.com/xhpmayun/hivemtk`(本地 remote 名 `gitee-upstream`)
  - `github.com/xiaofang142/hivemtk`(本地 remote 名 `upstream`)
- **禁止使用 PR 流程**(不发 PR、不提 PR、不走评审合并)。
- 协作模式为**成员开发直推**:每次开发完成后,将 `master` 直推到上述两个 remote。
- 推送前必须 `git fetch` 确认是 fast-forward;若分叉,先 merge 再推。
- 本规则与规则1冲突时以本规则为准。

## 🏆 规则1：每次任务完成自动提交并推送
- 任务/轮次完成后，必须自动 `git add` 变更文件
- 自动 `git commit` 含清晰 message（轮次/功能/测试状态）
- 自动 `git push` 到 **gitee-upstream(xhpmayun) 和 upstream(xiaofang142)** 双远端
- 不等待确认，不询问，作为工作流固化步骤执行

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
Router → Handler → Service → Repository → Model
```

### 各层职责
| 层 | 职责 | 禁止 |
|----|------|------|
| Router | URL→Handler映射 | 写业务逻辑、内联handler |
| Handler | 参数绑定+调Service+返回响应 | 写SQL、业务判断 |
| Service | 业务逻辑 | 直接操作DB |
| Repository | GORM数据访问 | 包含业务判断 |
| Model | 纯数据结构 | 外部依赖 |

### 铁律
1. **禁止跨层调用** — Handler → Repository = 违规
2. **禁止内联 handler** — `func(c *gin.Context) { c.JSON(...) }` 不允许在 router.go 中出现
3. **每域四层齐全** — 一个业务域 Handler/Service/Repository/Model 四文件必须完整
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
| manage 后台 | Vue3+ElementPlus | src/api/index.js | Pinia |
| uniapp 移动端 | UniApp | src/api/{module}.js | Pinia |

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