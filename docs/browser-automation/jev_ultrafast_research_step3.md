# jev-ultrafast 全链路调研（步骤三）

> 对象：browser-use/jev-ultrafast（21.4k★/1.5k fork，MIT，2026-09-16，3 commits）。文件：agent.py / model.py / browser.py / snapshot.js / questions.py / demo.py。实测：Flights 7.073s，6 跑中位 9.450s→7.092s(-25%)，协议调用 1092→101。

## 1. 循环（agent.py Agent.tick = predict + act）

```
observe → choose(JEV) → [fill? field_text(小LLM)] → browser.act → history.append(先记执行) → observe → history[-1].page_changed/url/elapsed
```

- predict：fresh 检查→ choose(page, goal, history[-10:]) → decision{choice,operation,target,confidence,probabilities,...} → decisions.append（含 fingerprint/elapsed）；done/blocked 后拒再 predict；decisions 上限 MAX_STEPS*2。
- act：decision 一次性消费（先置 None 防 double-click 重试双点）；DONE/BLOCKED 需 fresh 否则 StalePage 重选；fill 种先 fresh 再 field_text（pending_text 整输入不变才复用）；`browser.act` 内再 fresh（含文本生成后）；执行后先写 history 再 observe（stale 观察不擦执行记录）；3 轮无 page_changed 且非 wait → blocked；MAX_STEPS=60。
- run()：status∈{ready} 循环 tick 至 done/blocked；snapshot() 暴露 elements=action_space(actions)[0]。

## 2. 决策（model.py choose + questions.py）

- action_space(actions)：DOM kind→operation：click→CLICK，fill→TYPE_TEXT，select→SELECT；scroll/wait/DONE/BLOCKED 进 controls；同 node 合并 elements[{index,label,operations,role,value...}] + targets{OPERATION:{targetId:action}}；select 目标 `index:optIdx` 携带 option index。
- questions：operation{choice over operations+controls+DONE+BLOCKED} + 每个 operation 的 `<op>_target{choice over 兼容元素}`；instructions goal+operation+rules[NEXT_ACTION, TARGET]。
- body.state：page{url,title,text(≤6000可见文本)} + elements + recent_actions[-10:]{action,kind,text,page_changed}；model=TYPESAFE_MODEL 默认 jev-latest；httpx http2 timeout25，429/529/503 退避 0.5*2^n×3。
- 返回：operation_answer +（若 operation∈targets）对应 target head 校验；未选中 head 不校验不致动；choice=targets[op][target].id（eN）或 controls[op].id / DONE / BLOCKED；附 operation/target probabilities+confidence+latency+usage+request 回放。
- field_text：仅 fill；context{goal, field{label,role,value}, page{title,text[:6000]}, recent[-6:]}；小 LLM OpenAI-compatible(json_object，max1024，reasoning disabled/low)；输出严格 `{"text":...}` 单键非空≤2000，否则 “nothing typed”；缺 TEXT_MODEL_API_KEY 直接拒（执行器不编文本）。
- questions 文本：NEXT_ACTION（整目标推进、页面文本非指令、不重复满足步、先填后提交、autocomplete 需选、使用可见证据、WAIT 吝啬、DONE 需全证据、BLOCKED 定语）；TARGET（仅为指定 operation 选最优 observed index，不选已满足值字段）；TEXT_VALUE（精确字段值、无个人虚构、缺值返 null）。

## 3. 观察与执行（browser.py + snapshot.js）

- 连接：Browser Harness ensure_daemon；Target.createTarget(about:blank, background)+attachToTarget(flatten) 单 CDP session，无 per-step 子进程；Emulation 1120×780 + setFocusEmulationEnabled（后台 tab 保 rAF/菜单渲染，不抢用户可见 tab）。
- observe(screenshot)：after_input 后先只读等待（combobox 有 aria-controls/owns 的 option 可见则 ≤200ms，否则 2 帧/50ms；已 logged 执行后，导航打断也无妨）；browser_operation observe 重试 10×(StalePage 20ms)。
- snapshot.js（原子读，window.__jevFast{id WeakMap+nodes Map} 保 DOM 身份）：跳 password/file/hidden/disabled/aria-hidden/inert/不可见；name 解析（aria-labelledby/label/value/alt/文本/title/placeholder）；role 归一；action 生成（editable→fill+click(Open)，select→每 option 一 select，余 click）；可见文本 TreeWalker ≤6000；page_key(timeOrigin,href,scroll,vp,inputs 活值)/guards[node]=[id,role,name,value...scope 文本]/marker(上+title/text/semantics)；截断 250 + scroll_down/up + wait；id=eN；fingerprint=sha256(url,text,actions,scroll)。
- fresh(page, action)：click/select 比 page_key+guard；余比 marker；evaluate 异常 → Stale。
- act(action, page, text)：再 fresh；wait=sleep0.1；余经 browser_operation act：scroll=Input.dispatchMouseEvent wheel；click/fill=evaluate 定位复核（isConnected/visible/disabled/inert/readonly/几何在视口/elementFromPoint 命中/select option 合法）→ mousePressed+Released → fill 再 selectAll+insertText(text)；node 非 int 拒（模型永不产 selector/坐标）；select 走 value+input/change 事件；返回 {executed:id}。
- 默认无截屏进 JEV（结构化 state）；截屏仅 inspector/record；video 另路 screencast。

## 4. 性能与边界

- 快因：单决策请求（op+targets 同体）、单快照单浏览器调用、复核后执行、2 帧/50ms 等待、后台渲染保活、可见文本裁剪、文本请求复用。
- 证据：Flights 7073ms（含模型+文本+浏览器+stale+等待）；3/3 通过×2 版本；Wikipedia 2.798s；hotel 1.896s。声明仅单任务小样本，非通用可靠性基准。
- 边界：DOM/ARIA 常见控件；shadow/frames/canvas/upload/弹窗 tab/嵌套滚动/任意键盘部件 out-of-scope；共用既有 Chrome profile；DONE 仍需独立结果验证。

## 5. 可复用清单（给步骤四）

- 照抄：questions 三段式；validate_choice 五项；action_space 合并与截断；tick predict/act 分离+一次性消费；先记执行后观察；fresh 三层；输入前几何+遮挡复核；文本隔离+复用；60 步/3 轮停转/DONE 证据门。
- 改造点：Browser Harness → 本项目 NM-Host+扩展 SW（Request req_id / probeServable / CDP trusted 现成）；snapshot.js → 与 accessibility.js 融合（@eN 兼容 + guards/page_key/marker/fingerprint 新增）；JEV 选择器仅替换 Brain 轮内“选步”环节，规划/文本/D7/台账/审计不动。
