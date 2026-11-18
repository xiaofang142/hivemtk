# R5 验证

Result: partial。

[评估](assessment.md) · [修复](fix.md)

服务端测试包括内层渠道/账号/限定会话与query-body冲突拒绝、混合批次零实时/历史副作用，及省略继承、别名、合法多会话。中转保留正常调用、原取消/超时/凭证与四接口，新增内层冲突、多会话与ACK对照。

目标GREEN：Go `sh_1e6fa9591001C9W7sr4QSOmsIe` 0.755秒，JS `sh_1e6fa9594001uHdz4i4irQFXaV` 四文件65项0.919秒。最新完整扩展`sh_1e75fc031001DV6a2fOA4RMf0B`通过104文件1294项、跳过7项（78.07秒），仍有五次jsdom导航提示。未访问生产outbox，不声明跨租户攻击实测或已部署。
