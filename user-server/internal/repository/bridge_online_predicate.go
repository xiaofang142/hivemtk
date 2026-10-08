package repository

// bridgeOnlineSQLPredicate 是「这台桥接账号此刻收得到下行消息吗」在 SQL 侧的唯一写法。
//
// 不能按 status='online' 数：那一列只在 SSE 正常收尾时被 SetOffline 改回离线，扩展崩溃 /
// 浏览器被杀 / 断网都不走那条路径，列就粘在 online 上。实测某台实例 155 行里 152 行标 online，
// 而按最后同步时间判定的真值是 0 —— 于是巡检与管理面报出一屏"在线"，客户消息发出去没人来取。
//
// 三个读侧（渠道总览计数、主动触达选账号、离线回扫快照）必须同一条：各写一份 SQL 就会量出
// 三个不同的"在线数"，运维拿哪一份都对不上另两份。时长由调用方从
// config_params(bridge/online_grace_window) 读好以「秒」传入，仓库层不自己找配置。
//
// 语义与 bridge.isOnlineByLastSync 一致（那一层是 Go 侧逐行判定，这一层是 SQL 侧集合判定）；
// 占位符顺序固定为 (offline 字面量, grace 秒数)，改串时两个读侧的用例都会当场红。
const bridgeOnlineSQLPredicate = "status <> ? AND last_sync_at IS NOT NULL AND now() - last_sync_at < (? * interval '1 second')"

// bridgeStatusOffline 是 status 列的离线态字面量（与 bridge 包 SetOffline 写入值同值）。
const bridgeStatusOffline = "offline"
