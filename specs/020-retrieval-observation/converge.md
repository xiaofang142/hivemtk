# 020 规格验收：检索分阶段观测

**结论：受控要求通过，整体 partial；未声明生产收益或 Converged。**

| 来源 | 实现与受控证据 | 判定 |
|---|---|---|
| FR-001 | typed日志使用字符串product身份，UUID/数字/空库；兼容迁移保旧数字、NULL和租户，重复执行与注册有真实PG测试。 | 受控通过 |
| FR-002 | 嵌入、实际向量、BM25、RRF、rerank、parent及总体在调用前后计时；未执行阶段NULL，失败调用仍有实测时间。 | 受控通过 |
| FR-003 | 明确tenant及合法carrier范围；缺会话证据清turn/attempt；新日志只存query哈希与字符数，不存查询正文。 | 受控通过 |
| FR-004 | 每searcher至多4后台任务、容量损失可见；受控写入保2秒截止，观测失败不阻业务。 | 受控通过，T006已补 |
| FR-005 | 步骤记录实际操作时间；RecordNodeContext复用旧映射，兼容直写保deadline和租户，异步接收不冒已落库。 | 受控通过 |
| FR-006 | 召回、topK、发布可见性、父块和rerank失败回退不因观测改变。旧检索回归与完整正式Go通过。 | 受控通过 |

## 收敛补强与验证

初次核查发现兼容直写丢掉2秒上下文，已追加 T006 HIGH并真实RED→GREEN，而非修改规格降低承诺。真实PG排他表锁：100毫秒调用返回 DeadlineExceeded；检索主调用正常返回，后台持锁到2秒截止后 Failed=1、Persisted=0、InFlight=0，释放锁后两表无晚到写入。

完整 tracing **2.827秒**、retrieval **5.953秒**通过；之后第三正式全Go退出0，完整 retrieval/tracing/知识仓储/迁移等包通过。完整 lint/audit、build/vet通过。错误/异常/容量下降计测量损失，不当成功写入。

## 未覆盖

生产迁移和新日志未发布，冷/热知识检索及原7.844秒准备间隙没有新的真实分阶段样本，不声称已消除该耗时。FusedCount目前反映传入最终结果数量，不能作为整个中间候选池计数。依赖须遵守context，不能承诺终止任意不合作的Go函数。T005的最终报告/全链真实验收仍未完全结束。

关联：[规格](spec.md) · [计划](plan.md) · [任务](tasks.md) · [总验收](../../.specify/bugs/bridge-safe-quality-20261116-sol/acceptance.md)
