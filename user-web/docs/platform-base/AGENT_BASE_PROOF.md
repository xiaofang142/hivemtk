# Agent 基座论证报告（AGNET_BASE_PROOF / R22）

> 日期：2026-09-10。回应质疑：「哪些 AI 编辑器如何控制浏览器、如何操作网页、如何实现基座能力、如何编排 LLM 调用、如何封装提示词工具？我们做的是不是这个方向，只是加了 UI 编排任务和定时任务？」

---

## 1. 调研结论：业界怎么做的（四路对照）

### 1.1 控制浏览器通道

| 框架/产品 | 通道 | 与我们 |
|---|---|---|
| browser-use（70k★） | **CDP 直连**（cdp-use，自动 attach 本机 Chrome，1/2/4s 重连 watchdog） | 同向：我们 NM Host + WS + chrome.debugger，也是"寄生真实浏览器" |
| stagehand（browserbase） | CDP（Understudy 定位层）+ Chrome 扩展分发 | 同向 |
| Playwright-MCP / DevTools-MCP | Playwright/Puppeteer 接管（可连已开 Chrome） | 同向 |
| Midscene（字节） | Playwright/Puppeteer/**Chrome 扩展**/Android 全端 | 同向（扩展形态一致） |
| Claude for Chrome / Comet / Dia | 本地 Chrome 扩展（权限模型+注入分类器） | 同向 |
| Operator / Manus | **云端 VM 浏览器**（隔离） | 异向：我们刻意本地（用户登录态风控最优解，P1 原则，三平台实测无头全被拦） |

**结论：我们"寄生本地真实浏览器"的通道选择与业界主流一致，且是平台风控约束下的必然（非落后）。**

### 1.2 给 LLM 看什么（页面表示）

| 方案 | 代表 | 优劣 |
|---|---|---|
| 索引化 DOM 序列化（`[33]<div/>` 数字索引+selector_map） | browser-use | 省 token、可复现；需 visibility 过滤（我们 @e{N} refs 同构，已有 MAX_NODES=400 + 隐藏过滤） |
| a11y 树文本快照 + ref | Playwright-MCP / DevTools-MCP | 我们 accessibility.js 就是这条路（role "name" @eN） |
| 纯视觉截图+坐标 | Midscene 新版 / Claude CUA / Operator | 抗 DOM 隐藏；token 高、坐标易偏；需 VLM |
| DOM 快照（自研包） | AIPex | — |

**结论：@e{N} refs 快照 = browser-use 索引化 DOM 与 Playwright-MCP a11y 快照的混合体，形态正确；差距在下游用法（见 §2）。**

### 1.3 动作原语与 Agent 循环

- **原语集**：browser-use 25 个注册式 action（Pydantic 参数模型）；Midscene 分 aiAct/aiQuery/aiAssert 三类——**与我们「即时原子/洞察(assert/query)/规划式」三分类完全同构**（R19 落地）。
- **循环**：业界标配 = **observe → reflect（评估上一步）→ plan → act → verify**：
  - browser-use：单 agent 大循环，输出 `thinking/evaluation_previous_goal/memory/next_goal/action[]`，循环检测（20 步指纹）注入 nudge，**独立 judge LLM** 用轨迹+截图判 done；
  - stagehand：三原语微服务化 + self-heal（执行失败重拍快照重推理换 selector）+ 结果缓存；
  - Midscene：planning/locate/insight 三 prompt 分层，缓存 `.cache.yaml`（locate/plan 两类，执行失败标 stale）。

### 1.4 LLM 调用编排

- 结构化输出为主流：browser-use 用 Pydantic `output_format=AgentOutput`（非 function calling）；stagehand 用 `responseFormat: json_schema`（非 function calling）；Midscene 输出结构化动作 JSON。
- **共同点：JSON mode + schema 校验 + 失败重试**。我们已有 DispatchStructured + 3 次退避重试 + 恢复提示——形态一致，差距在 schema 的表达力（见 §2）。

### 1.5 提示词封装

- browser-use：**system prompt 按模型分 8 个 md 模板文件**（system_prompts/ 目录）；
- stagehand：**全部 prompt 集中一个 prompt.ts**（buildActSystemPrompt/buildObserveSystemPrompt/... 工厂函数）；
- Midscene：**prompt 目录化分层**（planning/locate/insight 各自 system-prompt.ts）。
- 共性：①prompt 是**代码资产**（进 git、可 review、可变体）；②工厂函数按场景拼装；③平台/设备知识作为 prompt 片段注入。

