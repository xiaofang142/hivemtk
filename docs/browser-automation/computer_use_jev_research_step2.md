# Computer Use + JEV 全流程调研（步骤二）

> 来源：TypeSafe docs / ZJU-REAL/CUA-JEV / awesome-jev / jev-browser / jev-desktop / Codex computer-use 生态。日期 2026-09-30。

## 1. JEV 本质（一句话）

JEV = System-One 决策模型：给定 state + typed question，返回**结构化可分支答案**（Choice / true-estimate / rubric-score），不写散文、不产坐标/脚本/JS。适用“在已知合法集合里选一个”，不适用开放生成（文案/代码/规划仍归 LLM）。

- API：`POST https://api.typesafe.ai/v1/systemone`，body `{model, state, questions}`；question `{type:"choice", criteria:{id:label|{...}}, instructions:{goal, operation, rules}}`；answer `{choice, probabilities, confidence}`。
- 校验（jev-ultrafast/model.py validate_choice）：choice∈ids、probabilities 键集合==ids、值∈[0,1]有限、sum≈1(±0.02)、choice 即 argmax；非法 → “Invalid TypeSafe response; no action executed”。
- 成本/速度：毫秒级、近零推理成本；与大 LLM 分工：JEV 选操作+目标，小 LLM 仅在 TYPE_TEXT 时写字段值。

## 2. Computer Use 通用全链路（CUA-JEV 提炼）

```
Observe/Generate → Select(JEV choice) → Guard/Execute → Verify/Repeat → Trace(JSONL)
```

1. Observe/Generate（task adapter）：读结构化 state（browser DOM / 桌面 UIA / Excel COM / terminal / fs），枚举**当前合法 intent×channel 候选**：`ActionCandidate{id, capability, args(typed), risk, verifier}`。不虚构不可执行动作；候选数有界（ultrafast 截断 250+scroll/wait）。
2. Select（JEV choice）：选 candidate ID；JEV 不执行 shell 文本、不解读截图像素（CUA-JEV：截图/VLM 仅作 planner 输入，JEV 只收文本）。
3. Guard（ActionGuard，fail-closed）：观察新鲜度（fingerprint/marker/page_key/guard）、candidate 身份、路径 scope、写权限、确认要求；过期/越权 → 拒。
4. Execute（ExecutorRegistry）：JEV 选，driver 执行，应用代码验证。信条：**Jev selects; the driver executes; application code verifies**。通道：DOM / PyAutoGUI(OS 鼠键) / COM / CLI(注册 argv 模板，禁 shell=True 任意串) / MCP(注册 typed tool) / file API。
5. Verify：executor receipt ≠ 成功；独立 verifier 查新 state（URL/title/text/活值/窗口显示值）；JSONL trace 记 choice/execution/timing/evidence；循环至 terminal 条件。

## 3. 关键模式

- 混合动作空间：同一意图多路由（DOM click vs GUI click vs MCP vs CLI）；JEV 在线选路由（如 CLI python_version 0.99 vs DOM，见 CUA-JEV scoped pilot 7.0s）。
- 推测扇出（speculative fan-out）：operation 与各 target heads 同一 state 一次请求；未选中 head 即便非法也不致动（ultrafast choose：只校验被选 head）。
- 文本隔离：TYPE_TEXT 才调小 LLM（OpenAI-compatible，response_format json_object，`{"text":...}` 严格校验：单键、非空、≤2000）；pending_text 复用（整输入不变才复用，防 stale 页误填）；执行器绝不编造字段文本。
- 新鲜度三件套：observe 指纹（url/text/actions/scroll sha256）→ predict 前 fresh 检查 → act 前再 fresh；StalePage → 重新 observe，不擦除已执行记录（先记 history 再 observe）。
- 执行前几何复核：输入前 resolve 当前几何 + 遮挡检查（elementFromPoint/覆盖/visible/disabled/inert/readonly），模型输出永不直变成 selector/坐标/shell/JS。
- 无效循环熔断：连续 3 轮 page_changed=false 且非 wait → blocked；MAX_STEPS 60；DONE 需可见证据全满足，BLOCKED=无支持操作可推进。
- 安全：写/MCP/可视点击显式 opt-in；截图字节/API key 不进 trace；写操作确认闸与本项目 D7 同构。

## 4. 代表实现

- ZJU-REAL/CUA-JEV：ActionCandidate/AgentRuntime/ActionGuard/ExecutorRegistry + observer/capability 接口；4 预定义 Win 工作流（Edge/Excel/VSCode/Explorer，Hybrid vs GUI-Only）+ 开放任务 open-browser/open-desktop/open-computer（model 提 grounded 意图 → 适配器编译 → JEV 选 → guard → 执行 → 验证）；VLM 仅 planner 融合，JEV 仍文本。
- Mrlyk/jev-browser & jkudish/jev-browser：CLI/MCP/library 形态，毫秒决策+近零成本；auth(OpenRouter/TypeSafe key)+skill 安装。
- jev-desktop(Codex)：Codex 懂目标/备文本/定允许动作/验结果；JEV 一请求选 operation+compatible target；复用既有 Codex Computer Use runtime 做 click/fill/scroll/shortcut，不另装浏览器控制器。
- awesome-jev：分层强调 — Jev selects / driver executes / app verifies；Browser Harness/Cua Driver 是 runtime 非模型。

## 5. 对本项目的直接启示

- 脑（大 LLM reflect+plan）保留做“开环规划/文本生成/验收”，步级“选哪个操作/哪个元素”下沉给 JEV choice → 降延迟/成本、消 JSON 抖动。
- 候选必须来自真实观察（现有 snapshot @eN + 新增 JEV 元素表），JEV 输出仅为 id（如 e7），执行前仍走现有 probe→CDP trusted→verify。
- 写操作/确认/台账/审计保持不变并包住 JEV（JEV 只提名，绝不直发不可逆动作）。
