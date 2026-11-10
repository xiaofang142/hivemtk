# J 轮头脑风暴二次审核 + 对抗性检查 + 回归收口（2026-11-10）

- 日期：2026-11-10
- 状态：已定（J 轮 4 项改动全部通过二次审核，无遗留缺陷）
- 目标：J 轮（G-9-11 修复、J-1 双形态登记、J-2 线上抽样、J-3 合并上游+推送）全部落地后的最后闸门——重审每个决策的替代方案与对抗面，确认没有更优/更安全的路径被遗漏
- 约束：本工作区有活跃并行 lane；只 add 本轮改动文件；lane 在途 47 脏 + 13 未跟踪原样保持；清单 K-4-x 属 lane 不碰

## 审核对象（J 轮改动全集）

1. `ba8906eb` fix(test)：r49CaptureLogs 管道死锁→并发排空（3 测试文件 6 helper，+55/-31，纯测试基建零触碰生产逻辑）
2. `4419eb92` Merge upstream/master（bc106975）：hardcode-tunable.baseline 冲突解（双方 ChainMaxSteps 判据合并、条目唯一）
3. `router-list.md`：256 行「11-10 J轮双形态」备注 + 20 行「11-10 J轮线上」备注 + J-2 收口节
4. `未完成任务清单.md`：G-9-11 / J-1 / J-2 / J-3 行清理

## 决策重审（头脑风暴二次审核：当时可选方案 vs 选定 vs 放弃理由）

### D1 管道死锁根因与修复路径
- 旧结论「TG 清扫协程嫌疑、留 TG 泳道立项」被 timeout dump 推翻：栈落在 zerolog→os.File.Write，根因是 os.Pipe() 换 os.Stdout 后 fn() 期间无人读读端、缓冲写满永久阻塞；该用例隔离跑 22s 绿与此一致（缓冲不满不触发）
- 选定：并发排空（go io.Copy + capBuf + drainDone + Cleanup 兜底 w.Close）
- 放弃：改 zerolog 输出（动生产日志语义，风险外溢）；跳过日志捕获（丢唯一行为证据）；加 sleep/重试（治标，全包仍挂）
- 对抗面：t.Fatalf/Goexit 路径 Cleanup 兜底；capBuf 单写者、close(drainDone) 建 happens-before；helper 间 pipe 独立无共享

### D2 远端分叉处置（远端 5a763262/bc106975 已带走同内容修复并持续前进）
- 选定：stash 2 交集脏文件 → merge upstream/master → 手工解 baseline 冲突（双方判据合并、条目唯一）→ stash pop（零冲突）→ 测试 → push 双远端
- 放弃：reset --hard 远端（丢 ba8906eb）；force push（毁 lane 260 文件提交）；plumbing 合并不动工作区（lane git status 观感混乱）
- 对抗面：交集重算恰 2 文件（inbox_ingress_persist.go 删 strings import / smart_cs_orchestrator.go 留 TruncateMediaURL）；merge 后 3 测试文件 drainDone 8/4/12 完好、llm_routing_log.go 与远端一致

### D3 J-1 登记判据
- 选定：以 JSON 实测字段为准（net 是字符串数组、来源轮按 JSON 文件内容定轮禁公式、n=1/2 真登录页跳过、blank=NotFound 预期、404/console.error 设计内 fail-closed、lf 误报 DOM 复核）
- 放弃：路径公式推轮（PowerShell [int] 四舍五入坑已实证）；一律判缺陷（误报淹没真问题）
- 对抗面：防重复 prepend 守卫、仅改 cells[9]、n=1/2 与表头逐字节不变、LF/BOM 保持

### D4 J-2 生产从严
- 选定：白名单剔除 测试/连接/配置/设置/上一步/下一步/新增页；无表单写入；每页 ≤5 点击；403/404 如实登记不判缺陷
- 放弃：复用本地全量白名单（生产可能触发真实 OAuth/测试通知/变更）；跳过线上复核（丢云端形态证据）
- 对抗面：n=33 云端 403（UI 空态降级、0 console 错，产品决策挂账）；n=177 lf 误报线上 DOM 复核（URL 不变、4 表 34 行、自带「用户名」筛选框）；n=173 与 n=13 同组件同数据态说明

### D5 基线冲突解法
- 选定：双方 ChainMaxSteps 判据注释合并、条目唯一
- 放弃：取单边（丢一方判据）；保留重复条目（基线不干净）
- 对抗面：check-hardcode-tunable.py 重跑 TUNABLE 350（原 349+1 回归）、ChainMaxSteps 唯一、冲突标记 0
- 过程教训：解冲突脚本 List.AddRange 在 PowerShell 下静默失效致条目整段丢失，amend 前经「行数/唯一性/脚本重跑」三重核验拦截——合并类改动必须以脚本判定而非行数口算收尾

## 对抗性检查矩阵

| 面 | 检查项 | 结果 |
|---|---|---|
| 并发 | capBuf 写读 happens-before、helper 独立 pipe、Cleanup 兜底 | 通过（18 用例 + 全包 769s）|
| 边界 | n=1/2 跳过、n=8 NotFound、:param 代入、blank/loginForm 分支 | 通过 |
| 安全 | 线上无表单写入、白名单剔变更类、每页 ≤5 点击、生产零副作用 | 通过 |
| 回归 | build/vet/gofmt 绿、3 文件域 18 用例、service 全包 769.206s、基线脚本 | 通过 |
| 纪律 | 逐文件 git status 核对、只 add 本轮文件、lane 47 脏+13 未跟踪原样、K-4-x 未碰 | 通过 |

## 回归证据（merge 后 + lane 脏混合态）

- `go build ./...` exit 0（191.9s）
- `go vet ./internal/service/` exit 0（5.2s）
- `gofmt -l` 3 修复文件：无输出
- 3 文件域 18 用例 ok 22.958s
- service 全包 ok 769.206s（0 FAIL、0 挂死；此前 run#2 的 FAIL/超时归因 lane 16:13 在途改动，dump 无管道死锁特征）
- check-hardcode-tunable.py：TUNABLE 350、新增未入库=1（仅 lane 在途 media_url.go 的 MaxMediaURLLen，非 J 轮改动）
- 双远端 upstream/gitee-upstream master = 4419eb92 == HEAD
- UI 回归：J-1 本地双形态 258 条 + J-2 线上 20 条抽样（采集器自检全过）

## 后续待验证（挂账，非 J 轮债）

- n=33 云端备份 API 403：是否开放云端 admin 备份权限，由产品定
- lane 在途 media_url.go MaxMediaURLLen 的基线登记归 lane
- K-4-1~K-4-4 由 lane 收口

## 结论

J 轮 4 项全部通过二次审核；未完成任务清单 G-9-11 / J-1 / J-2 / J-3 / J-4 行已清零。

提交与推送：
- 代码（G-9-11 修复 + merge 上游）已推送至双远端，HEAD 链为 `ba8906eb → 4419eb92`，后续 lane 的 `0c375b0c`（K 轮收口）线性接续，双远端 master 已同步。
- `router-list.md` 的 J-1/J-2 登记已随 lane 的 `0c375b0c` 一并入库（该提交含 router-list.md +277/-261），远端与工作区逐字节一致（258 行 J轮双形态 / 21 行 J轮线上 / J-2 收口节）。
- 本审核记录作为 J-4 交付物单独提交（docs/brainstorms/）。
- 证据备份于 $env:TEMP（router-list.pre-j1.bak / router-list.pre-j2.bak / j1_round-08..20.json / j2_online_sample.json / tg_j3_full_20261110.log）。
