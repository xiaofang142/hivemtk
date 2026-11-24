# Bug Fix: FA-29 / FA-30 RFM

- Fixed: 2026-11-22
- Status: applied
- Assessment: ./assessment.md

## Changes

customer_rfm.go 两条计算路径共用 rfmCompositeScore，维度1→0、5→100，权重30/30/40。ComputeAll 先读取租户激活规则并冻结副本；不存在时默认，其他故障零计算写入。原始指标使用 nowFunc，无有效消费日期为9999天。

## Deviations from Assessment

核对实际 CustomerRepository.List 上限为200，原分页按500推断页尾会漏读。同步将ComputeAll pageSize改200；501条多页回归验证不提前退出。

## Local verification

专用 PostgreSQL 127.0.0.1:8217，密码通过docker exec注入进程，不落盘。首次 rfm-red-2.log：综合分最低/中间/单维全100、固定时钟2509而非2、激活规则未消费均复现失败。修后 rfm-green-2.log：RFM、原始指标、分层、流失与cron相关全部通过，rc=0（1.4秒级）；包括真实规则仓储双租户相反阈值、规则更新、501条分页、每轮规则单读及注入查询失败零写入。测试初轮只设环境变量未初始化进程tenant.Mode，已按实际模式API修正测试并恢复全局值。

## Delivery boundary

未运行全仓门、真实页面或部署；任务仍待整体交付，不能据专项销项。编译前发现domain_pool.go在途修改未使用logger导入，仅删除该导入解除包编译错误，其余域名改动不归本修复。
