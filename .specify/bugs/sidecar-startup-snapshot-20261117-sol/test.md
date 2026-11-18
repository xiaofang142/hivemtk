# Sidecar快照验证

Result: partial（真实专用测试库与本地HTTP回归通过，生产重启/索引链尚未验证）。

依据[assessment.md](assessment.md)和[fix.md](fix.md)。执行`sh_1e8606544001m3Zg7ZrHi9JwDQ`：

```text
go test -p 1 ./internal/app ./internal/etl -run '^Test(SaaSConfigParams_|Sidecar|ParseDocument)' -count=1 -v
app: PASS 1.617s
etl: PASS 0.663s
```

新测试使用专用127.0.0.1:8217测试库的合成参数与两个本地httptest服务，验证首次解析、运行期参数更新后仍用原地址、明确重新装配后才切换、空地址装配后不能动态启用；清理时恢复参数单例与provider。原builtin、Sidecar启用、失败不回退和表格分块回归均通过。

原错误正则没有匹配测试的运行不计通过或RED。配置/JWT/测试密钥提示不作为生产故障。没有生产数据库写、外部解析请求、部署或用户消息发送；最终完整Go门禁仍待更新。
