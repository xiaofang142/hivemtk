# GCRA测试时钟与清理边界评估

Verdict: valid / medium。

## 证据

完整Go测试中`TestGCRASmoothOutput`等待300ms后允许5次，而旧断言把客户端等待当成Redis服务器准确经过300ms，限定为1–4次。依赖`redis_rate/v10`使用Redis TIME决定补额，测试执行和RPC时间同样产生合法补额。另测试夹具在本机Redis DB15执行FlushDB，会删除不属于本用例的数据；本轮不运行该清库路径。

## 论证与决策

- 生产GCRA算法、QPS、Burst、降级及恢复预算不变，不增加测试等待或超时。
- 所有测试使用独立随机键，只删除本用例明确生成的`rate:`键，保留同库其它数据。
- 平滑输出以Redis实际时间、TAT推进和真实补额上界核对，不再根据客户端固定sleep猜测服务器时间。
- 保留正常突发、拒绝、降级、零配置、Reset测试；增加静态清理边界回归，在旧代码仍存在FlushDB时先失败，不运行旧破坏性测试。

## Files likely to change

- `user-server/internal/service/reach_gcra_limiter_test.go`
- 新`user-server/internal/service/reach_gcra_fixture_safety_test.go`
- 本目录fix/test及主验收记录。

## 验证

静态清理回归先RED，然后唯一键测试与真实Redis TIME/TAT检查；仅指定测试键写入，不FlushDB、不触碰生产数据库。确认Redis可达、没有降级或Skip后才将真实GCRA结果列为通过。
