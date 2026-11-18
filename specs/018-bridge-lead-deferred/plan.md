# 蜂桥线索后置 Implementation Plan

**执行**：主智能体executing-plans串行；tasks.md唯一执行清单，整体完成后一次提交。
**架构**：消息服务端资格列→Repository唯一引用任务/租约→Service有界worker→app/router装配→cmd关闭。
**技术**：现有Go/GORM/PostgreSQL/SafeGo/tenant.Fanout，不安装依赖。

## Constitution check
v1.0.4，五层、服务无SQL、租户fail-closed、无裸go、无中途提交。新表与列通过AutoMigrate登记；生产本轮不写数据库。

## 文件范围
- model/ai_sales_champion.go 增服务端资格列；model/bridge_lead_task.go 新任务；pkg/db/migrate.go登记。
- repository/bridge_lead_task.go/test 收口补齐/认领/栅栏/过期，读取持久hub。
- service/inbox_ingress.go、inbox_ingress_persist.go、bridge_lead_worker.go/test 设置可信资格并处理任务。
- service/douyin_lead_miner.go、lead_miner_unified.go/test 详细错误入口与一次触达更新顺序，不改旧冷却/打分。
- bridge/handler_http.go 移除生产同步调用；app/bridge_lead_wiring.go、router/router.go、cmd/api/main.go负责装配/生命周期。
- bridge与app对应回归测试；本目录文档和主记录。

## 测试决策
先通过现有行为入口证明同步矿工与资格缺失，再新增任务公开API真实PG测试。新API缺失引起编译失败不计行为RED。无需网络的worker测试使用依赖接口，生产接口同时作为生命周期边界，不加test-only全局hook。

## 状态
Planned。任务运行后逐条记录RED/GREEN；真实平台、全门禁之前不Converged。
