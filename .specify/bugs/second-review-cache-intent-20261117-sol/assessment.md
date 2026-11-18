# R6 缓存资格触发业务识别评估

Verdict: valid / high（副作用可能扩展到业务流程，实际复现为意图记录写入）。

## 证据

`answer_cache_scope.go` 在读取缓存前与生成后复核均调用 `IntentRecognizerInterface.Recognize`。具体识别器会异步写入意图记录、累积弱标签，并在客户非空时尝试启动SOP。此前临时真实PG测试确认仅计算命名空间已产生1条意图记录；SOP启动次数未实测。

## 头脑风暴与决策

- 不把原 `Recognize` 全局改成只读，正常生成仍需要记录和业务流程。
- 不通过传空客户ID来隐式关闭部分副作用：它仍会写识别记录和弱标签。
- 选择独立可选 `PreviewIntent(ctx,text)` 能力，缓存资格只调用该入口。真实实现只用已有规则预判，不运行模型、写记录、累积弱标签或启动SOP；规则不能确认则不用最终答案缓存。没有该能力的自定义识别器也保守回到完整生成。
- 不降低投诉、人工、工具、SOP或脚本门；所有正常业务识别继续使用原接口。

## Files likely to change

- `user-server/internal/service/intent_recognition.go`：新增规则只读预判。
- `user-server/internal/service/sales_engine_recall.go`：可选只读能力接口，不改变原识别接口。
- `user-server/internal/service/answer_cache_scope.go`：资格仅调用预判。
- 新 `user-server/internal/service/answer_cache_intent_readonly_test.go`：真实识别器记录副作用、规则命中和未知能力边界。
- `user-server/internal/service/web_chat_e2e_test.go`、`answer_cache_scope_test.go`、`smart_cs_answer_cache_scope_test.go`：缓存正向夹具显式提供只读能力，保留正常识别与投诉/阈值对照。
- 本目录fix/test及总计划记录。

## 验证

先验证缓存资格不能写业务记录；正常 `Recognize` 的写入正向对照必须确实发生。覆盖缺只读能力、未知规则、取消/系统/未绑定租户拒绝，规则命中正常缓存不被全部关闭。执行缓存范围及正常意图识别/CS03知识闭环回归，专用PG，不真实触达。