---

## 2. 正面回答：我们是不是这个方向？

**是，且骨架同构；但 Brain 只写了"骨架的一半"。**

| 维度 | 业界标准 | 我们现状 | 判定 |
|---|---|---|---|
| 浏览器通道 | CDP/扩展寄真浏览器 | NM+WS+chrome.debugger | ✅ 达标 |
| 页面表示 | 索引化 DOM/a11y 快照 | @e{N} refs 快照 | ✅ 达标 |
| 原语三分类 | act/extract/assert | click.../extract/query/assert | ✅ 达标 |
| **LLM 输出 schema** | thinking/evaluation/memory/next_goal/action[] | `done/reasoning/steps` 三字段 | ❌ **缺 reflect 结构** |
| **Reflect 循环** | 每轮评估上一步+循环检测 nudge+独立 judge | history 平铺 24 条，无评估、无 nudge、无 judge | ❌ **缺** |
| **Prompt 资产化** | 模板文件化分变体 | 硬编码 const 字符串 | ❌ **缺** |
| 缓存/self-heal | selector 缓存+stale 治愈 | 无（重试是盲重试） | ⚠️ 后续 |
| UI 编排+定时任务 | browser-use 无、stagehand 无、Midscene 无 | **有（Task/Cron/Workflow/Dependency）** | ✅ **超出业界开源件** |

**结论：方向完全正确。我们 = "业界 agent 内核" + "业界没有的工程化外壳（任务/定时/依赖/审计/多平台适配器）"。被诟病的是内核太薄——Brain 的 prompt 与循环是 demo 级，不是产品级。本轮补齐。**

---

## 3. R22 改造方案（对标 browser-use/Midscene 内核）

### A. 结构化输出 schema 升级（对标 browser-use AgentOutput）
```json
{
  "thinking": "当前页面状态分析",
  "evaluation_previous_goal": "上一步成功/失败/未知 + 证据",
  "memory": "跨轮要记住的关键事实（提取到的数据、登录态等）",
  "next_goal": "本轮要达成什么",
  "done": false,
  "steps": [{"action": "...", ...}]
}
```
- Go 侧 planSchema 扩字段；全部落 browser_llm_plans（审计）。
- 兼容：旧三字段仍可解析（omitempty）。

### B. Prompt 模板资产化（对标 browser-use system_prompts/ + stagehand prompt.ts）
- `service/brain_prompts.go` 工厂函数组：`buildPlanSystemPrompt(variant)` / `buildRecoveryPrompt()` / `buildJudgePrompt()`；平台知识、动作表、输出 schema 说明全部模板化拼接。
- 变体预留（default/c json 维度），平台知识片段由 PlanPlatformKnowledge 继续注入。

### C. Reflect 循环（对标 browser-use 循环检测 + judge）
- 每轮把上轮 `evaluation_previous_goal`/`memory` 回喂（替代平铺 history——history 保留但降权为附录）。
- **循环检测**：连续 3 轮 action 序列指纹相同 → 注入 nudge（"你在原地打转，换路径或用 markdown 直取数据"）。
- **Judge**：done=true 时追加一次轻量 LLM 校验（goal + 最终 extract/摘要 → approve/reject），reject 则继续循环（上限内）。

### D. 消息管理器（对标 browser-use MessageManager）
- history 条目带状态（success/failed）+ 每轮 memory 增量合并；窗口 24 条保留，但输出格式升级为 `<state>` 结构块。

### 范围外（后续轮）
- 视觉通道（截图喂 VLM）：等需求出现（Midscene/CUA 路线），DOM 通道目前够用。
- selector 缓存： Brain 目标多变，缓存命中率低，暂不做。

---

## 4. 验收（本轮）

- B1：Brain 输出含 thinking/evaluation/memory/next_goal，落库可查。
- B2：循环检测 nudge 生效（构造重复场景验证日志）。
- B3：judge 独立验证 done（拒真/拒伪各至少验证一次路径）。
- B4：三平台 Brain E2E 不劣化（读链路 completed）。
- B5：prompt 全部出自模板工厂，无散落字符串拼接（grep 验证）。
