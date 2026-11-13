---
name: hivemtk-coding
description: HiveMtk 编码约束编排器——写代码前/中/后的统一动作序列，保证任何成员、任何智能体产出同一标准的代码。当用户要求写代码、编码、开发、新增功能、加接口、改 user-server/、改 user-web/、修 bug、写 controller/service/repository/model、写 Vue 组件、加路由时使用。只编排顺序与自检命令，规则本体一律指向权威文档，禁止在此复述。
---

# 编码约束编排器（统一标准的动作序列）

本 skill 不复述任何规则——规则本体在权威文档（单一事实源）。它只保证**顺序**：
开工读对文档 → 写码踩住红线 → 收工跑对门禁、贴出证据。跳过任何一步 = 未完成。

## 1. 开工三步（写第一行代码之前）

1. **定位应用根目录**：按 `AGENTS.md` §0 向上查找 `deploy/deploy-api.sh`（外层仓启动走兜底）。
2. **读对应标准**（按本轮触碰面，至少一项，逐个 Read）：
   | 触碰面 | 必读 |
   |---|---|
   | `user-server/**` | `docs/standards/BACKEND-CODING-STANDARDS.md`（五层职责/命名/Controller 样板/响应错误表）+ `AGENTS.md` §2 硬规则 |
   | `user-web/**` | `docs/standards/FRONTEND-CODING-STANDARDS.md`（api 层铁律/路由台账/隐私红线/引导体系） |
   | 接口/toast/i18n/路由 | `docs/standards/INTERACTION-STANDARDS.md` |
   | 写测试/判读失败 | `docs/standards/TESTING-STANDARDS.md` |
3. **读可判定契约**：`docs/standards/AI-EDITOR-CONTRACT.md` 逐条对照（C/D 系为编码契约）。
   Bug/新功能走 spec-kit 车道的，按 `CLAUDE.md` 规则 3/4 + `docs/standards/SPEC-KIT-WORKFLOW.md` 执行。

## 2. 写码中红线（一行口径，细节在标准文档）

- 五层 `Router→Controller→Service→Repository→Model` 禁跨层；**本仓无 `handler` 包，新域一律 `XxxController`**。
- 响应 `{"code":0,"data":...,"message":"ok"}`；错误码两条通道不得混用（`CLAUDE.md` §API 规范）。
- Router = 映射 + 依赖装配，禁业务逻辑/内联闭包；Service 不碰 `gin.Context`（流式用 `http.ResponseWriter`）。
- 新功能必须同步三层引导 + i18n 双语（`FRONTEND-CODING-STANDARDS.md` §1.6）；数据零出域。
- 发现问题当轮修（规则 2）；子 Agent 线性串行（规则 0b）；不踩并行会话在途文件（规则 0）。

## 3. 收工自检（宣称完成之前，全绿 + 贴证据）

按 `CLAUDE.md` §3.5 验证口径表跑（改哪层跑哪条），另有本仓三道静态门：

```bash
python3 scripts/check-layer-boundary.py    # 五层 import 边界（基线已清零，新增即红）
go build ./... && go vet ./...             # 在 user-server/ 下
# Go 测试前先导出 POSTGRES_TEST_*（§3.5 有完整命令与 28P01 判据——环境失败不是 bug）
```

宣称「已完成/已修复/已通过」必须附真实证据（测试输出/快照文本/接口状态码/截图路径），
无证据不得声称——契约 E1。

## 4. 完成后

改动范围对应发布链（`user-server/**`→链 A、`user-web/bridge/**`→链 B、上链→链 C）：
**加载 skill `hivemtk-release`** 按场景执行；只改 `user-web/src/**`（SPA）不走发布链（见该 skill §7）。
整体完成一次性 commit + push 双远端（规则 0/1；commit-msg 白名单见 `.githooks/`）。
