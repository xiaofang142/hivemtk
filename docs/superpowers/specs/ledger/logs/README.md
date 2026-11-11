# ledger/logs 目录说明

mut_* 驱动家族的常驻轮次日志落点（LOGROOT 锚，check-battery-identity.py A4/A5 消费）。

## 归档说明（I-5，2026-11-12；清洗轮订正后补落盘）

- 历史轮次零引用日志（169 份）压缩归档至 `../archive-logs-2026-11.tar.gz`（tar.gz，本机无 zstd）；
- 初次收口的 tar.gz 与本 README 均被 `.gitignore` 的 `logs/` 规则静默挡下未入库（清洗轮实查发现），
  本文件与归档包已 `git add -f` 强制入库——**入库状态以 `git ls-files` 实查为准**；
- 划线规则：非电池 3 族（R23dedup/R23retention/b-phase）整族不动（A5 反向格）+每驱动族最新轮整轮保留（A4）+被 scripts/docs 引用文件保留；
- 解包：`tar -xzf ../archive-logs-2026-11.tar.gz`（含 docs/ 前缀，在仓根展开）；新日志继续写入本目录不动。
