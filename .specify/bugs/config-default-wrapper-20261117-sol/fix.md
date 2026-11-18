# 修复记录

依据[assessment.md](assessment.md)，只扩展静态默认值识别器及合成源码测试，未改变webhook Raw读取的0哨兵、种子或实际30秒/60秒/100/180默认。

- 同文件包装必须完整匹配直接positiveIntOr/positiveDurationOr返回，或正数分支后明确返回默认值；不一致包装报告冲突，无法证明的返回不参与推断。
- 只有明确`time.Duration(n) * time.Second`的GetInt包装将默认时长换算为秒数，普通Duration不猜测量纲。
- 保留原B1/B2/B3识别及覆盖率基线，没有新增键名豁免或刷新基线。

真实RED：四种有效包装被原逻辑算为0；补充秒数量纲测试也先失败。修复后Python五个测试通过。完整默认值门比对230条、无不一致；其所在audit随后因两份外部删除脚本停止，不把该次audit描述为整体通过。
